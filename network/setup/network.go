package setup

import (
	"os"

	"github.com/vechain/networkhub/network"
	"github.com/vechain/networkhub/preset"
	"github.com/vechain/networkhub/thorbuilder"
	"github.com/vechain/thor/v2/log"
)

const (
	defaultThorRepo = "https://github.com/vechain/thor"
	// defaultThorBranch points to the branch containing EIP-6780 (opSuicide6780).
	// Update to "evm-upgrades" once vechain/thor#1590 is merged.
	defaultThorBranch = "moglu/eip6780"
)

// BuildNetwork constructs the 3-node network configuration for interstellar testing.
// The Thor repo and branch can be overridden via THOR_REPO and THOR_BRANCH env vars.
func BuildNetwork() *network.Network {
	repo := os.Getenv("THOR_REPO")
	if repo == "" {
		repo = defaultThorRepo
	}
	branch := os.Getenv("THOR_BRANCH")
	if branch == "" {
		branch = defaultThorBranch
	}

	downloadCfg := &thorbuilder.DownloadConfig{
		RepoUrl:    repo,
		Branch:     branch,
		IsReusable: true,
	}
	buildCfg := &thorbuilder.BuildConfig{
		ReuseBinary: true,
	}

	existingPath := os.Getenv("THOR_EXISTING_PATH")
	if existingPath != "" {
		downloadCfg = nil
		buildCfg.ExistingPath = existingPath
		log.Info("Using thor existing path: ", existingPath)
	}

	cfg := preset.LocalThreeNodesNetwork()

	// Modify the genesis, then explicitly apply it to every node via SetGenesis.
	// LocalThreeNodesNetwork currently shares a single *CustomGenesis pointer, but
	// we call SetGenesis on each node so this stays correct if that ever changes.
	gen := cfg.Nodes[0].GetGenesis()
	// Raise block gas limit above MaxTxGasLimit (1<<24) so EIP-7825 boundary
	// tests can verify at-limit transactions are both accepted and includable.
	gen.GasLimit = 40_000_000
	// Activate the INTERSTELLAR fork from block 1, leaving block 0 as a
	// pre-fork state that tests can simulate against via InspectClauses Revision("0").
	gen.ForkConfig.AddField("INTERSTELLAR", 1) //nolint:errcheck
	for _, n := range cfg.Nodes {
		n.SetGenesis(gen)
	}

	cfg.ThorBuilder = &thorbuilder.Config{
		DownloadConfig: downloadCfg,
		BuildConfig:    buildCfg,
	}

	return cfg
}
