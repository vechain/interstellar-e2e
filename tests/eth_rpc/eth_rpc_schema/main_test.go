package ethrpcschema

import (
	"os"
	"testing"

	"github.com/vechain/interstellar-e2e/tests/helper"
)

var nodeURL string

func TestMain(m *testing.M) {
	// The Ethereum-compatible JSON-RPC (POST/WS /rpc) lives on the thor
	// pedro/eth_eq_json_rpc branch; the default evm-upgrades branch built by
	// network/setup/network.go does not expose it. Set it unless the caller
	// already did. Same approach as tests/eth_rpc/ethersjs/ethersjs_test.go.
	if os.Getenv("THOR_BRANCH") == "" {
		os.Setenv("THOR_BRANCH", "pedro/eth_eq_json_rpc")
	}
	os.Exit(helper.RunTestMain(m, &nodeURL, nil))
}
