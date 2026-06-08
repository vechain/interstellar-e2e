# eth_rpc Client Method Test Report

> Test package: `github.com/vechain/interstellar-e2e/tests/eth_rpc`
> Test node: Thor branch `pedro/eth_eq_json_rpc` running on a local solo network
> Command: `go test ./tests/eth_rpc/... -v -timeout 600s`
> Total duration: ~102.5s
> Overall verdict: **FAIL** (most core RPCs pass; block hash consistency, `*AtHash` state lookups, `BlockReceipts(hash)`, and `eth_blobBaseFee` fail)

## Summary

| Status | Count |
| --- | --- |
| PASS (top-level cases) | 19 |
| FAIL (top-level cases) | 10 |
| SKIPPED (sub-cases) | 3 |

---

## 1. Network Family (`net_*`)

### 1.1 `NetworkID` — `net_version`

- **Top-level case**: `TestNetVersion`
- **RPC-Method**: `net_version`
- **Result**: PASS

### 1.2 `PeerCount` — `net_peerCount`

- **Top-level case**: `TestNetPeerCount`
- **RPC-Method**: `net_peerCount`
- **Result**: PASS

---

## 2. Chain State Family (`eth_*`)

### 2.1 `SyncProgress` — `eth_syncing`

- **Top-level case**: `TestEthSyncing`
- **RPC-Method**: `eth_syncing`
- **Result**: PASS
- **Description**: Asserts `eth_syncing` returns `false` (encoded by go-ethereum's client as `SyncProgress == nil`).

### 2.2 `ChainID` — `eth_chainId`

- **Top-level case**: `TestEthChainID`
- **RPC-Method**: `eth_chainId`
- **Result**: PASS
- **Description**: Calls `eth_chainId` and cross-checks it against the last 2 bytes of Thor's genesis block ID to make sure the chain ID matches the chain.

### 2.3 `SuggestGasPrice` — `eth_gasPrice`

- **Top-level case**: `TestEthGasPrice`
- **RPC-Method**: `eth_gasPrice`
- **Result**: PASS
- **Description**: Calls `eth_gasPrice` and asserts the value is non-negative.

### 2.4 `BlockNumber` — `eth_blockNumber`

- **Top-level case**: `TestEthBlockNumber`
- **RPC-Method**: `eth_blockNumber`
- **Result**: PASS
- **Description**: Fetches the latest block height.

---

## 3. Block Query Family (Header / Block)

### 3.1 `HeaderByNumber` — `eth_getBlockByNumber` (includeTxs=false)

- **Top-level case**: `TestEthGetBlockHeaderByNumber`
- **RPC-Method**: `eth_getBlockByNumber` (includeTxs=false)
- **Result**: FAIL (2 sub-cases fail, 1 sub-case passes)

| Sub-case | Result | Notes |
| --- | --- | --- |
| `latest` | FAIL | Header is returned, but `head.Hash()` (eth) differs byte-for-byte from `block.ID` returned by the Thor client |
| `nil (defaults to latest)` | FAIL | Same as above — block hash mismatch |
| `future block number` | PASS | A future block number correctly produces an error / nil header |

- **Root cause**: The block hash computed/returned by eth_rpc does not match Thor's native `block.ID`. This is a hash calculation method  difference between the eth-style and Thor-style block identifiers.

### 3.2 `HeaderByHash` — `eth_getBlockByHash` (includeTxs=false)

- **Top-level case**: `TestEthGetBlockByHash`
- **RPC-Method**: `eth_getBlockByHash` (includeTxs=false)
- **Result**: FAIL (1 fail, 2 pass)

| Sub-case | Result | Notes |
| --- | --- | --- |
| `valid hash` | FAIL | Header is returned, but head.Hash() (eth) differs byte-for-byte from `block.ID` returned by the Thor client |
| `non-existent hash` | PASS | A made-up hash correctly produces an error |
| `nil (zero) hash` | PASS | The zero hash correctly produces an error |

- **Root cause**: The block hash computed/returned by eth_rpc does not match Thor's native `block.ID`. This is a hash calculation method  difference between the eth-style and Thor-style block identifiers.

### 3.3 `BlockByNumber` — `eth_getBlockByNumber` (includeTxs=true)

- **Top-level case**: `TestEthGetBlockByNumber`
- **RPC-Method**: `eth_getBlockByNumber` (includeTxs=true)
- **Result**: FAIL (2 fail, 1 pass)

| Sub-case | Result | Notes |
| --- | --- | --- |
| `latest` | FAIL | Block ID mismatch against the Thor client |
| `nil (defaults to latest)` | FAIL | Block ID mismatch against the Thor client |
| `future block number` | PASS | Future block number returns an error (`unexpected end of JSON input`) |

- **Root cause**: The block hash computed/returned by eth_rpc does not match Thor's native `block.ID`. This is a hash calculation method  difference between the eth-style and Thor-style block identifiers.

### 3.4 `BlockByHash` — `eth_getBlockByHash` (includeTxs=true)

- **Top-level case**: `TestEthGetBlockByHashFull`
- **RPC-Method**: `eth_getBlockByHash` (includeTxs=true)
- **Result**: FAIL (1 fail, 1 pass)

| Sub-case | Result | Notes |
| --- | --- | --- |
| `valid hash` | FAIL | Block ID mismatch against the Thor client |
| `non-existent hash` | PASS | A made-up hash correctly produces an error |

- **Root cause**: The block hash computed/returned by eth_rpc does not match Thor's native `block.ID`. This is a hash calculation method  difference between the eth-style and Thor-style block identifiers.

---

## 4. Fee Market

### 4.1 `SuggestGasTipCap` — `eth_maxPriorityFeePerGas`

- **Top-level case**: `TestEthMaxPriorityFeePerGas`
- **RPC-Method**: `eth_maxPriorityFeePerGas`
- **Result**: PASS
- **Description**: Asserts the returned tip cap is non-negative. Actual value: `1000000000`.

### 4.2 `FeeHistory` — `eth_feeHistory`

- **Top-level case**: `TestEthFeeHistory`
- **RPC-Method**: `eth_feeHistory`
- **Result**: PASS (1 pass, 1 skip)

| Sub-case | Result | Notes |
| --- | --- | --- |
| `count=1 latest no percentiles` | PASS | Call without reward percentiles succeeds |
| `count=4 with percentiles` | SKIP | Node reports `reward percentiles are not yet supported`; the "unsupported" branch auto-skips |

### 4.3 `BlobBaseFee` — `eth_blobBaseFee`

- **Top-level case**: `TestEthBlobBaseFee`
- **RPC-Method**: `eth_blobBaseFee`
- **Result**: FAIL
- **Failure reason**: Node responds with `method "eth_blobBaseFee" not found` (Thor does not implement this RPC).
- **Suggestion**: Follow the pattern used elsewhere — skip via `skipIfMethodNotFound` when unsupported, instead of calling `require.NoError` unconditionally.

---

## 5. Account State Family (number / hash / pending variants)

### 5.1 `BalanceAt` / `BalanceAtHash` / `PendingBalanceAt` — `eth_getBalance`

- **Top-level case**: `TestEthGetBalance`
- **RPC-Method**: `eth_getBalance`
- **Result**: FAIL (1 fail, 2 pass)

| Sub-case | Client method | Result | Notes |
| --- | --- | --- | --- |
| `latest by number (nil)` | `BalanceAt` | PASS | Matches Thor account balance |
| `by hash` | `BalanceAtHash` | FAIL | Node returns `invalid block tag` — Thor does not accept a block hash as a block tag |
| `pending` | `PendingBalanceAt` | PASS | Non-nil balance returned |

### 5.2 `StorageAt` / `StorageAtHash` / `PendingStorageAt` — `eth_getStorageAt`

- **Top-level case**: `TestEthGetStorageAt`
- **RPC-Method**: `eth_getStorageAt`
- **Result**: FAIL (1 fail, 2 pass)

| Sub-case | Client method | Result | Notes |
| --- | --- | --- | --- |
| `latest by number (nil)` | `StorageAt` | PASS | Slot 0 of the zero address returns 32 zero bytes |
| `by hash` | `StorageAtHash` | FAIL | Node returns `invalid block tag` |
| `pending` | `PendingStorageAt` | PASS | Zero-value assertion passes |

### 5.3 `NonceAt` / `NonceAtHash` / `PendingNonceAt` — `eth_getTransactionCount`

- **Top-level case**: `TestEthGetTransactionCount`
- **RPC-Method**: `eth_getTransactionCount`
- **Result**: FAIL (1 fail, 2 pass)

| Sub-case | Client method | Result | Notes |
| --- | --- | --- | --- |
| `latest by number (nil)` | `NonceAt` | PASS | Zero-address nonce is 0 |
| `by hash` | `NonceAtHash` | FAIL | Node returns `invalid block tag` |
| `pending` | `PendingNonceAt` | PASS | Zero-value assertion passes |

### 5.4 `CodeAt` / `CodeAtHash` / `PendingCodeAt` — `eth_getCode`

- **Top-level case**: `TestEthGetCode`
- **RPC-Method**: `eth_getCode`
- **Result**: FAIL (1 fail, 2 pass)

| Sub-case | Client method | Result | Notes |
| --- | --- | --- | --- |
| `latest by number (nil)` | `CodeAt` | PASS | Zero address has no deployed code; returns empty |
| `by hash` | `CodeAtHash` | FAIL | Node returns `invalid block tag` |
| `pending` | `PendingCodeAt` | PASS | Returns empty |

> **Common issue**: Every `*AtHash` variant (state lookup by block hash) hits `invalid block tag` because Thor does not support hash-form block tags. This is a Thor implementation limitation; consider gating these sub-cases through `skipIfUnsupported` instead of failing.

---

## 6. Transaction / Block Counters

### 6.1 `SendTransaction` — `eth_sendRawTransaction`

- **Top-level case**: `TestEthSendTransaction`
- **RPC-Method**: `eth_sendRawTransaction`
- **Result**: PASS (10.01s)


### 6.2 `TransactionCount` — `eth_getBlockTransactionCountByHash`

- **Top-level case**: `TestEthGetBlockTransactionCountByHash`
- **RPC-Method**: `eth_getBlockTransactionCountByHash`
- **Result**: PASS (10.01s)

### 6.3 `PendingTransactionCount` — `eth_getBlockTransactionCountByNumber("pending")`

- **Top-level case**: `TestEthPendingTransactionCount`
- **RPC-Method**: `eth_getBlockTransactionCountByNumber` (`pending`)
- **Result**: PASS (10.01s)

### 6.4 `BlockReceipts` — `eth_getBlockReceipts`

- **Top-level case**: `TestEthGetBlockReceipts`
- **RPC-Method**: `eth_getBlockReceipts`
- **Result**: FAIL
- **Failure reason**: Calling with `BlockNumberOrHashWithHash(targetHash, false)` causes the node to return `invalid block tag` — Thor does not accept a block hash for this RPC either.
- **Suggestion**: Use the block-number form, or skip the hash variant.

---

## 7. Transaction Lookup

### 7.1 `TransactionByHash` — `eth_getTransactionByHash`

- **Top-level case**: `TestEthGetTransactionByHash`
- **RPC-Method**: `eth_getTransactionByHash`
- **Result**: PASS (10.01s)

### 7.2 `TransactionInBlock` — `eth_getTransactionByBlockHashAndIndex`

- **Top-level case**: `TestEthGetTransactionByBlockHashAndIndex`
- **RPC-Method**: `eth_getTransactionByBlockHashAndIndex`
- **Result**: PASS (10.01s)

### 7.3 `TransactionReceipt` — `eth_getTransactionReceipt`

- **Top-level case**: `TestEthGetTransactionReceipt`
- **RPC-Method**: `eth_getTransactionReceipt`
- **Result**: PASS

| Sub-case | Client method | Result | Notes |
| --- | --- | --- | --- |
| `valid hash` | `TransactionReceipt` | PASS | |
| `invalid hash` | `TransactionReceipt` | PASS | Returns error |

### 7.4 `TransactionSender`

- **Top-level case**: `TestEthTransactionSender`
- **RPC-Method**: `eth_getTransactionByBlockHashAndIndex` (slow-path fallback for sender recovery)
- **Result**: PASS (12.02s)
- **Description**: Sends a transaction → fetches its receipt → looks up the tx body via `TransactionByHash` → calls `TransactionSender`, which exercises the slow-path RPC fallback for sender recovery. Asserts the recovered address matches the one derived from `TestSenderKey`.

---

## 8. Call / Estimate

### 8.1 `CallContract` / `CallContractAtHash` / `PendingCallContract` — `eth_call`

- **Top-level case**: `TestEthCall`
- **RPC-Method**: `eth_call`
- **Result**: PASS (2 pass, 1 skip)

| Sub-case | Client method | Result | Notes |
| --- | --- | --- | --- |
| `latest by number (nil)` | `CallContract` | PASS | Empty call from zero to zero returns empty output |
| `by hash` | `CallContractAtHash` | SKIP | Node returns `invalid block tag`; auto-skipped |
| `pending` | `PendingCallContract` | PASS | Call succeeds |

### 8.2 `EstimateGas` / `EstimateGasAtBlock` / `EstimateGasAtBlockHash` — `eth_estimateGas`

- **Top-level case**: `TestEthEstimateGas`
- **RPC-Method**: `eth_estimateGas`
- **Result**: PASS (2 pass, 1 skip)

| Sub-case | Client method | Result | Notes |
| --- | --- | --- | --- |
| `default state` | `EstimateGas` | PASS | Estimate: `21000` |
| `at latest number` | `EstimateGasAtBlock` | PASS | Estimate: `21000` |
| `at hash` | `EstimateGasAtBlockHash` | SKIP | Node returns `invalid block tag`; auto-skipped |

---

## 9. Logs

### 9.1 `FilterLogs` — `eth_getLogs`

- **Top-level case**: `TestEthGetLogs`
- **RPC-Method**: `eth_getLogs`
- **Result**: PASS (8.01s)
- **Description**: Has node1 invoke `transfer(node2, 10*1e18)` on the VTHO contract, which emits a single `Transfer` log; then queries `FilterLogs(FromBlock=0, ToBlock=receipt.BlockNumber)` and asserts exactly one log is returned with a `TxHash` matching the submitted transaction.

---

## 10. Unit Utilities

### 10.1 `RevertErrorData` (package-level helper)

- **Top-level case**: `TestRevertErrorData`
- **RPC-Method**: N/A (pure unit test for a package-level helper, no RPC call)
- **Result**: PASS
- **Description**: Pure unit test, no RPC connection needed. Covers six sub-cases, all passing:

| Sub-case | Result |
| --- | --- |
| `nil error` | PASS |
| `plain error` | PASS |
| `wrong code` | PASS |
| `non-string data` | PASS |
| `invalid hex` | PASS |
| `valid revert payload` | PASS |

---

## Failure Root-Cause Buckets (for fix prioritization)

| Bucket | Affected methods | Root cause |
| --- | --- | --- |
| Block hash mismatch | `HeaderByNumber`, `HeaderByHash`, `BlockByNumber`, `BlockByHash` | The block hash returned by eth_rpc does not match Thor's internal `block.ID` at the byte level |
| `invalid block tag` | `BalanceAtHash`, `StorageAtHash`, `NonceAtHash`, `CodeAtHash`, `BlockReceipts(hash)`, `CallContractAtHash`, `EstimateGasAtBlockHash` | Thor does not accept a block hash as a block tag. The last two auto-skip in the test; the other five FAIL and could be gated with a skip branch |
| Not implemented by node | `BlobBaseFee` | Thor does not implement `eth_blobBaseFee`; the test lacks a `skipIfMethodNotFound` branch |
