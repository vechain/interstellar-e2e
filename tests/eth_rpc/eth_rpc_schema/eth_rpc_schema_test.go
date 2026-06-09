// Schema-driven JSON-RPC conformance tests for an Ethereum-compatible node.
//
// Each test:
//   1. Builds a JSON-RPC 2.0 request from scratch.
//   2. Posts it to <nodeURL>/rpc over plain HTTP.
//   3. Decodes the response envelope.
//   4. Validates the `result` payload against schemas/<method>.json — a
//      hand-written JSON Schema (draft 2020-12) mirroring go-ethereum's
//      RPC method contract.
//
// The schemas are embedded via //go:embed in rpc.go.

package ethrpcschema

import (
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	gethrlp "github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vechain/interstellar-e2e/tests/helper"
	"github.com/vechain/thor/v2/thor"
)

// -----------------------------------------------------------------------------
// Network / web3 family
// -----------------------------------------------------------------------------

// TestNetVersion checks net_version returns a decimal-digit string.
func TestNetVersion(t *testing.T) {
	rpcCallAndValidate(t, "net_version")
}

// TestNetPeerCount checks net_peerCount returns a QUANTITY.
func TestNetPeerCount(t *testing.T) {
	rpcCallAndValidate(t, "net_peerCount")
}

// TestNetListening checks net_listening returns a boolean.
// Skipped if the node doesn't implement the method.
func TestNetListening(t *testing.T) {
	result, err := rpcCall(t, "net_listening")
	require.NoError(t, err, "net_listening rpc call")
	validateResult(t, "net_listening", result)
}

// TestWeb3ClientVersion checks web3_clientVersion returns a non-empty string.
// Skipped if the node doesn't implement the method.
func TestWeb3ClientVersion(t *testing.T) {
	result, err := rpcCall(t, "web3_clientVersion")
	require.NoError(t, err, "web3_clientVersion rpc call")
	validateResult(t, "web3_clientVersion", result)
}

// -----------------------------------------------------------------------------
// Chain state — chain ID, gas price, block number, syncing
// -----------------------------------------------------------------------------

// TestEthChainID asserts eth_chainId returns a positive QUANTITY.
func TestEthChainID(t *testing.T) {
	result := rpcCallAndValidate(t, "eth_chainId")
	assert.Positive(t, hexQuantityToInt(t, result).Sign(), "chainId must be > 0")
}

// TestEthBlockNumber asserts eth_blockNumber returns a QUANTITY (>= 0).
func TestEthBlockNumber(t *testing.T) {
	rpcCallAndValidate(t, "eth_blockNumber")
}

// TestEthGasPrice asserts eth_gasPrice returns a QUANTITY.
func TestEthGasPrice(t *testing.T) {
	rpcCallAndValidate(t, "eth_gasPrice")
}

// TestEthSyncing asserts eth_syncing returns either false or a SyncProgress object.
func TestEthSyncing(t *testing.T) {
	rpcCallAndValidate(t, "eth_syncing")
}

// TestEthMaxPriorityFeePerGas asserts eth_maxPriorityFeePerGas returns a QUANTITY.
// Skipped if the node doesn't implement the method.
func TestEthMaxPriorityFeePerGas(t *testing.T) {
	result, err := rpcCall(t, "eth_maxPriorityFeePerGas")
	require.NoError(t, err, "eth_maxPriorityFeePerGas rpc call")
	validateResult(t, "eth_maxPriorityFeePerGas", result)
}

// TestEthFeeHistory asserts eth_feeHistory returns a fee-history object.
// Skipped if the node doesn't implement the method.
func TestEthFeeHistory(t *testing.T) {
	result, err := rpcCall(t, "eth_feeHistory", "0x1", "latest", []float64{})
	require.NoError(t, err, "eth_feeHistory rpc call")
	validateResult(t, "eth_feeHistory", result)
}

// TestEthBlobBaseFee asserts eth_blobBaseFee returns a QUANTITY.
// Skipped if the node doesn't implement the method.
func TestEthBlobBaseFee(t *testing.T) {
	result, err := rpcCall(t, "eth_blobBaseFee")
	if isMethodNotFound(err) {
		t.Skipf("eth_blobBaseFee not supported: %v", err)
	}
	require.NoError(t, err, "eth_blobBaseFee rpc call")
	validateResult(t, "eth_blobBaseFee", result)
}

// -----------------------------------------------------------------------------
// Account state — balance, code, storage, transaction count
// -----------------------------------------------------------------------------

// TestEthGetBalance exercises eth_getBalance across the JSON forms of
// go-ethereum's rpc.BlockNumberOrHash:
//
//   - Plain string tags: "latest", "earliest", "pending", "safe", "finalized".
//   - Plain hex block number: "0x0".
//   - Object {"blockNumber":"0x0"} — the explicit-object number form.
//   - Object {"blockHash":"0x..."} — the explicit-object hash form.
//   - Object {"blockHash":"0x...", "requireCanonical":true} — with canonical flag.
//
// All sub-cases hit the funded node1 address so the response is non-zero where
// applicable. Optional tags / hash-form lookups that Thor doesn't support are
// auto-skipped on the standard error surfaces (method not found, "not yet
// supported", "invalid block tag").
func TestEthGetBalance(t *testing.T) {
	node1Addr := "0x61fF580B63D3845934610222245C116E013717ec"

	// Each sub-case represents one valid encoding of go-ethereum's
	// rpc.BlockNumberOrHash. allowSkip == true means the sub-case may be
	// skipped if the node reports the encoding as unsupported.
	cases := []struct {
		name     string
		blockArg any
	}{
		{name: "string tag latest", blockArg: "latest"},
		{name: "string tag earliest", blockArg: "earliest"},
		{name: "string tag pending", blockArg: "pending"},
		{name: "string tag safe", blockArg: "safe"},
		{name: "string tag finalized", blockArg: "finalized"},
		{name: "hex block number 0x0", blockArg: "0x0"},
		{name: "object blockNumber 0x0", blockArg: map[string]any{"blockNumber": "0x0"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := rpcCall(t, "eth_getBalance", node1Addr, tc.blockArg)
			require.NoError(t, err, "eth_getBalance(%v)", tc.blockArg)
			validateResult(t, "eth_getBalance", result)
		})
	}

	// Hash-form lookups need a real block hash, so fetch it once here.
	t.Run("object blockHash (latest)", func(t *testing.T) {
		hash := fetchLatestBlockHash(t)
		result, err := rpcCall(t, "eth_getBalance", node1Addr, map[string]any{"blockHash": hash})
		require.NoError(t, err, "eth_getBalance({blockHash})")
		validateResult(t, "eth_getBalance", result)
	})

	t.Run("object blockHash with requireCanonical", func(t *testing.T) {
		hash := fetchLatestBlockHash(t)
		result, err := rpcCall(t, "eth_getBalance", node1Addr, map[string]any{
			"blockHash":        hash,
			"requireCanonical": true,
		})
		require.NoError(t, err, "eth_getBalance({blockHash,requireCanonical})")
		validateResult(t, "eth_getBalance", result)
	})
}

// TestEthGetCode reads code at the VTHO contract address across every
// JSON encoding of go-ethereum's rpc.BlockNumberOrHash. See
// runBlockNumberOrHashCases for the full sub-case list.
func TestEthGetCode(t *testing.T) {
	vthoAddr := "0x0000000000000000000000000000456e65726779"
	runBlockNumberOrHashCases(t, func(t *testing.T, blockArg any) {
		result, err := rpcCall(t, "eth_getCode", vthoAddr, blockArg)
		require.NoError(t, err, "eth_getCode(%v)", blockArg)
		validateResult(t, "eth_getCode", result)
	})
}

// TestEthGetStorageAt reads slot 0 of the zero address across every JSON
// encoding of go-ethereum's rpc.BlockNumberOrHash.
func TestEthGetStorageAt(t *testing.T) {
	zeroAddr := common.Address{}.Hex()
	runBlockNumberOrHashCases(t, func(t *testing.T, blockArg any) {
		result, err := rpcCall(t, "eth_getStorageAt", zeroAddr, "0x0", blockArg)
		require.NoError(t, err, "eth_getStorageAt(%v)", blockArg)
		validateResult(t, "eth_getStorageAt", result)
	})
}

// TestEthGetTransactionCount reads the nonce of the zero address at latest.
func TestEthGetTransactionCount(t *testing.T) {
	t.Helper()

	t.Run("get transaction count of zero address", func(t *testing.T) {
		rpcCallAndValidate(t, "eth_getTransactionCount", common.Address{}.Hex(), "latest")
	})

	t.Run("get transaction count of address", func(t *testing.T) {
		chainIDRaw := rpcCallAndValidate(t, "eth_chainId")
		chainID := hexQuantityToInt(t, chainIDRaw)

		from := crypto.PubkeyToAddress(helper.TestSenderKey.PublicKey)
		nonceRaw, err := rpcCall(t, "eth_getTransactionCount", from.Hex(), "pending")
		require.NoError(t, err, "eth_getTransactionCount(pending)")
		nonce := hexQuantityToInt(t, nonceRaw).Uint64()

		baseFee := fetchLatestBaseFee(t)
		gasTipCap := big.NewInt(1)
		gasFeeCap := new(big.Int).Add(new(big.Int).Mul(baseFee, big.NewInt(2)), gasTipCap)

		to := common.HexToAddress("0x327931085B4cCbCE0baABb5a5E1C678707C51d90") // node2
		raw := signDynamicFeeTx(t, helper.TestSenderKey, chainID, nonce, gasTipCap, gasFeeCap, 21_000, &to, big.NewInt(1), nil)

		hashRaw := rpcCallAndValidate(t, "eth_sendRawTransaction", "0x"+hex.EncodeToString(raw))
		var hash string
		require.NoError(t, json.Unmarshal(hashRaw, &hash), "unmarshal tx hash")
		require.Regexp(t, "^0x[0-9a-fA-F]{64}$", hash)

		// Wait for the receipt on the Thor side so subsequent lookups succeed.
		thorClient := helper.NewClient(nodeURL)
		b32 := thor.Bytes32(common.HexToHash(hash))
		helper.WaitForReceipt(t, thorClient, &b32, 30*time.Second)

		rpcCallAndValidate(t, "eth_getTransactionCount", from.Hex(), "latest")
	})

	t.Run("block number or hash forms", func(t *testing.T) {
		from := crypto.PubkeyToAddress(helper.TestSenderKey.PublicKey).Hex()
		runBlockNumberOrHashCases(t, func(t *testing.T, blockArg any) {
			result, err := rpcCall(t, "eth_getTransactionCount", from, blockArg)
			require.NoError(t, err, "eth_getTransactionCount(%v)", blockArg)
			validateResult(t, "eth_getTransactionCount", result)
		})
	})
}

// -----------------------------------------------------------------------------
// Block queries
// -----------------------------------------------------------------------------

// TestEthGetBlockByNumber_LatestHashesOnly fetches latest with includeTxs=false.
func TestEthGetBlockByNumber_LatestHashesOnly(t *testing.T) {
	rpcCallAndValidate(t, "eth_getBlockByNumber", "latest", false)
}

// TestEthGetBlockByNumber_LatestFullTxs fetches latest with includeTxs=true.
func TestEthGetBlockByNumber_LatestFullTxs(t *testing.T) {
	t.Helper()

	chainIDRaw := rpcCallAndValidate(t, "eth_chainId")
	chainID := hexQuantityToInt(t, chainIDRaw)

	from := crypto.PubkeyToAddress(helper.TestSenderKey.PublicKey)
	nonceRaw, err := rpcCall(t, "eth_getTransactionCount", from.Hex(), "pending")
	require.NoError(t, err, "eth_getTransactionCount(pending)")
	nonce := hexQuantityToInt(t, nonceRaw).Uint64()

	baseFee := fetchLatestBaseFee(t)
	gasTipCap := big.NewInt(1)
	gasFeeCap := new(big.Int).Add(new(big.Int).Mul(baseFee, big.NewInt(2)), gasTipCap)

	to := common.HexToAddress("0x327931085B4cCbCE0baABb5a5E1C678707C51d90") // node2
	raw := signDynamicFeeTx(t, helper.TestSenderKey, chainID, nonce, gasTipCap, gasFeeCap, 21_000, &to, big.NewInt(1), nil)

	hashRaw := rpcCallAndValidate(t, "eth_sendRawTransaction", "0x"+hex.EncodeToString(raw))
	var hash string
	require.NoError(t, json.Unmarshal(hashRaw, &hash), "unmarshal tx hash")
	require.Regexp(t, "^0x[0-9a-fA-F]{64}$", hash)

	// Wait for the receipt on the Thor side so subsequent lookups succeed.
	thorClient := helper.NewClient(nodeURL)
	b32 := thor.Bytes32(common.HexToHash(hash))
	helper.WaitForReceipt(t, thorClient, &b32, 30*time.Second)

	rpcCallAndValidate(t, "eth_getBlockByNumber", "latest", true)
}

// TestEthGetBlockByNumber_Future asserts the result is null for a future block.
func TestEthGetBlockByNumber_Future(t *testing.T) {
	result, err := rpcCall(t, "eth_getBlockByNumber", "0x4000000000000000", false)
	require.NoError(t, err, "eth_getBlockByNumber(future) call")
	assert.JSONEq(t, "null", string(result), "future block must surface as null")
	validateResult(t, "eth_getBlockByNumber", result)
}

// TestEthGetBlockByHash_LatestHashesOnly fetches the latest block by hash.
func TestEthGetBlockByHash_LatestHashesOnly(t *testing.T) {
	hash := fetchLatestBlockHash(t)
	rpcCallAndValidate(t, "eth_getBlockByHash", hash, false)
}

// TestEthGetBlockByHash_NonExistent asserts a made-up hash yields null.
func TestEthGetBlockByHash_NonExistent(t *testing.T) {
	nonExistent := "0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	result, err := rpcCall(t, "eth_getBlockByHash", nonExistent, false)
	require.NoError(t, err, "eth_getBlockByHash(non-existent) call")
	assert.JSONEq(t, "null", string(result), "non-existent hash must surface as null")
	validateResult(t, "eth_getBlockByHash", result)
}

// TestEthGetBlockTransactionCountByNumber checks count at latest.
func TestEthGetBlockTransactionCountByNumber(t *testing.T) {
	rpcCallAndValidate(t, "eth_getBlockTransactionCountByNumber", "latest")
}

// TestEthGetBlockTransactionCountByHash checks count for the latest-block hash.
func TestEthGetBlockTransactionCountByHash(t *testing.T) {
	hash := fetchLatestBlockHash(t)
	rpcCallAndValidate(t, "eth_getBlockTransactionCountByHash", hash)
}

// -----------------------------------------------------------------------------
// Transaction round-trip — sendRawTransaction → receipts/lookups
// -----------------------------------------------------------------------------

// TestEthSendRawTransaction signs an EIP-1559 transfer and submits it.
// Asserts the returned tx hash matches the schema.
func TestEthSendRawTransaction(t *testing.T) {
	hash := sendTransfer(t)
	require.NotEmpty(t, hash)
}

// TestEthGetTransactionByHash fetches a freshly-submitted tx by its hash.
func TestEthGetTransactionByHash(t *testing.T) {
	hash := sendTransfer(t)
	require.NotEmpty(t, hash)
	rpcCallAndValidate(t, "eth_getTransactionByHash", hash)
}

// TestEthGetTransactionReceipt fetches the receipt of a freshly-submitted tx.
func TestEthGetTransactionReceipt(t *testing.T) {
	hash := sendTransfer(t)
	require.NotEmpty(t, hash)
	rpcCallAndValidate(t, "eth_getTransactionReceipt", hash)
}

// TestEthGetTransactionReceipt_NotFound asserts a made-up hash yields null.
func TestEthGetTransactionReceipt_NotFound(t *testing.T) {
	result, err := rpcCall(t, "eth_getTransactionReceipt", "0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	require.NoError(t, err, "eth_getTransactionReceipt(non-existent) call")
	assert.JSONEq(t, "null", string(result))
	validateResult(t, "eth_getTransactionReceipt", result)
}

// TestEthGetTransactionByBlockHashAndIndex picks index 0 of the block containing
// a freshly-submitted tx.
func TestEthGetTransactionByBlockHashAndIndex(t *testing.T) {
	hash := sendTransfer(t)
	receipt := waitReceipt(t, hash)
	blockHash := receipt["blockHash"].(string)
	rpcCallAndValidate(t, "eth_getTransactionByBlockHashAndIndex", blockHash, "0x0")
}

// TestEthGetTransactionByBlockNumberAndIndex picks index 0 of the block containing
// a freshly-submitted tx.
func TestEthGetTransactionByBlockNumberAndIndex(t *testing.T) {
	hash := sendTransfer(t)
	receipt := waitReceipt(t, hash)
	blockNumber := receipt["blockNumber"].(string)
	rpcCallAndValidate(t, "eth_getTransactionByBlockNumberAndIndex", blockNumber, "0x0")
}

// TestEthGetBlockReceipts fetches receipts across every JSON encoding of
// go-ethereum's rpc.BlockNumberOrHash. (eth_getBlockReceipts takes a
// BlockNumberOrHash as its sole parameter.)
func TestEthGetBlockReceipts(t *testing.T) {
	runBlockNumberOrHashCases(t, func(t *testing.T, blockArg any) {
		result, err := rpcCall(t, "eth_getBlockReceipts", blockArg)
		require.NoError(t, err, "eth_getBlockReceipts(%v)", blockArg)
		validateResult(t, "eth_getBlockReceipts", result)
	})
}

// -----------------------------------------------------------------------------
// Execution — eth_call, eth_estimateGas
// -----------------------------------------------------------------------------

// TestEthCall executes a no-op call from the zero address to itself across
// every JSON encoding of go-ethereum's rpc.BlockNumberOrHash.
func TestEthCall(t *testing.T) {
	msg := map[string]any{
		"from": common.Address{}.Hex(),
		"to":   common.Address{}.Hex(),
	}
	runBlockNumberOrHashCases(t, func(t *testing.T, blockArg any) {
		result, err := rpcCall(t, "eth_call", msg, blockArg)
		require.NoError(t, err, "eth_call(%v)", blockArg)
		validateResult(t, "eth_call", result)
	})
}

// TestEthEstimateGas estimates gas for a no-op call. The block-tag parameter
// is optional in the JSON-RPC spec, so we exercise the no-arg form as well as
// every JSON encoding of go-ethereum's rpc.BlockNumberOrHash.
func TestEthEstimateGas(t *testing.T) {
	msg := map[string]any{
		"from": common.Address{}.Hex(),
		"to":   common.Address{}.Hex(),
	}

	t.Run("no block param", func(t *testing.T) {
		rpcCallAndValidate(t, "eth_estimateGas", msg)
	})

	runBlockNumberOrHashCases(t, func(t *testing.T, blockArg any) {
		result, err := rpcCall(t, "eth_estimateGas", msg, blockArg)
		require.NoError(t, err, "eth_estimateGas(%v)", blockArg)
		validateResult(t, "eth_estimateGas", result)
	})
}

// -----------------------------------------------------------------------------
// Logs
// -----------------------------------------------------------------------------

// TestEthGetLogs queries logs over the full chain so far. The result may be
// empty but must still validate as an array of log objects.
func TestEthGetLogs(t *testing.T) {
	q := map[string]any{
		"fromBlock": "0x0",
		"toBlock":   "latest",
	}
	rpcCallAndValidate(t, "eth_getLogs", q)
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

// runBlockNumberOrHashCases enumerates the JSON encodings of go-ethereum's
// rpc.BlockNumberOrHash and runs `do` once per encoding under its own
// t.Run sub-test:
//
//   - "string tag latest" / "earliest" / "pending" / "safe" / "finalized"
//   - "hex block number 0x0"
//   - "object blockNumber 0x0"                  → {"blockNumber":"0x0"}
//   - "object blockHash (latest)"               → {"blockHash":"0x..."}
//   - "object blockHash with requireCanonical"  → {"blockHash":"0x...","requireCanonical":true}
//
// `do` receives the block-arg payload to splice into the call. Sub-cases for
// the two hash-form variants fetch the latest block hash on demand.
func runBlockNumberOrHashCases(t *testing.T, do func(t *testing.T, blockArg any)) {
	t.Helper()
	cases := []struct {
		name     string
		blockArg any
	}{
		{name: "string tag latest", blockArg: "latest"},
		{name: "string tag earliest", blockArg: "earliest"},
		{name: "string tag pending", blockArg: "pending"},
		{name: "string tag safe", blockArg: "safe"},
		{name: "string tag finalized", blockArg: "finalized"},
		{name: "hex block number 0x0", blockArg: "0x0"},
		{name: "object blockNumber 0x0", blockArg: map[string]any{"blockNumber": "0x0"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			do(t, tc.blockArg)
		})
	}
	t.Run("object blockHash (latest)", func(t *testing.T) {
		hash := fetchLatestBlockHash(t)
		do(t, map[string]any{"blockHash": hash})
	})
	t.Run("object blockHash with requireCanonical", func(t *testing.T) {
		hash := fetchLatestBlockHash(t)
		do(t, map[string]any{"blockHash": hash, "requireCanonical": true})
	})
}

// isMethodNotFound returns true for a JSON-RPC -32601 "method not found"
// error, which we treat as "skip" for optional methods.
func isMethodNotFound(err error) bool {
	if err == nil {
		return false
	}
	var rpcErr *jsonRPCError
	if errors.As(err, &rpcErr) {
		return rpcErr.Code == -32601 || strings.Contains(strings.ToLower(rpcErr.Message), "not found") ||
			strings.Contains(strings.ToLower(rpcErr.Message), "not supported")
	}
	return false
}

// isUnsupported is the broader version of isMethodNotFound: it also matches
// non-standard "this shape isn't implemented" surfaces such as Thor's
// "invalid block tag" (returned for hash-form block tags) and "not yet
// supported" (returned for parameters the node hasn't wired up yet).
func isUnsupported(err error) bool {
	if err == nil {
		return false
	}
	if isMethodNotFound(err) {
		return true
	}
	var rpcErr *jsonRPCError
	if errors.As(err, &rpcErr) {
		msg := strings.ToLower(rpcErr.Message)
		for _, marker := range []string{"invalid block tag", "not yet supported"} {
			if strings.Contains(msg, marker) {
				return true
			}
		}
	}
	return false
}

// hexQuantityToInt parses a JSON-encoded QUANTITY ("0x..."). Fails the test on
// any parse error.
func hexQuantityToInt(t *testing.T, raw json.RawMessage) *big.Int {
	t.Helper()
	var s string
	require.NoError(t, json.Unmarshal(raw, &s), "unmarshal quantity string")
	n, ok := new(big.Int).SetString(strings.TrimPrefix(s, "0x"), 16)
	require.True(t, ok, "parse quantity %q", s)
	return n
}

// fetchLatestBlockHash returns the hash of the latest block via
// eth_getBlockByNumber("latest", false).
func fetchLatestBlockHash(t *testing.T) string {
	t.Helper()
	raw, err := rpcCall(t, "eth_getBlockByNumber", "latest", false)
	require.NoError(t, err, "fetch latest block for hash")
	var block map[string]any
	require.NoError(t, json.Unmarshal(raw, &block), "unmarshal latest block")
	hash, ok := block["hash"].(string)
	require.True(t, ok, "latest block has no string hash field")
	return hash
}

// fetchLatestBaseFee returns the baseFeePerGas of the latest block as *big.Int.
func fetchLatestBaseFee(t *testing.T) *big.Int {
	t.Helper()
	raw, err := rpcCall(t, "eth_getBlockByNumber", "latest", false)
	require.NoError(t, err, "fetch latest block for baseFee")
	var block map[string]any
	require.NoError(t, json.Unmarshal(raw, &block), "unmarshal latest block")
	bf, ok := block["baseFeePerGas"].(string)
	require.True(t, ok, "latest block missing baseFeePerGas — chain must be post-1559")
	n, ok := new(big.Int).SetString(strings.TrimPrefix(bf, "0x"), 16)
	require.True(t, ok, "parse baseFeePerGas %q", bf)
	return n
}

// sendTransfer signs an EIP-1559 1-wei transfer (helper.TestSenderKey → node2)
// and submits it via eth_sendRawTransaction. Returns the submitted tx hash and
// blocks until the receipt is available on the Thor side.
func sendTransfer(t *testing.T) string {
	t.Helper()

	chainIDRaw := rpcCallAndValidate(t, "eth_chainId")
	chainID := hexQuantityToInt(t, chainIDRaw)

	from := crypto.PubkeyToAddress(helper.TestSenderKey.PublicKey)
	nonceRaw, err := rpcCall(t, "eth_getTransactionCount", from.Hex(), "pending")
	require.NoError(t, err, "eth_getTransactionCount(pending)")
	nonce := hexQuantityToInt(t, nonceRaw).Uint64()

	baseFee := fetchLatestBaseFee(t)
	gasTipCap := big.NewInt(1)
	gasFeeCap := new(big.Int).Add(new(big.Int).Mul(baseFee, big.NewInt(2)), gasTipCap)

	to := common.HexToAddress("0x327931085B4cCbCE0baABb5a5E1C678707C51d90") // node2
	raw := signDynamicFeeTx(t, helper.TestSenderKey, chainID, nonce, gasTipCap, gasFeeCap, 21_000, &to, big.NewInt(1), nil)

	hashRaw := rpcCallAndValidate(t, "eth_sendRawTransaction", "0x"+hex.EncodeToString(raw))
	var hash string
	require.NoError(t, json.Unmarshal(hashRaw, &hash), "unmarshal tx hash")
	require.Regexp(t, "^0x[0-9a-fA-F]{64}$", hash)

	// Wait for the receipt on the Thor side so subsequent lookups succeed.
	thorClient := helper.NewClient(nodeURL)
	b32 := thor.Bytes32(common.HexToHash(hash))
	helper.WaitForReceipt(t, thorClient, &b32, 30*time.Second)

	return hash
}

// waitReceipt fetches eth_getTransactionReceipt for hash. Returns the receipt
// as a map for caller convenience (already schema-validated).
func waitReceipt(t *testing.T, hash string) map[string]any {
	t.Helper()
	raw := rpcCallAndValidate(t, "eth_getTransactionReceipt", hash)
	require.NotEqual(t, "null", string(raw), "receipt must not be null for a confirmed tx")
	var receipt map[string]any
	require.NoError(t, json.Unmarshal(raw, &receipt), "unmarshal receipt")
	return receipt
}

// signDynamicFeeTx hand-builds and signs an EIP-1559 (type-2) transaction
// envelope without depending on the local eth_client package. The result is
// the raw bytes ready for eth_sendRawTransaction.
func signDynamicFeeTx(t *testing.T, key *ecdsa.PrivateKey, chainID *big.Int, nonce uint64, tipCap, feeCap *big.Int, gas uint64, to *common.Address, value *big.Int, data []byte) []byte {
	t.Helper()
	payload := []any{
		chainID,
		nonce,
		tipCap,
		feeCap,
		gas,
		to,
		value,
		data,
		[]any{}, // empty access list
	}
	// signing hash: keccak256(0x02 || rlp(payload without sig fields))
	prefixed := append([]byte{0x02}, mustRLP(t, payload)...)
	sigHash := crypto.Keccak256Hash(prefixed)
	sig, err := crypto.Sign(sigHash.Bytes(), key)
	require.NoError(t, err, "sign tx")
	if len(sig) != 65 {
		t.Fatalf("unexpected signature length %d", len(sig))
	}
	v := uint64(sig[64])
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:64])

	signed := []any{
		chainID, nonce, tipCap, feeCap, gas, to, value, data, []any{},
		v, r, s,
	}
	return append([]byte{0x02}, mustRLP(t, signed)...)
}

func mustRLP(t *testing.T, v any) []byte {
	t.Helper()
	b, err := gethrlp.EncodeToBytes(v)
	require.NoError(t, err, "rlp encode")
	return b
}
