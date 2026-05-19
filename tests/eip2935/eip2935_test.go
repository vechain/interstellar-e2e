package eip2935

// EIP-2935 adds a historical block-hash facade contract activated at the
// INTERSTELLAR fork.
//
// Contract address: 0x0000F90827F1C53a10cb7A02335B175320002935 (fixed by EIP)
// Calldata: exactly 32 bytes — a uint256 block number, NO function selector.
// Dispatch goes through the fallback function.
//
// Return semantics:
//   - valid input  → 32-byte block ID for that block number
//   - any failure  → revert with empty returndata (matches EIP-2935 reference)
//
// Validity rules (post-fork):
//   - input length must be exactly 32 bytes
//   - num < block.number                      (no future / current block)
//   - block.number - num <= 8191              (within SERVE_WINDOW)
//
// Pre-fork: the address has no code, so calls to it succeed with empty
// output and do not revert (standard EVM behavior for empty accounts).

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"

	"github.com/vechain/interstellar-e2e/tests/helper"
)

// historyAddr is the fixed EIP-2935 facade address.
var historyAddr = thor.MustParseAddress("0x0000F90827F1C53a10cb7A02335B175320002935")

// encodeBlockNumber returns the 32-byte big-endian uint256 calldata expected
// by the History contract (no function selector — fallback dispatch).
func encodeBlockNumber(num uint32) []byte {
	var data [32]byte
	new(big.Int).SetUint64(uint64(num)).FillBytes(data[:])
	return data[:]
}

// waitForBlockAtLeast polls until best.Number >= min, so tests have a
// historical block to query (target = best - 1 must be >= 0).
func waitForBlockAtLeast(t *testing.T, client *thorclient.Client, min uint32, timeout time.Duration) *api.JSONCollapsedBlock {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		b, err := client.Block("best")
		require.NoError(t, err)
		if b.Number >= min {
			return b
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatalf("timed out waiting for block number >= %d", min)
	return nil
}

// TestEIP2935_BlockID verifies that History.BlockID(num) returns the canonical
// block ID for an in-range historical block, mirroring the canonical lookup
// served by the blocks API.
func TestEIP2935_BlockID(t *testing.T) {
	client := helper.NewClient(nodeURL)

	// Need at least 2 blocks so target = best - 1 is in the SERVE_WINDOW.
	best := waitForBlockAtLeast(t, client, 2, 30*time.Second)
	target := best.Number - 1

	// Pin both the evaluation revision and the canonical lookup to best.Number
	// to avoid races against the chain producing further blocks underneath us.
	bestRev := strconv.FormatUint(uint64(best.Number), 10)
	targetRev := strconv.FormatUint(uint64(target), 10)

	canonical, err := client.Block(targetRev)
	require.NoError(t, err)

	callData := &api.BatchCallData{
		Clauses: api.Clauses{{To: &historyAddr, Data: "0x" + hex.EncodeToString(encodeBlockNumber(target))}},
		Gas:     50_000,
	}

	t.Run("pre-fork", func(t *testing.T) {

		historyAcc, err := client.Account(&historyAddr, thorclient.Revision(helper.PreForkRevision))
		require.NoError(t, err)
		require.NotNil(t, historyAcc)
		require.False(t, historyAcc.HasCode, "pre-fork: History contract must have no code")

		results, err := client.InspectClauses(callData, thorclient.Revision(helper.PreForkRevision))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Reverted, "pre-fork: address has no code, call must not revert")
		assert.Empty(t, strings.TrimPrefix(results[0].Data, "0x"),
			"pre-fork: address has no code, output must be empty")
	})

	t.Run("post-fork", func(t *testing.T) {

		historyAcc, err := client.Account(&historyAddr, thorclient.Revision(bestRev))
		require.NoError(t, err)
		require.NotNil(t, historyAcc)
		require.True(t, historyAcc.HasCode, "post-fork: History contract must have code")

		results, err := client.InspectClauses(callData, thorclient.Revision(bestRev))
		require.NoError(t, err)
		require.Len(t, results, 1)
		require.False(t, results[0].Reverted,
			"post-fork: BlockID(best-1) must not revert (vmError: %s)", results[0].VMError)

		raw, err := hex.DecodeString(strings.TrimPrefix(results[0].Data, "0x"))
		require.NoError(t, err)
		require.Len(t, raw, 32, "post-fork: History must return a 32-byte block ID")
		assert.Equal(t, canonical.ID.Bytes(), raw,
			"post-fork: History.BlockID(n) must equal canonical block n ID")
	})
}

// TestEIP2935_FutureBlockReverts verifies that requesting a block number
// equal to or above the current best reverts post-fork with no return data.
// EIP-2935 SERVE_WINDOW is [best-8191, best-1].
func TestEIP2935_FutureBlockReverts(t *testing.T) {
	client := helper.NewClient(nodeURL)

	best, err := client.Block("best")
	require.NoError(t, err)
	bestRev := strconv.FormatUint(uint64(best.Number), 10)

	cases := []struct {
		name string
		num  uint32
	}{
		{"equal-to-best", best.Number},
		{"far-future", best.Number + 9999},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			callData := &api.BatchCallData{
				Clauses: api.Clauses{{To: &historyAddr, Data: "0x" + hex.EncodeToString(encodeBlockNumber(tc.num))}},
				Gas:     50_000,
			}

			t.Run("pre-fork", func(t *testing.T) {
				results, err := client.InspectClauses(callData, thorclient.Revision(helper.PreForkRevision))
				require.NoError(t, err)
				require.Len(t, results, 1)
				assert.False(t, results[0].Reverted, "pre-fork: no code at address — must not revert")
				assert.Empty(t, strings.TrimPrefix(results[0].Data, "0x"),
					"pre-fork: output must be empty")
			})

			t.Run("post-fork", func(t *testing.T) {
				results, err := client.InspectClauses(callData, thorclient.Revision(bestRev))
				require.NoError(t, err)
				require.Len(t, results, 1)
				assert.True(t, results[0].Reverted, "post-fork: out-of-range block must revert")
				assert.Empty(t, strings.TrimPrefix(results[0].Data, "0x"),
					"post-fork: EIP-2935 revert must carry no return data")
			})
		})
	}
}

// TestEIP2935_InvalidLength verifies that calldata not exactly 32 bytes long
// reverts with no return data post-fork (input.length != 32 → revert()).
func TestEIP2935_InvalidLength(t *testing.T) {
	client := helper.NewClient(nodeURL)

	for _, length := range []int{0, 31, 33} {
		t.Run(fmt.Sprintf("length-%d", length), func(t *testing.T) {
			callData := &api.BatchCallData{
				Clauses: api.Clauses{{To: &historyAddr, Data: "0x" + hex.EncodeToString(make([]byte, length))}},
				Gas:     50_000,
			}

			t.Run("pre-fork", func(t *testing.T) {
				results, err := client.InspectClauses(callData, thorclient.Revision(helper.PreForkRevision))
				require.NoError(t, err)
				require.Len(t, results, 1)
				assert.False(t, results[0].Reverted, "pre-fork: no code at address — must not revert")
				assert.Empty(t, strings.TrimPrefix(results[0].Data, "0x"),
					"pre-fork: output must be empty")
			})

			t.Run("post-fork", func(t *testing.T) {
				results, err := client.InspectClauses(callData, thorclient.Revision("best"))
				require.NoError(t, err)
				require.Len(t, results, 1)
				assert.True(t, results[0].Reverted, "post-fork: length=%d must revert", length)
				assert.Empty(t, strings.TrimPrefix(results[0].Data, "0x"),
					"post-fork: EIP-2935 revert must carry no return data")
			})
		})
	}
}

// TestEIP2935_Revision verifies that pinning the call to an explicit revision
// matches the canonical block lookup, and that a malformed revision string
// surfaces as a request-level error rather than a silent empty result.
func TestEIP2935_Revision(t *testing.T) {
	client := helper.NewClient(nodeURL)

	best := waitForBlockAtLeast(t, client, 2, 30*time.Second)
	target := best.Number - 1
	bestRev := strconv.FormatUint(uint64(best.Number), 10)

	callData := &api.BatchCallData{
		Clauses: api.Clauses{{To: &historyAddr, Data: "0x" + hex.EncodeToString(encodeBlockNumber(target))}},
		Gas:     50_000,
	}

	// Pin to best.Number explicitly — must match the canonical block ID.
	results, err := client.InspectClauses(callData, thorclient.Revision(bestRev))
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.False(t, results[0].Reverted,
		"pinned revision call must not revert (vmError: %s)", results[0].VMError)
	pinned, err := hex.DecodeString(strings.TrimPrefix(results[0].Data, "0x"))
	require.NoError(t, err)
	require.Len(t, pinned, 32)

	canonical, err := client.Block(strconv.FormatUint(uint64(target), 10))
	require.NoError(t, err)
	assert.Equal(t, canonical.ID.Bytes(), pinned,
		"pinned-revision History.BlockID(n) must match canonical block n ID")

	// Bad revision string should surface as a request error.
	_, err = client.InspectClauses(callData, thorclient.Revision("not-a-real-revision"))
	assert.Error(t, err, "malformed revision must surface as a request error")
}
