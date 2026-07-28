// WebSocket JSON-RPC harness for eth_subscribe / eth_unsubscribe.
//
// Mirrors rpc_test.go's HTTP harness — same envelope, same schema validation —
// but adds two extras only ws needs:
//
//   1. Notifications. Each `eth_subscribe` response carries a subID; the server
//      then pushes `{"method":"eth_subscription","params":{"subscription":<id>,"result":<payload>}}`
//      frames asynchronously. wsReadNotification dequeues the next one, scoped
//      to a subID and a deadline.
//   2. Frame demuxing. Responses (have an `id`) and notifications (don't) share
//      the same wire. wsCall reads in a loop until a response with the matching
//      id arrives, parking stray notifications into a per-conn queue for later
//      consumption.

package ethrpcschema

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// wsConn wraps a *websocket.Conn with a single-reader mutex and a buffered
// notification queue. Tests use one conn per subscription test; conn.Close()
// is wired into t.Cleanup so a panic or t.Fatal still tears it down.
type wsConn struct {
	conn    *websocket.Conn
	readMu  sync.Mutex
	queueMu sync.Mutex
	queued  []json.RawMessage // notifications received while waiting for an RPC response
}

// wsDial opens a WebSocket against nodeURL/rpc. Thor's eth_eq_json_rpc branch
// upgrades the same path as HTTP POST /rpc (cmd/thor/httpserver/api_server.go:161),
// so the only difference is the scheme.
func wsDial(t *testing.T) *wsConn {
	t.Helper()
	u, err := url.Parse(nodeURL)
	require.NoError(t, err, "parse nodeURL")
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/rpc"

	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second
	c, _, err := dialer.Dial(u.String(), nil)
	require.NoError(t, err, "websocket dial %s", u.String())

	wc := &wsConn{conn: c}
	t.Cleanup(func() {
		_ = c.Close()
	})
	return wc
}

// wsCall sends a JSON-RPC request and reads frames until the matching id
// arrives. Notifications received in the interim are appended to wc.queued so
// wsReadNotification can pick them up later. nil error means a JSON-RPC ok
// response; non-nil means transport failure or a jsonRPCError.
func wsCall(t *testing.T, wc *wsConn, id int, method string, params ...any) (json.RawMessage, error) {
	t.Helper()
	if params == nil {
		params = []any{}
	}
	body, err := json.Marshal(jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	})
	require.NoError(t, err, "marshal ws request")

	if err := wc.conn.WriteMessage(websocket.TextMessage, body); err != nil {
		return nil, fmt.Errorf("ws write: %w", err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for {
		if err := wc.conn.SetReadDeadline(deadline); err != nil {
			return nil, fmt.Errorf("set read deadline: %w", err)
		}
		wc.readMu.Lock()
		_, frame, err := wc.conn.ReadMessage()
		wc.readMu.Unlock()
		if err != nil {
			return nil, fmt.Errorf("ws read: %w", err)
		}

		// Frames without `id` are notifications. Park them for wsReadNotification.
		var probe struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if jsonErr := json.Unmarshal(frame, &probe); jsonErr == nil && probe.ID == nil && probe.Method == "eth_subscription" {
			wc.queueMu.Lock()
			wc.queued = append(wc.queued, append(json.RawMessage(nil), frame...))
			wc.queueMu.Unlock()
			continue
		}

		var resp jsonRPCResponse
		if err := json.Unmarshal(frame, &resp); err != nil {
			return nil, fmt.Errorf("decode ws response: %w (body=%s)", err, string(frame))
		}
		if resp.ID != id {
			// Stray response from a different request — drop on the floor and keep reading.
			continue
		}
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

// wsCallAndValidate is the ws cousin of rpcCallAndValidate: call, fail on
// jsonrpc error, validate the result against schemas/<schemaName>.json.
func wsCallAndValidate(t *testing.T, wc *wsConn, id int, schemaName, method string, params ...any) json.RawMessage {
	t.Helper()
	result, err := wsCall(t, wc, id, method, params...)
	require.NoError(t, err, "%s ws call", method)
	validateResult(t, schemaName, result)
	return result
}

// wsSubscriptionNotification is the on-wire push envelope. We mirror Thor's
// rpc/ws/conn.go:notification — no id, method="eth_subscription",
// params={subscription, result}.
type wsSubscriptionNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  struct {
		Subscription string          `json:"subscription"`
		Result       json.RawMessage `json:"result"`
	} `json:"params"`
}

// wsReadNotification waits for the next notification matching subID, returning
// only the `params.result` payload. Stray notifications for other subscriptions
// are dropped. Honours ctx — pass context.WithTimeout for a real deadline.
//
// The queue is drained first (so notifications that arrived during a prior
// wsCall are consumed in order). Once the queue is empty the helper blocks on
// the socket with the ctx deadline.
func wsReadNotification(t *testing.T, wc *wsConn, ctx context.Context, subID string) (json.RawMessage, error) {
	t.Helper()
	for {
		// Drain queued notifications first.
		wc.queueMu.Lock()
		for len(wc.queued) > 0 {
			frame := wc.queued[0]
			wc.queued = wc.queued[1:]
			wc.queueMu.Unlock()
			var n wsSubscriptionNotification
			if err := json.Unmarshal(frame, &n); err == nil && n.Params.Subscription == subID {
				return n.Params.Result, nil
			}
			wc.queueMu.Lock()
		}
		wc.queueMu.Unlock()

		// Block until ctx deadline or next frame.
		dl, ok := ctx.Deadline()
		if !ok {
			dl = time.Now().Add(30 * time.Second)
		}
		if err := wc.conn.SetReadDeadline(dl); err != nil {
			return nil, fmt.Errorf("set read deadline: %w", err)
		}
		wc.readMu.Lock()
		_, frame, err := wc.conn.ReadMessage()
		wc.readMu.Unlock()
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, fmt.Errorf("ws read: %w", err)
		}

		// Filter to notifications for our subID; everything else is dropped.
		var probe struct {
			ID *int `json:"id"`
		}
		if err := json.Unmarshal(frame, &probe); err == nil && probe.ID != nil {
			// Stray RPC response — drop.
			continue
		}
		var n wsSubscriptionNotification
		if err := json.Unmarshal(frame, &n); err != nil {
			continue
		}
		if n.Method != "eth_subscription" || n.Params.Subscription != subID {
			continue
		}
		return n.Params.Result, nil
	}
}
