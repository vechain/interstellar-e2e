package eip7951

// EIP-7951 adds a secp256r1 (P-256) elliptic curve signature verification precompile
// at the INTERSTELLAR fork.
//
// Precompile address: 0x0100
// Gas cost: 6900 (flat, P256VerifyGas)
// Input: 160 bytes = hash(32) || r(32) || s(32) || x(32) || y(32)
// Output (valid sig): 32-byte big-endian 1
// Output (invalid or wrong-length input): empty (nil)
//
// Pre-fork: 0x0100 has no code — call returns empty output, not reverted.

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/tx"

	"github.com/vechain/interstellar-e2e/tests/helper"
)

// p256VerifyAddr is the secp256r1 precompile address added at INTERSTELLAR.
var p256VerifyAddr = thor.BytesToAddress([]byte{0x1, 0x00})

// validP256Input is a known-good secp256r1 signature from p256Verify.json (Gas=6900).
// Layout: hash(32) || r(32) || s(32) || x(32) || y(32)
var validP256Input, _ = hex.DecodeString(
	"4cee90eb86eaa050036147a12d49004b6b9c72bd725d39d4785011fe190f0b4d" +
		"a73bd4903f0ce3b639bbbf6e8e80d16931ff4bcf5993d58468e8fb19086e8cac" +
		"36dbcd03009df8c59286b162af3bd7fcc0450c9aa81be5d10d312af6c66b1d60" +
		"4aebd3099c618202fcfe16ae7770b0c49ab5eadf74b754204a3bb6060e44eff3" +
		"7618b065f9832de4ca6ca971a7a1adc826d0f7c00181a5fb2ddf79ae00b4e10e",
)

// invalidP256Input is the same vector with the first byte of s flipped, making the signature invalid.
var invalidP256Input = func() []byte {
	b := make([]byte, len(validP256Input))
	copy(b, validP256Input)
	b[64] ^= 0xff // flip first byte of s (byte offset 32+32=64)
	return b
}()

func TestEIP7951_ValidSignature(t *testing.T) {
	// Pre-fork: 0x0100 has no code — call succeeds with empty output.
	// Post-fork: precompile returns 32-byte big-endian 1.
	client := helper.NewClient(nodeURL)
	callData := &api.BatchCallData{
		Clauses: api.Clauses{{To: &p256VerifyAddr, Data: "0x" + hex.EncodeToString(validP256Input)}},
		Gas:     10_000,
	}

	t.Run("pre-fork", func(t *testing.T) {
		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PreForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Reverted, "pre-fork: call to empty address must not revert")
		assert.Empty(t, strings.TrimPrefix(results[0].Data, "0x"),
			"pre-fork: 0x0100 has no code, output must be empty")
	})

	t.Run("post-fork", func(t *testing.T) {
		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PostForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Reverted, "post-fork: p256Verify must not revert for valid input")
		raw, err := hex.DecodeString(strings.TrimPrefix(results[0].Data, "0x"))
		require.NoError(t, err)
		require.Len(t, raw, 32, "post-fork: p256Verify must return 32 bytes for valid signature")
		assert.Equal(t, byte(1), raw[31], "post-fork: p256Verify must return 1 for valid signature")
	})
}

func TestEIP7951_InvalidSignature(t *testing.T) {
	// Corrupted s → precompile returns nil (empty), no revert, in both forks.
	client := helper.NewClient(nodeURL)
	callData := &api.BatchCallData{
		Clauses: api.Clauses{{To: &p256VerifyAddr, Data: "0x" + hex.EncodeToString(invalidP256Input)}},
		Gas:     10_000,
	}

	t.Run("pre-fork", func(t *testing.T) {
		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PreForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Reverted, "pre-fork: must not revert")
		assert.Empty(t, strings.TrimPrefix(results[0].Data, "0x"), "pre-fork: output must be empty")
	})

	t.Run("post-fork", func(t *testing.T) {
		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PostForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Reverted, "post-fork: p256Verify must not revert for invalid sig")
		assert.Empty(t, strings.TrimPrefix(results[0].Data, "0x"),
			"post-fork: p256Verify returns empty for invalid signature")
	})
}

func TestEIP7951_GasCost(t *testing.T) {
	// Post-fork: precompile charges exactly P256VerifyGas = 6900.
	// Pre-fork: 0x0100 has no code, GasUsed is 0.
	client := helper.NewClient(nodeURL)
	callData := &api.BatchCallData{
		Clauses: api.Clauses{{To: &p256VerifyAddr, Data: "0x" + hex.EncodeToString(validP256Input)}},
		Gas:     10_000,
	}

	t.Run("pre-fork", func(t *testing.T) {
		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PreForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, uint64(0), results[0].GasUsed,
			"pre-fork: no precompile at 0x0100, GasUsed must be 0")
	})

	t.Run("post-fork", func(t *testing.T) {
		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PostForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, uint64(6900), results[0].GasUsed,
			"post-fork: P256VerifyGas must be exactly 6900")
	})
}

func TestEIP7951_WrongInputLength(t *testing.T) {
	// EIP-7951 Run() requires exactly 160 bytes; any other length returns nil (empty), no revert.
	// RequiredGas always charges 6900 regardless of input length.
	client := helper.NewClient(nodeURL)

	assertWrongLength := func(t *testing.T, input []byte, label string) {
		t.Helper()
		callData := &api.BatchCallData{
			Clauses: api.Clauses{{To: &p256VerifyAddr, Data: "0x" + hex.EncodeToString(input)}},
			Gas:     10_000,
		}
		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PostForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Reverted, "%s: must not revert", label)
		assert.Empty(t, strings.TrimPrefix(results[0].Data, "0x"),
			"%s: must return empty output", label)
		assert.Equal(t, uint64(6900), results[0].GasUsed,
			"%s: RequiredGas charges 6900 regardless of input length", label)
	}

	t.Run("too-short", func(t *testing.T) {
		assertWrongLength(t, validP256Input[:100], "100-byte input")
	})

	t.Run("too-long", func(t *testing.T) {
		// 160 valid bytes + 32 zero bytes = 192 bytes total
		long := make([]byte, 192)
		copy(long, validP256Input)
		assertWrongLength(t, long, "192-byte input")
	})
}

// p256ProxyBytecode is a minimal EVM contract that forwards its calldata to the
// p256Verify precompile (0x0100) via staticcall and returns 32 bytes of output.
//
// Runtime bytecode (24 bytes):
//
//	CALLDATACOPY(calldatasize, mem[0])            — copy input to mem[0..160]
//	STATICCALL(gas, 0x0100, 0, cds, 0xa0, 32)    — call precompile, output at mem[0xa0]
//	RETURN(0xa0, 32)                              — return the 32-byte result
//
// Input is 160 bytes (0xa0), so output is placed at mem[0xa0] to avoid overlap.
// mem[0xa0] is zero-initialized; an empty precompile response leaves it all-zeros,
// making the contract return uint256(0) for invalid signatures.
//
// Deployment: 13-byte init code + 24-byte runtime = 37 bytes total.
var p256ProxyBytecode, _ = hex.DecodeString(
	"6100188061000d6000396000f3" + // init code (13 bytes): CODECOPY runtime → RETURN
		"366000600037" + // CALLDATASIZE, PUSH1 0, PUSH1 0, CALLDATACOPY
		"602060a0" + // PUSH1 32 (retLen), PUSH1 0xa0 (retOffset — after 160-byte input)
		"3660006101005a" + // CALLDATASIZE (argsLen), PUSH1 0 (argsOffset), PUSH2 0x0100 (addr), GAS
		"fa" + // STATICCALL
		"50602060a0f3", // POP, PUSH1 32, PUSH1 0xa0, RETURN
)

func TestEIP7951_ContractCall(t *testing.T) {
	// Deploy a minimal EVM proxy that forwards calldata to precompile 0x0100 via staticcall.
	// Verifies the full on-chain path: contract → precompile → caller.
	//
	// Contract returns 32-byte big-endian 1 for valid sig, 32-byte 0 for invalid sig.
	client := helper.NewClient(nodeURL)

	// Deploy the proxy contract.
	deployClause := tx.NewClause(nil).WithData(p256ProxyBytecode)
	deployTx := helper.BuildTx(t, client, 300_000, deployClause)
	deployResult, err := client.SendTransaction(deployTx)
	require.NoError(t, err)

	receipt := helper.WaitForReceipt(t, client, deployResult.ID, 30*time.Second)
	require.False(t, receipt.Reverted, "proxy deployment must not revert")
	require.Len(t, receipt.Outputs, 1)
	require.NotNil(t, receipt.Outputs[0].ContractAddress, "deployment receipt must contain contract address")
	contractAddr := *receipt.Outputs[0].ContractAddress

	t.Run("valid-signature", func(t *testing.T) {
		callData := &api.BatchCallData{
			Clauses: api.Clauses{{
				To:   &contractAddr,
				Data: "0x" + hex.EncodeToString(validP256Input),
			}},
			Gas: 50_000,
		}
		results, err := client.InspectClauses(callData, thorclient.Revision("best"))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Reverted, "contract call must not revert for valid input")
		raw, err := hex.DecodeString(strings.TrimPrefix(results[0].Data, "0x"))
		require.NoError(t, err)
		require.Len(t, raw, 32, "contract must return 32 bytes")
		assert.Equal(t, byte(1), raw[31], "contract must return 1 for valid P-256 signature")
	})

	t.Run("invalid-signature", func(t *testing.T) {
		callData := &api.BatchCallData{
			Clauses: api.Clauses{{
				To:   &contractAddr,
				Data: "0x" + hex.EncodeToString(invalidP256Input),
			}},
			Gas: 50_000,
		}
		results, err := client.InspectClauses(callData, thorclient.Revision("best"))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Reverted, "contract call must not revert for invalid input")
		raw, err := hex.DecodeString(strings.TrimPrefix(results[0].Data, "0x"))
		require.NoError(t, err)
		require.Len(t, raw, 32, "contract must return 32 bytes")
		assert.Equal(t, byte(0), raw[31], "contract must return 0 for invalid P-256 signature")
	})
}
