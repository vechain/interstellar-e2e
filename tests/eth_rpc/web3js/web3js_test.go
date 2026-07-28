// Thin Go wrapper that launches the mocha + web3.js v4 suite as part of
// `go test ./...`. The wrapper shares the network lifecycle with every other
// Go test package via helper.RunTestMain — when NODE_URL is exported (e.g. by
// `make test`), the existing network is reused; otherwise RunTestMain starts a
// fresh one.
//
// We invoke `npx mocha` directly (not `npm test`) to skip any pretest hook,
// mirroring the ethersjs wrapper.

package web3js

import (
	"os"
	"os/exec"
	"testing"

	"github.com/vechain/interstellar-e2e/tests/helper"
)

var nodeURL string

func TestMain(m *testing.M) {
	os.Exit(helper.RunTestMain(m, &nodeURL, nil))
}

func TestWeb3JS(t *testing.T) {
	if _, err := os.Stat("node_modules"); os.IsNotExist(err) {
		t.Fatal("node_modules missing — run `make test` (auto-installs) or `npm ci` in tests/eth_rpc/web3js/")
	}

	cmd := exec.CommandContext(t.Context(), "npx", "mocha")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "NODE_URL="+nodeURL)
	if err := cmd.Run(); err != nil {
		t.Fatalf("web3js mocha suite failed: %v", err)
	}
}
