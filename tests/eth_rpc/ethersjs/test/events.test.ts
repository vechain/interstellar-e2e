import { expect } from 'chai';
import {
  Contract,
  ContractFactory,
  EventLog,
  JsonRpcProvider,
  Wallet,
  id,
  toBeHex,
} from 'ethers';
import {
  loadStorageArtifact,
  makeProvider,
  makeWallet,
  NODE2_ADDRESS,
  NODE2_KEY,
  TEST_SENDER_ADDRESS,
  TEST_SENDER_KEY,
} from '../src/fixtures';

describe('Events — subscriptions & historical filters', () => {
  const artifact = loadStorageArtifact();
  let provider: JsonRpcProvider;
  let wallet: Wallet;
  let contract: Contract;
  let deployBlock: number;

  before(async () => {
    provider = makeProvider();
    wallet = makeWallet(TEST_SENDER_KEY, provider);
    // ethers v6 defaults to 4s polling; tighten so this suite finishes quickly.
    provider.pollingInterval = 500;

    const factory = new ContractFactory(artifact.abi, artifact.bytecode, wallet);
    const deployed = await factory.deploy();
    const receipt = await deployed.deploymentTransaction()!.wait();
    deployBlock = receipt!.blockNumber;
    const address = await deployed.getAddress();
    contract = new Contract(address, artifact.abi, wallet);
  });

  afterEach(async () => {
    await contract.removeAllListeners();
    provider.removeAllListeners();
  });

  it('contract.on("Set") fires when set() is called', async function () {
    this.timeout(60_000);

    const seen = new Promise<{ who: string; value: bigint }>((resolve) => {
      contract.on('Set', (who: string, value: bigint) => {
        resolve({ who, value });
      });
    });

    const tx = await contract.set(7n);
    await tx.wait();

    const ev = await seen;
    expect(ev.who.toLowerCase()).to.equal(TEST_SENDER_ADDRESS.toLowerCase());
    expect(ev.value).to.equal(7n);
  });

  it('queryFilter returns historical Set logs since deploy', async () => {
    const tx = await contract.set(123n);
    await tx.wait();

    const events = await contract.queryFilter(
      contract.filters.Set!(),
      deployBlock,
      'latest',
    );
    expect(events.length).to.be.greaterThan(0);
    const last = events[events.length - 1] as EventLog;
    expect(last.args).to.exist;
    expect(last.args.value).to.equal(123n);
    expect((last.args.who as string).toLowerCase()).to.equal(
      TEST_SENDER_ADDRESS.toLowerCase(),
    );
  });

  it('provider.on("block") observes at least one new block', async function () {
    this.timeout(60_000);

    const seen = new Promise<number>((resolve) => {
      provider.on('block', (n: number) => resolve(n));
    });

    const n = await seen;
    expect(n).to.be.a('number').and.greaterThan(0);
  });

  it('HTTP filter trio (eth_newFilter / eth_getFilterChanges / eth_uninstallFilter)', async function () {
    this.timeout(60_000);

    const address = await contract.getAddress();
    const topic = id('Set(address,uint256)');
    const fromBlock = toBeHex(await provider.getBlockNumber());

    // 1. eth_newFilter — register interest in Set events from our contract
    const filterId = (await provider.send('eth_newFilter', [
      { fromBlock, toBlock: 'latest', address, topics: [topic] },
    ])) as string;
    expect(filterId).to.match(/^0x[0-9a-fA-F]+$/);

    try {
      // 2. trigger a Set, then poll eth_getFilterChanges
      const tx = await contract.set(8675309n);
      await tx.wait();

      let changes: unknown[] = [];
      const deadline = Date.now() + 30_000;
      while (Date.now() < deadline) {
        changes = (await provider.send('eth_getFilterChanges', [filterId])) as unknown[];
        if (changes.length > 0) break;
        await new Promise((r) => setTimeout(r, 500));
      }
      expect(changes.length, 'eth_getFilterChanges').to.be.greaterThan(0);

      const log = changes[0] as { topics: string[]; data: string; address: string };
      expect(log.address.toLowerCase()).to.equal(address.toLowerCase());
      expect(log.topics[0]).to.equal(topic);
    } finally {
      // 3. eth_uninstallFilter — must return true
      const removed = (await provider.send('eth_uninstallFilter', [filterId])) as boolean;
      expect(removed, 'eth_uninstallFilter').to.equal(true);
    }
  });

  it('eth_getLogs accepts address[] and OR-of-topic / null-slot filter shapes', async function () {
    this.timeout(60_000);
    // ethers v6's high-level provider.getLogs forwards address arrays and
    // topic-array (OR-of-topic) / null-slot (wildcard) filter shapes verbatim
    // to eth_getLogs. Exercise all three on a real emitter so the wire
    // serialization is verified end-to-end, not just the well-formed-response
    // path.
    const address = await contract.getAddress();
    const setTopic = id('Set(address,uint256)');
    const tippedTopic = id('Tipped(address,uint256)');

    // Emit one of each event so the OR-filter has matches on both signatures.
    await (await contract.set(2024n)).wait();
    await (await contract.tip({ value: 5n })).wait();

    // address[]: real contract + zero address. Zero matches nothing, so every
    // returned log must still belong to the real contract.
    const multiAddr = await provider.getLogs({
      fromBlock: deployBlock,
      toBlock: 'latest',
      address: [address, '0x0000000000000000000000000000000000000000'],
    });
    expect(multiAddr, 'multi-address result').to.be.an('array').and.length.greaterThan(0);
    for (const l of multiAddr) {
      expect(l.address.toLowerCase()).to.equal(address.toLowerCase());
    }

    // topics: [[setTopic, tippedTopic]] — OR at position 0. Result must
    // include at least one log for each signature.
    const orTopic = await provider.getLogs({
      fromBlock: deployBlock,
      toBlock: 'latest',
      address,
      topics: [[setTopic, tippedTopic]],
    });
    const sigs = new Set(orTopic.map((l) => l.topics[0]));
    expect(sigs.has(setTopic), 'OR-of-topic must include Set').to.equal(true);
    expect(sigs.has(tippedTopic), 'OR-of-topic must include Tipped').to.equal(true);

    // topics: [setTopic, null] — null in position 1 is a wildcard on the
    // indexed-sender topic; must still match Set events.
    const nullSlot = await provider.getLogs({
      fromBlock: deployBlock,
      toBlock: 'latest',
      address,
      topics: [setTopic, null],
    });
    expect(nullSlot.length, 'null-slot must match Set logs').to.be.greaterThan(0);
    for (const l of nullSlot) {
      expect(l.topics[0]).to.equal(setTopic);
    }
  });

  it('queryFilter with an indexed-arg filter matches only that address', async function () {
    this.timeout(90_000);
    // Send Set from TEST_SENDER and Set from NODE2; filter on TEST_SENDER must
    // see only its own emission.
    const node2Wallet = makeWallet(NODE2_KEY, provider);
    const senderTx = await contract.set(1001n);
    const senderReceipt = await senderTx.wait();
    const node2Tx = await (contract.connect(node2Wallet) as Contract).set(1002n);
    const node2Receipt = await node2Tx.wait();
    const fromBlock = Math.min(senderReceipt!.blockNumber, node2Receipt!.blockNumber);

    const senderOnly = await contract.queryFilter(
      contract.filters.Set!(TEST_SENDER_ADDRESS),
      fromBlock,
      'latest',
    );
    const node2Only = await contract.queryFilter(
      contract.filters.Set!(NODE2_ADDRESS),
      fromBlock,
      'latest',
    );

    const senderValues = senderOnly.map((e) => (e as EventLog).args.value);
    const node2Values = node2Only.map((e) => (e as EventLog).args.value);

    expect(senderValues, 'sender-only').to.include(1001n);
    expect(senderValues, 'sender-only').to.not.include(1002n);
    expect(node2Values, 'node2-only').to.include(1002n);
    expect(node2Values, 'node2-only').to.not.include(1001n);
  });
});
