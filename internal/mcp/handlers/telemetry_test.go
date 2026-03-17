package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/telemetry"
	"github.com/hyperax/hyperax/pkg/types"
)

// ---------------------------------------------------------------------------
// Mock TelemetryRepo for handler tests
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

func (m *mockTelemetryRepo) UpdateSessionStats(_ context.Context, id string, toolCalls int, totalCost float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return fmt.Errorf("session %q not found", id)
	}
	s.ToolCalls = toolCalls
	s.TotalCost = totalCost
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
	m.mu.Lock()
	defer m.mu.Unlock()
	var estimates []*types.CostEstimate
	costs := make(map[string]*types.CostEstimate)
	for _, metricsSlice := range m.metrics {
		for _, metric := range metricsSlice {
			e, ok := costs[metric.ToolName]
			if !ok {
				e = &types.CostEstimate{ToolName: metric.ToolName}
				costs[metric.ToolName] = e
			}
			e.CallCount++
			e.TotalCost += metric.Cost
		}
	}
	for _, e := range costs {
		if e.CallCount > 0 {
			e.AvgCost = e.TotalCost / float64(e.CallCount)
		}
		estimates = append(estimates, e)
	}
	return estimates, nil
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
	return map[string]interface{}{
		"total_calls":     totalCalls,
		"total_errors":    totalErrors,
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
// Test setup
// ---------------------------------------------------------------------------

func telemetryTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func setupTelemetryHandler(t *testing.T) (*ObservabilityHandler, *mockTelemetryRepo, context.Context) {
	t.Helper()
	repo := newMockTelemetryRepo()
	bus := nervous.NewEventBus(64)
	logger := telemetryTestLogger()
	tracker := telemetry.NewSessionTracker(repo, bus, logger)
	evaluator := telemetry.NewAlertEvaluator(repo, bus, logger)
	collector := telemetry.NewCollector()

	handler := NewObservabilityHandler(nil, logger)
	handler.SetTelemetryDeps(repo, tracker, evaluator, collector)
	return handler, repo, context.Background()
}

// callTelemetryTool is a convenience wrapper for invoking handler methods.
func callTelemetryTool(t *testing.T, fn func(context.Context, json.RawMessage) (*types.ToolResult, error), ctx context.Context, args any) *types.ToolResult {
	t.Helper()
	data, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	result, err := fn(ctx, data)
	if err != nil {
		t.Fatalf("tool error: %v", err)
	}
	return result
}

func telemetryResultText(r *types.ToolResult) string {
	if len(r.Content) == 0 {
		return ""
	}
	return r.Content[0].Text
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestObservabilityHandler_TelemetryRegisterTools(t *testing.T) {
	h, _, _ := setupTelemetryHandler(t)
	registry := mcp.NewToolRegistry()
	h.RegisterTools(registry)

	// Consolidated handler registers a single "observability" tool.
	if registry.ToolCount() != 1 {
		t.Errorf("expected 1 tool (observability), got %d", registry.ToolCount())
	}

	schemas := registry.Schemas()
	if len(schemas) == 0 || schemas[0].Name != "observability" {
		t.Errorf("expected tool named 'observability', got %v", schemas)
	}
}

func TestTelemetryHandler_GetSessionTelemetry(t *testing.T) {
	h, repo, ctx := setupTelemetryHandler(t)

	// Create a session via the mock repo.
	sess := &types.Session{AgentID: "agent-1", Status: "active", Metadata: "{}"}
	sessID, err := repo.CreateSession(ctx, sess)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Add a metric.
	if err := repo.RecordToolCall(ctx, &types.ToolCallMetric{
		SessionID: sessID, ToolName: "search_code", StartedAt: time.Now(),
		Duration: 100 * time.Millisecond, Success: true, Cost: 0.001,
	}); err != nil {
		t.Fatalf("record tool call: %v", err)
	}

	type args struct {
		SessionID string `json:"session_id"`
	}
	result := callTelemetryTool(t, h.getSessionTelemetry, ctx, args{SessionID: sessID})

	if result.IsError {
		t.Fatalf("unexpected error: %s", telemetryResultText(result))
	}

	text := telemetryResultText(result)
	if !strings.Contains(text, sessID) {
		t.Errorf("expected session ID in result, got: %s", text)
	}
}

func TestTelemetryHandler_GetSessionTelemetry_NotFound(t *testing.T) {
	h, _, ctx := setupTelemetryHandler(t)

	type args struct {
		SessionID string `json:"session_id"`
	}
	result := callTelemetryTool(t, h.getSessionTelemetry, ctx, args{SessionID: "nonexistent"})

	if !result.IsError {
		t.Errorf("expected error for nonexistent session, got: %s", telemetryResultText(result))
	}
}

func TestTelemetryHandler_GetSessionTelemetry_MissingParam(t *testing.T) {
	h, _, ctx := setupTelemetryHandler(t)

	type args struct {
		SessionID string `json:"session_id"`
	}
	result := callTelemetryTool(t, h.getSessionTelemetry, ctx, args{SessionID: ""})

	if !result.IsError {
		t.Error("expected error for empty session_id")
	}
}

func TestTelemetryHandler_ListSessions(t *testing.T) {
	h, repo, ctx := setupTelemetryHandler(t)

	if _, err := repo.CreateSession(ctx, &types.Session{AgentID: "agent-1", Status: "active", Metadata: "{}"}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := repo.CreateSession(ctx, &types.Session{AgentID: "agent-2", Status: "active", Metadata: "{}"}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	type args struct {
		Limit int `json:"limit"`
	}
	result := callTelemetryTool(t, h.listSessions, ctx, args{Limit: 10})

	if result.IsError {
		t.Fatalf("unexpected error: %s", telemetryResultText(result))
	}

	text := telemetryResultText(result)
	if !strings.Contains(text, "agent-1") || !strings.Contains(text, "agent-2") {
		t.Errorf("expected both agents in result, got: %s", text)
	}
}

func TestTelemetryHandler_ListSessions_Empty(t *testing.T) {
	h, _, ctx := setupTelemetryHandler(t)

	result := callTelemetryTool(t, h.listSessions, ctx, struct{}{})

	if result.IsError {
		t.Fatalf("unexpected error: %s", telemetryResultText(result))
	}
	text := telemetryResultText(result)
	if text != "[]" {
		t.Errorf("expected empty JSON array '[]', got: %s", text)
	}
}

func TestTelemetryHandler_ListSessions_AgentFilter(t *testing.T) {
	h, repo, ctx := setupTelemetryHandler(t)

	if _, err := repo.CreateSession(ctx, &types.Session{AgentID: "agent-1", Status: "active", Metadata: "{}"}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := repo.CreateSession(ctx, &types.Session{AgentID: "agent-2", Status: "active", Metadata: "{}"}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	type args struct {
		AgentID string `json:"agent_id"`
	}
	result := callTelemetryTool(t, h.listSessions, ctx, args{AgentID: "agent-1"})

	if result.IsError {
		t.Fatalf("unexpected error: %s", telemetryResultText(result))
	}

	text := telemetryResultText(result)
	if !strings.Contains(text, "agent-1") {
		t.Errorf("expected agent-1 in filtered results, got: %s", text)
	}
	if strings.Contains(text, "agent-2") {
		t.Errorf("agent-2 should not appear in filtered results, got: %s", text)
	}
}

func TestTelemetryHandler_GetMetricsSummary(t *testing.T) {
	h, _, ctx := setupTelemetryHandler(t)

	// The collector is used here (non-nil), so record some data.
	h.collector.Record("search_code", 100*time.Millisecond, true)
	h.collector.Record("replace_lines", 200*time.Millisecond, false)

	result := callTelemetryTool(t, h.getMetricsSummary, ctx, struct{}{})

	if result.IsError {
		t.Fatalf("unexpected error: %s", telemetryResultText(result))
	}

	text := telemetryResultText(result)
	if !strings.Contains(text, "total_calls") {
		t.Errorf("expected 'total_calls' in summary, got: %s", text)
	}
}

func TestTelemetryHandler_GetCostReport(t *testing.T) {
	h, repo, ctx := setupTelemetryHandler(t)

	sess := &types.Session{AgentID: "agent-1", Status: "active", Metadata: "{}"}
	sessID, err := repo.CreateSession(ctx, sess)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := repo.RecordToolCall(ctx, &types.ToolCallMetric{
		SessionID: sessID, ToolName: "search_code", StartedAt: time.Now(),
		Duration: 50 * time.Millisecond, Success: true, Cost: 0.01,
	}); err != nil {
		t.Fatalf("record tool call: %v", err)
	}

	result := callTelemetryTool(t, h.getCostReport, ctx, struct{}{})

	if result.IsError {
		t.Fatalf("unexpected error: %s", telemetryResultText(result))
	}

	text := telemetryResultText(result)
	if !strings.Contains(text, "search_code") {
		t.Errorf("expected 'search_code' in cost report, got: %s", text)
	}
}

func TestTelemetryHandler_GetCostReport_Empty(t *testing.T) {
	h, _, ctx := setupTelemetryHandler(t)

	result := callTelemetryTool(t, h.getCostReport, ctx, struct{}{})

	if result.IsError {
		t.Fatalf("unexpected error: %s", telemetryResultText(result))
	}
	text := telemetryResultText(result)
	// Empty cost report returns a structured object with empty items and subtotals.
	if !strings.Contains(text, `"items":[]`) {
		t.Errorf("expected empty items array in cost report, got: %s", text)
	}
	if !strings.Contains(text, `"provider_subtotals"`) {
		t.Errorf("expected provider_subtotals key in cost report, got: %s", text)
	}
}

func TestTelemetryHandler_CreateAlert(t *testing.T) {
	h, _, ctx := setupTelemetryHandler(t)

	type args struct {
		Name      string  `json:"name"`
		Metric    string  `json:"metric"`
		Operator  string  `json:"operator"`
		Threshold float64 `json:"threshold"`
		Window    string  `json:"window"`
		Severity  string  `json:"severity"`
	}
	result := callTelemetryTool(t, h.createAlert, ctx, args{
		Name:      "high-cost",
		Metric:    "session_cost",
		Operator:  "gt",
		Threshold: 10.0,
		Window:    "1h",
		Severity:  "warning",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", telemetryResultText(result))
	}

	text := telemetryResultText(result)
	if !strings.Contains(text, "high-cost") {
		t.Errorf("expected alert name in result, got: %s", text)
	}
}

func TestTelemetryHandler_CreateAlert_MissingParams(t *testing.T) {
	h, _, ctx := setupTelemetryHandler(t)

	type args struct {
		Name string `json:"name"`
	}
	result := callTelemetryTool(t, h.createAlert, ctx, args{Name: ""})
	if !result.IsError {
		t.Error("expected error for missing name")
	}
}

func TestTelemetryHandler_CreateAlert_InvalidMetric(t *testing.T) {
	h, _, ctx := setupTelemetryHandler(t)

	type args struct {
		Name      string  `json:"name"`
		Metric    string  `json:"metric"`
		Operator  string  `json:"operator"`
		Threshold float64 `json:"threshold"`
	}
	result := callTelemetryTool(t, h.createAlert, ctx, args{
		Name: "bad", Metric: "invalid_metric", Operator: "gt", Threshold: 1.0,
	})
	if !result.IsError {
		t.Error("expected error for invalid metric")
	}
}

func TestTelemetryHandler_CreateAlert_InvalidOperator(t *testing.T) {
	h, _, ctx := setupTelemetryHandler(t)

	type args struct {
		Name      string  `json:"name"`
		Metric    string  `json:"metric"`
		Operator  string  `json:"operator"`
		Threshold float64 `json:"threshold"`
	}
	result := callTelemetryTool(t, h.createAlert, ctx, args{
		Name: "bad", Metric: "session_cost", Operator: "invalid", Threshold: 1.0,
	})
	if !result.IsError {
		t.Error("expected error for invalid operator")
	}
}

func TestTelemetryHandler_ListAlerts(t *testing.T) {
	h, repo, ctx := setupTelemetryHandler(t)

	if _, err := repo.CreateAlert(ctx, &types.Alert{
		Name: "alert-1", Metric: "session_cost", Operator: "gt",
		Threshold: 10.0, Window: "1h", Severity: "warning", Enabled: true,
	}); err != nil {
		t.Fatalf("create alert: %v", err)
	}

	result := callTelemetryTool(t, h.listAlerts, ctx, struct{}{})

	if result.IsError {
		t.Fatalf("unexpected error: %s", telemetryResultText(result))
	}

	text := telemetryResultText(result)
	if !strings.Contains(text, "alert-1") {
		t.Errorf("expected alert-1 in list, got: %s", text)
	}
}

func TestTelemetryHandler_ListAlerts_Empty(t *testing.T) {
	h, _, ctx := setupTelemetryHandler(t)

	result := callTelemetryTool(t, h.listAlerts, ctx, struct{}{})

	if result.IsError {
		t.Fatalf("unexpected error: %s", telemetryResultText(result))
	}
	text := telemetryResultText(result)
	if text != "[]" {
		t.Errorf("expected empty JSON array '[]', got: %s", text)
	}
}

func TestTelemetryHandler_DeleteAlert(t *testing.T) {
	h, repo, ctx := setupTelemetryHandler(t)

	alertID, err := repo.CreateAlert(ctx, &types.Alert{
		Name: "deleteme", Metric: "session_cost", Operator: "gt",
		Threshold: 1.0, Window: "1h", Severity: "info", Enabled: true,
	})
	if err != nil {
		t.Fatalf("create alert: %v", err)
	}

	type args struct {
		AlertID string `json:"alert_id"`
	}
	result := callTelemetryTool(t, h.deleteAlert, ctx, args{AlertID: alertID})

	if result.IsError {
		t.Fatalf("unexpected error: %s", telemetryResultText(result))
	}

	text := telemetryResultText(result)
	if !strings.Contains(text, alertID) {
		t.Errorf("expected alert ID in result, got: %s", text)
	}

	// Verify it's actually deleted.
	_, err = repo.GetAlert(ctx, alertID)
	if err == nil {
		t.Error("expected error when getting deleted alert")
	}
}

func TestTelemetryHandler_DeleteAlert_NotFound(t *testing.T) {
	h, _, ctx := setupTelemetryHandler(t)

	type args struct {
		AlertID string `json:"alert_id"`
	}
	result := callTelemetryTool(t, h.deleteAlert, ctx, args{AlertID: "nonexistent"})

	if !result.IsError {
		t.Error("expected error for nonexistent alert")
	}
}

func TestTelemetryHandler_DeleteAlert_MissingParam(t *testing.T) {
	h, _, ctx := setupTelemetryHandler(t)

	type args struct {
		AlertID string `json:"alert_id"`
	}
	result := callTelemetryTool(t, h.deleteAlert, ctx, args{AlertID: ""})

	if !result.IsError {
		t.Error("expected error for empty alert_id")
	}
}

// ---------------------------------------------------------------------------
// parseSinceParam tests
// ---------------------------------------------------------------------------

func TestParseSinceParam(t *testing.T) {
	// Empty defaults to 24h ago.
	result := parseSinceParam("", 24*time.Hour)
	if time.Since(result) < 23*time.Hour || time.Since(result) > 25*time.Hour {
		t.Errorf("empty since should default to ~24h ago, got %v ago", time.Since(result))
	}

	// Duration string.
	result = parseSinceParam("1h", 24*time.Hour)
	if time.Since(result) < 50*time.Minute || time.Since(result) > 70*time.Minute {
		t.Errorf("1h since should be ~1h ago, got %v ago", time.Since(result))
	}

	// Day suffix.
	result = parseSinceParam("7d", 24*time.Hour)
	if time.Since(result) < 6*24*time.Hour || time.Since(result) > 8*24*time.Hour {
		t.Errorf("7d since should be ~7d ago, got %v ago", time.Since(result))
	}

	// RFC3339.
	fixed := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	result = parseSinceParam(fixed.Format(time.RFC3339), 24*time.Hour)
	if !result.Equal(fixed) {
		t.Errorf("RFC3339 parse: got %v, want %v", result, fixed)
	}

	// Invalid falls back to default.
	result = parseSinceParam("garbage", 24*time.Hour)
	if time.Since(result) < 23*time.Hour || time.Since(result) > 25*time.Hour {
		t.Errorf("invalid since should default to ~24h ago, got %v ago", time.Since(result))
	}
}
