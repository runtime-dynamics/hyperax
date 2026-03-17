package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/pkg/types"
)

// InterjectionRepo implements repo.InterjectionRepo for PostgreSQL.
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
		 VALUES ($1, $2, $3, $4, $5, 'active', $6, $7, $8, $9, $10, $11)`,
		ij.ID, ij.Scope, ij.Severity, ij.Source, ij.Reason,
		pgNullStr(ij.CreatedBy), ij.SourceClearance,
		pgNullStr(ij.RemediationPersona), pgNullStr(ij.TrustLevel),
		pgNullStr(ij.TraceID), expiresAt,
	)
	if err != nil {
		return "", fmt.Errorf("postgres.InterjectionRepo.Create: %w", err)
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
		 FROM interjections WHERE id = $1`, id)

	ij, err := scanPgInterjection(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("interjection %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres.InterjectionRepo.GetByID: %w", err)
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
		 WHERE scope = $1 AND status = 'active'
		 ORDER BY created_at DESC`, scope)
	if err != nil {
		return nil, fmt.Errorf("postgres.InterjectionRepo.GetActive: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanPgInterjections(rows)
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
		return nil, fmt.Errorf("postgres.InterjectionRepo.GetAllActive: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanPgInterjections(rows)
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
		 WHERE scope = $1 AND status != 'active'
		 ORDER BY resolved_at DESC
		 LIMIT $2`, scope, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres.InterjectionRepo.GetHistory: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanPgInterjections(rows)
}

// Resolve marks an interjection as resolved with action and resolver info.
func (r *InterjectionRepo) Resolve(ctx context.Context, id string, action *types.ResolutionAction) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE interjections
		 SET status = 'resolved', resolution = $1, action = $2, resolved_by = $3,
		     resolver_clearance = $4, resolved_at = NOW()
		 WHERE id = $5 AND status = 'active'`,
		action.Resolution, action.Action, action.ResolvedBy,
		action.ResolverClearance, id,
	)
	if err != nil {
		return fmt.Errorf("postgres.InterjectionRepo.Resolve: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.InterjectionRepo.Resolve: %w", err)
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
		 WHERE id = $1 AND status = 'active'`, id)
	if err != nil {
		return fmt.Errorf("postgres.InterjectionRepo.Expire: %w", err)
	}
	return nil
}

// GetClearanceLevel retrieves the clearance_level for an agent (or legacy persona).
// Checks the agents table first (post-migration-028), falling back to personas.
func (r *InterjectionRepo) GetClearanceLevel(ctx context.Context, personaID string) (int, error) {
	var level int
	err := r.db.QueryRowContext(ctx,
		`SELECT clearance_level FROM agents WHERE id = $1`, personaID).Scan(&level)
	if err == nil {
		return level, nil
	}
	// Fallback to personas table for legacy IDs.
	err = r.db.QueryRowContext(ctx,
		`SELECT clearance_level FROM personas WHERE id = $1`, personaID).Scan(&level)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("agent %q not found", personaID)
	}
	if err != nil {
		return 0, fmt.Errorf("postgres.InterjectionRepo.GetClearanceLevel: %w", err)
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
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		bypass.ID, bypass.Scope, bypass.Pattern, bypass.GrantedBy,
		bypass.ExpiresAt.UTC(), pgNullStr(bypass.Reason),
	)
	if err != nil {
		return "", fmt.Errorf("postgres.InterjectionRepo.CreateBypass: %w", err)
	}
	return bypass.ID, nil
}

// GetActiveBypass returns active (non-expired, non-revoked) bypasses for a scope.
func (r *InterjectionRepo) GetActiveBypass(ctx context.Context, scope string) ([]*types.SieveBypass, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, scope, pattern, granted_by, granted_at, expires_at, reason, revoked
		 FROM sieve_bypass
		 WHERE scope = $1 AND revoked = FALSE AND expires_at > NOW()
		 ORDER BY granted_at DESC`, scope)
	if err != nil {
		return nil, fmt.Errorf("postgres.InterjectionRepo.GetActiveBypass: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []*types.SieveBypass
	for rows.Next() {
		b := &types.SieveBypass{}
		var reason sql.NullString

		if err := rows.Scan(&b.ID, &b.Scope, &b.Pattern, &b.GrantedBy,
			&b.GrantedAt, &b.ExpiresAt, &reason, &b.Revoked); err != nil {
			return nil, fmt.Errorf("postgres.InterjectionRepo.GetActiveBypass: %w", err)
		}
		if reason.Valid {
			b.Reason = reason.String
		}
		results = append(results, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.InterjectionRepo.GetActiveBypass: %w", err)
	}
	return results, nil
}

// RevokeBypass marks a bypass as revoked.
func (r *InterjectionRepo) RevokeBypass(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE sieve_bypass SET revoked = TRUE WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("postgres.InterjectionRepo.RevokeBypass: %w", err)
	}
	return nil
}

// ExpireBypasses marks all expired but non-revoked bypasses as revoked.
func (r *InterjectionRepo) ExpireBypasses(ctx context.Context) (int, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE sieve_bypass SET revoked = TRUE
		 WHERE revoked = FALSE AND expires_at <= NOW()`)
	if err != nil {
		return 0, fmt.Errorf("postgres.InterjectionRepo.ExpireBypasses: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("postgres.InterjectionRepo.ExpireBypasses: %w", err)
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
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		entry.ID, entry.InterjectionID, entry.MessageType, entry.Payload, entry.Source, entry.Scope,
	)
	if err != nil {
		return "", fmt.Errorf("postgres.InterjectionRepo.EnqueueDLQ: %w", err)
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
		 WHERE interjection_id = $1 AND status = 'queued'
		 ORDER BY queued_at ASC
		 LIMIT $2`, interjectionID, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres.InterjectionRepo.ListDLQ: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []*types.DLQEntry
	for rows.Next() {
		e := &types.DLQEntry{}
		var replayedAt, dismissedAt sql.NullTime

		if err := rows.Scan(&e.ID, &e.InterjectionID, &e.MessageType, &e.Payload,
			&e.Source, &e.Scope, &e.QueuedAt, &replayedAt, &dismissedAt, &e.Status); err != nil {
			return nil, fmt.Errorf("postgres.InterjectionRepo.ListDLQ: %w", err)
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
		return nil, fmt.Errorf("postgres.InterjectionRepo.ListDLQ: %w", err)
	}
	return results, nil
}

// ReplayDLQ marks a DLQ entry as replayed.
func (r *InterjectionRepo) ReplayDLQ(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE interject_dlq SET status = 'replayed', replayed_at = NOW() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("postgres.InterjectionRepo.ReplayDLQ: %w", err)
	}
	return nil
}

// DismissDLQ marks a DLQ entry as dismissed.
func (r *InterjectionRepo) DismissDLQ(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE interject_dlq SET status = 'dismissed', dismissed_at = NOW() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("postgres.InterjectionRepo.DismissDLQ: %w", err)
	}
	return nil
}

// CountDLQ returns the number of queued DLQ entries for a given interjection.
func (r *InterjectionRepo) CountDLQ(ctx context.Context, interjectionID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM interject_dlq WHERE interjection_id = $1 AND status = 'queued'`,
		interjectionID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("postgres.InterjectionRepo.CountDLQ: %w", err)
	}
	return count, nil
}

// --- helpers ---

// pgNullStr converts a string to sql.NullString (empty -> NULL).
func pgNullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

type pgInterjectionScanner interface {
	Scan(dest ...any) error
}

func scanPgInterjectionRow(s pgInterjectionScanner) (*types.Interjection, error) {
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
		return nil, fmt.Errorf("postgres.scanPgInterjectionRow: %w", err)
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

func scanPgInterjection(row *sql.Row) (*types.Interjection, error) {
	return scanPgInterjectionRow(row)
}

func scanPgInterjections(rows *sql.Rows) ([]*types.Interjection, error) {
	var results []*types.Interjection
	for rows.Next() {
		ij, err := scanPgInterjectionRow(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres.scanPgInterjections: %w", err)
		}
		results = append(results, ij)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.scanPgInterjections: %w", err)
	}
	return results, nil
}
