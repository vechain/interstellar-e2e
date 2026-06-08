// Package eth_client provides a typed wrapper around the Ethereum JSON-RPC API.
//
// It is a reimplementation of go-ethereum's ethclient package
// (https://github.com/ethereum/go-ethereum/tree/master/ethclient), adapted to
// build against the dependency set used by this module
// (github.com/vechain/go-ethereum). The public surface mirrors the upstream
// client; the following upstream methods are intentionally omitted because
// they require types not present in the dependency set:
//
//   - SendTransaction, SendTransactionSync, SendRawTransactionSync — need
//     Transaction2.MarshalBinary and the concrete TxData hierarchy.
//   - SimulateV1 and its supporting types.
//   - SubscribeNewHead, SubscribeFilterLogs, SubscribeTransactionReceipts —
//     need ethereum.Subscription plumbing.
package ethclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
)

// Client defines typed wrappers for the Ethereum RPC API.
type Client struct {
	c *rpc.Client
}

// Dial connects a client to the given URL.
func Dial(rawurl string) (*Client, error) {
	return DialContext(context.Background(), rawurl)
}

// DialContext connects a client to the given URL with context.
func DialContext(ctx context.Context, rawurl string) (*Client, error) {
	c, err := rpc.DialContext(ctx, rawurl)
	if err != nil {
		return nil, err
	}
	return NewClient(c), nil
}

// NewClient creates a client that uses the given RPC client.
func NewClient(c *rpc.Client) *Client {
	return &Client{c}
}

// Close closes the underlying RPC connection.
func (ec *Client) Close() {
	ec.c.Close()
}

// Client returns the underlying RPC client.
func (ec *Client) Client() *rpc.Client {
	return ec.c
}

// CallRaw invokes the named RPC method with the given positional arguments and
// returns the raw JSON of the response's "result" field. Useful when the
// caller wants to decode the payload itself or pass it through verbatim.
func (ec *Client) CallRaw(ctx context.Context, method string, args ...any) (json.RawMessage, error) {
	var result json.RawMessage
	if err := ec.c.CallContext(ctx, &result, method, args...); err != nil {
		return nil, err
	}
	return result, nil
}

// NetworkID returns the network ID for this client.
func (ec *Client) NetworkID(ctx context.Context) (*big.Int, error) {
	version := new(big.Int)
	var ver string
	if err := ec.c.CallContext(ctx, &ver, "net_version"); err != nil {
		return nil, err
	}
	if _, ok := version.SetString(ver, 0); !ok {
		return nil, fmt.Errorf("invalid net_version result %q", ver)
	}
	return version, nil
}

// PeerCount returns the number of p2p peers as reported by the net_peerCount method.
func (ec *Client) PeerCount(ctx context.Context) (uint64, error) {
	var result hexutil.Uint64
	err := ec.c.CallContext(ctx, &result, "net_peerCount")
	return uint64(result), err
}

// ChainID retrieves the current chain ID for transaction replay protection.
func (ec *Client) ChainID(ctx context.Context) (*big.Int, error) {
	var result hexutil.Big
	err := ec.c.CallContext(ctx, &result, "eth_chainId")
	if err != nil {
		return nil, err
	}
	return (*big.Int)(&result), err
}

// BlockNumber returns the most recent block number
func (ec *Client) BlockNumber(ctx context.Context) (uint64, error) {
	var result hexutil.Uint64
	err := ec.c.CallContext(ctx, &result, "eth_blockNumber")
	return uint64(result), err
}

// SuggestGasPrice retrieves the currently suggested gas price to allow a timely
// execution of a transaction.
func (ec *Client) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	var hex hexutil.Big
	if err := ec.c.CallContext(ctx, &hex, "eth_gasPrice"); err != nil {
		return nil, err
	}
	return (*big.Int)(&hex), nil
}

// SuggestGasTipCap retrieves the currently suggested gas tip cap after 1559 to
// allow a timely execution of a transaction.
func (ec *Client) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	var hex hexutil.Big
	if err := ec.c.CallContext(ctx, &hex, "eth_maxPriorityFeePerGas"); err != nil {
		return nil, err
	}
	return (*big.Int)(&hex), nil
}

// FeeHistory retrieves the fee market history.
func (ec *Client) FeeHistory(ctx context.Context, blockCount uint64, lastBlock *big.Int, rewardPercentiles []float64) (*FeeHistory, error) {
	var res feeHistoryResultMarshaling
	if err := ec.c.CallContext(ctx, &res, "eth_feeHistory", hexutil.Uint(blockCount), toBlockNumArg(lastBlock), rewardPercentiles); err != nil {
		return nil, err
	}
	reward := make([][]*big.Int, len(res.Reward))
	for i, r := range res.Reward {
		reward[i] = make([]*big.Int, len(r))
		for j, r := range r {
			reward[i][j] = (*big.Int)(r)
		}
	}
	baseFee := make([]*big.Int, len(res.BaseFee))
	for i, b := range res.BaseFee {
		baseFee[i] = (*big.Int)(b)
	}
	return &FeeHistory{
		OldestBlock:  (*big.Int)(res.OldestBlock),
		Reward:       reward,
		BaseFee:      baseFee,
		GasUsedRatio: res.GasUsedRatio,
	}, nil
}

func toBlockNumArg(number *big.Int) string {
	if number == nil {
		return "latest"
	}
	if number.Sign() >= 0 {
		return hexutil.EncodeBig(number)
	}
	// It's negative.
	if number.IsInt64() {
		bn := rpc.BlockNumber(number.Int64())
		switch bn {
		case rpc.BlockNumber(-5):
			return "earliest"
		case rpc.BlockNumber(-4):
			return "latest"
		case rpc.BlockNumber(-3):
			return "pending"
		case rpc.BlockNumber(-2):
			return "finalized"
		case rpc.BlockNumber(-1):
			return "safe"
		default:
			if bn < 0 {
				return fmt.Sprintf("<invalid %d>", bn)
			}
			return hexutil.Uint64(bn).String()
		}

	}
	// It's negative and large, which is invalid.
	return fmt.Sprintf("<invalid %d>", number)
}

// BlobBaseFee retrieves the current blob base fee.
func (ec *Client) BlobBaseFee(ctx context.Context) (*big.Int, error) {
	var hex hexutil.Big
	if err := ec.c.CallContext(ctx, &hex, "eth_blobBaseFee"); err != nil {
		return nil, err
	}
	return (*big.Int)(&hex), nil
}

// SyncProgress retrieves the current progress of the sync algorithm. If there's
// no sync currently running, it returns nil.
func (ec *Client) SyncProgress(ctx context.Context) (*SyncProgress, error) {
	var raw json.RawMessage
	if err := ec.c.CallContext(ctx, &raw, "eth_syncing"); err != nil {
		return nil, err
	}
	// Handle the possible response types
	var syncing bool
	if err := json.Unmarshal(raw, &syncing); err == nil {
		return nil, nil // Not syncing (always false)
	}
	var p *rpcProgress
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	return p.toSyncProgress(), nil
}

// PendingBalanceAt returns the wei balance of the given account in the pending state.
func (ec *Client) PendingBalanceAt(ctx context.Context, account common.Address) (*big.Int, error) {
	var result hexutil.Big
	err := ec.c.CallContext(ctx, &result, "eth_getBalance", account, "pending")
	return (*big.Int)(&result), err
}

// BalanceAt returns the wei balance of the given account.
// The block number can be nil, in which case the balance is taken from the latest known block.
func (ec *Client) BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error) {
	var result hexutil.Big
	err := ec.c.CallContext(ctx, &result, "eth_getBalance", account, toBlockNumArg(blockNumber))
	return (*big.Int)(&result), err
}

// BalanceAtHash returns the wei balance of the given account.
func (ec *Client) BalanceAtHash(ctx context.Context, account common.Address, blockHash common.Hash) (*big.Int, error) {
	var result hexutil.Big
	err := ec.c.CallContext(ctx, &result, "eth_getBalance", account, BlockNumberOrHashWithHash(blockHash, false))
	return (*big.Int)(&result), err
}

// StorageAt returns the value of key in the contract storage of the given account.
// The block number can be nil, in which case the value is taken from the latest known block.
func (ec *Client) StorageAt(ctx context.Context, account common.Address, key common.Hash, blockNumber *big.Int) ([]byte, error) {
	var result hexutil.Bytes
	err := ec.c.CallContext(ctx, &result, "eth_getStorageAt", account, key, toBlockNumArg(blockNumber))
	return result, err
}

// StorageAtHash returns the value of key in the contract storage of the given account.
func (ec *Client) StorageAtHash(ctx context.Context, account common.Address, key common.Hash, blockHash common.Hash) ([]byte, error) {
	var result hexutil.Bytes
	err := ec.c.CallContext(ctx, &result, "eth_getStorageAt", account, key, BlockNumberOrHashWithHash(blockHash, false))
	return result, err
}

// PendingStorageAt returns the value of key in the contract storage of the given account in the pending state.
func (ec *Client) PendingStorageAt(ctx context.Context, account common.Address, key common.Hash) ([]byte, error) {
	var result hexutil.Bytes
	err := ec.c.CallContext(ctx, &result, "eth_getStorageAt", account, key, "pending")
	return result, err
}

// PendingNonceAt returns the account nonce of the given account in the pending state.
// This is the nonce that should be used for the next transaction.
func (ec *Client) PendingNonceAt(ctx context.Context, account common.Address) (uint64, error) {
	var result hexutil.Uint64
	err := ec.c.CallContext(ctx, &result, "eth_getTransactionCount", account, "pending")
	return uint64(result), err
}

// NonceAt returns the account nonce of the given account.
// The block number can be nil, in which case the nonce is taken from the latest known block.
func (ec *Client) NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error) {
	var result hexutil.Uint64
	err := ec.c.CallContext(ctx, &result, "eth_getTransactionCount", account, toBlockNumArg(blockNumber))
	return uint64(result), err
}

// NonceAtHash returns the account nonce of the given account.
func (ec *Client) NonceAtHash(ctx context.Context, account common.Address, blockHash common.Hash) (uint64, error) {
	var result hexutil.Uint64
	err := ec.c.CallContext(ctx, &result, "eth_getTransactionCount", account, BlockNumberOrHashWithHash(blockHash, false))
	return uint64(result), err
}

// TransactionCount returns the total number of transactions in the given block.
func (ec *Client) TransactionCount(ctx context.Context, blockHash common.Hash) (uint, error) {
	var num hexutil.Uint
	err := ec.c.CallContext(ctx, &num, "eth_getBlockTransactionCountByHash", blockHash)
	return uint(num), err
}

// PendingTransactionCount returns the total number of transactions in the pending state.
func (ec *Client) PendingTransactionCount(ctx context.Context) (uint, error) {
	var num hexutil.Uint
	err := ec.c.CallContext(ctx, &num, "eth_getBlockTransactionCountByNumber", "pending")
	return uint(num), err
}

// CodeAt returns the contract code of the given account.
// The block number can be nil, in which case the code is taken from the latest known block.
func (ec *Client) CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error) {
	var result hexutil.Bytes
	err := ec.c.CallContext(ctx, &result, "eth_getCode", account, toBlockNumArg(blockNumber))
	return result, err
}

// CodeAtHash returns the contract code of the given account.
func (ec *Client) CodeAtHash(ctx context.Context, account common.Address, blockHash common.Hash) ([]byte, error) {
	var result hexutil.Bytes
	err := ec.c.CallContext(ctx, &result, "eth_getCode", account, BlockNumberOrHashWithHash(blockHash, false))
	return result, err
}

// PendingCodeAt returns the contract code of the given account in the pending state.
func (ec *Client) PendingCodeAt(ctx context.Context, account common.Address) ([]byte, error) {
	var result hexutil.Bytes
	err := ec.c.CallContext(ctx, &result, "eth_getCode", account, "pending")
	return result, err
}

func toCallArg(msg CallMsg) interface{} {
	arg := map[string]interface{}{
		"from": msg.From,
		"to":   msg.To,
	}
	if len(msg.Data) > 0 {
		arg["input"] = hexutil.Bytes(msg.Data)
	}
	if msg.Value != nil {
		arg["value"] = (*hexutil.Big)(msg.Value)
	}
	if msg.Gas != 0 {
		arg["gas"] = hexutil.Uint64(msg.Gas)
	}
	if msg.GasPrice != nil {
		arg["gasPrice"] = (*hexutil.Big)(msg.GasPrice)
	}
	if msg.GasFeeCap != nil {
		arg["maxFeePerGas"] = (*hexutil.Big)(msg.GasFeeCap)
	}
	if msg.GasTipCap != nil {
		arg["maxPriorityFeePerGas"] = (*hexutil.Big)(msg.GasTipCap)
	}
	if msg.AccessList != nil {
		arg["accessList"] = msg.AccessList
	}
	if msg.BlobGasFeeCap != nil {
		arg["maxFeePerBlobGas"] = (*hexutil.Big)(msg.BlobGasFeeCap)
	}
	if msg.BlobHashes != nil {
		arg["blobVersionedHashes"] = msg.BlobHashes
	}
	if msg.AuthorizationList != nil {
		arg["authorizationList"] = msg.AuthorizationList
	}
	return arg
}

// CallContract executes a message call transaction, which is directly executed in the VM
// of the node, but never mined into the blockchain.
//
// blockNumber selects the block height at which the call runs. It can be nil, in which
// case the code is taken from the latest known block. Note that state from very old
// blocks might not be available.
func (ec *Client) CallContract(ctx context.Context, msg CallMsg, blockNumber *big.Int) ([]byte, error) {
	var hex hexutil.Bytes
	err := ec.c.CallContext(ctx, &hex, "eth_call", toCallArg(msg), toBlockNumArg(blockNumber))
	if err != nil {
		return nil, err
	}
	return hex, nil
}

// CallContractAtHash is almost the same as CallContract except that it selects
// the block by block hash instead of block height.
func (ec *Client) CallContractAtHash(ctx context.Context, msg CallMsg, blockHash common.Hash) ([]byte, error) {
	var hex hexutil.Bytes
	err := ec.c.CallContext(ctx, &hex, "eth_call", toCallArg(msg), BlockNumberOrHashWithHash(blockHash, false))
	if err != nil {
		return nil, err
	}
	return hex, nil
}

// PendingCallContract executes a message call transaction using the EVM.
// The state seen by the contract call is the pending state.
func (ec *Client) PendingCallContract(ctx context.Context, msg CallMsg) ([]byte, error) {
	var hex hexutil.Bytes
	err := ec.c.CallContext(ctx, &hex, "eth_call", toCallArg(msg), "pending")
	if err != nil {
		return nil, err
	}
	return hex, nil
}

// EstimateGas tries to estimate the gas needed to execute a specific transaction based on
// the current state of the backend blockchain. There is no guarantee that this is the
// true gas limit requirement as other transactions may be added or removed by miners, but
// it should provide a basis for setting a reasonable default.
//
// Note that the state used by this method is implementation-defined by the remote RPC
// server, but it's reasonable to assume that it will either be the pending or latest
// state.
func (ec *Client) EstimateGas(ctx context.Context, msg CallMsg) (uint64, error) {
	var hex hexutil.Uint64
	err := ec.c.CallContext(ctx, &hex, "eth_estimateGas", toCallArg(msg))
	if err != nil {
		return 0, err
	}
	return uint64(hex), nil
}

// EstimateGasAtBlock is almost the same as EstimateGas except that it selects the block height
// instead of using the remote RPC's default state for gas estimation.
func (ec *Client) EstimateGasAtBlock(ctx context.Context, msg CallMsg, blockNumber *big.Int) (uint64, error) {
	var hex hexutil.Uint64
	err := ec.c.CallContext(ctx, &hex, "eth_estimateGas", toCallArg(msg), toBlockNumArg(blockNumber))
	if err != nil {
		return 0, err
	}
	return uint64(hex), nil
}

// EstimateGasAtBlockHash is almost the same as EstimateGas except that it selects the block
// hash instead of using the remote RPC's default state for gas estimation.
func (ec *Client) EstimateGasAtBlockHash(ctx context.Context, msg CallMsg, blockHash common.Hash) (uint64, error) {
	var hex hexutil.Uint64
	err := ec.c.CallContext(ctx, &hex, "eth_estimateGas", toCallArg(msg), BlockNumberOrHashWithHash(blockHash, false))
	if err != nil {
		return 0, err
	}
	return uint64(hex), nil
}

// HeaderByHash returns the block header with the given hash.
func (ec *Client) HeaderByHash(ctx context.Context, hash common.Hash) (*Header, error) {
	var head *Header
	err := ec.c.CallContext(ctx, &head, "eth_getBlockByHash", hash, false)
	if err == nil && head == nil {
		err = ethereum.NotFound
	}
	return head, err
}

// BlockByHash returns the given full block.
//
// Note that loading full blocks requires two requests. Use HeaderByHash
// if you don't need all transactions or uncle headers.
func (ec *Client) BlockByHash(ctx context.Context, hash common.Hash) (*Block, error) {
	return ec.getBlock(ctx, "eth_getBlockByHash", hash, true)
}

// HeaderByNumber returns a block header from the current canonical chain.
// If `number` is nil, the latest known block header is returned.
//
// Supported special block number tags:
// - `earliest`  : The genesis (earliest) block
// - `latest`    : The most recently included block
// - `safe`      : The latest safe head block
// - `finalized` : The latest finalized block
// - `pending`   : The pending block
//
// Example usage:
//
// ```go
// HeaderByNumber(context.Background(), big.NewInt(int64(rpc.LatestBlockNumber)))
// ```
func (ec *Client) HeaderByNumber(ctx context.Context, number *big.Int) (*Header, error) {
	var head *Header
	err := ec.c.CallContext(ctx, &head, "eth_getBlockByNumber", toBlockNumArg(number), false)
	if err == nil && head == nil {
		err = ethereum.NotFound
	}
	return head, err
}

// BlockByNumber returns a block from the current canonical chain.
// If `number` is nil, the latest known block is returned.
//
// Use `HeaderByNumber` if you don't need full transaction data or uncle headers.
//
// Supported special block number tags:
// - `earliest`  : The genesis (earliest) block
// - `latest`    : The most recently included block
// - `safe`      : The latest safe head block
// - `finalized` : The latest finalized block
// - `pending`   : The pending block
//
// Example usage:
//
// ```go
// BlockByNumber(context.Background(), big.NewInt(int64(rpc.LatestBlockNumber)))
// ```
func (ec *Client) BlockByNumber(ctx context.Context, number *big.Int) (*Block, error) {
	return ec.getBlock(ctx, "eth_getBlockByNumber", toBlockNumArg(number), true)
}

func (ec *Client) getBlock(ctx context.Context, method string, args ...interface{}) (*Block, error) {
	var raw json.RawMessage
	err := ec.c.CallContext(ctx, &raw, method, args...)
	if err != nil {
		return nil, err
	}

	// Decode header and transactions.
	var head *Header
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, err
	}
	// When the block is not found, the API returns JSON null.
	if head == nil {
		return nil, ethereum.NotFound
	}

	var body rpcBlock
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	// Pending blocks don't return a block hash, compute it for sender caching.
	if body.Hash == nil {
		tmp := head.Hash()
		body.Hash = &tmp
	}

	// Quick-verify transaction and uncle lists. This mostly helps with debugging the server.
	if head.UncleHash == EmptyUncleHash && len(body.UncleHashes) > 0 {
		return nil, errors.New("server returned non-empty uncle list but block header indicates no uncles")
	}
	if head.UncleHash != EmptyUncleHash && len(body.UncleHashes) == 0 {
		return nil, errors.New("server returned empty uncle list but block header indicates uncles")
	}
	if head.TxHash == EmptyTxsHash && len(body.Transactions) > 0 {
		return nil, errors.New("server returned non-empty transaction list but block header indicates no transactions")
	}
	if head.TxHash != EmptyTxsHash && len(body.Transactions) == 0 {
		return nil, errors.New("server returned empty transaction list but block header indicates transactions")
	}
	// Load uncles because they are not included in the block response.
	var uncles []*Header
	if len(body.UncleHashes) > 0 {
		uncles = make([]*Header, len(body.UncleHashes))
		reqs := make([]rpc.BatchElem, len(body.UncleHashes))
		for i := range reqs {
			reqs[i] = rpc.BatchElem{
				Method: "eth_getUncleByBlockHashAndIndex",
				Args:   []interface{}{body.Hash, hexutil.EncodeUint64(uint64(i))},
				Result: &uncles[i],
			}
		}
		if err := ec.c.BatchCallContext(ctx, reqs); err != nil {
			return nil, err
		}
		for i := range reqs {
			if reqs[i].Error != nil {
				return nil, reqs[i].Error
			}
			if uncles[i] == nil {
				return nil, fmt.Errorf("got null header for uncle %d of block %x", i, body.Hash[:])
			}
		}
	}

	// Fill the sender cache of transactions in the block.
	txs := make([]*Transaction2, len(body.Transactions))
	for i, tx := range body.Transactions {
		if tx.From != nil {
			setSenderFromServer(tx.tx, *tx.From, *body.Hash)
		}
		txs[i] = tx.tx
	}

	return NewBlockWithHeader(head).WithBody(
		Body{
			Transactions: txs,
			Uncles:       uncles,
			Withdrawals:  body.Withdrawals,
		}), nil
}

// TransactionByHash returns the transaction with the given hash.
func (ec *Client) TransactionByHash(ctx context.Context, hash common.Hash) (tx *Transaction2, isPending bool, err error) {
	var json *rpcTransaction
	err = ec.c.CallContext(ctx, &json, "eth_getTransactionByHash", hash)
	if err != nil {
		return nil, false, err
	} else if json == nil {
		return nil, false, ethereum.NotFound
	} else if _, r, _ := json.tx.RawSignatureValues(); r == nil {
		return nil, false, errors.New("server returned transaction without signature")
	}
	if json.From != nil && json.BlockHash != nil {
		setSenderFromServer(json.tx, *json.From, *json.BlockHash)
	}
	return json.tx, json.BlockNumber == nil, nil
}

// TransactionInBlock returns a single transaction at index in the given block.
func (ec *Client) TransactionInBlock(ctx context.Context, blockHash common.Hash, index uint) (*Transaction2, error) {
	var json *rpcTransaction
	err := ec.c.CallContext(ctx, &json, "eth_getTransactionByBlockHashAndIndex", blockHash, hexutil.Uint64(index))
	if err != nil {
		return nil, err
	}
	if json == nil {
		return nil, ethereum.NotFound
	} else if _, r, _ := json.tx.RawSignatureValues(); r == nil {
		return nil, errors.New("server returned transaction without signature")
	}
	if json.From != nil && json.BlockHash != nil {
		setSenderFromServer(json.tx, *json.From, *json.BlockHash)
	}
	return json.tx, err
}

func (ec *Client) TransactionSender(ctx context.Context, tx *Transaction2, block common.Hash, index uint) (common.Address, error) {
	if sender, err := Sender(&senderFromServer{blockhash: block}, tx); err == nil {
		return sender, nil
	}
	var meta struct {
		Hash common.Hash    `json:"hash"`
		From common.Address `json:"from"`
	}
	if err := ec.c.CallContext(ctx, &meta, "eth_getTransactionByBlockHashAndIndex", block, hexutil.Uint64(index)); err != nil {
		return common.Address{}, err
	}
	if meta.Hash == (common.Hash{}) {
		return common.Address{}, errors.New("wrong inclusion block/index")
	}
	return meta.From, nil
}

// TransactionReceipt returns the receipt of a transaction by transaction hash.
// Note that the receipt is not available for pending transactions.
func (ec *Client) TransactionReceipt(ctx context.Context, txHash common.Hash) (*Receipt, error) {
	var r *Receipt
	err := ec.c.CallContext(ctx, &r, "eth_getTransactionReceipt", txHash)
	if err == nil && r == nil {
		return nil, ethereum.NotFound
	}
	return r, err
}

// BlockReceipts returns the receipts of a given block number or hash.
func (ec *Client) BlockReceipts(ctx context.Context, blockNrOrHash BlockNumberOrHash) ([]*Receipt, error) {
	var r []*Receipt
	err := ec.c.CallContext(ctx, &r, "eth_getBlockReceipts", blockNrOrHash)
	if err == nil && r == nil {
		return nil, ethereum.NotFound
	}
	return r, err
}

// FilterLogs executes a filter query.
func (ec *Client) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]Log, error) {
	var result []Log
	arg, err := toFilterArg(q)
	if err != nil {
		return nil, err
	}
	err = ec.c.CallContext(ctx, &result, "eth_getLogs", arg)
	return result, err
}

// dataError is satisfied by JSON-RPC error implementations that carry a Data
// payload (e.g. Geth's `*jsonError`). The vechain/go-ethereum v1.8.x rpc
// package does not export this interface, so it is declared locally.
type dataError interface {
	ErrorData() any
}

// RevertErrorData returns the revert reason data if `err` is a JSON-RPC error
// from a contract call. This is geth-specific behavior — error code 3 carries
// the revert payload as a hex-encoded string. Backends that don't surface
// revert data (or use a different error code) return (nil, false).
func RevertErrorData(err error) ([]byte, bool) {
	var ec rpc.Error
	var ed dataError
	if errors.As(err, &ec) && errors.As(err, &ed) && ec.ErrorCode() == 3 {
		if eds, ok := ed.ErrorData().(string); ok {
			if revertData, decErr := hexutil.Decode(eds); decErr == nil {
				return revertData, true
			}
		}
	}
	return nil, false
}

// SendTransaction injects a signed transaction into the pending pool for execution.
//
// If the transaction was a contract creation use the TransactionReceipt method to get the
// contract address after the transaction has been mined.
func (ec *Client) SendTransaction(ctx context.Context, tx *Transaction2) (hexutil.Bytes, error) {
	data, err := tx.MarshalBinary()
	var result hexutil.Bytes
	if err != nil {
		return nil, err
	}
	err = ec.c.CallContext(ctx, &result, "eth_sendRawTransaction", hexutil.Encode(data))
	return result, err
}
