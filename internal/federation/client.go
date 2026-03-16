// Package federation implements MCP federation — the ability for Hyperax to
// connect to external MCP servers over HTTP and proxy their tools into the
// local tool registry.
//
// client.go provides a JSON-RPC 2.0 client that speaks the MCP protocol over
// HTTP (Streamable HTTP transport). Each request is a synchronous POST with a
// JSON-RPC body; the response is read from the HTTP response body.
package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
)

// Client is an MCP client that communicates over HTTP (Streamable HTTP transport).
// Unlike the stdio-based MCPClient in internal/plugin, this client sends each
// JSON-RPC request as an HTTP POST and reads the response synchronously from
// the response body. No background reader goroutine is needed.
type Client struct {
	endpoint   string
	httpClient *http.Client
	nextID     atomic.Int64
	authToken  string // optional Bearer token for remote server auth
}

// NewClient creates an HTTP MCP client for the given endpoint URL.
//
// Parameters:
//   - endpoint: the URL of the remote MCP server (e.g., "https://remote.example.com/mcp")
//   - opts: functional options (WithAuthToken, WithHTTPClient)
func NewClient(endpoint string, opts ...ClientOption) *Client {
	c := &Client{
		endpoint:   endpoint,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithAuthToken sets a Bearer token for authenticating with the remote MCP server.
func WithAuthToken(token string) ClientOption {
	return func(c *Client) { c.authToken = token }
}

// WithHTTPClient replaces the default http.Client (useful for testing or custom TLS).
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *Client) { c.httpClient = hc }
}

// jsonRPCRequest is the outbound JSON-RPC 2.0 request format.
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPCResponse is the inbound JSON-RPC 2.0 response format.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// jsonRPCError is a JSON-RPC 2.0 error object.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ToolDef is the tool definition returned by tools/list. This mirrors
// plugin.MCPToolDef but is defined locally to avoid a cross-package dependency.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// Send sends a JSON-RPC 2.0 request to the remote MCP server and returns the
// raw result. It posts the request as the HTTP body with Content-Type application/json.
//
// Parameters:
//   - ctx: context for cancellation and timeouts
//   - method: the JSON-RPC method name (e.g., "tools/list", "tools/call")
//   - params: the method parameters (will be marshaled to JSON)
//
// Returns the raw JSON result or an error if the server responded with a
// JSON-RPC error, HTTP error, or the context was cancelled.
func (c *Client) Send(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("federation.Client.Send: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("federation.Client.Send: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if c.authToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("federation.Client.Send: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, 10*1024*1024)) // 10MB max
	if err != nil {
		return nil, fmt.Errorf("federation.Client.Send: %w", err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, fmt.Errorf("federation.Client.Send: http %d: %s", httpResp.StatusCode, string(respBody))
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("federation.Client.Send: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("federation.Client.Send: rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	return resp.Result, nil
}

// Initialize sends the MCP initialize handshake and the notifications/initialized
// follow-up. Returns the server capabilities or an error.
func (c *Client) Initialize(ctx context.Context) (json.RawMessage, error) {
	params := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "hyperax-federation",
			"version": "0.1.0",
		},
	}

	result, err := c.Send(ctx, "initialize", params)
	if err != nil {
		return nil, fmt.Errorf("federation.Client.Initialize: %w", err)
	}

	// Send the initialized notification. Per MCP spec this is a notification
	// (no response expected), but we still send it as a JSON-RPC message with
	// ID 0 over HTTP — the server should ignore the response.
	if _, err := c.Send(ctx, "notifications/initialized", nil); err != nil {
		slog.Error("failed to send MCP initialized notification", "error", err)
	}

	return result, nil
}

// ListTools sends tools/list and returns the remote server's tool definitions.
func (c *Client) ListTools(ctx context.Context) ([]ToolDef, error) {
	result, err := c.Send(ctx, "tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("federation.Client.ListTools: %w", err)
	}

	var resp struct {
		Tools []ToolDef `json:"tools"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("federation.Client.ListTools: %w", err)
	}

	return resp.Tools, nil
}

// CallTool sends tools/call for the named tool with the given arguments.
// Returns the raw result from the remote server.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	params := map[string]any{
		"name":      name,
		"arguments": json.RawMessage(args),
	}

	return c.Send(ctx, "tools/call", params)
}

// Close is a no-op for HTTP clients (no persistent connection to tear down).
// It exists to satisfy a consistent interface with the stdio MCPClient.
func (c *Client) Close() {
	// HTTP transport is stateless — nothing to clean up.
}

// Endpoint returns the remote server URL this client is connected to.
func (c *Client) Endpoint() string {
	return c.endpoint
}
