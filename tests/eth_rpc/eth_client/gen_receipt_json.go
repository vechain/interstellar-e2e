// JSON unmarshaling support for Receipt.
//
// Mirrors the generated code from upstream go-ethereum
// (core/types/gen_receipt_json.go), but only the unmarshal direction is
// implemented because eth_client is read-only — it decodes JSON-RPC
// responses but never serializes receipts back out.
//
// Without these methods Thor's hex-encoded receipt fields (e.g.
// BlockNumber = "0x2", status = "0x1", gasUsed = "0x5208") fail to
// decode into the raw *big.Int / uint64 / uint fields of Receipt,
// breaking TransactionReceipt / BlockReceipts.

package ethclient

import (
	"encoding/json"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// UnmarshalJSON unmarshals a Receipt from JSON. Numeric / byte fields are
// decoded through hexutil so that "0x"-prefixed hex strings used by the
// JSON-RPC API are accepted.
func (r *Receipt) UnmarshalJSON(input []byte) error {
	type Receipt struct {
		Type              *hexutil.Uint64 `json:"type,omitempty"`
		PostState         *hexutil.Bytes  `json:"root"`
		Status            *hexutil.Uint64 `json:"status"`
		CumulativeGasUsed *hexutil.Uint64 `json:"cumulativeGasUsed"`
		Bloom             *Bloom          `json:"logsBloom"`
		Logs              []*Log          `json:"logs"`
		TxHash            *common.Hash    `json:"transactionHash"`
		ContractAddress   *common.Address `json:"contractAddress"`
		GasUsed           *hexutil.Uint64 `json:"gasUsed"`
		EffectiveGasPrice *hexutil.Big    `json:"effectiveGasPrice"`
		BlobGasUsed       *hexutil.Uint64 `json:"blobGasUsed,omitempty"`
		BlobGasPrice      *hexutil.Big    `json:"blobGasPrice,omitempty"`
		BlockHash         *common.Hash    `json:"blockHash,omitempty"`
		BlockNumber       *hexutil.Big    `json:"blockNumber,omitempty"`
		TransactionIndex  *hexutil.Uint   `json:"transactionIndex"`
	}
	var dec Receipt
	if err := json.Unmarshal(input, &dec); err != nil {
		return err
	}
	if dec.Type != nil {
		r.Type = uint8(*dec.Type)
	}
	if dec.PostState != nil {
		r.PostState = *dec.PostState
	}
	if dec.Status != nil {
		r.Status = uint64(*dec.Status)
	}
	if dec.CumulativeGasUsed == nil {
		return errors.New("missing required field 'cumulativeGasUsed' for Receipt")
	}
	r.CumulativeGasUsed = uint64(*dec.CumulativeGasUsed)
	if dec.Bloom == nil {
		return errors.New("missing required field 'logsBloom' for Receipt")
	}
	r.Bloom = *dec.Bloom
	if dec.Logs == nil {
		return errors.New("missing required field 'logs' for Receipt")
	}
	r.Logs = dec.Logs
	if dec.TxHash == nil {
		return errors.New("missing required field 'transactionHash' for Receipt")
	}
	r.TxHash = *dec.TxHash
	if dec.ContractAddress != nil {
		r.ContractAddress = *dec.ContractAddress
	}
	if dec.GasUsed == nil {
		return errors.New("missing required field 'gasUsed' for Receipt")
	}
	r.GasUsed = uint64(*dec.GasUsed)
	if dec.EffectiveGasPrice != nil {
		r.EffectiveGasPrice = (*big.Int)(dec.EffectiveGasPrice)
	}
	if dec.BlobGasUsed != nil {
		r.BlobGasUsed = uint64(*dec.BlobGasUsed)
	}
	if dec.BlobGasPrice != nil {
		r.BlobGasPrice = (*big.Int)(dec.BlobGasPrice)
	}
	if dec.BlockHash != nil {
		r.BlockHash = *dec.BlockHash
	}
	if dec.BlockNumber != nil {
		r.BlockNumber = (*big.Int)(dec.BlockNumber)
	}
	if dec.TransactionIndex != nil {
		r.TransactionIndex = uint(*dec.TransactionIndex)
	}
	return nil
}
