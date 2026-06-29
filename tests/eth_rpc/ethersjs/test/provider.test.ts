import { expect } from 'chai';
import { JsonRpcProvider, Wallet, ZeroAddress, id, toBeHex, toQuantity } from 'ethers';
import {
  getHttpUrl,
  makeProvider,
  makeWallet,
  TEST_SENDER_ADDRESS,
  NODE2_ADDRESS,
  TEST_SENDER_KEY,
} from '../src/fixtures';

// Walk an unknown error / response and collect every string-valued leaf — used
// when an RPC error's diagnostic text may live at .message, .shortMessage,
// .info.error.message, etc.
function collectStrings(obj: unknown): string[] {
  const out: string[] = [];
  const walk = (o: unknown, depth: number) => {
    if (depth > 3 || o == null) return;
    if (typeof o === 'string') out.push(o);
    else if (typeof o === 'object')
      for (const v of Object.values(o as Record<string, unknown>)) walk(v, depth + 1);
  };
  walk(obj, 0);
  return out;
}

describe('Provider read-only RPC', () => {
  let provider: JsonRpcProvider;
  before(() => {
    provider = makeProvider();
  });

  it('getBlockNumber returns a positive integer', async () => {
    const n = await provider.getBlockNumber();
    expect(n).to.be.a('number').and.to.be.greaterThan(0);
  });

  it('getBlock("latest") returns a block with expected fields', async () => {
    const block = await provider.getBlock('latest');
    expect(block, 'latest block').to.not.be.null;
    expect(block!.number).to.be.greaterThan(0);
    expect(block!.hash).to.match(/^0x[0-9a-fA-F]{64}$/);
    expect(block!.parentHash).to.match(/^0x[0-9a-fA-F]{64}$/);
    expect(block!.timestamp).to.be.a('number').and.greaterThan(0);
  });

  it('getBalance returns a non-negative bigint for funded sender', async () => {
    const bal = await provider.getBalance(TEST_SENDER_ADDRESS);
    expect(bal).to.be.a('bigint');
    expect(bal > 0n, `balance was ${bal}`).to.equal(true);
  });

  it('getTransactionCount returns a non-negative number', async () => {
    const n = await provider.getTransactionCount(TEST_SENDER_ADDRESS);
    expect(n).to.be.a('number').and.to.be.at.least(0);
  });

  it('getCode for a non-contract address returns 0x', async () => {
    const code = await provider.getCode(NODE2_ADDRESS);
    expect(code).to.equal('0x');
  });

  it('call returns 0x for a no-op call to EOA', async () => {
    const result = await provider.call({ to: ZeroAddress, data: '0x' });
    expect(result).to.equal('0x');
  });

  it('estimateGas returns a positive bigint for a plain value transfer', async () => {
    const gas = await provider.estimateGas({
      from: TEST_SENDER_ADDRESS,
      to: NODE2_ADDRESS,
      value: 1n,
    });
    expect(gas).to.be.a('bigint');
    expect(gas > 0n, `gas was ${gas}`).to.equal(true);
  });

  it('chainId from getNetwork matches direct eth_chainId', async () => {
    const fromNet = (await provider.getNetwork()).chainId;
    const direct = await provider.send('eth_chainId', []);
    expect(toBeHex(fromNet)).to.equal(toBeHex(BigInt(direct)));
  });

  it('getFeeData returns gasPrice and/or EIP-1559 fee fields', async () => {
    const fee = await provider.getFeeData();
    // At minimum one of these must be populated; EIP-1559 chains return all three.
    const hasAny =
      fee.gasPrice !== null ||
      fee.maxFeePerGas !== null ||
      fee.maxPriorityFeePerGas !== null;
    expect(hasAny, JSON.stringify(fee, (_, v) => (typeof v === 'bigint' ? v.toString() : v)))
      .to.equal(true);
  });

  describe('tx & log lookups (after a real send)', () => {
    let wallet: Wallet;
    let txHash: string;
    let blockNumber: number;

    before(async () => {
      wallet = makeWallet(TEST_SENDER_KEY, provider);
      const tx = await wallet.sendTransaction({
        to: NODE2_ADDRESS,
        value: 1n,
        type: 2,
      });
      const r = await tx.wait();
      txHash = tx.hash;
      blockNumber = r!.blockNumber;
    });

    it('getTransaction by hash returns the sent tx', async () => {
      const t = await provider.getTransaction(txHash);
      expect(t, 'tx lookup').to.not.be.null;
      expect(t!.hash).to.equal(txHash);
      expect(t!.from.toLowerCase()).to.equal(TEST_SENDER_ADDRESS.toLowerCase());
      expect(t!.to!.toLowerCase()).to.equal(NODE2_ADDRESS.toLowerCase());
      expect(t!.value).to.equal(1n);
    });

    it('getTransactionReceipt by hash returns a status-1 receipt', async () => {
      const r = await provider.getTransactionReceipt(txHash);
      expect(r, 'receipt lookup').to.not.be.null;
      expect(r!.status).to.equal(1);
      expect(r!.blockNumber).to.equal(blockNumber);
    });

    it('waitForTransaction resolves with the receipt', async () => {
      const r = await provider.waitForTransaction(txHash, 1, 30_000);
      expect(r, 'waitForTransaction').to.not.be.null;
      expect(r!.hash).to.equal(txHash);
    });

    it('getLogs returns logs for a known block range', async () => {
      // Plain value transfer doesn't emit logs, but the call itself must succeed
      // and return an array — the Set events test exercises non-empty cases.
      const logs = await provider.getLogs({
        fromBlock: blockNumber,
        toBlock: blockNumber,
      });
      expect(logs).to.be.an('array');
    });

    it('getStorage returns 0x00..00 for an empty EOA slot 0', async () => {
      const slot = await provider.getStorage(NODE2_ADDRESS, 0);
      expect(slot).to.match(/^0x0+$/);
    });
  });

  it('eth_feeHistory returns baseFee and gasUsedRatio (no percentiles)', async () => {
    // Thor accepts eth_feeHistory but rejects rewardPercentiles with
    // "reward percentiles are not yet supported" — call without the third arg.
    // The non-percentile shape is the path ethers' getFeeData uses internally.
    const raw = await provider.send('eth_feeHistory', ['0x4', 'latest', []]);
    expect(raw, 'eth_feeHistory result').to.be.an('object');
    expect(raw.oldestBlock, 'oldestBlock').to.match(/^0x[0-9a-fA-F]+$/);
    expect(raw.baseFeePerGas).to.be.an('array').and.to.have.length.greaterThan(0);
    expect(raw.gasUsedRatio).to.be.an('array').and.to.have.length.greaterThan(0);
  });

  it('eth_feeHistory with rewardPercentiles is rejected by Thor', async () => {
    let caught: unknown;
    try {
      await provider.send('eth_feeHistory', ['0x4', 'latest', [25, 50, 75]]);
    } catch (err) {
      caught = err;
    }
    expect(caught, 'expected percentile request to be rejected').to.not.be.undefined;
    const haystack: string[] = [];
    const walk = (obj: unknown, depth: number) => {
      if (depth > 3 || obj == null) return;
      if (typeof obj === 'string') haystack.push(obj);
      else if (typeof obj === 'object')
        for (const v of Object.values(obj as Record<string, unknown>)) walk(v, depth + 1);
    };
    walk(caught, 0);
    expect(haystack.join(' || ')).to.match(/percentile|not yet supported|coalesce/i);
  });

  it('eth_getBlockReceipts returns the receipt array for the latest block', async () => {
    const receipts = await provider.send('eth_getBlockReceipts', ['latest']);
    expect(receipts).to.be.an('array');
    for (const r of receipts as Array<Record<string, unknown>>) {
      expect(r.blockHash, 'receipt.blockHash').to.match(/^0x[0-9a-fA-F]{64}$/);
      expect(r.transactionHash, 'receipt.transactionHash').to.match(/^0x[0-9a-fA-F]{64}$/);
      expect(r.status, 'receipt.status').to.match(/^0x[01]$/);
    }
  });

  it('getLogs with topic filter returns event sig matches', async () => {
    // Topic for keccak256("Set(address,uint256)")
    const topic = id('Set(address,uint256)');
    const latest = await provider.getBlockNumber();
    const from = Math.max(0, latest - 100);
    const logs = await provider.getLogs({
      fromBlock: from,
      toBlock: 'latest',
      topics: [topic],
    });
    expect(logs).to.be.an('array');
    // We don't assert non-empty — this provider test may run before the contract
    // suite emits any Set events. The point is that the topic filter is accepted
    // and produces a well-formed response.
  });

  describe('getBlock variants', () => {
    it('getBlock(<hash>) round-trips with the latest block', async () => {
      const latest = await provider.getBlock('latest');
      expect(latest, 'latest block').to.not.be.null;
      const byHash = await provider.getBlock(latest!.hash!);
      expect(byHash, 'block by hash').to.not.be.null;
      expect(byHash!.number).to.equal(latest!.number);
      expect(byHash!.hash).to.equal(latest!.hash);
    });

    it('getBlock(<number>) matches getBlock("latest") at the same height', async () => {
      const latest = await provider.getBlock('latest');
      expect(latest, 'latest block').to.not.be.null;
      const byNumber = await provider.getBlock(latest!.number);
      expect(byNumber, 'block by number').to.not.be.null;
      expect(byNumber!.hash).to.equal(latest!.hash);
    });

    it('getBlock(<number>, true) prefetches transaction objects when present', async () => {
      // Send a tx to guarantee at least one in the resulting block, then re-fetch
      // that block with prefetchTxs=true. ethers v6 exposes the prefetched
      // objects via block.prefetchedTransactions / block.getPrefetchedTransaction.
      const wallet = makeWallet(TEST_SENDER_KEY, provider);
      const tx = await wallet.sendTransaction({
        to: NODE2_ADDRESS,
        value: 1n,
        type: 2,
      });
      const receipt = await tx.wait();
      const block = await provider.getBlock(receipt!.blockNumber, true);
      expect(block, 'block with txs').to.not.be.null;
      expect(block!.transactions.length).to.be.greaterThan(0);
      const prefetched = block!.prefetchedTransactions;
      expect(prefetched, 'prefetchedTransactions').to.be.an('array').and.length.greaterThan(0);
      const found = prefetched.find((p) => p.hash === tx.hash);
      expect(found, `prefetched tx ${tx.hash}`).to.exist;
      expect(found!.from.toLowerCase()).to.equal(TEST_SENDER_ADDRESS.toLowerCase());
    });
  });

  describe('block tag handling', () => {
    it('getBlock("earliest") returns the genesis block at #0', async () => {
      const block = await provider.getBlock('earliest');
      expect(block, 'earliest block').to.not.be.null;
      expect(block!.number).to.equal(0);
    });

    it('getBlock("finalized") returns a block at or below latest', async () => {
      const finalized = await provider.getBlock('finalized');
      const latest = await provider.getBlock('latest');
      expect(finalized, 'finalized block').to.not.be.null;
      expect(latest, 'latest block').to.not.be.null;
      expect(finalized!.number).to.be.at.most(latest!.number);
    });

    it('getBlock("safe") returns a block at or below latest', async () => {
      const safe = await provider.getBlock('safe');
      const latest = await provider.getBlock('latest');
      expect(safe, 'safe block').to.not.be.null;
      expect(latest, 'latest block').to.not.be.null;
      expect(safe!.number).to.be.at.most(latest!.number);
    });

    it('getBlock("pending") returns a block (Thor mirrors latest — no separate mempool)', async () => {
      // Thor's eth_eq_json_rpc accepts the 'pending' tag but has no public
      // mempool, so the response is a block-shaped object tracking the head
      // (typically equal to latest at fetch time, possibly +1 if a new block
      // was packed between the two reads). We only assert that the call
      // succeeds and returns a sane block — if Thor ever surfaces a real
      // mempool view, tighten this.
      const latest = await provider.getBlock('latest');
      const pending = await provider.getBlock('pending');
      expect(latest, 'latest block').to.not.be.null;
      expect(pending, 'pending block').to.not.be.null;
      expect(pending!.number).to.be.a('number').and.at.least(0);
    });
  });

  describe('JsonRpcSigner — node-side keystore path (expected absent on Thor)', () => {
    it('eth_accounts is reachable and returns an empty array (no unlocked keys)', async () => {
      // Thor's RPC implements eth_accounts but the node holds no signing keys —
      // every dApp that tries provider.getSigner() ends up here. Asserting [] keeps
      // the gap pinned down; if Thor ever exposes node-side keys this flips.
      const accounts = (await provider.send('eth_accounts', [])) as unknown;
      expect(accounts).to.be.an('array');
      expect((accounts as unknown[]).length).to.equal(0);
    });

    it('listAccounts() (high-level wrapper) returns an empty signer array', async () => {
      // JsonRpcApiProvider.listAccounts() maps eth_accounts into JsonRpcSigner[].
      // The lower-level send('eth_accounts') is asserted above; this pins the
      // high-level path dApps actually call.
      const accounts = await provider.listAccounts();
      expect(accounts).to.be.an('array');
      expect(accounts.length).to.equal(0);
    });

    it('getSigner() rejects — index 0 has no unlocked node account behind it', async () => {
      // getSigner() defaults to account index 0 and reads eth_accounts; with an
      // empty keystore ethers throws "no such account" before returning a
      // JsonRpcSigner. This is the high-level failure every getSigner()-based
      // dApp hits on Thor. Flip to a success path if Thor ever unlocks keys.
      let caught: unknown;
      try {
        await provider.getSigner();
      } catch (err) {
        caught = err;
      }
      expect(caught, 'expected getSigner() to reject with no node accounts').to.not.be.undefined;
      // ethers throws a plain Error("no such account"); its message is a
      // non-enumerable own property, so read it directly rather than via the
      // enumerable-leaf walker used for richer JSON-RPC error objects.
      const msg = String((caught as Error).message ?? '');
      expect(msg, 'error should explain the missing node account').to.match(/account/i);
    });

    it('eth_sendTransaction is rejected — no node-side signer to deliver to', async () => {
      let caught: unknown;
      try {
        await provider.send('eth_sendTransaction', [
          { from: TEST_SENDER_ADDRESS, to: NODE2_ADDRESS, value: '0x1' },
        ]);
      } catch (err) {
        caught = err;
      }
      expect(caught, 'expected eth_sendTransaction to be rejected').to.not.be.undefined;
      const blob = collectStrings(caught).join(' || ');
      expect(blob.length, 'error must carry a diagnostic message').to.be.greaterThan(0);
    });

    it('personal_sign is rejected — no node-side keys to sign with', async () => {
      let caught: unknown;
      try {
        await provider.send('personal_sign', ['0x68656c6c6f', TEST_SENDER_ADDRESS]);
      } catch (err) {
        caught = err;
      }
      expect(caught, 'expected personal_sign to be rejected').to.not.be.undefined;
    });
  });

  describe('EIP-4844 / blob fees (expected unsupported on Thor)', () => {
    it('eth_blobBaseFee is rejected — Thor has not implemented EIP-4844', async () => {
      // Standard go-ethereum returns a QUANTITY hex string here. Thor's
      // Ethereum-compat RPC does not register this method; the call must
      // surface as a JSON-RPC error. Flip to a success path when Thor
      // ships 4844 (and add a schema entry on the Go side).
      let caught: unknown;
      try {
        await provider.send('eth_blobBaseFee', []);
      } catch (err) {
        caught = err;
      }
      expect(caught, 'expected eth_blobBaseFee to be rejected').to.not.be.undefined;
      const blob = collectStrings(caught).join(' || ');
      expect(blob.length, 'error must carry a diagnostic message').to.be.greaterThan(0);
    });
  });

  describe('JsonRpcProvider batching (HTTP)', () => {
    it('batchMaxCount=5 carries 3 concurrent reads in a single batched POST', async () => {
      // ethers v6 collates concurrent send() calls into a JSON array up to
      // batchMaxCount, then flushes after batchStallTime ms. We can't trivially
      // assert the framing without intercepting fetch — but if Thor handles
      // the batch envelope, every promise resolves with its own result.
      const batched = new JsonRpcProvider(getHttpUrl(), undefined, {
        batchMaxCount: 5,
        batchStallTime: 10,
        staticNetwork: true,
      });
      try {
        const [bn, chainIdRaw, gp] = await Promise.all([
          batched.send('eth_blockNumber', []),
          batched.send('eth_chainId', []),
          batched.send('eth_gasPrice', []),
        ]);
        expect(bn).to.match(/^0x[0-9a-fA-F]+$/);
        expect(chainIdRaw).to.match(/^0x[0-9a-fA-F]+$/);
        expect(gp).to.match(/^0x[0-9a-fA-F]+$/);
      } finally {
        await batched.destroy();
      }
    });

    it('raw 11-request batch exceeds Thor maxBatchRequests=10 and the whole batch is rejected', async () => {
      // Thor's HTTP jsonrpc dispatcher caps batches at 10 requests
      // (thor/rpc/jsonrpc.maxBatchRequests). An 11-request batch posted raw
      // must either be rejected with an HTTP 4xx or come back as a JSON-RPC
      // error response, NOT as 11 successful results.
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

      // Three valid shapes Thor might use, all of which count as rejection:
      //   1. HTTP non-2xx (e.g. 400/413)
      //   2. A single error envelope: { error: { code, message } }
      //   3. An array shorter than 11 (truncated) — Thor refused some entries
      const okHttp = resp.ok;
      const isErrorEnvelope =
        parsed != null && typeof parsed === 'object' && !Array.isArray(parsed) &&
        'error' in (parsed as Record<string, unknown>);
      const isAllSuccess =
        Array.isArray(parsed) && (parsed as unknown[]).length === 11 &&
        (parsed as Array<Record<string, unknown>>).every((r) => 'result' in r && !('error' in r));

      expect(!okHttp || isErrorEnvelope || !isAllSuccess,
        `expected Thor to reject an 11-request batch, got ok=${okHttp} body=${text.slice(0, 200)}`)
        .to.equal(true);
    });
  });

  describe('EIP-1898 block reference forms', () => {
    it('eth_getBalance accepts {blockNumber: tag} object form', async () => {
      const bal = await provider.send('eth_getBalance', [
        TEST_SENDER_ADDRESS,
        { blockNumber: 'latest' },
      ]);
      expect(bal).to.match(/^0x[0-9a-fA-F]+$/);
    });

    it('eth_getBalance accepts {blockHash: ...} object form', async () => {
      const latest = await provider.getBlock('latest');
      expect(latest, 'latest block').to.not.be.null;
      const bal = await provider.send('eth_getBalance', [
        TEST_SENDER_ADDRESS,
        { blockHash: latest!.hash },
      ]);
      expect(bal).to.match(/^0x[0-9a-fA-F]+$/);
    });

    it('eth_call accepts {blockNumber: tag} object form', async () => {
      const result = await provider.send('eth_call', [
        { to: ZeroAddress, data: '0x' },
        { blockNumber: 'latest' },
      ]);
      expect(result).to.equal('0x');
    });

    it('eth_getTransactionCount accepts {blockNumber: 0x<hex>} numeric object form', async () => {
      const latestBn = await provider.getBlockNumber();
      const n = await provider.send('eth_getTransactionCount', [
        TEST_SENDER_ADDRESS,
        { blockNumber: toBeHex(latestBn) },
      ]);
      expect(n).to.match(/^0x[0-9a-fA-F]+$/);
    });
  });

  describe('ENS resolution (expected unsupported — Thor network carries no ENS plugin)', () => {
    // ethers v6 throws UNSUPPORTED_OPERATION "network does not support ENS"
    // because the Network object for Thor's chainId has no Ens plugin
    // registered. Each test attempts the real call and *skips* (rather than
    // failing the suite) when that client-side gap is detected — if Thor ever
    // ships an ENS registry + a matching network plugin, these flip to hard
    // assertions on the resolved value.
    const ensUnsupported = (err: unknown): boolean =>
      /does not support ENS|UNSUPPORTED_OPERATION/i.test(collectStrings(err).join(' || '));

    it('resolveName(name) — forward resolution', async function () {
      try {
        const addr = await provider.resolveName('vitalik.eth');
        expect(addr === null || /^0x[0-9a-fA-F]{40}$/.test(addr)).to.equal(true);
      } catch (err) {
        if (ensUnsupported(err)) this.skip();
        throw err;
      }
    });

    it('lookupAddress(address) — reverse resolution', async function () {
      try {
        const name = await provider.lookupAddress(TEST_SENDER_ADDRESS);
        expect(name === null || typeof name === 'string').to.equal(true);
      } catch (err) {
        if (ensUnsupported(err)) this.skip();
        throw err;
      }
    });

    it('getResolver(name) — resolver lookup', async function () {
      try {
        const resolver = await provider.getResolver('vitalik.eth');
        expect(resolver === null || typeof resolver === 'object').to.equal(true);
      } catch (err) {
        if (ensUnsupported(err)) this.skip();
        throw err;
      }
    });

    it('getAvatar(name) — avatar lookup', async function () {
      try {
        const avatar = await provider.getAvatar('vitalik.eth');
        expect(avatar === null || typeof avatar === 'string').to.equal(true);
      } catch (err) {
        if (ensUnsupported(err)) this.skip();
        throw err;
      }
    });
  });
});

// ---------------------------------------------------------------------------
// Coverage for eth_* RPC methods Thor implements but ethers exposes no
// high-level wrapper for — exercised via provider.send and asserted against
// the Ethereum JSON-RPC contract. Cross-checked against thor's rpc/ handlers
// on branch pedro/eth_eq_json_rpc.
// ---------------------------------------------------------------------------
describe('Block & transaction index methods (implemented on Thor)', () => {
  let provider: JsonRpcProvider;
  let txHash: string;
  let blockNumber: number;
  let blockHash: string;
  let txIndex: number;

  before(async () => {
    provider = makeProvider();
    const wallet = makeWallet(TEST_SENDER_KEY, provider);
    const tx = await wallet.sendTransaction({ to: NODE2_ADDRESS, value: 1n, type: 2 });
    const r = await tx.wait();
    txHash = tx.hash;
    blockNumber = r!.blockNumber;
    blockHash = r!.blockHash;
    txIndex = r!.index; // ethers v6 exposes the tx position as receipt.index
  });

  it('eth_getBlockTransactionCountByNumber matches the block tx array length', async () => {
    const block = await provider.getBlock(blockNumber, false);
    const count = (await provider.send('eth_getBlockTransactionCountByNumber', [
      toQuantity(blockNumber),
    ])) as string;
    expect(count).to.match(/^0x[0-9a-fA-F]+$/);
    expect(Number(BigInt(count))).to.equal(block!.transactions.length);
  });

  it('eth_getBlockTransactionCountByHash matches the block tx array length', async () => {
    const block = await provider.getBlock(blockNumber, false);
    const count = (await provider.send('eth_getBlockTransactionCountByHash', [blockHash])) as string;
    expect(count).to.match(/^0x[0-9a-fA-F]+$/);
    expect(Number(BigInt(count))).to.equal(block!.transactions.length);
  });

  it('eth_getTransactionByBlockNumberAndIndex returns the sent tx at its index', async () => {
    const t = (await provider.send('eth_getTransactionByBlockNumberAndIndex', [
      toQuantity(blockNumber),
      toQuantity(txIndex),
    ])) as { hash: string; blockHash: string } | null;
    expect(t, 'tx by (number,index)').to.not.be.null;
    expect(t!.hash.toLowerCase()).to.equal(txHash.toLowerCase());
    expect(t!.blockHash.toLowerCase()).to.equal(blockHash.toLowerCase());
  });

  it('eth_getTransactionByBlockHashAndIndex returns the sent tx at its index', async () => {
    const t = (await provider.send('eth_getTransactionByBlockHashAndIndex', [
      blockHash,
      toQuantity(txIndex),
    ])) as { hash: string } | null;
    expect(t, 'tx by (hash,index)').to.not.be.null;
    expect(t!.hash.toLowerCase()).to.equal(txHash.toLowerCase());
  });

  it('eth_getTransactionByBlockNumberAndIndex returns null for an out-of-range index', async () => {
    const t = await provider.send('eth_getTransactionByBlockNumberAndIndex', [
      toQuantity(blockNumber),
      '0xffff',
    ]);
    expect(t).to.equal(null);
  });
});

describe('Uncle methods (VeChain has no uncles — implemented as empty)', () => {
  let provider: JsonRpcProvider;
  before(() => {
    provider = makeProvider();
  });

  it('eth_getUncleCountByBlockNumber returns 0x0', async () => {
    const c = (await provider.send('eth_getUncleCountByBlockNumber', ['latest'])) as string;
    expect(c).to.match(/^0x0+$/);
  });

  it('eth_getUncleCountByBlockHash returns 0x0', async () => {
    const latest = await provider.getBlock('latest');
    const c = (await provider.send('eth_getUncleCountByBlockHash', [latest!.hash])) as string;
    expect(c).to.match(/^0x0+$/);
  });

  it('eth_getUncleByBlockNumberAndIndex returns null', async () => {
    const u = await provider.send('eth_getUncleByBlockNumberAndIndex', ['latest', '0x0']);
    expect(u).to.equal(null);
  });

  it('eth_getUncleByBlockHashAndIndex returns null', async () => {
    const latest = await provider.getBlock('latest');
    const u = await provider.send('eth_getUncleByBlockHashAndIndex', [latest!.hash, '0x0']);
    expect(u).to.equal(null);
  });
});

describe('Chain & node metadata (implemented on Thor)', () => {
  let provider: JsonRpcProvider;
  before(() => {
    provider = makeProvider();
  });

  it('net_version equals the decimal chainId', async () => {
    const netV = (await provider.send('net_version', [])) as string;
    const chainId = (await provider.getNetwork()).chainId;
    expect(netV).to.be.a('string');
    expect(BigInt(netV)).to.equal(chainId);
  });

  it('net_listening returns true', async () => {
    expect(await provider.send('net_listening', [])).to.equal(true);
  });

  it('net_peerCount returns a hex quantity', async () => {
    const pc = (await provider.send('net_peerCount', [])) as string;
    expect(pc).to.match(/^0x[0-9a-fA-F]+$/);
  });

  it('web3_clientVersion returns a Thor/* string', async () => {
    const v = (await provider.send('web3_clientVersion', [])) as string;
    expect(v).to.be.a('string').and.match(/thor/i);
  });

  it('eth_coinbase returns the zero address (PoA — no coinbase reward addr)', async () => {
    const cb = (await provider.send('eth_coinbase', [])) as string;
    expect(cb).to.match(/^0x0{40}$/);
  });

  it('eth_mining returns false (PoA — no local mining)', async () => {
    expect(await provider.send('eth_mining', [])).to.equal(false);
  });

  it('eth_hashrate returns 0x0 (PoA — no hashrate)', async () => {
    const hr = (await provider.send('eth_hashrate', [])) as string;
    expect(hr).to.match(/^0x0+$/);
  });

  it('eth_syncing returns false or a syncing-status object', async () => {
    const s = await provider.send('eth_syncing', []);
    expect(s === false || (typeof s === 'object' && s !== null)).to.equal(true);
  });
});

describe('Unimplemented standard eth_* methods (skipped until Thor ships them)', () => {
  let provider: JsonRpcProvider;
  before(() => {
    provider = makeProvider();
  });

  // True when an error reads as a JSON-RPC "method not found" / unsupported gap
  // rather than a transport or params failure.
  const notFound = (err: unknown): boolean =>
    /not found|not supported|unsupported|does not exist|not available|method .*missing/i.test(
      collectStrings(err).join(' || '),
    );

  // Standard Ethereum methods thor's pedro/eth_eq_json_rpc dispatcher does NOT
  // register. Each test attempts the call and skips while it 404s at the method
  // level; if Thor ever registers one, the call succeeds and the assertion
  // (result is defined) keeps it honest. A non-"not found" error fails loudly.
  const unimplemented: Array<{ method: string; params: unknown[] }> = [
    { method: 'eth_getProof', params: [TEST_SENDER_ADDRESS, [], 'latest'] },
    { method: 'eth_createAccessList', params: [{ from: TEST_SENDER_ADDRESS, to: NODE2_ADDRESS }, 'latest'] },
    { method: 'eth_protocolVersion', params: [] },
    { method: 'eth_pendingTransactions', params: [] },
    { method: 'eth_sign', params: [TEST_SENDER_ADDRESS, '0x68656c6c6f'] },
    { method: 'eth_signTransaction', params: [{ from: TEST_SENDER_ADDRESS, to: NODE2_ADDRESS, value: '0x1' }] },
    { method: 'eth_getRawTransactionByHash', params: ['0x' + '00'.repeat(32)] },
    { method: 'debug_traceTransaction', params: ['0x' + '00'.repeat(32)] },
  ];

  for (const { method, params } of unimplemented) {
    it(`${method} — skipped while unimplemented`, async function () {
      let result: unknown;
      let caught: unknown;
      try {
        result = await provider.send(method, params);
      } catch (err) {
        caught = err;
      }
      if (caught !== undefined && notFound(caught)) {
        this.skip();
      }
      // Reached only if Thor answered or errored for some *other* reason.
      expect(
        caught,
        `${method} errored for a non-"not found" reason: ${collectStrings(caught).join(' || ')}`,
      ).to.be.undefined;
      expect(result, `${method} unexpectedly returned undefined without an error`).to.not.be.undefined;
    });
  }
});

describe('Category-3 divergences from Ethereum (skipped until Thor aligns)', () => {
  let provider: JsonRpcProvider;
  before(() => {
    provider = makeProvider();
  });

  // geth's eth_feeHistory returns a per-block × per-percentile `reward` matrix
  // when called with rewardPercentiles. Thor (rpc/fees/handler.go) currently
  // rejects the percentile form — "reward percentiles are not yet supported" —
  // so a fee estimator that requests percentiles can't use it. We SKIP on that
  // documented gap; if Thor ever ships it, the call succeeds and the
  // reward-matrix assertion keeps it honest. The sibling "is rejected by Thor"
  // test above covers the current behavior.
  it('eth_feeHistory with rewardPercentiles returns a reward matrix (geth parity)', async function () {
    let raw: { reward?: string[][] };
    try {
      raw = (await provider.send('eth_feeHistory', ['0x4', 'latest', [25, 50, 75]])) as {
        reward?: string[][];
      };
    } catch (err) {
      if (/percentile|not yet supported/i.test(collectStrings(err).join(' || '))) {
        this.skip();
      }
      throw err;
    }
    expect(raw, 'feeHistory result').to.be.an('object');
    expect(raw.reward, 'reward matrix').to.be.an('array').and.length.greaterThan(0);
    for (const row of raw.reward!) {
      expect(row, 'per-block reward row').to.be.an('array').and.length(3);
    }
  });
});
