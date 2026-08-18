// Copyright (c) 2018 The VeChainThor developers
// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package contracts

// --evm-version cancun is pinned explicitly: TSTORE/TLOAD first exist at
// Cancun, and letting solc pick its own default risks emitting opcodes from a
// later fork that the INTERSTELLAR EVM does not implement.

// --platform linux/amd64 is required because ghcr.io/argotorg/solc publishes no
// arm64 manifest; without it the pull fails outright on Apple Silicon.

//go:generate sh -c "docker run --rm --platform linux/amd64 -v $(pwd):/src ghcr.io/argotorg/solc:stable --evm-version cancun --optimize --optimize-runs 200 --combined-json abi,bin,bin-runtime,hashes /src/TransientProbe.sol | docker run --rm -i -v $(pwd):/src otherview/solgen:latest --out /src/generated"
