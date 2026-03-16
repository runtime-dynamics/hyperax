package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperax/hyperax/pkg/types"
)

func setupTransport(t *testing.T) *SSETransport {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := NewServer(logger)

	// Register a test tool
	server.Registry.Register("ping", "Returns pong", json.RawMessage(`{"type":"object"}`),
		func(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
			return types.NewToolResult("pong"), nil
		},
	)

	return NewSSETransport(server, logger)
}

func postJSON(t *testing.T, handler http.HandlerFunc, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

func TestStreamableHTTP_Initialize(t *testing.T) {
	transport := setupTransport(t)

	rr := postJSON(t, transport.HandleStreamableHTTP, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
	})

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}

	var resp JSONRPCResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Error != nil {
		t.Errorf("unexpected error: %v", resp.Error)
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("protocolVersion = %v", result["protocolVersion"])
	}

	serverInfo := result["serverInfo"].(map[string]any)
	if serverInfo["name"] != "hyperax" {
		t.Errorf("serverInfo.name = %v", serverInfo["name"])
	}
}

func TestStreamableHTTP_ToolsList(t *testing.T) {
	transport := setupTransport(t)

	rr := postJSON(t, transport.HandleStreamableHTTP, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`2`),
		Method:  "tools/list",
	})

	var resp JSONRPCResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error)
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	tools := result["tools"].([]any)
	if len(tools) != 1 {
		t.Errorf("tools count = %d, want 1", len(tools))
	}

	tool := tools[0].(map[string]any)
	if tool["name"] != "ping" {
		t.Errorf("tool name = %v", tool["name"])
	}
}

func TestStreamableHTTP_ToolsCall(t *testing.T) {
	transport := setupTransport(t)

	params, _ := json.Marshal(map[string]any{
		"name":      "ping",
		"arguments": json.RawMessage(`{}`),
	})

	rr := postJSON(t, transport.HandleStreamableHTTP, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`3`),
		Method:  "tools/call",
		Params:  params,
	})

	var resp JSONRPCResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error)
	}

	var result types.ToolResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if result.IsError {
		t.Error("tool result should not be error")
	}
	if len(result.Content) == 0 {
		t.Error("expected content")
	}
}

func TestStreamableHTTP_ToolsCall_UnknownTool(t *testing.T) {
	transport := setupTransport(t)

	params, _ := json.Marshal(map[string]any{
		"name":      "nonexistent",
		"arguments": json.RawMessage(`{}`),
	})

	rr := postJSON(t, transport.HandleStreamableHTTP, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`4`),
		Method:  "tools/call",
		Params:  params,
	})

	var resp JSONRPCResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Error == nil {
		t.Error("expected error for unknown tool")
	}
	if resp.Error.Code != -32000 {
		t.Errorf("error code = %d, want -32000", resp.Error.Code)
	}
}

func TestStreamableHTTP_MethodNotFound(t *testing.T) {
	transport := setupTransport(t)

	rr := postJSON(t, transport.HandleStreamableHTTP, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`5`),
		Method:  "unknown/method",
	})

	var resp JSONRPCResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Error == nil {
		t.Error("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", resp.Error.Code)
	}
}

func TestStreamableHTTP_InvalidJSON(t *testing.T) {
	transport := setupTransport(t)

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	transport.HandleStreamableHTTP(rr, req)

	var resp JSONRPCResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Error == nil {
		t.Error("expected parse error")
	}
	if resp.Error.Code != -32700 {
		t.Errorf("error code = %d, want -32700", resp.Error.Code)
	}
}

func TestGenerateSessionID(t *testing.T) {
	id1 := generateSessionID()
	id2 := generateSessionID()

	if len(id1) != 32 {
		t.Errorf("id length = %d, want 32", len(id1))
	}
	if id1 == id2 {
		t.Error("session IDs should be unique")
	}
}
