package eip1153

// EIP-1153 says transient storage is "discarded at the end of the transaction",
// but the specification was written for Ethereum, where a transaction is a
// single call. A VeChain transaction carries several clauses, each of which is
// its own top-level call, so "the end of the transaction" has two defensible
// readings:
//
//	shared     — one transient namespace spans every clause of the tx, which is
//	             the literal reading of the EIP;
//	per-clause — each clause starts with a clean namespace, treating a clause as
//	             the equivalent of an Ethereum transaction.
//
// The choice is a VeChain semantic decision, not something the EIP settles.
// These tests pin down whichever behaviour INTERSTELLAR actually implements, on
// both the mined path (a real multi-clause transaction) and the simulated path
// (InspectClauses with several clauses), so a silent change to either is caught.

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/tx"

	"github.com/vechain/interstellar-e2e/tests/eip1153/contracts/generated/transientprobe"
	"github.com/vechain/interstellar-e2e/tests/helper"
)

// multiClauseKey and multiClauseValue are shared by both directions of the test.
const (
	multiClauseKey   = 0x1153
	multiClauseValue = 0xABCDEF
)

// TestEIP1153_MultiClause_MinedTransaction sends one transaction whose first
// clause writes a transient slot and reads it straight back, and whose second
// clause reads the same slot again. Both clauses report through an event: a
// Thor receipt records events, not return data, so that is the only way to
// observe what a non-final clause saw.
//
// Clause 0 is the positive control. It must report the value it just wrote,
// which rules out the failure mode where clause 1's zero says nothing about
// transient storage because the write or the event path never worked at all.
func TestEIP1153_MultiClause_MinedTransaction(t *testing.T) {
	deployedProbes(t)

	client := helper.NewClient(nodeURL)

	writeAndReadData := transientprobe.Methods().WriteThenReadToEventMethod().MustPack(
		big.NewInt(multiClauseKey), big.NewInt(multiClauseValue))
	readData := transientprobe.Methods().ReadToEventMethod().MustPack(big.NewInt(multiClauseKey))

	trx := helper.BuildTx(t, client, 300_000,
		tx.NewClause(&probeA).WithData(writeAndReadData.Bytes()),
		tx.NewClause(&probeA).WithData(readData.Bytes()),
	)

	result, err := client.SendTransaction(trx)
	require.NoError(t, err, "multi-clause tx must be accepted by the txpool")

	receipt := helper.WaitForReceipt(t, client, result.ID, 60*time.Second)
	require.False(t, receipt.Reverted, "multi-clause tx must not revert")
	require.Len(t, receipt.Outputs, 2, "both clauses must produce an output")

	withinClause := findLoadedEvent(t, receipt.Outputs[0].Events)
	require.Equal(t, uint64(multiClauseValue), withinClause.Uint64(),
		"control: a TLOAD in the same clause as its TSTORE must return the stored value; "+
			"got 0x%x, so the clause-1 result below would be meaningless", withinClause.Uint64())

	acrossClauses := findLoadedEvent(t, receipt.Outputs[1].Events)
	assert.Equal(t, uint64(0), acrossClauses.Uint64(),
		"clause 1 read 0x%x from the slot clause 0 wrote — INTERSTELLAR clears transient "+
			"storage at each clause boundary, so a later clause must not observe an earlier "+
			"clause's transient writes", acrossClauses.Uint64())
}

// TestEIP1153_MultiClause_Simulated is the same experiment on the simulation
// path. It must agree with the mined path: a divergence between what
// InspectClauses predicts and what a mined transaction does would break every
// caller that uses simulation to estimate gas or preview a result.
func TestEIP1153_MultiClause_Simulated(t *testing.T) {
	deployedProbes(t)

	client := helper.NewClient(nodeURL)

	writeData := transientprobe.Methods().WriteMethod().MustPack(
		big.NewInt(multiClauseKey), big.NewInt(multiClauseValue))
	readData := transientprobe.Methods().ReadMethod().MustPack(big.NewInt(multiClauseKey))
	// Clause 2 is the positive control: it writes and reads the same slot inside
	// one clause, so it must return the value even though clause 1 does not.
	controlData := transientprobe.Methods().InnerWritePersistsMethod().MustPack(
		big.NewInt(multiClauseKey), big.NewInt(multiClauseValue))

	results, err := client.InspectClauses(&api.BatchCallData{
		Clauses: api.Clauses{
			{To: &probeA, Data: string(writeData)},
			{To: &probeA, Data: string(readData)},
			{To: &probeA, Data: string(controlData)},
		},
		Gas: 300_000,
	}, thorclient.Revision("best"))
	require.NoError(t, err)
	require.Len(t, results, 3)
	for i, r := range results {
		require.False(t, r.Reverted, "clause %d must not revert (vmError: %s)", i, r.VMError)
	}

	control := decodeWords(t, results[2].Data, 1)[0]
	require.Equal(t, uint64(multiClauseValue), control.Uint64(),
		"control: a write and read inside one simulated clause must return the stored value; "+
			"got 0x%x, so the clause-1 result below would be meaningless", control.Uint64())

	got := decodeWords(t, results[1].Data, 1)[0]
	assert.Equal(t, uint64(0), got.Uint64(),
		"simulated clause 1 read 0x%x, but the mined path clears transient storage between "+
			"clauses; simulation must predict the same result", got.Uint64())
}

// findLoadedEvent returns the value carried by the probe's Loaded event.
// The key is indexed, so it travels in topics[1] and the value is the whole of
// the data field.
func findLoadedEvent(t *testing.T, events []*api.Event) *big.Int {
	t.Helper()

	topic := transientprobe.Events().LoadedEventDecoder().Topic
	for _, ev := range events {
		if len(ev.Topics) < 2 || !strings.EqualFold(ev.Topics[0].String(), topic.String()) {
			continue
		}
		require.Equal(t, uint64(multiClauseKey), new(big.Int).SetBytes(ev.Topics[1][:]).Uint64(),
			"Loaded event must report the key that was read")

		raw, err := hex.DecodeString(strings.TrimPrefix(ev.Data, "0x"))
		require.NoError(t, err, "event data must be valid hex: %q", ev.Data)
		require.Len(t, raw, 32, "Loaded event must carry one 32-byte value")
		return new(big.Int).SetBytes(raw)
	}

	t.Fatalf("clause 1 emitted no Loaded event; got %d events", len(events))
	return nil
}
