// Equivalent of the upstream gen_header_rlp.go, but written against the
// legacy rlp.Encode API exposed by the vechain go-ethereum fork
// (NewEncoderBuffer / ErrNegativeBigInt are not available here).
//
// Byte-for-byte compatible with upstream Header.EncodeRLP. The "trailing
// optional with 0x80 placeholders" rule is implemented by substituting
// rlp.RawValue{0x80} for nil pointers before encoding — relying on
// reflection alone is unsafe because reflect's writeInterface emits 0xC0
// (empty list) for a typed-nil pointer wrapped in interface{}, not 0x80.

package ethclient

import (
	"errors"
	"io"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

// rlpEmptyString is the byte the legacy rlp package writes for an empty
// byte string. Used as the placeholder for nil trailing optionals (and as
// a typed-nil substitute for the required *big.Int fields) — same byte the
// upstream generated code writes via rlp.EmptyString.
var rlpEmptyString = rlp.RawValue{0x80}

func (h *Header) EncodeRLP(w io.Writer) error {
	if h.Difficulty != nil && h.Difficulty.Sign() < 0 {
		return errors.New("rlp: cannot encode negative *big.Int (Difficulty)")
	}
	if h.Number != nil && h.Number.Sign() < 0 {
		return errors.New("rlp: cannot encode negative *big.Int (Number)")
	}
	if h.BaseFee != nil && h.BaseFee.Sign() < 0 {
		return errors.New("rlp: cannot encode negative *big.Int (BaseFee)")
	}

	items := make([]interface{}, 0, 23)
	items = append(items,
		h.ParentHash,
		h.UncleHash,
		h.Coinbase,
		h.Root,
		h.TxHash,
		h.ReceiptHash,
		h.Bloom,
		bigIntOrEmpty(h.Difficulty),
		bigIntOrEmpty(h.Number),
		h.GasLimit,
		h.GasUsed,
		h.Time,
		h.Extra,
		h.MixDigest,
		h.Nonce,
	)

	// Trailing-optional rule: if any optional is set, every earlier optional
	// up to it is encoded too (nil ones as a 0x80 placeholder).
	type opt struct {
		nonNil bool
		value  interface{}
	}
	opts := []opt{
		{h.BaseFee != nil, bigIntOrEmpty(h.BaseFee)},
		{h.WithdrawalsHash != nil, hashOrEmpty(h.WithdrawalsHash)},
		{h.BlobGasUsed != nil, uint64OrEmpty(h.BlobGasUsed)},
		{h.ExcessBlobGas != nil, uint64OrEmpty(h.ExcessBlobGas)},
		{h.ParentBeaconRoot != nil, hashOrEmpty(h.ParentBeaconRoot)},
		{h.RequestsHash != nil, hashOrEmpty(h.RequestsHash)},
		{h.BlockAccessListHash != nil, hashOrEmpty(h.BlockAccessListHash)},
		{h.SlotNumber != nil, uint64OrEmpty(h.SlotNumber)},
	}

	last := -1
	for i, o := range opts {
		if o.nonNil {
			last = i
		}
	}
	for i := 0; i <= last; i++ {
		items = append(items, opts[i].value)
	}

	return rlp.Encode(w, items)
}

// bigIntOrEmpty returns the *big.Int wrapped in interface{}, or the 0x80
// placeholder if the pointer is nil. We can't pass a typed-nil *big.Int
// through interface{} to the reflection encoder — reflect's writeInterface
// would emit 0xC0 (empty list) instead of 0x80.
func bigIntOrEmpty(b *big.Int) interface{} {
	if b == nil {
		return rlpEmptyString
	}
	return b
}

func hashOrEmpty(p *common.Hash) interface{} {
	if p == nil {
		return rlpEmptyString
	}
	return p
}

func uint64OrEmpty(p *uint64) interface{} {
	if p == nil {
		return rlpEmptyString
	}
	return p
}

// DecodeRLP is a stub so that the legacy rlp package's genTypeInfo doesn't
// try to build a reflection-based decoder for Header — that would walk the
// struct fields and choke on the rlp:"optional" tag (which the legacy
// package's parseStructTag doesn't recognize). The error would propagate
// back up through cachedTypeInfo and silently empty the hasher inside
// rlpHash, leaving Header.Hash() returning keccak256("").
//
// Header is only ever populated via JSON in this package; we never need to
// decode it from RLP, so returning an error here is fine.
func (h *Header) DecodeRLP(s *rlp.Stream) error {
	return errors.New("rlp: Header decoding is not implemented")
}
