package cmd

const stateFilePath = "/tmp/interstellar-network.json"

type networkState struct {
	PID   int      `json:"pid"`
	Nodes []string `json:"nodes"`
}
