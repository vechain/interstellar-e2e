package eip7934

import (
	"os"
	"testing"

	"github.com/vechain/interstellar-e2e/tests/helper"
)

var (
	nodeURL     string
	nodeP2PPort int
)

func TestMain(m *testing.M) {
	os.Exit(helper.RunTestMain(m, &nodeURL, &nodeP2PPort))
}
