import { expect } from 'chai';
import { Web3 } from 'web3';
import {
  collectStrings,
  makeWeb3,
  rpc,
  sendEip1559,
  NODE2_ADDRESS,
  TEST_SENDER_ADDRESS,
  TEST_SENDER_KEY,
} from '../src/fixtures';

describe('Network & node info (supported on Thor)', () => {
  let web3: Web3;
  before(() => {
    web3 = makeWeb3();
  });

  it('net.getId returns a network id', async () => {
    const id = await web3.eth.net.getId();
    expect(id).to.be.a('bigint');
    expect(id >= 0n).to.equal(true);
  });

  it('net.isListening returns true', async () => {
    const listening = await web3.eth.net.isListening();
    expect(listening).to.equal(true);
  });

  it('net.getPeerCount returns a non-negative count', async () => {
    const peers = await web3.eth.net.getPeerCount();
    expect(peers >= 0n).to.equal(true);
  });

  it('getNodeInfo (web3_clientVersion) returns a non-empty string', async () => {
    const info = await web3.eth.getNodeInfo();
    expect(info).to.be.a('string').and.length.greaterThan(0);
  });

  it('isSyncing (eth_syncing) returns a boolean or a syncing object', async () => {
    const syncing = await web3.eth.isSyncing();
    expect(['boolean', 'object']).to.include(typeof syncing);
  });
});

describe('Misc eth_* methods supported on Thor', () => {
  let web3: Web3;
  before(() => {
    web3 = makeWeb3();
  });

  it('eth_coinbase returns an address (zero on this PoA network)', async () => {
    const cb = (await rpc(web3, 'eth_coinbase')) as string;
    expect(cb).to.match(/^0x[0-9a-fA-F]{40}$/);
  });

  it('eth_mining returns a boolean', async () => {
    const mining = await rpc(web3, 'eth_mining');
    expect(mining).to.be.a('boolean');
  });

  it('eth_hashrate returns a hex quantity', async () => {
    const hr = (await rpc(web3, 'eth_hashrate')) as string;
    expect(hr).to.match(/^0x[0-9a-fA-F]+$/);
  });

  it('eth_getUncleCountByBlockNumber returns 0x0 (Thor has no uncles)', async () => {
    const count = (await rpc(web3, 'eth_getUncleCountByBlockNumber', ['latest'])) as string;
    expect(count).to.match(/^0x0+$/);
  });

  it('eth_getUncleCountByBlockHash returns 0x0 (Thor has no uncles)', async () => {
    const latest = await web3.eth.getBlock('latest');
    const count = (await rpc(web3, 'eth_getUncleCountByBlockHash', [latest.hash])) as string;
    expect(count).to.match(/^0x0+$/);
  });

  it('eth_getUncleByBlockNumberAndIndex returns null', async () => {
    const uncle = await rpc(web3, 'eth_getUncleByBlockNumberAndIndex', ['latest', '0x0']);
    expect(uncle).to.equal(null);
  });

  it('eth_getUncleByBlockHashAndIndex returns null', async () => {
    const latest = await web3.eth.getBlock('latest');
    const uncle = await rpc(web3, 'eth_getUncleByBlockHashAndIndex', [latest.hash, '0x0']);
    expect(uncle).to.equal(null);
  });

  it('eth_newBlockFilter returns a filter id', async () => {
    const id = (await rpc(web3, 'eth_newBlockFilter')) as string;
    expect(id).to.match(/^0x[0-9a-fA-F]+$/);
    await rpc(web3, 'eth_uninstallFilter', [id]);
  });

  it('eth_newPendingTransactionFilter returns a filter id', async () => {
    const id = (await rpc(web3, 'eth_newPendingTransactionFilter')) as string;
    expect(id).to.match(/^0x[0-9a-fA-F]+$/);
    await rpc(web3, 'eth_uninstallFilter', [id]);
  });
});

describe('Block & transaction index methods (implemented on Thor)', () => {
  let web3: Web3;
  let txHash: string;
  let blockNumber: bigint;
  let blockHash: string;
  let txIndex: bigint;

  before(async () => {
    web3 = makeWeb3();
    const receipt = await sendEip1559(web3, TEST_SENDER_KEY, {
      to: NODE2_ADDRESS,
      value: 1n,
      gas: 21_000n,
    });
    txHash = receipt.transactionHash as string;
    blockNumber = receipt.blockNumber as bigint;
    blockHash = receipt.blockHash as string;
    txIndex = receipt.transactionIndex as bigint;
  });

  it('eth_getBlockTransactionCountByNumber matches the block tx array length', async () => {
    const block = await web3.eth.getBlock(blockNumber, false);
    const count = (await rpc(web3, 'eth_getBlockTransactionCountByNumber', [
      '0x' + blockNumber.toString(16),
    ])) as string;
    expect(count).to.match(/^0x[0-9a-fA-F]+$/);
    expect(BigInt(count)).to.equal(BigInt(block.transactions.length));
  });

  it('eth_getBlockTransactionCountByHash matches the block tx array length', async () => {
    const block = await web3.eth.getBlock(blockNumber, false);
    const count = (await rpc(web3, 'eth_getBlockTransactionCountByHash', [blockHash])) as string;
    expect(count).to.match(/^0x[0-9a-fA-F]+$/);
    expect(BigInt(count)).to.equal(BigInt(block.transactions.length));
  });

  it('eth_getTransactionByBlockNumberAndIndex returns the sent tx at its index', async () => {
    const t = (await rpc(web3, 'eth_getTransactionByBlockNumberAndIndex', [
      '0x' + blockNumber.toString(16),
      '0x' + txIndex.toString(16),
    ])) as { hash: string; blockHash: string } | null;
    expect(t, 'tx by (number,index)').to.not.be.null;
    expect(t!.hash.toLowerCase()).to.equal(txHash.toLowerCase());
    expect(t!.blockHash.toLowerCase()).to.equal(blockHash.toLowerCase());
  });

  it('eth_getTransactionByBlockHashAndIndex returns the sent tx at its index', async () => {
    const t = (await rpc(web3, 'eth_getTransactionByBlockHashAndIndex', [
      blockHash,
      '0x' + txIndex.toString(16),
    ])) as { hash: string } | null;
    expect(t, 'tx by (hash,index)').to.not.be.null;
    expect(t!.hash.toLowerCase()).to.equal(txHash.toLowerCase());
  });

  it('eth_getTransactionByBlockNumberAndIndex returns null for an out-of-range index', async () => {
    const t = await rpc(web3, 'eth_getTransactionByBlockNumberAndIndex', [
      '0x' + blockNumber.toString(16),
      '0xffff',
    ]);
    expect(t).to.equal(null);
  });
});

describe('eth_* methods NOT implemented by Thor (skipped until shipped)', () => {
  let web3: Web3;
  before(() => {
    web3 = makeWeb3();
  });

  const notFound = (err: unknown): boolean =>
    /not found|not supported|unsupported|does not exist|not available/i.test(
      collectStrings(err).join(' | '),
    );

  // Standard Ethereum methods thor's pedro/eth_eq_json_rpc dispatcher does NOT
  // register. Each attempts the call and skips while it 404s at the method
  // level; if Thor ever registers one, the success path keeps it honest, and a
  // non-"not found" error fails loudly.
  const unimplemented: Array<{ name: string; params: unknown[] }> = [
    { name: 'eth_getProof', params: [TEST_SENDER_ADDRESS, [], 'latest'] },
    { name: 'eth_createAccessList', params: [{ from: TEST_SENDER_ADDRESS, to: NODE2_ADDRESS }, 'latest'] },
    { name: 'eth_protocolVersion', params: [] },
    { name: 'eth_pendingTransactions', params: [] },
    { name: 'eth_sign', params: [TEST_SENDER_ADDRESS, '0x68656c6c6f'] },
    { name: 'eth_signTransaction', params: [{ from: TEST_SENDER_ADDRESS, to: NODE2_ADDRESS, value: '0x1' }] },
    { name: 'eth_getRawTransactionByHash', params: ['0x' + '00'.repeat(32)] },
    { name: 'debug_traceTransaction', params: ['0x' + '00'.repeat(32)] },
  ];

  for (const c of unimplemented) {
    it(`${c.name} — skipped while unimplemented`, async function () {
      let result: unknown;
      let caught: unknown;
      try {
        result = await rpc(web3, c.name, c.params);
      } catch (err) {
        caught = err;
      }
      if (caught !== undefined && notFound(caught)) {
        this.skip();
      }
      expect(
        caught,
        `${c.name} errored for a non-"not found" reason: ${collectStrings(caught).join(' | ')}`,
      ).to.be.undefined;
      expect(result, `${c.name} unexpectedly returned undefined without an error`).to.not.be.undefined;
    });
  }
});

describe('Category-3 divergences from Ethereum (skipped until Thor aligns)', () => {
  let web3: Web3;
  before(() => {
    web3 = makeWeb3();
  });

  // geth's eth_feeHistory returns a per-block × per-percentile `reward` matrix
  // when called with rewardPercentiles. Thor (rpc/fees/handler.go) currently
  // rejects the percentile form — "reward percentiles are not yet supported" —
  // so a fee estimator that requests percentiles can't use it. We SKIP on that
  // documented gap; if Thor ever ships it, the call succeeds and the
  // reward-matrix assertion keeps it honest. The sibling "rejected by Thor" test
  // in provider.test.ts covers the current behavior.
  it('eth_feeHistory with rewardPercentiles returns a reward matrix (geth parity)', async function () {
    let raw: { reward?: string[][] };
    try {
      raw = (await rpc(web3, 'eth_feeHistory', ['0x4', 'latest', [25, 50, 75]])) as {
        reward?: string[][];
      };
    } catch (err) {
      if (/percentile|not yet supported/i.test(collectStrings(err).join(' | '))) {
        this.skip();
      }
      throw err;
    }
    expect(raw, 'feeHistory result').to.be.an('object');
    expect(raw.reward, 'reward matrix').to.be.an('array').and.length.greaterThan(0);
    for (const row of raw.reward ?? []) {
      expect(row, 'per-block reward row').to.be.an('array').and.length(3);
    }
  });
});
