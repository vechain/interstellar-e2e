import { expect } from 'chai';
import { Web3 } from 'web3';
import {
  contractSet,
  deployContract,
  loadStorageArtifact,
  makeWeb3,
  makeWsWeb3,
  sendEip1559,
  NODE2_ADDRESS,
  TEST_SENDER_KEY,
} from '../src/fixtures';

// web3.js v4 WebSocketProvider exposes disconnect()/getStatus() on currentProvider.
interface SocketProviderLike {
  disconnect: (code?: number, reason?: string) => void;
  getStatus: () => 'connecting' | 'connected' | 'disconnected';
}
function socket(web3: Web3): SocketProviderLike {
  return web3.currentProvider as unknown as SocketProviderLike;
}

describe('WebSocketProvider — eth_subscribe (newHeads / logs / pending)', () => {
  it('getChainId works over a WebSocket transport', async () => {
    const ws = makeWsWeb3();
    try {
      const httpChainId = await makeWeb3().eth.getChainId();
      const wsChainId = await ws.eth.getChainId();
      expect(wsChainId).to.equal(httpChainId);
    } finally {
      socket(ws).disconnect();
    }
  });

  it('subscribe("newBlockHeaders") receives a notification over eth_subscribe', async function () {
    this.timeout(60_000);
    const ws = makeWsWeb3();
    try {
      const sub = await ws.eth.subscribe('newBlockHeaders');
      const header = await new Promise<{ number: bigint }>((resolve, reject) => {
        sub.on('data', (h) => resolve(h as unknown as { number: bigint }));
        sub.on('error', reject);
      });
      expect(header.number > 0n, `header.number was ${header.number}`).to.equal(true);
      await sub.unsubscribe();
    } finally {
      socket(ws).disconnect();
    }
  });

  it('subscribe("logs") receives a Set log over eth_subscribe(logs)', async function () {
    this.timeout(60_000);
    // Deploy + emit via HTTP so the WS path only carries subscription traffic.
    const http = makeWeb3();
    const setTopic = http.utils.keccak256('Set(address,uint256)');
    const artifact = loadStorageArtifact();
    const { address } = await deployContract(http, TEST_SENDER_KEY, artifact);

    const ws = makeWsWeb3();
    try {
      const sub = await ws.eth.subscribe('logs', { address, topics: [setTopic] });
      const seen = new Promise<{ topics: string[]; transactionHash: string }>((resolve, reject) => {
        sub.on('data', (l) => resolve(l as unknown as { topics: string[]; transactionHash: string }));
        sub.on('error', reject);
      });

      const receipt = await contractSet(http, TEST_SENDER_KEY, address, artifact.abi, 4242n);
      const log = await seen;
      expect(log.topics[0]).to.equal(setTopic);
      expect(log.transactionHash.toLowerCase()).to.equal(
        (receipt.transactionHash as string).toLowerCase(),
      );
      await sub.unsubscribe();
    } finally {
      socket(ws).disconnect();
    }
  });

  it('subscribe("pendingTransactions") receives a tx-hash notification', async function () {
    this.timeout(60_000);
    const http = makeWeb3();
    const ws = makeWsWeb3();
    try {
      const sub = await ws.eth.subscribe('pendingTransactions');
      const seen = new Promise<string>((resolve, reject) => {
        sub.on('data', (h) => resolve(h as unknown as string));
        sub.on('error', reject);
      });

      const receipt = await sendEip1559(http, TEST_SENDER_KEY, {
        to: NODE2_ADDRESS,
        value: 1n,
        gas: 21_000n,
      });

      const observed = await seen;
      expect(observed).to.match(/^0x[0-9a-fA-F]{64}$/);
      expect(observed.toLowerCase()).to.equal((receipt.transactionHash as string).toLowerCase());
      await sub.unsubscribe();
    } finally {
      socket(ws).disconnect();
    }
  });

  it('subscribe("syncing") is accepted by Thor (returns a subscription id)', async function () {
    this.timeout(30_000);
    // Thor's eth_eq_json_rpc branch implements the 'syncing' subtype: eth_subscribe
    // returns a subscription id and immediately pushes the status (false when in
    // sync). The earlier "syncing is rejected" expectation no longer holds — the
    // Go-side rejection test was removed alongside this change. We assert on the
    // subscription id (proof Thor accepted the eth_subscribe) rather than the
    // data frame, since web3.js's SyncingSubscription does not surface a `false`
    // (not-syncing) payload as a 'data' event.
    const ws = makeWsWeb3();
    try {
      const sub = await ws.eth.subscribe('syncing');
      expect(sub.id, 'syncing subscription id').to.match(/^0x[0-9a-fA-F]+$/);
      await sub.unsubscribe();
    } finally {
      socket(ws).disconnect();
    }
  });

  it('provider.disconnect() closes the websocket cleanly', async () => {
    const ws = makeWsWeb3();
    const bn = await ws.eth.getBlockNumber();
    expect(bn > 0n).to.equal(true);
    expect(socket(ws).getStatus()).to.equal('connected');

    socket(ws).disconnect();
    await new Promise((r) => setTimeout(r, 200));
    expect(socket(ws).getStatus()).to.equal('disconnected');
  });
});
