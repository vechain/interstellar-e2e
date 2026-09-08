# interstellar-e2e

End-to-end tests for the VeChain **INTERSTELLAR** fork, which activates at block 1 in the test network.

## EIPs covered

| Folder | EIP | Description |
|--------|-----|-------------|
| `tests/eip5656` | [EIP-5656](https://eips.ethereum.org/EIPS/eip-5656) | `MCOPY` opcode (0x5e) for in-memory copying |
| `tests/eip7823` | [EIP-7823](https://eips.ethereum.org/EIPS/eip-7823) | ModExp upper bound (1024-byte limit on base/exp/mod) |
| `tests/eip7825` | [EIP-7825](https://eips.ethereum.org/EIPS/eip-7825) | Per-transaction gas limit cap (`MaxTxGasLimit = 1 << 24`) |
| `tests/eip7934` | [EIP-7934](https://eips.ethereum.org/EIPS/eip-7934) | Max RLP-encoded block size (`MaxRLPBlockSize = 8_388_608`); packer-level split test + P2P consensus-level rejection of oversized blocks |
| `tests/eip7883` | [EIP-7883](https://eips.ethereum.org/EIPS/eip-7883) | ModExp precompile repricing |
| `tests/eip7939` | [EIP-7939](https://eips.ethereum.org/EIPS/eip-7939) | `CLZ` opcode (0x1e) — count leading zeros |
| `tests/eip2935` | [EIP-2935](https://eips.ethereum.org/EIPS/eip-2935) | Serve historical block hashes from state |
| `tests/eip1153` | [EIP-1153](https://eips.ethereum.org/EIPS/eip-1153) | Transient storage opcodes `TLOAD`/`TSTORE` (0xb3/0xb4) |
| `tests/eip6780` | [EIP-6780](https://eips.ethereum.org/EIPS/eip-6780) | `SELFDESTRUCT` only when called in the same transaction that created the contract |
| `tests/eip7951` | [EIP-7951](https://eips.ethereum.org/EIPS/eip-7951) | secp256r1 `P256VERIFY` precompile (0x100) |

Not yet covered: [EIP-2537](https://eips.ethereum.org/EIPS/eip-2537) (BLS12-381 precompiles, addresses `0x0b`–`0x11`).

## Repository layout

```
interstellar-e2e/
├── go.work                  # workspace linking this repo + local thor + networkhub
├── Makefile
├── network/                 # network binary (start/stop/status/node-url/node-p2p-port)
│   ├── cmd/
│   └── setup/               # 3-node genesis config with INTERSTELLAR at block 1
└── tests/
    ├── helper/              # shared test utilities (client, network lifecycle)
    ├── eip5656/
    ├── eip7823/
    ├── eip7825/
    ├── eip7934/
    ├── eip7883/
    └── eip7939/
    └── eip2935/
```

## Prerequisites

- Go 1.26+

INTERSTELLAR shipped as the tagged release [`vechain/thor@v2.5.0`](https://github.com/vechain/thor/releases/tag/v2.5.0), so no local thor checkout is required — `make test` builds thor from that tag automatically. For iterating against unreleased thor changes, point `THOR_BRANCH` at a branch/commit (see Environment variables below), or use a local checkout via `go.work`:

```
parent/
├── interstellar-e2e/
└── thor/
```

## Running the tests

```bash
make test
```

This builds the network binary, starts a 3-node local network, runs all test packages against it, then stops the network. On the first run, ThorBuilder clones and compiles thor `v2.5.0` — this can take ~15 minutes. Subsequent runs reuse the cached binary.

To run a single EIP package during development:

```bash
go test -v ./tests/eip7883/...
go test -v ./tests/eip7934/...
```

This starts its own network automatically (no `make` needed).

## Makefile targets

| Target | Description |
|--------|-------------|
| `make test` | Build, start network, run all tests, stop |
| `make build-network` | Compile the network binary to `/tmp/interstellar-network` |
| `make stop` | Stop a running network |
| `make status` | Show running network nodes and health |
| `make clean` | Stop network and remove binary + state file |

## Environment variables

| Variable | Description |
|----------|-------------|
| `NODE_URL` | Skip network start and use this node URL directly |
| `NODE_P2P_PORT` | Passed automatically by `make test`; set it manually only for P2P-based tests such as `tests/eip7934` when `NODE_URL` points to an already-running external node |
| `THOR_EXISTING_PATH` | Use a pre-built thor binary instead of building from source |
| `THOR_REPO` | Override the thor Git repo URL (default: `https://github.com/vechain/thor`) |
| `THOR_BRANCH` | Override the thor branch/tag to build (default: `v2.5.0`) |

## Pre-fork / post-fork testing

Each EIP test uses `InspectClauses` with a block revision to test behaviour on both sides of the fork boundary without mining new blocks:

- `Revision("0")` — genesis block, INTERSTELLAR not yet active
- `Revision("1")` — block 1, INTERSTELLAR active

EIP-7825 and EIP-7934 are exceptions: their checks are enforced on transaction submission/packing paths, not by `InspectClauses`, so those tests send real transactions and assert submission/inclusion behaviour. EIP-7934 additionally tests the **consensus-layer** rejection path by connecting to a node via devp2p and sending a validly-signed block whose RLP size exceeds the limit.
