package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

// ---------------------------------------------------------------------------
// In-memory mock for repo.TelemetryRepo
// ---------------------------------------------------------------------------

type mockTelemetryRepo struct {
	mu       sync.Mutex
	sessions map[string]*types.Session
	metrics  map[string][]*types.ToolCallMetric
	alerts   map[string]*types.Alert
	nextID   int
}

func newMockTelemetryRepo() *mockTelemetryRepo {
	return &mockTelemetryRepo{
		sessions: make(map[string]*types.Session),
		metrics:  make(map[string][]*types.ToolCallMetric),
		alerts:   make(map[string]*types.Alert),
	}
}

func (m *mockTelemetryRepo) CreateSession(_ context.Context, session *types.Session) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	if session.ID == "" {
		session.ID = fmt.Sprintf("sess-mock-%d", m.nextID)
	}
	s := *session
	s.StartedAt = time.Now()
	s.CreatedAt = time.Now()
	m.sessions[s.ID] = &s
	return s.ID, nil
}

func (m *mockTelemetryRepo) GetSession(_ context.Context, id string) (*types.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("session %q not found", id)
	}
	return s, nil
}

func (m *mockTelemetryRepo) ListSessions(_ context.Context, agentID string, limit int) ([]*types.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*types.Session
	for _, s := range m.sessions {
		if agentID != "" && s.AgentID != agentID {
			continue
		}
		out = append(out, s)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *mockTelemetryRepo) EndSession(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok || s.Status != "active" {
		return fmt.Errorf("session %q not found or already ended", id)
	}
	now := time.Now()
	s.EndedAt = &now
	s.Status = "completed"
	return nil
}

func (m *mockTelemetryRepo) UpdateSessionStats(_ context.Context, id string, toolCalls int, totalCost float64, promptTokens, completionTokens, totalTokens int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return fmt.Errorf("session %q not found", id)
	}
	s.ToolCalls = toolCalls
	s.TotalCost = totalCost
	s.PromptTokens = promptTokens
	s.CompletionTokens = completionTokens
	s.TotalTokens = totalTokens
	return nil
}

func (m *mockTelemetryRepo) RecordToolCall(_ context.Context, metric *types.ToolCallMetric) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	if metric.ID == "" {
		metric.ID = fmt.Sprintf("tc-mock-%d", m.nextID)
	}
	m.metrics[metric.SessionID] = append(m.metrics[metric.SessionID], metric)
	return nil
}

func (m *mockTelemetryRepo) GetSessionMetrics(_ context.Context, sessionID string) ([]*types.ToolCallMetric, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.metrics[sessionID], nil
}

func (m *mockTelemetryRepo) GetCostReport(_ context.Context, _ time.Time) ([]*types.CostEstimate, error) {
	return nil, nil
}

func (m *mockTelemetryRepo) GetMetricsSummary(_ context.Context, _ time.Time) (map[string]interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var totalCalls int64
	var totalErrors int64
	var totalCost float64
	for _, metricsSlice := range m.metrics {
		for _, metric := range metricsSlice {
			totalCalls++
			totalCost += metric.Cost
			if !metric.Success {
				totalErrors++
			}
		}
	}
	errorRate := 0.0
	if totalCalls > 0 {
		errorRate = float64(totalErrors) / float64(totalCalls)
	}
	return map[string]interface{}{
		"total_calls":     totalCalls,
		"total_errors":    totalErrors,
		"error_rate":      errorRate,
		"total_cost":      totalCost,
		"avg_duration_ms": 0.0,
		"tools":           []map[string]interface{}{},
	}, nil
}

func (m *mockTelemetryRepo) CreateAlert(_ context.Context, alert *types.Alert) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	if alert.ID == "" {
		alert.ID = fmt.Sprintf("alert-mock-%d", m.nextID)
	}
	m.alerts[alert.ID] = alert
	return alert.ID, nil
}

func (m *mockTelemetryRepo) GetAlert(_ context.Context, id string) (*types.Alert, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.alerts[id]
	if !ok {
		return nil, fmt.Errorf("alert %q not found", id)
	}
	return a, nil
}

func (m *mockTelemetryRepo) ListAlerts(_ context.Context) ([]*types.Alert, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*types.Alert
	for _, a := range m.alerts {
		out = append(out, a)
	}
	return out, nil
}

func (m *mockTelemetryRepo) UpdateAlert(_ context.Context, alert *types.Alert) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.alerts[alert.ID]; !ok {
		return fmt.Errorf("alert %q not found", alert.ID)
	}
	m.alerts[alert.ID] = alert
	return nil
}

func (m *mockTelemetryRepo) DeleteAlert(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.alerts[id]; !ok {
		return fmt.Errorf("alert %q not found", id)
	}
	delete(m.alerts, id)
	return nil
}

func (m *mockTelemetryRepo) UpdateAlertFired(_ context.Context, id string, firedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.alerts[id]
	if !ok {
		return fmt.Errorf("alert %q not found", id)
	}
	a.LastFiredAt = &firedAt
	return nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// ---------------------------------------------------------------------------
// SessionTracker tests
// ---------------------------------------------------------------------------

func TestSessionTracker_StartSession(t *testing.T) {
	repo := newMockTelemetryRepo()
	bus := nervous.NewEventBus(64)
	tracker := NewSessionTracker(repo, bus, testLogger())

	ctx := context.Background()
	id, err := tracker.StartSession(ctx, "agent-1", `{"model":"test"}`)
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	if id == "" {
		t.Error("expected non-empty session ID")
	}

	// Should appear in active sessions.
	active := tracker.GetActiveSession("agent-1")
	if active == nil {
		t.Fatal("expected active session for agent-1")
	}
	if active.ID != id {
		t.Errorf("active session ID = %q, want %q", active.ID, id)
	}
	if active.Status != "active" {
		t.Errorf("status = %q, want 'active'", active.Status)
	}
}

func TestSessionTracker_StartSession_EmptyAgentID(t *testing.T) {
	repo := newMockTelemetryRepo()
	bus := nervous.NewEventBus(64)
	tracker := NewSessionTracker(repo, bus, testLogger())

	_, err := tracker.StartSession(context.Background(), "", "")
	if err == nil {
		t.Error("expected error for empty agent_id")
	}
}

func TestSessionTracker_EndSession(t *testing.T) {
	repo := newMockTelemetryRepo()
	bus := nervous.NewEventBus(64)
	tracker := NewSessionTracker(repo, bus, testLogger())

	ctx := context.Background()
	id, err := tracker.StartSession(ctx, "agent-1", "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	if err := tracker.EndSession(ctx, id); err != nil {
		t.Fatalf("end: %v", err)
	}

	// Should no longer be active.
	active := tracker.GetActiveSession("agent-1")
	if active != nil {
		t.Error("session should not be active after ending")
	}

	if tracker.ActiveSessionCount() != 0 {
		t.Errorf("active count = %d, want 0", tracker.ActiveSessionCount())
	}
}

func TestSessionTracker_EndSession_NotFound(t *testing.T) {
	repo := newMockTelemetryRepo()
	bus := nervous.NewEventBus(64)
	tracker := NewSessionTracker(repo, bus, testLogger())

	err := tracker.EndSession(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}

func TestSessionTracker_RecordToolCall(t *testing.T) {
	repo := newMockTelemetryRepo()
	bus := nervous.NewEventBus(64)
	tracker := NewSessionTracker(repo, bus, testLogger())

	ctx := context.Background()
	id, err := tracker.StartSession(ctx, "agent-1", "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	metric := &types.ToolCallMetric{
		ToolName:   "search_code",
		StartedAt:  time.Now(),
		Duration:   150 * time.Millisecond,
		Success:    true,
		InputSize:  100,
		OutputSize: 500,
	}

	if err := tracker.RecordToolCall(ctx, id, metric); err != nil {
		t.Fatalf("record tool call: %v", err)
	}

	// Session stats should be updated.
	active := tracker.GetActiveSession("agent-1")
	if active == nil {
		t.Fatal("expected active session")
	}
	if active.ToolCalls != 1 {
		t.Errorf("tool_calls = %d, want 1", active.ToolCalls)
	}
	if active.TotalCost <= 0 {
		t.Errorf("total_cost should be positive, got %f", active.TotalCost)
	}
}

func TestSessionTracker_RecordToolCall_AutoEstimatesCost(t *testing.T) {
	repo := newMockTelemetryRepo()
	bus := nervous.NewEventBus(64)
	tracker := NewSessionTracker(repo, bus, testLogger())

	ctx := context.Background()
	id, err := tracker.StartSession(ctx, "agent-1", "")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	metric := &types.ToolCallMetric{
		ToolName:  "search_code",
		StartedAt: time.Now(),
		Duration:  100 * time.Millisecond,
		Success:   true,
		Cost:      0, // should be auto-estimated
	}

	if err := tracker.RecordToolCall(ctx, id, metric); err != nil {
		t.Fatalf("RecordToolCall: %v", err)
	}

	if metric.Cost <= 0 {
		t.Errorf("cost should have been auto-estimated, got %f", metric.Cost)
	}
}

func TestSessionTracker_GetActiveSession_NoSession(t *testing.T) {
	repo := newMockTelemetryRepo()
	bus := nervous.NewEventBus(64)
	tracker := NewSessionTracker(repo, bus, testLogger())

	active := tracker.GetActiveSession("nonexistent-agent")
	if active != nil {
		t.Error("expected nil for nonexistent agent")
	}
}

func TestSessionTracker_MultipleAgents(t *testing.T) {
	repo := newMockTelemetryRepo()
	bus := nervous.NewEventBus(64)
	tracker := NewSessionTracker(repo, bus, testLogger())

	ctx := context.Background()
	id1, err := tracker.StartSession(ctx, "agent-1", "")
	if err != nil {
		t.Fatalf("StartSession agent-1: %v", err)
	}
	id2, err := tracker.StartSession(ctx, "agent-2", "")
	if err != nil {
		t.Fatalf("StartSession agent-2: %v", err)
	}

	if id1 == id2 {
		t.Error("session IDs should be unique")
	}

	if tracker.ActiveSessionCount() != 2 {
		t.Errorf("active count = %d, want 2", tracker.ActiveSessionCount())
	}

	s1 := tracker.GetActiveSession("agent-1")
	s2 := tracker.GetActiveSession("agent-2")
	if s1 == nil || s2 == nil {
		t.Fatal("both agents should have active sessions")
	}
	if s1.AgentID != "agent-1" || s2.AgentID != "agent-2" {
		t.Error("agent IDs do not match")
	}
}

func TestSessionTracker_EventBusPublish(t *testing.T) {
	bus := nervous.NewEventBus(64)
	sub := bus.SubscribeTypes("test-sub",
		types.EventTelemetrySessionStart,
		types.EventTelemetrySessionEnd,
		types.EventTelemetryToolCall,
	)

	repo := newMockTelemetryRepo()
	tracker := NewSessionTracker(repo, bus, testLogger())
	ctx := context.Background()

	// Start session -> should publish session start event.
	id, err := tracker.StartSession(ctx, "agent-1", "")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	select {
	case ev := <-sub.Ch:
		if ev.Type != types.EventTelemetrySessionStart {
			t.Errorf("expected session start event, got %s", ev.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for session start event")
	}

	// Record tool call -> should publish tool call event.
	if err := tracker.RecordToolCall(ctx, id, &types.ToolCallMetric{
		ToolName:  "test_tool",
		StartedAt: time.Now(),
		Duration:  10 * time.Millisecond,
		Success:   true,
	}); err != nil {
		t.Fatalf("RecordToolCall: %v", err)
	}

	select {
	case ev := <-sub.Ch:
		if ev.Type != types.EventTelemetryToolCall {
			t.Errorf("expected tool call event, got %s", ev.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for tool call event")
	}

	// End session -> should publish session end event.
	if err := tracker.EndSession(ctx, id); err != nil {
		t.Fatalf("EndSession: %v", err)
	}

	select {
	case ev := <-sub.Ch:
		if ev.Type != types.EventTelemetrySessionEnd {
			t.Errorf("expected session end event, got %s", ev.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for session end event")
	}

	bus.Unsubscribe("test-sub")
}
