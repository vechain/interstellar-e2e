package ethclient

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/holiman/uint256"
)

const (
	DynamicFeeTxType = 0x02
)

// FeeHistory provides recent fee market data that consumers can use to determine
// a reasonable maxPriorityFeePerGas value.
type FeeHistory struct {
	OldestBlock  *big.Int     // block corresponding to first response value
	Reward       [][]*big.Int // list every txs priority fee per block
	BaseFee      []*big.Int   // list of each block's base fee
	GasUsedRatio []float64    // ratio of gas used out of the total available limit
}

type feeHistoryResultMarshaling struct {
	OldestBlock  *hexutil.Big     `json:"oldestBlock"`
	Reward       [][]*hexutil.Big `json:"reward,omitempty"`
	BaseFee      []*hexutil.Big   `json:"baseFeePerGas,omitempty"`
	GasUsedRatio []float64        `json:"gasUsedRatio"`
}

type SyncProgress struct {
	StartingBlock uint64 // Block number where sync began
	CurrentBlock  uint64 // Current block number where sync is at
	HighestBlock  uint64 // Highest alleged block number in the chain

	// "fast sync" fields. These used to be sent by geth, but are no longer used
	// since version v1.10.
	PulledStates uint64 // Number of state trie entries already downloaded
	KnownStates  uint64 // Total number of state trie entries known about

	// "snap sync" fields.
	SyncedAccounts      uint64 // Number of accounts downloaded
	SyncedAccountBytes  uint64 // Number of account trie bytes persisted to disk
	SyncedBytecodes     uint64 // Number of bytecodes downloaded
	SyncedBytecodeBytes uint64 // Number of bytecode bytes downloaded
	SyncedStorage       uint64 // Number of storage slots downloaded
	SyncedStorageBytes  uint64 // Number of storage trie bytes persisted to disk

	HealedTrienodes     uint64 // Number of state trie nodes downloaded
	HealedTrienodeBytes uint64 // Number of state trie bytes persisted to disk
	HealedBytecodes     uint64 // Number of bytecodes downloaded
	HealedBytecodeBytes uint64 // Number of bytecodes persisted to disk

	HealingTrienodes uint64 // Number of state trie nodes pending
	HealingBytecode  uint64 // Number of bytecodes pending

	// "transaction indexing" fields
	TxIndexFinishedBlocks  uint64 // Number of blocks whose transactions are already indexed
	TxIndexRemainingBlocks uint64 // Number of blocks whose transactions are not indexed yet

	// "historical data indexing" fields
	StateIndexRemaining    uint64 // Number of states remain unindexed
	TrienodeIndexRemaining uint64 // Number of trienodes remain unindexed
}

// rpcProgress is a copy of SyncProgress with hex-encoded fields.
type rpcProgress struct {
	StartingBlock hexutil.Uint64
	CurrentBlock  hexutil.Uint64
	HighestBlock  hexutil.Uint64

	PulledStates hexutil.Uint64
	KnownStates  hexutil.Uint64

	SyncedAccounts         hexutil.Uint64
	SyncedAccountBytes     hexutil.Uint64
	SyncedBytecodes        hexutil.Uint64
	SyncedBytecodeBytes    hexutil.Uint64
	SyncedStorage          hexutil.Uint64
	SyncedStorageBytes     hexutil.Uint64
	HealedTrienodes        hexutil.Uint64
	HealedTrienodeBytes    hexutil.Uint64
	HealedBytecodes        hexutil.Uint64
	HealedBytecodeBytes    hexutil.Uint64
	HealingTrienodes       hexutil.Uint64
	HealingBytecode        hexutil.Uint64
	TxIndexFinishedBlocks  hexutil.Uint64
	TxIndexRemainingBlocks hexutil.Uint64
	StateIndexRemaining    hexutil.Uint64
	TrienodeIndexRemaining hexutil.Uint64
}

func (p *rpcProgress) toSyncProgress() *SyncProgress {
	if p == nil {
		return nil
	}
	return &SyncProgress{
		StartingBlock:          uint64(p.StartingBlock),
		CurrentBlock:           uint64(p.CurrentBlock),
		HighestBlock:           uint64(p.HighestBlock),
		PulledStates:           uint64(p.PulledStates),
		KnownStates:            uint64(p.KnownStates),
		SyncedAccounts:         uint64(p.SyncedAccounts),
		SyncedAccountBytes:     uint64(p.SyncedAccountBytes),
		SyncedBytecodes:        uint64(p.SyncedBytecodes),
		SyncedBytecodeBytes:    uint64(p.SyncedBytecodeBytes),
		SyncedStorage:          uint64(p.SyncedStorage),
		SyncedStorageBytes:     uint64(p.SyncedStorageBytes),
		HealedTrienodes:        uint64(p.HealedTrienodes),
		HealedTrienodeBytes:    uint64(p.HealedTrienodeBytes),
		HealedBytecodes:        uint64(p.HealedBytecodes),
		HealedBytecodeBytes:    uint64(p.HealedBytecodeBytes),
		HealingTrienodes:       uint64(p.HealingTrienodes),
		HealingBytecode:        uint64(p.HealingBytecode),
		TxIndexFinishedBlocks:  uint64(p.TxIndexFinishedBlocks),
		TxIndexRemainingBlocks: uint64(p.TxIndexRemainingBlocks),
		StateIndexRemaining:    uint64(p.StateIndexRemaining),
		TrienodeIndexRemaining: uint64(p.TrienodeIndexRemaining),
	}
}

type BlockNumber int64

type BlockNumberOrHash struct {
	BlockNumber      *BlockNumber `json:"blockNumber,omitempty"`
	BlockHash        *common.Hash `json:"blockHash,omitempty"`
	RequireCanonical bool         `json:"requireCanonical,omitempty"`
}

func BlockNumberOrHashWithHash(hash common.Hash, canonical bool) BlockNumberOrHash {
	return BlockNumberOrHash{
		BlockNumber:      nil,
		BlockHash:        &hash,
		RequireCanonical: canonical,
	}
}

// AccessList is an EIP-2930 access list.
type AccessList []AccessTuple

// AccessTuple is the element type of an access list.
type AccessTuple struct {
	Address     common.Address `json:"address"     gencodec:"required"`
	StorageKeys []common.Hash  `json:"storageKeys" gencodec:"required"`
}

// SetCodeAuthorization is an authorization from an account to deploy code at its address.
type SetCodeAuthorization struct {
	ChainID uint256.Int    `json:"chainId" gencodec:"required"`
	Address common.Address `json:"address" gencodec:"required"`
	Nonce   uint64         `json:"nonce" gencodec:"required"`
	V       uint8          `json:"yParity" gencodec:"required"`
	R       uint256.Int    `json:"r" gencodec:"required"`
	S       uint256.Int    `json:"s" gencodec:"required"`
}

// CallMsg contains parameters for contract calls.
type CallMsg struct {
	From      common.Address  // the sender of the 'transaction'
	To        *common.Address // the destination contract (nil for contract creation)
	Gas       uint64          // if 0, the call executes with near-infinite gas
	GasPrice  *big.Int        // wei <-> gas exchange ratio
	GasFeeCap *big.Int        // EIP-1559 fee cap per gas.
	GasTipCap *big.Int        // EIP-1559 tip per gas.
	Value     *big.Int        // amount of wei sent along with the call
	Data      []byte          // input data, usually an ABI-encoded contract method invocation

	AccessList AccessList // EIP-2930 access list.

	// For BlobTxType
	BlobGasFeeCap *big.Int
	BlobHashes    []common.Hash

	// For SetCodeTxType
	AuthorizationList []SetCodeAuthorization
}

const (
	// BloomByteLength represents the number of bytes used in a header log bloom.
	BloomByteLength = 256

	// BloomBitLength represents the number of bits used in a header log bloom.
	BloomBitLength = 8 * BloomByteLength
)

type Bloom [BloomByteLength]byte

// A BlockNonce is a 64-bit hash which proves (combined with the
// mix-hash) that a sufficient amount of computation has been carried
// out on a block.
type BlockNonce [8]byte

// Header represents a block header in the Ethereum blockchain.
type Header struct {
	ParentHash  common.Hash    `json:"parentHash"       gencodec:"required"`
	UncleHash   common.Hash    `json:"sha3Uncles"       gencodec:"required"`
	Coinbase    common.Address `json:"miner"`
	Root        common.Hash    `json:"stateRoot"        gencodec:"required"`
	TxHash      common.Hash    `json:"transactionsRoot" gencodec:"required"`
	ReceiptHash common.Hash    `json:"receiptsRoot"     gencodec:"required"`
	Bloom       Bloom          `json:"logsBloom"        gencodec:"required"`
	Difficulty  *big.Int       `json:"difficulty"       gencodec:"required"`
	Number      *big.Int       `json:"number"           gencodec:"required"`
	GasLimit    uint64         `json:"gasLimit"         gencodec:"required"`
	GasUsed     uint64         `json:"gasUsed"          gencodec:"required"`
	Time        uint64         `json:"timestamp"        gencodec:"required"`
	Extra       []byte         `json:"extraData"        gencodec:"required"`
	MixDigest   common.Hash    `json:"mixHash"`
	Nonce       BlockNonce     `json:"nonce"`

	// BaseFee was added by EIP-1559 and is ignored in legacy headers.
	BaseFee *big.Int `json:"baseFeePerGas" rlp:"optional"`

	// WithdrawalsHash was added by EIP-4895 and is ignored in legacy headers.
	WithdrawalsHash *common.Hash `json:"withdrawalsRoot" rlp:"optional"`

	// BlobGasUsed was added by EIP-4844 and is ignored in legacy headers.
	BlobGasUsed *uint64 `json:"blobGasUsed" rlp:"optional"`

	// ExcessBlobGas was added by EIP-4844 and is ignored in legacy headers.
	ExcessBlobGas *uint64 `json:"excessBlobGas" rlp:"optional"`

	// ParentBeaconRoot was added by EIP-4788 and is ignored in legacy headers.
	ParentBeaconRoot *common.Hash `json:"parentBeaconBlockRoot" rlp:"optional"`

	// RequestsHash was added by EIP-7685 and is ignored in legacy headers.
	RequestsHash *common.Hash `json:"requestsHash" rlp:"optional"`

	// BlockAccessListHash was added by EIP-7928 and is ignored in legacy headers.
	BlockAccessListHash *common.Hash `json:"blockAccessListHash" rlp:"optional"`

	// SlotNumber was added by EIP-7843 and is ignored in legacy headers.
	SlotNumber *uint64 `json:"slotNumber" rlp:"optional"`
}

// Transactions implements DerivableList for transactions.
type Transactions []*Transaction2

// Withdrawal represents a validator withdrawal from the consensus layer.
type Withdrawal struct {
	Index     uint64         `json:"index"`          // monotonically increasing identifier issued by consensus layer
	Validator uint64         `json:"validatorIndex"` // index of validator associated with withdrawal
	Address   common.Address `json:"address"`        // target address for withdrawn ether
	Amount    uint64         `json:"amount"`         // value of withdrawal in Gwei
}

// Withdrawals implements DerivableList for withdrawals.
type Withdrawals []*Withdrawal

type BlockAccessList []AccountAccess

// encodingStorageWrite is one transaction's write to a storage slot.
type encodingStorageWrite struct {
	BlockAccessIndex uint32       `json:"blockAccessIndex"`
	PostValue        *uint256.Int `json:"postValue"`
}

// encodingSlotChanges aggregates all per-tx writes to a single storage slot.
type encodingSlotChanges struct {
	Slot        *uint256.Int           `json:"slot"`
	SlotChanges []encodingStorageWrite `json:"slotChanges"`
}

// encodingBalanceChange is one transaction's post-state balance for an account.
type encodingBalanceChange struct {
	BlockAccessIndex uint32       `json:"blockAccessIndex"`
	PostBalance      *uint256.Int `json:"postBalance"`
}

// encodingAccountNonce is one transaction's post-state nonce for an account.
type encodingAccountNonce struct {
	BlockAccessIndex uint32 `json:"blockAccessIndex"`
	PostNonce        uint64 `json:"postNonce"`
}

// encodingCodeChange is one transaction's deployed runtime bytecode for an account.
type encodingCodeChange struct {
	BlockAccessIndex uint32 `json:"blockAccessIndex"`
	NewCode          []byte `json:"newCode"`
}

// AccountAccess is the encoding format of ConstructionAccountAccess.
type AccountAccess struct {
	Address        common.Address          `json:"address"`
	StorageChanges []encodingSlotChanges   `json:"storageChanges"`
	StorageReads   []*uint256.Int          `json:"storageReads"`
	BalanceChanges []encodingBalanceChange `json:"balanceChanges"`
	NonceChanges   []encodingAccountNonce  `json:"nonceChanges"`
	CodeChanges    []encodingCodeChange    `json:"codeChanges"`
}

type Block struct {
	header       *Header
	uncles       []*Header
	transactions Transactions
	withdrawals  Withdrawals
	accessList   BlockAccessList

	// caches
	hash atomic.Pointer[common.Hash]
	size atomic.Uint64

	// These fields are used by package eth to track
	// inter-peer block relay.
	ReceivedAt   time.Time
	ReceivedFrom interface{}
}

type TxData interface {
	txType() byte // returns the type ID
	rawSignatureValues() (v, r, s *big.Int)
	encode(*bytes.Buffer) error
}

type Signer interface {
	Equal(Signer) bool
	// Sender returns the sender address of the transaction.
	Sender(tx *Transaction2) (common.Address, error)
}

type txExtraInfo struct {
	BlockNumber *string         `json:"blockNumber,omitempty"`
	BlockHash   *common.Hash    `json:"blockHash,omitempty"`
	From        *common.Address `json:"from,omitempty"`
}

type rpcTransaction struct {
	tx *Transaction2
	txExtraInfo
}

// sigCache is used to cache the derived sender and contains
// the signer used to derive it.
type sigCache struct {
	signer Signer
	from   common.Address
}

// Transaction is an Ethereum transaction.
type Transaction2 struct {
	inner TxData    // Consensus contents of a transaction
	time  time.Time // Time first seen locally (spam avoidance)

	// caches
	hash atomic.Pointer[common.Hash]
	size atomic.Uint64
	from atomic.Pointer[sigCache]
}

type rpcBlock struct {
	Hash         *common.Hash     `json:"hash"`
	Transactions []rpcTransaction `json:"transactions"`
	UncleHashes  []common.Hash    `json:"uncles"`
	Withdrawals  []*Withdrawal    `json:"withdrawals,omitempty"`
}

// rlpHash encodes x and hashes the encoded bytes.
func rlpHash(x interface{}) (h common.Hash) {
	hw := crypto.NewKeccakState()
	rlp.Encode(hw, x)
	hw.Read(h[:])
	return h
}

// prefixedRlpHash writes the prefix into the hasher before rlp-encoding x.
// It's used for typed transactions.
func prefixedRlpHash(prefix byte, x interface{}) (h common.Hash) {
	hw := crypto.NewKeccakState()
	hw.Write([]byte{prefix})
	rlp.Encode(hw, x)
	hw.Read(h[:])
	return h
}

var (
	// EmptyRootHash is the known root hash of an empty merkle trie.
	EmptyRootHash = common.HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")

	// EmptyUncleHash is the known hash of the empty uncle set.
	EmptyUncleHash = rlpHash([]*Header(nil)) // 1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347

	// EmptyCodeHash is the known hash of the empty EVM bytecode.
	EmptyCodeHash = crypto.Keccak256Hash(nil) // c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470

	// EmptyTxsHash is the known hash of the empty transaction set.
	EmptyTxsHash = common.HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")

	// EmptyReceiptsHash is the known hash of the empty receipt set.
	EmptyReceiptsHash = common.HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")

	// EmptyWithdrawalsHash is the known hash of the empty withdrawal set.
	EmptyWithdrawalsHash = common.HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")

	// EmptyRequestsHash is the known hash of an empty request set, sha256("").
	EmptyRequestsHash = common.HexToHash("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")

	// EmptyBlockAccessListHash is the known hash of an empty block accessList, keccak256(rlp.encode([])).
	EmptyBlockAccessListHash = common.HexToHash("0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347")

	// EmptyBinaryHash is the known hash of an empty binary trie.
	EmptyBinaryHash = common.Hash{}
)

// Hash returns the block hash of the header, which is simply the keccak256 hash of its
// RLP encoding.
func (h *Header) Hash() common.Hash {
	return rlpHash(h)
}

// Body is a simple (mutable, non-safe) data container for storing and moving
// a block's data contents (transactions and uncles) together.
type Body struct {
	Transactions []*Transaction2
	Uncles       []*Header
	Withdrawals  []*Withdrawal `rlp:"optional"`
}

// WithBody returns a new block with the original header and a deep copy of the
// provided body.
func (b *Block) WithBody(body Body) *Block {
	block := &Block{
		header:       b.header,
		transactions: slices.Clone(body.Transactions),
		uncles:       make([]*Header, len(body.Uncles)),
		withdrawals:  slices.Clone(body.Withdrawals),
		accessList:   b.accessList,
	}
	for i := range body.Uncles {
		block.uncles[i] = CopyHeader(body.Uncles[i])
	}
	return block
}

// NewBlockWithHeader creates a block with the given header data. The
// header data is copied, changes to header and to the field values
// will not affect the block.
func NewBlockWithHeader(header *Header) *Block {
	return &Block{header: CopyHeader(header)}
}

// CopyHeader creates a deep copy of a block header.
func CopyHeader(h *Header) *Header {
	cpy := *h
	if cpy.Difficulty = new(big.Int); h.Difficulty != nil {
		cpy.Difficulty.Set(h.Difficulty)
	}
	if cpy.Number = new(big.Int); h.Number != nil {
		cpy.Number.Set(h.Number)
	}
	if h.BaseFee != nil {
		cpy.BaseFee = new(big.Int).Set(h.BaseFee)
	}
	if len(h.Extra) > 0 {
		cpy.Extra = make([]byte, len(h.Extra))
		copy(cpy.Extra, h.Extra)
	}
	if h.WithdrawalsHash != nil {
		cpy.WithdrawalsHash = new(common.Hash)
		*cpy.WithdrawalsHash = *h.WithdrawalsHash
	}
	if h.ExcessBlobGas != nil {
		cpy.ExcessBlobGas = new(uint64)
		*cpy.ExcessBlobGas = *h.ExcessBlobGas
	}
	if h.BlobGasUsed != nil {
		cpy.BlobGasUsed = new(uint64)
		*cpy.BlobGasUsed = *h.BlobGasUsed
	}
	if h.ParentBeaconRoot != nil {
		cpy.ParentBeaconRoot = new(common.Hash)
		*cpy.ParentBeaconRoot = *h.ParentBeaconRoot
	}
	if h.RequestsHash != nil {
		cpy.RequestsHash = new(common.Hash)
		*cpy.RequestsHash = *h.RequestsHash
	}
	if h.BlockAccessListHash != nil {
		cpy.BlockAccessListHash = new(common.Hash)
		*cpy.BlockAccessListHash = *h.BlockAccessListHash
	}
	if h.SlotNumber != nil {
		cpy.SlotNumber = new(uint64)
		*cpy.SlotNumber = *h.SlotNumber
	}
	return &cpy
}

// RawSignatureValues returns the V, R, S signature values of the transaction.
// The return values should not be modified by the caller.
// The return values may be nil or zero, if the transaction is unsigned.
func (tx *Transaction2) RawSignatureValues() (v, r, s *big.Int) {
	return tx.inner.rawSignatureValues()
}

// Receipt represents the results of a transaction.
type Receipt struct {
	// Consensus fields: These fields are defined by the Yellow Paper
	Type              uint8  `json:"type,omitempty"`
	PostState         []byte `json:"root"`
	Status            uint64 `json:"status"`
	CumulativeGasUsed uint64 `json:"cumulativeGasUsed" gencodec:"required"`
	Bloom             Bloom  `json:"logsBloom"         gencodec:"required"`
	Logs              []*Log `json:"logs"              gencodec:"required"`

	// Implementation fields: These fields are added by geth when processing a transaction.
	TxHash            common.Hash    `json:"transactionHash" gencodec:"required"`
	ContractAddress   common.Address `json:"contractAddress"`
	GasUsed           uint64         `json:"gasUsed" gencodec:"required"`
	EffectiveGasPrice *big.Int       `json:"effectiveGasPrice"` // required, but tag omitted for backwards compatibility
	BlobGasUsed       uint64         `json:"blobGasUsed,omitempty"`
	BlobGasPrice      *big.Int       `json:"blobGasPrice,omitempty"`

	// Inclusion information: These fields provide information about the inclusion of the
	// transaction corresponding to this receipt.
	BlockHash        common.Hash `json:"blockHash,omitempty"`
	BlockNumber      *big.Int    `json:"blockNumber,omitempty"`
	TransactionIndex uint        `json:"transactionIndex"`
}

type Log struct {
	// Consensus fields:
	// address of the contract that generated the event
	Address common.Address `json:"address" gencodec:"required"`
	// list of topics provided by the contract.
	Topics []common.Hash `json:"topics" gencodec:"required"`
	// supplied by the contract, usually ABI-encoded
	Data []byte `json:"data" gencodec:"required"`

	// Derived fields. These fields are filled in by the node
	// but not secured by consensus.
	// block in which the transaction was included
	BlockNumber uint64 `json:"blockNumber" rlp:"-"`
	// hash of the transaction
	TxHash common.Hash `json:"transactionHash" gencodec:"required" rlp:"-"`
	// index of the transaction in the block
	TxIndex uint `json:"transactionIndex" rlp:"-"`
	// hash of the block in which the transaction was included
	BlockHash common.Hash `json:"blockHash" rlp:"-"`
	// timestamp of the block in which the transaction was included
	BlockTimestamp uint64 `json:"blockTimestamp" rlp:"-"`
	// index of the log in the block
	Index uint `json:"logIndex" rlp:"-"`

	// The Removed field is true if this log was reverted due to a chain reorganisation.
	// You must pay attention to this field if you receive logs through a filter query.
	Removed bool `json:"removed" rlp:"-"`
}

func toFilterArg(q ethereum.FilterQuery) (interface{}, error) {
	arg := map[string]interface{}{}
	// Only include "address" when there are actual address filters.
	// An empty slice is treated the same as nil (no filter), and omitting
	// the field avoids sending "address":[] to nodes that reject empty arrays
	// (e.g. Hedera, some non-Geth implementations).
	if len(q.Addresses) > 0 {
		arg["address"] = q.Addresses
	}
	if q.Topics != nil {
		arg["topics"] = q.Topics
	}
	if q.BlockHash != nil {
		arg["blockHash"] = *q.BlockHash
		if q.FromBlock != nil || q.ToBlock != nil {
			return nil, errors.New("cannot specify both BlockHash and FromBlock/ToBlock")
		}
	} else {
		if q.FromBlock == nil {
			arg["fromBlock"] = "0x0"
		} else {
			arg["fromBlock"] = toBlockNumArg(q.FromBlock)
		}
		arg["toBlock"] = toBlockNumArg(q.ToBlock)
	}
	return arg, nil
}

// MarshalBinary returns the canonical encoding of the transaction.
// For legacy transactions, it returns the RLP encoding. For EIP-2718 typed
// transactions, it returns the type and payload.
func (tx *Transaction2) MarshalBinary() ([]byte, error) {
	var buf bytes.Buffer
	err := tx.encodeTyped(&buf)
	return buf.Bytes(), err
}

// Type returns the transaction type.
func (tx *Transaction2) Type() uint8 {
	return tx.inner.txType()
}

// Hash returns the transaction hash.
func (tx *Transaction2) Hash() common.Hash {
	if hash := tx.hash.Load(); hash != nil {
		return *hash
	}
	h := prefixedRlpHash(tx.Type(), tx.inner)
	tx.hash.Store(&h)
	return h
}

// encodeTyped writes the canonical encoding of a typed transaction to w.
func (tx *Transaction2) encodeTyped(w *bytes.Buffer) error {
	w.WriteByte(tx.Type())
	return tx.inner.encode(w)
}

// Header returns the block header (as a copy).
func (b *Block) Header() *Header {
	return CopyHeader(b.header)
}

// DynamicFeeTx represents an EIP-1559 dynamic-fee transaction's consensus
// contents. It implements the TxData interface and is the concrete inner
// payload stored in a Transaction2 of type DynamicFeeTxType.
type DynamicFeeTx struct {
	ChainID    *big.Int
	Nonce      uint64
	GasTipCap  *big.Int // a.k.a. maxPriorityFeePerGas
	GasFeeCap  *big.Int // a.k.a. maxFeePerGas
	Gas        uint64
	To         *common.Address `rlp:"nil"` // nil means contract creation
	Value      *big.Int
	Data       []byte
	AccessList AccessList

	// Signature values
	V *big.Int
	R *big.Int
	S *big.Int
}

func (tx *DynamicFeeTx) txType() byte { return DynamicFeeTxType }

func (tx *DynamicFeeTx) rawSignatureValues() (v, r, s *big.Int) {
	return tx.V, tx.R, tx.S
}

// encode writes the RLP encoding of the dynamic-fee payload (without the
// leading type byte) into b. The type byte is prepended by
// Transaction2.encodeTyped.
func (tx *DynamicFeeTx) encode(b *bytes.Buffer) error {
	return rlp.Encode(b, tx)
}

// NewTx wraps the given TxData payload in a Transaction2.
func NewTx(inner TxData) *Transaction2 {
	return &Transaction2{
		inner: inner,
		time:  time.Now(),
	}
}

// SignWith signs tx using prv and returns a new Transaction2 carrying the
// signature. The receiver is left untouched. Currently only DynamicFeeTx is
// supported as the inner payload.
func (tx *Transaction2) SignWith(prv *ecdsa.PrivateKey) (*Transaction2, error) {
	switch inner := tx.inner.(type) {
	case *DynamicFeeTx:
		h := dynamicFeeSigningHash(inner)
		sig, err := crypto.Sign(h[:], prv)
		if err != nil {
			return nil, err
		}
		signed := *inner
		signed.R = new(big.Int).SetBytes(sig[:32])
		signed.S = new(big.Int).SetBytes(sig[32:64])
		signed.V = new(big.Int).SetBytes([]byte{sig[64]})
		return NewTx(&signed), nil
	default:
		return nil, fmt.Errorf("eth_client: SignWith unsupported tx type %T", inner)
	}
}

// txJSON is the JSON wire-format of a transaction. Only the subset of fields
// required to decode an EIP-1559 (DynamicFee) transaction is included.
type txJSON struct {
	Type hexutil.Uint64 `json:"type"`

	ChainID              *hexutil.Big    `json:"chainId,omitempty"`
	Nonce                *hexutil.Uint64 `json:"nonce"`
	To                   *common.Address `json:"to"`
	Gas                  *hexutil.Uint64 `json:"gas"`
	MaxPriorityFeePerGas *hexutil.Big    `json:"maxPriorityFeePerGas"`
	MaxFeePerGas         *hexutil.Big    `json:"maxFeePerGas"`
	Value                *hexutil.Big    `json:"value"`
	Input                *hexutil.Bytes  `json:"input"`
	AccessList           *AccessList     `json:"accessList,omitempty"`
	V                    *hexutil.Big    `json:"v"`
	R                    *hexutil.Big    `json:"r"`
	S                    *hexutil.Big    `json:"s"`
	YParity              *hexutil.Uint64 `json:"yParity,omitempty"`

	Hash common.Hash `json:"hash"`
}

// yParityValue returns the YParity value from JSON. The JSON-RPC response may
// carry it in either the 'v' field or the 'yParity' field; when both are
// present they must agree.
func (j *txJSON) yParityValue() (*big.Int, error) {
	if j.YParity != nil {
		val := uint64(*j.YParity)
		if val != 0 && val != 1 {
			return nil, errors.New("invalid yParity (must be 0 or 1)")
		}
		bigval := new(big.Int).SetUint64(val)
		if j.V != nil && j.V.ToInt().Cmp(bigval) != 0 {
			return nil, errors.New("v / yParity mismatch")
		}
		return bigval, nil
	}
	if j.V != nil {
		return j.V.ToInt(), nil
	}
	return nil, errors.New("missing yParity / v in transaction")
}

// UnmarshalJSON decodes a JSON-RPC eth_getTransactionBy* response into a
// Transaction2. Only DynamicFeeTx (type 0x02) is supported.
func (tx *Transaction2) UnmarshalJSON(input []byte) error {
	var dec txJSON
	if err := json.Unmarshal(input, &dec); err != nil {
		return err
	}
	if byte(dec.Type) != DynamicFeeTxType {
		return fmt.Errorf("eth_client: unsupported transaction type 0x%x (only DynamicFeeTx is supported)", byte(dec.Type))
	}
	var itx DynamicFeeTx
	if dec.ChainID == nil {
		return errors.New("missing required field 'chainId'")
	}
	itx.ChainID = (*big.Int)(dec.ChainID)
	if dec.Nonce == nil {
		return errors.New("missing required field 'nonce'")
	}
	itx.Nonce = uint64(*dec.Nonce)
	itx.To = dec.To
	if dec.Gas == nil {
		return errors.New("missing required field 'gas'")
	}
	itx.Gas = uint64(*dec.Gas)
	if dec.MaxPriorityFeePerGas == nil {
		return errors.New("missing required field 'maxPriorityFeePerGas'")
	}
	itx.GasTipCap = (*big.Int)(dec.MaxPriorityFeePerGas)
	if dec.MaxFeePerGas == nil {
		return errors.New("missing required field 'maxFeePerGas'")
	}
	itx.GasFeeCap = (*big.Int)(dec.MaxFeePerGas)
	if dec.Value == nil {
		return errors.New("missing required field 'value'")
	}
	itx.Value = (*big.Int)(dec.Value)
	if dec.Input == nil {
		return errors.New("missing required field 'input'")
	}
	itx.Data = *dec.Input
	if dec.AccessList != nil {
		itx.AccessList = *dec.AccessList
	}
	if dec.R == nil {
		return errors.New("missing required field 'r'")
	}
	itx.R = (*big.Int)(dec.R)
	if dec.S == nil {
		return errors.New("missing required field 's'")
	}
	itx.S = (*big.Int)(dec.S)
	v, err := dec.yParityValue()
	if err != nil {
		return err
	}
	itx.V = v

	tx.inner = &itx
	tx.time = time.Now()
	return nil
}

// UnmarshalJSON unmarshals a rpcTransaction's tx payload and the surrounding
// txExtraInfo metadata from a single JSON object.
func (rt *rpcTransaction) UnmarshalJSON(msg []byte) error {
	if err := json.Unmarshal(msg, &rt.tx); err != nil {
		return err
	}
	return json.Unmarshal(msg, &rt.txExtraInfo)
}

// UnmarshalJSON decodes a JSON-RPC eth_getLogs / eth_getFilterLogs entry.
// The hex-encoded numeric and byte-slice fields are routed through hexutil
// shims so that "0x..." strings decode correctly instead of producing
// "cannot unmarshal string into Go struct field ... of type uint64".
func (l *Log) UnmarshalJSON(input []byte) error {
	type logJSON struct {
		Address        *common.Address `json:"address"`
		Topics         []common.Hash   `json:"topics"`
		Data           *hexutil.Bytes  `json:"data"`
		BlockNumber    *hexutil.Uint64 `json:"blockNumber"`
		TxHash         *common.Hash    `json:"transactionHash"`
		TxIndex        *hexutil.Uint   `json:"transactionIndex"`
		BlockHash      *common.Hash    `json:"blockHash"`
		BlockTimestamp *hexutil.Uint64 `json:"blockTimestamp"`
		Index          *hexutil.Uint   `json:"logIndex"`
		Removed        *bool           `json:"removed"`
	}
	var dec logJSON
	if err := json.Unmarshal(input, &dec); err != nil {
		return err
	}
	if dec.Address == nil {
		return errors.New("missing required field 'address' for Log")
	}
	l.Address = *dec.Address
	if dec.Topics == nil {
		return errors.New("missing required field 'topics' for Log")
	}
	l.Topics = dec.Topics
	if dec.Data == nil {
		return errors.New("missing required field 'data' for Log")
	}
	l.Data = *dec.Data
	if dec.BlockNumber != nil {
		l.BlockNumber = uint64(*dec.BlockNumber)
	}
	if dec.TxHash == nil {
		return errors.New("missing required field 'transactionHash' for Log")
	}
	l.TxHash = *dec.TxHash
	if dec.TxIndex != nil {
		l.TxIndex = uint(*dec.TxIndex)
	}
	if dec.BlockHash != nil {
		l.BlockHash = *dec.BlockHash
	}
	if dec.BlockTimestamp != nil {
		l.BlockTimestamp = uint64(*dec.BlockTimestamp)
	}
	if dec.Index != nil {
		l.Index = uint(*dec.Index)
	}
	if dec.Removed != nil {
		l.Removed = *dec.Removed
	}
	return nil
}

// dynamicFeeSigningHash returns the keccak256 hash of the EIP-1559 signing
// payload: 0x02 || rlp([chainID, nonce, gasTipCap, gasFeeCap, gas, to, value,
// data, accessList]).
func dynamicFeeSigningHash(tx *DynamicFeeTx) common.Hash {
	h := crypto.NewKeccakState()
	h.Write([]byte{DynamicFeeTxType})
	_ = rlp.Encode(h, []any{
		tx.ChainID,
		tx.Nonce,
		tx.GasTipCap,
		tx.GasFeeCap,
		tx.Gas,
		tx.To,
		tx.Value,
		tx.Data,
		tx.AccessList,
	})
	var out common.Hash
	h.Read(out[:])
	return out
}
