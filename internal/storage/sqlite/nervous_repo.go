package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/pkg/types"
)

// NervousRepo implements repo.NervousRepo for SQLite.
// It operates on the domain_events table (created in 001_core.up.sql)
// and the event_handlers table (created in 006_nervous.up.sql).
type NervousRepo struct {
	db *sql.DB
}

// --------------------------------------------------------------------------
// Domain events
// --------------------------------------------------------------------------

// PersistEvent stores a single domain event in the domain_events table.
// The promoted_by column (required by the 001 schema) defaults to the
// event's Source field.
func (r *NervousRepo) PersistEvent(ctx context.Context, event *types.DomainEvent) error {
	if event.ID == "" {
		event.ID = uuid.New().String()
	}

	// The 001_core schema has: id, event_type, source, payload, promoted_by,
	// scope, sequence_id, trace_id, created_at, expires_at.
	// promoted_by is NOT NULL -- we default it to Source.
	promotedBy := event.Source
	if promotedBy == "" {
		promotedBy = "nervous-persister"
	}

	// Handle nullable expires_at.
	var expiresAt *string
	if !event.ExpiresAt.IsZero() {
		s := event.ExpiresAt.Format(sqliteTimeFormat)
		expiresAt = &s
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO domain_events
		    (id, event_type, source, payload, promoted_by, scope, sequence_id, trace_id, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID,
		string(event.EventType),
		event.Source,
		event.Payload,
		promotedBy,
		event.Scope,
		event.SequenceID,
		event.TraceID,
		event.CreatedAt.Format(sqliteTimeFormat),
		expiresAt,
	)
	if err != nil {
		return fmt.Errorf("sqlite.NervousRepo.PersistEvent: %w", err)
	}

	return nil
}

// QueryEvents returns persisted events matching the given type (or all if
// eventType is empty) created after 'since', ordered by created_at DESC.
func (r *NervousRepo) QueryEvents(ctx context.Context, eventType string, since time.Time, limit int) ([]*types.DomainEvent, error) {
	query := `SELECT id, event_type, COALESCE(source, ''), COALESCE(scope, ''),
	                 COALESCE(payload, ''), COALESCE(trace_id, ''), sequence_id,
	                 created_at, COALESCE(expires_at, '')
	          FROM domain_events WHERE 1=1`
	args := make([]any, 0, 3)

	if eventType != "" {
		query += " AND event_type = ?"
		args = append(args, eventType)
	}

	if !since.IsZero() {
		query += " AND created_at >= ?"
		args = append(args, since.Format(sqliteTimeFormat))
	}

	query += " ORDER BY created_at DESC"

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite.NervousRepo.QueryEvents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanDomainEvents(rows)
}

// PurgeExpired deletes all domain events whose expires_at is in the past.
func (r *NervousRepo) PurgeExpired(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		"DELETE FROM domain_events WHERE expires_at IS NOT NULL AND expires_at < datetime('now')")
	if err != nil {
		return 0, fmt.Errorf("sqlite.NervousRepo.PurgeExpired: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("sqlite.NervousRepo.PurgeExpired: %w", err)
	}

	return n, nil
}

// GetEventStats returns a map of event_type -> count.
func (r *NervousRepo) GetEventStats(ctx context.Context) (map[string]int64, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT event_type, COUNT(*) FROM domain_events GROUP BY event_type ORDER BY COUNT(*) DESC")
	if err != nil {
		return nil, fmt.Errorf("sqlite.NervousRepo.GetEventStats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	stats := make(map[string]int64)
	for rows.Next() {
		var eventType string
		var count int64
		if err := rows.Scan(&eventType, &count); err != nil {
			return nil, fmt.Errorf("sqlite.NervousRepo.GetEventStats: %w", err)
		}
		stats[eventType] = count
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.NervousRepo.GetEventStats: %w", err)
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

	enabled := 0
	if handler.Enabled {
		enabled = 1
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO event_handlers (id, name, event_filter, action, action_payload, enabled)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		handler.ID, handler.Name, handler.EventFilter,
		handler.Action, handler.ActionPayload, enabled,
	)
	if err != nil {
		return "", fmt.Errorf("sqlite.NervousRepo.CreateHandler: %w", err)
	}

	return handler.ID, nil
}

// GetHandler retrieves an event handler by ID.
func (r *NervousRepo) GetHandler(ctx context.Context, id string) (*types.EventHandler, error) {
	h := &types.EventHandler{}
	var enabled int
	var createdAt, updatedAt string

	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, event_filter, action, COALESCE(action_payload, '{}'),
		        enabled, created_at, updated_at
		 FROM event_handlers WHERE id = ?`, id,
	).Scan(
		&h.ID, &h.Name, &h.EventFilter, &h.Action, &h.ActionPayload,
		&enabled, &createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("event handler %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite.NervousRepo.GetHandler: %w", err)
	}

	h.Enabled = enabled == 1
	if h.CreatedAt, err = parseSQLiteTime(createdAt, "sqlite.NervousRepo.GetHandler"); err != nil {
		return nil, err
	}
	if h.UpdatedAt, err = parseSQLiteTime(updatedAt, "sqlite.NervousRepo.GetHandler"); err != nil {
		return nil, err
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
		return nil, fmt.Errorf("sqlite.NervousRepo.ListHandlers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var handlers []*types.EventHandler
	for rows.Next() {
		h := &types.EventHandler{}
		var enabled int
		var createdAt, updatedAt string

		if err := rows.Scan(
			&h.ID, &h.Name, &h.EventFilter, &h.Action, &h.ActionPayload,
			&enabled, &createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("sqlite.NervousRepo.ListHandlers: %w", err)
		}

		h.Enabled = enabled == 1
		var parseErr error
		if h.CreatedAt, parseErr = parseSQLiteTime(createdAt, "sqlite.NervousRepo.ListHandlers"); parseErr != nil {
			return nil, parseErr
		}
		if h.UpdatedAt, parseErr = parseSQLiteTime(updatedAt, "sqlite.NervousRepo.ListHandlers"); parseErr != nil {
			return nil, parseErr
		}
		handlers = append(handlers, h)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.NervousRepo.ListHandlers: %w", err)
	}
	return handlers, nil
}

// UpdateHandler updates an existing event handler by its ID.
func (r *NervousRepo) UpdateHandler(ctx context.Context, handler *types.EventHandler) error {
	enabled := 0
	if handler.Enabled {
		enabled = 1
	}

	res, err := r.db.ExecContext(ctx,
		`UPDATE event_handlers SET
		    name = ?, event_filter = ?, action = ?, action_payload = ?,
		    enabled = ?, updated_at = datetime('now')
		 WHERE id = ?`,
		handler.Name, handler.EventFilter, handler.Action,
		handler.ActionPayload, enabled, handler.ID,
	)
	if err != nil {
		return fmt.Errorf("sqlite.NervousRepo.UpdateHandler: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.NervousRepo.UpdateHandler: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("event handler %q not found", handler.ID)
	}

	return nil
}

// DeleteHandler deletes an event handler by ID.
func (r *NervousRepo) DeleteHandler(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM event_handlers WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("sqlite.NervousRepo.DeleteHandler: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.NervousRepo.DeleteHandler: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("event handler %q not found", id)
	}

	return nil
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// scanDomainEvents scans rows from a domain_events query into a slice.
// Expects columns: id, event_type, source, scope, payload, trace_id,
// sequence_id, created_at, expires_at.
func scanDomainEvents(rows *sql.Rows) ([]*types.DomainEvent, error) {
	var events []*types.DomainEvent
	for rows.Next() {
		e := &types.DomainEvent{}
		var eventType, createdAt, expiresAt string

		if err := rows.Scan(
			&e.ID, &eventType, &e.Source, &e.Scope, &e.Payload,
			&e.TraceID, &e.SequenceID, &createdAt, &expiresAt,
		); err != nil {
			return nil, fmt.Errorf("sqlite.scanDomainEvents: %w", err)
		}

		e.EventType = types.EventType(eventType)
		var parseErr error
		if e.CreatedAt, parseErr = parseSQLiteTime(createdAt, "sqlite.scanDomainEvents"); parseErr != nil {
			return nil, parseErr
		}
		if expiresAt != "" {
			if e.ExpiresAt, parseErr = parseSQLiteTime(expiresAt, "sqlite.scanDomainEvents.expiresAt"); parseErr != nil {
				return nil, parseErr
			}
		}
		events = append(events, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.scanDomainEvents: %w", err)
	}
	return events, nil
}
