// Copyright (c) 2018 The VeChainThor developers
// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package contracts

// //go:generate docker run --rm -v ./:/sources ghcr.io/argotorg/solc:0.8.28 --evm-version cancun --optimize --optimize-runs 200 -o /sources/compiled --overwrite --abi --bin --bin-runtime /sources/Destructible.sol /sources/Factory.sol

//go:generate sh -c "docker run --rm -v $(pwd):/src ghcr.io/argotorg/solc:stable --combined-json abi,bin,bin-runtime,hashes /src/Destructible.sol | docker run --rm -i -v $(pwd):/src solgen:local --out /src/generated"
//go:generate sh -c "docker run --rm -v $(pwd):/src ghcr.io/argotorg/solc:stable --combined-json abi,bin,bin-runtime,hashes /src/Factory.sol | docker run --rm -i -v $(pwd):/src solgen:local --out /src/generated"
