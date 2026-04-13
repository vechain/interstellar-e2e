package eip1153

// EIP-1153 (Transient Storage) adds two opcodes active at the INTERSTELLAR fork:
//   TLOAD  (0x5C): load a value from the call's transient storage slot
//   TSTORE (0x5D): store a value into the call's transient storage slot
//
// Transient storage is zeroed at the start of every transaction.
// Pre-fork: both opcodes are invalid; the EVM reverts.

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/tx"

	"github.com/vechain/interstellar-e2e/tests/helper"
)

// tstoreTloadRoundtripBytecode stores 0xDEADBEEF in transient slot 0 via TSTORE,
// then loads it back via TLOAD and returns the 32-byte result.
// Used as init-code (nil To clause), so RETURN delivers data as CallResult.Data.
//
//  1. TSTORE 0xDEADBEEF at transient slot 0
//  2. TLOAD transient slot 0
//  3. RETURN 32 bytes from memory offset 0
//
// Expected post-fork output: 32-byte word ending with 0xDEADBEEF.
// Pre-fork: 0x5D (TSTORE) is an invalid opcode; the EVM reverts.
var tstoreTloadRoundtripBytecode = []byte{
	0x63, 0xDE, 0xAD, 0xBE, 0xEF, // PUSH4 0xDEADBEEF   (value to store)
	0x60, 0x00, // PUSH1 0x00          (transient slot key = 0)
	0x5D,       // TSTORE              transient[0] = 0xDEADBEEF
	0x60, 0x00, // PUSH1 0x00          (transient slot key = 0)
	0x5C,       // TLOAD               stack[top] = transient[0]
	0x60, 0x00, // PUSH1 0x00          (MSTORE offset)
	0x52,       // MSTORE              mem[0:32] = value
	0x60, 0x20, // PUSH1 0x20          (RETURN size = 32)
	0x60, 0x00, // PUSH1 0x00          (RETURN offset = 0)
	0xF3, // RETURN
}

func TestTSTORE_TLOAD_Roundtrip(t *testing.T) {
	client := helper.NewClient(nodeURL)
	callData := &api.BatchCallData{
		Clauses: api.Clauses{
			// nil To = contract-creation simulation; bytecode runs as init code.
			{Data: "0x" + hex.EncodeToString(tstoreTloadRoundtripBytecode)},
		},
		Gas: 100_000,
	}

	t.Run("pre-fork", func(t *testing.T) {
		// 0x5D (TSTORE) is not in the pre-INTERSTELLAR instruction set.
		// The EVM treats it as an invalid opcode and reverts.
		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PreForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.True(t, results[0].Reverted,
			"TSTORE must revert before INTERSTELLAR (invalid opcode)")
	})

	t.Run("post-fork", func(t *testing.T) {
		// TSTORE and TLOAD are part of the INTERSTELLAR instruction set.
		// The roundtrip should succeed and return the stored value.
		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PostForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Reverted,
			"TSTORE/TLOAD must not revert after INTERSTELLAR (vmError: %s)", results[0].VMError)
		assert.True(t, strings.HasSuffix(strings.TrimPrefix(results[0].Data, "0x"), "deadbeef"),
			"expected output ending in deadbeef, got: %s", results[0].Data)
	})
}

// tloadUninitializedBytecode loads transient slot 0 without any prior TSTORE.
// EIP-1153: uninitialized transient storage slots are zero-valued.
//
// Expected post-fork output: 32 bytes of zeros.
// Pre-fork: 0x5C (TLOAD) is an invalid opcode; the EVM reverts.
var tloadUninitializedBytecode = []byte{
	0x60, 0x00, // PUSH1 0x00   (transient slot key = 0)
	0x5C,       // TLOAD        stack[top] = transient[0]  (== 0, zero-initialised)
	0x60, 0x00, // PUSH1 0x00   (MSTORE offset)
	0x52,       // MSTORE       mem[0:32] = 0
	0x60, 0x20, // PUSH1 0x20   (RETURN size = 32)
	0x60, 0x00, // PUSH1 0x00   (RETURN offset = 0)
	0xF3, // RETURN
}

func TestTLOAD_UninitializedSlot(t *testing.T) {
	client := helper.NewClient(nodeURL)
	callData := &api.BatchCallData{
		Clauses: api.Clauses{
			{Data: "0x" + hex.EncodeToString(tloadUninitializedBytecode)},
		},
		Gas: 100_000,
	}

	t.Run("pre-fork", func(t *testing.T) {
		// 0x5C (TLOAD) is an invalid opcode before INTERSTELLAR.
		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PreForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.True(t, results[0].Reverted,
			"TLOAD must revert before INTERSTELLAR (invalid opcode)")
	})

	t.Run("post-fork", func(t *testing.T) {
		// Uninitialized transient slot must read as zero (EIP-1153 §Semantics).
		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PostForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Reverted,
			"TLOAD on uninitialized slot must not revert (vmError: %s)", results[0].VMError)
		// Output must be 32 zero bytes: "0x" followed by 64 hex zeros.
		trimmed := strings.TrimPrefix(results[0].Data, "0x")
		assert.Equal(t, strings.Repeat("0", 64), trimmed,
			"TLOAD on uninitialized slot must return 32 zero bytes, got: %s", results[0].Data)
	})
}

// tstoreOnlyBytecode measures the gas cost of a single TSTORE.
// Sequence: PUSH4 value → PUSH1 key → TSTORE → STOP
// Expected gas: PUSH4(3) + PUSH1(3) + TSTORE(100) + STOP(0) = 106
var tstoreOnlyBytecode = []byte{
	0x63, 0xDE, 0xAD, 0xBE, 0xEF, // PUSH4 0xDEADBEEF   (value)
	0x60, 0x00, // PUSH1 0x00          (transient slot key)
	0x5D, // TSTORE
	0x00, // STOP
}

// tloadOnlyBytecode measures the gas cost of a single TLOAD.
// Uses STOP (not RETURN) to avoid the 200-gas/byte code-deposit charge that
// init-code (nil-To clause) incurs when RETURN delivers non-empty bytecode.
// Sequence: PUSH1 key → TLOAD → STOP
// Expected gas: PUSH1(3) + TLOAD(100) + STOP(0) = 103
var tloadOnlyBytecode = []byte{
	0x60, 0x00, // PUSH1 0x00   (transient slot key = 0)
	0x5C, // TLOAD        stack[top] = transient[0] (value left on stack; STOP ignores it)
	0x00, // STOP
}

func TestTSTORE_TLOAD_GasCost(t *testing.T) {
	client := helper.NewClient(nodeURL)

	t.Run("TSTORE costs 100 gas", func(t *testing.T) {
		callData := &api.BatchCallData{
			Clauses: api.Clauses{
				{Data: "0x" + hex.EncodeToString(tstoreOnlyBytecode)},
			},
			Gas: 100_000,
		}
		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PostForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Reverted, "TSTORE gas bytecode must not revert: %s", results[0].VMError)
		// PUSH4(3) + PUSH1(3) + TSTORE(100) + STOP(0) = 106
		assert.Equal(t, uint64(106), results[0].GasUsed,
			"TSTORE opcode must cost 100 gas (total 106 with surrounding pushes)")
	})

	t.Run("TLOAD costs 100 gas", func(t *testing.T) {
		callData := &api.BatchCallData{
			Clauses: api.Clauses{
				{Data: "0x" + hex.EncodeToString(tloadOnlyBytecode)},
			},
			Gas: 100_000,
		}
		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PostForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Reverted, "TLOAD gas bytecode must not revert: %s", results[0].VMError)
		// PUSH1(3) + TLOAD(100) + STOP(0) = 103
		assert.Equal(t, uint64(103), results[0].GasUsed,
			"TLOAD opcode must cost 100 gas (total 103 with surrounding instructions)")
	})
}

// tstoreDoesNotPolluteSloadBytecode stores 0x42 into transient slot 0 via TSTORE,
// then reads slot 0 via SLOAD (persistent storage) and returns the result.
//
// EIP-1153: TSTORE and SLOAD operate on separate namespaces.
// SLOAD must return 0 even though TSTORE was just called with the same key.
//
// Expected post-fork output: 32 bytes of zeros (SLOAD returns 0).
// Pre-fork: 0x5D (TSTORE) is an invalid opcode; the EVM reverts.
var tstoreDoesNotPolluteSloadBytecode = []byte{
	0x60, 0x42, // PUSH1 0x42   (value to store transiently)
	0x60, 0x00, // PUSH1 0x00   (transient slot key = 0)
	0x5D,       // TSTORE       transient[0] = 0x42
	0x60, 0x00, // PUSH1 0x00   (persistent storage slot = 0)
	0x54,       // SLOAD        persistent_storage[0] → must be 0 (not 0x42)
	0x60, 0x00, // PUSH1 0x00   (MSTORE offset)
	0x52,       // MSTORE       mem[0:32] = sload_result
	0x60, 0x20, // PUSH1 0x20   (RETURN size = 32)
	0x60, 0x00, // PUSH1 0x00   (RETURN offset = 0)
	0xF3, // RETURN
}

func TestTransientStorage_DoesNotPersistToSLOAD(t *testing.T) {
	client := helper.NewClient(nodeURL)
	callData := &api.BatchCallData{
		Clauses: api.Clauses{
			{Data: "0x" + hex.EncodeToString(tstoreDoesNotPolluteSloadBytecode)},
		},
		Gas: 100_000,
	}

	t.Run("pre-fork", func(t *testing.T) {
		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PreForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.True(t, results[0].Reverted,
			"TSTORE must revert before INTERSTELLAR (invalid opcode)")
	})

	t.Run("post-fork", func(t *testing.T) {
		// TSTORE at slot 0 must NOT affect SLOAD at slot 0.
		// The two opcodes access completely separate storage namespaces.
		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PostForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Reverted,
			"TSTORE+SLOAD must not revert after INTERSTELLAR (vmError: %s)", results[0].VMError)
		trimmed := strings.TrimPrefix(results[0].Data, "0x")
		assert.Equal(t, strings.Repeat("0", 64), trimmed,
			"SLOAD after TSTORE at the same key must return 0 (separate namespace), got: %s", results[0].Data)
	})
}

// transientIsolationRuntime is the runtime bytecode for a helper contract that:
//   - empty calldata  → TSTORE 0xCAFE at slot 0, STOP
//   - any calldata    → TLOAD slot 0, return 32-byte result
//
// Byte layout (dispatcher at bytes 0–4, TLOAD handler 5–15, TSTORE handler 16–23):
var transientIsolationRuntime = []byte{
	// dispatcher
	0x36,       // CALLDATASIZE
	0x15,       // ISZERO
	0x60, 0x10, // PUSH1 0x10   (TSTORE JUMPDEST at byte 16)
	0x57, // JUMPI
	// TLOAD handler (bytes 5–15)
	0x60, 0x00, // PUSH1 0x00   (key = slot 0)
	0x5C,       // TLOAD
	0x60, 0x00, // PUSH1 0x00   (MSTORE offset)
	0x52,       // MSTORE
	0x60, 0x20, // PUSH1 0x20   (return size = 32)
	0x60, 0x00, // PUSH1 0x00   (return offset = 0)
	0xF3, // RETURN
	// TSTORE handler (bytes 16–23)
	0x5B,             // JUMPDEST  (byte 16 == 0x10 ✓)
	0x61, 0xCA, 0xFE, // PUSH2 0xCAFE  (value)
	0x60, 0x00, // PUSH1 0x00    (key = slot 0)
	0x5D, // TSTORE
	0x00, // STOP
}

// makeTransientIsolationInitCode wraps transientIsolationRuntime in standard EVM
// deployer init code. The 12-byte header places the runtime at offset 0x0C.
func makeTransientIsolationInitCode() []byte {
	n := byte(len(transientIsolationRuntime)) // 24 bytes
	header := []byte{
		0x60, n, // PUSH1 n        (runtime length)
		0x60, 0x0C, // PUSH1 0x0C     (runtime starts at byte 12)
		0x60, 0x00, // PUSH1 0x00     (memory destination)
		0x39,    // CODECOPY
		0x60, n, // PUSH1 n        (return length)
		0x60, 0x00, // PUSH1 0x00     (return offset)
		0xF3, // RETURN
	}
	return append(header, transientIsolationRuntime...)
}

func TestTransientStorage_ClearedBetweenTransactions(t *testing.T) {
	client := helper.NewClient(nodeURL)

	// Step 1: Deploy the helper contract.
	initCode := makeTransientIsolationInitCode()
	deployClause := tx.NewClause(nil).WithData(initCode)
	deployTx := helper.BuildTx(t, client, 500_000, deployClause)
	deployResult, err := client.SendTransaction(deployTx)
	require.NoError(t, err, "contract deployment tx must be accepted")

	deployReceipt := helper.WaitForReceipt(t, client, deployResult.ID, 30*time.Second)
	require.False(t, deployReceipt.Reverted, "contract deployment must not revert")
	require.NotEmpty(t, deployReceipt.Outputs, "deployment receipt must have outputs")
	require.NotNil(t, deployReceipt.Outputs[0].ContractAddress,
		"deployment output must contain a contract address")

	contractAddr := *deployReceipt.Outputs[0].ContractAddress

	// Step 2: Call the TSTORE path (empty calldata) in a real transaction.
	tstoreClause := tx.NewClause(&contractAddr).WithData([]byte{})
	tstoreTx := helper.BuildTx(t, client, 100_000, tstoreClause)
	tstoreResult, err := client.SendTransaction(tstoreTx)
	require.NoError(t, err, "TSTORE tx must be accepted by the txpool")

	tstoreReceipt := helper.WaitForReceipt(t, client, tstoreResult.ID, 30*time.Second)
	require.False(t, tstoreReceipt.Reverted, "TSTORE tx must execute without reverting")

	// Step 3: InspectClauses calls the TLOAD path as a NEW simulated transaction.
	// The transient slot must be 0 — cleared at the end of the TSTORE transaction.
	tloadCallData := &api.BatchCallData{
		Clauses: api.Clauses{
			{To: &contractAddr, Data: "0x01"}, // non-empty data → TLOAD path
		},
		Gas: 100_000,
	}
	results, err := client.InspectClauses(tloadCallData, thorclient.Revision(tstoreReceipt.Meta.BlockID.String()))
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].Reverted,
		"TLOAD call must not revert (vmError: %s)", results[0].VMError)
	trimmed := strings.TrimPrefix(results[0].Data, "0x")
	assert.Equal(t, strings.Repeat("0", 64), trimmed,
		"transient storage must be 0 in a new transaction — TSTORE from prior tx must not persist; got: %s",
		results[0].Data)
}
