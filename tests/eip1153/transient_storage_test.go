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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/thorclient"

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
	0x60, 0x00,                     // PUSH1 0x00          (transient slot key = 0)
	0x5D,                           // TSTORE              transient[0] = 0xDEADBEEF
	0x60, 0x00,                     // PUSH1 0x00          (transient slot key = 0)
	0x5C,                           // TLOAD               stack[top] = transient[0]
	0x60, 0x00,                     // PUSH1 0x00          (MSTORE offset)
	0x52,                           // MSTORE              mem[0:32] = value
	0x60, 0x20,                     // PUSH1 0x20          (RETURN size = 32)
	0x60, 0x00,                     // PUSH1 0x00          (RETURN offset = 0)
	0xF3,                           // RETURN
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
