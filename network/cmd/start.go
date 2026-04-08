package cmd

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	nhclient "github.com/vechain/networkhub/client"

	"github.com/vechain/interstellar-e2e/network/setup"
)

func Start() error {
	net := setup.BuildNetwork()

	c, err := nhclient.New(net)
	if err != nil {
		return fmt.Errorf("create network client: %w", err)
	}

	slog.Info("Starting network (this may take a while on first run — ThorBuilder clones and compiles thor)...")
	if err := c.Start(); err != nil {
		return fmt.Errorf("start network: %w", err)
	}

	slog.Info("Nodes started, waiting for block 1 and peer connectivity...")
	if err := net.HealthCheck(1, 3*time.Minute); err != nil {
		_ = c.Stop()
		return fmt.Errorf("network health check: %w", err)
	}

	urls := make([]string, len(net.Nodes))
	p2pPorts := make([]int, len(net.Nodes))
	for i, n := range net.Nodes {
		urls[i] = n.GetHTTPAddr()
		p2pPorts[i] = n.GetP2PListenPort()
	}

	// Write state file so that stop/status commands can find the process.
	state := networkState{PID: os.Getpid(), Nodes: urls, P2PPorts: p2pPorts}
	if data, err := json.Marshal(state); err == nil {
		_ = os.WriteFile(stateFilePath, data, 0o600)
	}

	// Emit a single JSON line to stdout — TestMain reads this to get node connection details.
	ready, _ := json.Marshal(struct {
		Nodes    []string `json:"nodes"`
		P2PPorts []int    `json:"p2pPorts"`
	}{
		Nodes:    urls,
		P2PPorts: p2pPorts,
	})
	fmt.Println(string(ready))

	slog.Info("Network ready", "nodes", urls)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	sig := <-sigCh
	slog.Info("Shutting down", "signal", sig)

	_ = c.Stop()
	_ = os.Remove(stateFilePath)
	return nil
}
