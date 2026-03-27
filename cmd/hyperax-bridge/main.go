// Package main implements the hyperax-bridge binary — a standalone MCP channel server
// that connects Claude Code to a running HyperAX instance over stdio (JSON-RPC 2.0).
//
// Architecture:
//
//	Claude Code <--stdin/stdout--> hyperax-bridge <--HTTP--> HyperAX Server
//
// The bridge registers as a channel session with HyperAX, long-polls for events,
// and relays them to Claude Code as MCP channel notifications. Claude Code can
// respond via the "reply" tool and forward permission requests back through the bridge.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const (
	// bridgeVersion is the semantic version reported in MCP serverInfo.
	bridgeVersion = "0.1.0"

	// protocolVersion is the MCP protocol version we implement.
	protocolVersion = "2024-11-05"

	// eventPollInterval is how often we poll HyperAX for new events.
	eventPollInterval = 2 * time.Second

	// heartbeatInterval is how often we send keepalive heartbeats.
	heartbeatInterval = 15 * time.Second

	// httpTimeout is the default timeout for outbound HTTP requests to HyperAX.
	httpTimeout = 10 * time.Second
)

// ---------------------------------------------------------------------------
// JSON-RPC types
// ---------------------------------------------------------------------------

// jsonRPCRequest represents an inbound JSON-RPC 2.0 request from Claude Code.
type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // may be number, string, or null
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// jsonRPCResponse is an outbound JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

// jsonRPCNotification is an outbound JSON-RPC 2.0 notification (no ID).
type jsonRPCNotification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  any `json:"params,omitempty"`
}

// rpcError is the JSON-RPC error object.
type rpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    any `json:"data,omitempty"`
}

// ---------------------------------------------------------------------------
// MCP capability / tool types
// ---------------------------------------------------------------------------

// initializeResult is the response to the MCP "initialize" request.
type initializeResult struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      serverInfo             `json:"serverInfo"`
	Instructions    string                 `json:"instructions"`
}

// serverInfo identifies this MCP server.
type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// toolDefinition describes a single MCP tool.
type toolDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema any `json:"inputSchema"`
}

// toolsListResult is the response to "tools/list".
type toolsListResult struct {
	Tools []toolDefinition `json:"tools"`
}

// toolCallParams is the params object for a "tools/call" request.
type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// replyArguments captures the arguments for the "reply" tool.
type replyArguments struct {
	SessionID string `json:"session_id,omitempty"`
	Text      string `json:"text"`
	TaskID    string `json:"task_id,omitempty"`
	Status    string `json:"status,omitempty"`
}

// toolCallResult is the response envelope for a "tools/call" result.
type toolCallResult struct {
	Content []toolResultContent `json:"content"`
	IsError bool                `json:"isError,omitempty"`
}

// toolResultContent is a single content block inside a tool call result.
type toolResultContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ---------------------------------------------------------------------------
// Channel event types (from HyperAX)
// ---------------------------------------------------------------------------

// channelEvent represents a single event received from the HyperAX event poll.
type channelEvent struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	Meta      string `json:"meta"`       // JSON string from server; parsed in relayEvent
	EventType string `json:"event_type"`
}

// channelNotificationParams is sent to Claude Code as a channel notification.
type channelNotificationParams struct {
	Content string                 `json:"content"`
	Meta    map[string]any `json:"meta"`
}

// permissionRequestParams is received from Claude Code when it needs approval.
type permissionRequestParams struct {
	RequestID    string `json:"request_id"`
	ToolName     string `json:"tool_name"`
	Description  string `json:"description"`
	InputPreview string `json:"input_preview"`
}

// permissionVerdict is sent to Claude Code via notifications/claude/channel/permission
// when HyperAX returns a permission decision for a previously forwarded request.
type permissionVerdict struct {
	RequestID string `json:"request_id"`
	Behavior  string `json:"behavior"`
}

// ---------------------------------------------------------------------------
// Bridge
// ---------------------------------------------------------------------------

// bufferedMessage is a channel event held in local memory until Claude Code
// retrieves it via the check_messages tool.
type bufferedMessage struct {
	ID         string         `json:"id"`
	Content    string         `json:"content"`
	Meta       map[string]any `json:"meta"`
	EventType  string         `json:"event_type"`
	ReceivedAt time.Time      `json:"received_at"`
}

// bridge is the core runtime that connects Claude Code (stdio) to HyperAX (HTTP).
type bridge struct {
	hyperaxURL  string
	sessionName string
	workspaceID string
	sessionID   string

	httpClient *http.Client
	logger     *slog.Logger

	// writer serialises JSON-RPC writes to stdout.
	writerMu sync.Mutex
	writer   *json.Encoder

	// msgBuf holds channel events that were polled from HyperAX but not yet
	// retrieved by Claude Code via check_messages.
	msgBufMu sync.Mutex
	msgBuf   []bufferedMessage
}

// newBridge constructs a bridge instance. It does not start any goroutines.
func newBridge(hyperaxURL, sessionName, workspaceID string, logger *slog.Logger) *bridge {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	return &bridge{
		hyperaxURL:  hyperaxURL,
		sessionName: sessionName,
		workspaceID: workspaceID,
		httpClient: &http.Client{
			Timeout: httpTimeout,
		},
		logger: logger,
		writer: enc,
	}
}

// ---------------------------------------------------------------------------
// Outbound JSON-RPC helpers
// ---------------------------------------------------------------------------

// sendResponse writes a JSON-RPC response to stdout (thread-safe).
func (b *bridge) sendResponse(id json.RawMessage, result any, rpcErr *rpcError) error {
	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
		Error:   rpcErr,
	}
	b.writerMu.Lock()
	defer b.writerMu.Unlock()
	return b.writer.Encode(resp)
}

// sendNotification writes a JSON-RPC notification to stdout (thread-safe).
func (b *bridge) sendNotification(method string, params any) error {
	notif := jsonRPCNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	b.writerMu.Lock()
	defer b.writerMu.Unlock()
	return b.writer.Encode(notif)
}

// ---------------------------------------------------------------------------
// HyperAX HTTP helpers
// ---------------------------------------------------------------------------

// register creates a channel session with HyperAX and stores the session ID.
func (b *bridge) register(ctx context.Context) error {
	body := map[string]string{
		"name":         b.sessionName,
		"workspace_id": b.workspaceID,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal register body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.hyperaxURL+"/api/v1/channels/register", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create register request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("register request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			b.logger.Warn("failed to close register response body", "error", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("register failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var result struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode register response: %w", err)
	}
	if result.SessionID == "" {
		return fmt.Errorf("register response missing session_id")
	}

	b.sessionID = result.SessionID
	b.logger.Info("registered with HyperAX", "session_id", b.sessionID)
	return nil
}

// deregister removes the channel session from HyperAX. Best-effort; errors are logged.
func (b *bridge) deregister() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, b.hyperaxURL+"/api/v1/channels/"+b.sessionID, nil)
	if err != nil {
		b.logger.Error("failed to create deregister request", "error", err)
		return
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		b.logger.Error("deregister request failed", "error", err)
		return
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			b.logger.Warn("failed to close deregister response body", "error", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		b.logger.Warn("deregister returned non-200", "status", resp.StatusCode)
		return
	}
	b.logger.Info("deregistered from HyperAX", "session_id", b.sessionID)
}

// sendHeartbeat posts a keepalive to HyperAX. Returns an error on failure.
func (b *bridge) sendHeartbeat(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.hyperaxURL+"/api/v1/channels/"+b.sessionID+"/heartbeat", nil)
	if err != nil {
		return fmt.Errorf("create heartbeat request: %w", err)
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("heartbeat request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			b.logger.Warn("failed to close heartbeat response body", "error", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("heartbeat returned status %d", resp.StatusCode)
	}
	return nil
}

// pollEvents fetches pending events from HyperAX for this session.
func (b *bridge) pollEvents(ctx context.Context) ([]channelEvent, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.hyperaxURL+"/api/v1/channels/"+b.sessionID+"/events", nil)
	if err != nil {
		return nil, fmt.Errorf("create poll request: %w", err)
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("poll request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			b.logger.Warn("failed to close poll response body", "error", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("poll returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var envelope struct {
		Events []channelEvent `json:"events"`
		Count  int            `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode poll response: %w", err)
	}
	return envelope.Events, nil
}

// postReply sends a reply to HyperAX for the given session.
func (b *bridge) postReply(ctx context.Context, sessionID string, args replyArguments) error {
	body := map[string]string{
		"content": args.Text,
	}
	if args.TaskID != "" {
		body["task_id"] = args.TaskID
	}
	if args.Status != "" {
		body["status"] = args.Status
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal reply body: %w", err)
	}

	targetSession := b.sessionID
	if sessionID != "" {
		targetSession = sessionID
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.hyperaxURL+"/api/v1/channels/"+targetSession+"/reply", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create reply request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("reply request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			b.logger.Warn("failed to close reply response body", "error", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("reply returned status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// forwardPermissionRequest relays a permission request from Claude Code to HyperAX.
func (b *bridge) forwardPermissionRequest(ctx context.Context, params permissionRequestParams) error {
	payload, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshal permission request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.hyperaxURL+"/api/v1/channels/"+b.sessionID+"/permission", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create permission request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("permission request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			b.logger.Warn("failed to close permission response body", "error", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("permission request returned status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// ---------------------------------------------------------------------------
// MCP request handlers
// ---------------------------------------------------------------------------

// handleInitialize responds to the MCP "initialize" request.
func (b *bridge) handleInitialize(id json.RawMessage) error {
	result := initializeResult{
		ProtocolVersion: protocolVersion,
		Capabilities: map[string]any{
			"experimental": map[string]any{
				"claude/channel":            map[string]any{},
				"claude/channel/permission": map[string]any{},
			},
			"tools": map[string]any{},
		},
		ServerInfo: serverInfo{
			Name:    "hyperax",
			Version: bridgeVersion,
		},
		Instructions: "Messages arrive as <channel source=\"hyperax\" ...>. " +
			"For task assignments, work on the task and report back via the reply tool with status. " +
			"For chat messages, respond via the reply tool.",
	}
	return b.sendResponse(id, result, nil)
}

// handleToolsList responds to the MCP "tools/list" request.
func (b *bridge) handleToolsList(id json.RawMessage) error {
	result := toolsListResult{
		Tools: []toolDefinition{
			{
				Name:        "reply",
				Description: "Send a reply back to HyperAX (task completion, chat response, or status update)",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"session_id": map[string]any{
							"type":        "string",
							"description": "The session to reply to (from channel meta)",
						},
						"text": map[string]any{
							"type":        "string",
							"description": "The reply content",
						},
						"task_id": map[string]any{
							"type":        "string",
							"description": "Task ID if this is a task completion (from channel meta)",
						},
						"status": map[string]any{
							"type":        "string",
							"enum":        []string{"completed", "blocked", "needs_review", "in_progress"},
							"description": "Task status update",
						},
					},
					"required": []string{"text"},
				},
			},
			{
				Name:        "check_messages",
				Description: "Check for new messages from HyperAX. Call this periodically or when you expect incoming messages. Returns any buffered channel events that arrived since the last check.",
				InputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
		},
	}
	return b.sendResponse(id, result, nil)
}

// handleToolCall dispatches a "tools/call" request to the appropriate handler.
func (b *bridge) handleToolCall(ctx context.Context, id json.RawMessage, rawParams json.RawMessage) error {
	var params toolCallParams
	if err := json.Unmarshal(rawParams, &params); err != nil {
		return b.sendResponse(id, nil, &rpcError{
			Code:    -32602,
			Message: fmt.Sprintf("invalid tool call params: %v", err),
		})
	}

	switch params.Name {
	case "reply":
		return b.handleReplyTool(ctx, id, params.Arguments)
	case "check_messages":
		return b.handleCheckMessages(id)
	default:
		return b.sendResponse(id, nil, &rpcError{
			Code:    -32602,
			Message: fmt.Sprintf("unknown tool: %s", params.Name),
		})
	}
}

// handleReplyTool processes the "reply" tool call.
func (b *bridge) handleReplyTool(ctx context.Context, id json.RawMessage, rawArgs json.RawMessage) error {
	var args replyArguments
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return b.sendResponse(id, toolCallResult{
			Content: []toolResultContent{{Type: "text", Text: fmt.Sprintf("invalid arguments: %v", err)}},
			IsError: true,
		}, nil)
	}

	if args.Text == "" {
		return b.sendResponse(id, toolCallResult{
			Content: []toolResultContent{{Type: "text", Text: "text is required"}},
			IsError: true,
		}, nil)
	}

	if err := b.postReply(ctx, args.SessionID, args); err != nil {
		b.logger.Error("reply tool failed", "error", err)
		return b.sendResponse(id, toolCallResult{
			Content: []toolResultContent{{Type: "text", Text: fmt.Sprintf("failed to send reply: %v", err)}},
			IsError: true,
		}, nil)
	}

	return b.sendResponse(id, toolCallResult{
		Content: []toolResultContent{{Type: "text", Text: "Reply sent successfully"}},
	}, nil)
}

// handleCheckMessages drains the local message buffer and returns all pending messages.
func (b *bridge) handleCheckMessages(id json.RawMessage) error {
	b.msgBufMu.Lock()
	msgs := b.msgBuf
	b.msgBuf = nil
	b.msgBufMu.Unlock()

	if len(msgs) == 0 {
		return b.sendResponse(id, toolCallResult{
			Content: []toolResultContent{{Type: "text", Text: "No new messages."}},
		}, nil)
	}

	payload, err := json.Marshal(msgs)
	if err != nil {
		return b.sendResponse(id, toolCallResult{
			Content: []toolResultContent{{Type: "text", Text: fmt.Sprintf("marshal error: %v", err)}},
			IsError: true,
		}, nil)
	}

	b.logger.Info("check_messages returning buffered events", "count", len(msgs))
	return b.sendResponse(id, toolCallResult{
		Content: []toolResultContent{{Type: "text", Text: string(payload)}},
	}, nil)
}

// handlePermissionRequest processes permission request notifications from Claude Code.
func (b *bridge) handlePermissionRequest(ctx context.Context, rawParams json.RawMessage) {
	var params permissionRequestParams
	if err := json.Unmarshal(rawParams, &params); err != nil {
		b.logger.Error("failed to parse permission request", "error", err)
		return
	}

	b.logger.Info("forwarding permission request",
		"request_id", params.RequestID,
		"tool_name", params.ToolName,
	)

	if err := b.forwardPermissionRequest(ctx, params); err != nil {
		b.logger.Error("failed to forward permission request", "error", err)
	}
}

// ---------------------------------------------------------------------------
// Main dispatch
// ---------------------------------------------------------------------------

// dispatchRequest routes an inbound JSON-RPC message to the correct handler.
func (b *bridge) dispatchRequest(ctx context.Context, msg jsonRPCRequest) {
	switch msg.Method {

	// --- Requests (have an ID, expect a response) ---
	case "initialize":
		if err := b.handleInitialize(msg.ID); err != nil {
			b.logger.Error("failed to send initialize response", "error", err)
		}

	case "tools/list":
		if err := b.handleToolsList(msg.ID); err != nil {
			b.logger.Error("failed to send tools/list response", "error", err)
		}

	case "tools/call":
		if err := b.handleToolCall(ctx, msg.ID, msg.Params); err != nil {
			b.logger.Error("failed to handle tools/call", "error", err)
		}

	// --- Notifications (no ID, no response) ---
	case "notifications/initialized":
		b.logger.Info("Claude Code initialized the channel")

	case "notifications/claude/channel/permission_request":
		b.handlePermissionRequest(ctx, msg.Params)

	default:
		b.logger.Warn("unhandled method", "method", msg.Method)
		// If it has an ID, send a method-not-found error.
		if msg.ID != nil {
			if err := b.sendResponse(msg.ID, nil, &rpcError{
				Code:    -32601,
				Message: fmt.Sprintf("method not found: %s", msg.Method),
			}); err != nil {
				b.logger.Error("failed to send method-not-found response", "error", err)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Long-running goroutines
// ---------------------------------------------------------------------------

// runStdinReader reads JSON-RPC messages from stdin and dispatches them.
// It returns when stdin reaches EOF or the context is cancelled.
func (b *bridge) runStdinReader(ctx context.Context) {
	scanner := bufio.NewScanner(os.Stdin)
	// Allow up to 1MB per line for large tool call payloads.
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

		var msg jsonRPCRequest
		if err := json.Unmarshal(line, &msg); err != nil {
			b.logger.Warn("malformed JSON-RPC message", "error", err, "raw", string(line))
			continue
		}

		b.dispatchRequest(ctx, msg)
	}

	if err := scanner.Err(); err != nil {
		b.logger.Error("stdin read error", "error", err)
	}
	b.logger.Info("stdin closed, shutting down")
}

// runEventPoller polls HyperAX for events and pushes them to Claude Code.
func (b *bridge) runEventPoller(ctx context.Context) {
	ticker := time.NewTicker(eventPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			events, err := b.pollEvents(ctx)
			if err != nil {
				b.logger.Warn("event poll failed", "error", err)
				continue
			}

			for _, evt := range events {
				if err := b.relayEvent(evt); err != nil {
					b.logger.Error("failed to relay event to Claude Code", "error", err, "event_id", evt.ID)
				}
			}
		}
	}
}

// parseMeta parses the Meta JSON string from the server into a map.
// Returns an empty map if the string is empty or invalid JSON.
func parseMeta(raw string) map[string]any {
	if raw == "" {
		return make(map[string]any)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return make(map[string]any)
	}
	return m
}

// relayEvent sends a single polled event to Claude Code as the appropriate notification type.
// Permission verdict events are sent as notifications/claude/channel/permission;
// all other events are sent as notifications/claude/channel.
func (b *bridge) relayEvent(evt channelEvent) error {
	meta := parseMeta(evt.Meta)

	// Permission verdicts need special routing so Claude Code unblocks the pending request.
	if evt.EventType == "permission_verdict" {
		verdict := permissionVerdict{}
		if reqID, ok := meta["request_id"].(string); ok {
			verdict.RequestID = reqID
		}
		if behavior, ok := meta["behavior"].(string); ok {
			verdict.Behavior = behavior
		}
		if verdict.RequestID == "" || verdict.Behavior == "" {
			return fmt.Errorf("permission_verdict event missing request_id or behavior: %v", meta)
		}
		b.logger.Info("relaying permission verdict", "request_id", verdict.RequestID, "behavior", verdict.Behavior)
		return b.sendNotification("notifications/claude/channel/permission", verdict)
	}

	// Default: general channel notification.
	// Ensure session_id is in meta so the reply tool knows where to respond.
	if _, ok := meta["session_id"]; !ok {
		meta["session_id"] = b.sessionID
	}

	params := channelNotificationParams{
		Content: evt.Content,
		Meta:    meta,
	}

	// Buffer the message locally so Claude Code can retrieve it via check_messages.
	// The notification below may be silently dropped if Claude Code doesn't support
	// the experimental channel capability, so the buffer is the reliable path.
	b.msgBufMu.Lock()
	b.msgBuf = append(b.msgBuf, bufferedMessage{
		ID:         evt.ID,
		Content:    evt.Content,
		Meta:       meta,
		EventType:  evt.EventType,
		ReceivedAt: time.Now(),
	})
	b.msgBufMu.Unlock()

	// Still attempt the push notification for forward compatibility.
	if err := b.sendNotification("notifications/claude/channel", params); err != nil {
		b.logger.Warn("channel notification failed (buffered for check_messages)", "error", err)
	}
	b.logger.Debug("buffered event for Claude Code", "event_id", evt.ID, "type", evt.EventType)
	return nil
}

// runHeartbeat sends periodic heartbeats to HyperAX.
func (b *bridge) runHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := b.sendHeartbeat(ctx); err != nil {
				b.logger.Warn("heartbeat failed", "error", err)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Entrypoint
// ---------------------------------------------------------------------------

func main() {
	hyperaxURL := flag.String("url", "http://localhost:9090", "HyperAX server base URL")
	sessionName := flag.String("name", "claude-code", "Session name for this channel")
	workspaceID := flag.String("workspace", "", "Workspace ID to bind to")
	flag.Parse()

	// Logger writes to stderr — stdout is the MCP transport.
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	logger.Info("hyperax-bridge starting",
		"version", bridgeVersion,
		"hyperax_url", *hyperaxURL,
		"session_name", *sessionName,
		"workspace_id", *workspaceID,
	)

	b := newBridge(*hyperaxURL, *sessionName, *workspaceID, logger)

	// Top-level context: cancelled on SIGINT/SIGTERM or stdin EOF.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Register with HyperAX.
	if err := b.register(ctx); err != nil {
		logger.Error("failed to register with HyperAX", "error", err)
		os.Exit(1)
	}

	// WaitGroup tracks background goroutines so we can wait for clean shutdown.
	var wg sync.WaitGroup

	// Event poller goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		b.runEventPoller(ctx)
	}()

	// Heartbeat goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		b.runHeartbeat(ctx)
	}()

	// Stdin reader runs in the main goroutine's context. When stdin closes
	// (Claude Code exited), we cancel the context to stop all goroutines.
	stdinDone := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		b.runStdinReader(ctx)
		close(stdinDone)
	}()

	// Wait for either signal or stdin EOF.
	select {
	case <-ctx.Done():
		logger.Info("received shutdown signal")
	case <-stdinDone:
		logger.Info("stdin closed, initiating shutdown")
		cancel()
	}

	// Deregister session (best-effort).
	b.deregister()

	// Wait for goroutines to finish.
	wg.Wait()
	logger.Info("hyperax-bridge stopped")
}
