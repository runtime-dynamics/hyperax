package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/pkg/types"
)

// NervousRepo implements repo.NervousRepo for PostgreSQL.
type NervousRepo struct {
	db *sql.DB
}

// --------------------------------------------------------------------------
// Domain events
// --------------------------------------------------------------------------

// PersistEvent stores a single domain event in the domain_events table.
func (r *NervousRepo) PersistEvent(ctx context.Context, event *types.DomainEvent) error {
	if event.ID == "" {
		event.ID = uuid.New().String()
	}

	promotedBy := event.Source
	if promotedBy == "" {
		promotedBy = "nervous-persister"
	}

	var expiresAt *time.Time
	if !event.ExpiresAt.IsZero() {
		t := event.ExpiresAt
		expiresAt = &t
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO domain_events
		    (id, event_type, source, payload, promoted_by, scope, sequence_id, trace_id, created_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		event.ID,
		string(event.EventType),
		event.Source,
		event.Payload,
		promotedBy,
		event.Scope,
		event.SequenceID,
		event.TraceID,
		event.CreatedAt,
		expiresAt,
	)
	if err != nil {
		return fmt.Errorf("postgres.NervousRepo.PersistEvent: %w", err)
	}
	return nil
}

// QueryEvents returns persisted events matching the given type (or all if
// eventType is empty) created after 'since', ordered by created_at DESC.
func (r *NervousRepo) QueryEvents(ctx context.Context, eventType string, since time.Time, limit int) ([]*types.DomainEvent, error) {
	query := `SELECT id, event_type, COALESCE(source, ''), COALESCE(scope, ''),
	                 COALESCE(payload, ''), COALESCE(trace_id, ''), sequence_id,
	                 created_at, expires_at
	          FROM domain_events WHERE 1=1`
	args := make([]any, 0, 3)
	paramIdx := 1

	if eventType != "" {
		query += fmt.Sprintf(" AND event_type = $%d", paramIdx)
		args = append(args, eventType)
		paramIdx++
	}

	if !since.IsZero() {
		query += fmt.Sprintf(" AND created_at >= $%d", paramIdx)
		args = append(args, since)
		paramIdx++
	}

	query += " ORDER BY created_at DESC"

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", paramIdx)
		args = append(args, limit)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres.NervousRepo.QueryEvents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanPgDomainEvents(rows)
}

// PurgeExpired deletes all domain events whose expires_at is in the past.
func (r *NervousRepo) PurgeExpired(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		"DELETE FROM domain_events WHERE expires_at IS NOT NULL AND expires_at < NOW()")
	if err != nil {
		return 0, fmt.Errorf("postgres.NervousRepo.PurgeExpired: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("postgres.NervousRepo.PurgeExpired: %w", err)
	}
	return n, nil
}

// GetEventStats returns a map of event_type -> count.
func (r *NervousRepo) GetEventStats(ctx context.Context) (map[string]int64, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT event_type, COUNT(*) FROM domain_events GROUP BY event_type ORDER BY COUNT(*) DESC")
	if err != nil {
		return nil, fmt.Errorf("postgres.NervousRepo.GetEventStats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	stats := make(map[string]int64)
	for rows.Next() {
		var eventType string
		var count int64
		if err := rows.Scan(&eventType, &count); err != nil {
			return nil, fmt.Errorf("postgres.NervousRepo.GetEventStats: %w", err)
		}
		stats[eventType] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.NervousRepo.GetEventStats: %w", err)
	}
	return stats, nil
}

// --------------------------------------------------------------------------
// Event handlers
// --------------------------------------------------------------------------

// CreateHandler inserts a new event handler and returns its generated ID.
func (r *NervousRepo) CreateHandler(ctx context.Context, handler *types.EventHandler) (string, error) {
	if handler.ID == "" {
		handler.ID = uuid.New().String()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO event_handlers (id, name, event_filter, action, action_payload, enabled)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		handler.ID, handler.Name, handler.EventFilter,
		handler.Action, handler.ActionPayload, handler.Enabled,
	)
	if err != nil {
		return "", fmt.Errorf("postgres.NervousRepo.CreateHandler: %w", err)
	}
	return handler.ID, nil
}

// GetHandler retrieves an event handler by ID.
func (r *NervousRepo) GetHandler(ctx context.Context, id string) (*types.EventHandler, error) {
	h := &types.EventHandler{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, event_filter, action, COALESCE(action_payload, '{}'),
		        enabled, created_at, updated_at
		 FROM event_handlers WHERE id = $1`, id,
	).Scan(
		&h.ID, &h.Name, &h.EventFilter, &h.Action, &h.ActionPayload,
		&h.Enabled, &h.CreatedAt, &h.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("event handler %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres.NervousRepo.GetHandler: %w", err)
	}
	return h, nil
}

// ListHandlers returns all configured event handlers ordered by name.
func (r *NervousRepo) ListHandlers(ctx context.Context) ([]*types.EventHandler, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, event_filter, action, COALESCE(action_payload, '{}'),
		        enabled, created_at, updated_at
		 FROM event_handlers ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("postgres.NervousRepo.ListHandlers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var handlers []*types.EventHandler
	for rows.Next() {
		h := &types.EventHandler{}
		if err := rows.Scan(
			&h.ID, &h.Name, &h.EventFilter, &h.Action, &h.ActionPayload,
			&h.Enabled, &h.CreatedAt, &h.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("postgres.NervousRepo.ListHandlers: %w", err)
		}
		handlers = append(handlers, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.NervousRepo.ListHandlers: %w", err)
	}
	return handlers, nil
}

// UpdateHandler updates an existing event handler by its ID.
func (r *NervousRepo) UpdateHandler(ctx context.Context, handler *types.EventHandler) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE event_handlers SET
		    name = $1, event_filter = $2, action = $3, action_payload = $4,
		    enabled = $5, updated_at = NOW()
		 WHERE id = $6`,
		handler.Name, handler.EventFilter, handler.Action,
		handler.ActionPayload, handler.Enabled, handler.ID,
	)
	if err != nil {
		return fmt.Errorf("postgres.NervousRepo.UpdateHandler: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.NervousRepo.UpdateHandler: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("event handler %q not found", handler.ID)
	}
	return nil
}

// DeleteHandler deletes an event handler by ID.
func (r *NervousRepo) DeleteHandler(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM event_handlers WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("postgres.NervousRepo.DeleteHandler: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.NervousRepo.DeleteHandler: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("event handler %q not found", id)
	}
	return nil
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

func scanPgDomainEvents(rows *sql.Rows) ([]*types.DomainEvent, error) {
	var events []*types.DomainEvent
	for rows.Next() {
		e := &types.DomainEvent{}
		var eventType string
		var expiresAt sql.NullTime

		if err := rows.Scan(
			&e.ID, &eventType, &e.Source, &e.Scope, &e.Payload,
			&e.TraceID, &e.SequenceID, &e.CreatedAt, &expiresAt,
		); err != nil {
			return nil, fmt.Errorf("postgres.scanPgDomainEvents: %w", err)
		}

		e.EventType = types.EventType(eventType)
		if expiresAt.Valid {
			e.ExpiresAt = expiresAt.Time
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.scanPgDomainEvents: %w", err)
	}
	return events, nil
}
