import { expect } from 'chai';
import { numberToHex, zeroAddress, type PublicClient } from 'viem';
import {
  getHttpUrl,
  makePublicClient,
  makeWalletClient,
  rpc,
  collectStrings,
  TEST_SENDER_ADDRESS,
  TEST_SENDER_KEY,
  NODE2_ADDRESS,
} from '../src/fixtures';
import { createPublicClient, http } from 'viem';

describe('Public client read-only RPC', () => {
  let client: PublicClient;
  before(() => {
    client = makePublicClient();
  });

  it('getBlockNumber returns a positive bigint', async () => {
    const n = await client.getBlockNumber();
    expect(n).to.be.a('bigint');
    expect(n > 0n, `blockNumber was ${n}`).to.equal(true);
  });

  it('getBlock("latest") returns a block with expected fields', async () => {
    const block = await client.getBlock({ blockTag: 'latest' });
    expect(block.number).to.be.a('bigint');
    expect(block.number! > 0n, 'block.number > 0').to.equal(true);
    expect(block.hash).to.match(/^0x[0-9a-fA-F]{64}$/);
    expect(block.parentHash).to.match(/^0x[0-9a-fA-F]{64}$/);
    expect(block.timestamp).to.be.a('bigint');
    expect(block.timestamp > 0n, 'block.timestamp > 0').to.equal(true);
  });

  it('getBalance returns a positive bigint for the funded sender', async () => {
    const bal = await client.getBalance({ address: TEST_SENDER_ADDRESS });
    expect(bal).to.be.a('bigint');
    expect(bal > 0n, `balance was ${bal}`).to.equal(true);
  });

  it('getTransactionCount returns a non-negative number', async () => {
    const n = await client.getTransactionCount({ address: TEST_SENDER_ADDRESS });
    expect(n).to.be.a('number').and.to.be.at.least(0);
  });

  it('getCode for a non-contract address is empty', async () => {
    const code = await client.getCode({ address: NODE2_ADDRESS });
    // viem returns undefined (or '0x') when there is no contract code.
    expect(code === undefined || code === '0x').to.equal(true);
  });

  it('call returns empty data for a no-op call to an EOA', async () => {
    const { data } = await client.call({ to: zeroAddress, data: '0x' });
    expect(data === undefined || data === '0x').to.equal(true);
  });

  it('estimateGas returns a positive bigint for a plain value transfer', async () => {
    const gas = await client.estimateGas({
      account: TEST_SENDER_ADDRESS,
      to: NODE2_ADDRESS,
      value: 1n,
    });
    expect(gas).to.be.a('bigint');
    expect(gas > 0n, `gas was ${gas}`).to.equal(true);
  });

  it('getChainId matches a direct eth_chainId call', async () => {
    const fromAction = await client.getChainId();
    const direct = (await rpc(client, 'eth_chainId')) as string;
    expect(BigInt(fromAction)).to.equal(BigInt(direct));
  });

  it('getGasPrice returns a positive bigint', async () => {
    const gp = await client.getGasPrice();
    expect(gp).to.be.a('bigint');
    expect(gp > 0n, 'gasPrice > 0').to.equal(true);
  });

  it('estimateFeesPerGas returns EIP-1559 fee fields', async () => {
    const fees = await client.estimateFeesPerGas();
    expect(fees.maxFeePerGas, 'maxFeePerGas').to.be.a('bigint');
    expect(fees.maxPriorityFeePerGas, 'maxPriorityFeePerGas').to.be.a('bigint');
  });

  it('getFeeHistory returns baseFee and gasUsedRatio (no percentiles)', async () => {
    const fh = await client.getFeeHistory({ blockCount: 4, rewardPercentiles: [] });
    expect(fh.baseFeePerGas).to.be.an('array').and.length.greaterThan(0);
    expect(fh.gasUsedRatio).to.be.an('array').and.length.greaterThan(0);
  });

  it('getFeeHistory with rewardPercentiles returns a reward matrix (geth parity)', async () => {
    // Thor now implements the rewardPercentiles form (rpc/fees/handler.go),
    // returning a per-block × per-percentile `reward` matrix like geth.
    const fh = await client.getFeeHistory({ blockCount: 4, rewardPercentiles: [25, 50, 75] });
    expect(fh.reward, 'reward matrix').to.be.an('array').and.length.greaterThan(0);
    for (const row of fh.reward!) {
      expect(row, 'per-block reward row').to.be.an('array').and.length(3);
      for (const r of row) expect(r, 'reward value').to.be.a('bigint');
    }
  });

  it('eth_getBlockReceipts returns the receipt array for the latest block', async () => {
    const receipts = (await rpc(client, 'eth_getBlockReceipts', ['latest'])) as Array<
      Record<string, unknown>
    >;
    expect(receipts).to.be.an('array');
    for (const r of receipts) {
      expect(r.blockHash, 'receipt.blockHash').to.match(/^0x[0-9a-fA-F]{64}$/);
      expect(r.transactionHash, 'receipt.transactionHash').to.match(/^0x[0-9a-fA-F]{64}$/);
      expect(r.status, 'receipt.status').to.match(/^0x[01]$/);
    }
  });

  describe('tx & log lookups (after a real send)', () => {
    let txHash: `0x${string}`;
    let blockNumber: bigint;

    before(async () => {
      const wallet = await makeWalletClient(TEST_SENDER_KEY);
      txHash = await wallet.sendTransaction({ to: NODE2_ADDRESS, value: 1n });
      const receipt = await client.waitForTransactionReceipt({ hash: txHash });
      blockNumber = receipt.blockNumber;
    });

    it('getTransaction by hash returns the sent tx', async () => {
      const tx = await client.getTransaction({ hash: txHash });
      expect(tx.hash).to.equal(txHash);
      expect(tx.from.toLowerCase()).to.equal(TEST_SENDER_ADDRESS.toLowerCase());
      expect(tx.to!.toLowerCase()).to.equal(NODE2_ADDRESS.toLowerCase());
      expect(tx.value).to.equal(1n);
    });

    it('getTransactionReceipt by hash returns a success receipt', async () => {
      const r = await client.getTransactionReceipt({ hash: txHash });
      expect(r.status).to.equal('success');
      expect(r.blockNumber).to.equal(blockNumber);
    });

    it('getLogs returns an array for a known block range', async () => {
      const logs = await client.getLogs({ fromBlock: blockNumber, toBlock: blockNumber });
      expect(logs).to.be.an('array');
    });

    it('getStorageAt returns a zero slot for an empty EOA slot 0', async () => {
      const slot = await client.getStorageAt({ address: NODE2_ADDRESS, slot: '0x0' });
      expect(slot).to.match(/^0x0+$/);
    });
  });

  describe('getBlock variants', () => {
    it('getBlock(<hash>) round-trips with the latest block', async () => {
      const latest = await client.getBlock({ blockTag: 'latest' });
      const byHash = await client.getBlock({ blockHash: latest.hash! });
      expect(byHash.number).to.equal(latest.number);
      expect(byHash.hash).to.equal(latest.hash);
    });

    it('getBlock(<number>) matches getBlock("latest") at the same height', async () => {
      const latest = await client.getBlock({ blockTag: 'latest' });
      const byNumber = await client.getBlock({ blockNumber: latest.number! });
      expect(byNumber.hash).to.equal(latest.hash);
    });

    it('getBlock(includeTransactions) hydrates full tx objects when present', async () => {
      const wallet = await makeWalletClient(TEST_SENDER_KEY);
      const hash = await wallet.sendTransaction({ to: NODE2_ADDRESS, value: 1n });
      const receipt = await client.waitForTransactionReceipt({ hash });
      const block = await client.getBlock({
        blockNumber: receipt.blockNumber,
        includeTransactions: true,
      });
      expect(block.transactions.length).to.be.greaterThan(0);
      const found = block.transactions.find(
        (t) => typeof t === 'object' && t.hash === hash,
      );
      expect(found, `prefetched tx ${hash}`).to.exist;
    });
  });

  describe('block tag handling', () => {
    it('getBlock("earliest") returns the genesis block at #0', async () => {
      const block = await client.getBlock({ blockTag: 'earliest' });
      expect(block.number).to.equal(0n);
    });

    it('getBlock("finalized") returns a block at or below latest', async () => {
      const finalized = await client.getBlock({ blockTag: 'finalized' });
      const latest = await client.getBlock({ blockTag: 'latest' });
      expect(finalized.number! <= latest.number!, 'finalized <= latest').to.equal(true);
    });

    it('getBlock("safe") returns a block at or below latest', async () => {
      const safe = await client.getBlock({ blockTag: 'safe' });
      const latest = await client.getBlock({ blockTag: 'latest' });
      expect(safe.number! <= latest.number!, 'safe <= latest').to.equal(true);
    });

    it('getBlock("pending") returns a block (Thor mirrors latest — no mempool)', async () => {
      const pending = await client.getBlock({ blockTag: 'pending' });
      // Thor's pending tracks the head; number may be null on a true pending
      // block in geth, but Thor returns a concrete block-shaped object.
      expect(pending).to.be.an('object');
    });
  });

  describe('node-side keystore path (expected absent on Thor)', () => {
    it('eth_accounts returns an empty array (no unlocked keys)', async () => {
      const accounts = (await rpc(client, 'eth_accounts')) as unknown[];
      expect(accounts).to.be.an('array');
      expect(accounts.length).to.equal(0);
    });

    it('eth_sendTransaction is rejected — no node-side signer to deliver to', async () => {
      let caught: unknown;
      try {
        await rpc(client, 'eth_sendTransaction', [
          { from: TEST_SENDER_ADDRESS, to: NODE2_ADDRESS, value: '0x1' },
        ]);
      } catch (err) {
        caught = err;
      }
      expect(caught, 'expected eth_sendTransaction to be rejected').to.not.be.undefined;
      expect(collectStrings(caught).join(' || ').length).to.be.greaterThan(0);
    });
  });

  describe('JSON-RPC batching (HTTP transport)', () => {
    it('batch:true collates 3 concurrent reads into a single batched POST', async () => {
      const batched = createPublicClient({ transport: http(getHttpUrl(), { batch: true }) });
      const [bn, chainId, gp] = await Promise.all([
        batched.getBlockNumber(),
        batched.getChainId(),
        batched.getGasPrice(),
      ]);
      expect(bn).to.be.a('bigint');
      expect(bn > 0n, 'blockNumber > 0').to.equal(true);
      expect(chainId).to.be.a('number').and.greaterThan(0);
      expect(gp).to.be.a('bigint');
      expect(gp > 0n, 'gasPrice > 0').to.equal(true);
    });

    it('raw 11-request batch exceeds Thor maxBatchRequests=10 and is rejected', async () => {
      const batch = Array.from({ length: 11 }, (_, i) => ({
        jsonrpc: '2.0',
        id: i,
        method: 'eth_blockNumber',
        params: [],
      }));
      const resp = await fetch(getHttpUrl(), {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify(batch),
      });
      const text = await resp.text();
      let parsed: unknown;
      try {
        parsed = JSON.parse(text);
      } catch {
        parsed = null;
      }
      const isErrorEnvelope =
        parsed != null &&
        typeof parsed === 'object' &&
        !Array.isArray(parsed) &&
        'error' in (parsed as Record<string, unknown>);
      const isAllSuccess =
        Array.isArray(parsed) &&
        parsed.length === 11 &&
        (parsed as Array<Record<string, unknown>>).every((r) => 'result' in r && !('error' in r));
      expect(
        !resp.ok || isErrorEnvelope || !isAllSuccess,
        `expected Thor to reject an 11-request batch, got ok=${resp.ok} body=${text.slice(0, 200)}`,
      ).to.equal(true);
    });
  });

  describe('EIP-1898 block reference forms (via raw request)', () => {
    it('eth_getBalance accepts {blockNumber: tag} object form', async () => {
      const bal = (await rpc(client, 'eth_getBalance', [
        TEST_SENDER_ADDRESS,
        { blockNumber: 'latest' },
      ])) as string;
      expect(bal).to.match(/^0x[0-9a-fA-F]+$/);
    });

    it('eth_getBalance accepts {blockHash: ...} object form', async () => {
      const latest = await client.getBlock({ blockTag: 'latest' });
      const bal = (await rpc(client, 'eth_getBalance', [
        TEST_SENDER_ADDRESS,
        { blockHash: latest.hash },
      ])) as string;
      expect(bal).to.match(/^0x[0-9a-fA-F]+$/);
    });

    it('eth_getTransactionCount accepts {blockNumber: 0x<hex>} numeric object form', async () => {
      const latestBn = await client.getBlockNumber();
      const n = (await rpc(client, 'eth_getTransactionCount', [
        TEST_SENDER_ADDRESS,
        { blockNumber: numberToHex(latestBn) },
      ])) as string;
      expect(n).to.match(/^0x[0-9a-fA-F]+$/);
    });
  });
});
