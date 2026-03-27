package repo

import (
	"context"
	"time"
)

// ChannelSession represents a connected Claude Code instance via the hyperax-bridge.
type ChannelSession struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	WorkspaceID    string    `json:"workspace_id"`
	Status         string    `json:"status"` // connected, disconnected, idle
	BridgeVersion  string    `json:"bridge_version"`
	Metadata       string    `json:"metadata"`
	ConnectedAt    time.Time `json:"connected_at"`
	LastHeartbeat  time.Time `json:"last_heartbeat"`
	DisconnectedAt *time.Time `json:"disconnected_at,omitempty"`
}

// ChannelMessage represents a chat message between the dashboard and a Claude Code session.
type ChannelMessage struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Direction string    `json:"direction"` // inbound (from user), outbound (to claude), system
	Content   string    `json:"content"`
	Sender    string    `json:"sender"`
	Metadata  string    `json:"metadata"`
	CreatedAt time.Time `json:"created_at"`
}

// ChannelEvent represents a pending event to push to Claude Code via the bridge.
type ChannelEvent struct {
	ID          string     `json:"id"`
	SessionID   string     `json:"session_id"`
	EventType   string     `json:"event_type"` // message, task_dispatch, notification
	Content     string     `json:"content"`
	Meta        string     `json:"meta"` // JSON: becomes <channel> tag attributes
	Status      string     `json:"status"` // pending, delivered, expired
	CreatedAt   time.Time  `json:"created_at"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
}

// ChannelPermission represents a pending tool-use approval request from Claude Code.
type ChannelPermission struct {
	ID           string     `json:"id"`
	SessionID    string     `json:"session_id"`
	RequestID    string     `json:"request_id"` // 5-char code from Claude Code
	ToolName     string     `json:"tool_name"`
	Description  string     `json:"description"`
	InputPreview string     `json:"input_preview"`
	Status       string     `json:"status"` // pending, allowed, denied, expired
	CreatedAt    time.Time  `json:"created_at"`
	ResolvedAt   *time.Time `json:"resolved_at,omitempty"`
	ResolvedBy   string     `json:"resolved_by,omitempty"`
}

// ChannelRepo manages Claude Code channel session lifecycle, messaging, and events.
type ChannelRepo interface {
	// Sessions
	CreateSession(ctx context.Context, session *ChannelSession) error
	GetSession(ctx context.Context, id string) (*ChannelSession, error)
	ListSessions(ctx context.Context, statusFilter string) ([]*ChannelSession, error)
	UpdateHeartbeat(ctx context.Context, id string) error
	UpdateSessionStatus(ctx context.Context, id, status string) error
	DeleteSession(ctx context.Context, id string) error

	// Messages (chat history)
	CreateMessage(ctx context.Context, msg *ChannelMessage) error
	ListMessages(ctx context.Context, sessionID string, limit int) ([]*ChannelMessage, error)

	// Event queue (outbound to Claude Code)
	EnqueueEvent(ctx context.Context, event *ChannelEvent) error
	DequeueEvents(ctx context.Context, sessionID string, limit int) ([]*ChannelEvent, error)
	MarkEventDelivered(ctx context.Context, eventID string) error
	ExpireOldEvents(ctx context.Context, olderThan time.Time) (int64, error)

	// Permission relay
	CreatePermission(ctx context.Context, perm *ChannelPermission) error
	GetPendingPermissions(ctx context.Context, sessionID string) ([]*ChannelPermission, error)
	ResolvePermission(ctx context.Context, requestID, behavior, resolvedBy string) error
}
