// Schema-driven WebSocket subscription tests for Thor's
// eth_subscribe / eth_unsubscribe (rpc/ws/conn.go).
//
// Each test:
//   1. Dials ws://nodeURL/rpc (same path as HTTP /rpc, upgrade-on-demand).
//   2. Issues eth_subscribe and validates the subID against
//      schemas/eth_subscribe.json.
//   3. Triggers an event the subscription should fire on — a new block
//      for newHeads, a deployment that emits LOG1 for logs, an EIP-1559
//      transfer for newPendingTransactions.
//   4. Reads the next notification frame and validates its inner result
//      against schemas/eth_subscription_<subtype>.json.
//   5. Issues eth_unsubscribe and validates the boolean against
//      schemas/eth_unsubscribe.json.
//
// The syncing subscription is documented as a rejection — Thor's switch in
// rpc/ws/conn.go:182-207 only implements newHeads/logs/newPendingTransactions;
// any other subtype returns InvalidParams (-32602). If Thor ever ships
// 'syncing' that test flips.

package ethrpcschema

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vechain/thor/v2/thor"

	"github.com/vechain/interstellar-e2e/tests/helper"
)

// jsonRPCInvalidParams is the JSON-RPC 2.0 code reserved for parameter errors.
// Thor's rpc/ws/conn.go:206 returns this for unsupported subscription subtypes.
const jsonRPCInvalidParams = -32602

// TestWsSubscribeNewHeads validates that an eth_subscribe('newHeads') call
// returns a hex subID, pushes a block-shaped notification on the next packed
// block, and a subsequent eth_unsubscribe returns true.
func TestWsSubscribeNewHeads(t *testing.T) {
	wc := wsDial(t)

	subIDRaw := wsCallAndValidate(t, wc, 1, "eth_subscribe", "eth_subscribe", "newHeads")
	var subID string
	require.NoError(t, json.Unmarshal(subIDRaw, &subID), "unmarshal subID")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	notif, err := wsReadNotification(t, wc, ctx, subID)
	require.NoError(t, err, "wait for newHeads notification")
	validateResult(t, "eth_subscription_newHeads", notif)

	unsubRaw, err := wsCall(t, wc, 2, "eth_unsubscribe", subID)
	require.NoError(t, err, "eth_unsubscribe")
	validateResult(t, "eth_unsubscribe", unsubRaw)
	var ok bool
	require.NoError(t, json.Unmarshal(unsubRaw, &ok))
	assert.True(t, ok, "eth_unsubscribe must return true for an active subID")
}

// TestWsSubscribeLogs validates that an eth_subscribe('logs', {topics:[t]})
// receives a LOG1 event emitted by a contract deployed in the same test.
// The contract is a 43-byte init blob that LOG1's a fixed topic and returns
// empty runtime code.
func TestWsSubscribeLogs(t *testing.T) {
	wc := wsDial(t)

	// Fixed topic so we can filter the subscription to just our emission.
	const topicHex = "0x1212121212121212121212121212121212121212121212121212121212121212"

	subIDRaw := wsCallAndValidate(t, wc, 1, "eth_subscribe", "eth_subscribe", "logs", map[string]any{
		"topics": []any{topicHex},
	})
	var subID string
	require.NoError(t, json.Unmarshal(subIDRaw, &subID), "unmarshal subID")

	// LogOnDeploy init bytecode:
	//   PUSH32 topic; PUSH1 0; PUSH1 0; LOG1 ; PUSH1 0; PUSH1 0; RETURN
	// Emits a single LOG1 with topic == topicHex and empty data, then returns
	// zero-byte runtime code.
	const initCode = "7f1212121212121212121212121212121212121212121212121212121212121212" +
		"60006000a160006000f3"

	deployHash := broadcastEthTx(t, common.Hex2Bytes(initCode), nil, 200_000)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	notif, err := wsReadNotification(t, wc, ctx, subID)
	require.NoError(t, err, "wait for logs notification")
	validateResult(t, "eth_subscription_logs", notif)

	// Beyond schema shape, sanity-check that the emitted log carries our topic
	// and ties back to the deployment tx hash.
	var log map[string]any
	require.NoError(t, json.Unmarshal(notif, &log), "unmarshal log payload")
	topics, ok := log["topics"].([]any)
	require.True(t, ok, "log.topics must be array, got %T", log["topics"])
	require.Len(t, topics, 1, "LOG1 must produce exactly one topic")
	assert.Equal(t, topicHex, topics[0], "log.topics[0]")
	assert.Equal(t, deployHash, log["transactionHash"], "log.transactionHash must match deploy tx")
	assert.Equal(t, false, log["removed"], "removed must be false on canonical chain")

	unsubRaw, err := wsCall(t, wc, 2, "eth_unsubscribe", subID)
	require.NoError(t, err, "eth_unsubscribe")
	validateResult(t, "eth_unsubscribe", unsubRaw)
}

// TestWsSubscribeNewPendingTransactions validates that an
// eth_subscribe('newPendingTransactions') pushes a tx-hash notification when
// an EIP-1559 transfer enters the pool, and that the hash matches what
// eth_sendRawTransaction returned.
func TestWsSubscribeNewPendingTransactions(t *testing.T) {
	wc := wsDial(t)

	subIDRaw := wsCallAndValidate(t, wc, 1, "eth_subscribe", "eth_subscribe", "newPendingTransactions")
	var subID string
	require.NoError(t, json.Unmarshal(subIDRaw, &subID), "unmarshal subID")

	// Broadcast a 1-wei transfer; runNewPendingTransactions fires only for
	// executable TypeEthDynamicFee txs (rpc/ws/subscriptions.go:113-117), which
	// matches what signDynamicFeeTx produces.
	to := common.HexToAddress("0x327931085B4cCbCE0baABb5a5E1C678707C51d90") // node2
	txHash := broadcastEthTx(t, nil, &to, 21_000)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	notif, err := wsReadNotification(t, wc, ctx, subID)
	require.NoError(t, err, "wait for newPendingTransactions notification")
	validateResult(t, "eth_subscription_newPendingTransactions", notif)

	var pendingHash string
	require.NoError(t, json.Unmarshal(notif, &pendingHash), "unmarshal hash payload")
	assert.Equal(t, strings.ToLower(txHash), strings.ToLower(pendingHash), "pending hash must match broadcast")

	unsubRaw, err := wsCall(t, wc, 2, "eth_unsubscribe", subID)
	require.NoError(t, err, "eth_unsubscribe")
	validateResult(t, "eth_unsubscribe", unsubRaw)
}

// TestWsSubscribeSyncingRejected pins down that Thor's eth_subscribe rejects
// the 'syncing' subtype with InvalidParams (-32602). Standard go-ethereum
// nodes accept 'syncing'; if Thor catches up, flip this test to a success path.
// Reference: rpc/ws/conn.go:206 ('unsupported subscription type ...').
func TestWsSubscribeSyncingRejected(t *testing.T) {
	wc := wsDial(t)

	_, err := wsCall(t, wc, 1, "eth_subscribe", "syncing")
	require.Error(t, err, "expected eth_subscribe('syncing') to be rejected")

	var rpcErr *jsonRPCError
	require.ErrorAs(t, err, &rpcErr, "error must be a jsonRPCError")
	assert.Equal(t, jsonRPCInvalidParams, rpcErr.Code, "expected InvalidParams (-32602)")
	assert.Contains(t, strings.ToLower(rpcErr.Message), "unsupported subscription type")
}

// broadcastEthTx signs and submits an EIP-1559 transaction from helper.TestSenderKey.
// Returns the tx hash from eth_sendRawTransaction. Used by the logs and pending
// subscription tests to trigger a server-side notification.
func broadcastEthTx(t *testing.T, data []byte, to *common.Address, gas uint64) string {
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

	raw := signDynamicFeeTx(t, helper.TestSenderKey, chainID, nonce, gasTipCap, gasFeeCap, gas, to, big.NewInt(1), data)

	hashRaw := rpcCallAndValidate(t, "eth_sendRawTransaction", "0x"+hex.EncodeToString(raw))
	var hash string
	require.NoError(t, json.Unmarshal(hashRaw, &hash), "unmarshal tx hash")
	require.Regexp(t, "^0x[0-9a-fA-F]{64}$", hash)

	// Wait for the receipt on the Thor side so the test exits cleanly after
	// the notification fires (some downstream tests assume past txs are mined).
	thorClient := helper.NewClient(nodeURL)
	b32 := thor.Bytes32(common.HexToHash(hash))
	helper.WaitForReceipt(t, thorClient, &b32, 20*time.Second)

	return hash
}
