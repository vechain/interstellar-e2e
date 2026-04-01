package eip7934

// EIP-7934 caps the RLP-encoded block size at MaxRLPBlockSize = 8,388,608 bytes (8 MiB).
// After the INTERSTELLAR fork the packer tracks accumulated transaction size and stops
// adding transactions once the next one would push the block past the limit. The
// consensus layer independently rejects any block whose RLP encoding exceeds the cap.
//
// Why a burst of many transactions?
//
// Individual transactions are bounded by the txpool's MaxTxSize (64 KB). A single
// transaction can therefore never exceed the 8 MiB block limit on its own. The block-
// size constraint only becomes the binding limit when many large transactions compete
// for space in the same block.
//
// With the test network's 40 M block gas limit and ~64 KB txs (each costing ~277 K
// intrinsic gas for 64,000 zero-byte data), gas alone would allow ~144 txs per block,
// but the 8 MiB block-size cap is reached at ~130.  This makes block size the binding
// constraint — exactly what EIP-7934 is designed to enforce.

import (
	"crypto/ecdsa"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vechain/interstellar-e2e/tests/helper"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/tx"
)

const (
	maxRLPBlockSize uint64 = 8_388_608

	// txDataSize is chosen so that (data + ~200 B of RLP/signature overhead)
	// stays under the txpool's MaxTxSize (64 KB = 65,536 B).
	txDataSize = 64_000

	// numLargeTxs must exceed the ~130 that fit in one block by size, so the
	// packer is forced to spill into a second block.
	numLargeTxs = 150
)

// signers are the three pre-funded node accounts from LocalThreeNodesNetwork.
// We distribute transactions round-robin to stay within the txpool's per-account
// quota (LimitPerAccount = 128).
var signers = []*ecdsa.PrivateKey{
	helper.TestSenderKey,
	helper.Node2Key,
	helper.Node3Key,
}

func TestEIP7934(t *testing.T) {
	client := helper.NewClient(nodeURL)

	chainTag, err := client.ChainTag()
	require.NoError(t, err)
	best, err := client.Block("best")
	require.NoError(t, err)

	to := thor.BytesToAddress([]byte{0xde, 0xad})
	intrinsicGas := uint64(21_000 + txDataSize*4) // 4 is the gas cost per zero byte of calldata
	baseNonce := uint64(time.Now().UnixNano())

	txIDs := make([]*thor.Bytes32, numLargeTxs)
	for i := range numLargeTxs {
		clause := tx.NewClause(&to).WithData(make([]byte, txDataSize))
		trx := tx.NewBuilder(tx.TypeLegacy).
			ChainTag(chainTag).
			Clause(clause).
			Gas(intrinsicGas).
			BlockRef(tx.NewBlockRefFromID(best.ID)).
			Expiration(100).
			Nonce(baseNonce + uint64(i)).
			Build()

		signer := signers[i%len(signers)]
		signed, err := tx.Sign(trx, signer)
		require.NoError(t, err)

		result, err := client.SendTransaction(signed)
		require.NoError(t, err, "tx %d must be accepted by the txpool", i)
		txIDs[i] = result.ID
	}

	// Collect receipts and group by block number.
	blockTxCount := make(map[uint32]int)
	for i, txID := range txIDs {
		receipt := helper.WaitForReceipt(t, client, txID, 120*time.Second)
		require.NotNil(t, receipt, "tx %d must be mined", i)
		blockTxCount[receipt.Meta.BlockNumber]++
	}

	// Query the RLP size of every block that included our transactions.
	type blockInfo struct {
		number uint32
		size   uint32
		txs    int
	}
	blocks := make([]blockInfo, 0, len(blockTxCount))
	for num, count := range blockTxCount {
		blk, err := client.Block(fmt.Sprintf("%d", num))
		require.NoError(t, err)
		blocks = append(blocks, blockInfo{number: num, size: blk.Size, txs: count})
	}

	// --- BlockSizeAboveMax: the packer must not stuff all txs in one block ---
	t.Run("BlockSizeAboveMax", func(t *testing.T) {
		// 150 txs × ~64 KB ≈ 9.6 MiB, well over the 8 MiB cap.
		// The packer should have split them across at least two blocks.
		require.Greater(t, len(blocks), 1,
			"packer should split %d large txs across multiple blocks", numLargeTxs)

		// Every block must honour the cap.
		for _, b := range blocks {
			assert.LessOrEqual(t, uint64(b.size), maxRLPBlockSize,
				"block %d: size %d exceeds MaxRLPBlockSize %d", b.number, b.size, maxRLPBlockSize)
		}
	})

	// --- BlockSizeMatchingMax: the fullest block should approach the cap ---
	t.Run("BlockSizeMatchingMax", func(t *testing.T) {
		// The packer fills each block as close to 8 MiB as possible.  The
		// fullest block should use at least 50% of the cap.  In practice it
		// reaches ~99% (≈130 txs × 64 KB ≈ 8.3 MiB), but we use a conservative
		// threshold to avoid flakiness from block-timing variance.
		var maxSize uint32
		for _, b := range blocks {
			if b.size > maxSize {
				maxSize = b.size
			}
		}
		assert.Greater(t, uint64(maxSize), maxRLPBlockSize/2,
			"largest block (%d B) should be at least 50%% of MaxRLPBlockSize", maxSize)
	})

	// --- BlockSizeBelowMax: a normal small tx works fine ---
	t.Run("BlockSizeBelowMax", func(t *testing.T) {
		trx := helper.BuildTx(t, client, 21_000, helper.ZeroClause())
		result, err := client.SendTransaction(trx)

		require.NoError(t, err, "normal-sized tx must be accepted")
		require.NotNil(t, result.ID)

		receipt := helper.WaitForReceipt(t, client, result.ID, 30*time.Second)
		assert.False(t, receipt.Reverted)
	})
}
