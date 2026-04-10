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

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/block"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/tx"
	"github.com/vechain/thor/v2/vrf"

	"github.com/vechain/interstellar-e2e/tests/helper"
)

const (
	maxRLPBlockSize uint64 = 8_388_608

	// txDataSize is chosen so that (data + ~200 B of RLP/signature overhead)
	// stays under the txpool's MaxTxSize (64 KB = 65,536 B).
	txDataSize = 64_000

	targetSize       = maxRLPBlockSize + 1
	baseTxCount      = 130
	estimatedPadding = 30_000
)

// signers are the three pre-funded node accounts from LocalThreeNodesNetwork.
// We distribute transactions round-robin to stay within the txpool's per-account
// quota (LimitPerAccount = 128).
var signers = []*ecdsa.PrivateKey{
	helper.TestSenderKey,
	helper.Node2Key,
	helper.Node3Key,
}

// TestEIP7934 constructs a validly-signed block whose RLP
// encoding exceeds MaxRLPBlockSize and disseminates it to a running Thor node
// via the devp2p MsgNewBlock message. The consensus validator (validateBlockBody)
// must reject it at the size check, so the block must NOT appear in the chain.
func TestEIP7934(t *testing.T) {
	client := helper.NewClient(nodeURL)

	genesis, err := client.Block("0")
	require.NoError(t, err)

	// Wait for a fresh block so we can copy its already-validated scheduling.
	initialBest, err := client.Block("best")
	require.NoError(t, err)

	var observed *api.JSONCollapsedBlock
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		b, err := client.Block("best")
		require.NoError(t, err)
		if b.Number > initialBest.Number {
			observed = b
			break
		}
		time.Sleep(time.Second)
	}
	require.NotNil(t, observed, "must observe a new block within timeout")

	// Identify which of our keys signed the observed block.
	var proposerKey *ecdsa.PrivateKey
	for _, key := range signers {
		addr := thor.Address(crypto.PubkeyToAddress(key.PublicKey))
		if addr == observed.Signer {
			proposerKey = key
			break
		}
	}
	require.NotNil(t, proposerKey,
		"observed block signer %s must match one of our known keys", observed.Signer)

	// Fetch the observed block's full header for Alpha and BaseFee.
	observedHeader, err := helper.FetchRawBlockHeader(nodeURL, fmt.Sprintf("%d", observed.Number))
	require.NoError(t, err)

	alpha := observedHeader.Alpha()
	require.NotEmpty(t, alpha, "post-VIP214 block must carry Alpha")

	// Build a block that is exactly MaxRLPBlockSize + 1 bytes. We use
	// baseTxCount full-size transactions plus one "padding" transaction
	// whose data length is calibrated to land on the exact byte target.
	// Within the 56–65535 data-length range each extra byte of tx data
	// adds exactly one byte to the block's RLP size, so a single
	// probe-and-adjust pass is enough.
	buildBlock := func(paddingDataLen int) *block.Block {
		b := new(block.Builder).
			ParentID(observed.ParentID).
			Timestamp(observed.Timestamp).
			GasLimit(observed.GasLimit).
			TotalScore(observed.TotalScore).
			GasUsed(0).
			Beneficiary(observed.Beneficiary).
			StateRoot(thor.Bytes32{}).
			ReceiptsRoot(thor.Bytes32{}).
			TransactionFeatures(tx.DelegationFeature).
			Alpha(alpha).
			BaseFee(observedHeader.BaseFee())

		for i := range baseTxCount {
			clause := tx.NewClause(nil).WithData(make([]byte, txDataSize))
			trx := tx.NewBuilder(tx.TypeLegacy).
				Clause(clause).
				Gas(21_000).
				Nonce(uint64(i)).
				Build()
			b.Transaction(trx)
		}

		clause := tx.NewClause(nil).WithData(make([]byte, paddingDataLen))
		trx := tx.NewBuilder(tx.TypeLegacy).
			Clause(clause).
			Gas(21_000).
			Nonce(uint64(baseTxCount)).
			Build()
		b.Transaction(trx)

		return b.Build()
	}

	signBlock := func(blk *block.Block) *block.Block {
		ecSig, err := crypto.Sign(blk.Header().SigningHash().Bytes(), proposerKey)
		require.NoError(t, err)
		_, proof, err := vrf.Prove(proposerKey, alpha)
		require.NoError(t, err)
		sig, err := block.NewComplexSignature(ecSig, proof)
		require.NoError(t, err)
		return blk.WithSignature(sig)
	}

	probe := signBlock(buildBlock(estimatedPadding))
	probeSize := uint64(probe.Size())

	adjustedPadding := int(estimatedPadding) + int(targetSize) - int(probeSize)
	require.Greater(t, adjustedPadding, 0, "padding calculation must yield positive data size")

	oversized := signBlock(buildBlock(adjustedPadding))

	require.Equal(t, targetSize, uint64(oversized.Size()),
		"block must be exactly MaxRLPBlockSize + 1")
	require.NotEqual(t, thor.Bytes32{}, oversized.Header().ID(),
		"block ID must be non-zero (valid signature)")

	// Connect via P2P and send the oversized block.
	p2pClient := helper.NewThorP2PClient(genesis.ID, observed.ParentID, observed.TotalScore-1)
	err = p2pClient.Connect(helper.TestSenderKey, nodeP2PPort)
	require.NoError(t, err, "P2P connection to node1 must succeed")
	defer p2pClient.Stop()

	err = p2pClient.SendBlock(oversized)
	require.NoError(t, err, "sending oversized block via P2P must not error at the transport level")

	// Give the node time to process the block and continue producing.
	time.Sleep(30 * time.Second)

	blockID := oversized.Header().ID()
	found, _ := client.Block(blockID.String())
	assert.Nil(t, found,
		"oversized block (ID %s) must NOT be accepted into the chain", blockID)

	newBest, err := client.Block("best")
	require.NoError(t, err)
	assert.Greater(t, newBest.Number, observed.Number,
		"node must continue producing blocks after rejecting the oversized P2P block")
}
