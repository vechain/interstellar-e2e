package helper

import (
	"log"
	"os"
	"testing"
)

// RunTestMain manages the test network lifecycle for a test package's TestMain.
// If NODE_URL is set, it connects to the already-running network at that address.
// Otherwise it builds the network binary, starts a fresh network, and stops it
// after tests complete.
//
// Usage:
//
//	var nodeURL string
//
//	func TestMain(m *testing.M) {
//	    os.Exit(helper.RunTestMain(m, &nodeURL))
//	}
func RunTestMain(m *testing.M, nodeURL *string) int {
	if url := os.Getenv("NODE_URL"); url != "" {
		*nodeURL = url
		return m.Run()
	}

	if err := BuildNetworkBinary(); err != nil {
		log.Fatalf("build network binary: %v", err)
	}

	info, stop, err := StartNetwork()
	if err != nil {
		log.Fatalf("start network: %v", err)
	}
	defer stop()

	*nodeURL = info.Nodes[0]
	log.Printf("network ready — node: %s", *nodeURL)

	return m.Run()
}
