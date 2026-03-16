package repo

import (
	"context"
	"time"

	"github.com/hyperax/hyperax/pkg/types"
)

// NervousRepo provides persistence for the Enhanced Nervous System.
// It handles domain event storage/query and declarative event handler CRUD.
type NervousRepo interface {
	// --- Domain events ---

	// PersistEvent stores a single domain event.
	PersistEvent(ctx context.Context, event *types.DomainEvent) error

	// QueryEvents returns persisted events matching the given type (or all if
	// eventType is empty) created after 'since', ordered by created_at DESC.
	// Limit controls the maximum number of results (0 = no limit).
	QueryEvents(ctx context.Context, eventType string, since time.Time, limit int) ([]*types.DomainEvent, error)

	// PurgeExpired deletes all domain events whose expires_at is in the past.
	// Returns the number of rows deleted.
	PurgeExpired(ctx context.Context) (int64, error)

	// GetEventStats returns a map of event_type -> count for all persisted
	// domain events.
	GetEventStats(ctx context.Context) (map[string]int64, error)

	// --- Event handlers ---

	// CreateHandler inserts a new event handler and returns its generated ID.
	CreateHandler(ctx context.Context, handler *types.EventHandler) (string, error)

	// GetHandler retrieves an event handler by ID.
	GetHandler(ctx context.Context, id string) (*types.EventHandler, error)

	// ListHandlers returns all configured event handlers.
	ListHandlers(ctx context.Context) ([]*types.EventHandler, error)

	// UpdateHandler updates an existing event handler.
	UpdateHandler(ctx context.Context, handler *types.EventHandler) error

	// DeleteHandler deletes an event handler by ID.
	DeleteHandler(ctx context.Context, id string) error
}
