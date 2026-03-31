package helper

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/tx"
)

// node1Key is the private key for Node 1's master address (0x61fF580B63D3845934610222245C116E013717ec).
// This account is pre-funded with a large balance in LocalThreeNodesNetwork genesis.
const node1Key = "01a4107bfb7d5141ec519e75788c34295741a1eefbfe460320efd2ada944071e"

// PreForkRevision targets block 0 (genesis), which is before INTERSTELLAR activates.
// PostForkRevision targets block 1, the block at which INTERSTELLAR activates.
// Both must stay in sync with AddField("INTERSTELLAR", N) in network/setup/network.go.
const (
	PreForkRevision  = "0"
	PostForkRevision = "1"
)

// TestSenderKey is the signing key used across all tests.
var TestSenderKey, _ = crypto.HexToECDSA(node1Key)

// NewClient returns a thorclient pointed at the given node URL.
func NewClient(nodeURL string) *thorclient.Client {
	return thorclient.New(nodeURL)
}

// BuildTx constructs and signs a legacy transaction with the given gas limit and clause.
func BuildTx(t testing.TB, client *thorclient.Client, gas uint64, clause *tx.Clause) *tx.Transaction {
	t.Helper()

	chainTag, err := client.ChainTag()
	require.NoError(t, err)

	best, err := client.Block("best")
	require.NoError(t, err)

	trx := tx.NewBuilder(tx.TypeLegacy).
		ChainTag(chainTag).
		Clause(clause).
		Gas(gas).
		BlockRef(tx.NewBlockRefFromID(best.ID)).
		Expiration(100).
		Nonce(uint64(time.Now().UnixNano())).
		Build()

	signed, err := tx.Sign(trx, TestSenderKey)
	require.NoError(t, err)
	return signed
}

// WaitForReceipt polls until the transaction receipt is available or timeout elapses.
func WaitForReceipt(t *testing.T, client *thorclient.Client, txID *thor.Bytes32, timeout time.Duration) *api.Receipt {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		receipt, err := client.TransactionReceipt(txID)
		if err == nil && receipt != nil {
			return receipt
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("timed out waiting for receipt: %s", txID)
	return nil
}

// ZeroClause returns a minimal clause that transfers 0 VET to a burn address.
func ZeroClause() *tx.Clause {
	to := thor.BytesToAddress([]byte{0xde, 0xad})
	return tx.NewClause(&to).WithValue(big.NewInt(0))
}
