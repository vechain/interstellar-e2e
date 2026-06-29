import * as fs from 'node:fs';
import * as path from 'node:path';
import {
  createPublicClient,
  createWalletClient,
  defineChain,
  http,
  webSocket,
  type Abi,
  type Chain,
} from 'viem';
import { privateKeyToAccount } from 'viem/accounts';

// NODE_URL is exported by the Go wrapper (tests/eth_rpc/viem/viem_test.go) which
// manages the network lifecycle via helper.RunTestMain. Running `npx mocha` or
// `npm test` directly requires the caller to export NODE_URL themselves.
export function getNodeUrl(): string {
  const url = process.env.NODE_URL;
  if (!url) {
    throw new Error(
      'NODE_URL not set — run the suite via `go test` (which starts the network) or export NODE_URL manually',
    );
  }
  return url;
}

// Pre-funded master accounts from LocalThreeNodesNetwork genesis.
// Mirrors tests/helper/client.go:16-21 and the ethersjs / web3js fixtures.
export const TEST_SENDER_KEY =
  '0x01a4107bfb7d5141ec519e75788c34295741a1eefbfe460320efd2ada944071e' as const;
export const TEST_SENDER_ADDRESS = '0x61fF580B63D3845934610222245C116E013717ec' as const;

export const NODE2_KEY =
  '0x7072249b800ddac1d29a3cd06468cc1a917cbcd110dde358a905d03dad51748d' as const;
export const NODE2_ADDRESS = '0x327931085B4cCbCE0baABb5a5E1C678707C51d90' as const;

export const NODE3_KEY =
  '0xc55455943bf026dc44fcf189e8765eb0587c94e66029d580bae795386c0b737a' as const;
export const NODE3_ADDRESS = '0x084E48c8AE79656D7e27368AE5317b5c2D6a7497' as const;

export function getHttpUrl(): string {
  // Thor exposes the Ethereum-compatible JSON-RPC at <nodeURL>/rpc — the bare
  // URL returns 307. Mirrors tests/eth_rpc/eth_rpc_schema/rpc_test.go:71.
  return getNodeUrl().replace(/\/$/, '') + '/rpc';
}

export function getWsUrl(): string {
  // Thor accepts a WebSocket upgrade on the same /rpc path as HTTP POST
  // (cmd/thor/httpserver/api_server.go — `router.PathPrefix("/rpc").Handler(rpcWs)`).
  const base = getNodeUrl().replace(/\/$/, '');
  return base.replace(/^http/, 'ws') + '/rpc';
}

// makePublicClient builds a read-only client over the HTTP transport.
export function makePublicClient() {
  // viem defaults to 4s polling; tighten so waitForTransactionReceipt picks up a
  // freshly-packed receipt promptly under the 2s block interval.
  return createPublicClient({ transport: http(getHttpUrl()), pollingInterval: 250 });
}

// makeWsClient builds a client over the WebSocket transport — viem routes
// watch* actions through eth_subscribe on this transport.
export function makeWsClient() {
  return createPublicClient({ transport: webSocket(getWsUrl()) });
}

// getThorChain fetches eth_chainId once and wraps it in a viem Chain. The wallet
// client needs a Chain so local-account signing can stamp the right chainId; the
// id is dynamic for the local network so we read it rather than hardcode.
let cachedChain: Chain | undefined;
export async function getThorChain(): Promise<Chain> {
  if (cachedChain) return cachedChain;
  const chainId = await makePublicClient().getChainId();
  cachedChain = defineChain({
    id: chainId,
    name: 'thor-local',
    nativeCurrency: { name: 'VeThor', symbol: 'VTHO', decimals: 18 },
    rpcUrls: { default: { http: [getHttpUrl()], webSocket: [getWsUrl()] } },
  });
  return cachedChain;
}

// makeWalletClient builds a wallet client bound to a local account. viem signs
// locally and submits via eth_sendRawTransaction, which is exactly what Thor's
// EIP-1559-only RPC accepts.
export async function makeWalletClient(key: `0x${string}`) {
  const account = privateKeyToAccount(key);
  const chain = await getThorChain();
  return createWalletClient({ account, chain, transport: http(getHttpUrl()), pollingInterval: 250 });
}

// rpc is the raw JSON-RPC escape hatch — the viem analogue of ethers'
// provider.send() / web3's requestManager.send(). Used for methods that lack a
// high-level viem action (net_*, eth_coinbase, the uncle / filter family,
// EIP-1898 object forms, expected-rejection probes, ...).
export async function rpc(
  client: { request: (args: { method: string; params?: unknown }) => Promise<unknown> },
  method: string,
  params: unknown[] = [],
): Promise<unknown> {
  return client.request({ method, params });
}

export interface ContractArtifact {
  contractName: string;
  abi: Abi;
  bytecode: `0x${string}`;
}

export function loadStorageArtifact(): ContractArtifact {
  return loadArtifact('Storage');
}

export function loadCreate2FactoryArtifact(): ContractArtifact {
  return loadArtifact('Create2Factory');
}

function loadArtifact(name: string): ContractArtifact {
  const artifactPath = path.join(__dirname, '..', 'contracts', `${name}.json`);
  const raw = fs.readFileSync(artifactPath, 'utf8');
  return JSON.parse(raw) as ContractArtifact;
}

// collectStrings walks an unknown error/response and gathers every string-valued
// leaf — RPC error text can live at .message, .details, .cause.message,
// .shortMessage, etc. Used for skip / reject detection.
export function collectStrings(obj: unknown, depth = 0): string[] {
  const out: string[] = [];
  if (depth > 5 || obj == null) return out;
  if (typeof obj === 'string') {
    out.push(obj);
    return out;
  }
  if (typeof obj === 'object') {
    for (const v of Object.values(obj as Record<string, unknown>)) {
      out.push(...collectStrings(v, depth + 1));
    }
  }
  return out;
}
