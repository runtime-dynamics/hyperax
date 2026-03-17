package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/pkg/types"
)

// CommHubRepo implements repo.CommHubRepo for PostgreSQL.
type CommHubRepo struct {
	db *sql.DB
}

// --- Hierarchy ---

// SetRelationship creates or replaces an agent relationship.
func (r *CommHubRepo) SetRelationship(ctx context.Context, rel *types.AgentRelationship) error {
	if rel.ID == "" {
		rel.ID = uuid.New().String()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO agent_relationships (id, parent_agent, child_agent, relationship)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT(id) DO UPDATE SET
		    parent_agent = EXCLUDED.parent_agent,
		    child_agent  = EXCLUDED.child_agent,
		    relationship = EXCLUDED.relationship`,
		rel.ID, rel.ParentAgent, rel.ChildAgent, rel.Relationship,
	)
	if err != nil {
		return fmt.Errorf("postgres.CommHubRepo.SetRelationship: %w", err)
	}
	return nil
}

// GetRelationship retrieves a relationship by parent and child agent IDs.
func (r *CommHubRepo) GetRelationship(ctx context.Context, parentAgent, childAgent string) (*types.AgentRelationship, error) {
	rel := &types.AgentRelationship{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, parent_agent, child_agent, relationship, created_at
		 FROM agent_relationships
		 WHERE parent_agent = $1 AND child_agent = $2`,
		parentAgent, childAgent,
	).Scan(&rel.ID, &rel.ParentAgent, &rel.ChildAgent, &rel.Relationship, &rel.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("relationship not found: %s -> %s", parentAgent, childAgent)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres.CommHubRepo.GetRelationship: %w", err)
	}
	return rel, nil
}

// GetChildren returns all child relationships for a parent agent.
func (r *CommHubRepo) GetChildren(ctx context.Context, parentAgent string) ([]*types.AgentRelationship, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, parent_agent, child_agent, relationship, created_at
		 FROM agent_relationships
		 WHERE parent_agent = $1
		 ORDER BY child_agent`,
		parentAgent,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.CommHubRepo.GetChildren: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanPgRelationships(rows)
}

// GetParent returns the parent relationship for a child agent.
func (r *CommHubRepo) GetParent(ctx context.Context, childAgent string) (*types.AgentRelationship, error) {
	rel := &types.AgentRelationship{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, parent_agent, child_agent, relationship, created_at
		 FROM agent_relationships
		 WHERE child_agent = $1
		 LIMIT 1`,
		childAgent,
	).Scan(&rel.ID, &rel.ParentAgent, &rel.ChildAgent, &rel.Relationship, &rel.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no parent for agent %q", childAgent)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres.CommHubRepo.GetParent: %w", err)
	}
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
		return nil, fmt.Errorf("postgres.CommHubRepo.GetFullHierarchy: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanPgRelationships(rows)
}

// DeleteRelationship removes a relationship by its ID.
func (r *CommHubRepo) DeleteRelationship(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM agent_relationships WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("postgres.CommHubRepo.DeleteRelationship: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.CommHubRepo.DeleteRelationship: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("relationship %q not found", id)
	}
	return nil
}

func scanPgRelationships(rows *sql.Rows) ([]*types.AgentRelationship, error) {
	var rels []*types.AgentRelationship
	for rows.Next() {
		rel := &types.AgentRelationship{}
		if err := rows.Scan(&rel.ID, &rel.ParentAgent, &rel.ChildAgent, &rel.Relationship, &rel.CreatedAt); err != nil {
			return nil, fmt.Errorf("postgres.scanPgRelationships: %w", err)
		}
		rels = append(rels, rel)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.scanPgRelationships: %w", err)
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
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		entry.ID, entry.FromAgent, entry.ToAgent, entry.ContentType, entry.Content, entry.Trust, entry.Direction,
	)
	if err != nil {
		return fmt.Errorf("postgres.CommHubRepo.LogMessage: %w", err)
	}
	return nil
}

// GetCommLog retrieves communication log entries for a specific agent.
func (r *CommHubRepo) GetCommLog(ctx context.Context, agentID string, limit int) ([]*types.CommLogEntry, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, from_agent, to_agent, content_type, content, trust, direction, created_at
		 FROM comm_log
		 WHERE from_agent = $1 OR to_agent = $2
		 ORDER BY created_at DESC
		 LIMIT $3`,
		agentID, agentID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.CommHubRepo.GetCommLog: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanPgCommLogEntries(rows)
}

// GetCommLogBetween retrieves log entries between two specific agents.
func (r *CommHubRepo) GetCommLogBetween(ctx context.Context, from, to string, limit int) ([]*types.CommLogEntry, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, from_agent, to_agent, content_type, content, trust, direction, created_at
		 FROM comm_log
		 WHERE (from_agent = $1 AND to_agent = $2) OR (from_agent = $3 AND to_agent = $4)
		 ORDER BY created_at DESC
		 LIMIT $5`,
		from, to, to, from, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.CommHubRepo.GetCommLogBetween: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanPgCommLogEntries(rows)
}

// scanPgCommLogEntries scans a sql.Rows result set into a slice of CommLogEntry.
// Supports both 8-column (legacy) and 9-column (with session_id) result sets.
func scanPgCommLogEntries(rows *sql.Rows) ([]*types.CommLogEntry, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("postgres.scanPgCommLogEntries: %w", err)
	}
	hasSessionID := len(cols) >= 9

	var entries []*types.CommLogEntry
	for rows.Next() {
		e := &types.CommLogEntry{}
		if hasSessionID {
			var sessionID sql.NullString
			if err := rows.Scan(&e.ID, &e.FromAgent, &e.ToAgent, &e.ContentType, &e.Content, &e.Trust, &e.Direction, &sessionID, &e.CreatedAt); err != nil {
				return nil, fmt.Errorf("postgres.scanPgCommLogEntries: %w", err)
			}
			if sessionID.Valid {
				e.SessionID = sessionID.String
			}
		} else {
			if err := rows.Scan(&e.ID, &e.FromAgent, &e.ToAgent, &e.ContentType, &e.Content, &e.Trust, &e.Direction, &e.CreatedAt); err != nil {
				return nil, fmt.Errorf("postgres.scanPgCommLogEntries: %w", err)
			}
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.scanPgCommLogEntries: %w", err)
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
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		entry.ID, entry.FromAgent, entry.ToAgent, entry.ContentType, entry.Content, entry.Trust, entry.Direction, entry.SessionID,
	)
	if err != nil {
		return fmt.Errorf("postgres.CommHubRepo.LogMessageWithSession: %w", err)
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
		 WHERE session_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2`,
		sessionID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.CommHubRepo.GetCommLogBySession: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanPgCommLogEntries(rows)
}

// --- Permissions ---

// GrantPermission creates or replaces a communication permission.
func (r *CommHubRepo) GrantPermission(ctx context.Context, perm *types.CommPermission) error {
	if perm.ID == "" {
		perm.ID = uuid.New().String()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO comm_permissions (id, agent_id, target_id, permission)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT(agent_id, target_id) DO UPDATE SET
		    permission = EXCLUDED.permission`,
		perm.ID, perm.AgentID, perm.TargetID, perm.Permission,
	)
	if err != nil {
		return fmt.Errorf("postgres.CommHubRepo.GrantPermission: %w", err)
	}
	return nil
}

// RevokePermission removes a communication permission for the given agent-target pair.
func (r *CommHubRepo) RevokePermission(ctx context.Context, agentID, targetID string) error {
	res, err := r.db.ExecContext(ctx,
		"DELETE FROM comm_permissions WHERE agent_id = $1 AND target_id = $2",
		agentID, targetID,
	)
	if err != nil {
		return fmt.Errorf("postgres.CommHubRepo.RevokePermission: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.CommHubRepo.RevokePermission: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("permission not found for %s -> %s", agentID, targetID)
	}
	return nil
}

// CheckPermission returns true if the agent has permission to communicate with the target.
func (r *CommHubRepo) CheckPermission(ctx context.Context, agentID, targetID string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM comm_permissions
		 WHERE agent_id = $1 AND (target_id = $2 OR target_id = '*')
		 AND permission IN ('send', 'both')`,
		agentID, targetID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("postgres.CommHubRepo.CheckPermission: %w", err)
	}
	return count > 0, nil
}

// ListPermissions returns all permissions granted to the given agent.
func (r *CommHubRepo) ListPermissions(ctx context.Context, agentID string) ([]*types.CommPermission, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, agent_id, target_id, permission, created_at
		 FROM comm_permissions
		 WHERE agent_id = $1
		 ORDER BY target_id`,
		agentID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.CommHubRepo.ListPermissions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var perms []*types.CommPermission
	for rows.Next() {
		p := &types.CommPermission{}
		if err := rows.Scan(&p.ID, &p.AgentID, &p.TargetID, &p.Permission, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("postgres.CommHubRepo.ListPermissions: %w", err)
		}
		perms = append(perms, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.CommHubRepo.ListPermissions: %w", err)
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
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		entry.ID, entry.AgentID, entry.From, entry.To, entry.ContentType, entry.Content, entry.Trust, string(metaJSON), entry.OriginalTS,
	)
	if err != nil {
		return fmt.Errorf("postgres.CommHubRepo.PersistOverflow: %w", err)
	}
	return nil
}

// DrainOverflow retrieves and marks as retrieved up to limit overflow messages.
func (r *CommHubRepo) DrainOverflow(ctx context.Context, agentID string, limit int) ([]*types.OverflowEntry, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, agent_id, from_agent, to_agent, content_type, content, trust, metadata, original_ts, created_at
		 FROM commhub_overflow
		 WHERE agent_id = $1 AND retrieved = FALSE
		 ORDER BY original_ts ASC
		 LIMIT $2`,
		agentID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.CommHubRepo.DrainOverflow: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []*types.OverflowEntry
	var ids []any
	for rows.Next() {
		e := &types.OverflowEntry{}
		var metaJSON string
		if err := rows.Scan(&e.ID, &e.AgentID, &e.From, &e.To, &e.ContentType, &e.Content, &e.Trust, &metaJSON, &e.OriginalTS, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("postgres.CommHubRepo.DrainOverflow: %w", err)
		}
		if err := json.Unmarshal([]byte(metaJSON), &e.Metadata); err != nil {
			return nil, fmt.Errorf("postgres.CommHubRepo.DrainOverflow: unmarshal metadata: %w", err)
		}
		if e.Metadata == nil {
			e.Metadata = make(map[string]string)
		}
		entries = append(entries, e)
		ids = append(ids, e.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.CommHubRepo.DrainOverflow: %w", err)
	}

	// Mark as retrieved.
	for _, id := range ids {
		if _, err := r.db.ExecContext(ctx, "UPDATE commhub_overflow SET retrieved = TRUE WHERE id = $1", id); err != nil {
			return nil, fmt.Errorf("postgres.CommHubRepo.DrainOverflow: mark retrieved id=%v: %w", id, err)
		}
	}
	return entries, nil
}

// CountOverflow returns the number of unretrieved overflow entries for an agent.
func (r *CommHubRepo) CountOverflow(ctx context.Context, agentID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM commhub_overflow WHERE agent_id = $1 AND retrieved = FALSE",
		agentID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("postgres.CommHubRepo.CountOverflow: %w", err)
	}
	return count, nil
}

// PurgeOverflow deletes retrieved overflow entries older than the given cutoff.
// RenameAgentRefs cascades an agent name change to all comm_log entries
// and agent_relationships referencing the old name.
func (r *CommHubRepo) RenameAgentRefs(ctx context.Context, oldName, newName string) error {
	if _, err := r.db.ExecContext(ctx,
		`UPDATE comm_log SET from_agent = $1 WHERE from_agent = $2`,
		newName, oldName,
	); err != nil {
		return fmt.Errorf("postgres.CommHubRepo.RenameAgentRefs: %w", err)
	}
	if _, err := r.db.ExecContext(ctx,
		`UPDATE comm_log SET to_agent = $1 WHERE to_agent = $2`,
		newName, oldName,
	); err != nil {
		return fmt.Errorf("postgres.CommHubRepo.RenameAgentRefs: %w", err)
	}
	if _, err := r.db.ExecContext(ctx,
		`UPDATE agent_relationships SET parent_agent = $1 WHERE parent_agent = $2`,
		newName, oldName,
	); err != nil {
		return fmt.Errorf("postgres.CommHubRepo.RenameAgentRefs: %w", err)
	}
	if _, err := r.db.ExecContext(ctx,
		`UPDATE agent_relationships SET child_agent = $1 WHERE child_agent = $2`,
		newName, oldName,
	); err != nil {
		return fmt.Errorf("postgres.CommHubRepo.RenameAgentRefs: %w", err)
	}
	return nil
}

func (r *CommHubRepo) PurgeOverflow(ctx context.Context, olderThan string) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		"DELETE FROM commhub_overflow WHERE retrieved = TRUE AND created_at < $1",
		olderThan,
	)
	if err != nil {
		return 0, fmt.Errorf("postgres.CommHubRepo.PurgeOverflow: %w", err)
	}
	return res.RowsAffected()
}
