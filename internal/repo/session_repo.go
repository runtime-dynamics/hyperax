package repo

import (
	"context"

	"github.com/hyperax/hyperax/pkg/types"
)

// SessionRepo manages chat session lifecycle.
type SessionRepo interface {
	// CreateSession starts a new session between an agent and peer, returning the session ID.
	CreateSession(ctx context.Context, agentName, peerID string) (string, error)

	// GetActiveSession returns the most recent session that has not been ended.
	// Returns (nil, nil) when no active session exists.
	GetActiveSession(ctx context.Context, agentName, peerID string) (*types.ChatSession, error)

	// EndSession marks a session as ended by setting its ended_at timestamp.
	EndSession(ctx context.Context, sessionID string) error

	// SetSessionSummary updates the summary field for a session.
	SetSessionSummary(ctx context.Context, sessionID, summary string) error

	// ArchiveSession marks a session as archived and ends it if still active.
	ArchiveSession(ctx context.Context, sessionID string) error

	// ListSessions returns all non-archived sessions between an agent and peer, ordered by most recent first.
	ListSessions(ctx context.Context, agentName, peerID string) ([]*types.ChatSession, error)

	// RenameAgent cascades an agent name change to all chat_sessions entries.
	RenameAgent(ctx context.Context, oldName, newName string) error
}
