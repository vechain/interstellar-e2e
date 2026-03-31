package eip7883

// EIP-7883 reprices the ModExp precompile (address 0x05) at the INTERSTELLAR fork:
//
//	osakaMultComplexity(x):
//	  x <= 32  → 16
//	  x >  32  → 2 * ceil(x/8)²
//
//	adjExpLen = 16*(expLen-32) [if expLen>32] + msb(first 32 bytes of exp)
//	gas       = osakaMultComplexity(max(baseLen, modLen)) * max(adjExpLen, 1)
//	minimum   = 500
//
// Pre-fork (EIP-2565): multComplexity(x) = ceil(x/8)², gas /= 3, minimum = 200.

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

// encodeModExpInput encodes inputs for the ModExp precompile.
// Wire format: [baseLen (32B)][expLen (32B)][modLen (32B)][base][exp][mod]
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

func TestEIP7883_MinimumGas(t *testing.T) {
	// base=1B (0x01), exp=1B (0x01, value=1, msb=0), mod=1B (0x03)
	// EIP-2565 (pre-fork):  multComplexity(1)=1, adjExpLen=0→max→1, gas=1*1/3=0  → minimum 200
	// EIP-7883 (post-fork): osakaMultComplexity(1)=16, adjExpLen=0→max→1, gas=16 → minimum 500
	input := encodeModExpInput([]byte{0x01}, []byte{0x01}, []byte{0x03})
	client := helper.NewClient(nodeURL)
	callData := &api.BatchCallData{
		Clauses: api.Clauses{{To: &modExpAddr, Data: "0x" + hex.EncodeToString(input)}},
		Gas:     10_000,
	}

	t.Run("pre-fork", func(t *testing.T) {
		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PreForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Reverted, "ModExp must not revert: %s", results[0].VMError)
		assert.Equal(t, uint64(200), results[0].GasUsed,
			"pre-INTERSTELLAR: EIP-2565 minimum gas should be 200")
	})

	t.Run("post-fork", func(t *testing.T) {
		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PostForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Reverted, "ModExp must not revert: %s", results[0].VMError)
		assert.Equal(t, uint64(500), results[0].GasUsed,
			"post-INTERSTELLAR: EIP-7883 minimum gas should be 500")
	})
}

func TestEIP7883_GasFormula(t *testing.T) {
	// base=1B (0x01), exp=2B (0x0800, value=2048, msb=11), mod=33B
	// EIP-2565 (pre-fork):  multComplexity(33)=ceil(33/8)²=25, adjExpLen=11, gas=25*11/3=91 → minimum 200
	// EIP-7883 (post-fork): osakaMultComplexity(33)=2*ceil(33/8)²=50, adjExpLen=11, gas=50*11=550
	base := []byte{0x01}
	exp := []byte{0x08, 0x00} // 2048
	mod := make([]byte, 33)
	mod[0] = 0x03 // non-zero to avoid trivial zero result
	input := encodeModExpInput(base, exp, mod)
	client := helper.NewClient(nodeURL)
	callData := &api.BatchCallData{
		Clauses: api.Clauses{{To: &modExpAddr, Data: "0x" + hex.EncodeToString(input)}},
		Gas:     10_000,
	}

	t.Run("pre-fork", func(t *testing.T) {
		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PreForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Reverted, "ModExp must not revert: %s", results[0].VMError)
		assert.Equal(t, uint64(200), results[0].GasUsed,
			"pre-INTERSTELLAR: EIP-2565 formula gives 91, hits 200 minimum")
	})

	t.Run("post-fork", func(t *testing.T) {
		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PostForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Reverted, "ModExp must not revert: %s", results[0].VMError)
		assert.Equal(t, uint64(550), results[0].GasUsed,
			"post-INTERSTELLAR: EIP-7883 gas for (base=1B, exp=2B/2048, mod=33B) should be 550")
	})
}
