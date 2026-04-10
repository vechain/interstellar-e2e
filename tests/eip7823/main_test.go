package eip7823

import (
	"os"
	"testing"

	"github.com/vechain/interstellar-e2e/tests/helper"
)

var nodeURL string

func TestMain(m *testing.M) {
	os.Exit(helper.RunTestMain(m, &nodeURL, nil))
}
