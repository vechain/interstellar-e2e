// Thin Go wrapper that launches the mocha + ethers.js v6 suite as part of
// `go test ./...`. The wrapper shares the network lifecycle with every other
// Go test package via helper.RunTestMain — when NODE_URL is exported (e.g. by
// `make test`), the existing network is reused; otherwise RunTestMain starts a
// fresh one.
//
// We invoke `npx mocha` directly (not `npm test`) to skip the `pretest` hook,
// which rebuilds /tmp/interstellar-network — that work is already done by
// `make build-network` or by RunTestMain.

package ethersjs

import (
	"os"
	"os/exec"
	"testing"

	"github.com/vechain/interstellar-e2e/tests/helper"
)

var nodeURL string

func TestMain(m *testing.M) {
	os.Setenv("THOR_BRANCH", "pedro/eth_eq_json_rpc")
	os.Exit(helper.RunTestMain(m, &nodeURL, nil))
}

func TestEthersJS(t *testing.T) {
	if _, err := os.Stat("node_modules"); os.IsNotExist(err) {
		t.Fatal("node_modules missing — run `make test` (auto-installs) or `npm ci` in tests/eth_rpc/ethersjs/")
	}

	cmd := exec.CommandContext(t.Context(), "npx", "mocha")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "NODE_URL="+nodeURL)
	if err := cmd.Run(); err != nil {
		t.Fatalf("ethersjs mocha suite failed: %v", err)
	}
}
