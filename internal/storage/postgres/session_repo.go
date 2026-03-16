package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/pkg/types"
)

// SessionRepo implements repo.SessionRepo for PostgreSQL.
type SessionRepo struct {
	db *sql.DB
}

// CreateSession starts a new session between an agent and peer, returning the session ID.
func (r *SessionRepo) CreateSession(ctx context.Context, agentName, peerID string) (string, error) {
	id := uuid.New().String()

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO chat_sessions (id, agent_name, peer_id) VALUES ($1, $2, $3)`,
		id, agentName, peerID,
	)
	if err != nil {
		return "", fmt.Errorf("postgres.SessionRepo.CreateSession: %w", err)
	}

	return id, nil
}

// GetActiveSession returns the most recent session that has not been ended.
// Returns (nil, nil) when no active session exists.
func (r *SessionRepo) GetActiveSession(ctx context.Context, agentName, peerID string) (*types.ChatSession, error) {
	s := &types.ChatSession{}

	err := r.db.QueryRowContext(ctx,
		`SELECT id, agent_name, peer_id, started_at, summary
		 FROM chat_sessions
		 WHERE agent_name = $1 AND peer_id = $2 AND ended_at IS NULL AND archived_at IS NULL
		 ORDER BY started_at DESC
		 LIMIT 1`,
		agentName, peerID,
	).Scan(&s.ID, &s.AgentName, &s.PeerID, &s.StartedAt, &s.Summary)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("postgres.SessionRepo.GetActiveSession: %w", err)
	}

	return s, nil
}

// EndSession marks a session as ended by setting its ended_at timestamp.
func (r *SessionRepo) EndSession(ctx context.Context, sessionID string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE chat_sessions SET ended_at = NOW() WHERE id = $1`,
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("postgres.SessionRepo.EndSession: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.SessionRepo.EndSession: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("session %q not found", sessionID)
	}
	return nil
}

// SetSessionSummary updates the summary field for a session.
func (r *SessionRepo) SetSessionSummary(ctx context.Context, sessionID, summary string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE chat_sessions SET summary = $1 WHERE id = $2`,
		summary, sessionID,
	)
	if err != nil {
		return fmt.Errorf("postgres.SessionRepo.SetSessionSummary: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.SessionRepo.SetSessionSummary: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("session %q not found", sessionID)
	}
	return nil
}

// ArchiveSession marks a session as archived and ends it if still active.
func (r *SessionRepo) ArchiveSession(ctx context.Context, sessionID string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE chat_sessions
		 SET archived_at = NOW(),
		     ended_at = COALESCE(ended_at, NOW())
		 WHERE id = $1`,
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("postgres.SessionRepo.ArchiveSession: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.SessionRepo.ArchiveSession: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("session %q not found", sessionID)
	}
	return nil
}

// RenameAgent cascades an agent name change to all chat_sessions entries.
func (r *SessionRepo) RenameAgent(ctx context.Context, oldName, newName string) error {
	if _, err := r.db.ExecContext(ctx,
		`UPDATE chat_sessions SET agent_name = $1 WHERE agent_name = $2`,
		newName, oldName,
	); err != nil {
		return fmt.Errorf("postgres.SessionRepo.RenameAgent: %w", err)
	}
	return nil
}

// ListSessions returns all non-archived sessions between an agent and peer, ordered by most recent first.
func (r *SessionRepo) ListSessions(ctx context.Context, agentName, peerID string) ([]*types.ChatSession, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, agent_name, peer_id, started_at, ended_at, archived_at, summary
		 FROM chat_sessions
		 WHERE agent_name = $1 AND peer_id = $2 AND archived_at IS NULL
		 ORDER BY started_at DESC`,
		agentName, peerID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.SessionRepo.ListSessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var sessions []*types.ChatSession
	for rows.Next() {
		s := &types.ChatSession{}
		if err := rows.Scan(&s.ID, &s.AgentName, &s.PeerID, &s.StartedAt, &s.EndedAt, &s.ArchivedAt, &s.Summary); err != nil {
			return nil, fmt.Errorf("postgres.SessionRepo.ListSessions: %w", err)
		}
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.SessionRepo.ListSessions: %w", err)
	}
	return sessions, nil
}
