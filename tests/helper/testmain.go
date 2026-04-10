package helper

import (
	"fmt"
	"log"
	"os"
	"strconv"
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
//	var nodeP2PPort int
//
//	func TestMain(m *testing.M) {
//	    os.Exit(helper.RunTestMain(m, &nodeURL, &nodeP2PPort))
//	}
func RunTestMain(m *testing.M, nodeURL *string, nodeP2PPort *int) int {
	if url := os.Getenv("NODE_URL"); url != "" {
		*nodeURL = url
		if nodeP2PPort != nil {
			port, err := resolveNodeP2PPort(url)
			if err != nil {
				log.Fatal(err)
			}
			*nodeP2PPort = port
		}
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
	if nodeP2PPort != nil {
		if len(info.P2PPorts) == 0 {
			log.Fatal("network did not expose any P2P ports")
		}
		*nodeP2PPort = info.P2PPorts[0]
		log.Printf("network ready — node: %s, p2p: %d", *nodeURL, *nodeP2PPort)
	} else {
		log.Printf("network ready — node: %s", *nodeURL)
	}

	return m.Run()
}

func resolveNodeP2PPort(nodeURL string) (int, error) {
	if port := os.Getenv("NODE_P2P_PORT"); port != "" {
		parsedPort, err := strconv.Atoi(port)
		if err != nil {
			return 0, fmt.Errorf("parse NODE_P2P_PORT: %w", err)
		}
		return parsedPort, nil
	}

	return 0, fmt.Errorf("could not determine P2P port for %s; set NODE_P2P_PORT or start the network via the helper", nodeURL)
}
