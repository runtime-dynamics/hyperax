package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/repo"
)

// ChannelRepo implements repo.ChannelRepo for SQLite.
type ChannelRepo struct {
	db *sql.DB
}

// ---------------------------------------------------------------------------
// Sessions
// ---------------------------------------------------------------------------

// CreateSession inserts a new channel session record.
// If session.ID is empty a UUID is generated.
func (r *ChannelRepo) CreateSession(ctx context.Context, session *repo.ChannelSession) error {
	if session.ID == "" {
		session.ID = uuid.New().String()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO channel_sessions (id, name, workspace_id, status, bridge_version, metadata, connected_at, last_heartbeat)
		 VALUES (?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))`,
		session.ID, session.Name, session.WorkspaceID, session.Status,
		session.BridgeVersion, session.Metadata,
	)
	if err != nil {
		return fmt.Errorf("sqlite.ChannelRepo.CreateSession: %w", err)
	}
	return nil
}

// GetSession retrieves a single channel session by ID.
// Returns an error when the session does not exist.
func (r *ChannelRepo) GetSession(ctx context.Context, id string) (*repo.ChannelSession, error) {
	s := &repo.ChannelSession{}
	var connectedAt, lastHeartbeat string
	var disconnectedAt sql.NullString

	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, workspace_id, status, bridge_version, metadata,
		        connected_at, last_heartbeat, disconnected_at
		 FROM channel_sessions WHERE id = ?`, id,
	).Scan(
		&s.ID, &s.Name, &s.WorkspaceID, &s.Status,
		&s.BridgeVersion, &s.Metadata,
		&connectedAt, &lastHeartbeat, &disconnectedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("channel session %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite.ChannelRepo.GetSession: %w", err)
	}

	var parseErr error
	if s.ConnectedAt, parseErr = parseSQLiteTime(connectedAt, "sqlite.ChannelRepo.GetSession.connectedAt"); parseErr != nil {
		return nil, parseErr
	}
	if s.LastHeartbeat, parseErr = parseSQLiteTime(lastHeartbeat, "sqlite.ChannelRepo.GetSession.lastHeartbeat"); parseErr != nil {
		return nil, parseErr
	}
	if disconnectedAt.Valid {
		t, dErr := parseSQLiteTime(disconnectedAt.String, "sqlite.ChannelRepo.GetSession.disconnectedAt")
		if dErr != nil {
			return nil, dErr
		}
		s.DisconnectedAt = &t
	}

	return s, nil
}

// ListSessions returns channel sessions, optionally filtered by status.
// Pass an empty statusFilter to return all sessions. Results are ordered
// by connected_at descending (most recent first).
func (r *ChannelRepo) ListSessions(ctx context.Context, statusFilter string) ([]*repo.ChannelSession, error) {
	var (
		sqlStr string
		args   []any
	)

	if statusFilter != "" {
		sqlStr = `SELECT id, name, workspace_id, status, bridge_version, metadata,
		                 connected_at, last_heartbeat, disconnected_at
		          FROM channel_sessions WHERE status = ? ORDER BY connected_at DESC`
		args = []any{statusFilter}
	} else {
		sqlStr = `SELECT id, name, workspace_id, status, bridge_version, metadata,
		                 connected_at, last_heartbeat, disconnected_at
		          FROM channel_sessions ORDER BY connected_at DESC`
	}

	rows, err := r.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite.ChannelRepo.ListSessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var sessions []*repo.ChannelSession
	for rows.Next() {
		s := &repo.ChannelSession{}
		var connectedAt, lastHeartbeat string
		var disconnectedAt sql.NullString

		if err := rows.Scan(
			&s.ID, &s.Name, &s.WorkspaceID, &s.Status,
			&s.BridgeVersion, &s.Metadata,
			&connectedAt, &lastHeartbeat, &disconnectedAt,
		); err != nil {
			return nil, fmt.Errorf("sqlite.ChannelRepo.ListSessions: %w", err)
		}

		var parseErr error
		if s.ConnectedAt, parseErr = parseSQLiteTime(connectedAt, "sqlite.ChannelRepo.ListSessions.connectedAt"); parseErr != nil {
			return nil, parseErr
		}
		if s.LastHeartbeat, parseErr = parseSQLiteTime(lastHeartbeat, "sqlite.ChannelRepo.ListSessions.lastHeartbeat"); parseErr != nil {
			return nil, parseErr
		}
		if disconnectedAt.Valid {
			t, dErr := parseSQLiteTime(disconnectedAt.String, "sqlite.ChannelRepo.ListSessions.disconnectedAt")
			if dErr != nil {
				return nil, dErr
			}
			s.DisconnectedAt = &t
		}

		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.ChannelRepo.ListSessions: %w", err)
	}
	return sessions, nil
}

// UpdateHeartbeat bumps the last_heartbeat timestamp for the given session.
func (r *ChannelRepo) UpdateHeartbeat(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE channel_sessions SET last_heartbeat = datetime('now') WHERE id = ?`, id,
	)
	if err != nil {
		return fmt.Errorf("sqlite.ChannelRepo.UpdateHeartbeat: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.ChannelRepo.UpdateHeartbeat: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("channel session %q not found", id)
	}
	return nil
}

// UpdateSessionStatus sets the status column for a session.
// When setting "disconnected", the disconnected_at timestamp is also recorded.
func (r *ChannelRepo) UpdateSessionStatus(ctx context.Context, id, status string) error {
	var (
		res sql.Result
		err error
	)

	if status == "disconnected" {
		res, err = r.db.ExecContext(ctx,
			`UPDATE channel_sessions SET status = ?, disconnected_at = datetime('now') WHERE id = ?`,
			status, id,
		)
	} else {
		res, err = r.db.ExecContext(ctx,
			`UPDATE channel_sessions SET status = ? WHERE id = ?`,
			status, id,
		)
	}
	if err != nil {
		return fmt.Errorf("sqlite.ChannelRepo.UpdateSessionStatus: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.ChannelRepo.UpdateSessionStatus: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("channel session %q not found", id)
	}
	return nil
}

// DeleteSession removes a channel session by ID.
func (r *ChannelRepo) DeleteSession(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM channel_sessions WHERE id = ?`, id,
	)
	if err != nil {
		return fmt.Errorf("sqlite.ChannelRepo.DeleteSession: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.ChannelRepo.DeleteSession: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("channel session %q not found", id)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Messages
// ---------------------------------------------------------------------------

// CreateMessage inserts a new chat message associated with a channel session.
// If msg.ID is empty a UUID is generated.
func (r *ChannelRepo) CreateMessage(ctx context.Context, msg *repo.ChannelMessage) error {
	if msg.ID == "" {
		msg.ID = uuid.New().String()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO channel_messages (id, session_id, direction, content, sender, metadata, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, datetime('now'))`,
		msg.ID, msg.SessionID, msg.Direction, msg.Content, msg.Sender, msg.Metadata,
	)
	if err != nil {
		return fmt.Errorf("sqlite.ChannelRepo.CreateMessage: %w", err)
	}
	return nil
}

// ListMessages returns the most recent messages for a session, ordered by
// created_at descending. The limit parameter caps the result set size;
// when limit <= 0 all messages are returned.
func (r *ChannelRepo) ListMessages(ctx context.Context, sessionID string, limit int) ([]*repo.ChannelMessage, error) {
	var (
		sqlStr string
		args   []any
	)

	if limit > 0 {
		sqlStr = `SELECT id, session_id, direction, content, sender, metadata, created_at
		          FROM channel_messages WHERE session_id = ?
		          ORDER BY created_at DESC LIMIT ?`
		args = []any{sessionID, limit}
	} else {
		sqlStr = `SELECT id, session_id, direction, content, sender, metadata, created_at
		          FROM channel_messages WHERE session_id = ?
		          ORDER BY created_at DESC`
		args = []any{sessionID}
	}

	rows, err := r.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite.ChannelRepo.ListMessages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var messages []*repo.ChannelMessage
	for rows.Next() {
		m := &repo.ChannelMessage{}
		var createdAt string

		if err := rows.Scan(
			&m.ID, &m.SessionID, &m.Direction, &m.Content,
			&m.Sender, &m.Metadata, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("sqlite.ChannelRepo.ListMessages: %w", err)
		}

		var parseErr error
		if m.CreatedAt, parseErr = parseSQLiteTime(createdAt, "sqlite.ChannelRepo.ListMessages"); parseErr != nil {
			return nil, parseErr
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.ChannelRepo.ListMessages: %w", err)
	}
	return messages, nil
}

// ---------------------------------------------------------------------------
// Event Queue
// ---------------------------------------------------------------------------

// EnqueueEvent inserts a new pending event destined for a Claude Code session.
// If event.ID is empty a UUID is generated. Status defaults to "pending".
func (r *ChannelRepo) EnqueueEvent(ctx context.Context, event *repo.ChannelEvent) error {
	if event.ID == "" {
		event.ID = uuid.New().String()
	}

	status := event.Status
	if status == "" {
		status = "pending"
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO channel_events (id, session_id, event_type, content, meta, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, datetime('now'))`,
		event.ID, event.SessionID, event.EventType, event.Content, event.Meta, status,
	)
	if err != nil {
		return fmt.Errorf("sqlite.ChannelRepo.EnqueueEvent: %w", err)
	}
	return nil
}

// DequeueEvents returns pending events for a session, ordered by created_at
// ascending (oldest first). The caller is responsible for calling
// MarkEventDelivered after successful delivery.
func (r *ChannelRepo) DequeueEvents(ctx context.Context, sessionID string, limit int) ([]*repo.ChannelEvent, error) {
	var (
		sqlStr string
		args   []any
	)

	if limit > 0 {
		sqlStr = `SELECT id, session_id, event_type, content, meta, status, created_at, delivered_at
		          FROM channel_events
		          WHERE session_id = ? AND status = 'pending'
		          ORDER BY created_at ASC LIMIT ?`
		args = []any{sessionID, limit}
	} else {
		sqlStr = `SELECT id, session_id, event_type, content, meta, status, created_at, delivered_at
		          FROM channel_events
		          WHERE session_id = ? AND status = 'pending'
		          ORDER BY created_at ASC`
		args = []any{sessionID}
	}

	rows, err := r.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite.ChannelRepo.DequeueEvents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var events []*repo.ChannelEvent
	for rows.Next() {
		e := &repo.ChannelEvent{}
		var createdAt string
		var deliveredAt sql.NullString

		if err := rows.Scan(
			&e.ID, &e.SessionID, &e.EventType, &e.Content,
			&e.Meta, &e.Status, &createdAt, &deliveredAt,
		); err != nil {
			return nil, fmt.Errorf("sqlite.ChannelRepo.DequeueEvents: %w", err)
		}

		var parseErr error
		if e.CreatedAt, parseErr = parseSQLiteTime(createdAt, "sqlite.ChannelRepo.DequeueEvents"); parseErr != nil {
			return nil, parseErr
		}
		if deliveredAt.Valid {
			t, dErr := parseSQLiteTime(deliveredAt.String, "sqlite.ChannelRepo.DequeueEvents.deliveredAt")
			if dErr != nil {
				return nil, dErr
			}
			e.DeliveredAt = &t
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.ChannelRepo.DequeueEvents: %w", err)
	}
	return events, nil
}

// MarkEventDelivered transitions an event to "delivered" and records the
// delivery timestamp.
func (r *ChannelRepo) MarkEventDelivered(ctx context.Context, eventID string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE channel_events SET status = 'delivered', delivered_at = datetime('now') WHERE id = ?`,
		eventID,
	)
	if err != nil {
		return fmt.Errorf("sqlite.ChannelRepo.MarkEventDelivered: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.ChannelRepo.MarkEventDelivered: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("channel event %q not found", eventID)
	}
	return nil
}

// ExpireOldEvents transitions all pending events older than the given cutoff
// to "expired" status. Returns the number of affected rows.
func (r *ChannelRepo) ExpireOldEvents(ctx context.Context, olderThan time.Time) (int64, error) {
	cutoff := olderThan.UTC().Format(sqliteTimestamp)

	res, err := r.db.ExecContext(ctx,
		`UPDATE channel_events SET status = 'expired'
		 WHERE status = 'pending' AND created_at < ?`, cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("sqlite.ChannelRepo.ExpireOldEvents: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("sqlite.ChannelRepo.ExpireOldEvents: %w", err)
	}
	return affected, nil
}

// ---------------------------------------------------------------------------
// Permissions
// ---------------------------------------------------------------------------

// CreatePermission inserts a new pending tool-use permission request.
// If perm.ID is empty a UUID is generated.
func (r *ChannelRepo) CreatePermission(ctx context.Context, perm *repo.ChannelPermission) error {
	if perm.ID == "" {
		perm.ID = uuid.New().String()
	}

	status := perm.Status
	if status == "" {
		status = "pending"
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO channel_permissions (id, session_id, request_id, tool_name, description, input_preview, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		perm.ID, perm.SessionID, perm.RequestID, perm.ToolName,
		perm.Description, perm.InputPreview, status,
	)
	if err != nil {
		return fmt.Errorf("sqlite.ChannelRepo.CreatePermission: %w", err)
	}
	return nil
}

// GetPendingPermissions returns all pending permission requests for a session,
// ordered by created_at ascending (oldest first).
func (r *ChannelRepo) GetPendingPermissions(ctx context.Context, sessionID string) ([]*repo.ChannelPermission, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, session_id, request_id, tool_name, description, input_preview,
		        status, created_at, resolved_at, resolved_by
		 FROM channel_permissions
		 WHERE session_id = ? AND status = 'pending'
		 ORDER BY created_at ASC`, sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.ChannelRepo.GetPendingPermissions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var perms []*repo.ChannelPermission
	for rows.Next() {
		p := &repo.ChannelPermission{}
		var createdAt string
		var resolvedAt sql.NullString
		var resolvedBy sql.NullString

		if err := rows.Scan(
			&p.ID, &p.SessionID, &p.RequestID, &p.ToolName,
			&p.Description, &p.InputPreview, &p.Status,
			&createdAt, &resolvedAt, &resolvedBy,
		); err != nil {
			return nil, fmt.Errorf("sqlite.ChannelRepo.GetPendingPermissions: %w", err)
		}

		var parseErr error
		if p.CreatedAt, parseErr = parseSQLiteTime(createdAt, "sqlite.ChannelRepo.GetPendingPermissions"); parseErr != nil {
			return nil, parseErr
		}
		if resolvedAt.Valid {
			t, rErr := parseSQLiteTime(resolvedAt.String, "sqlite.ChannelRepo.GetPendingPermissions.resolvedAt")
			if rErr != nil {
				return nil, rErr
			}
			p.ResolvedAt = &t
		}
		p.ResolvedBy = resolvedBy.String

		perms = append(perms, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.ChannelRepo.GetPendingPermissions: %w", err)
	}
	return perms, nil
}

// ResolvePermission updates a permission request identified by its request_id
// with the given behavior (allowed/denied) and resolved_by user identifier.
func (r *ChannelRepo) ResolvePermission(ctx context.Context, requestID, behavior, resolvedBy string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE channel_permissions
		 SET status = ?, resolved_at = datetime('now'), resolved_by = ?
		 WHERE request_id = ? AND status = 'pending'`,
		behavior, resolvedBy, requestID,
	)
	if err != nil {
		return fmt.Errorf("sqlite.ChannelRepo.ResolvePermission: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.ChannelRepo.ResolvePermission: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("pending permission with request_id %q not found", requestID)
	}
	return nil
}
