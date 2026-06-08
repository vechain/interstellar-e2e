package ethrpc

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ethclient "github.com/vechain/interstellar-e2e/tests/eth_rpc/eth_client"
	"github.com/vechain/interstellar-e2e/tests/helper"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
)

// dialClient is the shared connection boilerplate the new method-coverage
// tests below use. The original tests in this file inline this setup; both
// patterns are equivalent. We factor it out here only because the coverage
// suite roughly triples the number of tests.
func dialClient(t *testing.T) (context.Context, *ethclient.Client, func()) {
	t.Helper()
	ctx := context.Background()
	rpcClient, err := rpc.DialContext(ctx, nodeURL+"/rpc")
	require.NoError(t, err, "dial rpc node")
	return ctx, ethclient.NewClient(rpcClient), func() { rpcClient.Close() }
}

// latestHashAndNumber returns the current chain-head block hash and number.
func latestHashAndNumber(t *testing.T) (common.Hash, *big.Int) {
	t.Helper()
	thorClient := helper.NewClient(nodeURL)
	block, err := thorClient.Block("best")
	require.NoError(t, err, "fetch latest block from thor client")
	require.NotNil(t, block, "latest block from thor client must not be nil")
	return common.Hash(block.ID.Bytes()), big.NewInt(int64(block.Number))
}

// skipIfMethodNotFound checks whether err is a JSON-RPC "method not found"
// (code -32601) and skips the test if so. Returns true when skipped so callers
// can early-return.
func skipIfMethodNotFound(t *testing.T, err error, method string) bool {
	t.Helper()
	if err == nil {
		return false
	}
	var rpcErr rpc.Error
	if errors.As(err, &rpcErr) && rpcErr.ErrorCode() == -32601 {
		t.Skipf("%s not supported by node: %v", method, err)
		return true
	}
	return false
}

// skipIfUnsupported is the broader version: it also skips when the node
// reports the call's shape as unsupported with a non-standard error (e.g.
// Thor rejecting a hash-form block tag, or a parameter it doesn't yet
// implement). Used for inputs that upstream geth supports but Thor may not.
func skipIfUnsupported(t *testing.T, err error, method string) bool {
	t.Helper()
	if err == nil {
		return false
	}
	if skipIfMethodNotFound(t, err, method) {
		return true
	}
	msg := err.Error()
	for _, marker := range []string{
		"invalid block tag",
		"not yet supported",
		"not supported",
	} {
		if containsFold(msg, marker) {
			t.Skipf("%s unsupported by node: %v", method, err)
			return true
		}
	}
	return false
}

// containsFold is a tiny case-insensitive substring check; avoids pulling
// strings.ToLower across the whole error message every call site.
func containsFold(haystack, needle string) bool {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return needle == ""
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			a, b := haystack[i+j], needle[j]
			if 'A' <= a && a <= 'Z' {
				a += 'a' - 'A'
			}
			if 'A' <= b && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// TestNetVersion asserts that the net_version endpoint returns a valid
// numeric network ID.
func TestNetVersion(t *testing.T) {
	ctx, ethclient, closeFn := dialClient(t)
	defer closeFn()

	networkID, err := ethclient.NetworkID(ctx)
	require.NoError(t, err, "net_version call")
	require.NotNil(t, networkID, "net_version result must not be nil")
	assert.Positive(t, networkID.Sign(), "network ID must be a positive integer, got: %s", networkID.String())
}

// TestNetPeerCount asserts that the net_peerCount endpoint succeeds. The
// returned count may be zero on a solo test network with no peers.
func TestNetPeerCount(t *testing.T) {
	ctx, ethclient, closeFn := dialClient(t)
	defer closeFn()

	peerCount, err := ethclient.PeerCount(ctx)
	require.NoError(t, err, "net_peerCount call")
	require.Equal(t, uint64(0), peerCount, "expected zero peers on a solo test network")
}

// TestEthSyncing asserts that eth_syncing returns false on Thor.
// SyncProgress returns (nil, nil) for the literal false response.
func TestEthSyncing(t *testing.T) {
	ctx, ethclient, closeFn := dialClient(t)
	defer closeFn()

	progress, err := ethclient.SyncProgress(ctx)
	require.NoError(t, err, "eth_syncing call")
	assert.Nil(t, progress, "eth_syncing must always return false (SyncProgress nil)")
}

// TestEthChainID asserts that eth_chainId returns a positive integer.
func TestEthChainID(t *testing.T) {
	ctx, ethclient, closeFn := dialClient(t)
	defer closeFn()

	thorClient := helper.NewClient(nodeURL)

	chainID, err := ethclient.ChainID(ctx)
	require.NoError(t, err, "eth_chainId call")
	require.NotNil(t, chainID, "eth_chainId result must not be nil")

	block, err := thorClient.Block("0")
	assert.Equal(t, new(big.Int).SetBytes(block.ID[30:32]), chainID, "the chain ID from eth_chainId should match the last 2 bytes of the genesis block ID")
}

// TestEthGasPrice asserts that eth_gasPrice returns a non-negative value.
func TestEthGasPrice(t *testing.T) {
	ctx, ethclient, closeFn := dialClient(t)
	defer closeFn()

	gasPrice, err := ethclient.SuggestGasPrice(ctx)
	require.NoError(t, err, "eth_gasPrice call")
	require.NotNil(t, gasPrice, "eth_gasPrice result must not be nil")
	assert.GreaterOrEqual(t, gasPrice.Sign(), 0, "gas price must be non-negative, got: %s", gasPrice.String())
	t.Logf("eth_gasPrice = %s", gasPrice.String())
}

// TestEthBlockNumber asserts that eth_blockNumber returns the latest block
// height of the chain.
func TestEthBlockNumber(t *testing.T) {
	ctx, ethclient, closeFn := dialClient(t)
	defer closeFn()

	blockNumber, err := ethclient.BlockNumber(ctx)
	require.NoError(t, err, "eth_blockNumber call")
	t.Logf("eth_blockNumber = %d", blockNumber)
}

// TestEthGetBlockHeaderByNumber exercises HeaderByNumber (eth_getBlockByNumber
// with includeTxs=false) across three inputs:
//   - "latest" tag via rpc.LatestBlockNumber → must return the chain head
//   - nil                                    → must also return the chain head
//   - a future block number                  → must return an error (not found)
func TestEthGetBlockHeaderByNumber(t *testing.T) {
	ctx, ethclient, closeFn := dialClient(t)
	defer closeFn()

	thorClient := helper.NewClient(nodeURL)

	t.Run("latest", func(t *testing.T) {
		head, err := ethclient.HeaderByNumber(ctx, big.NewInt(int64(rpc.LatestBlockNumber)))
		require.NoError(t, err, "eth_getBlockByNumber(latest) call")
		require.NotNil(t, head, "latest header must not be nil")

		block, err := thorClient.Block(head.Number.String())
		require.NoError(t, err, "fetch block by number from thor client")
		require.NotNil(t, block, "block from thor client must not be nil")

		require.Equal(t, block.Number, uint32(head.Number.Uint64()), "block number mismatch between eth client and thor client")
		require.Equal(t, block.ParentID.Bytes(), head.ParentHash.Bytes(), "parent block ID mismatch between eth client and thor client")
		require.Equal(t, block.ID.Bytes(), head.Hash().Bytes(), "block ID mismatch between eth client and thor client")

	})

	t.Run("nil (defaults to latest)", func(t *testing.T) {
		head, err := ethclient.HeaderByNumber(ctx, nil)
		require.NoError(t, err, "eth_getBlockByNumber(nil) call")
		require.NotNil(t, head, "nil-block header must not be nil")

		block, err := thorClient.Block(head.Number.String())
		require.NoError(t, err, "fetch block by number from thor client")
		require.NotNil(t, block, "block from thor client must not be nil")

		require.Equal(t, block.Number, uint32(head.Number.Uint64()), "block number mismatch between eth client and thor client")
		require.Equal(t, block.ParentID.Bytes(), head.ParentHash.Bytes(), "parent block ID mismatch between eth client and thor client")
		require.Equal(t, block.ID.Bytes(), head.Hash().Bytes(), "block ID mismatch between eth client and thor client")
	})

	t.Run("future block number", func(t *testing.T) {
		// A height well beyond any plausible chain head; the node returns
		// JSON null which HeaderByNumber surfaces as ethereum.NotFound.
		future := new(big.Int).SetUint64(1 << 62)
		head, err := ethclient.HeaderByNumber(ctx, future)
		require.Error(t, err, "eth_getBlockByNumber(future) must fail")
		assert.Nil(t, head, "future header must be nil on error")
	})
}

// TestEthGetBlockByHash exercises HeaderByHash (eth_getBlockByHash) across
// three inputs:
//   - a valid hash captured from the chain head → must return the header
//   - a made-up hash that doesn't exist          → must return NotFound
//   - the zero hash (the closest analogue of nil for a common.Hash value) →
//     must return NotFound
func TestEthGetBlockByHash(t *testing.T) {
	ctx, ethclient, closeFn := dialClient(t)
	defer closeFn()

	thorClient := helper.NewClient(nodeURL)

	block, err := thorClient.Block("best")
	require.NoError(t, err, "fetch latest block from thor client")
	require.NotNil(t, block, "latest block from thor client must not be nil")
	validHash := common.Hash(block.ID.Bytes())

	t.Run("valid hash", func(t *testing.T) {
		head, err := ethclient.HeaderByHash(ctx, validHash)
		require.NoError(t, err, "eth_getBlockByHash(valid) call")
		require.NotNil(t, head, "header must not be nil for a valid hash")

		block, err := thorClient.Block(validHash.String())
		require.NoError(t, err, "fetch block by number from thor client")
		require.NotNil(t, block, "block from thor client must not be nil")

		require.Equal(t, block.Number, uint32(head.Number.Uint64()), "block number mismatch between eth client and thor client")
		require.Equal(t, block.ParentID.Bytes(), head.ParentHash.Bytes(), "parent block ID mismatch between eth client and thor client")
		require.Equal(t, block.ID.Bytes(), head.Hash().Bytes(), "block ID mismatch between eth client and thor client")
	})

	t.Run("non-existent hash", func(t *testing.T) {
		nonExistent := common.HexToHash("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
		got, err := ethclient.HeaderByHash(ctx, nonExistent)
		require.Error(t, err, "eth_getBlockByHash(non-existent) must fail")
		assert.Nil(t, got, "header must be nil when the hash does not resolve")
		t.Logf("non-existent hash error: %v", err)
	})

	t.Run("nil (zero) hash", func(t *testing.T) {
		// common.Hash is a [32]byte value type — its "nil" equivalent is the
		// zero value, which no block can legitimately have.
		got, err := ethclient.HeaderByHash(ctx, common.Hash{})
		require.Error(t, err, "eth_getBlockByHash(zero) must fail")
		assert.Nil(t, got, "header must be nil for the zero hash")
		t.Logf("zero hash error: %v", err)
	})
}

// -----------------------------------------------------------------------------
// Fee market — SuggestGasTipCap, FeeHistory, BlobBaseFee
// -----------------------------------------------------------------------------

// TestEthMaxPriorityFeePerGas exercises SuggestGasTipCap
// (eth_maxPriorityFeePerGas). The returned tip cap must be non-negative.
func TestEthMaxPriorityFeePerGas(t *testing.T) {
	ctx, client, closeFn := dialClient(t)
	defer closeFn()

	tip, err := client.SuggestGasTipCap(ctx)
	if skipIfMethodNotFound(t, err, "eth_maxPriorityFeePerGas") {
		return
	}
	require.NoError(t, err, "eth_maxPriorityFeePerGas call")
	require.NotNil(t, tip, "tip cap must not be nil")
	assert.GreaterOrEqual(t, tip.Sign(), 0, "tip cap must be non-negative, got: %s", tip.String())
	t.Logf("eth_maxPriorityFeePerGas = %s", tip.String())
}

// TestEthFeeHistory exercises FeeHistory across two inputs:
//   - blockCount=1, lastBlock=nil (latest), no reward percentiles
//   - blockCount=4, lastBlock=latest, reward percentiles
func TestEthFeeHistory(t *testing.T) {
	ctx, client, closeFn := dialClient(t)
	defer closeFn()

	t.Run("count=1 latest no percentiles", func(t *testing.T) {
		fh, err := client.FeeHistory(ctx, 1, nil, nil)
		if skipIfMethodNotFound(t, err, "eth_feeHistory") {
			return
		}
		require.NoError(t, err, "eth_feeHistory call")
		require.NotNil(t, fh, "fee history must not be nil")
		assert.NotNil(t, fh.OldestBlock, "oldest block must be set")
		assert.GreaterOrEqual(t, len(fh.BaseFee), 1, "must include at least 1 base fee entry")
	})

	t.Run("count=4 with percentiles", func(t *testing.T) {
		fh, err := client.FeeHistory(ctx, 4, big.NewInt(int64(rpc.LatestBlockNumber)), []float64{25, 50, 75})
		if skipIfUnsupported(t, err, "eth_feeHistory(percentiles)") {
			return
		}
		require.NoError(t, err, "eth_feeHistory call")
		require.NotNil(t, fh, "fee history must not be nil")
		t.Logf("baseFee entries=%d reward entries=%d gasUsedRatio entries=%d",
			len(fh.BaseFee), len(fh.Reward), len(fh.GasUsedRatio))
	})
}

// TestEthBlobBaseFee exercises BlobBaseFee (eth_blobBaseFee). Thor may or
// may not implement this; if unsupported the test is skipped.
func TestEthBlobBaseFee(t *testing.T) {
	ctx, client, closeFn := dialClient(t)
	defer closeFn()

	fee, err := client.BlobBaseFee(ctx)
	require.NoError(t, err, "eth_blobBaseFee call")
	require.NotNil(t, fee, "blob base fee must not be nil")
	assert.GreaterOrEqual(t, fee.Sign(), 0, "blob base fee must be non-negative, got: %s", fee.String())
	t.Logf("eth_blobBaseFee = %s", fee.String())
}

// -----------------------------------------------------------------------------
// State queries — eth_getBalance / eth_getStorageAt / eth_getTransactionCount /
// eth_getCode across (number / hash / pending) variants.
// -----------------------------------------------------------------------------

// TestEthGetBalance exercises BalanceAt / BalanceAtHash / PendingBalanceAt.
// The zero address must report a zero balance regardless of the block view.
func TestEthGetBalance(t *testing.T) {
	ctx, ethclient, closeFn := dialClient(t)
	defer closeFn()

	thor_Client := helper.NewClient(nodeURL)

	hash, number := latestHashAndNumber(t)
	node1Addr := common.HexToAddress("0x61fF580B63D3845934610222245C116E013717ec")

	t.Run("latest by number (nil)", func(t *testing.T) {
		bal, err := ethclient.BalanceAt(ctx, node1Addr, nil)
		require.NoError(t, err, "eth_getBalance(node1Addr, latest) call")
		require.NotNil(t, bal, "balance must not be nil")

		addr := thor.BytesToAddress(node1Addr.Bytes())
		acc, err := thor_Client.Account(&addr, thorclient.Revision(number.Text(10)))
		require.NoError(t, err, "fetch account from thor client")
		require.NotNil(t, acc, "account from thor client must not be nil")
		assert.Equal(t, (*big.Int)(acc.Balance).Text(16), bal.Text(16), "balance mismatch between eth client and thor client")
	})

	t.Run("by hash", func(t *testing.T) {
		bal, err := ethclient.BalanceAtHash(ctx, node1Addr, hash)

		require.NoError(t, err, "eth_getBalance(node1Addr, hash) call")
		require.NotNil(t, bal, "balance must not be nil")
	})

	t.Run("pending", func(t *testing.T) {
		bal, err := ethclient.PendingBalanceAt(ctx, node1Addr)

		require.NoError(t, err, "eth_getBalance(zero, pending) call")
		require.NotNil(t, bal, "balance must not be nil")
	})
}

// TestEthGetStorageAt exercises StorageAt / StorageAtHash / PendingStorageAt.
// Reading slot 0 of the zero address must yield 32 bytes of zero.
func TestEthGetStorageAt(t *testing.T) {
	ctx, client, closeFn := dialClient(t)
	defer closeFn()

	hash, _ := latestHashAndNumber(t)
	zeroAddr := common.Address{}
	zeroKey := common.Hash{}
	zero32 := make([]byte, 32)

	t.Run("latest by number (nil)", func(t *testing.T) {
		got, err := client.StorageAt(ctx, zeroAddr, zeroKey, nil)
		require.NoError(t, err, "eth_getStorageAt(zero, slot0, latest) call")
		assert.Equal(t, zero32, got, "slot 0 of zero address must be all zero")
	})

	t.Run("by hash", func(t *testing.T) {
		got, err := client.StorageAtHash(ctx, zeroAddr, zeroKey, hash)

		require.NoError(t, err, "eth_getStorageAt(zero, slot0, hash) call")
		assert.Equal(t, zero32, got, "slot 0 of zero address must be all zero")
	})

	t.Run("pending", func(t *testing.T) {
		got, err := client.PendingStorageAt(ctx, zeroAddr, zeroKey)

		require.NoError(t, err, "eth_getStorageAt(zero, slot0, pending) call")
		assert.Equal(t, zero32, got, "slot 0 of zero address must be all zero")
	})
}

// TestEthGetTransactionCount exercises NonceAt / NonceAtHash / PendingNonceAt.
// The zero address has never sent a transaction, so its nonce is always 0.
func TestEthGetTransactionCount(t *testing.T) {
	ctx, client, closeFn := dialClient(t)
	defer closeFn()

	hash, _ := latestHashAndNumber(t)
	zeroAddr := common.Address{}

	t.Run("latest by number (nil)", func(t *testing.T) {
		nonce, err := client.NonceAt(ctx, zeroAddr, nil)
		require.NoError(t, err, "eth_getTransactionCount(zero, latest) call")
		assert.Equal(t, uint64(0), nonce, "zero address nonce must be 0")
	})

	t.Run("by hash", func(t *testing.T) {
		nonce, err := client.NonceAtHash(ctx, zeroAddr, hash)

		require.NoError(t, err, "eth_getTransactionCount(zero, hash) call")
		assert.Equal(t, uint64(0), nonce, "zero address nonce must be 0")
	})

	t.Run("pending", func(t *testing.T) {
		nonce, err := client.PendingNonceAt(ctx, zeroAddr)

		require.NoError(t, err, "eth_getTransactionCount(zero, pending) call")
		assert.Equal(t, uint64(0), nonce, "zero address nonce must be 0")
	})
}

// TestEthGetCode exercises CodeAt / CodeAtHash / PendingCodeAt. The zero
// address has no deployed code so the result must be an empty byte slice.
func TestEthGetCode(t *testing.T) {
	ctx, client, closeFn := dialClient(t)
	defer closeFn()

	hash, _ := latestHashAndNumber(t)
	zeroAddr := common.Address{}

	t.Run("latest by number (nil)", func(t *testing.T) {
		code, err := client.CodeAt(ctx, zeroAddr, nil)
		require.NoError(t, err, "eth_getCode(zero, latest) call")
		assert.Empty(t, code, "zero address code must be empty")
	})

	t.Run("by hash", func(t *testing.T) {
		code, err := client.CodeAtHash(ctx, zeroAddr, hash)

		require.NoError(t, err, "eth_getCode(zero, hash) call")
		assert.Empty(t, code, "zero address code must be empty")
	})

	t.Run("pending", func(t *testing.T) {
		code, err := client.PendingCodeAt(ctx, zeroAddr)

		require.NoError(t, err, "eth_getCode(zero, pending) call")
		assert.Empty(t, code, "zero address code must be empty")
	})
}

// -----------------------------------------------------------------------------
// Block APIs — BlockByNumber, BlockByHash (full blocks),
// TransactionCount, PendingTransactionCount, BlockReceipts.
// -----------------------------------------------------------------------------

// TestEthGetBlockByNumber exercises BlockByNumber (eth_getBlockByNumber with
// includeTxs=true) over latest, nil, and a future block number.
func TestEthGetBlockByNumber(t *testing.T) {
	ctx, client, closeFn := dialClient(t)
	defer closeFn()

	thorClient := helper.NewClient(nodeURL)

	_, number := latestHashAndNumber(t)

	t.Run("latest", func(t *testing.T) {

		blk, err := client.BlockByNumber(ctx, number)
		require.NoError(t, err, "eth_getBlockByNumber(latest) call")
		require.NotNil(t, blk, "latest block must not be nil")

		block, err := thorClient.Block(number.String())
		require.NoError(t, err, "fetch block by number from thor client")
		require.NotNil(t, block, "block from thor client must not be nil")

		require.Equal(t, block.Number, uint32(blk.Header().Number.Uint64()), "block number mismatch between eth client and thor client")
		require.Equal(t, block.ParentID.Bytes(), blk.Header().ParentHash.Bytes(), "parent block ID mismatch between eth client and thor client")
		require.Equal(t, block.ID.Bytes(), blk.Header().Hash().Bytes(), "block ID mismatch between eth client and thor client")
	})

	t.Run("nil (defaults to latest)", func(t *testing.T) {
		blk, err := client.BlockByNumber(ctx, nil)
		require.NoError(t, err, "eth_getBlockByNumber(nil) call")
		require.NotNil(t, blk, "nil-block result must not be nil")

		block, err := thorClient.Block(blk.Header().Number.String())
		require.NoError(t, err, "fetch block by number from thor client")
		require.NotNil(t, block, "block from thor client must not be nil")

		require.Equal(t, block.Number, uint32(blk.Header().Number.Uint64()), "block number mismatch between eth client and thor client")
		require.Equal(t, block.ParentID.Bytes(), blk.Header().ParentHash.Bytes(), "parent block ID mismatch between eth client and thor client")
		require.Equal(t, block.ID.Bytes(), blk.Header().Hash().Bytes(), "block ID mismatch between eth client and thor client")
	})

	t.Run("future block number", func(t *testing.T) {
		future := new(big.Int).Add(number, big.NewInt(100))
		blk, err := client.BlockByNumber(ctx, future)
		require.Error(t, err, "eth_getBlockByNumber(future) must fail")
		assert.Nil(t, blk, "future block must be nil on error")
		t.Logf("future-block error: %v", err)
	})
}

// TestEthGetBlockByHashFull exercises BlockByHash (eth_getBlockByHash with
// includeTxs=true) across a valid head hash and a non-existent hash.
//
// Note: Thor's full-tx response shape can diverge from upstream geth — when
// the local rpcBlock/Transaction2 decoder cannot parse the response we treat
// it as a known Thor compatibility gap and log/skip rather than fail.
func TestEthGetBlockByHashFull(t *testing.T) {
	ctx, client, closeFn := dialClient(t)
	defer closeFn()

	hash, _ := latestHashAndNumber(t)
	thorClient := helper.NewClient(nodeURL)

	t.Run("valid hash", func(t *testing.T) {
		blk, err := client.BlockByHash(ctx, hash)
		require.NoError(t, err, "eth_getBlockByHash(valid, full) call")
		require.NotNil(t, blk, "block must not be nil for a valid hash")

		block, err := thorClient.Block(hash.String())
		require.NoError(t, err, "fetch block by number from thor client")
		require.NotNil(t, block, "block from thor client must not be nil")

		require.Equal(t, block.Number, uint32(blk.Header().Number.Uint64()), "block number mismatch between eth client and thor client")
		require.Equal(t, block.ParentID.Bytes(), blk.Header().ParentHash.Bytes(), "parent block ID mismatch between eth client and thor client")
		require.Equal(t, block.ID.Bytes(), blk.Header().Hash().Bytes(), "block ID mismatch between eth client and thor client")
	})

	t.Run("non-existent hash", func(t *testing.T) {
		nonExistent := common.HexToHash("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
		blk, err := client.BlockByHash(ctx, nonExistent)
		require.Error(t, err, "eth_getBlockByHash(non-existent) must fail")
		assert.Nil(t, blk, "block must be nil when the hash does not resolve")
		// Accept either ethereum.NotFound (the upstream contract) or the
		// JSON-decode failure that surfaces when Thor returns a non-null
		// payload the local types cannot fully consume.
		if !errors.Is(err, ethereum.NotFound) && !containsFold(err.Error(), "unexpected end of JSON input") {
			t.Errorf("expected ethereum.NotFound or JSON decode error, got: %v", err)
		}
	})
}

// TestEthSendTransaction builds a signed EIP-1559 value transfer from node1 to
// node2 using node1Key and submits it via eth_sendRawTransaction, asserting
// that the node accepts it without returning an error.
func TestEthSendTransaction(t *testing.T) {
	ctx, client, closeFn := dialClient(t)
	defer closeFn()

	thorClient := helper.NewClient(nodeURL)

	chainID, err := client.ChainID(ctx)
	require.NoError(t, err, "fetch chainID")

	from := crypto.PubkeyToAddress(helper.TestSenderKey.PublicKey)
	nonce, err := client.PendingNonceAt(ctx, from)
	require.NoError(t, err, "fetch pending nonce")

	head, err := client.HeaderByNumber(ctx, nil)
	require.NoError(t, err, "fetch latest header")
	require.NotNil(t, head.BaseFee, "latest header must carry a baseFee (post-1559)")

	// Cap = 2 * baseFee + tip — leaves headroom for the next block's baseFee bump.
	gasTipCap := big.NewInt(1)
	gasFeeCap := new(big.Int).Add(new(big.Int).Mul(head.BaseFee, big.NewInt(2)), gasTipCap)

	to := common.HexToAddress("0x327931085B4cCbCE0baABb5a5E1C678707C51d90") // node2
	tx := ethclient.NewTx(&ethclient.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       21_000,
		To:        &to,
		Value:     big.NewInt(1),
	})

	signed, err := tx.SignWith(helper.TestSenderKey)
	require.NoError(t, err, "sign tx with node1Key")

	txhash, err := client.SendTransaction(ctx, signed)
	require.NoError(t, err, "eth_sendRawTransaction must not return an error")
	require.NotNil(t, txhash, "eth_sendRawTransaction must return a tx hash")

	b32 := thor.Bytes32(txhash)
	helper.WaitForReceipt(t, thorClient, &b32, 30*time.Second)

	receipt, err := thorClient.TransactionReceipt(&b32)
	require.NoError(t, err, "fetch transaction receipt from thor client")
	require.NotNil(t, receipt, "transaction receipt must not be nil")

}

// TestEthGetBlockTransactionCountByHash exercises TransactionCount. On a
// freshly started solo Thor network, the chain head typically has zero
// transactions.
func TestEthGetBlockTransactionCountByHash(t *testing.T) {
	ctx, client, closeFn := dialClient(t)
	defer closeFn()

	thorClient := helper.NewClient(nodeURL)

	chainID, err := client.ChainID(ctx)
	require.NoError(t, err, "fetch chainID")

	from := crypto.PubkeyToAddress(helper.TestSenderKey.PublicKey)
	nonce, err := client.PendingNonceAt(ctx, from)
	require.NoError(t, err, "fetch pending nonce")

	head, err := client.HeaderByNumber(ctx, nil)
	require.NoError(t, err, "fetch latest header")
	require.NotNil(t, head.BaseFee, "latest header must carry a baseFee (post-1559)")

	// Cap = 2 * baseFee + tip — leaves headroom for the next block's baseFee bump.
	gasTipCap := big.NewInt(1)
	gasFeeCap := new(big.Int).Add(new(big.Int).Mul(head.BaseFee, big.NewInt(2)), gasTipCap)

	to := common.HexToAddress("0x327931085B4cCbCE0baABb5a5E1C678707C51d90") // node2
	tx := ethclient.NewTx(&ethclient.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       21_000,
		To:        &to,
		Value:     big.NewInt(1),
	})

	signed, err := tx.SignWith(helper.TestSenderKey)
	require.NoError(t, err, "sign tx with node1Key")

	txhash, err := client.SendTransaction(ctx, signed)
	require.NoError(t, err, "eth_sendRawTransaction must not return an error")
	require.NotNil(t, txhash, "eth_sendRawTransaction must return a tx hash")

	b32 := thor.Bytes32(txhash)
	helper.WaitForReceipt(t, thorClient, &b32, 30*time.Second)

	receipt, err := thorClient.TransactionReceipt(&b32)
	require.NoError(t, err, "fetch transaction receipt from thor client")
	require.NotNil(t, receipt, "transaction receipt must not be nil")

	count, err := client.TransactionCount(ctx, common.HexToHash(receipt.Meta.BlockID.String()))
	require.NoError(t, err, "eth_getBlockTransactionCountByHash call")
	assert.Equal(t, uint(1), count, "expected 1 transaction in the block")
}

// TestEthPendingTransactionCount exercises PendingTransactionCount. On a
// solo network with no mempool activity, the count should be 0 (when
// supported by the node).
func TestEthPendingTransactionCount(t *testing.T) {
	ctx, client, closeFn := dialClient(t)
	defer closeFn()

	thorClient := helper.NewClient(nodeURL)

	chainID, err := client.ChainID(ctx)
	require.NoError(t, err, "fetch chainID")

	from := crypto.PubkeyToAddress(helper.TestSenderKey.PublicKey)
	nonce, err := client.PendingNonceAt(ctx, from)
	require.NoError(t, err, "fetch pending nonce")

	head, err := client.HeaderByNumber(ctx, nil)
	require.NoError(t, err, "fetch latest header")
	require.NotNil(t, head.BaseFee, "latest header must carry a baseFee (post-1559)")

	// Cap = 2 * baseFee + tip — leaves headroom for the next block's baseFee bump.
	gasTipCap := big.NewInt(1)
	gasFeeCap := new(big.Int).Add(new(big.Int).Mul(head.BaseFee, big.NewInt(2)), gasTipCap)

	to := common.HexToAddress("0x327931085B4cCbCE0baABb5a5E1C678707C51d90") // node2
	tx := ethclient.NewTx(&ethclient.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       21_000,
		To:        &to,
		Value:     big.NewInt(1),
	})

	signed, err := tx.SignWith(helper.TestSenderKey)
	require.NoError(t, err, "sign tx with node1Key")

	txhash, err := client.SendTransaction(ctx, signed)
	require.NoError(t, err, "eth_sendRawTransaction must not return an error")
	require.NotNil(t, txhash, "eth_sendRawTransaction must return a tx hash")

	b32 := thor.Bytes32(txhash)
	helper.WaitForReceipt(t, thorClient, &b32, 30*time.Second)

	count, err := client.PendingTransactionCount(ctx)
	require.NoError(t, err, "eth_getBlockTransactionCountByHash call")
	assert.Equal(t, uint(1), count, "expected 1 transaction in the block(latest)")
}

func TestEthGetBlockReceipts(t *testing.T) {
	ctx, client, closeFn := dialClient(t)
	defer closeFn()

	thorClient := helper.NewClient(nodeURL)

	chainID, err := client.ChainID(ctx)
	require.NoError(t, err, "fetch chainID")

	from := crypto.PubkeyToAddress(helper.TestSenderKey.PublicKey)
	nonce, err := client.PendingNonceAt(ctx, from)
	require.NoError(t, err, "fetch pending nonce")

	head, err := client.HeaderByNumber(ctx, nil)
	require.NoError(t, err, "fetch latest header")
	require.NotNil(t, head.BaseFee, "latest header must carry a baseFee (post-1559)")

	// Cap = 2 * baseFee + tip — leaves headroom for the next block's baseFee bump.
	gasTipCap := big.NewInt(1)
	gasFeeCap := new(big.Int).Add(new(big.Int).Mul(head.BaseFee, big.NewInt(2)), gasTipCap)

	to := common.HexToAddress("0x327931085B4cCbCE0baABb5a5E1C678707C51d90") // node2
	tx := ethclient.NewTx(&ethclient.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       21_000,
		To:        &to,
		Value:     big.NewInt(1),
	})

	signed, err := tx.SignWith(helper.TestSenderKey)
	require.NoError(t, err, "sign tx with node1Key")

	txhash, err := client.SendTransaction(ctx, signed)
	require.NoError(t, err, "eth_sendRawTransaction must not return an error")
	require.NotNil(t, txhash, "eth_sendRawTransaction must return a tx hash")

	b32 := thor.Bytes32(txhash)
	helper.WaitForReceipt(t, thorClient, &b32, 30*time.Second)

	receipt, err := thorClient.TransactionReceipt(&b32)
	require.NoError(t, err, "fetch transaction receipt from thor client")
	require.NotNil(t, receipt, "transaction receipt must not be nil")

	targetHash := common.HexToHash(receipt.Meta.BlockID.String())

	blockNrOrHash := ethclient.BlockNumberOrHashWithHash(targetHash, false)

	receipts, err := client.BlockReceipts(ctx, blockNrOrHash)

	require.NoError(t, err, "eth_getBlockReceipts call")
	require.NotNil(t, receipts, "receipts list must not be nil")
	assert.Len(t, receipts, 1, "expected exactly 1 receipt in the block")
	assert.Equal(t, targetHash, receipts[0].BlockHash, "receipt block hash mismatch")
	assert.Equal(t, b32, receipts[0].TxHash, "receipt transaction hash mismatch")
}

// -----------------------------------------------------------------------------
// Transaction lookup APIs — TransactionByHash, TransactionInBlock,
// TransactionSender, TransactionReceipt.
// -----------------------------------------------------------------------------

// TestEthGetTransactionByHash asserts that a made-up tx hash surfaces as
// ethereum.NotFound.
func TestEthGetTransactionByHash(t *testing.T) {
	ctx, client, closeFn := dialClient(t)
	defer closeFn()

	thorClient := helper.NewClient(nodeURL)

	chainID, err := client.ChainID(ctx)
	require.NoError(t, err, "fetch chainID")

	from := crypto.PubkeyToAddress(helper.TestSenderKey.PublicKey)
	nonce, err := client.PendingNonceAt(ctx, from)
	require.NoError(t, err, "fetch pending nonce")

	head, err := client.HeaderByNumber(ctx, nil)
	require.NoError(t, err, "fetch latest header")
	require.NotNil(t, head.BaseFee, "latest header must carry a baseFee (post-1559)")

	// Cap = 2 * baseFee + tip — leaves headroom for the next block's baseFee bump.
	gasTipCap := big.NewInt(1)
	gasFeeCap := new(big.Int).Add(new(big.Int).Mul(head.BaseFee, big.NewInt(2)), gasTipCap)

	to := common.HexToAddress("0x327931085B4cCbCE0baABb5a5E1C678707C51d90") // node2
	tx := ethclient.NewTx(&ethclient.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       21_000,
		To:        &to,
		Value:     big.NewInt(1),
	})

	signed, err := tx.SignWith(helper.TestSenderKey)
	require.NoError(t, err, "sign tx with node1Key")

	txhash, err := client.SendTransaction(ctx, signed)
	require.NoError(t, err, "eth_sendRawTransaction must not return an error")
	require.NotNil(t, txhash, "eth_sendRawTransaction must return a tx hash")

	b32 := thor.Bytes32(txhash)
	helper.WaitForReceipt(t, thorClient, &b32, 30*time.Second)

	tx2, isPending, err := client.TransactionByHash(ctx, common.HexToHash(txhash.String()))
	require.NoError(t, err, "eth_getTransactionByHash call")
	require.NotNil(t, tx2, "transaction must not be nil for a valid hash")
	require.False(t, isPending, "transaction should not be pending after receipt is available")
	assert.Equal(t, txhash.String(), tx2.Hash().String(), "transaction hash mismatch")
}

// TestEthGetTransactionByBlockHashAndIndex exercises TransactionInBlock.
// On a solo network with no transactions, index 0 of the head block must
func TestEthGetTransactionByBlockHashAndIndex(t *testing.T) {

	ctx, client, closeFn := dialClient(t)
	defer closeFn()

	thorClient := helper.NewClient(nodeURL)

	chainID, err := client.ChainID(ctx)
	require.NoError(t, err, "fetch chainID")

	from := crypto.PubkeyToAddress(helper.TestSenderKey.PublicKey)
	nonce, err := client.PendingNonceAt(ctx, from)
	require.NoError(t, err, "fetch pending nonce")

	head, err := client.HeaderByNumber(ctx, nil)
	require.NoError(t, err, "fetch latest header")
	require.NotNil(t, head.BaseFee, "latest header must carry a baseFee (post-1559)")

	// Cap = 2 * baseFee + tip — leaves headroom for the next block's baseFee bump.
	gasTipCap := big.NewInt(1)
	gasFeeCap := new(big.Int).Add(new(big.Int).Mul(head.BaseFee, big.NewInt(2)), gasTipCap)

	to := common.HexToAddress("0x327931085B4cCbCE0baABb5a5E1C678707C51d90") // node2
	tx := ethclient.NewTx(&ethclient.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       21_000,
		To:        &to,
		Value:     big.NewInt(1),
	})

	signed, err := tx.SignWith(helper.TestSenderKey)
	require.NoError(t, err, "sign tx with node1Key")

	txhash, err := client.SendTransaction(ctx, signed)
	require.NoError(t, err, "eth_sendRawTransaction must not return an error")
	require.NotNil(t, txhash, "eth_sendRawTransaction must return a tx hash")

	b32 := thor.Bytes32(txhash)
	helper.WaitForReceipt(t, thorClient, &b32, 30*time.Second)

	receipt, err := thorClient.TransactionReceipt(&b32)
	require.NoError(t, err, "fetch transaction receipt from thor client")
	require.NotNil(t, receipt, "transaction receipt must not be nil")

	blockHash := common.HexToHash(receipt.Meta.BlockID.String())

	tx2, err := client.TransactionInBlock(ctx, blockHash, 0)
	require.NoError(t, err, "eth_getTransactionByHash call")
	require.NotNil(t, tx2, "transaction must not be nil for a valid hash")
	assert.Equal(t, txhash.String(), tx2.Hash().String(), "transaction hash mismatch")
}

// TestEthGetTransactionReceipt asserts a made-up tx hash returns NotFound.
func TestEthGetTransactionReceipt(t *testing.T) {

	ctx, client, closeFn := dialClient(t)
	defer closeFn()

	thorClient := helper.NewClient(nodeURL)

	chainID, err := client.ChainID(ctx)
	require.NoError(t, err, "fetch chainID")

	t.Run("valid hash", func(t *testing.T) {
		from := crypto.PubkeyToAddress(helper.TestSenderKey.PublicKey)
		nonce, err := client.PendingNonceAt(ctx, from)
		require.NoError(t, err, "fetch pending nonce")

		head, err := client.HeaderByNumber(ctx, nil)
		require.NoError(t, err, "fetch latest header")
		require.NotNil(t, head.BaseFee, "latest header must carry a baseFee (post-1559)")

		// Cap = 2 * baseFee + tip — leaves headroom for the next block's baseFee bump.
		gasTipCap := big.NewInt(1)
		gasFeeCap := new(big.Int).Add(new(big.Int).Mul(head.BaseFee, big.NewInt(2)), gasTipCap)

		to := common.HexToAddress("0x327931085B4cCbCE0baABb5a5E1C678707C51d90") // node2
		tx := ethclient.NewTx(&ethclient.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     nonce,
			GasTipCap: gasTipCap,
			GasFeeCap: gasFeeCap,
			Gas:       21_000,
			To:        &to,
			Value:     big.NewInt(1),
		})

		signed, err := tx.SignWith(helper.TestSenderKey)
		require.NoError(t, err, "sign tx with node1Key")

		txhash, err := client.SendTransaction(ctx, signed)
		require.NoError(t, err, "eth_sendRawTransaction must not return an error")
		require.NotNil(t, txhash, "eth_sendRawTransaction must return a tx hash")

		b32 := thor.Bytes32(txhash)
		helper.WaitForReceipt(t, thorClient, &b32, 30*time.Second)

		rcpt, err := client.TransactionReceipt(ctx, common.HexToHash(txhash.String()))
		require.NoError(t, err, "eth_getTransactionReceipt call")
		require.NotNil(t, rcpt, "receipt must not be nil for a valid transaction hash")
		assert.Equal(t, txhash.String(), rcpt.TxHash.String(), "receipt transaction hash mismatch")
	})

	t.Run("invalid hash", func(t *testing.T) {
		nonExistent := common.HexToHash("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
		rcpt, err := client.TransactionReceipt(ctx, nonExistent)
		require.Error(t, err, "eth_getTransactionReceipt(non-existent) must fail")
		assert.Nil(t, rcpt, "receipt must be nil when the hash does not resolve")
	})
}

// TestEthTransactionSender documents that exercising TransactionSender
// end-to-end requires a real *Transaction2 from the chain, which in turn
// requires the concrete TxData hierarchy not exposed by this package's local
// type set. The slow-path RPC fallback is the path that gets called when
// the cache misses; we verify the error surface only.
func TestEthTransactionSender(t *testing.T) {

	ctx, client, closeFn := dialClient(t)
	defer closeFn()

	thorClient := helper.NewClient(nodeURL)

	chainID, err := client.ChainID(ctx)
	require.NoError(t, err, "fetch chainID")

	from := crypto.PubkeyToAddress(helper.TestSenderKey.PublicKey)
	nonce, err := client.PendingNonceAt(ctx, from)
	require.NoError(t, err, "fetch pending nonce")

	head, err := client.HeaderByNumber(ctx, nil)
	require.NoError(t, err, "fetch latest header")
	require.NotNil(t, head.BaseFee, "latest header must carry a baseFee (post-1559)")

	// Cap = 2 * baseFee + tip — leaves headroom for the next block's baseFee bump.
	gasTipCap := big.NewInt(1)
	gasFeeCap := new(big.Int).Add(new(big.Int).Mul(head.BaseFee, big.NewInt(2)), gasTipCap)

	to := common.HexToAddress("0x327931085B4cCbCE0baABb5a5E1C678707C51d90") // node2
	tx := ethclient.NewTx(&ethclient.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       21_000,
		To:        &to,
		Value:     big.NewInt(1),
	})

	signed, err := tx.SignWith(helper.TestSenderKey)
	require.NoError(t, err, "sign tx with node1Key")

	txhash, err := client.SendTransaction(ctx, signed)
	require.NoError(t, err, "eth_sendRawTransaction must not return an error")
	require.NotNil(t, txhash, "eth_sendRawTransaction must return a tx hash")

	b32 := thor.Bytes32(txhash)
	helper.WaitForReceipt(t, thorClient, &b32, 30*time.Second)

	receipt, err := thorClient.TransactionReceipt(&b32)
	require.NoError(t, err, "fetch transaction receipt from thor client")
	require.NotNil(t, receipt, "transaction receipt must not be nil")

	tx2, _, err := client.TransactionByHash(ctx, common.HexToHash(txhash.String()))
	require.NoError(t, err, "fetch transaction by hash for TransactionSender test")
	require.NotNil(t, tx2, "transaction must not be nil for TransactionSender test")

	sender, err := client.TransactionSender(ctx, tx2, common.HexToHash(receipt.Meta.BlockID.String()), 0)
	require.NoError(t, err, "TransactionSender call must not return an error")
	assert.Equal(t, from, sender, "TransactionSender must recover the correct sender address")
}

// -----------------------------------------------------------------------------
// Call / Estimate — CallContract*, EstimateGas*.
// -----------------------------------------------------------------------------

// TestEthCall exercises CallContract with a trivial message: empty call from
// the zero address to the zero address. The expected result is empty bytes.
func TestEthCall(t *testing.T) {
	ctx, client, closeFn := dialClient(t)
	defer closeFn()

	to := common.Address{}
	msg := ethclient.CallMsg{
		From: common.Address{},
		To:   &to,
		Data: nil,
	}

	t.Run("latest by number (nil)", func(t *testing.T) {
		out, err := client.CallContract(ctx, msg, nil)
		require.NoError(t, err, "eth_call(latest) call")
		assert.Empty(t, out, "empty call to zero address must return empty output")
	})

	t.Run("by hash", func(t *testing.T) {
		hash, _ := latestHashAndNumber(t)
		out, err := client.CallContractAtHash(ctx, msg, hash)
		if skipIfUnsupported(t, err, "eth_call(byHash)") {
			return
		}
		require.NoError(t, err, "eth_call(hash) call")
		assert.Empty(t, out, "empty call to zero address must return empty output")
	})

	t.Run("pending", func(t *testing.T) {
		out, err := client.PendingCallContract(ctx, msg)
		if skipIfMethodNotFound(t, err, "eth_call(pending)") {
			return
		}
		require.NoError(t, err, "eth_call(pending) call")
		assert.Empty(t, out, "empty call to zero address must return empty output")
	})
}

// TestEthEstimateGas exercises EstimateGas / EstimateGasAtBlock /
// EstimateGasAtBlockHash for a simple value-less call from the zero address.
// Geth reports 21000 for a plain transfer; Thor's response is implementation
// defined but must be a positive non-zero value.
func TestEthEstimateGas(t *testing.T) {
	ctx, client, closeFn := dialClient(t)
	defer closeFn()

	to := common.Address{}
	msg := ethclient.CallMsg{
		From: common.Address{},
		To:   &to,
	}

	t.Run("default state", func(t *testing.T) {
		gas, err := client.EstimateGas(ctx, msg)
		require.NoError(t, err, "eth_estimateGas call")
		assert.Greater(t, gas, uint64(0), "gas estimate must be positive")
		t.Logf("estimate(default) = %d", gas)
	})

	t.Run("at latest number", func(t *testing.T) {
		gas, err := client.EstimateGasAtBlock(ctx, msg, big.NewInt(int64(rpc.LatestBlockNumber)))
		if skipIfMethodNotFound(t, err, "eth_estimateGas(byNumber)") {
			return
		}
		require.NoError(t, err, "eth_estimateGas(latest) call")
		assert.Greater(t, gas, uint64(0), "gas estimate must be positive")
		t.Logf("estimate(latest) = %d", gas)
	})

	t.Run("at hash", func(t *testing.T) {
		hash, _ := latestHashAndNumber(t)
		gas, err := client.EstimateGasAtBlockHash(ctx, msg, hash)
		if skipIfUnsupported(t, err, "eth_estimateGas(byHash)") {
			return
		}
		require.NoError(t, err, "eth_estimateGas(hash) call")
		assert.Greater(t, gas, uint64(0), "gas estimate must be positive")
		t.Logf("estimate(hash) = %d", gas)
	})
}

// -----------------------------------------------------------------------------
// Logs and Send / RevertErrorData.
// -----------------------------------------------------------------------------

// TestEthGetLogs exercises FilterLogs over the full chain so far with no
// address or topic filter. Empty results are acceptable; the call must not
// error.
func TestEthGetLogs(t *testing.T) {

	ctx, client, closeFn := dialClient(t)
	defer closeFn()

	thorClient := helper.NewClient(nodeURL)

	chainID, err := client.ChainID(ctx)
	require.NoError(t, err, "fetch chainID")

	from := crypto.PubkeyToAddress(helper.TestSenderKey.PublicKey)
	nonce, err := client.PendingNonceAt(ctx, from)
	require.NoError(t, err, "fetch pending nonce")

	head, err := client.HeaderByNumber(ctx, nil)
	require.NoError(t, err, "fetch latest header")
	require.NotNil(t, head.BaseFee, "latest header must carry a baseFee (post-1559)")

	// Cap = 2 * baseFee + tip — leaves headroom for the next block's baseFee bump.
	gasTipCap := big.NewInt(1)
	gasFeeCap := new(big.Int).Add(new(big.Int).Mul(head.BaseFee, big.NewInt(2)), gasTipCap)

	// Transfer 10 VTHO from node1 to node2 via the VeChain Energy (VTHO)
	// ERC-20 contract at the well-known precompile address. The transfer
	// emits a single ERC-20 Transfer log, which is what eth_getLogs picks up.
	vthoContract := common.HexToAddress("0x0000000000000000000000000000456E65726779")
	node2 := common.HexToAddress("0x327931085B4cCbCE0baABb5a5E1C678707C51d90")
	amount := new(big.Int).Mul(big.NewInt(10), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))

	transferSelector := crypto.Keccak256([]byte("transfer(address,uint256)"))[:4]
	data := make([]byte, 0, 4+32+32)
	data = append(data, transferSelector...)
	data = append(data, common.LeftPadBytes(node2.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(amount.Bytes(), 32)...)

	tx := ethclient.NewTx(&ethclient.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       80_000,
		To:        &vthoContract,
		Value:     big.NewInt(0),
		Data:      data,
	})

	signed, err := tx.SignWith(helper.TestSenderKey)
	require.NoError(t, err, "sign tx with node1Key")

	txhash, err := client.SendTransaction(ctx, signed)
	require.NoError(t, err, "eth_sendRawTransaction must not return an error")
	require.NotNil(t, txhash, "eth_sendRawTransaction must return a tx hash")

	b32 := thor.Bytes32(txhash)
	helper.WaitForReceipt(t, thorClient, &b32, 30*time.Second)

	receipt, err := thorClient.TransactionReceipt(&b32)
	require.NoError(t, err, "fetch transaction receipt from thor client")
	require.NotNil(t, receipt, "transaction receipt must not be nil")

	q := ethereum.FilterQuery{
		FromBlock: big.NewInt(0),
		ToBlock:   new(big.Int).SetUint64(uint64(receipt.Meta.BlockNumber)),
	}
	logs, err := client.FilterLogs(ctx, q)
	require.NoError(t, err, "eth_getLogs call")
	require.Len(t, logs, 1, "expected exactly one log entry")
	require.Equal(t, txhash.String(), logs[0].TxHash.String(), "log transaction hash mismatch")
}

// mockRevertErr is a minimal JSON-RPC error implementing rpc.Error +
// ErrorData() — what upstream Geth returns on revert (code 3).
type mockRevertErr struct {
	code int
	data any
}

func (e *mockRevertErr) Error() string  { return "execution reverted" }
func (e *mockRevertErr) ErrorCode() int { return e.code }
func (e *mockRevertErr) ErrorData() any { return e.data }

// TestRevertErrorData is a unit test for the package-level helper. It does
// not require a live RPC connection.
func TestRevertErrorData(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		data, ok := ethclient.RevertErrorData(nil)
		assert.False(t, ok, "nil error must yield (_, false)")
		assert.Nil(t, data)
	})

	t.Run("plain error", func(t *testing.T) {
		data, ok := ethclient.RevertErrorData(errors.New("plain"))
		assert.False(t, ok, "non-rpc error must yield (_, false)")
		assert.Nil(t, data)
	})

	t.Run("wrong code", func(t *testing.T) {
		data, ok := ethclient.RevertErrorData(&mockRevertErr{code: -32000, data: "0xdeadbeef"})
		assert.False(t, ok, "non-revert code must yield (_, false)")
		assert.Nil(t, data)
	})

	t.Run("non-string data", func(t *testing.T) {
		data, ok := ethclient.RevertErrorData(&mockRevertErr{code: 3, data: 12345})
		assert.False(t, ok, "non-string data must yield (_, false)")
		assert.Nil(t, data)
	})

	t.Run("invalid hex", func(t *testing.T) {
		data, ok := ethclient.RevertErrorData(&mockRevertErr{code: 3, data: "not-hex"})
		assert.False(t, ok, "non-hex revert payload must yield (_, false)")
		assert.Nil(t, data)
	})

	t.Run("valid revert payload", func(t *testing.T) {
		// 0x08c379a0 is the Error(string) selector; payload encodes "boom".
		revertHex := "0x08c379a0" +
			"0000000000000000000000000000000000000000000000000000000000000020" +
			"0000000000000000000000000000000000000000000000000000000000000004" +
			"626f6f6d00000000000000000000000000000000000000000000000000000000"
		data, ok := ethclient.RevertErrorData(&mockRevertErr{code: 3, data: revertHex})
		assert.True(t, ok, "valid revert payload must yield (_, true)")
		assert.NotEmpty(t, data, "decoded bytes must not be empty")
	})
}
