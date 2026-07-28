import { expect } from 'chai';
import { Web3 } from 'web3';
import {
  contractSet,
  deployContract,
  loadStorageArtifact,
  makeWeb3,
  rpc,
  sendEip1559,
  NODE2_ADDRESS,
  NODE2_KEY,
  TEST_SENDER_ADDRESS,
  TEST_SENDER_KEY,
} from '../src/fixtures';

const ZERO_ADDRESS = '0x0000000000000000000000000000000000000000';

describe('Events — historical filters & HTTP polling filters', () => {
  const artifact = loadStorageArtifact();
  let web3: Web3;
  let address: string;
  let deployBlock: bigint;
  let setTopic: string;
  let tippedTopic: string;

  before(async () => {
    web3 = makeWeb3();
    setTopic = web3.utils.keccak256('Set(address,uint256)');
    tippedTopic = web3.utils.keccak256('Tipped(address,uint256)');
    const d = await deployContract(web3, TEST_SENDER_KEY, artifact);
    address = d.address;
    deployBlock = d.blockNumber;
  });

  it('getPastEvents returns historical Set logs since deploy', async () => {
    await contractSet(web3, TEST_SENDER_KEY, address, artifact.abi, 123n);

    const contract = new web3.eth.Contract(artifact.abi, address);
    const events = await contract.getPastEvents('Set', {
      fromBlock: deployBlock,
      toBlock: 'latest',
    });
    expect(events.length).to.be.greaterThan(0);
    const last = events[events.length - 1] as { returnValues: Record<string, unknown> };
    expect(last.returnValues.value).to.equal(123n);
    expect((last.returnValues.who as string).toLowerCase()).to.equal(
      TEST_SENDER_ADDRESS.toLowerCase(),
    );
  });

  it('HTTP filter trio (eth_newFilter / eth_getFilterChanges / eth_uninstallFilter)', async function () {
    this.timeout(60_000);

    const fromBlock = '0x' + (await web3.eth.getBlockNumber()).toString(16);
    const filterId = (await rpc(web3, 'eth_newFilter', [
      { fromBlock, toBlock: 'latest', address, topics: [setTopic] },
    ])) as string;
    expect(filterId).to.match(/^0x[0-9a-fA-F]+$/);

    try {
      await contractSet(web3, TEST_SENDER_KEY, address, artifact.abi, 8675309n);

      let changes: Array<{ topics: string[]; address: string }> = [];
      const deadline = Date.now() + 30_000;
      while (Date.now() < deadline) {
        changes = (await rpc(web3, 'eth_getFilterChanges', [filterId])) as typeof changes;
        if (changes.length > 0) break;
        await new Promise((r) => setTimeout(r, 500));
      }
      expect(changes.length, 'eth_getFilterChanges').to.be.greaterThan(0);
      expect(changes[0].address.toLowerCase()).to.equal(address.toLowerCase());
      expect(changes[0].topics[0]).to.equal(setTopic);
    } finally {
      const removed = (await rpc(web3, 'eth_uninstallFilter', [filterId])) as boolean;
      expect(removed, 'eth_uninstallFilter').to.equal(true);
    }
  });

  it('eth_getFilterLogs returns the full matching log set for a log filter', async function () {
    this.timeout(60_000);
    // eth_getFilterLogs returns every log matching the filter (unlike
    // eth_getFilterChanges, which is incremental since the last poll).
    const fromBlock = '0x' + (await web3.eth.getBlockNumber()).toString(16);
    const filterId = (await rpc(web3, 'eth_newFilter', [
      { fromBlock, toBlock: 'latest', address, topics: [setTopic] },
    ])) as string;
    try {
      await contractSet(web3, TEST_SENDER_KEY, address, artifact.abi, 13579n);
      let logs: Array<{ topics: string[]; address: string }> = [];
      const deadline = Date.now() + 30_000;
      while (Date.now() < deadline) {
        logs = (await rpc(web3, 'eth_getFilterLogs', [filterId])) as typeof logs;
        if (logs.length > 0) break;
        await new Promise((r) => setTimeout(r, 500));
      }
      expect(logs.length, 'eth_getFilterLogs').to.be.greaterThan(0);
      expect(logs[0].topics[0]).to.equal(setTopic);
      expect(logs[0].address.toLowerCase()).to.equal(address.toLowerCase());
    } finally {
      await rpc(web3, 'eth_uninstallFilter', [filterId]);
    }
  });

  it('getLogs accepts address[] and OR-of-topic / null-slot filter shapes', async function () {
    this.timeout(90_000);
    // Emit one of each event so the OR-filter has matches on both signatures.
    await contractSet(web3, TEST_SENDER_KEY, address, artifact.abi, 2024n);
    const contract = new web3.eth.Contract(artifact.abi, address);
    const tip = contract.methods.tip();
    const tipGas = await tip.estimateGas({ from: TEST_SENDER_ADDRESS, value: 5n });
    await sendEip1559(web3, TEST_SENDER_KEY, {
      to: address,
      data: tip.encodeABI(),
      value: 5n,
      gas: tipGas,
    });

    // address[]: real contract + zero address. Every returned log must still
    // belong to the real contract.
    const multiAddr = await web3.eth.getPastLogs({
      fromBlock: deployBlock,
      toBlock: 'latest',
      address: [address, ZERO_ADDRESS],
    });
    expect(multiAddr, 'multi-address result').to.be.an('array').and.length.greaterThan(0);
    for (const l of multiAddr) {
      expect((l as { address: string }).address.toLowerCase()).to.equal(address.toLowerCase());
    }

    // topics: [[setTopic, tippedTopic]] — OR at position 0.
    const orTopic = await web3.eth.getPastLogs({
      fromBlock: deployBlock,
      toBlock: 'latest',
      address,
      topics: [[setTopic, tippedTopic]],
    });
    const sigs = new Set((orTopic as Array<{ topics: string[] }>).map((l) => l.topics[0]));
    expect(sigs.has(setTopic), 'OR-of-topic must include Set').to.equal(true);
    expect(sigs.has(tippedTopic), 'OR-of-topic must include Tipped').to.equal(true);

    // topics: [setTopic, null] — wildcard on the indexed-sender slot.
    const nullSlot = await web3.eth.getPastLogs({
      fromBlock: deployBlock,
      toBlock: 'latest',
      address,
      topics: [setTopic, null],
    });
    expect((nullSlot as unknown[]).length, 'null-slot must match Set logs').to.be.greaterThan(0);
    for (const l of nullSlot as Array<{ topics: string[] }>) {
      expect(l.topics[0]).to.equal(setTopic);
    }
  });

  it('getPastEvents with an indexed-arg filter matches only that address', async function () {
    this.timeout(120_000);
    // Set from TEST_SENDER and from NODE2; filter who=TEST_SENDER must see only
    // its own emission.
    const senderReceipt = await contractSet(web3, TEST_SENDER_KEY, address, artifact.abi, 1001n);
    const node2Receipt = await contractSet(web3, NODE2_KEY, address, artifact.abi, 1002n);
    const fromBlock =
      (senderReceipt.blockNumber as bigint) < (node2Receipt.blockNumber as bigint)
        ? (senderReceipt.blockNumber as bigint)
        : (node2Receipt.blockNumber as bigint);

    const contract = new web3.eth.Contract(artifact.abi, address);
    const senderOnly = await contract.getPastEvents('Set', {
      fromBlock,
      toBlock: 'latest',
      filter: { who: TEST_SENDER_ADDRESS },
    });
    const node2Only = await contract.getPastEvents('Set', {
      fromBlock,
      toBlock: 'latest',
      filter: { who: NODE2_ADDRESS },
    });

    const senderValues = (senderOnly as Array<{ returnValues: Record<string, unknown> }>).map(
      (e) => e.returnValues.value,
    );
    const node2Values = (node2Only as Array<{ returnValues: Record<string, unknown> }>).map(
      (e) => e.returnValues.value,
    );

    expect(senderValues, 'sender-only').to.include(1001n);
    expect(senderValues, 'sender-only').to.not.include(1002n);
    expect(node2Values, 'node2-only').to.include(1002n);
    expect(node2Values, 'node2-only').to.not.include(1001n);
  });
});
