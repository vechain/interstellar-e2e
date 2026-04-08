package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

const nodeURLTimeout = 20 * time.Minute

// NodeURL blocks until the network is ready and prints the first node URL to stdout.
// It polls the state file and pings the node until both are reachable, making it
// safe to call immediately after starting the network binary in the background.
func NodeURL() error {
	deadline := time.Now().Add(nodeURLTimeout)
	for time.Now().Before(deadline) {
		if url, _, ok := tryNodeInfo(); ok {
			fmt.Println(url)
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out after %s waiting for network to become ready", nodeURLTimeout)
}

// NodeP2PPort blocks until the network is ready and prints the first node's
// P2P listen port to stdout.
func NodeP2PPort() error {
	deadline := time.Now().Add(nodeURLTimeout)
	for time.Now().Before(deadline) {
		_, p2pPort, ok := tryNodeInfo()
		if ok && p2pPort != 0 {
			fmt.Println(p2pPort)
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out after %s waiting for network to become ready", nodeURLTimeout)
}

func tryNodeInfo() (string, int, bool) {
	data, err := os.ReadFile(stateFilePath)
	if err != nil {
		return "", 0, false
	}
	var state networkState
	if err := json.Unmarshal(data, &state); err != nil || len(state.Nodes) == 0 {
		return "", 0, false
	}
	resp, err := http.Get(state.Nodes[0] + "/blocks/best") //nolint:noctx
	if err != nil || resp.StatusCode != http.StatusOK {
		return "", 0, false
	}
	resp.Body.Close()

	var p2pPort int
	if len(state.P2PPorts) > 0 {
		p2pPort = state.P2PPorts[0]
	}

	return state.Nodes[0], p2pPort, true
}
