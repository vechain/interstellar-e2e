package main

import (
	"fmt"
	"os"

	"github.com/vechain/interstellar-e2e/network/cmd"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: interstellar-network <start|stop|status|node-url|node-p2p-port>")
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "start":
		err = cmd.Start()
	case "stop":
		err = cmd.Stop()
	case "status":
		err = cmd.Status()
	case "node-url":
		err = cmd.NodeURL()
	case "node-p2p-port":
		err = cmd.NodeP2PPort()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
