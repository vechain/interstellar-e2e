import { expect } from 'chai';
import {
  Contract,
  ContractFactory,
  WebSocketProvider,
  Wallet,
} from 'ethers';
import {
  getWsUrl,
  loadStorageArtifact,
  makeProvider,
  makeWallet,
  makeWsProvider,
  NODE2_ADDRESS,
  TEST_SENDER_ADDRESS,
  TEST_SENDER_KEY,
} from '../src/fixtures';

// Minimal structural type for the Node 22+ global WebSocket — @types/node@20
// doesn't declare it, so we reach for it through globalThis with our own shape
// rather than relying on the lib typings.
interface RawWebSocket {
  send(data: string): void;
  close(): void;
  addEventListener(type: 'open' | 'error', listener: () => void): void;
  addEventListener(type: 'message', listener: (ev: { data: unknown }) => void): void;
}
const RawWebSocket = (globalThis as unknown as {
  WebSocket: new (url: string) => RawWebSocket;
}).WebSocket;

describe('WebSocketProvider — eth_subscribe (newHeads / logs)', () => {
  it('getChainId works over a WebSocket transport', async () => {
    const wsProvider = makeWsProvider();
    try {
      const httpChainId = (await makeProvider().getNetwork()).chainId;
      const wsChainId = (await wsProvider.getNetwork()).chainId;
      expect(wsChainId).to.equal(httpChainId);
    } finally {
      await wsProvider.destroy();
    }
  });

  it('provider.on("block") receives a notification over eth_subscribe', async function () {
    this.timeout(60_000);
    const wsProvider = makeWsProvider();
    try {
      const seen = new Promise<number>((resolve) => {
        wsProvider.on('block', (n: number) => resolve(n));
      });
      const observed = await seen;
      expect(observed).to.be.a('number').and.greaterThan(0);
    } finally {
      wsProvider.removeAllListeners();
      await wsProvider.destroy();
    }
  });

  it('contract.on("Set") receives a notification over eth_subscribe(logs)', async function () {
    this.timeout(60_000);
    // We need a deployed contract; deploy it via the HTTP provider so the WS
    // path only carries the subscription traffic we're actually testing.
    const httpProvider = makeProvider();
    const wallet: Wallet = makeWallet(TEST_SENDER_KEY, httpProvider);
    const artifact = loadStorageArtifact();
    const factory = new ContractFactory(artifact.abi, artifact.bytecode, wallet);
    const deployed = await factory.deploy();
    await deployed.waitForDeployment();
    const address = await deployed.getAddress();

    const wsProvider = makeWsProvider();
    try {
      const contract = new Contract(address, artifact.abi, wsProvider);
      const seen = new Promise<{ who: string; value: bigint }>((resolve) => {
        contract.on('Set', (who: string, value: bigint) => {
          resolve({ who, value });
        });
      });

      // Send the Set tx through the HTTP wallet — the WS contract handle is
      // listen-only. ethers' `contract.connect(httpWallet)` clones with a writer.
      const writableContract = contract.connect(wallet) as Contract;
      const tx = await writableContract.set(4242n);
      await tx.wait();

      const ev = await seen;
      expect(ev.who.toLowerCase()).to.equal(TEST_SENDER_ADDRESS.toLowerCase());
      expect(ev.value).to.equal(4242n);
    } finally {
      await wsProvider.destroy();
    }
  });

  it('provider.on("pending") receives a tx-hash notification over eth_subscribe(newPendingTransactions)', async function () {
    this.timeout(60_000);
    // ethers v6 maps the high-level 'pending' event to
    // eth_subscribe('newPendingTransactions'). Thor pushes the tx hash for
    // every executable TypeEthDynamicFee tx that enters the pool
    // (rpc/ws/subscriptions.go:118), so an EIP-1559 transfer from the HTTP
    // wallet must produce a callback on the WS provider before the receipt
    // lands.
    const wsProvider = makeWsProvider();
    const httpProvider = makeProvider();
    const wallet = makeWallet(TEST_SENDER_KEY, httpProvider);

    try {
      const seen = new Promise<string>((resolve) => {
        wsProvider.on('pending', (hash: string) => resolve(hash));
      });

      const tx = await wallet.sendTransaction({
        to: NODE2_ADDRESS,
        value: 1n,
        type: 2,
      });

      const observed = await seen;
      expect(observed).to.match(/^0x[0-9a-fA-F]{64}$/);
      expect(observed.toLowerCase()).to.equal(tx.hash.toLowerCase());
    } finally {
      wsProvider.removeAllListeners();
      await wsProvider.destroy();
    }
  });

  it('wsProvider.once("block") fires exactly once over the socket', async function () {
    this.timeout(60_000);
    const wsProvider = makeWsProvider();
    try {
      let count = 0;
      const n = await new Promise<number>((resolve) => {
        wsProvider.once('block', (b: number) => {
          count += 1;
          resolve(b);
        });
      });
      expect(n).to.be.a('number').and.greaterThan(0);
      // A couple of head notifications should arrive in the next 2s; a one-shot
      // listener must not refire on them.
      await new Promise((r) => setTimeout(r, 2_000));
      expect(count, 'ws once("block") fired more than once').to.equal(1);
    } finally {
      wsProvider.removeAllListeners();
      await wsProvider.destroy();
    }
  });

  it('eth_subscribe("syncing") over the socket — skip if Thor does not register it', async function () {
    this.timeout(30_000);
    // ethers exposes no high-level "syncing" event and routing a raw
    // eth_subscribe through its SocketProvider fights the internal subscription
    // manager — so drive a bare WebSocket and speak JSON-RPC directly. Thor has
    // historically not implemented the "syncing" subscription topic; if it
    // still rejects the request we skip (documenting the gap) instead of
    // failing. If Thor adds it, this asserts a well-formed subscription id.
    const ws = new RawWebSocket(getWsUrl());
    let response: { result?: unknown; error?: unknown };
    try {
      response = await new Promise((resolve, reject) => {
        const timer = setTimeout(
          () => reject(new Error('eth_subscribe(syncing) timed out')),
          15_000,
        );
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
      // Thor rejected the subscription topic — documented gap, not a failure.
      this.skip();
    }
    expect(response.result, 'subscription id').to.match(/^0x[0-9a-fA-F]+$/);
  });

  it('provider.destroy() closes the websocket cleanly', async () => {
    const wsProvider = makeWsProvider();
    // One successful call to confirm the connection is live...
    const bn1 = await wsProvider.getBlockNumber();
    expect(bn1).to.be.greaterThan(0);

    // ethers v6 exposes the underlying socket via `websocket`; we capture its
    // pre-destroy readyState (1 = OPEN) and then check that destroy moves it
    // to CLOSING (2) or CLOSED (3). Subsequent RPC behavior is timing-dependent
    // (ethers may resolve in-flight requests or reject immediately), so we
    // anchor on the socket-level state transition instead.
    const ws = (wsProvider as unknown as { websocket: { readyState: number } }).websocket;
    expect(ws.readyState, 'OPEN before destroy').to.equal(1);

    await wsProvider.destroy();

    // Give the underlying close handshake a tick to settle.
    await new Promise((r) => setTimeout(r, 100));
    expect(ws.readyState, 'CLOSING (2) or CLOSED (3) after destroy').to.be.oneOf([2, 3]);
  });
});
