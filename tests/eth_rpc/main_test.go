package ethrpc

import (
	"os"
	"testing"

	"github.com/vechain/interstellar-e2e/tests/helper"
)

var nodeURL string

func TestMain(m *testing.M) {
	os.Setenv("THOR_BRANCH", "pedro/eth_eq_json_rpc")
	os.Exit(helper.RunTestMain(m, &nodeURL, nil))
}
