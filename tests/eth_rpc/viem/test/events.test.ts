import { expect } from 'chai';
import {
  createPublicClient,
  http,
  toEventSelector,
  zeroAddress,
  type AbiEvent,
  type Address,
  type PublicClient,
} from 'viem';
import {
  getHttpUrl,
  loadStorageArtifact,
  makeWalletClient,
  rpc,
  TEST_SENDER_ADDRESS,
  TEST_SENDER_KEY,
  NODE2_ADDRESS,
  NODE2_KEY,
} from '../src/fixtures';

const artifact = loadStorageArtifact();
const setEvent = artifact.abi.find(
  (x) => x.type === 'event' && x.name === 'Set',
) as AbiEvent;
const tippedEvent = artifact.abi.find(
  (x) => x.type === 'event' && x.name === 'Tipped',
) as AbiEvent;
const setTopic = toEventSelector('Set(address,uint256)');
const tippedTopic = toEventSelector('Tipped(address,uint256)');

describe('Events — subscriptions & historical filters', () => {
  // Poll fast so watch* actions resolve quickly (default is 4s).
  let client: PublicClient;
  let wallet: Awaited<ReturnType<typeof makeWalletClient>>;
  let address: Address;
  let deployBlock: bigint;

  before(async () => {
    client = createPublicClient({ transport: http(getHttpUrl()), pollingInterval: 500 });
    wallet = await makeWalletClient(TEST_SENDER_KEY);
    const hash = await wallet.deployContract({ abi: artifact.abi, bytecode: artifact.bytecode });
    const receipt = await client.waitForTransactionReceipt({ hash });
    address = receipt.contractAddress!;
    deployBlock = receipt.blockNumber;
  });

  async function set(value: bigint, key = TEST_SENDER_KEY): Promise<void> {
    const w = await makeWalletClient(key);
    const hash = await w.writeContract({ address, abi: artifact.abi, functionName: 'set', args: [value] });
    await client.waitForTransactionReceipt({ hash });
  }

  it('watchContractEvent("Set") fires when set() is called', async function () {
    this.timeout(60_000);
    const seen = new Promise<{ who: string; value: bigint }>((resolve) => {
      const unwatch = client.watchContractEvent({
        address,
        abi: artifact.abi,
        eventName: 'Set',
        onLogs: (logs) => {
          const args = (logs[0] as { args: { who: string; value: bigint } }).args;
          unwatch();
          resolve(args);
        },
      });
    });
    await set(7n);
    const ev = await seen;
    expect(ev.who.toLowerCase()).to.equal(TEST_SENDER_ADDRESS.toLowerCase());
    expect(ev.value).to.equal(7n);
  });

  it('getContractEvents returns historical Set logs since deploy', async () => {
    await set(123n);
    const events = await client.getContractEvents({
      address,
      abi: artifact.abi,
      eventName: 'Set',
      fromBlock: deployBlock,
      toBlock: 'latest',
    });
    expect(events.length).to.be.greaterThan(0);
    const last = events[events.length - 1] as { args: { who: string; value: bigint } };
    expect(last.args.value).to.equal(123n);
    expect(last.args.who.toLowerCase()).to.equal(TEST_SENDER_ADDRESS.toLowerCase());
  });

  it('HTTP filter trio (createContractEventFilter / getFilterChanges / uninstallFilter)', async function () {
    this.timeout(60_000);
    const filter = await client.createContractEventFilter({
      address,
      abi: artifact.abi,
      eventName: 'Set',
      fromBlock: await client.getBlockNumber(),
    });

    try {
      await set(8675309n);
      let changes: unknown[] = [];
      const deadline = Date.now() + 30_000;
      while (Date.now() < deadline) {
        changes = await client.getFilterChanges({ filter });
        if (changes.length > 0) break;
        await new Promise((r) => setTimeout(r, 500));
      }
      expect(changes.length, 'getFilterChanges').to.be.greaterThan(0);
      const log = changes[0] as { address: string; topics: string[] };
      expect(log.address.toLowerCase()).to.equal(address.toLowerCase());
      expect(log.topics[0]).to.equal(setTopic);
    } finally {
      const removed = await client.uninstallFilter({ filter });
      expect(removed, 'uninstallFilter').to.equal(true);
    }
  });

  it('getFilterLogs returns the full matching log set for a contract-event filter', async function () {
    this.timeout(60_000);
    const filter = await client.createContractEventFilter({
      address,
      abi: artifact.abi,
      eventName: 'Set',
      fromBlock: deployBlock,
    });
    try {
      await set(13579n);
      let logs: unknown[] = [];
      const deadline = Date.now() + 30_000;
      while (Date.now() < deadline) {
        logs = await client.getFilterLogs({ filter });
        if (logs.length > 0) break;
        await new Promise((r) => setTimeout(r, 500));
      }
      expect(logs.length, 'getFilterLogs').to.be.greaterThan(0);
      const log = logs[0] as { topics: string[] };
      expect(log.topics[0]).to.equal(setTopic);
    } finally {
      await client.uninstallFilter({ filter });
    }
  });

  it('getLogs accepts address[] and OR-of-event / null-slot filter shapes', async function () {
    this.timeout(90_000);
    await set(2024n);
    const tipWallet = await makeWalletClient(TEST_SENDER_KEY);
    const tipHash = await tipWallet.writeContract({
      address,
      abi: artifact.abi,
      functionName: 'tip',
      value: 5n,
    });
    await client.waitForTransactionReceipt({ hash: tipHash });

    // address[]: real contract + zero address. Every returned log belongs to the
    // real contract.
    const multiAddr = await client.getLogs({
      address: [address, zeroAddress],
      event: setEvent,
      fromBlock: deployBlock,
      toBlock: 'latest',
    });
    expect(multiAddr, 'multi-address result').to.be.an('array').and.length.greaterThan(0);
    for (const l of multiAddr) {
      expect(l.address.toLowerCase()).to.equal(address.toLowerCase());
    }

    // events: [Set, Tipped] — OR at topic position 0.
    const orTopic = await client.getLogs({
      address,
      events: [setEvent, tippedEvent],
      fromBlock: deployBlock,
      toBlock: 'latest',
    });
    const sigs = new Set(orTopic.map((l) => l.topics[0]));
    expect(sigs.has(setTopic), 'OR must include Set').to.equal(true);
    expect(sigs.has(tippedTopic), 'OR must include Tipped').to.equal(true);

    // null-slot wildcard via raw eth_getLogs (topics: [setTopic, null]).
    const nullSlot = (await rpc(client, 'eth_getLogs', [
      { fromBlock: '0x0', toBlock: 'latest', address, topics: [setTopic, null] },
    ])) as Array<{ topics: string[] }>;
    expect(nullSlot.length, 'null-slot must match Set logs').to.be.greaterThan(0);
    for (const l of nullSlot) {
      expect(l.topics[0]).to.equal(setTopic);
    }
  });

  it('getLogs with an indexed-arg filter matches only that address', async function () {
    this.timeout(90_000);
    await set(1001n);
    await set(1002n, NODE2_KEY);

    const senderOnly = await client.getLogs({
      address,
      event: setEvent,
      args: { who: TEST_SENDER_ADDRESS },
      fromBlock: deployBlock,
      toBlock: 'latest',
    });
    const node2Only = await client.getLogs({
      address,
      event: setEvent,
      args: { who: NODE2_ADDRESS },
      fromBlock: deployBlock,
      toBlock: 'latest',
    });

    const senderValues = senderOnly.map((l) => (l as { args: { value: bigint } }).args.value);
    const node2Values = node2Only.map((l) => (l as { args: { value: bigint } }).args.value);

    expect(senderValues, 'sender-only').to.include(1001n);
    expect(senderValues, 'sender-only').to.not.include(1002n);
    expect(node2Values, 'node2-only').to.include(1002n);
    expect(node2Values, 'node2-only').to.not.include(1001n);
  });
});
