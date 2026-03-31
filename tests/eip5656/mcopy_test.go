package eip5656

// EIP-5656 (MCOPY) adds opcode 0x5e for in-memory copying, active at the INTERSTELLAR fork.
//
// The bytecode below:
//  1. MSTORE 0xDEADBEEF at offset 0  (right-aligned in a 32-byte word)
//  2. MCOPY 32 bytes from offset 0 to offset 32
//  3. RETURN 32 bytes from offset 32
//
// Expected post-fork output: 32 bytes ending with 0xDEADBEEF.
// Pre-fork: 0x5e is an invalid opcode; the EVM reverts.

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

// mcopyBytecode is init-code that uses MCOPY (0x5e) to copy memory then returns the result.
// Running it via InspectClauses (To=nil = contract creation simulation) returns the data
// passed to RETURN as the CallResult.Data.
var mcopyBytecode = []byte{
	0x63, 0xDE, 0xAD, 0xBE, 0xEF, // PUSH4 0xDEADBEEF
	0x60, 0x00, // PUSH1 0x00       (MSTORE offset)
	0x52,       // MSTORE           mem[0:32] = 0x000...0DEADBEEF
	0x60, 0x20, // PUSH1 0x20       (MCOPY length = 32)
	0x60, 0x00, // PUSH1 0x00       (MCOPY src = 0)
	0x60, 0x20, // PUSH1 0x20       (MCOPY dst = 32)
	0x5e,       // MCOPY            mem[32:64] = mem[0:32]
	0x60, 0x20, // PUSH1 0x20       (RETURN size = 32)
	0x60, 0x20, // PUSH1 0x20       (RETURN offset = 32)
	0xf3, // RETURN
}

func TestMCOPY_OpcodeExecutes(t *testing.T) {
	client := helper.NewClient(nodeURL)
	callData := &api.BatchCallData{
		Clauses: api.Clauses{
			// nil To = contract creation simulation; bytecode runs as init code.
			{Data: "0x" + hex.EncodeToString(mcopyBytecode)},
		},
		Gas: 100_000,
	}

	t.Run("pre-fork", func(t *testing.T) {
		// 0x5e is not in the pre-Osaka instruction set; the EVM treats it as an
		// invalid opcode and halts with a revert.
		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PreForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.True(t, results[0].Reverted,
			"MCOPY must revert before INTERSTELLAR (invalid opcode)")
	})

	t.Run("post-fork", func(t *testing.T) {
		// MCOPY is part of the Osaka instruction set; bytecode executes successfully
		// and RETURN delivers 32 bytes ending with 0xDEADBEEF.
		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PostForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Reverted,
			"MCOPY must not revert after INTERSTELLAR (vmError: %s)", results[0].VMError)
		assert.True(t, strings.HasSuffix(strings.TrimPrefix(results[0].Data, "0x"), "deadbeef"),
			"expected output ending in deadbeef, got: %s", results[0].Data)
	})
}
