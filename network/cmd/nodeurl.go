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
		if url, ok := tryNodeURL(); ok {
			fmt.Println(url)
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out after %s waiting for network to become ready", nodeURLTimeout)
}

func tryNodeURL() (string, bool) {
	data, err := os.ReadFile(stateFilePath)
	if err != nil {
		return "", false
	}
	var state networkState
	if err := json.Unmarshal(data, &state); err != nil || len(state.Nodes) == 0 {
		return "", false
	}
	resp, err := http.Get(state.Nodes[0] + "/blocks/best") //nolint:noctx
	if err != nil || resp.StatusCode != http.StatusOK {
		return "", false
	}
	resp.Body.Close()
	return state.Nodes[0], true
}
