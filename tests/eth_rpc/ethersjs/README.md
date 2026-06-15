# ethers.js v6 RPC compatibility tests

Mocha + TypeScript suite (41 tests) that drives Thor's Ethereum-compatible
JSON-RPC through stock `ethers@^6`. Covers provider reads (block/tx/log/fee
lookups, `eth_feeHistory`, `eth_getBlockReceipts`), offline signing
(`populateTransaction` / `signTransaction` / `broadcastTransaction`), in-line
EIP-1559 sends, EIP-191/712 signing, HD-wallet derivation, contract
deploy/view/write, payable functions with value, custom error decoding,
`Interface.parseLog`, event subscriptions (`contract.on`, `provider.on('block')`),
historical filters (`queryFilter` w/ indexed args), and HTTP long-polling
filters (`eth_newFilter` / `eth_getFilterChanges` / `eth_uninstallFilter`).

## Known Thor ↔ Ethereum gaps

The suite asserts these as **rejection** tests so they fail loudly when Thor
catches up — flip the assertion when that happens.

- **Legacy (type-0) transactions are rejected.** Thor's
  `eth_sendRawTransaction` only accepts EIP-1559 envelopes; legacy RLP comes
  back as `rlp: expected List`. See `test/wallet.test.ts`.
- **`eth_feeHistory` rejects `rewardPercentiles`.** The method works without
  the third argument (baseFee + gasUsedRatio come back fine); passing
  percentiles returns `"reward percentiles are not yet supported"`. See
  `test/provider.test.ts`.

## Run

```sh
npm install
npm test
```

`pretest` rebuilds `/tmp/interstellar-network` from the workspace; the global
fixture in `src/globalSetup.ts` spawns it, reads the JSON ready-line from
stdout, and SIGTERMs it on teardown.

To target an externally-managed node, set `NODE_URL`:

```sh
NODE_URL=http://127.0.0.1:8131 npm test
```

## Can Mocha call `helper.RunTestMain`?

No — `helper.RunTestMain` is a Go function and Mocha is JavaScript. The
fixture in `src/globalSetup.ts` mirrors the same protocol the Go helper uses:

| `helper.RunTestMain` (Go)                          | `mochaGlobalSetup` (TS)                       |
| -------------------------------------------------- | --------------------------------------------- |
| Honors `NODE_URL` env var                          | same                                          |
| Spawns `/tmp/interstellar-network start`           | same                                          |
| Line-scans stdout for `{"nodes":[...],...}` JSON   | same                                          |
| 15-minute startup timeout (first-run thor compile) | same                                          |
| `defer stop()` → SIGTERM the child                 | `mochaGlobalTeardown` → SIGTERM + await exit  |

So the JS fixture is a one-to-one port of the Go helper.

## Thor branch requirement

Thor's Ethereum-compat RPC (`POST /rpc`) ships on the `pedro/eth_eq_json_rpc`
branch. The default `evm-upgrades` branch wired into `network/setup/network.go`
does not expose it. `globalSetup.ts` sets `THOR_BRANCH=pedro/eth_eq_json_rpc`
for the spawned binary unless the caller has already exported it.

## Regenerating the contract artifact

`contracts/Storage.json` is checked in. To rebuild it after changing
`contracts/Storage.sol`:

```sh
npm run compile:contracts
```

## Layout

```text
src/globalSetup.ts   # spawns network binary, exposes node URL via env
src/fixtures.ts      # provider/wallet factories + pre-funded test keys
contracts/Storage.sol, Storage.json
scripts/compile.cjs  # one-shot solc → Storage.json
test/provider.test.ts # 16 tests — block/tx/log lookups, getFeeData,
                      #            eth_feeHistory, eth_getBlockReceipts
test/wallet.test.ts   # 10 tests — EIP-1559 send, EIP-191/712 sign, HD wallet,
                      #            populate/sign/broadcastTransaction
test/contract.test.ts # 9 tests  — deploy, view/write, custom error, parseLog,
                      #            payable tip with value
test/events.test.ts   # 5 tests  — contract.on, queryFilter, indexed filter,
                      #            HTTP filter trio (newFilter/getChanges/uninstall)
```

The pre-funded test accounts mirror `tests/helper/client.go:16-21` —
`TEST_SENDER_ADDRESS`, `NODE2_ADDRESS`, `NODE3_ADDRESS` and their keys.

## Coverage gaps still open

The suite hits the main ethers v6 surface every dApp uses, but does **not**
yet cover: `WebSocketProvider` / `eth_subscribe`, `eth_blobBaseFee` (EIP-4844),
`eth_getBlockReceipts`, EIP-2930 access lists, multicall / `Multicall3`,
`getCreate2Address` deploy parity, reorg-driven `removed` event handling,
batched `JsonRpcProvider`. Add as the corresponding RPC methods land in Thor
or as a real dApp scenario calls for them.
