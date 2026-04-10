package eip7823

// EIP-7823 introduces an upper bound of 1024 bytes for the base, exponent, and
// modulus length parameters of the MODEXP precompile (address 0x05).
//
// Pre-fork: no upper-bound check; the same inputs succeed as long as enough gas
// is supplied.
//
// Post-fork (INTERSTELLAR): any declared length > 1024 — or a length field that
// overflows uint64 — causes the precompile to revert with
//   "one or more of base/exponent/modulus length exceeded 1024 bytes"

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"

	"github.com/vechain/interstellar-e2e/tests/helper"
)

var modExpAddr = thor.BytesToAddress([]byte{5})

// encodeModExpInput encodes inputs for the MODEXP precompile.
// Wire format: [baseLen (32 B)][expLen (32 B)][modLen (32 B)][base][exp][mod]
func encodeModExpInput(base, exp, mod []byte) []byte {
	buf := make([]byte, 96+len(base)+len(exp)+len(mod))
	new(big.Int).SetUint64(uint64(len(base))).FillBytes(buf[0:32])
	new(big.Int).SetUint64(uint64(len(exp))).FillBytes(buf[32:64])
	new(big.Int).SetUint64(uint64(len(mod))).FillBytes(buf[64:96])
	copy(buf[96:], base)
	copy(buf[96+len(base):], exp)
	copy(buf[96+len(base)+len(exp):], mod)
	return buf
}

// encodeModExpOverflowInput builds a raw MODEXP input whose baseLen field
// exceeds uint64 (2^64 + 1), with expLen=1 and modLen=1.
func encodeModExpOverflowInput() []byte {
	buf := make([]byte, 96+3)
	// baseLen = 2^64 + 1  →  32-byte big-endian with bit 64 set plus 1
	buf[23] = 0x01
	buf[31] = 0x01
	// expLen = 1
	buf[63] = 0x01
	// modLen = 1
	buf[95] = 0x01
	// minimal data: base=0x01, exp=0x01, mod=0x03
	buf[96] = 0x01
	buf[97] = 0x01
	buf[98] = 0x03
	return buf
}

func TestEIP7823_WithinBound(t *testing.T) {
	client := helper.NewClient(nodeURL)

	base := make([]byte, 32)
	base[31] = 0x02     // 2
	exp := []byte{0x0a} // 10
	mod := make([]byte, 32)
	mod[30] = 0x03
	mod[31] = 0xe8 // 1000

	input := encodeModExpInput(base, exp, mod)
	callData := &api.BatchCallData{
		Clauses: api.Clauses{{To: &modExpAddr, Data: "0x" + hex.EncodeToString(input)}},
		Gas:     100_000,
	}

	t.Run("pre-fork", func(t *testing.T) {
		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PreForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Reverted,
			"small modexp must not revert pre-fork: %s", results[0].VMError)
	})

	t.Run("post-fork", func(t *testing.T) {
		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PostForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Reverted,
			"small modexp must not revert post-fork: %s", results[0].VMError)
	})
}

func TestEIP7823_ExactBoundary(t *testing.T) {
	client := helper.NewClient(nodeURL)

	base := make([]byte, 1024)
	base[0] = 0x01
	exp := []byte{0x01}
	mod := []byte{0x03}

	input := encodeModExpInput(base, exp, mod)
	callData := &api.BatchCallData{
		Clauses: api.Clauses{{To: &modExpAddr, Data: "0x" + hex.EncodeToString(input)}},
		Gas:     100_000,
	}

	results, err := client.InspectClauses(callData, thorclient.Revision(helper.PostForkRevision))
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].Reverted,
		"1024-byte base is at the limit and must not revert post-fork: %s", results[0].VMError)
}

func TestEIP7823_ExceedsBound_PostFork(t *testing.T) {
	client := helper.NewClient(nodeURL)

	tests := []struct {
		name    string
		baseLen int
		expLen  int
		modLen  int
		gas     uint64
	}{
		{"base_exceeds_1024", 1025, 1, 1, 100_000},
		{"exp_exceeds_1024", 1, 1025, 1, 500_000},
		{"mod_exceeds_1024", 1, 1, 1025, 100_000},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := make([]byte, tc.baseLen)
			base[0] = 0x01
			exp := make([]byte, tc.expLen)
			exp[0] = 0x01
			mod := make([]byte, tc.modLen)
			mod[0] = 0x03

			input := encodeModExpInput(base, exp, mod)
			callData := &api.BatchCallData{
				Clauses: api.Clauses{{To: &modExpAddr, Data: "0x" + hex.EncodeToString(input)}},
				Gas:     tc.gas,
			}

			results, err := client.InspectClauses(callData, thorclient.Revision(helper.PostForkRevision))
			require.NoError(t, err)
			require.Len(t, results, 1)
			assert.True(t, results[0].Reverted,
				"modexp with lengths exceeding 1024 must revert post-fork")
			assert.Contains(t, results[0].VMError, "exceeded 1024",
				"vmError should mention the 1024-byte limit")
		})
	}

	// When all three lengths exceed 1024, RequiredGas (~273M) exceeds the
	// node's gas limit so Run is never reached — the call still reverts.
	t.Run("all_exceed_1024", func(t *testing.T) {
		base := make([]byte, 1025)
		base[0] = 0x01
		exp := make([]byte, 1025)
		exp[0] = 0x01
		mod := make([]byte, 1025)
		mod[0] = 0x03

		input := encodeModExpInput(base, exp, mod)
		callData := &api.BatchCallData{
			Clauses: api.Clauses{{To: &modExpAddr, Data: "0x" + hex.EncodeToString(input)}},
			Gas:     10_000_000,
		}

		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PostForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.True(t, results[0].Reverted,
			"modexp with all lengths exceeding 1024 must revert post-fork")
	})
}

func TestEIP7823_ExceedsBound_PreFork(t *testing.T) {
	client := helper.NewClient(nodeURL)

	base := make([]byte, 1025)
	base[0] = 0x01
	exp := []byte{0x01}
	mod := []byte{0x03}

	input := encodeModExpInput(base, exp, mod)
	callData := &api.BatchCallData{
		Clauses: api.Clauses{{To: &modExpAddr, Data: "0x" + hex.EncodeToString(input)}},
		Gas:     100_000,
	}

	results, err := client.InspectClauses(callData, thorclient.Revision(helper.PreForkRevision))
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].Reverted,
		"modexp with baseLen=1025 must succeed pre-fork (no upper-bound check): %s", results[0].VMError)
}

func TestEIP7823_LengthOverflow(t *testing.T) {
	client := helper.NewClient(nodeURL)

	input := encodeModExpOverflowInput()
	callData := &api.BatchCallData{
		Clauses: api.Clauses{{To: &modExpAddr, Data: "0x" + hex.EncodeToString(input)}},
		Gas:     100_000,
	}

	// RequiredGas returns math.MaxUint64 for overflowing length fields, so the
	// EVM always reverts with "out of gas" before Run (and the EIP-7823 check)
	// is reached. We only assert that the call is rejected.
	results, err := client.InspectClauses(callData, thorclient.Revision(helper.PostForkRevision))
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Reverted,
		"modexp with baseLen overflowing uint64 must revert post-fork")
}
