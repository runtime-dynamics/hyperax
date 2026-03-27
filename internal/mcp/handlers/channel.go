package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/internal/storage"
	"github.com/hyperax/hyperax/pkg/types"
)

// actionClearanceChannel maps each channel action to its minimum ABAC clearance.
var actionClearanceChannel = map[string]int{
	"list_sessions":     0, // view connected sessions
	"get_session":       0, // get session details
	"send_message":      1, // push a message to a session
	"get_history":       0, // get chat history
	"dispatch_task":     1, // manually dispatch a task
	"list_permissions":  0, // view pending permission requests
	"resolve_permission": 1, // approve/deny a permission request
	"disconnect":        2, // force disconnect a session
}

// ChannelHandler implements the consolidated "channel" MCP tool for managing
// Claude Code sessions connected via the hyperax-bridge.
type ChannelHandler struct {
	store  *storage.Store
	bus    *nervous.EventBus
	logger *slog.Logger
}

// NewChannelHandler creates a ChannelHandler with all required dependencies.
func NewChannelHandler(store *storage.Store, bus *nervous.EventBus, logger *slog.Logger) *ChannelHandler {
	return &ChannelHandler{
		store:  store,
		bus:    bus,
		logger: logger,
	}
}

// RegisterTools registers the consolidated "channel" tool with the MCP registry.
func (h *ChannelHandler) RegisterTools(registry *mcp.ToolRegistry) {
	registry.Register(
		"channel",
		"Channel session management: view Claude Code sessions, send messages, dispatch tasks, "+
			"manage permission requests, and disconnect sessions. "+
			"Actions: list_sessions | get_session | send_message | get_history | dispatch_task | "+
			"list_permissions | resolve_permission | disconnect",
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"action":     {"type": "string", "enum": ["list_sessions", "get_session", "send_message", "get_history", "dispatch_task", "list_permissions", "resolve_permission", "disconnect"], "description": "Action to perform"},
				"session_id": {"type": "string", "description": "Channel session ID"},
				"content":    {"type": "string", "description": "Message content (send_message)"},
				"task_id":    {"type": "string", "description": "Task to dispatch (dispatch_task)"},
				"request_id": {"type": "string", "description": "Permission request ID (resolve_permission)"},
				"behavior":   {"type": "string", "enum": ["allow", "deny"], "description": "Permission verdict (resolve_permission)"},
				"limit":      {"type": "integer", "description": "Message history limit (get_history, default 50)"}
			},
			"required": ["action"]
		}`),
		h.dispatch,
	)
}

// dispatch routes the consolidated "channel" tool to the correct handler method.
func (h *ChannelHandler) dispatch(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var envelope struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return nil, fmt.Errorf("handlers.ChannelHandler.dispatch: %w", err)
	}

	if err := checkActionClearance(ctx, envelope.Action, actionClearanceChannel); err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	switch envelope.Action {
	case "list_sessions":
		return h.listSessions(ctx, params)
	case "get_session":
		return h.getSession(ctx, params)
	case "send_message":
		return h.sendMessage(ctx, params)
	case "get_history":
		return h.getHistory(ctx, params)
	case "dispatch_task":
		return h.dispatchTask(ctx, params)
	case "list_permissions":
		return h.listPermissions(ctx, params)
	case "resolve_permission":
		return h.resolvePermission(ctx, params)
	case "disconnect":
		return h.disconnect(ctx, params)
	default:
		return types.NewErrorResult(fmt.Sprintf(
			"unknown channel action %q: valid actions are list_sessions, get_session, send_message, get_history, dispatch_task, list_permissions, resolve_permission, disconnect",
			envelope.Action,
		)), nil
	}
}

// listSessions returns all channel sessions, optionally filtered by status.
func (h *ChannelHandler) listSessions(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		StatusFilter string `json:"status_filter"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ChannelHandler.listSessions: %w", err)
	}

	if h.store.Channels == nil {
		return types.NewErrorResult("channel system not available"), nil
	}

	sessions, err := h.store.Channels.ListSessions(ctx, args.StatusFilter)
	if err != nil {
		return nil, fmt.Errorf("handlers.ChannelHandler.listSessions: %w", err)
	}

	return types.NewToolResult(map[string]any{
		"sessions": sessions,
		"count":    len(sessions),
	}), nil
}

// getSession returns details for a single channel session.
func (h *ChannelHandler) getSession(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ChannelHandler.getSession: %w", err)
	}
	if args.SessionID == "" {
		return types.NewErrorResult("session_id is required"), nil
	}

	if h.store.Channels == nil {
		return types.NewErrorResult("channel system not available"), nil
	}

	session, err := h.store.Channels.GetSession(ctx, args.SessionID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("session not found: %v", err)), nil
	}

	return types.NewToolResult(session), nil
}

// sendMessage creates an outbound message and enqueues a channel event for delivery.
func (h *ChannelHandler) sendMessage(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		SessionID string `json:"session_id"`
		Content   string `json:"content"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ChannelHandler.sendMessage: %w", err)
	}
	if args.SessionID == "" {
		return types.NewErrorResult("session_id is required"), nil
	}
	if args.Content == "" {
		return types.NewErrorResult("content is required"), nil
	}

	if h.store.Channels == nil {
		return types.NewErrorResult("channel system not available"), nil
	}

	// Resolve caller identity from ABAC context.
	auth := mcp.AuthFromContext(ctx)
	sender := auth.PersonaID
	if sender == "" {
		sender = "dashboard"
	}

	msgID := channelGenerateID()
	msg := &repo.ChannelMessage{
		ID:        msgID,
		SessionID: args.SessionID,
		Direction: "outbound",
		Content:   args.Content,
		Sender:    sender,
		CreatedAt: time.Now(),
	}
	if err := h.store.Channels.CreateMessage(ctx, msg); err != nil {
		return nil, fmt.Errorf("handlers.ChannelHandler.sendMessage: create message: %w", err)
	}

	// Enqueue a channel event so the bridge can poll and deliver it.
	eventID := channelGenerateID()
	event := &repo.ChannelEvent{
		ID:        eventID,
		SessionID: args.SessionID,
		EventType: "message",
		Content:   args.Content,
		Status:    "pending",
		CreatedAt: time.Now(),
	}
	if err := h.store.Channels.EnqueueEvent(ctx, event); err != nil {
		return nil, fmt.Errorf("handlers.ChannelHandler.sendMessage: enqueue event: %w", err)
	}

	// Publish event for real-time dashboard updates.
	if h.bus != nil {
		h.bus.Publish(nervous.NewEvent(
			types.EventChannelEventPushed,
			"channel_handler",
			"global",
			map[string]string{
				"session_id": args.SessionID,
				"message_id": msgID,
				"event_id":   eventID,
			},
		))
	}

	h.logger.Info("channel message sent",
		"session_id", args.SessionID,
		"message_id", msgID,
		"event_id", eventID,
	)

	return types.NewToolResult(map[string]any{
		"message_id": msgID,
		"event_id":   eventID,
		"status":     "enqueued",
	}), nil
}

// getHistory returns chat messages for a session, most recent first.
func (h *ChannelHandler) getHistory(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		SessionID string `json:"session_id"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ChannelHandler.getHistory: %w", err)
	}
	if args.SessionID == "" {
		return types.NewErrorResult("session_id is required"), nil
	}
	if args.Limit <= 0 {
		args.Limit = 50
	}

	if h.store.Channels == nil {
		return types.NewErrorResult("channel system not available"), nil
	}

	messages, err := h.store.Channels.ListMessages(ctx, args.SessionID, args.Limit)
	if err != nil {
		return nil, fmt.Errorf("handlers.ChannelHandler.getHistory: %w", err)
	}

	return types.NewToolResult(map[string]any{
		"messages": messages,
		"count":    len(messages),
	}), nil
}

// dispatchTask looks up a task, formats it as a channel event, enqueues it,
// and updates the task status to "in_progress".
func (h *ChannelHandler) dispatchTask(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		SessionID string `json:"session_id"`
		TaskID    string `json:"task_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ChannelHandler.dispatchTask: %w", err)
	}
	if args.SessionID == "" {
		return types.NewErrorResult("session_id is required"), nil
	}
	if args.TaskID == "" {
		return types.NewErrorResult("task_id is required"), nil
	}

	if h.store.Channels == nil {
		return types.NewErrorResult("channel system not available"), nil
	}
	if h.store.Projects == nil {
		return types.NewErrorResult("project system not available"), nil
	}

	// Look up the task from the project store.
	task, err := h.store.Projects.GetTask(ctx, args.TaskID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("task not found: %v", err)), nil
	}

	// Format the task as event content.
	taskPayload, err := json.Marshal(map[string]any{
		"task_id":     task.ID,
		"name":        task.Name,
		"description": task.Description,
		"priority":    task.Priority,
		"status":      task.Status,
	})
	if err != nil {
		return nil, fmt.Errorf("handlers.ChannelHandler.dispatchTask: marshal task: %w", err)
	}

	// Enqueue a task_dispatch event for the bridge.
	eventID := channelGenerateID()
	event := &repo.ChannelEvent{
		ID:        eventID,
		SessionID: args.SessionID,
		EventType: "task_dispatch",
		Content:   string(taskPayload),
		Status:    "pending",
		CreatedAt: time.Now(),
	}
	if err := h.store.Channels.EnqueueEvent(ctx, event); err != nil {
		return nil, fmt.Errorf("handlers.ChannelHandler.dispatchTask: enqueue event: %w", err)
	}

	// Update the task status to in_progress.
	if err := h.store.Projects.UpdateTaskStatus(ctx, args.TaskID, "in_progress"); err != nil {
		h.logger.Error("failed to update task status after dispatch",
			"task_id", args.TaskID,
			"error", err,
		)
		// Non-fatal: the event was enqueued, so we report success with a warning.
	}

	// Publish events for real-time dashboard updates.
	if h.bus != nil {
		h.bus.Publish(nervous.NewEvent(
			types.EventChannelTaskDispatched,
			"channel_handler",
			"global",
			map[string]string{
				"session_id": args.SessionID,
				"task_id":    args.TaskID,
				"event_id":   eventID,
			},
		))
	}

	h.logger.Info("task dispatched to channel session",
		"session_id", args.SessionID,
		"task_id", args.TaskID,
		"event_id", eventID,
	)

	return types.NewToolResult(map[string]any{
		"event_id": eventID,
		"task_id":  args.TaskID,
		"status":   "dispatched",
	}), nil
}

// listPermissions returns pending permission requests for a session.
func (h *ChannelHandler) listPermissions(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ChannelHandler.listPermissions: %w", err)
	}
	if args.SessionID == "" {
		return types.NewErrorResult("session_id is required"), nil
	}

	if h.store.Channels == nil {
		return types.NewErrorResult("channel system not available"), nil
	}

	perms, err := h.store.Channels.GetPendingPermissions(ctx, args.SessionID)
	if err != nil {
		return nil, fmt.Errorf("handlers.ChannelHandler.listPermissions: %w", err)
	}

	return types.NewToolResult(map[string]any{
		"permissions": perms,
		"count":       len(perms),
	}), nil
}

// resolvePermission approves or denies a pending permission request.
func (h *ChannelHandler) resolvePermission(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		RequestID string `json:"request_id"`
		Behavior  string `json:"behavior"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ChannelHandler.resolvePermission: %w", err)
	}
	if args.RequestID == "" {
		return types.NewErrorResult("request_id is required"), nil
	}
	if args.Behavior != "allow" && args.Behavior != "deny" {
		return types.NewErrorResult("behavior must be 'allow' or 'deny'"), nil
	}

	if h.store.Channels == nil {
		return types.NewErrorResult("channel system not available"), nil
	}

	// Resolve caller identity for audit trail.
	auth := mcp.AuthFromContext(ctx)
	resolvedBy := auth.PersonaID
	if resolvedBy == "" {
		resolvedBy = "dashboard"
	}

	if err := h.store.Channels.ResolvePermission(ctx, args.RequestID, args.Behavior, resolvedBy); err != nil {
		return types.NewErrorResult(fmt.Sprintf("resolve permission: %v", err)), nil
	}

	// Publish event for real-time dashboard updates.
	if h.bus != nil {
		h.bus.Publish(nervous.NewEvent(
			types.EventChannelPermissionResolved,
			"channel_handler",
			"global",
			map[string]string{
				"request_id":  args.RequestID,
				"behavior":    args.Behavior,
				"resolved_by": resolvedBy,
			},
		))
	}

	h.logger.Info("channel permission resolved",
		"request_id", args.RequestID,
		"behavior", args.Behavior,
		"resolved_by", resolvedBy,
	)

	return types.NewToolResult(map[string]any{
		"request_id": args.RequestID,
		"behavior":   args.Behavior,
		"status":     "resolved",
	}), nil
}

// disconnect force-disconnects a channel session.
func (h *ChannelHandler) disconnect(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ChannelHandler.disconnect: %w", err)
	}
	if args.SessionID == "" {
		return types.NewErrorResult("session_id is required"), nil
	}

	if h.store.Channels == nil {
		return types.NewErrorResult("channel system not available"), nil
	}

	if err := h.store.Channels.UpdateSessionStatus(ctx, args.SessionID, "disconnected"); err != nil {
		return types.NewErrorResult(fmt.Sprintf("disconnect session: %v", err)), nil
	}

	// Publish event for real-time dashboard updates.
	if h.bus != nil {
		h.bus.Publish(nervous.NewEvent(
			types.EventChannelSessionDisconnected,
			"channel_handler",
			"global",
			map[string]string{
				"session_id": args.SessionID,
				"reason":     "force_disconnect",
			},
		))
	}

	h.logger.Info("channel session force-disconnected",
		"session_id", args.SessionID,
	)

	return types.NewToolResult(map[string]any{
		"session_id": args.SessionID,
		"status":     "disconnected",
	}), nil
}

// channelGenerateID creates a random 16-byte hex ID for channel entities.
func channelGenerateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}
