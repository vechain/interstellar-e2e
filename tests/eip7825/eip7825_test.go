package eip7825

// EIP-7825 introduces a per-transaction gas limit cap (MaxTxGasLimit = 1 << 24 = 16,777,216)
// enforced by the txpool once the INTERSTELLAR fork is active.

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vechain/interstellar-e2e/tests/helper"
)

// maxTxGasLimit mirrors thor.MaxTxGasLimit (1 << 24).
const maxTxGasLimit = 1 << 24 // 16,777,216

func TestEIP7825_GasAboveLimit(t *testing.T) {
	client := helper.NewClient(nodeURL)

	trx := helper.BuildTx(t, client, maxTxGasLimit+1, helper.ZeroClause())
	_, err := client.SendTransaction(trx)

	require.Error(t, err, "tx with gas > MaxTxGasLimit must be rejected by the txpool")
}

func TestEIP7825_GasAtLimit(t *testing.T) {
	client := helper.NewClient(nodeURL)

	trx := helper.BuildTx(t, client, maxTxGasLimit, helper.ZeroClause())
	result, err := client.SendTransaction(trx)

	require.NoError(t, err, "tx with gas == MaxTxGasLimit must be accepted")
	require.NotNil(t, result.ID, "accepted tx must have a transaction ID")

	receipt := helper.WaitForReceipt(t, client, result.ID, 30*time.Second)
	assert.False(t, receipt.Reverted, "tx at gas limit must execute successfully")
}

func TestEIP7825_GasBelowLimit(t *testing.T) {
	client := helper.NewClient(nodeURL)

	trx := helper.BuildTx(t, client, 21_000, helper.ZeroClause())
	result, err := client.SendTransaction(trx)

	require.NoError(t, err, "tx with normal gas must be accepted")
	require.NotNil(t, result.ID)

	receipt := helper.WaitForReceipt(t, client, result.ID, 30*time.Second)
	assert.False(t, receipt.Reverted)
}
