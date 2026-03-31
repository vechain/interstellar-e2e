package helper

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

const (
	networkBinary  = "/tmp/interstellar-network"
	startupTimeout = 15 * time.Minute // allows for ThorBuilder clone + compile on first run
)

// NetworkInfo holds node URLs emitted by the network binary on startup.
type NetworkInfo struct {
	Nodes []string `json:"nodes"`
}

// BuildNetworkBinary compiles the network/ module to a binary.
// If the binary already exists it is reused and the build is skipped.
func BuildNetworkBinary() error {
	root := findWorkspaceRoot()
	cmd := exec.Command("go", "build", "-o", networkBinary,
		"github.com/vechain/interstellar-e2e/network")
	cmd.Dir = root
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// StartNetwork runs the network binary and waits until it emits the ready JSON line.
// Returns the node info and a stop function that sends SIGTERM.
func StartNetwork() (NetworkInfo, func(), error) {
	cmd := exec.Command(networkBinary, "start")
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return NetworkInfo{}, nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return NetworkInfo{}, nil, fmt.Errorf("start binary: %w", err)
	}

	readyCh := make(chan NetworkInfo, 1)
	errCh := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Bytes()
			var info NetworkInfo
			if json.Unmarshal(line, &info) == nil && len(info.Nodes) > 0 {
				readyCh <- info
				return
			}
			// Non-JSON lines (e.g. git output from ThorBuilder) are forwarded to
			// stderr so they remain visible without blocking the ready-line scan.
			fmt.Fprintf(os.Stderr, "%s\n", line)
		}
		err := scanner.Err()
		if err == nil {
			err = fmt.Errorf("stdout closed before ready line (process may have exited early)")
		}
		errCh <- err
	}()

	select {
	case info := <-readyCh:
		stop := func() {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			_ = cmd.Wait()
		}
		return info, stop, nil
	case err := <-errCh:
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return NetworkInfo{}, nil, fmt.Errorf("network startup failed: %w", err)
	case <-time.After(startupTimeout):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return NetworkInfo{}, nil, fmt.Errorf("network did not start within %s", startupTimeout)
	}
}

// findWorkspaceRoot walks up from the current directory until it finds go.work.
func findWorkspaceRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
