import { expect } from 'chai';
import { Web3 } from 'web3';
import {
  collectStrings,
  getHttpUrl,
  makeWeb3,
  rpc,
  sendEip1559,
  NODE2_ADDRESS,
  TEST_SENDER_ADDRESS,
  TEST_SENDER_KEY,
} from '../src/fixtures';

const ZERO_ADDRESS = '0x0000000000000000000000000000000000000000';

describe('web3.eth read-only RPC', () => {
  let web3: Web3;
  before(() => {
    web3 = makeWeb3();
  });

  it('getBlockNumber returns a positive bigint', async () => {
    const n = await web3.eth.getBlockNumber();
    expect(n).to.be.a('bigint');
    expect(n > 0n, `blockNumber was ${n}`).to.equal(true);
  });

  it('getBlock("latest") returns a block with expected fields', async () => {
    const block = await web3.eth.getBlock('latest');
    expect(block, 'latest block').to.not.be.undefined;
    expect(block.number > 0n).to.equal(true);
    expect(block.hash).to.match(/^0x[0-9a-fA-F]{64}$/);
    expect(block.parentHash).to.match(/^0x[0-9a-fA-F]{64}$/);
    expect(block.timestamp > 0n).to.equal(true);
  });

  it('getBalance returns a positive bigint for the funded sender', async () => {
    const bal = await web3.eth.getBalance(TEST_SENDER_ADDRESS);
    expect(bal).to.be.a('bigint');
    expect(bal > 0n, `balance was ${bal}`).to.equal(true);
  });

  it('getTransactionCount returns a non-negative bigint', async () => {
    const n = await web3.eth.getTransactionCount(TEST_SENDER_ADDRESS);
    expect(n).to.be.a('bigint');
    expect(n >= 0n).to.equal(true);
  });

  it('getCode for a non-contract address returns 0x', async () => {
    const code = await web3.eth.getCode(NODE2_ADDRESS);
    expect(code).to.equal('0x');
  });

  it('call returns 0x for a no-op call to an EOA', async () => {
    const result = await web3.eth.call({ to: ZERO_ADDRESS, data: '0x' });
    expect(result).to.equal('0x');
  });

  it('estimateGas returns a positive bigint for a plain value transfer', async () => {
    const gas = await web3.eth.estimateGas({
      from: TEST_SENDER_ADDRESS,
      to: NODE2_ADDRESS,
      value: 1n,
    });
    expect(gas).to.be.a('bigint');
    expect(gas > 0n, `gas was ${gas}`).to.equal(true);
  });

  it('getChainId matches a direct eth_chainId call', async () => {
    const fromApi = await web3.eth.getChainId();
    const direct = (await rpc(web3, 'eth_chainId')) as string;
    expect(fromApi).to.equal(BigInt(direct));
  });

  it('getGasPrice returns a positive bigint', async () => {
    const gp = await web3.eth.getGasPrice();
    expect(gp).to.be.a('bigint');
    expect(gp > 0n, `gasPrice was ${gp}`).to.equal(true);
  });

  it('getMaxPriorityFeePerGas returns a bigint', async () => {
    const tip = await web3.eth.getMaxPriorityFeePerGas();
    expect(tip).to.be.a('bigint');
    expect(tip >= 0n).to.equal(true);
  });

  it('eth_feeHistory returns baseFee and gasUsedRatio (no percentiles)', async () => {
    // Thor accepts eth_feeHistory but rejects rewardPercentiles — call with [].
    const raw = (await rpc(web3, 'eth_feeHistory', ['0x4', 'latest', []])) as {
      oldestBlock: string;
      baseFeePerGas: string[];
      gasUsedRatio: number[];
    };
    expect(raw, 'eth_feeHistory result').to.be.an('object');
    expect(raw.oldestBlock).to.match(/^0x[0-9a-fA-F]+$/);
    expect(raw.baseFeePerGas).to.be.an('array').and.to.have.length.greaterThan(0);
    expect(raw.gasUsedRatio).to.be.an('array').and.to.have.length.greaterThan(0);
  });

  it('eth_feeHistory with rewardPercentiles is rejected by Thor', async () => {
    let caught: unknown;
    try {
      await rpc(web3, 'eth_feeHistory', ['0x4', 'latest', [25, 50, 75]]);
    } catch (err) {
      caught = err;
    }
    expect(caught, 'expected percentile request to be rejected').to.not.be.undefined;
    const blob = collectStrings(caught).join(' || ');
    expect(blob).to.match(/percentile|not yet supported|coalesce/i);
  });

  it('eth_getBlockReceipts returns the receipt array for the latest block', async () => {
    const receipts = (await rpc(web3, 'eth_getBlockReceipts', ['latest'])) as Array<
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
    let txHash: string;
    let blockNumber: bigint;

    before(async () => {
      const receipt = await sendEip1559(web3, TEST_SENDER_KEY, {
        to: NODE2_ADDRESS,
        value: 1n,
        gas: 21_000n,
      });
      txHash = receipt.transactionHash as string;
      blockNumber = receipt.blockNumber as bigint;
    });

    it('getTransaction by hash returns the sent tx', async () => {
      const t = await web3.eth.getTransaction(txHash);
      expect(t, 'tx lookup').to.not.be.undefined;
      expect((t.hash as string).toLowerCase()).to.equal(txHash.toLowerCase());
      expect((t.from as string).toLowerCase()).to.equal(TEST_SENDER_ADDRESS.toLowerCase());
      expect((t.to as string).toLowerCase()).to.equal(NODE2_ADDRESS.toLowerCase());
      expect(t.value).to.equal(1n);
    });

    it('getTransactionReceipt by hash returns a status-1 receipt', async () => {
      const r = await web3.eth.getTransactionReceipt(txHash);
      expect(r, 'receipt lookup').to.not.be.undefined;
      expect(r.status).to.equal(1n);
      expect(r.blockNumber).to.equal(blockNumber);
    });

    it('getLogs returns an array for a known block range', async () => {
      // Plain value transfer emits no logs, but the call must succeed and
      // return an array — non-empty cases are covered by the events suite.
      const logs = await web3.eth.getPastLogs({
        fromBlock: blockNumber,
        toBlock: blockNumber,
      });
      expect(logs).to.be.an('array');
    });

    it('getStorageAt returns 0x00..00 for an empty EOA slot 0', async () => {
      const slot = await web3.eth.getStorageAt(NODE2_ADDRESS, 0);
      expect(slot).to.match(/^0x0+$/);
    });
  });

  describe('getBlock variants', () => {
    it('getBlock(<hash>) round-trips with the latest block', async () => {
      const latest = await web3.eth.getBlock('latest');
      const byHash = await web3.eth.getBlock(latest.hash as string);
      expect(byHash.number).to.equal(latest.number);
      expect(byHash.hash).to.equal(latest.hash);
    });

    it('getBlock(<number>) matches getBlock("latest") at the same height', async () => {
      const latest = await web3.eth.getBlock('latest');
      const byNumber = await web3.eth.getBlock(latest.number);
      expect(byNumber.hash).to.equal(latest.hash);
    });

    it('getBlock(<number>, true) hydrates transaction objects when present', async () => {
      const receipt = await sendEip1559(web3, TEST_SENDER_KEY, {
        to: NODE2_ADDRESS,
        value: 1n,
        gas: 21_000n,
      });
      const block = await web3.eth.getBlock(receipt.blockNumber, true);
      expect(block.transactions, 'hydrated transactions').to.be.an('array').and.length.greaterThan(0);
      const found = (block.transactions as Array<Record<string, unknown>>).find(
        (tx) => (tx.hash as string).toLowerCase() === (receipt.transactionHash as string).toLowerCase(),
      );
      expect(found, `hydrated tx ${receipt.transactionHash}`).to.exist;
      expect((found!.from as string).toLowerCase()).to.equal(TEST_SENDER_ADDRESS.toLowerCase());
    });
  });

  describe('block tag handling', () => {
    it('getBlock("earliest") returns the genesis block at #0', async () => {
      const block = await web3.eth.getBlock('earliest');
      expect(block.number).to.equal(0n);
    });

    it('getBlock("finalized") returns a block at or below latest', async () => {
      const finalized = await web3.eth.getBlock('finalized');
      const latest = await web3.eth.getBlock('latest');
      expect(finalized.number <= latest.number).to.equal(true);
    });

    it('getBlock("safe") returns a block at or below latest', async () => {
      const safe = await web3.eth.getBlock('safe');
      const latest = await web3.eth.getBlock('latest');
      expect(safe.number <= latest.number).to.equal(true);
    });

    it('getBlock("pending") returns a block (Thor mirrors latest — no separate mempool)', async () => {
      const pending = await web3.eth.getBlock('pending');
      expect(pending, 'pending block').to.not.be.undefined;
      expect(pending.number >= 0n).to.equal(true);
    });
  });

  describe('node-side keystore path (expected absent on Thor)', () => {
    it('getAccounts / eth_accounts returns an empty array (no unlocked keys)', async () => {
      const accounts = await web3.eth.getAccounts();
      expect(accounts).to.be.an('array');
      expect(accounts.length).to.equal(0);
    });

    it('eth_sendTransaction is rejected — no node-side signer to deliver to', async () => {
      let caught: unknown;
      try {
        await rpc(web3, 'eth_sendTransaction', [
          { from: TEST_SENDER_ADDRESS, to: NODE2_ADDRESS, value: '0x1' },
        ]);
      } catch (err) {
        caught = err;
      }
      expect(caught, 'expected eth_sendTransaction to be rejected').to.not.be.undefined;
      expect(collectStrings(caught).join(' || ').length).to.be.greaterThan(0);
    });

    it('personal_sign is rejected — no node-side keys to sign with', async () => {
      let caught: unknown;
      try {
        await rpc(web3, 'personal_sign', ['0x68656c6c6f', TEST_SENDER_ADDRESS]);
      } catch (err) {
        caught = err;
      }
      expect(caught, 'expected personal_sign to be rejected').to.not.be.undefined;
    });
  });

  describe('EIP-4844 / blob fees (expected unsupported on Thor)', () => {
    it('eth_blobBaseFee is rejected — Thor has not implemented EIP-4844', async () => {
      let caught: unknown;
      try {
        await rpc(web3, 'eth_blobBaseFee', []);
      } catch (err) {
        caught = err;
      }
      expect(caught, 'expected eth_blobBaseFee to be rejected').to.not.be.undefined;
      expect(collectStrings(caught).join(' || ').length).to.be.greaterThan(0);
    });
  });

  describe('JSON-RPC batching (HTTP)', () => {
    it('web3.BatchRequest carries 3 concurrent reads in a single batched POST', async () => {
      const batch = new web3.BatchRequest();
      const pBlock = batch.add({ method: 'eth_blockNumber', params: [] });
      const pChain = batch.add({ method: 'eth_chainId', params: [] });
      const pGas = batch.add({ method: 'eth_gasPrice', params: [] });
      await batch.execute();
      const [bn, chainId, gp] = await Promise.all([pBlock, pChain, pGas]);
      expect(bn as string).to.match(/^0x[0-9a-fA-F]+$/);
      expect(chainId as string).to.match(/^0x[0-9a-fA-F]+$/);
      expect(gp as string).to.match(/^0x[0-9a-fA-F]+$/);
    });

    it('raw 11-request batch exceeds Thor maxBatchRequests=10 and the whole batch is rejected', async () => {
      // Thor's HTTP jsonrpc dispatcher caps batches at 10 requests. An 11-request
      // batch posted raw must NOT come back as 11 successful results.
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

      const okHttp = resp.ok;
      const isErrorEnvelope =
        parsed != null &&
        typeof parsed === 'object' &&
        !Array.isArray(parsed) &&
        'error' in (parsed as Record<string, unknown>);
      const isAllSuccess =
        Array.isArray(parsed) &&
        (parsed as unknown[]).length === 11 &&
        (parsed as Array<Record<string, unknown>>).every((r) => 'result' in r && !('error' in r));

      expect(
        !okHttp || isErrorEnvelope || !isAllSuccess,
        `expected Thor to reject an 11-request batch, got ok=${okHttp} body=${text.slice(0, 200)}`,
      ).to.equal(true);
    });
  });

  describe('EIP-1898 block reference forms', () => {
    it('eth_getBalance accepts {blockNumber: tag} object form', async () => {
      const bal = (await rpc(web3, 'eth_getBalance', [
        TEST_SENDER_ADDRESS,
        { blockNumber: 'latest' },
      ])) as string;
      expect(bal).to.match(/^0x[0-9a-fA-F]+$/);
    });

    it('eth_getBalance accepts {blockHash: ...} object form', async () => {
      const latest = await web3.eth.getBlock('latest');
      const bal = (await rpc(web3, 'eth_getBalance', [
        TEST_SENDER_ADDRESS,
        { blockHash: latest.hash },
      ])) as string;
      expect(bal).to.match(/^0x[0-9a-fA-F]+$/);
    });

    it('eth_call accepts {blockNumber: tag} object form', async () => {
      const result = (await rpc(web3, 'eth_call', [
        { to: ZERO_ADDRESS, data: '0x' },
        { blockNumber: 'latest' },
      ])) as string;
      expect(result).to.equal('0x');
    });

    it('eth_getTransactionCount accepts {blockNumber: 0x<hex>} numeric object form', async () => {
      const latestBn = await web3.eth.getBlockNumber();
      const n = (await rpc(web3, 'eth_getTransactionCount', [
        TEST_SENDER_ADDRESS,
        { blockNumber: '0x' + latestBn.toString(16) },
      ])) as string;
      expect(n).to.match(/^0x[0-9a-fA-F]+$/);
    });
  });
});
