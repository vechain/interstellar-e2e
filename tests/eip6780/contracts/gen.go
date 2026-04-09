// Copyright (c) 2018 The VeChainThor developers
// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package contracts

//go:generate docker run --rm -v ./:/sources ghcr.io/argotorg/solc:0.8.28 --evm-version cancun --optimize --optimize-runs 200 -o /sources/compiled --overwrite --abi --bin --bin-runtime /sources/Destructible.sol /sources/Factory.sol
