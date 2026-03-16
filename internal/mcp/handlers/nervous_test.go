package handlers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// --------------------------------------------------------------------------
// In-memory NervousRepo mock for handler tests
// --------------------------------------------------------------------------

type mockNervousRepo struct {
	events   []*types.DomainEvent
	handlers map[string]*types.EventHandler
	nextID   int
}

func newMockNervousRepo() *mockNervousRepo {
	return &mockNervousRepo{
		handlers: make(map[string]*types.EventHandler),
	}
}

func (m *mockNervousRepo) PersistEvent(_ context.Context, event *types.DomainEvent) error {
	m.events = append(m.events, event)
	return nil
}

func (m *mockNervousRepo) QueryEvents(_ context.Context, eventType string, since time.Time, limit int) ([]*types.DomainEvent, error) {
	var result []*types.DomainEvent
	for _, e := range m.events {
		if eventType != "" && string(e.EventType) != eventType {
			continue
		}
		if !since.IsZero() && e.CreatedAt.Before(since) {
			continue
		}
		result = append(result, e)
	}

	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *mockNervousRepo) PurgeExpired(_ context.Context) (int64, error) {
	now := time.Now()
	var kept []*types.DomainEvent
	var purged int64
	for _, e := range m.events {
		if e.ExpiresAt.Before(now) {
			purged++
		} else {
			kept = append(kept, e)
		}
	}
	m.events = kept
	return purged, nil
}

func (m *mockNervousRepo) GetEventStats(_ context.Context) (map[string]int64, error) {
	stats := make(map[string]int64)
	for _, e := range m.events {
		stats[string(e.EventType)]++
	}
	return stats, nil
}

func (m *mockNervousRepo) CreateHandler(_ context.Context, handler *types.EventHandler) (string, error) {
	m.nextID++
	handler.ID = "handler-" + string(rune('0'+m.nextID))
	handler.CreatedAt = time.Now()
	handler.UpdatedAt = handler.CreatedAt
	m.handlers[handler.ID] = handler
	return handler.ID, nil
}

func (m *mockNervousRepo) GetHandler(_ context.Context, id string) (*types.EventHandler, error) {
	h, ok := m.handlers[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return h, nil
}

func (m *mockNervousRepo) ListHandlers(_ context.Context) ([]*types.EventHandler, error) {
	var result []*types.EventHandler
	for _, h := range m.handlers {
		result = append(result, h)
	}
	return result, nil
}

func (m *mockNervousRepo) UpdateHandler(_ context.Context, handler *types.EventHandler) error {
	if _, ok := m.handlers[handler.ID]; !ok {
		return repo.ErrNotFound
	}
	handler.UpdatedAt = time.Now()
	m.handlers[handler.ID] = handler
	return nil
}

func (m *mockNervousRepo) DeleteHandler(_ context.Context, id string) error {
	if _, ok := m.handlers[id]; !ok {
		return repo.ErrNotFound
	}
	delete(m.handlers, id)
	return nil
}

// --------------------------------------------------------------------------
// Mock logger
// --------------------------------------------------------------------------

type testLogger struct{}

func (l *testLogger) Info(_ string, _ ...any)  {}
func (l *testLogger) Error(_ string, _ ...any) {}

// --------------------------------------------------------------------------
// Setup helpers
// --------------------------------------------------------------------------

func setupNervousHandler(t *testing.T) (*EventHandler, *nervous.EventBus, *nervous.RingBuffer, *mockNervousRepo) {
	t.Helper()
	bus := nervous.NewEventBus(64)
	rb := nervous.NewRingBuffer(100)
	mockRepo := newMockNervousRepo()
	handler := NewEventHandler(mockRepo, bus, rb, nil, &testLogger{})
	return handler, bus, rb, mockRepo
}

// nervousCallTool invokes a handler method with marshalled args.
func nervousCallTool(t *testing.T, fn func(context.Context, json.RawMessage) (*types.ToolResult, error), args any) *types.ToolResult {
	t.Helper()
	data, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	result, err := fn(context.Background(), data)
	if err != nil {
		t.Fatalf("tool error: %v", err)
	}
	return result
}

// nervousResultText extracts the text content from a ToolResult.
func nervousResultText(r *types.ToolResult) string {
	if len(r.Content) == 0 {
		return ""
	}
	return r.Content[0].Text
}

// --------------------------------------------------------------------------
// Tests
// --------------------------------------------------------------------------

func TestEventHandler_RegisterTools(t *testing.T) {
	h, _, _, _ := setupNervousHandler(t)
	registry := mcp.NewToolRegistry()
	h.RegisterTools(registry)

	// Consolidated handler registers a single "event" tool.
	if registry.ToolCount() != 1 {
		t.Errorf("expected 1 tool (event), got %d", registry.ToolCount())
	}

	schemas := registry.Schemas()
	if len(schemas) == 0 || schemas[0].Name != "event" {
		t.Errorf("expected tool named 'event', got %v", schemas)
	}
}

func TestNervousHandler_SubscribeEvents(t *testing.T) {
	h, bus, _, _ := setupNervousHandler(t)

	type args struct {
		SubscriberID string `json:"subscriber_id"`
		EventFilter  string `json:"event_filter"`
	}

	result := nervousCallTool(t, h.subscribeEvents, args{
		SubscriberID: "test-sub",
		EventFilter:  "pipeline.*",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", nervousResultText(result))
	}

	text := nervousResultText(result)
	if !strings.Contains(text, "test-sub") {
		t.Errorf("expected subscriber_id in result, got: %s", text)
	}
	if !strings.Contains(text, "pipeline.*") {
		t.Errorf("expected event_filter in result, got: %s", text)
	}

	// Verify the subscription exists on the bus.
	if bus.SubscriberCount() < 1 {
		t.Errorf("expected at least 1 subscriber, got %d", bus.SubscriberCount())
	}

	// Cleanup
	bus.Unsubscribe("test-sub")
}

func TestNervousHandler_SubscribeEvents_MissingFields(t *testing.T) {
	h, _, _, _ := setupNervousHandler(t)

	type args struct {
		SubscriberID string `json:"subscriber_id"`
		EventFilter  string `json:"event_filter"`
	}

	// Missing subscriber_id
	result := nervousCallTool(t, h.subscribeEvents, args{EventFilter: "pipeline.*"})
	if !result.IsError {
		t.Error("expected error for missing subscriber_id")
	}

	// Missing event_filter
	result = nervousCallTool(t, h.subscribeEvents, args{SubscriberID: "test"})
	if !result.IsError {
		t.Error("expected error for missing event_filter")
	}
}

func TestNervousHandler_GetEventStats(t *testing.T) {
	h, _, _, mockRepo := setupNervousHandler(t)

	// Empty stats
	result := nervousCallTool(t, h.getEventStats, struct{}{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", nervousResultText(result))
	}
	if nervousResultText(result) != "{}" {
		t.Errorf("expected empty JSON object '{}', got: %s", nervousResultText(result))
	}

	// Add some events
	mockRepo.events = append(mockRepo.events,
		&types.DomainEvent{EventType: "pipeline.start"},
		&types.DomainEvent{EventType: "pipeline.start"},
		&types.DomainEvent{EventType: "cron.fire"},
	)

	result = nervousCallTool(t, h.getEventStats, struct{}{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", nervousResultText(result))
	}

	text := nervousResultText(result)
	if !strings.Contains(text, "pipeline.start") {
		t.Errorf("expected pipeline.start in stats, got: %s", text)
	}
	if !strings.Contains(text, "cron.fire") {
		t.Errorf("expected cron.fire in stats, got: %s", text)
	}
}

func TestNervousHandler_GetEventStats_NoRepo(t *testing.T) {
	bus := nervous.NewEventBus(64)
	rb := nervous.NewRingBuffer(10)
	h := NewEventHandler(nil, bus, rb, nil, &testLogger{})

	result := nervousCallTool(t, h.getEventStats, struct{}{})
	if !result.IsError {
		t.Error("expected error when repo is nil")
	}
	if !strings.Contains(nervousResultText(result), "nervous repo not available") {
		t.Errorf("expected repo unavailable message, got: %s", nervousResultText(result))
	}
}

func TestNervousHandler_CreateEventHandler(t *testing.T) {
	h, _, _, _ := setupNervousHandler(t)

	type args struct {
		Name        string `json:"name"`
		EventFilter string `json:"event_filter"`
		Action      string `json:"action"`
	}

	result := nervousCallTool(t, h.createEventHandler, args{
		Name:        "log-pipelines",
		EventFilter: "pipeline.*",
		Action:      "log",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", nervousResultText(result))
	}

	text := nervousResultText(result)
	if !strings.Contains(text, "log-pipelines") {
		t.Errorf("expected handler name in result, got: %s", text)
	}
}

func TestNervousHandler_CreateEventHandler_MissingFields(t *testing.T) {
	h, _, _, _ := setupNervousHandler(t)

	type args struct {
		Name        string `json:"name"`
		EventFilter string `json:"event_filter"`
		Action      string `json:"action"`
	}

	// Missing name
	result := nervousCallTool(t, h.createEventHandler, args{EventFilter: "*", Action: "log"})
	if !result.IsError {
		t.Error("expected error for missing name")
	}

	// Missing event_filter
	result = nervousCallTool(t, h.createEventHandler, args{Name: "test", Action: "log"})
	if !result.IsError {
		t.Error("expected error for missing event_filter")
	}

	// Missing action
	result = nervousCallTool(t, h.createEventHandler, args{Name: "test", EventFilter: "*"})
	if !result.IsError {
		t.Error("expected error for missing action")
	}
}

func TestNervousHandler_ListEventHandlers(t *testing.T) {
	h, _, _, _ := setupNervousHandler(t)

	// Empty list
	result := nervousCallTool(t, h.listEventHandlers, struct{}{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", nervousResultText(result))
	}
	if nervousResultText(result) != "[]" {
		t.Errorf("expected empty JSON array '[]', got: %s", nervousResultText(result))
	}

	// Create a handler first
	type createArgs struct {
		Name        string `json:"name"`
		EventFilter string `json:"event_filter"`
		Action      string `json:"action"`
	}
	nervousCallTool(t, h.createEventHandler, createArgs{
		Name:        "test-handler",
		EventFilter: "pipeline.*",
		Action:      "log",
	})

	// List again
	result = nervousCallTool(t, h.listEventHandlers, struct{}{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", nervousResultText(result))
	}

	text := nervousResultText(result)
	if !strings.Contains(text, "test-handler") {
		t.Errorf("expected handler name in list, got: %s", text)
	}
}

func TestNervousHandler_QueryDomainEvents(t *testing.T) {
	h, _, _, mockRepo := setupNervousHandler(t)

	// Empty query
	result := nervousCallTool(t, h.queryDomainEvents, struct{}{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", nervousResultText(result))
	}
	if nervousResultText(result) != "[]" {
		t.Errorf("expected empty JSON array '[]', got: %s", nervousResultText(result))
	}

	// Add events
	now := time.Now()
	mockRepo.events = append(mockRepo.events,
		&types.DomainEvent{
			ID:        "e1",
			EventType: "pipeline.start",
			Source:    "test",
			CreatedAt: now,
			ExpiresAt: now.Add(7 * 24 * time.Hour),
		},
		&types.DomainEvent{
			ID:        "e2",
			EventType: "cron.fire",
			Source:    "test",
			CreatedAt: now,
			ExpiresAt: now.Add(7 * 24 * time.Hour),
		},
	)

	// Query all
	result = nervousCallTool(t, h.queryDomainEvents, struct{}{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", nervousResultText(result))
	}

	text := nervousResultText(result)
	if !strings.Contains(text, "pipeline.start") {
		t.Errorf("expected pipeline.start in results, got: %s", text)
	}

	// Query with type filter
	type queryArgs struct {
		EventType string `json:"event_type"`
		Limit     int    `json:"limit"`
	}
	result = nervousCallTool(t, h.queryDomainEvents, queryArgs{EventType: "cron.fire", Limit: 10})
	if result.IsError {
		t.Fatalf("unexpected error: %s", nervousResultText(result))
	}

	text = nervousResultText(result)
	if !strings.Contains(text, "cron.fire") {
		t.Errorf("expected cron.fire in filtered results, got: %s", text)
	}
}

func TestNervousHandler_PromoteEvent(t *testing.T) {
	h, bus, _, _ := setupNervousHandler(t)

	// Subscribe to capture the promoted event.
	sub := bus.Subscribe("promote-test", nil)
	defer bus.Unsubscribe("promote-test")

	type args struct {
		EventType string `json:"event_type"`
		Source    string `json:"source"`
		Scope    string `json:"scope"`
		Payload  string `json:"payload"`
	}

	result := nervousCallTool(t, h.promoteEvent, args{
		EventType: "test.ping",
		Source:    "unit-test",
		Scope:    "diagnostic",
		Payload:   `{"msg":"hello"}`,
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", nervousResultText(result))
	}

	text := nervousResultText(result)
	if !strings.Contains(text, "test.ping") {
		t.Errorf("expected event type in result, got: %s", text)
	}

	// Verify event was published on the bus.
	select {
	case e := <-sub.Ch:
		if string(e.Type) != "test.ping" {
			t.Errorf("event type = %s, want test.ping", e.Type)
		}
		if e.Source != "unit-test" {
			t.Errorf("source = %s, want unit-test", e.Source)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for promoted event on bus")
	}
}

func TestNervousHandler_PromoteEvent_MissingType(t *testing.T) {
	h, _, _, _ := setupNervousHandler(t)

	type args struct {
		EventType string `json:"event_type"`
	}

	result := nervousCallTool(t, h.promoteEvent, args{})
	if !result.IsError {
		t.Error("expected error for missing event_type")
	}
}

func TestNervousHandler_TestSensor(t *testing.T) {
	h, _, _, _ := setupNervousHandler(t)

	type args struct {
		TraceID string `json:"trace_id"`
	}

	result := nervousCallTool(t, h.testSensor, args{TraceID: "test-trace-123"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", nervousResultText(result))
	}

	text := nervousResultText(result)
	if !strings.Contains(text, "test-trace-123") {
		t.Errorf("expected trace_id in result, got: %s", text)
	}
	if !strings.Contains(text, "sequence_id") {
		t.Errorf("expected sequence_id in result, got: %s", text)
	}
}

func TestNervousHandler_TestSensor_AutoTraceID(t *testing.T) {
	h, _, _, _ := setupNervousHandler(t)

	// No trace_id provided — should auto-generate.
	result := nervousCallTool(t, h.testSensor, struct{}{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", nervousResultText(result))
	}

	text := nervousResultText(result)
	if !strings.Contains(text, "test-") {
		t.Errorf("expected auto-generated trace_id starting with 'test-', got: %s", text)
	}
}
