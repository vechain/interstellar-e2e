import { expect } from 'chai';
import {
  Contract,
  ContractFactory,
  WebSocketProvider,
  Wallet,
} from 'ethers';
import {
  loadStorageArtifact,
  makeProvider,
  makeWallet,
  makeWsProvider,
  NODE2_ADDRESS,
  TEST_SENDER_ADDRESS,
  TEST_SENDER_KEY,
} from '../src/fixtures';

// ethers v6 `WebSocketProvider` is functionally equivalent to JsonRpcProvider
// for read RPCs, but `provider.on('block')` / `contract.on('Event')` use
// `eth_subscribe('newHeads' | 'logs')` instead of the HTTP filter trio
// (eth_newFilter + eth_getFilterChanges polling). Thor's pedro/eth_eq_json_rpc
// branch handles the WS upgrade on the same /rpc path (cmd/thor/httpserver/api_server.go:161)
// and supports newHeads / logs subscriptions (rpc/ws/conn.go:183-200).
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
