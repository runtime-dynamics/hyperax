package nervous

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// mockNervousRepo is a minimal mock for testing the executor.
type mockNervousRepo struct {
	mu       sync.Mutex
	handlers []*types.EventHandler
	events   []*types.DomainEvent
}

func (m *mockNervousRepo) PersistEvent(_ context.Context, event *types.DomainEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	return nil
}

func (m *mockNervousRepo) QueryEvents(_ context.Context, _ string, _ time.Time, _ int) ([]*types.DomainEvent, error) {
	return nil, nil
}

func (m *mockNervousRepo) PurgeExpired(_ context.Context) (int64, error) {
	return 0, nil
}

func (m *mockNervousRepo) GetEventStats(_ context.Context) (map[string]int64, error) {
	return nil, nil
}

func (m *mockNervousRepo) CreateHandler(_ context.Context, h *types.EventHandler) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h.ID = "mock-id"
	m.handlers = append(m.handlers, h)
	return h.ID, nil
}

func (m *mockNervousRepo) GetHandler(_ context.Context, id string) (*types.EventHandler, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, h := range m.handlers {
		if h.ID == id {
			return h, nil
		}
	}
	return nil, repo.ErrNotFound
}

func (m *mockNervousRepo) ListHandlers(_ context.Context) ([]*types.EventHandler, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*types.EventHandler, len(m.handlers))
	copy(result, m.handlers)
	return result, nil
}

func (m *mockNervousRepo) UpdateHandler(_ context.Context, _ *types.EventHandler) error {
	return nil
}

func (m *mockNervousRepo) DeleteHandler(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, h := range m.handlers {
		if h.ID == id {
			m.handlers = append(m.handlers[:i], m.handlers[i+1:]...)
			return nil
		}
	}
	return repo.ErrNotFound
}

func (m *mockNervousRepo) persistedEvents() []*types.DomainEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*types.DomainEvent, len(m.events))
	copy(result, m.events)
	return result
}

// discardLogger returns a *slog.Logger that discards all output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestExecutor_DispatchLogAction(t *testing.T) {
	mockRepo := &mockNervousRepo{
		handlers: []*types.EventHandler{
			{
				ID:          "h1",
				Name:        "log-pipeline",
				EventFilter: "pipeline.*",
				Action:      "log",
				Enabled:     true,
			},
		},
	}

	bus := NewEventBus(64)
	executor := NewExecutor(mockRepo, bus, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go executor.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Publish a matching event.
	bus.Publish(NewEvent(types.EventPipelineComplete, "test", "global", map[string]string{"pipeline": "build"}))
	time.Sleep(100 * time.Millisecond)

	events := mockRepo.persistedEvents()
	if len(events) == 0 {
		t.Fatal("expected at least one persisted event from log action")
	}

	found := false
	for _, e := range events {
		if e.EventType == types.EventPipelineComplete {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected pipeline.complete event to be persisted")
	}
}

func TestExecutor_NoMatchNoAction(t *testing.T) {
	mockRepo := &mockNervousRepo{
		handlers: []*types.EventHandler{
			{
				ID:          "h1",
				Name:        "log-pipeline",
				EventFilter: "pipeline.*",
				Action:      "log",
				Enabled:     true,
			},
		},
	}

	bus := NewEventBus(64)
	executor := NewExecutor(mockRepo, bus, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go executor.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Publish a non-matching event.
	bus.Publish(NewEvent(types.EventCronFire, "test", "global", nil))
	time.Sleep(100 * time.Millisecond)

	events := mockRepo.persistedEvents()
	for _, e := range events {
		if e.EventType == types.EventCronFire {
			t.Error("cron.fire should not have been persisted by pipeline.* handler")
		}
	}
}

func TestExecutor_DisabledHandlerSkipped(t *testing.T) {
	mockRepo := &mockNervousRepo{
		handlers: []*types.EventHandler{
			{
				ID:          "h1",
				Name:        "disabled-handler",
				EventFilter: "*",
				Action:      "log",
				Enabled:     false,
			},
		},
	}

	bus := NewEventBus(64)
	executor := NewExecutor(mockRepo, bus, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go executor.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	bus.Publish(NewEvent(types.EventPipelineStart, "test", "global", nil))
	time.Sleep(100 * time.Millisecond)

	events := mockRepo.persistedEvents()
	for _, e := range events {
		if e.EventType == types.EventPipelineStart {
			t.Error("disabled handler should not persist events")
		}
	}
}

func TestExecutor_ReloadHandlers(t *testing.T) {
	mockRepo := &mockNervousRepo{
		handlers: []*types.EventHandler{},
	}

	bus := NewEventBus(64)
	executor := NewExecutor(mockRepo, bus, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go executor.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Publish event — no handlers, nothing should be persisted.
	bus.Publish(NewEvent(types.EventCronFire, "test", "global", nil))
	time.Sleep(50 * time.Millisecond)

	if len(mockRepo.persistedEvents()) != 0 {
		t.Fatal("expected no persisted events before adding handler")
	}

	// Add a handler and reload.
	mockRepo.mu.Lock()
	mockRepo.handlers = append(mockRepo.handlers, &types.EventHandler{
		ID:          "h2",
		Name:        "catch-all",
		EventFilter: "*",
		Action:      "log",
		Enabled:     true,
	})
	mockRepo.mu.Unlock()

	executor.ReloadHandlers()
	time.Sleep(50 * time.Millisecond)

	bus.Publish(NewEvent(types.EventCronFire, "test", "global", nil))
	time.Sleep(100 * time.Millisecond)

	events := mockRepo.persistedEvents()
	if len(events) == 0 {
		t.Error("expected persisted event after handler reload")
	}
}

func TestExecutor_PromoteAction(t *testing.T) {
	mockRepo := &mockNervousRepo{
		handlers: []*types.EventHandler{
			{
				ID:          "h1",
				Name:        "promote-all",
				EventFilter: "test.*",
				Action:      "promote",
				Enabled:     true,
			},
		},
	}

	bus := NewEventBus(64)
	executor := NewExecutor(mockRepo, bus, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go executor.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	bus.Publish(NewEvent(types.EventType("test.ping"), "test", "global", nil))
	time.Sleep(100 * time.Millisecond)

	events := mockRepo.persistedEvents()
	found := false
	for _, e := range events {
		if e.EventType == types.EventType("test.ping") {
			found = true
		}
	}
	if !found {
		t.Error("expected test.ping to be persisted by promote action")
	}
}

func TestExecutor_RouteActionNoOp(t *testing.T) {
	mockRepo := &mockNervousRepo{
		handlers: []*types.EventHandler{
			{
				ID:          "h1",
				Name:        "route-handler",
				EventFilter: "test.*",
				Action:      "route",
				Enabled:     true,
			},
		},
	}

	bus := NewEventBus(64)
	executor := NewExecutor(mockRepo, bus, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go executor.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	bus.Publish(NewEvent(types.EventType("test.route"), "test", "global", nil))
	time.Sleep(100 * time.Millisecond)

	// Route is no-op — nothing persisted.
	events := mockRepo.persistedEvents()
	for _, e := range events {
		if e.EventType == types.EventType("test.route") {
			t.Error("route action should not persist events")
		}
	}
}

func TestExecutor_Stop(t *testing.T) {
	mockRepo := &mockNervousRepo{handlers: []*types.EventHandler{}}
	bus := NewEventBus(64)
	executor := NewExecutor(mockRepo, bus, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		executor.Start(ctx)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	executor.Stop()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("executor did not stop within timeout")
	}
}

func TestExecutor_MultipleHandlersMatch(t *testing.T) {
	mockRepo := &mockNervousRepo{
		handlers: []*types.EventHandler{
			{
				ID:          "h1",
				Name:        "handler-a",
				EventFilter: "pipeline.*",
				Action:      "log",
				Enabled:     true,
			},
			{
				ID:          "h2",
				Name:        "handler-b",
				EventFilter: "*",
				Action:      "promote",
				Enabled:     true,
			},
		},
	}

	bus := NewEventBus(64)
	executor := NewExecutor(mockRepo, bus, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go executor.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	bus.Publish(NewEvent(types.EventPipelineStart, "test", "global", nil))
	time.Sleep(100 * time.Millisecond)

	events := mockRepo.persistedEvents()
	count := 0
	for _, e := range events {
		if e.EventType == types.EventPipelineStart {
			count++
		}
	}
	if count < 2 {
		t.Errorf("expected at least 2 persisted events from two matching handlers, got %d", count)
	}
}
