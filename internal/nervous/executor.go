package nervous

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

const (
	// webhookTimeout is the HTTP timeout for webhook action calls.
	webhookTimeout = 10 * time.Second
)

// Executor loads declarative EventHandlers from the NervousRepo, subscribes
// to the EventBus with a catch-all filter, and executes handler actions when
// incoming events match the handler's EventFilter glob pattern.
type Executor struct {
	repo   repo.NervousRepo
	bus    *EventBus
	logger *slog.Logger

	mu       sync.RWMutex
	handlers []*types.EventHandler

	cancel context.CancelFunc
}

// NewExecutor creates an Executor wired to the nervous repo, event bus, and logger.
func NewExecutor(repo repo.NervousRepo, bus *EventBus, logger *slog.Logger) *Executor {
	return &Executor{
		repo:   repo,
		bus:    bus,
		logger: logger,
	}
}

// Start loads active handlers from the repo, subscribes to the event bus with
// a catch-all filter, and dispatches matching events to handler actions.
// Blocks until ctx is cancelled.
func (e *Executor) Start(ctx context.Context) {
	ctx, e.cancel = context.WithCancel(ctx)

	if err := e.loadHandlers(ctx); err != nil {
		e.logger.Error("executor: initial handler load failed", "error", err)
	}

	sub := e.bus.Subscribe("nervous-executor", nil)

	for {
		select {
		case <-ctx.Done():
			e.bus.Unsubscribe("nervous-executor")
			return
		case event, ok := <-sub.Ch:
			if !ok {
				return
			}
			e.dispatch(ctx, event)
		}
	}
}

// ReloadHandlers reloads handlers from the repo. Call this after CRUD
// operations on event handlers to pick up changes without restarting.
func (e *Executor) ReloadHandlers() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := e.loadHandlers(ctx); err != nil {
		e.logger.Error("executor: handler reload failed", "error", err)
	} else {
		e.logger.Info("executor: handlers reloaded", "count", len(e.handlers))
	}
}

// Stop cancels the executor's context, causing Start to return.
func (e *Executor) Stop() {
	if e.cancel != nil {
		e.cancel()
	}
}

// loadHandlers fetches all handlers from the repo and caches enabled ones.
func (e *Executor) loadHandlers(ctx context.Context) error {
	if e.repo == nil {
		return fmt.Errorf("nervous.Executor.loadHandlers: nervous repo not available")
	}

	all, err := e.repo.ListHandlers(ctx)
	if err != nil {
		return fmt.Errorf("nervous.Executor.loadHandlers: %w", err)
	}

	var enabled []*types.EventHandler
	for _, h := range all {
		if h.Enabled {
			enabled = append(enabled, h)
		}
	}

	e.mu.Lock()
	e.handlers = enabled
	e.mu.Unlock()

	return nil
}

// dispatch checks each loaded handler against the incoming event and
// executes matching actions.
func (e *Executor) dispatch(ctx context.Context, event types.NervousEvent) {
	e.mu.RLock()
	handlers := e.handlers
	e.mu.RUnlock()

	for _, h := range handlers {
		if !MatchEventType(h.EventFilter, event.Type) {
			continue
		}
		e.executeAction(ctx, h, event)
	}
}

// executeAction runs the handler's configured action for the matched event.
func (e *Executor) executeAction(ctx context.Context, handler *types.EventHandler, event types.NervousEvent) {
	switch handler.Action {
	case "promote", "log":
		e.actionPersist(ctx, handler, event)
	case "webhook":
		e.actionWebhook(ctx, handler, event)
	case "route":
		// Future: route event payload as a message. For now, just log.
		e.logger.Info("executor: route action (no-op)",
			"handler", handler.Name,
			"event_type", event.Type,
		)
	default:
		e.logger.Warn("executor: unknown action type",
			"handler", handler.Name,
			"action", handler.Action,
		)
	}
}

// actionPersist persists the event to the domain event stream.
func (e *Executor) actionPersist(ctx context.Context, handler *types.EventHandler, event types.NervousEvent) {
	if e.repo == nil {
		e.logger.Error("executor: cannot persist, repo unavailable", "handler", handler.Name)
		return
	}

	domain := &types.DomainEvent{
		ID:         uuid.New().String(),
		EventType:  event.Type,
		Source:     event.Source,
		Scope:      event.Scope,
		Payload:    string(event.Payload),
		TraceID:    event.TraceID,
		SequenceID: event.SequenceID,
		CreatedAt:  event.Timestamp,
		ExpiresAt:  event.Timestamp.Add(7 * 24 * time.Hour),
	}
	if err := e.repo.PersistEvent(ctx, domain); err != nil {
		e.logger.Error("executor: persist action failed",
			"handler", handler.Name,
			"event_type", event.Type,
			"error", err,
		)
		return
	}

	e.logger.Info("executor: event persisted by handler",
		"handler", handler.Name,
		"event_type", event.Type,
		"sequence_id", event.SequenceID,
	)
}

// actionWebhook POSTs the event JSON to the URL specified in ActionPayload.
func (e *Executor) actionWebhook(ctx context.Context, handler *types.EventHandler, event types.NervousEvent) {
	if handler.ActionPayload == "" {
		e.logger.Error("executor: webhook action missing URL in action_payload", "handler", handler.Name)
		return
	}

	// Parse the action payload to extract the URL.
	var config struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(handler.ActionPayload), &config); err != nil {
		// If it's not JSON, treat the entire payload as a URL.
		config.URL = handler.ActionPayload
	}
	if config.URL == "" {
		e.logger.Error("executor: webhook action has empty URL", "handler", handler.Name)
		return
	}

	body, err := json.Marshal(event)
	if err != nil {
		e.logger.Error("executor: webhook marshal failed", "handler", handler.Name, "error", err)
		return
	}

	reqCtx, cancel := context.WithTimeout(ctx, webhookTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, config.URL, bytes.NewReader(body))
	if err != nil {
		e.logger.Error("executor: webhook request creation failed", "handler", handler.Name, "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.logger.Error("executor: webhook call failed",
			"handler", handler.Name,
			"url", config.URL,
			"error", err,
		)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		e.logger.Warn("executor: webhook returned error status",
			"handler", handler.Name,
			"url", config.URL,
			"status", resp.StatusCode,
		)
		return
	}

	e.logger.Info("executor: webhook delivered",
		"handler", handler.Name,
		"url", config.URL,
		"status", resp.StatusCode,
	)
}
