package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

func Status() error {
	data, err := os.ReadFile(stateFilePath)
	if err != nil {
		fmt.Println("No running network found")
		return nil
	}

	var state networkState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("parse state file: %w", err)
	}

	fmt.Printf("Network PID: %d\n", state.PID)
	for i, url := range state.Nodes {
		health := "healthy"
		resp, err := http.Get(url + "/blocks/best") //nolint:noctx
		if err != nil || resp.StatusCode != http.StatusOK {
			health = "unreachable"
		}
		if resp != nil {
			resp.Body.Close()
		}
		fmt.Printf("  node%d: %s [%s]\n", i+1, url, health)
	}
	return nil
}
