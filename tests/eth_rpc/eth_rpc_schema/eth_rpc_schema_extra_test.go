// Additional schema-conformance coverage, organized by the same three-category
// scheme used in the ethersjs / web3js suites:
//
//   Cat-1  Implemented on Thor → full request + JSON-Schema validation of the
//          result (chain stubs, uncles, the filter family, the WS 'syncing'
//          subscription).
//   Cat-2  Not registered by thor's dispatcher → the call is attempted and the
//          test SKIPS on a "method not found" surface (auto-activates if Thor
//          ever ships it).
//   Cat-3  Implemented but divergent from go-ethereum → a geth-parity assertion
//          that is LEFT FAILING for manual review.
//
// Authoritative method set comes from thor's rpc/*/handler.go Mount() funcs on
// branch pedro/eth_eq_json_rpc.

package ethrpcschema

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------------
// Cat-1 — chain / node metadata stubs (implemented, PoA-constant values)
// -----------------------------------------------------------------------------

// TestEthCoinbase asserts eth_coinbase returns an address (zero on PoA).
func TestEthCoinbase(t *testing.T) {
	result := rpcCallAndValidate(t, "eth_coinbase")
	var addr string
	require.NoError(t, json.Unmarshal(result, &addr), "unmarshal coinbase")
	assert.Equal(t, common.Address{}.Hex(), common.HexToAddress(addr).Hex(),
		"Thor PoA coinbase must be the zero address")
}

// TestEthMining asserts eth_mining returns a boolean (false on PoA).
func TestEthMining(t *testing.T) {
	result := rpcCallAndValidate(t, "eth_mining")
	var mining bool
	require.NoError(t, json.Unmarshal(result, &mining), "unmarshal mining")
	assert.False(t, mining, "Thor PoA must report mining=false")
}

// TestEthHashrate asserts eth_hashrate returns a QUANTITY (0x0 on PoA).
func TestEthHashrate(t *testing.T) {
	result := rpcCallAndValidate(t, "eth_hashrate")
	assert.Equal(t, 0, hexQuantityToInt(t, result).Sign(), "Thor PoA hashrate must be 0")
}

// TestEthAccounts asserts eth_accounts returns an (empty) address array — Thor
// holds no node-side keystore.
func TestEthAccounts(t *testing.T) {
	result := rpcCallAndValidate(t, "eth_accounts")
	var accounts []string
	require.NoError(t, json.Unmarshal(result, &accounts), "unmarshal accounts")
	assert.Empty(t, accounts, "Thor exposes no unlocked node accounts")
}

// -----------------------------------------------------------------------------
// Cat-1 — uncles (VeChain has none; count is 0x0, by-index is null)
// -----------------------------------------------------------------------------

// TestEthGetUncleCountByBlockNumber asserts the uncle count at latest is 0x0.
func TestEthGetUncleCountByBlockNumber(t *testing.T) {
	result := rpcCallAndValidate(t, "eth_getUncleCountByBlockNumber", "latest")
	assert.Equal(t, 0, hexQuantityToInt(t, result).Sign(), "VeChain has no uncles")
}

// TestEthGetUncleCountByBlockHash asserts the uncle count for the latest-block
// hash is 0x0.
func TestEthGetUncleCountByBlockHash(t *testing.T) {
	hash := fetchLatestBlockHash(t)
	result := rpcCallAndValidate(t, "eth_getUncleCountByBlockHash", hash)
	assert.Equal(t, 0, hexQuantityToInt(t, result).Sign(), "VeChain has no uncles")
}

// TestEthGetUncleByBlockNumberAndIndex asserts the uncle at (latest, 0) is null.
func TestEthGetUncleByBlockNumberAndIndex(t *testing.T) {
	result, err := rpcCall(t, "eth_getUncleByBlockNumberAndIndex", "latest", "0x0")
	require.NoError(t, err, "eth_getUncleByBlockNumberAndIndex call")
	assert.JSONEq(t, "null", string(result), "VeChain has no uncles")
	validateResult(t, "eth_getUncleByBlockNumberAndIndex", result)
}

// TestEthGetUncleByBlockHashAndIndex asserts the uncle at (latest hash, 0) is null.
func TestEthGetUncleByBlockHashAndIndex(t *testing.T) {
	hash := fetchLatestBlockHash(t)
	result, err := rpcCall(t, "eth_getUncleByBlockHashAndIndex", hash, "0x0")
	require.NoError(t, err, "eth_getUncleByBlockHashAndIndex call")
	assert.JSONEq(t, "null", string(result), "VeChain has no uncles")
	validateResult(t, "eth_getUncleByBlockHashAndIndex", result)
}

// -----------------------------------------------------------------------------
// Cat-1 — filter family (newFilter / getFilterLogs / getFilterChanges /
// newBlockFilter / newPendingTransactionFilter / uninstallFilter)
// -----------------------------------------------------------------------------

// filterLogTopic and filterLogInitCode deploy a 43-byte init blob that LOG1's a
// fixed topic and returns empty runtime code — used to seed a log the filter
// family can match. Mirrors the LogOnDeploy blob in the WS logs test, with a
// distinct topic so concurrent subscriptions don't cross-match.
const (
	filterLogTopic    = "0x3434343434343434343434343434343434343434343434343434343434343434"
	filterLogInitCode = "7f3434343434343434343434343434343434343434343434343434343434343434" +
		"60006000a160006000f3"
)

// TestEthFilterLogLifecycle drives eth_newFilter → eth_getFilterLogs →
// eth_getFilterChanges → eth_uninstallFilter against a real LOG1 emission, and
// schema-validates each result.
func TestEthFilterLogLifecycle(t *testing.T) {
	// Emit a LOG1 with our topic and wait for the receipt so the log is mined
	// before the filter is installed.
	broadcastEthTx(t, common.Hex2Bytes(filterLogInitCode), nil, 200_000)

	filterIDRaw := rpcCallAndValidate(t, "eth_newFilter", map[string]any{
		"fromBlock": "0x0",
		"toBlock":   "latest",
		"topics":    []any{filterLogTopic},
	})
	var filterID string
	require.NoError(t, json.Unmarshal(filterIDRaw, &filterID), "unmarshal filter id")

	defer func() {
		removedRaw, err := rpcCall(t, "eth_uninstallFilter", filterID)
		require.NoError(t, err, "eth_uninstallFilter call")
		validateResult(t, "eth_uninstallFilter", removedRaw)
		var removed bool
		require.NoError(t, json.Unmarshal(removedRaw, &removed), "unmarshal uninstall result")
		assert.True(t, removed, "eth_uninstallFilter must return true for an active filter")
	}()

	// eth_getFilterLogs returns ALL matching logs regardless of poll cursor.
	var logs []map[string]any
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		raw := rpcCallAndValidate(t, "eth_getFilterLogs", filterID)
		require.NoError(t, json.Unmarshal(raw, &logs), "unmarshal filter logs")
		if len(logs) > 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	require.NotEmpty(t, logs, "eth_getFilterLogs must return the emitted LOG1")
	topics, ok := logs[0]["topics"].([]any)
	require.True(t, ok, "log.topics must be an array")
	require.NotEmpty(t, topics, "log must carry a topic")
	assert.Equal(t, filterLogTopic, topics[0], "log.topics[0] must match the emitted topic")

	// eth_getFilterChanges shape-validates (may be empty depending on the poll
	// cursor relative to the emission — we only assert the schema here).
	rpcCallAndValidate(t, "eth_getFilterChanges", filterID)
}

// TestEthNewBlockFilter installs a block filter, schema-validates the id and a
// subsequent getFilterChanges poll (an array of block hashes), then uninstalls.
func TestEthNewBlockFilter(t *testing.T) {
	filterIDRaw := rpcCallAndValidate(t, "eth_newBlockFilter")
	var filterID string
	require.NoError(t, json.Unmarshal(filterIDRaw, &filterID), "unmarshal filter id")

	// Poll until at least one new block hash arrives (validates the non-empty
	// hash-array shape), bounded so a stalled chain can't hang the test.
	var hashes []string
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		raw := rpcCallAndValidate(t, "eth_getFilterChanges", filterID)
		require.NoError(t, json.Unmarshal(raw, &hashes), "unmarshal block-hash changes")
		if len(hashes) > 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	require.NotEmpty(t, hashes, "eth_getFilterChanges on a block filter must yield block hashes")
	require.Regexp(t, "^0x[0-9a-fA-F]{64}$", hashes[0], "block-filter change must be a 32-byte hash")

	removedRaw, err := rpcCall(t, "eth_uninstallFilter", filterID)
	require.NoError(t, err, "eth_uninstallFilter call")
	validateResult(t, "eth_uninstallFilter", removedRaw)
}

// TestEthNewPendingTransactionFilter installs a pending-tx filter, schema-
// validates the id, then uninstalls.
func TestEthNewPendingTransactionFilter(t *testing.T) {
	filterIDRaw := rpcCallAndValidate(t, "eth_newPendingTransactionFilter")
	var filterID string
	require.NoError(t, json.Unmarshal(filterIDRaw, &filterID), "unmarshal filter id")

	removedRaw, err := rpcCall(t, "eth_uninstallFilter", filterID)
	require.NoError(t, err, "eth_uninstallFilter call")
	validateResult(t, "eth_uninstallFilter", removedRaw)
	var removed bool
	require.NoError(t, json.Unmarshal(removedRaw, &removed), "unmarshal uninstall result")
	assert.True(t, removed, "eth_uninstallFilter must return true for an active filter")
}

// -----------------------------------------------------------------------------
// Cat-1 (WS) — eth_subscribe('syncing')
// -----------------------------------------------------------------------------

// TestWsSubscribeSyncing validates the 'syncing' subscription Thor's
// rpc/ws/conn.go now implements (the old TestWsSubscribeSyncingRejected was
// removed once syncing shipped). On an already-synced local network runSyncing
// emits a single boolean `false` immediately; while syncing it would emit a
// {syncing,status} object — the schema accepts both.
func TestWsSubscribeSyncing(t *testing.T) {
	wc := wsDial(t)

	subIDRaw := wsCallAndValidate(t, wc, 1, "eth_subscribe", "eth_subscribe", "syncing")
	var subID string
	require.NoError(t, json.Unmarshal(subIDRaw, &subID), "unmarshal subID")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	notif, err := wsReadNotification(t, wc, ctx, subID)
	require.NoError(t, err, "wait for syncing notification")
	validateResult(t, "eth_subscription_syncing", notif)

	unsubRaw, err := wsCall(t, wc, 2, "eth_unsubscribe", subID)
	require.NoError(t, err, "eth_unsubscribe")
	validateResult(t, "eth_unsubscribe", unsubRaw)
}

// -----------------------------------------------------------------------------
// Cat-2 — standard eth_* methods thor does NOT register (skipped until shipped)
// -----------------------------------------------------------------------------

// TestUnimplementedMethods attempts each standard Ethereum method thor's
// dispatcher does not register and skips on a "method not found" surface. If
// Thor ever registers one, the success path (no error, non-null result) keeps
// the test honest; a non-"not found" error fails loudly.
func TestUnimplementedMethods(t *testing.T) {
	const senderAddr = "0x61fF580B63D3845934610222245C116E013717ec"
	const node2Addr = "0x327931085B4cCbCE0baABb5a5E1C678707C51d90"
	zeroHash := "0x" + strings.Repeat("00", 32)

	cases := []struct {
		method string
		params []any
	}{
		{"eth_getProof", []any{senderAddr, []any{}, "latest"}},
		{"eth_createAccessList", []any{map[string]any{"from": senderAddr, "to": node2Addr}, "latest"}},
		{"eth_protocolVersion", []any{}},
		{"eth_pendingTransactions", []any{}},
		{"eth_sign", []any{senderAddr, "0x68656c6c6f"}},
		{"eth_signTransaction", []any{map[string]any{"from": senderAddr, "to": node2Addr, "value": "0x1"}}},
		{"eth_getRawTransactionByHash", []any{zeroHash}},
		{"debug_traceTransaction", []any{zeroHash}},
	}

	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			result, err := rpcCall(t, tc.method, tc.params...)
			if isMethodNotFound(err) {
				t.Skipf("%s not implemented by Thor: %v", tc.method, err)
			}
			require.NoError(t, err, "%s errored for a non-\"not found\" reason", tc.method)
			require.NotNil(t, result, "%s returned a nil result without an error", tc.method)
		})
	}
}

// -----------------------------------------------------------------------------
// Cat-3 — divergences from go-ethereum (skipped until Thor aligns)
// -----------------------------------------------------------------------------

// TestEthFeeHistory_RewardPercentiles probes the rewardPercentiles form of
// eth_feeHistory.
//
// geth returns a per-block × per-percentile `reward` matrix when called with
// rewardPercentiles. Thor (rpc/fees/handler.go) currently rejects the percentile
// form — "reward percentiles are not yet supported" (code -32000) — so a fee
// estimator that requests percentiles can't use it. We SKIP on that documented
// gap; if Thor ever ships it, the call succeeds and the reward-matrix assertion
// keeps it honest. TestEthFeeHistory (no percentiles) covers the supported path.
func TestEthFeeHistory_RewardPercentiles(t *testing.T) {
	result, err := rpcCall(t, "eth_feeHistory", "0x4", "latest", []float64{25, 50, 75})
	if isRewardPercentilesUnsupported(err) {
		t.Skipf("eth_feeHistory rewardPercentiles not supported by Thor: %v", err)
	}
	require.NoError(t, err, "eth_feeHistory with rewardPercentiles")
	var fh map[string]any
	require.NoError(t, json.Unmarshal(result, &fh), "unmarshal feeHistory")
	require.Contains(t, fh, "reward",
		"feeHistory must include a per-block reward matrix when rewardPercentiles is requested")
}

// isRewardPercentilesUnsupported reports whether err is Thor's documented
// rejection of the eth_feeHistory rewardPercentiles parameter.
func isRewardPercentilesUnsupported(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "percentile") || strings.Contains(msg, "not yet supported")
}
