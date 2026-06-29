import * as fs from 'node:fs';
import * as path from 'node:path';
import { ethers, JsonRpcProvider, Wallet, WebSocketProvider } from 'ethers';

// NODE_URL is exported by the Go wrapper (tests/eth_rpc/ethersjs/ethersjs_test.go)
// which manages the network lifecycle via helper.RunTestMain. Running `npx mocha`
// or `npm test` directly requires the caller to export NODE_URL themselves.
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
// Mirrors tests/helper/client.go:16-21.
export const TEST_SENDER_KEY =
  '0x01a4107bfb7d5141ec519e75788c34295741a1eefbfe460320efd2ada944071e';
export const TEST_SENDER_ADDRESS = '0x61fF580B63D3845934610222245C116E013717ec';

export const NODE2_KEY =
  '0x7072249b800ddac1d29a3cd06468cc1a917cbcd110dde358a905d03dad51748d';
export const NODE2_ADDRESS = '0x327931085B4cCbCE0baABb5a5E1C678707C51d90';

export const NODE3_KEY =
  '0xc55455943bf026dc44fcf189e8765eb0587c94e66029d580bae795386c0b737a';
export const NODE3_ADDRESS = '0x084E48c8AE79656D7e27368AE5317b5c2D6a7497';

export function getHttpUrl(): string {
  // Thor exposes the Ethereum-compatible JSON-RPC at <nodeURL>/rpc — the bare
  // URL returns 307. Mirrors tests/eth_rpc/eth_rpc_schema/rpc_test.go:71.
  return getNodeUrl().replace(/\/$/, '') + '/rpc';
}

export function makeProvider(): JsonRpcProvider {
  const provider = new JsonRpcProvider(getHttpUrl());
  // ethers v6 defaults to 4s polling; tighten so tx confirmations are observed
  // promptly under the 2s block interval instead of lagging a full poll period.
  provider.pollingInterval = 250;
  return provider;
}

export function getWsUrl(): string {
  // Thor accepts a WebSocket upgrade on the same /rpc path as HTTP POST
  // (cmd/thor/httpserver/api_server.go:161 — `router.PathPrefix("/rpc").Handler(rpcWs)`).
  const base = getNodeUrl().replace(/\/$/, '');
  return base.replace(/^http/, 'ws') + '/rpc';
}

export function makeWsProvider(): WebSocketProvider {
  // Node 22+ exposes a global WebSocket constructor; ethers picks it up when
  // given a URL string. CI Node version is pinned in .github/workflows/test.yml.
  return new WebSocketProvider(getWsUrl());
}

export function makeWallet(privateKey: string, provider: JsonRpcProvider): Wallet {
  return new Wallet(privateKey, provider);
}

export interface StorageArtifact {
  contractName: string;
  abi: ethers.InterfaceAbi;
  bytecode: string;
}

export function loadStorageArtifact(): StorageArtifact {
  return loadArtifact('Storage');
}

export function loadCreate2FactoryArtifact(): StorageArtifact {
  return loadArtifact('Create2Factory');
}

function loadArtifact(name: string): StorageArtifact {
  const artifactPath = path.join(__dirname, '..', 'contracts', `${name}.json`);
  const raw = fs.readFileSync(artifactPath, 'utf8');
  return JSON.parse(raw) as StorageArtifact;
}
