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
	"math/big"
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
	// Input shorter than 160 bytes → precompile returns nil (empty), no revert.
	shortInput := validP256Input[:100]
	client := helper.NewClient(nodeURL)
	callData := &api.BatchCallData{
		Clauses: api.Clauses{{To: &p256VerifyAddr, Data: "0x" + hex.EncodeToString(shortInput)}},
		Gas:     10_000,
	}

	t.Run("post-fork", func(t *testing.T) {
		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PostForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Reverted, "wrong-length input must not revert")
		assert.Empty(t, strings.TrimPrefix(results[0].Data, "0x"),
			"wrong-length input must return empty output")
	})
}

// p256VerifyContractBytecode is compiled from (solc 0.8.24, --evm-version shanghai):
//
//	contract P256Verify {
//	  function p256VerifyBytes(bytes memory input) public view returns (uint256 ret) {
//	    assembly {
//	      let p := mload(0x40)
//	      let ok := staticcall(gas(), 0x100, add(input, 32), mload(input), p, 32)
//	      if ok { ret := mload(p) }
//	    }
//	  }
//	}
var p256VerifyContractBytecode, _ = hex.DecodeString(
	"608060405234801561000f575f80fd5b5060043610610029575f3560e01c80633ed4e7d61461002d575b5f80fd5b" +
		"61004061003b36600461008a565b610052565b60405190815260200160405180910390f35b5f6040516020818451" +
		"602086016101005afa801561006f57815192505b5050919050565b634e487b7160e01b5f52604160045260245ffd" +
		"5b5f6020828403121561009a575f80fd5b813567ffffffffffffffff808211156100b1575f80fd5b818401915084" +
		"601f8301126100c4575f80fd5b8135818111156100d6576100d6610076565b604051601f8201601f19908116603f" +
		"011681019083821181831017156100fe576100fe610076565b81604052828152876020848701011115610116575f" +
		"80fd5b826020860160208301375f92810160200192909252509594505050505056fea264697066735822122032c5" +
		"2793fb9d70df956cbaf79030d76e52225d5b1d9adc7a7b26606ef9d4c54a64736f6c63430008180033",
)

// encodeP256VerifyCall ABI-encodes a call to p256VerifyBytes(bytes).
// Selector: keccak256("p256VerifyBytes(bytes)") = 0x3ed4e7d6
// Layout: [selector 4B][offset 32B = 0x20][length 32B][data padded to 32B boundary]
func encodeP256VerifyCall(input []byte) []byte {
	padLen := (len(input) + 31) / 32 * 32
	buf := make([]byte, 4+32+32+padLen)
	copy(buf[0:4], []byte{0x3e, 0xd4, 0xe7, 0xd6})
	buf[35] = 0x20 // offset = 32
	new(big.Int).SetUint64(uint64(len(input))).FillBytes(buf[36:68])
	copy(buf[68:], input)
	return buf
}

func TestEIP7951_ContractCall(t *testing.T) {
	// Deploy a Solidity wrapper that calls p256Verify via staticcall(gas(), 0x100, ...).
	// Verifies the full on-chain path: contract → precompile → result.
	//
	// Contract returns uint256(1) for valid sig, uint256(0) for invalid sig.
	client := helper.NewClient(nodeURL)

	// Deploy the contract.
	deployClause := tx.NewClause(nil).WithData(p256VerifyContractBytecode)
	deployTx := helper.BuildTx(t, client, 300_000, deployClause)
	deployResult, err := client.SendTransaction(deployTx)
	require.NoError(t, err)

	receipt := helper.WaitForReceipt(t, client, deployResult.ID, 30*time.Second)
	require.False(t, receipt.Reverted, "contract deployment must not revert")
	require.Len(t, receipt.Outputs, 1)
	require.NotNil(t, receipt.Outputs[0].ContractAddress, "deployment receipt must contain contract address")
	contractAddr := *receipt.Outputs[0].ContractAddress

	t.Run("valid-signature", func(t *testing.T) {
		callData := &api.BatchCallData{
			Clauses: api.Clauses{{
				To:   &contractAddr,
				Data: "0x" + hex.EncodeToString(encodeP256VerifyCall(validP256Input)),
			}},
			Gas: 50_000,
		}
		results, err := client.InspectClauses(callData, thorclient.Revision("best"))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Reverted, "contract call must not revert for valid input")
		raw, err := hex.DecodeString(strings.TrimPrefix(results[0].Data, "0x"))
		require.NoError(t, err)
		require.Len(t, raw, 32, "contract must return 32-byte uint256")
		assert.Equal(t, byte(1), raw[31], "contract must return uint256(1) for valid P-256 signature")
	})

	t.Run("invalid-signature", func(t *testing.T) {
		callData := &api.BatchCallData{
			Clauses: api.Clauses{{
				To:   &contractAddr,
				Data: "0x" + hex.EncodeToString(encodeP256VerifyCall(invalidP256Input)),
			}},
			Gas: 50_000,
		}
		results, err := client.InspectClauses(callData, thorclient.Revision("best"))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Reverted, "contract call must not revert for invalid input")
		raw, err := hex.DecodeString(strings.TrimPrefix(results[0].Data, "0x"))
		require.NoError(t, err)
		require.Len(t, raw, 32, "contract must return 32-byte uint256")
		assert.Equal(t, byte(0), raw[31], "contract must return uint256(0) for invalid P-256 signature")
	})
}
