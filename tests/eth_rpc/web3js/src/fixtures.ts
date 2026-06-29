import * as fs from 'node:fs';
import * as path from 'node:path';
import { Web3 } from 'web3';
import type { TransactionReceipt } from 'web3';

// NODE_URL is exported by the Go wrapper (tests/eth_rpc/web3js/web3js_test.go)
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
// Mirrors tests/helper/client.go:16-21 and the ethersjs fixtures.
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

export function getWsUrl(): string {
  // Thor accepts a WebSocket upgrade on the same /rpc path as HTTP POST
  // (cmd/thor/httpserver/api_server.go — `router.PathPrefix("/rpc").Handler(rpcWs)`).
  const base = getNodeUrl().replace(/\/$/, '');
  return base.replace(/^http/, 'ws') + '/rpc';
}

export function makeWeb3(): Web3 {
  return new Web3(getHttpUrl());
}

export function makeWsWeb3(): Web3 {
  // web3.js v4 auto-selects a WebSocketProvider when handed a ws:// URL.
  return new Web3(getWsUrl());
}

// Raw JSON-RPC escape hatch — the web3.js analogue of ethers' provider.send().
// Used for methods that lack a high-level wrapper (eth_getBlockReceipts,
// eth_uninstallFilter, EIP-1898 object forms, expected-rejection probes, ...).
export async function rpc(web3: Web3, method: string, params: unknown[] = []): Promise<unknown> {
  return web3.requestManager.send({ method, params });
}

export interface StorageArtifact {
  contractName: string;
  abi: import('web3').ContractAbi;
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

export interface Eip1559Tx {
  to?: string;
  value?: bigint;
  data?: string;
  gas: bigint;
}

// sendEip1559 offline-signs a type-2 transaction with `key` and submits it via
// eth_sendSignedTransaction (eth_sendRawTransaction), returning the mined
// receipt. Thor's Ethereum-compat RPC only accepts EIP-1559 envelopes, so every
// state-changing helper goes through this single deterministic path rather than
// web3.js's wallet auto-signing (which may emit a legacy type Thor would reject).
export async function sendEip1559(
  web3: Web3,
  key: string,
  tx: Eip1559Tx,
): Promise<TransactionReceipt> {
  const account = web3.eth.accounts.privateKeyToAccount(key);
  const chainId = await web3.eth.getChainId();
  const nonce = await web3.eth.getTransactionCount(account.address, 'pending');
  const baseFee = await fetchBaseFee(web3);
  const maxPriorityFeePerGas = 1n;
  const maxFeePerGas = baseFee * 2n + maxPriorityFeePerGas;

  const signed = await account.signTransaction({
    to: tx.to,
    value: tx.value ?? 0n,
    data: tx.data,
    gas: tx.gas,
    nonce,
    chainId,
    maxFeePerGas,
    maxPriorityFeePerGas,
    type: 2,
  });
  return web3.eth.sendSignedTransaction(signed.rawTransaction);
}

// signEip1559Raw offline-signs a type-2 tx and returns the raw RLP hex without
// broadcasting — for the "broadcast an offline-signed tx" and parse-back cases.
export async function signEip1559Raw(
  web3: Web3,
  key: string,
  tx: Eip1559Tx,
): Promise<string> {
  const account = web3.eth.accounts.privateKeyToAccount(key);
  const chainId = await web3.eth.getChainId();
  const nonce = await web3.eth.getTransactionCount(account.address, 'pending');
  const baseFee = await fetchBaseFee(web3);
  const maxPriorityFeePerGas = 1n;
  const maxFeePerGas = baseFee * 2n + maxPriorityFeePerGas;

  const signed = await account.signTransaction({
    to: tx.to,
    value: tx.value ?? 0n,
    data: tx.data,
    gas: tx.gas,
    nonce,
    chainId,
    maxFeePerGas,
    maxPriorityFeePerGas,
    type: 2,
  });
  return signed.rawTransaction;
}

export async function fetchBaseFee(web3: Web3): Promise<bigint> {
  const block = await web3.eth.getBlock('latest');
  return block.baseFeePerGas ?? 0n;
}

// deployContract deploys `artifact` from `key` via the EIP-1559 raw-tx path and
// returns { address, blockNumber } from the mined receipt.
export async function deployContract(
  web3: Web3,
  key: string,
  artifact: StorageArtifact,
): Promise<{ address: string; blockNumber: bigint }> {
  const account = web3.eth.accounts.privateKeyToAccount(key);
  const deployTx = new web3.eth.Contract(artifact.abi).deploy({ data: artifact.bytecode });
  const data = deployTx.encodeABI();
  const gas = await deployTx.estimateGas({ from: account.address });
  const receipt = await sendEip1559(web3, key, { data, gas });
  return {
    address: receipt.contractAddress as string,
    blockNumber: receipt.blockNumber as bigint,
  };
}

// contractSet submits a Storage.set(v) (or any single-uint256 setter) from `key`
// and waits for the receipt. Convenience for the events/websocket suites.
export async function contractSet(
  web3: Web3,
  key: string,
  address: string,
  abi: import('web3').ContractAbi,
  value: bigint,
): Promise<TransactionReceipt> {
  const account = web3.eth.accounts.privateKeyToAccount(key);
  const contract = new web3.eth.Contract(abi, address);
  // abi is the loose ContractAbi type, so method names aren't statically known.
  const method = (contract.methods as Record<string, (...args: unknown[]) => {
    estimateGas: (opts: { from: string }) => Promise<bigint>;
    encodeABI: () => string;
  }>).set(value);
  const gas = await method.estimateGas({ from: account.address });
  return sendEip1559(web3, key, { to: address, data: method.encodeABI(), gas });
}

// Collect every string-valued leaf of an unknown error/response — RPC error text
// can live at .message, .cause.message, .innerError, .data, etc.
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
