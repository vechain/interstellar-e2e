package eip1153

// Shared fixtures for the multi-frame EIP-1153 tests.
//
// The probe contract lives in contracts/TransientProbe.sol; the Go binding is
// generated from it (cd tests/eip1153/contracts && go generate).

import (
	"encoding/hex"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/tx"

	"github.com/vechain/interstellar-e2e/tests/eip1153/contracts/generated/transientprobe"
	"github.com/vechain/interstellar-e2e/tests/helper"
)

// Two probe instances at two addresses own two independent transient
// namespaces. probeA is the contract under test; probeB is its counterparty —
// the CALL target, the DELEGATECALL implementation, and the re-entrancy relay.
var (
	probeOnce sync.Once
	probeA    thor.Address
	probeB    thor.Address
	probeErr  error
)

// deployedProbes deploys both instances once per test binary and reuses them.
// Deployment is a real transaction, so sharing it keeps the suite from paying a
// block wait per test; the tests themselves never write persistent state, and
// transient storage is discarded between transactions, so no test can observe
// another's leftovers.
func deployedProbes(t *testing.T) (thor.Address, thor.Address) {
	t.Helper()

	probeOnce.Do(func() {
		client := helper.NewClient(nodeURL)

		// Both instances are deployed by one two-clause transaction: two
		// creations, one block wait.
		initCode := transientprobe.Bytecode.Bytes()
		deployTx := helper.BuildTx(t, client, 3_000_000,
			tx.NewClause(nil).WithData(initCode),
			tx.NewClause(nil).WithData(initCode),
		)

		result, err := client.SendTransaction(deployTx)
		if err != nil {
			probeErr = err
			return
		}

		receipt := helper.WaitForReceipt(t, client, result.ID, 60*time.Second)
		require.False(t, receipt.Reverted, "TransientProbe deployment must not revert")
		require.Len(t, receipt.Outputs, 2, "both deployment clauses must produce an output")
		require.NotNil(t, receipt.Outputs[0].ContractAddress)
		require.NotNil(t, receipt.Outputs[1].ContractAddress)

		probeA = *receipt.Outputs[0].ContractAddress
		probeB = *receipt.Outputs[1].ContractAddress
	})

	require.NoError(t, probeErr, "TransientProbe deployment must be accepted by the node")
	require.NotEqual(t, probeA, probeB, "the two probes must be distinct contracts")
	return probeA, probeB
}

// callProbe simulates a single call to `to` and returns the result. It runs at
// the "best" revision rather than helper.PostForkRevision: the probes are
// deployed by a transaction mined well after block 1, so they are not in state
// at the fork block. Everything under test here is post-fork behaviour, and
// "best" is always post-fork.
func callProbe(t *testing.T, to thor.Address, data transientprobe.HexData, gas uint64) *api.CallResult {
	t.Helper()

	client := helper.NewClient(nodeURL)
	results, err := client.InspectClauses(&api.BatchCallData{
		Clauses: api.Clauses{{To: &to, Data: string(data)}},
		Gas:     gas,
	}, thorclient.Revision("best"))
	require.NoError(t, err)
	require.Len(t, results, 1)
	return results[0]
}

// probeAddr converts a thor.Address into the address type the generated binding
// encodes.
func probeAddr(a thor.Address) transientprobe.Address {
	return transientprobe.Address(a)
}

// decodeWords splits ABI return data into exactly n 32-byte words. Every probe
// method returns a fixed number of uint256/bool values, so a head-only decode
// is sufficient and avoids depending on decoder helpers the generator does not
// emit for return values.
func decodeWords(t *testing.T, data string, n int) []*big.Int {
	t.Helper()

	raw, err := hex.DecodeString(strings.TrimPrefix(data, "0x"))
	require.NoError(t, err, "return data must be valid hex: %q", data)
	require.Len(t, raw, n*32, "expected %d return words, got %d bytes: %q", n, len(raw), data)

	words := make([]*big.Int, n)
	for i := range words {
		words[i] = new(big.Int).SetBytes(raw[i*32 : (i+1)*32])
	}
	return words
}
