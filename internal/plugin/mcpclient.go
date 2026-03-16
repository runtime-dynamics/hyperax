// Package plugin implements the Hyperax plugin system.
//
// mcpclient.go provides a JSON-RPC 2.0 client that speaks the MCP protocol
// over io.Reader/io.Writer (designed for stdio transport with plugin subprocesses).
// It handles request/response correlation, concurrent calls, and server notifications.
package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
)

// MCPClient is a JSON-RPC 2.0 client that communicates with an MCP plugin server
// over stdio (io.Reader for responses, io.Writer for requests).
//
// It supports:
//   - Synchronous request/response via Send()
//   - Concurrent in-flight requests with ID-based correlation
//   - Asynchronous notification delivery via NotificationHandler
//
// The client spawns a reader goroutine (via Run) that continuously reads
// JSON-RPC messages from the server and dispatches them to pending requests
// or the notification handler.
type MCPClient struct {
	writer io.Writer
	reader io.Reader
	logger *slog.Logger

	// nextID is an atomic counter for generating unique JSON-RPC request IDs.
	nextID atomic.Int64

	// pending maps request IDs to channels waiting for a response.
	mu      sync.Mutex
	pending map[int64]chan *jsonRPCClientResponse

	// NotificationHandler is called for any JSON-RPC message from the server
	// that has no "id" field (i.e., a notification). The method and params
	// are passed directly. This is used by the EventBridge to map plugin
	// notifications to NervousEvent publications.
	NotificationHandler func(method string, params json.RawMessage)

	// done is closed when the reader goroutine exits.
	done chan struct{}
}

// jsonRPCClientRequest is the outbound request format.
type jsonRPCClientRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPCClientResponse is the inbound response/notification format.
type jsonRPCClientResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"` // nil for notifications
	Method  string          `json:"method,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// jsonRPCError is a JSON-RPC 2.0 error object.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// jsonRPCNotification is the outbound notification format.
// Notifications intentionally omit the "id" field per JSON-RPC 2.0 spec.
type jsonRPCNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}


// NewMCPClient creates an MCP client that writes requests to w and reads
// responses/notifications from r. Call Run() to start the reader goroutine.
func NewMCPClient(r io.Reader, w io.Writer, logger *slog.Logger) *MCPClient {
	return &MCPClient{
		writer:  w,
		reader:  r,
		logger:  logger.With("component", "mcp-client"),
		pending: make(map[int64]chan *jsonRPCClientResponse),
		done:    make(chan struct{}),
	}
}

// Run starts the reader goroutine that processes messages from the server.
// It blocks until the reader encounters an error (EOF, closed pipe, etc.)
// or ctx is cancelled. Always call Run in a separate goroutine.
func (c *MCPClient) Run(ctx context.Context) {
	defer close(c.done)

	scanner := bufio.NewScanner(c.reader)
	// Allow up to 1MB per line for large tool results.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg jsonRPCClientResponse
		if err := json.Unmarshal(line, &msg); err != nil {
			c.logger.Warn("failed to parse JSON-RPC message from plugin",
				"error", err.Error(),
				"raw", string(line),
			)
			continue
		}

		if msg.ID != nil {
			// This is a response to a pending request.
			c.mu.Lock()
			ch, ok := c.pending[*msg.ID]
			if ok {
				delete(c.pending, *msg.ID)
			}
			c.mu.Unlock()

			if ok {
				ch <- &msg
			} else {
				c.logger.Warn("received response for unknown request ID",
					"id", *msg.ID,
				)
			}
		} else if msg.Method != "" {
			// This is a notification (no ID).
			if c.NotificationHandler != nil {
				c.NotificationHandler(msg.Method, msg.Params)
			} else {
				c.logger.Debug("received notification but no handler set",
					"method", msg.Method,
				)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		c.logger.Debug("mcp client reader stopped", "error", err.Error())
	}
}

// Send sends a JSON-RPC request and waits for the corresponding response.
// Returns the raw result or an error if the server responded with an error
// object or the context was cancelled.
func (c *MCPClient) Send(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	req := jsonRPCClientRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("plugin.MCPClient.Send: marshal request: %w", err)
	}

	// Register a pending channel before writing to avoid races.
	ch := make(chan *jsonRPCClientResponse, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	// Write the request as a single line followed by newline.
	data = append(data, '\n')
	if _, err := c.writer.Write(data); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("plugin.MCPClient.Send: write request: %w", err)
	}

	// Wait for response or cancellation.
	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("plugin.MCPClient.Send: rpc error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	case <-c.done:
		return nil, fmt.Errorf("plugin.MCPClient.Send: connection closed")
	}
}

// SendNotification sends a JSON-RPC notification (fire-and-forget, no response
// expected). Unlike Send(), this does not include an "id" field and does not
// wait for a reply. Used for server-to-plugin messages like notifications/configChanged.
func (c *MCPClient) SendNotification(method string, params any) error {
	notif := jsonRPCNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	data, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("plugin.MCPClient.SendNotification: marshal: %w", err)
	}
	data = append(data, '\n')
	if _, err := c.writer.Write(data); err != nil {
		return fmt.Errorf("plugin.MCPClient.SendNotification: write: %w", err)
	}
	c.logger.Debug("notification sent to plugin", "method", method)
	return nil
}


// Initialize sends the MCP initialize request and returns the server capabilities.
func (c *MCPClient) Initialize(ctx context.Context) (json.RawMessage, error) {
	params := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "hyperax-plugin-host",
			"version": "0.1.0",
		},
	}

	result, err := c.Send(ctx, "initialize", params)
	if err != nil {
		return nil, fmt.Errorf("plugin.MCPClient.Initialize: %w", err)
	}

	// Send the initialized notification (no response expected).
	if err := c.SendNotification("notifications/initialized", nil); err != nil {
		c.logger.Warn("failed to send initialized notification", "error", err)
	}

	return result, nil
}

// MCPToolDef is the tool definition returned by tools/list.
type MCPToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ListTools sends tools/list and returns the plugin's tool definitions.
func (c *MCPClient) ListTools(ctx context.Context) ([]MCPToolDef, error) {
	result, err := c.Send(ctx, "tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("plugin.MCPClient.ListTools: %w", err)
	}

	var resp struct {
		Tools []MCPToolDef `json:"tools"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("plugin.MCPClient.ListTools: unmarshal: %w", err)
	}

	return resp.Tools, nil
}

// CallTool sends tools/call for the named tool with the given arguments.
// Returns the raw result from the plugin.
func (c *MCPClient) CallTool(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	params := map[string]any{
		"name":      name,
		"arguments": json.RawMessage(args),
	}

	result, err := c.Send(ctx, "tools/call", params)
	if err != nil {
		return nil, fmt.Errorf("plugin.MCPClient.CallTool: %s: %w", name, err)
	}

	return result, nil
}

// Done returns a channel that is closed when the reader goroutine exits.
func (c *MCPClient) Done() <-chan struct{} {
	return c.done
}

// Close cleans up pending requests. The actual transport (stdin/stdout pipes)
// should be closed by the subprocess manager.
func (c *MCPClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
}
