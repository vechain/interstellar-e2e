// JSON unmarshaling support for Header, Bloom, and BlockNonce.
//
// Mirrors the generated code from upstream go-ethereum
// (core/types/gen_header_json.go), but only the unmarshal direction is
// implemented because eth_client is read-only — it decodes JSON-RPC
// responses but never serializes block headers back out.
//
// Without these methods Thor's hex-encoded header fields (e.g. Number = "0x1")
// fail to decode into the raw *big.Int / uint64 / [N]byte fields of Header,
// breaking HeaderByNumber / HeaderByHash / BlockByNumber / BlockByHash.

package ethclient

import (
	"encoding/json"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// UnmarshalText decodes a hex-encoded bloom filter from JSON.
func (b *Bloom) UnmarshalText(input []byte) error {
	return hexutil.UnmarshalFixedText("Bloom", input, b[:])
}

// UnmarshalText decodes a hex-encoded block nonce from JSON.
func (n *BlockNonce) UnmarshalText(input []byte) error {
	return hexutil.UnmarshalFixedText("BlockNonce", input, n[:])
}

// UnmarshalJSON unmarshals a Header from JSON. Numeric / byte fields are
// decoded through hexutil so that "0x"-prefixed hex strings used by the
// JSON-RPC API are accepted.
func (h *Header) UnmarshalJSON(input []byte) error {
	type Header struct {
		ParentHash          *common.Hash    `json:"parentHash"`
		UncleHash           *common.Hash    `json:"sha3Uncles"`
		Coinbase            *common.Address `json:"miner"`
		Root                *common.Hash    `json:"stateRoot"`
		TxHash              *common.Hash    `json:"transactionsRoot"`
		ReceiptHash         *common.Hash    `json:"receiptsRoot"`
		Bloom               *Bloom          `json:"logsBloom"`
		Difficulty          *hexutil.Big    `json:"difficulty"`
		Number              *hexutil.Big    `json:"number"`
		GasLimit            *hexutil.Uint64 `json:"gasLimit"`
		GasUsed             *hexutil.Uint64 `json:"gasUsed"`
		Time                *hexutil.Uint64 `json:"timestamp"`
		Extra               *hexutil.Bytes  `json:"extraData"`
		MixDigest           *common.Hash    `json:"mixHash"`
		Nonce               *BlockNonce     `json:"nonce"`
		BaseFee             *hexutil.Big    `json:"baseFeePerGas"`
		WithdrawalsHash     *common.Hash    `json:"withdrawalsRoot"`
		BlobGasUsed         *hexutil.Uint64 `json:"blobGasUsed"`
		ExcessBlobGas       *hexutil.Uint64 `json:"excessBlobGas"`
		ParentBeaconRoot    *common.Hash    `json:"parentBeaconBlockRoot"`
		RequestsHash        *common.Hash    `json:"requestsHash"`
		BlockAccessListHash *common.Hash    `json:"blockAccessListHash"`
		SlotNumber          *hexutil.Uint64 `json:"slotNumber"`
		Hash                *common.Hash    `json:"hash"`
	}
	var dec Header
	if err := json.Unmarshal(input, &dec); err != nil {
		return err
	}
	if dec.ParentHash == nil {
		return errors.New("missing required field 'parentHash' for Header")
	}
	h.ParentHash = *dec.ParentHash
	if dec.UncleHash == nil {
		return errors.New("missing required field 'sha3Uncles' for Header")
	}
	h.UncleHash = *dec.UncleHash
	if dec.Coinbase != nil {
		h.Coinbase = *dec.Coinbase
	}
	if dec.Root == nil {
		return errors.New("missing required field 'stateRoot' for Header")
	}
	h.Root = *dec.Root
	if dec.TxHash == nil {
		return errors.New("missing required field 'transactionsRoot' for Header")
	}
	h.TxHash = *dec.TxHash
	if dec.ReceiptHash == nil {
		return errors.New("missing required field 'receiptsRoot' for Header")
	}
	h.ReceiptHash = *dec.ReceiptHash
	if dec.Bloom == nil {
		return errors.New("missing required field 'logsBloom' for Header")
	}
	h.Bloom = *dec.Bloom
	if dec.Difficulty == nil {
		return errors.New("missing required field 'difficulty' for Header")
	}
	h.Difficulty = (*big.Int)(dec.Difficulty)
	if dec.Number == nil {
		return errors.New("missing required field 'number' for Header")
	}
	h.Number = (*big.Int)(dec.Number)
	if dec.GasLimit == nil {
		return errors.New("missing required field 'gasLimit' for Header")
	}
	h.GasLimit = uint64(*dec.GasLimit)
	if dec.GasUsed == nil {
		return errors.New("missing required field 'gasUsed' for Header")
	}
	h.GasUsed = uint64(*dec.GasUsed)
	if dec.Time == nil {
		return errors.New("missing required field 'timestamp' for Header")
	}
	h.Time = uint64(*dec.Time)
	if dec.Extra == nil {
		return errors.New("missing required field 'extraData' for Header")
	}
	h.Extra = *dec.Extra
	if dec.MixDigest != nil {
		h.MixDigest = *dec.MixDigest
	}
	if dec.Nonce != nil {
		h.Nonce = *dec.Nonce
	}
	if dec.BaseFee != nil {
		h.BaseFee = (*big.Int)(dec.BaseFee)
	}
	if dec.WithdrawalsHash != nil {
		h.WithdrawalsHash = dec.WithdrawalsHash
	}
	if dec.BlobGasUsed != nil {
		h.BlobGasUsed = (*uint64)(dec.BlobGasUsed)
	}
	if dec.ExcessBlobGas != nil {
		h.ExcessBlobGas = (*uint64)(dec.ExcessBlobGas)
	}
	if dec.ParentBeaconRoot != nil {
		h.ParentBeaconRoot = dec.ParentBeaconRoot
	}
	if dec.RequestsHash != nil {
		h.RequestsHash = dec.RequestsHash
	}
	if dec.BlockAccessListHash != nil {
		h.BlockAccessListHash = dec.BlockAccessListHash
	}
	if dec.SlotNumber != nil {
		h.SlotNumber = (*uint64)(dec.SlotNumber)
	}
	return nil
}
