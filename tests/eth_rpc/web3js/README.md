# web3.js v4 compatibility tests

Exercises Thor's Ethereum-compatible JSON-RPC (`<node>/rpc`, HTTP + WebSocket)
through the [web3.js](https://docs.web3js.org) v4 client. The sibling
[`../ethersjs`](../ethersjs) suite covers the same surface via ethers v6; this
suite is the web3.js counterpart so both major JS clients are validated.

## Running

The suite is driven by a thin Go wrapper (`web3js_test.go`) so it runs as part
of the top-level `make test`, sharing the local three-node network lifecycle
with every other Go package (`helper.RunTestMain`). Dependencies install
automatically via the `web3js-deps` Make target.

Run just this suite (network must already be up and `NODE_URL` exported):

```sh
cd tests/eth_rpc/web3js
npm ci
NODE_URL=http://127.0.0.1:8131 npm test
```

## Layout

| File | Surface |
|---|---|
| `test/provider.test.ts`   | read-only RPC, fee history, block tags, EIP-1898, batching, node-side/EIP-4844 rejections |
| `test/wallet.test.ts`     | offline EIP-1559 signing & send, legacy/EIP-2930 rejection, message signing & recover |
| `test/contract.test.ts`   | deploy, `call`, reverts (string + custom error), estimateGas, CREATE2 parity, payable |
| `test/events.test.ts`     | `getPastEvents`, HTTP filter trio (`eth_newFilter`/`getFilterChanges`/`uninstallFilter`), `getLogs` shapes |
| `test/websocket.test.ts`  | `eth_subscribe` newHeads / logs / pendingTransactions, syncing-rejected, clean disconnect |
| `test/rpc-extra.test.ts`  | net/version (supported) + compatibility probes for methods Thor's support is TBD on |

## Thor-specific notes

- **EIP-1559 only.** `eth_sendRawTransaction` accepts only type-2 envelopes;
  legacy (type 0) and EIP-2930 (type 1) are rejected. Every state-changing
  helper signs type-2 offline and submits via `sendSignedTransaction`
  (`src/fixtures.ts: sendEip1559`) rather than web3.js wallet auto-signing,
  which may emit a legacy type.
- **No node-side keystore.** `eth_accounts` is `[]`; `eth_sendTransaction`,
  `personal_sign`, `eth_signTypedData_v4` are rejected.
- **No EIP-4844.** `eth_blobBaseFee` is rejected.
- **`eth_feeHistory`** rejects `rewardPercentiles`.
- **WS `syncing`** subscription is rejected (Thor implements only
  newHeads / logs / newPendingTransactions).
- **Probes** in `rpc-extra.test.ts` log each method's observed status; tighten
  them to a strict success/rejection assertion once Thor's behavior is settled.
