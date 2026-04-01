package helper

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/vechain/thor/v2/block"
	"github.com/vechain/thor/v2/comm/proto"
	"github.com/vechain/thor/v2/p2p"
	"github.com/vechain/thor/v2/p2p/discover"
	"github.com/vechain/thor/v2/p2psrv/rpc"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/tx"
)

// Taken from LocalThreeNodesNetwork
const Node1P2PPort = 8031

// ThorP2PClient connects to a running Thor node via devp2p (RLPx)
// using the thor/1 sub-protocol and allows sending raw blocks.
type ThorP2PClient struct {
	server    *p2p.Server
	genesisID thor.Bytes32
	bestID    thor.Bytes32
	bestScore uint64

	mu        sync.Mutex
	peerRPC   *rpc.RPC
	peerReady chan struct{}
}

// NewThorP2PClient creates a client configured for the status handshake.
// genesisID, bestBlockID, and bestTotalScore are used when responding
// to the remote node's MsgGetStatus call.
func NewThorP2PClient(genesisID, bestBlockID thor.Bytes32, bestTotalScore uint64) *ThorP2PClient {
	return &ThorP2PClient{
		genesisID: genesisID,
		bestID:    bestBlockID,
		bestScore: bestTotalScore,
		peerReady: make(chan struct{}),
	}
}

// Connect dials the target Thor node's P2P port, performs the RLPx
// handshake, negotiates the thor/1 protocol, and exchanges status.
func (c *ThorP2PClient) Connect(targetNodeKey *ecdsa.PrivateKey, targetP2PPort int) error {
	ourKey, err := crypto.GenerateKey()
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}

	c.server = &p2p.Server{
		Config: p2p.Config{
			PrivateKey:  ourKey,
			MaxPeers:    1,
			NoDiscovery: true,
			ListenAddr:  "127.0.0.1:0",
			Name:        "e2e-test-peer",
			Protocols: []p2p.Protocol{
				{
					Name:    proto.Name,
					Version: proto.Version,
					Length:  proto.Length,
					Run:     c.protocolHandler,
				},
			},
		},
	}

	if err := c.server.Start(); err != nil {
		return fmt.Errorf("start p2p server: %w", err)
	}

	targetNodeID := discover.PubkeyID(&targetNodeKey.PublicKey)
	targetNode := &discover.Node{
		ID:  targetNodeID,
		IP:  net.ParseIP("127.0.0.1"),
		TCP: uint16(targetP2PPort),
	}
	c.server.AddPeer(targetNode)

	select {
	case <-c.peerReady:
		return nil
	case <-time.After(15 * time.Second):
		c.server.Stop()
		return fmt.Errorf("timeout waiting for P2P handshake")
	}
}

// SendBlock sends a block to the connected peer via MsgNewBlock (Notify).
func (c *ThorP2PClient) SendBlock(blk *block.Block) error {
	c.mu.Lock()
	r := c.peerRPC
	c.mu.Unlock()

	if r == nil {
		return fmt.Errorf("no peer connection")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return proto.NotifyNewBlock(ctx, r, blk)
}

// Stop tears down the P2P server and all connections.
func (c *ThorP2PClient) Stop() {
	if c.server != nil {
		c.server.Stop()
	}
}

// protocolHandler runs for the thor/1 sub-protocol once the RLPx and
// capability negotiation complete. It responds to the remote node's
// status query and keeps the connection alive for block sending.
func (c *ThorP2PClient) protocolHandler(peer *p2p.Peer, rw p2p.MsgReadWriter) error {
	r := rpc.New(peer, rw)

	c.mu.Lock()
	c.peerRPC = r
	c.mu.Unlock()

	go func() {
		time.Sleep(3 * time.Second)
		select {
		case <-c.peerReady:
		default:
			close(c.peerReady)
		}
	}()

	return r.Serve(func(msg *p2p.Msg, write func(any)) error {
		switch msg.Code {
		case proto.MsgGetStatus:
			if err := msg.Decode(&struct{}{}); err != nil {
				return err
			}
			write(&proto.Status{
				GenesisBlockID: c.genesisID,
				SysTimestamp:   uint64(time.Now().Unix()),
				BestBlockID:    c.bestID,
				TotalScore:     c.bestScore,
			})
		case proto.MsgGetTxs:
			if err := msg.Decode(&struct{}{}); err != nil {
				return err
			}
			write(tx.Transactions(nil))
		default:
			msg.Discard()
		}
		return nil
	}, proto.MaxMsgSize)
}

// FetchRawBlockHeader retrieves a block header in raw RLP form from
// the Thor REST API and decodes it. This exposes fields (Alpha, Beta,
// Signature) that are not available in the standard JSON response.
func FetchRawBlockHeader(nodeURL, revision string) (*block.Header, error) {
	url := fmt.Sprintf("%s/blocks/%s?raw=true", strings.TrimRight(nodeURL, "/"), revision)

	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("fetch raw block header: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("fetch raw block header: HTTP %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Raw string `json:"raw"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	rawHex := strings.TrimPrefix(result.Raw, "0x")
	rawBytes, err := hex.DecodeString(rawHex)
	if err != nil {
		return nil, fmt.Errorf("hex decode: %w", err)
	}

	var header block.Header
	if err := rlp.DecodeBytes(rawBytes, &header); err != nil {
		return nil, fmt.Errorf("RLP decode: %w", err)
	}

	return &header, nil
}
