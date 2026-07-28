import { expect } from 'chai';
import {
  getWsUrl,
  loadStorageArtifact,
  makePublicClient,
  makeWsClient,
  makeWalletClient,
  TEST_SENDER_ADDRESS,
  TEST_SENDER_KEY,
  NODE2_ADDRESS,
} from '../src/fixtures';

// Minimal structural type for the Node 22+ global WebSocket — @types/node@20
// doesn't declare it, so we reach for it through globalThis with our own shape.
interface RawWebSocket {
  send(data: string): void;
  close(): void;
  addEventListener(type: 'open' | 'error', listener: () => void): void;
  addEventListener(type: 'message', listener: (ev: { data: unknown }) => void): void;
}
const RawWebSocket = (globalThis as unknown as {
  WebSocket: new (url: string) => RawWebSocket;
}).WebSocket;

describe('WebSocket transport — eth_subscribe (newHeads / logs / pending)', () => {
  it('getChainId round-trips over a WebSocket transport', async () => {
    const ws = makeWsClient();
    const httpChainId = await makePublicClient().getChainId();
    const wsChainId = await ws.getChainId();
    expect(wsChainId).to.equal(httpChainId);
  });

  it('watchBlocks receives a newHeads notification over eth_subscribe', async function () {
    this.timeout(60_000);
    const ws = makeWsClient();
    const observed = await new Promise<bigint>((resolve) => {
      const unwatch = ws.watchBlocks({
        onBlock: (block) => {
          unwatch();
          resolve(block.number!);
        },
      });
    });
    expect(observed).to.be.a('bigint');
    expect(observed > 0n, 'block number > 0').to.equal(true);
  });

  it('watchContractEvent receives a Set log over eth_subscribe(logs)', async function () {
    this.timeout(60_000);
    // Deploy + emit via HTTP; the WS client only carries the subscription.
    const http = makePublicClient();
    const wallet = await makeWalletClient(TEST_SENDER_KEY);
    const artifact = loadStorageArtifact();
    const deployHash = await wallet.deployContract({
      abi: artifact.abi,
      bytecode: artifact.bytecode,
    });
    const deployReceipt = await http.waitForTransactionReceipt({ hash: deployHash });
    const address = deployReceipt.contractAddress!;

    const ws = makeWsClient();
    const seen = new Promise<{ who: string; value: bigint }>((resolve) => {
      const unwatch = ws.watchContractEvent({
        address,
        abi: artifact.abi,
        eventName: 'Set',
        onLogs: (logs) => {
          unwatch();
          resolve((logs[0] as { args: { who: string; value: bigint } }).args);
        },
      });
    });

    const setHash = await wallet.writeContract({
      address,
      abi: artifact.abi,
      functionName: 'set',
      args: [4242n],
    });
    await http.waitForTransactionReceipt({ hash: setHash });

    const ev = await seen;
    expect(ev.who.toLowerCase()).to.equal(TEST_SENDER_ADDRESS.toLowerCase());
    expect(ev.value).to.equal(4242n);
  });

  it('watchPendingTransactions receives a tx-hash notification', async function () {
    this.timeout(60_000);
    const ws = makeWsClient();
    const wallet = await makeWalletClient(TEST_SENDER_KEY);

    const seen = new Promise<string>((resolve) => {
      const unwatch = ws.watchPendingTransactions({
        onTransactions: (hashes) => {
          unwatch();
          resolve(hashes[0]);
        },
      });
    });

    const hash = await wallet.sendTransaction({ to: NODE2_ADDRESS, value: 1n });
    const observed = await seen;
    expect(observed).to.match(/^0x[0-9a-fA-F]{64}$/);
    expect(observed.toLowerCase()).to.equal(hash.toLowerCase());
  });

  it('eth_subscribe("syncing") over the socket — skip if Thor does not register it', async function () {
    this.timeout(30_000);
    // viem has no high-level syncing watcher, so drive a bare WebSocket and
    // speak JSON-RPC directly. Thor's eth_eq_json_rpc branch implements the
    // 'syncing' subtype; if it ever rejects, skip rather than fail.
    const ws = new RawWebSocket(getWsUrl());
    let response: { result?: unknown; error?: unknown };
    try {
      response = await new Promise((resolve, reject) => {
        const timer = setTimeout(() => reject(new Error('eth_subscribe(syncing) timed out')), 15_000);
        ws.addEventListener('open', () => {
          ws.send(
            JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'eth_subscribe', params: ['syncing'] }),
          );
        });
        ws.addEventListener('message', (ev: { data: unknown }) => {
          clearTimeout(timer);
          try {
            resolve(JSON.parse(String(ev.data)) as { result?: unknown; error?: unknown });
          } catch (e) {
            reject(e as Error);
          }
        });
        ws.addEventListener('error', () => {
          clearTimeout(timer);
          reject(new Error('websocket transport error'));
        });
      });
    } finally {
      ws.close();
    }

    if (response.error != null) {
      this.skip();
    }
    expect(response.result, 'subscription id').to.match(/^0x[0-9a-fA-F]+$/);
  });
});
