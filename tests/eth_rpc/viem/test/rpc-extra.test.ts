import { expect } from 'chai';
import { numberToHex, type PublicClient } from 'viem';
import {
  makePublicClient,
  makeWalletClient,
  rpc,
  collectStrings,
  TEST_SENDER_ADDRESS,
  TEST_SENDER_KEY,
  NODE2_ADDRESS,
} from '../src/fixtures';

describe('Network & node info (supported on Thor)', () => {
  let client: PublicClient;
  before(() => {
    client = makePublicClient();
  });

  it('net_version equals the decimal chainId', async () => {
    const netV = (await rpc(client, 'net_version')) as string;
    const chainId = await client.getChainId();
    expect(BigInt(netV)).to.equal(BigInt(chainId));
  });

  it('net_listening returns true', async () => {
    expect(await rpc(client, 'net_listening')).to.equal(true);
  });

  it('net_peerCount returns a hex quantity', async () => {
    expect(await rpc(client, 'net_peerCount')).to.match(/^0x[0-9a-fA-F]+$/);
  });

  it('web3_clientVersion returns a Thor/* string', async () => {
    const v = (await rpc(client, 'web3_clientVersion')) as string;
    expect(v).to.be.a('string').and.match(/thor/i);
  });

  it('eth_syncing returns false or a syncing object', async () => {
    const s = await rpc(client, 'eth_syncing');
    expect(s === false || (typeof s === 'object' && s !== null)).to.equal(true);
  });
});

describe('Misc eth_* methods supported on Thor', () => {
  let client: PublicClient;
  before(() => {
    client = makePublicClient();
  });

  it('eth_coinbase returns the zero address (PoA)', async () => {
    expect(await rpc(client, 'eth_coinbase')).to.match(/^0x0{40}$/);
  });

  it('eth_mining returns false (PoA)', async () => {
    expect(await rpc(client, 'eth_mining')).to.equal(false);
  });

  it('eth_hashrate returns 0x0 (PoA)', async () => {
    expect(await rpc(client, 'eth_hashrate')).to.match(/^0x0+$/);
  });

  it('eth_accounts returns an empty array (no node-side keystore)', async () => {
    const accounts = (await rpc(client, 'eth_accounts')) as unknown[];
    expect(accounts).to.be.an('array').and.length(0);
  });

  it('eth_getUncleCountByBlockNumber returns 0x0', async () => {
    expect(await rpc(client, 'eth_getUncleCountByBlockNumber', ['latest'])).to.match(/^0x0+$/);
  });

  it('eth_getUncleCountByBlockHash returns 0x0', async () => {
    const latest = await client.getBlock({ blockTag: 'latest' });
    expect(await rpc(client, 'eth_getUncleCountByBlockHash', [latest.hash])).to.match(/^0x0+$/);
  });

  it('eth_getUncleByBlockNumberAndIndex returns null', async () => {
    expect(await rpc(client, 'eth_getUncleByBlockNumberAndIndex', ['latest', '0x0'])).to.equal(null);
  });

  it('eth_getUncleByBlockHashAndIndex returns null', async () => {
    const latest = await client.getBlock({ blockTag: 'latest' });
    expect(await rpc(client, 'eth_getUncleByBlockHashAndIndex', [latest.hash, '0x0'])).to.equal(null);
  });

  it('eth_newBlockFilter returns a filter id and uninstalls', async () => {
    const id = (await rpc(client, 'eth_newBlockFilter')) as string;
    expect(id).to.match(/^0x[0-9a-fA-F]+$/);
    expect(await rpc(client, 'eth_uninstallFilter', [id])).to.equal(true);
  });

  it('eth_newPendingTransactionFilter returns a filter id and uninstalls', async () => {
    const id = (await rpc(client, 'eth_newPendingTransactionFilter')) as string;
    expect(id).to.match(/^0x[0-9a-fA-F]+$/);
    expect(await rpc(client, 'eth_uninstallFilter', [id])).to.equal(true);
  });
});

describe('Block & transaction index methods (implemented on Thor)', () => {
  let client: PublicClient;
  let txHash: `0x${string}`;
  let blockNumber: bigint;
  let blockHash: string;
  let txIndex: number;

  before(async () => {
    client = makePublicClient();
    const wallet = await makeWalletClient(TEST_SENDER_KEY);
    txHash = await wallet.sendTransaction({ to: NODE2_ADDRESS, value: 1n });
    const receipt = await client.waitForTransactionReceipt({ hash: txHash });
    blockNumber = receipt.blockNumber;
    blockHash = receipt.blockHash;
    txIndex = receipt.transactionIndex;
  });

  it('eth_getBlockTransactionCountByNumber matches the block tx array length', async () => {
    const block = await client.getBlock({ blockNumber });
    const count = (await rpc(client, 'eth_getBlockTransactionCountByNumber', [
      numberToHex(blockNumber),
    ])) as string;
    expect(BigInt(count)).to.equal(BigInt(block.transactions.length));
  });

  it('eth_getBlockTransactionCountByHash matches the block tx array length', async () => {
    const block = await client.getBlock({ blockNumber });
    const count = (await rpc(client, 'eth_getBlockTransactionCountByHash', [blockHash])) as string;
    expect(BigInt(count)).to.equal(BigInt(block.transactions.length));
  });

  it('eth_getTransactionByBlockNumberAndIndex returns the sent tx at its index', async () => {
    const t = (await rpc(client, 'eth_getTransactionByBlockNumberAndIndex', [
      numberToHex(blockNumber),
      numberToHex(txIndex),
    ])) as { hash: string; blockHash: string } | null;
    expect(t, 'tx by (number,index)').to.not.be.null;
    expect(t!.hash.toLowerCase()).to.equal(txHash.toLowerCase());
    expect(t!.blockHash.toLowerCase()).to.equal(blockHash.toLowerCase());
  });

  it('eth_getTransactionByBlockHashAndIndex returns the sent tx at its index', async () => {
    const t = (await rpc(client, 'eth_getTransactionByBlockHashAndIndex', [
      blockHash,
      numberToHex(txIndex),
    ])) as { hash: string } | null;
    expect(t, 'tx by (hash,index)').to.not.be.null;
    expect(t!.hash.toLowerCase()).to.equal(txHash.toLowerCase());
  });

  it('eth_getTransactionByBlockNumberAndIndex returns null for an out-of-range index', async () => {
    const t = await rpc(client, 'eth_getTransactionByBlockNumberAndIndex', [
      numberToHex(blockNumber),
      '0xffff',
    ]);
    expect(t).to.equal(null);
  });
});

describe('eth_* methods NOT implemented by Thor (skipped until shipped)', () => {
  let client: PublicClient;
  before(() => {
    client = makePublicClient();
  });

  const notFound = (err: unknown): boolean =>
    /not found|not supported|unsupported|does not exist|not available/i.test(
      collectStrings(err).join(' | '),
    );

  const unimplemented: Array<{ name: string; params: unknown[] }> = [
    { name: 'eth_getProof', params: [TEST_SENDER_ADDRESS, [], 'latest'] },
    { name: 'eth_createAccessList', params: [{ from: TEST_SENDER_ADDRESS, to: NODE2_ADDRESS }, 'latest'] },
    { name: 'eth_protocolVersion', params: [] },
    { name: 'eth_pendingTransactions', params: [] },
    { name: 'eth_sign', params: [TEST_SENDER_ADDRESS, '0x68656c6c6f'] },
    { name: 'eth_signTransaction', params: [{ from: TEST_SENDER_ADDRESS, to: NODE2_ADDRESS, value: '0x1' }] },
    { name: 'eth_getRawTransactionByHash', params: ['0x' + '00'.repeat(32)] },
    { name: 'debug_traceTransaction', params: ['0x' + '00'.repeat(32)] },
    { name: 'eth_blobBaseFee', params: [] },
  ];

  for (const c of unimplemented) {
    it(`${c.name} — skipped while unimplemented`, async function () {
      let result: unknown;
      let caught: unknown;
      try {
        result = await rpc(client, c.name, c.params);
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

  it('getEnsAddress — skipped (Thor chain has no ENS registry)', async function () {
    try {
      const addr = await client.getEnsAddress({ name: 'vitalik.eth' });
      expect(addr === null || /^0x[0-9a-fA-F]{40}$/.test(addr)).to.equal(true);
    } catch (err) {
      // viem refuses ENS on a chain with no resolver, throwing a plain Error
      // ("client chain not configured. universalResolverAddress is required.").
      // Its message is non-enumerable, so fold it in alongside collectStrings.
      const haystack = [collectStrings(err).join(' | '), String((err as Error)?.message ?? '')].join(
        ' | ',
      );
      if (/ens|universalresolver|chain not configured|does not support|unsupported/i.test(haystack)) {
        this.skip();
      }
      throw err;
    }
  });
});

describe('Category-3 divergences from Ethereum (skipped until Thor aligns)', () => {
  let client: PublicClient;
  before(() => {
    client = makePublicClient();
  });

  // geth's eth_feeHistory returns a per-block × per-percentile reward matrix when
  // called with rewardPercentiles. Thor (rpc/fees/handler.go) currently rejects
  // the percentile form — "reward percentiles are not yet supported" — so a fee
  // estimator that requests percentiles can't use it. We SKIP on that documented
  // gap; if Thor ever ships it, the reward-matrix assertion keeps it honest.
  it('getFeeHistory with rewardPercentiles returns a reward matrix (geth parity)', async function () {
    let fh: { reward?: bigint[][] };
    try {
      fh = await client.getFeeHistory({
        blockCount: 4,
        rewardPercentiles: [25, 50, 75],
      });
    } catch (err) {
      if (/percentile|not yet supported/i.test(collectStrings(err).join(' | '))) {
        this.skip();
      }
      throw err;
    }
    expect(fh.reward, 'reward matrix').to.be.an('array').and.length.greaterThan(0);
    for (const row of fh.reward!) {
      expect(row, 'per-block reward row').to.be.an('array').and.length(3);
    }
  });
});
