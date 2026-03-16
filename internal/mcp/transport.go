package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/hyperax/hyperax/internal/web/render"
	"github.com/hyperax/hyperax/pkg/types"
)

// JSONRPCRequest is a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse is a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError is a JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// DispatchFunc is the function signature for tool dispatch (allows middleware wrapping).
type DispatchFunc func(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error)

// SSETransport handles SSE-based MCP communication.
type SSETransport struct {
	server   *Server
	auth     *Authenticator
	dispatch DispatchFunc // defaults to server.Registry.Dispatch
	sessions sync.Map     // sessionID -> chan []byte
	logger   *slog.Logger
}

// NewSSETransport creates an SSE transport for the MCP server.
func NewSSETransport(server *Server, logger *slog.Logger) *SSETransport {
	return &SSETransport{
		server:   server,
		dispatch: server.Registry.Dispatch, // default: no middleware
		logger:   logger,
	}
}

// SetAuthenticator configures bearer token authentication on the transport.
// When set, all MCP requests are validated against the token repository.
func (t *SSETransport) SetAuthenticator(auth *Authenticator) {
	t.auth = auth
}

// SetABAC wraps the dispatch function with ABAC enforcement.
// Must be called after NewSSETransport and before serving requests.
func (t *SSETransport) SetABAC(abac *ABACMiddleware) {
	t.dispatch = abac.WrapDispatch(t.dispatch)
}

// DispatchWrapper wraps a dispatch function with additional middleware logic.
// Used by guard middleware and other dispatch-wrapping layers.
type DispatchWrapper interface {
	WrapDispatch(
		original func(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error),
	) func(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error)
}

// SetGuard wraps the dispatch function with guard enforcement.
// Must be called after SetABAC and before serving requests.
func (t *SSETransport) SetGuard(wrapper DispatchWrapper) {
	t.dispatch = wrapper.WrapDispatch(t.dispatch)
}

// HandleSSE establishes an SSE connection for a single MCP client.
func (t *SSETransport) HandleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		render.Error(w, r, "streaming not supported", http.StatusInternalServerError)
		return
	}

	sessionID := generateSessionID()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Send the endpoint event
	if _, err := fmt.Fprintf(w, "event: endpoint\ndata: /mcp/messages?session_id=%s\n\n", sessionID); err != nil {
		return
	}
	flusher.Flush()

	events := make(chan []byte, 64)
	t.sessions.Store(sessionID, events)
	defer t.sessions.Delete(sessionID)

	t.logger.Info("mcp sse client connected", "session_id", sessionID)

	for {
		select {
		case <-r.Context().Done():
			t.logger.Info("mcp sse client disconnected", "session_id", sessionID)
			return
		case data := <-events:
			if _, err := fmt.Fprintf(w, "event: message\ndata: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// HandleMessage processes a message from an SSE client.
func (t *SSETransport) HandleMessage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Authenticate: use Authenticator if configured, otherwise grant max
	// clearance to localhost connections automatically.
	if t.auth != nil {
		authCtx, ac := t.auth.Authenticate(ctx, r)
		if ac == nil {
			writeJSONRPCError(w, nil, -32001, "authentication required")
			return
		}
		ctx = authCtx
	} else if isLocalhost(r) {
		ac := types.AuthContext{
			PersonaID:      "localhost",
			ClearanceLevel: MaxClearanceLevel,
			Scopes:         []string{"*"},
			Authenticated:  true,
		}
		ctx = withAuthContext(ctx, ac)
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeJSONRPCError(w, nil, -32600, "missing session_id")
		return
	}

	eventsI, ok := t.sessions.Load(sessionID)
	if !ok {
		t.logger.Warn("mcp sse unknown session", "session_id", sessionID)
		writeJSONRPCError(w, nil, -32600, "unknown session")
		return
	}
	events := eventsI.(chan []byte)

	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONRPCError(w, nil, -32700, "parse error")
		return
	}

	t.logger.Info("mcp sse request", "method", req.Method, "session_id", sessionID)
	resp := t.processRequest(ctx, &req)
	t.logger.Info("mcp sse response", "method", req.Method, "has_error", resp.Error != nil)

	data, _ := json.Marshal(resp)
	select {
	case events <- data:
	default:
		t.logger.Warn("sse channel full, dropping response", "session_id", sessionID)
	}

	render.Flush(w, http.StatusAccepted, []byte(`{"status":"accepted"}`))
}

// HandleStreamableHTTP processes a single MCP request over Streamable HTTP.
func (t *SSETransport) HandleStreamableHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Authenticate: use Authenticator if configured, otherwise grant max
	// clearance to localhost connections automatically.
	if t.auth != nil {
		authCtx, ac := t.auth.Authenticate(ctx, r)
		if ac == nil {
			writeJSONRPCError(w, nil, -32001, "authentication required")
			return
		}
		ctx = authCtx
	} else if isLocalhost(r) {
		ac := types.AuthContext{
			PersonaID:      "localhost",
			ClearanceLevel: MaxClearanceLevel,
			Scopes:         []string{"*"},
			Authenticated:  true,
		}
		ctx = withAuthContext(ctx, ac)
	}

	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONRPCError(w, nil, -32700, "parse error")
		return
	}

	t.logger.Debug("mcp streamable request", "method", req.Method)
	resp := t.processRequest(ctx, &req)
	t.logger.Debug("mcp streamable response", "method", req.Method, "has_error", resp.Error != nil)

	// Marshal to buffer so we can set Content-Length, ensuring the client
	// knows exactly when the response is complete (no chunked ambiguity).
	data, err := json.Marshal(resp)
	if err != nil {
		writeJSONRPCError(w, req.ID, -32603, "internal error: failed to marshal response")
		return
	}
	render.Flush(w, http.StatusOK, data)
}

func (t *SSETransport) processRequest(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return t.handleInitialize(req)
	case "tools/list":
		return t.handleToolsList(req)
	case "tools/call":
		return t.handleToolsCall(ctx, req)
	case "notifications/initialized":
		// Client acknowledgment, no response needed
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID}
	default:
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &JSONRPCError{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)},
		}
	}
}

func (t *SSETransport) handleInitialize(req *JSONRPCRequest) *JSONRPCResponse {
	result, _ := json.Marshal(map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"tools": map[string]any{
				"listChanged": false,
			},
		},
		"serverInfo": map[string]any{
			"name":    "hyperax",
			"version": "0.1.0",
		},
	})
	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
}

func (t *SSETransport) handleToolsList(req *JSONRPCRequest) *JSONRPCResponse {
	schemas := t.server.Registry.Schemas()
	tools := make([]map[string]any, len(schemas))
	for i, s := range schemas {
		tools[i] = map[string]any{
			"name":        s.Name,
			"description": s.Description,
			"inputSchema": json.RawMessage(s.InputSchema),
		}
	}
	result, _ := json.Marshal(map[string]any{"tools": tools})
	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
}

func (t *SSETransport) handleToolsCall(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &JSONRPCError{Code: -32602, Message: "invalid params"},
		}
	}

	t.logger.Info("mcp tool call", "tool", params.Name)
	result, err := t.dispatch(ctx, params.Name, params.Arguments)
	if err != nil {
		code := -32000
		// Use -32003 for ABAC denials (error starts with "forbidden:").
		if len(err.Error()) > 10 && err.Error()[:10] == "forbidden:" {
			code = -32003
		}
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &JSONRPCError{Code: code, Message: err.Error()},
		}
	}

	data, _ := json.Marshal(result)
	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: data}
}

func writeJSONRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &JSONRPCError{Code: code, Message: message},
	}
	data, _ := json.Marshal(resp)
	render.Flush(w, http.StatusOK, data)
}

func generateSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}
