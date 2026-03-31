package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"syscall"
)

func Stop() error {
	data, err := os.ReadFile(stateFilePath)
	if err != nil {
		return fmt.Errorf("no running network found (state file missing): %w", err)
	}

	var state networkState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("parse state file: %w", err)
	}

	proc, err := os.FindProcess(state.PID)
	if err != nil {
		return fmt.Errorf("find process %d: %w", state.PID, err)
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("signal process %d: %w", state.PID, err)
	}

	fmt.Printf("Sent SIGTERM to process %d\n", state.PID)
	return nil
}
