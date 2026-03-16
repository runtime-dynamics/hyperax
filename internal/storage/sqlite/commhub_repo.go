package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/pkg/types"
)

// CommHubRepo implements repo.CommHubRepo for SQLite.
type CommHubRepo struct {
	db *sql.DB
}

// --- Hierarchy ---

// SetRelationship creates or replaces an agent relationship.
// Generates a UUID if the relationship ID is empty.
func (r *CommHubRepo) SetRelationship(ctx context.Context, rel *types.AgentRelationship) error {
	if rel.ID == "" {
		rel.ID = uuid.New().String()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO agent_relationships (id, parent_agent, child_agent, relationship)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		    parent_agent = excluded.parent_agent,
		    child_agent  = excluded.child_agent,
		    relationship = excluded.relationship`,
		rel.ID, rel.ParentAgent, rel.ChildAgent, rel.Relationship,
	)
	if err != nil {
		return fmt.Errorf("sqlite.CommHubRepo.SetRelationship: %w", err)
	}

	return nil
}

// GetRelationship retrieves a relationship by parent and child agent IDs.
func (r *CommHubRepo) GetRelationship(ctx context.Context, parentAgent, childAgent string) (*types.AgentRelationship, error) {
	rel := &types.AgentRelationship{}
	var createdAt string

	err := r.db.QueryRowContext(ctx,
		`SELECT id, parent_agent, child_agent, relationship, created_at
		 FROM agent_relationships
		 WHERE parent_agent = ? AND child_agent = ?`,
		parentAgent, childAgent,
	).Scan(&rel.ID, &rel.ParentAgent, &rel.ChildAgent, &rel.Relationship, &createdAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("repo.CommHubRepo.GetRelationship: not found: %s -> %s", parentAgent, childAgent)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite.CommHubRepo.GetRelationship: %w", err)
	}

	rel.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	return rel, nil
}

// GetChildren returns all child relationships for a parent agent.
func (r *CommHubRepo) GetChildren(ctx context.Context, parentAgent string) ([]*types.AgentRelationship, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, parent_agent, child_agent, relationship, created_at
		 FROM agent_relationships
		 WHERE parent_agent = ?
		 ORDER BY child_agent`,
		parentAgent,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.CommHubRepo.GetChildren: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanRelationships(rows)
}

// GetParent returns the parent relationship for a child agent.
func (r *CommHubRepo) GetParent(ctx context.Context, childAgent string) (*types.AgentRelationship, error) {
	rel := &types.AgentRelationship{}
	var createdAt string

	err := r.db.QueryRowContext(ctx,
		`SELECT id, parent_agent, child_agent, relationship, created_at
		 FROM agent_relationships
		 WHERE child_agent = ?
		 LIMIT 1`,
		childAgent,
	).Scan(&rel.ID, &rel.ParentAgent, &rel.ChildAgent, &rel.Relationship, &createdAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("repo.CommHubRepo.GetParent: not found for agent %q", childAgent)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite.CommHubRepo.GetParent: %w", err)
	}

	rel.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	return rel, nil
}

// GetFullHierarchy returns all agent relationships in the system.
func (r *CommHubRepo) GetFullHierarchy(ctx context.Context) ([]*types.AgentRelationship, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, parent_agent, child_agent, relationship, created_at
		 FROM agent_relationships
		 ORDER BY parent_agent, child_agent`,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.CommHubRepo.GetFullHierarchy: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanRelationships(rows)
}

// DeleteRelationship removes a relationship by its ID.
func (r *CommHubRepo) DeleteRelationship(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM agent_relationships WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("sqlite.CommHubRepo.DeleteRelationship: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.CommHubRepo.DeleteRelationship: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("relationship %q not found", id)
	}

	return nil
}

// scanRelationships scans a sql.Rows result set into a slice of AgentRelationship.
func scanRelationships(rows *sql.Rows) ([]*types.AgentRelationship, error) {
	var rels []*types.AgentRelationship
	for rows.Next() {
		rel := &types.AgentRelationship{}
		var createdAt string
		if err := rows.Scan(&rel.ID, &rel.ParentAgent, &rel.ChildAgent, &rel.Relationship, &createdAt); err != nil {
			return nil, fmt.Errorf("sqlite.scanRelationships: %w", err)
		}
		rel.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		rels = append(rels, rel)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.scanRelationships: %w", err)
	}
	return rels, nil
}

// --- Comm Log ---

// LogMessage persists a communication log entry with a generated UUID.
func (r *CommHubRepo) LogMessage(ctx context.Context, entry *types.CommLogEntry) error {
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO comm_log (id, from_agent, to_agent, content_type, content, trust, direction)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.FromAgent, entry.ToAgent, entry.ContentType, entry.Content, entry.Trust, entry.Direction,
	)
	if err != nil {
		return fmt.Errorf("sqlite.CommHubRepo.LogMessage: %w", err)
	}

	return nil
}

// GetCommLog retrieves communication log entries for a specific agent,
// either as sender or receiver, ordered by most recent first.
func (r *CommHubRepo) GetCommLog(ctx context.Context, agentID string, limit int) ([]*types.CommLogEntry, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, from_agent, to_agent, content_type, content, trust, direction, created_at
		 FROM comm_log
		 WHERE from_agent = ? OR to_agent = ?
		 ORDER BY created_at DESC
		 LIMIT ?`,
		agentID, agentID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.CommHubRepo.GetCommLog: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanCommLogEntries(rows)
}

// GetCommLogBetween retrieves log entries between two specific agents,
// ordered by most recent first.
func (r *CommHubRepo) GetCommLogBetween(ctx context.Context, from, to string, limit int) ([]*types.CommLogEntry, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, from_agent, to_agent, content_type, content, trust, direction, created_at
		 FROM comm_log
		 WHERE (from_agent = ? AND to_agent = ?) OR (from_agent = ? AND to_agent = ?)
		 ORDER BY created_at DESC
		 LIMIT ?`,
		from, to, to, from, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.CommHubRepo.GetCommLogBetween: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanCommLogEntries(rows)
}

// scanCommLogEntries scans a sql.Rows result set into a slice of CommLogEntry.
// Supports both 8-column (legacy) and 9-column (with session_id) result sets.
func scanCommLogEntries(rows *sql.Rows) ([]*types.CommLogEntry, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("sqlite.scanCommLogEntries: %w", err)
	}
	hasSessionID := len(cols) >= 9

	var entries []*types.CommLogEntry
	for rows.Next() {
		e := &types.CommLogEntry{}
		var createdAt string
		if hasSessionID {
			var sessionID sql.NullString
			if err := rows.Scan(&e.ID, &e.FromAgent, &e.ToAgent, &e.ContentType, &e.Content, &e.Trust, &e.Direction, &sessionID, &createdAt); err != nil {
				return nil, fmt.Errorf("sqlite.scanCommLogEntries: %w", err)
			}
			if sessionID.Valid {
				e.SessionID = sessionID.String
			}
		} else {
			if err := rows.Scan(&e.ID, &e.FromAgent, &e.ToAgent, &e.ContentType, &e.Content, &e.Trust, &e.Direction, &createdAt); err != nil {
				return nil, fmt.Errorf("sqlite.scanCommLogEntries: %w", err)
			}
		}
		e.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.scanCommLogEntries: %w", err)
	}
	return entries, nil
}

// LogMessageWithSession persists a communication log entry including its session_id.
func (r *CommHubRepo) LogMessageWithSession(ctx context.Context, entry *types.CommLogEntry) error {
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO comm_log (id, from_agent, to_agent, content_type, content, trust, direction, session_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.FromAgent, entry.ToAgent, entry.ContentType, entry.Content, entry.Trust, entry.Direction, entry.SessionID,
	)
	if err != nil {
		return fmt.Errorf("sqlite.CommHubRepo.LogMessageWithSession: %w", err)
	}
	return nil
}

// GetCommLogBySession retrieves log entries for a specific session,
// ordered by created_at DESC, limited to the given count.
func (r *CommHubRepo) GetCommLogBySession(ctx context.Context, sessionID string, limit int) ([]*types.CommLogEntry, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, from_agent, to_agent, content_type, content, trust, direction, session_id, created_at
		 FROM comm_log
		 WHERE session_id = ?
		 ORDER BY created_at DESC
		 LIMIT ?`,
		sessionID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.CommHubRepo.GetCommLogBySession: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanCommLogEntries(rows)
}

// --- Permissions ---

// GrantPermission creates or replaces a communication permission.
// Uses UPSERT semantics on the (agent_id, target_id) unique constraint.
func (r *CommHubRepo) GrantPermission(ctx context.Context, perm *types.CommPermission) error {
	if perm.ID == "" {
		perm.ID = uuid.New().String()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO comm_permissions (id, agent_id, target_id, permission)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(agent_id, target_id) DO UPDATE SET
		    permission = excluded.permission`,
		perm.ID, perm.AgentID, perm.TargetID, perm.Permission,
	)
	if err != nil {
		return fmt.Errorf("sqlite.CommHubRepo.GrantPermission: %w", err)
	}

	return nil
}

// RevokePermission removes a communication permission for the given agent-target pair.
func (r *CommHubRepo) RevokePermission(ctx context.Context, agentID, targetID string) error {
	res, err := r.db.ExecContext(ctx,
		"DELETE FROM comm_permissions WHERE agent_id = ? AND target_id = ?",
		agentID, targetID,
	)
	if err != nil {
		return fmt.Errorf("sqlite.CommHubRepo.RevokePermission: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.CommHubRepo.RevokePermission: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("permission not found for %s -> %s", agentID, targetID)
	}

	return nil
}

// CheckPermission returns true if the agent has permission to communicate
// with the target. Checks both exact target match and wildcard ("*") grant.
func (r *CommHubRepo) CheckPermission(ctx context.Context, agentID, targetID string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM comm_permissions
		 WHERE agent_id = ? AND (target_id = ? OR target_id = '*')
		 AND permission IN ('send', 'both')`,
		agentID, targetID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("sqlite.CommHubRepo.CheckPermission: %w", err)
	}

	return count > 0, nil
}

// ListPermissions returns all permissions granted to the given agent.
func (r *CommHubRepo) ListPermissions(ctx context.Context, agentID string) ([]*types.CommPermission, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, agent_id, target_id, permission, created_at
		 FROM comm_permissions
		 WHERE agent_id = ?
		 ORDER BY target_id`,
		agentID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.CommHubRepo.ListPermissions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var perms []*types.CommPermission
	for rows.Next() {
		p := &types.CommPermission{}
		var createdAt string
		if err := rows.Scan(&p.ID, &p.AgentID, &p.TargetID, &p.Permission, &createdAt); err != nil {
			return nil, fmt.Errorf("sqlite.CommHubRepo.ListPermissions: %w", err)
		}
		p.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		perms = append(perms, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.CommHubRepo.ListPermissions: %w", err)
	}
	return perms, nil
}

// --- Overflow Persistence ---

// PersistOverflow stores a message that was dropped due to inbox backpressure.
func (r *CommHubRepo) PersistOverflow(ctx context.Context, entry *types.OverflowEntry) error {
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}

	metaJSON, err := json.Marshal(entry.Metadata)
	if err != nil {
		metaJSON = []byte("{}")
	}

	_, err = r.db.ExecContext(ctx,
		`INSERT INTO commhub_overflow (id, agent_id, from_agent, to_agent, content_type, content, trust, metadata, original_ts)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.AgentID, entry.From, entry.To, entry.ContentType, entry.Content, entry.Trust, string(metaJSON), entry.OriginalTS,
	)
	if err != nil {
		return fmt.Errorf("sqlite.CommHubRepo.PersistOverflow: %w", err)
	}
	return nil
}

// DrainOverflow retrieves and marks as retrieved up to limit overflow messages
// for the given agent, ordered oldest first.
func (r *CommHubRepo) DrainOverflow(ctx context.Context, agentID string, limit int) ([]*types.OverflowEntry, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, agent_id, from_agent, to_agent, content_type, content, trust, metadata, original_ts, created_at
		 FROM commhub_overflow
		 WHERE agent_id = ? AND retrieved = 0
		 ORDER BY original_ts ASC
		 LIMIT ?`,
		agentID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.CommHubRepo.DrainOverflow: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []*types.OverflowEntry
	var ids []any
	for rows.Next() {
		e := &types.OverflowEntry{}
		var metaJSON, createdAt string
		if err := rows.Scan(&e.ID, &e.AgentID, &e.From, &e.To, &e.ContentType, &e.Content, &e.Trust, &metaJSON, &e.OriginalTS, &createdAt); err != nil {
			return nil, fmt.Errorf("sqlite.CommHubRepo.DrainOverflow: %w", err)
		}
		if err := json.Unmarshal([]byte(metaJSON), &e.Metadata); err != nil {
			slog.Error("failed to unmarshal overflow metadata from database", "error", err)
		}
		if e.Metadata == nil {
			e.Metadata = make(map[string]string)
		}
		e.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		entries = append(entries, e)
		ids = append(ids, e.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.CommHubRepo.DrainOverflow: %w", err)
	}

	// Mark as retrieved.
	for _, id := range ids {
		if _, err := r.db.ExecContext(ctx, "UPDATE commhub_overflow SET retrieved = 1 WHERE id = ?", id); err != nil {
			slog.Error("failed to mark overflow as retrieved — may cause duplicate delivery", "id", id, "error", err)
		}
	}

	return entries, nil
}

// CountOverflow returns the number of unretrieved overflow entries for an agent.
func (r *CommHubRepo) CountOverflow(ctx context.Context, agentID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM commhub_overflow WHERE agent_id = ? AND retrieved = 0",
		agentID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("sqlite.CommHubRepo.CountOverflow: %w", err)
	}
	return count, nil
}

// RenameAgentRefs cascades an agent name change to all comm_log entries
// and agent_relationships referencing the old name.
func (r *CommHubRepo) RenameAgentRefs(ctx context.Context, oldName, newName string) error {
	if _, err := r.db.ExecContext(ctx,
		`UPDATE comm_log SET from_agent = ? WHERE from_agent = ?`,
		newName, oldName,
	); err != nil {
		return fmt.Errorf("sqlite.CommHubRepo.RenameAgentRefs: %w", err)
	}
	if _, err := r.db.ExecContext(ctx,
		`UPDATE comm_log SET to_agent = ? WHERE to_agent = ?`,
		newName, oldName,
	); err != nil {
		return fmt.Errorf("sqlite.CommHubRepo.RenameAgentRefs: %w", err)
	}
	if _, err := r.db.ExecContext(ctx,
		`UPDATE agent_relationships SET parent_agent = ? WHERE parent_agent = ?`,
		newName, oldName,
	); err != nil {
		return fmt.Errorf("sqlite.CommHubRepo.RenameAgentRefs: %w", err)
	}
	if _, err := r.db.ExecContext(ctx,
		`UPDATE agent_relationships SET child_agent = ? WHERE child_agent = ?`,
		newName, oldName,
	); err != nil {
		return fmt.Errorf("sqlite.CommHubRepo.RenameAgentRefs: %w", err)
	}
	return nil
}

// PurgeOverflow deletes retrieved overflow entries older than the given cutoff.
func (r *CommHubRepo) PurgeOverflow(ctx context.Context, olderThan string) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		"DELETE FROM commhub_overflow WHERE retrieved = 1 AND created_at < ?",
		olderThan,
	)
	if err != nil {
		return 0, fmt.Errorf("sqlite.CommHubRepo.PurgeOverflow: %w", err)
	}
	return res.RowsAffected()
}
