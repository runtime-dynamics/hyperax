package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/pkg/types"
)

// InterjectionRepo implements repo.InterjectionRepo for MySQL.
type InterjectionRepo struct {
	db *sql.DB
}

// Create inserts a new interjection with status "active" and returns its generated ID.
func (r *InterjectionRepo) Create(ctx context.Context, ij *types.Interjection) (string, error) {
	if ij.ID == "" {
		ij.ID = uuid.New().String()
	}

	var expiresAt *time.Time
	if ij.ExpiresAt != nil {
		t := ij.ExpiresAt.UTC()
		expiresAt = &t
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO interjections
		 (id, scope, severity, source, reason, status, created_by, source_clearance,
		  remediation_persona, trust_level, trace_id, expires_at)
		 VALUES (?, ?, ?, ?, ?, 'active', ?, ?, ?, ?, ?, ?)`,
		ij.ID, ij.Scope, ij.Severity, ij.Source, ij.Reason,
		myNullStr(ij.CreatedBy), ij.SourceClearance,
		myNullStr(ij.RemediationPersona), myNullStr(ij.TrustLevel),
		myNullStr(ij.TraceID), expiresAt,
	)
	if err != nil {
		return "", fmt.Errorf("mysql.InterjectionRepo.Create: %w", err)
	}

	return ij.ID, nil
}

// GetByID returns a single interjection by ID.
func (r *InterjectionRepo) GetByID(ctx context.Context, id string) (*types.Interjection, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, scope, severity, source, reason, status, resolution,
		        created_by, source_clearance, resolved_by, resolver_clearance,
		        remediation_persona, action, trust_level, trace_id,
		        created_at, resolved_at, expires_at
		 FROM interjections WHERE id = ?`, id)

	ij, err := scanMyInterjection(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("interjection %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("mysql.InterjectionRepo.GetByID: %w", err)
	}
	return ij, nil
}

// GetActive returns all active interjections for the given scope.
func (r *InterjectionRepo) GetActive(ctx context.Context, scope string) ([]*types.Interjection, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, scope, severity, source, reason, status, resolution,
		        created_by, source_clearance, resolved_by, resolver_clearance,
		        remediation_persona, action, trust_level, trace_id,
		        created_at, resolved_at, expires_at
		 FROM interjections
		 WHERE scope = ? AND status = 'active'
		 ORDER BY created_at DESC`, scope)
	if err != nil {
		return nil, fmt.Errorf("mysql.InterjectionRepo.GetActive: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanMyInterjections(rows)
}

// GetAllActive returns all active interjections across all scopes.
func (r *InterjectionRepo) GetAllActive(ctx context.Context) ([]*types.Interjection, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, scope, severity, source, reason, status, resolution,
		        created_by, source_clearance, resolved_by, resolver_clearance,
		        remediation_persona, action, trust_level, trace_id,
		        created_at, resolved_at, expires_at
		 FROM interjections
		 WHERE status = 'active'
		 ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("mysql.InterjectionRepo.GetAllActive: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanMyInterjections(rows)
}

// GetHistory returns resolved interjections for a scope, newest first.
func (r *InterjectionRepo) GetHistory(ctx context.Context, scope string, limit int) ([]*types.Interjection, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, scope, severity, source, reason, status, resolution,
		        created_by, source_clearance, resolved_by, resolver_clearance,
		        remediation_persona, action, trust_level, trace_id,
		        created_at, resolved_at, expires_at
		 FROM interjections
		 WHERE scope = ? AND status != 'active'
		 ORDER BY resolved_at DESC
		 LIMIT ?`, scope, limit)
	if err != nil {
		return nil, fmt.Errorf("mysql.InterjectionRepo.GetHistory: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanMyInterjections(rows)
}

// Resolve marks an interjection as resolved with action and resolver info.
func (r *InterjectionRepo) Resolve(ctx context.Context, id string, action *types.ResolutionAction) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE interjections
		 SET status = 'resolved', resolution = ?, action = ?, resolved_by = ?,
		     resolver_clearance = ?, resolved_at = NOW()
		 WHERE id = ? AND status = 'active'`,
		action.Resolution, action.Action, action.ResolvedBy,
		action.ResolverClearance, id,
	)
	if err != nil {
		return fmt.Errorf("mysql.InterjectionRepo.Resolve: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mysql.InterjectionRepo.Resolve: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("interjection %q not found or already resolved", id)
	}

	return nil
}

// Expire marks an interjection as expired (TTL-based).
func (r *InterjectionRepo) Expire(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE interjections
		 SET status = 'expired', resolved_at = NOW()
		 WHERE id = ? AND status = 'active'`, id)
	if err != nil {
		return fmt.Errorf("mysql.InterjectionRepo.Expire: %w", err)
	}
	return nil
}

// GetClearanceLevel retrieves the clearance_level for an agent (or legacy persona).
// Checks the agents table first (post-migration-028), falling back to personas.
func (r *InterjectionRepo) GetClearanceLevel(ctx context.Context, personaID string) (int, error) {
	var level int
	err := r.db.QueryRowContext(ctx,
		`SELECT clearance_level FROM agents WHERE id = ?`, personaID).Scan(&level)
	if err == nil {
		return level, nil
	}
	// Fallback to personas table for legacy IDs.
	err = r.db.QueryRowContext(ctx,
		`SELECT clearance_level FROM personas WHERE id = ?`, personaID).Scan(&level)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("agent %q not found", personaID)
	}
	if err != nil {
		return 0, fmt.Errorf("mysql.InterjectionRepo.GetClearanceLevel: %w", err)
	}
	return level, nil
}

// --- Sieve Bypass ---

// CreateBypass stores a sieve bypass grant.
func (r *InterjectionRepo) CreateBypass(ctx context.Context, bypass *types.SieveBypass) (string, error) {
	if bypass.ID == "" {
		bypass.ID = uuid.New().String()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO sieve_bypass (id, scope, pattern, granted_by, expires_at, reason)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		bypass.ID, bypass.Scope, bypass.Pattern, bypass.GrantedBy,
		bypass.ExpiresAt.UTC(), myNullStr(bypass.Reason),
	)
	if err != nil {
		return "", fmt.Errorf("mysql.InterjectionRepo.CreateBypass: %w", err)
	}
	return bypass.ID, nil
}

// GetActiveBypass returns active (non-expired, non-revoked) bypasses for a scope.
func (r *InterjectionRepo) GetActiveBypass(ctx context.Context, scope string) ([]*types.SieveBypass, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, scope, pattern, granted_by, granted_at, expires_at, reason, revoked
		 FROM sieve_bypass
		 WHERE scope = ? AND revoked = 0 AND expires_at > NOW()
		 ORDER BY granted_at DESC`, scope)
	if err != nil {
		return nil, fmt.Errorf("mysql.InterjectionRepo.GetActiveBypass: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []*types.SieveBypass
	for rows.Next() {
		b := &types.SieveBypass{}
		var reason sql.NullString

		if err := rows.Scan(&b.ID, &b.Scope, &b.Pattern, &b.GrantedBy,
			&b.GrantedAt, &b.ExpiresAt, &reason, &b.Revoked); err != nil {
			return nil, fmt.Errorf("mysql.InterjectionRepo.GetActiveBypass: %w", err)
		}
		if reason.Valid {
			b.Reason = reason.String
		}
		results = append(results, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql.InterjectionRepo.GetActiveBypass: %w", err)
	}
	return results, nil
}

// RevokeBypass marks a bypass as revoked.
func (r *InterjectionRepo) RevokeBypass(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE sieve_bypass SET revoked = 1 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("mysql.InterjectionRepo.RevokeBypass: %w", err)
	}
	return nil
}

// ExpireBypasses marks all expired but non-revoked bypasses as revoked.
func (r *InterjectionRepo) ExpireBypasses(ctx context.Context) (int, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE sieve_bypass SET revoked = 1
		 WHERE revoked = 0 AND expires_at <= NOW()`)
	if err != nil {
		return 0, fmt.Errorf("mysql.InterjectionRepo.ExpireBypasses: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("mysql.InterjectionRepo.ExpireBypasses: %w", err)
	}
	return int(n), nil
}

// --- Dead Letter Queue ---

// EnqueueDLQ adds an entry to the dead letter queue.
func (r *InterjectionRepo) EnqueueDLQ(ctx context.Context, entry *types.DLQEntry) (string, error) {
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO interject_dlq (id, interjection_id, message_type, payload, source, scope)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.InterjectionID, entry.MessageType, entry.Payload, entry.Source, entry.Scope,
	)
	if err != nil {
		return "", fmt.Errorf("mysql.InterjectionRepo.EnqueueDLQ: %w", err)
	}
	return entry.ID, nil
}

// ListDLQ returns queued DLQ entries for an interjection.
func (r *InterjectionRepo) ListDLQ(ctx context.Context, interjectionID string, limit int) ([]*types.DLQEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, interjection_id, message_type, payload, source, scope,
		        queued_at, replayed_at, dismissed_at, status
		 FROM interject_dlq
		 WHERE interjection_id = ? AND status = 'queued'
		 ORDER BY queued_at ASC
		 LIMIT ?`, interjectionID, limit)
	if err != nil {
		return nil, fmt.Errorf("mysql.InterjectionRepo.ListDLQ: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []*types.DLQEntry
	for rows.Next() {
		e := &types.DLQEntry{}
		var replayedAt, dismissedAt sql.NullTime

		if err := rows.Scan(&e.ID, &e.InterjectionID, &e.MessageType, &e.Payload,
			&e.Source, &e.Scope, &e.QueuedAt, &replayedAt, &dismissedAt, &e.Status); err != nil {
			return nil, fmt.Errorf("mysql.InterjectionRepo.ListDLQ: %w", err)
		}
		if replayedAt.Valid {
			e.ReplayedAt = &replayedAt.Time
		}
		if dismissedAt.Valid {
			e.DismissedAt = &dismissedAt.Time
		}
		results = append(results, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql.InterjectionRepo.ListDLQ: %w", err)
	}
	return results, nil
}

// ReplayDLQ marks a DLQ entry as replayed.
func (r *InterjectionRepo) ReplayDLQ(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE interject_dlq SET status = 'replayed', replayed_at = NOW() WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("mysql.InterjectionRepo.ReplayDLQ: %w", err)
	}
	return nil
}

// DismissDLQ marks a DLQ entry as dismissed.
func (r *InterjectionRepo) DismissDLQ(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE interject_dlq SET status = 'dismissed', dismissed_at = NOW() WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("mysql.InterjectionRepo.DismissDLQ: %w", err)
	}
	return nil
}

// CountDLQ returns the number of queued DLQ entries for a given interjection.
func (r *InterjectionRepo) CountDLQ(ctx context.Context, interjectionID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM interject_dlq WHERE interjection_id = ? AND status = 'queued'`,
		interjectionID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("mysql.InterjectionRepo.CountDLQ: %w", err)
	}
	return count, nil
}

// --- helpers ---

// myNullStr converts a string to sql.NullString (empty -> NULL).
func myNullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

type myInterjectionScanner interface {
	Scan(dest ...any) error
}

func scanMyInterjectionRow(s myInterjectionScanner) (*types.Interjection, error) {
	ij := &types.Interjection{}
	var resolution, createdBy, resolvedBy, remediationPersona sql.NullString
	var action, trustLevel, traceID sql.NullString
	var resolvedAt, expiresAt sql.NullTime
	var resolverClearance sql.NullInt64

	if err := s.Scan(
		&ij.ID, &ij.Scope, &ij.Severity, &ij.Source, &ij.Reason, &ij.Status,
		&resolution, &createdBy, &ij.SourceClearance, &resolvedBy, &resolverClearance,
		&remediationPersona, &action, &trustLevel, &traceID,
		&ij.CreatedAt, &resolvedAt, &expiresAt,
	); err != nil {
		return nil, fmt.Errorf("mysql.scanMyInterjectionRow: %w", err)
	}

	if resolution.Valid {
		ij.Resolution = resolution.String
	}
	if createdBy.Valid {
		ij.CreatedBy = createdBy.String
	}
	if resolvedBy.Valid {
		ij.ResolvedBy = resolvedBy.String
	}
	if resolverClearance.Valid {
		ij.ResolverClearance = int(resolverClearance.Int64)
	}
	if remediationPersona.Valid {
		ij.RemediationPersona = remediationPersona.String
	}
	if action.Valid {
		ij.Action = action.String
	}
	if trustLevel.Valid {
		ij.TrustLevel = trustLevel.String
	}
	if traceID.Valid {
		ij.TraceID = traceID.String
	}
	if resolvedAt.Valid {
		ij.ResolvedAt = &resolvedAt.Time
	}
	if expiresAt.Valid {
		ij.ExpiresAt = &expiresAt.Time
	}

	return ij, nil
}

func scanMyInterjection(row *sql.Row) (*types.Interjection, error) {
	return scanMyInterjectionRow(row)
}

func scanMyInterjections(rows *sql.Rows) ([]*types.Interjection, error) {
	var results []*types.Interjection
	for rows.Next() {
		ij, err := scanMyInterjectionRow(rows)
		if err != nil {
			return nil, fmt.Errorf("mysql.scanMyInterjections: %w", err)
		}
		results = append(results, ij)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql.scanMyInterjections: %w", err)
	}
	return results, nil
}
