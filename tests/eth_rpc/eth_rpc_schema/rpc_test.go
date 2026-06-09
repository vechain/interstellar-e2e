// JSON-RPC over HTTP test harness for Ethereum-compatible endpoints.
//
// The harness sends standard JSON-RPC 2.0 envelopes to <nodeURL>/rpc and
// validates the returned `result` against a JSON Schema embedded under
// schemas/<method>.json. Schemas reference shared definitions in _defs.json.

package ethrpcschema

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/stretchr/testify/require"
)

//go:embed schemas/*.json
var schemaFS embed.FS

// jsonRPCRequest is the on-wire JSON-RPC 2.0 request envelope.
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

// jsonRPCError is the JSON-RPC 2.0 error object.
type jsonRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *jsonRPCError) Error() string {
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

// jsonRPCResponse is the JSON-RPC 2.0 response envelope.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// rpcCall sends a JSON-RPC 2.0 request to nodeURL+"/rpc" and returns the raw
// `result` field. Non-nil error means either transport-level failure or a
// JSON-RPC error response.
func rpcCall(t *testing.T, method string, params ...any) (json.RawMessage, error) {
	t.Helper()
	if params == nil {
		params = []any{}
	}
	body, err := json.Marshal(jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  params,
	})
	require.NoError(t, err, "marshal request")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, nodeURL+"/rpc", bytes.NewReader(body))
	require.NoError(t, err, "build http request")
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := http.DefaultClient.Do(httpReq)
	require.NoError(t, err, "http POST to rpc endpoint")
	defer httpResp.Body.Close()

	raw, err := io.ReadAll(httpResp.Body)
	require.NoError(t, err, "read http response")

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d: %s", httpResp.StatusCode, string(raw))
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode response: %w (body=%s)", err, string(raw))
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	return resp.Result, nil
}

// compiledSchemas caches one compiler per process; each call to validateResult
// compiles a single root schema but reuses the underlying $ref resolution.
var compiledSchemas = map[string]*jsonschema.Schema{}

// loadSchema reads schemas/<name>.json from the embedded FS and compiles it
// alongside every other schema (so $ref to "_defs.json" or sibling files
// resolves). Compilation is cached per process.
func loadSchema(t *testing.T, name string) *jsonschema.Schema {
	t.Helper()
	if sch, ok := compiledSchemas[name]; ok {
		return sch
	}
	compiler := jsonschema.NewCompiler()
	entries, err := schemaFS.ReadDir("schemas")
	require.NoError(t, err, "read embedded schemas dir")
	for _, e := range entries {
		data, err := schemaFS.ReadFile("schemas/" + e.Name())
		require.NoError(t, err, "read schema %s", e.Name())
		require.NoError(t, compiler.AddResource(e.Name(), bytes.NewReader(data)), "register schema %s", e.Name())
	}
	sch, err := compiler.Compile(name + ".json")
	require.NoError(t, err, "compile schema %s", name)
	compiledSchemas[name] = sch
	return sch
}

// validateResult validates a raw JSON-RPC `result` value against the named
// schema in schemas/<schemaName>.json. Failures include both the validator
// error and the original payload to make root-causing fast.
func validateResult(t *testing.T, schemaName string, result json.RawMessage) {
	t.Helper()
	var v any
	require.NoError(t, json.Unmarshal(result, &v), "unmarshal result for schema validation")
	sch := loadSchema(t, schemaName)
	if err := sch.Validate(v); err != nil {
		t.Fatalf("schema validation failed for %s:\n  error: %v\n  result: %s", schemaName, err, string(result))
	}
}

// rpcCallAndValidate is a convenience: send the request, fail on jsonrpc
// error, then validate the result against schemas/<method>.json (which uses
// the same name as the RPC method).
func rpcCallAndValidate(t *testing.T, method string, params ...any) json.RawMessage {
	t.Helper()
	result, err := rpcCall(t, method, params...)
	require.NoError(t, err, "%s rpc call", method)
	validateResult(t, method, result)
	return result
}
