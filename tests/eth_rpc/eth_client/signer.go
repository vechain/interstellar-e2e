package ethclient

import (
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// senderFromServer is a types.Signer that remembers the sender address
// returned by the RPC server. It is stored in the transaction's sender cache
// so subsequent TransactionSender calls don't need a round-trip.
type senderFromServer struct {
	addr      common.Address
	blockhash common.Hash
}

var errNotCached = errors.New("sender not cached")

func setSenderFromServer(tx *Transaction2, addr common.Address, block common.Hash) {
	// types.Sender has the side effect of caching our signer; we don't care
	// about the returned address here.
	Sender(&senderFromServer{addr, block}, tx)
}

func (s *senderFromServer) Equal(other Signer) bool {
	os, ok := other.(*senderFromServer)
	return ok && os.blockhash == s.blockhash
}

func (s *senderFromServer) Sender(tx *Transaction2) (common.Address, error) {
	if s.addr == (common.Address{}) {
		return common.Address{}, errNotCached
	}
	return s.addr, nil
}

func (s *senderFromServer) Hash(tx *Transaction2) common.Hash {
	panic("can't sign with senderFromServer")
}

func (s *senderFromServer) SignatureValues(tx *Transaction2, sig []byte) (R, S, V *big.Int, err error) {
	panic("can't sign with senderFromServer")
}

// Sender returns the address derived from the signature (V, R, S) using secp256k1
// elliptic curve and an error if it failed deriving or upon an incorrect
// signature.
//
// Sender may cache the address, allowing it to be used regardless of
// signing method. The cache is invalidated if the cached signer does
// not match the signer used in the current call.
func Sender(signer Signer, tx *Transaction2) (common.Address, error) {
	if sigCache := tx.from.Load(); sigCache != nil {
		// If the signer used to derive from in a previous
		// call is not the same as used current, invalidate
		// the cache.
		if sigCache.signer.Equal(signer) {
			return sigCache.from, nil
		}
	}

	addr, err := signer.Sender(tx)
	if err != nil {
		return common.Address{}, err
	}
	tx.from.Store(&sigCache{signer: signer, from: addr})
	return addr, nil
}
