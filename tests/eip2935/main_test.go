package eip2935

import (
	"os"
	"testing"

	"github.com/vechain/interstellar-e2e/tests/helper"
)

var nodeURL string

func TestMain(m *testing.M) {
	os.Setenv("THOR_BRANCH", "718bd161a70ba3ab7deadff6bce639ea95e2b546")
	os.Exit(helper.RunTestMain(m, &nodeURL, nil))
}
