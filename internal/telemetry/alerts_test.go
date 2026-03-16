package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

func TestAlertEvaluator_NoAlerts(t *testing.T) {
	repo := newMockTelemetryRepo()
	bus := nervous.NewEventBus(64)
	eval := NewAlertEvaluator(repo, bus, testLogger())

	firings, err := eval.Evaluate(context.Background())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(firings) != 0 {
		t.Errorf("expected 0 firings, got %d", len(firings))
	}
}

func TestAlertEvaluator_ThresholdNotBreached(t *testing.T) {
	repo := newMockTelemetryRepo()
	bus := nervous.NewEventBus(64)
	eval := NewAlertEvaluator(repo, bus, testLogger())

	ctx := context.Background()

	// Create alert: total_cost > 100 (should not fire with no data).
	_, _ = repo.CreateAlert(ctx, &types.Alert{
		Name:      "high-cost",
		Metric:    "session_cost",
		Operator:  "gt",
		Threshold: 100.0,
		Window:    "1h",
		Severity:  "warning",
		Enabled:   true,
	})

	firings, err := eval.Evaluate(ctx)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(firings) != 0 {
		t.Errorf("expected 0 firings (threshold not breached), got %d", len(firings))
	}
}

func TestAlertEvaluator_ThresholdBreached(t *testing.T) {
	repo := newMockTelemetryRepo()
	bus := nervous.NewEventBus(64)
	sub := bus.SubscribeTypes("test-alert-sub", types.EventTelemetryAlert)
	eval := NewAlertEvaluator(repo, bus, testLogger())

	ctx := context.Background()

	// Create a session and add some metrics to generate cost.
	sess := &types.Session{
		AgentID: "agent-1",
		Status:  "active",
	}
	sessID, _ := repo.CreateSession(ctx, sess)
	for i := 0; i < 10; i++ {
		_ = repo.RecordToolCall(ctx, &types.ToolCallMetric{
			SessionID: sessID,
			ToolName:  "search_code",
			StartedAt: time.Now(),
			Duration:  100 * time.Millisecond,
			Success:   true,
			Cost:      5.0, // 10 * 5.0 = 50.0 total
		})
	}

	// Create alert: session_cost > 10 (should fire).
	_, _ = repo.CreateAlert(ctx, &types.Alert{
		Name:      "cost-alert",
		Metric:    "session_cost",
		Operator:  "gt",
		Threshold: 10.0,
		Window:    "1h",
		Severity:  "critical",
		Enabled:   true,
	})

	firings, err := eval.Evaluate(ctx)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(firings) != 1 {
		t.Fatalf("expected 1 firing, got %d", len(firings))
	}

	f := firings[0]
	if f.AlertName != "cost-alert" {
		t.Errorf("alert_name = %q, want 'cost-alert'", f.AlertName)
	}
	if f.Severity != "critical" {
		t.Errorf("severity = %q, want 'critical'", f.Severity)
	}
	if f.Value <= f.Threshold {
		t.Errorf("value (%f) should exceed threshold (%f)", f.Value, f.Threshold)
	}

	// Should have published an event.
	select {
	case ev := <-sub.Ch:
		if ev.Type != types.EventTelemetryAlert {
			t.Errorf("expected alert event, got %s", ev.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for alert event")
	}

	bus.Unsubscribe("test-alert-sub")
}

func TestAlertEvaluator_DisabledAlertSkipped(t *testing.T) {
	repo := newMockTelemetryRepo()
	bus := nervous.NewEventBus(64)
	eval := NewAlertEvaluator(repo, bus, testLogger())
	ctx := context.Background()

	// Add metrics.
	sess := &types.Session{AgentID: "agent-1", Status: "active"}
	sessID, _ := repo.CreateSession(ctx, sess)
	_ = repo.RecordToolCall(ctx, &types.ToolCallMetric{
		SessionID: sessID, ToolName: "x", StartedAt: time.Now(),
		Duration: 10 * time.Millisecond, Success: true, Cost: 100.0,
	})

	// Create disabled alert.
	_, _ = repo.CreateAlert(ctx, &types.Alert{
		Name:      "disabled-alert",
		Metric:    "session_cost",
		Operator:  "gt",
		Threshold: 1.0,
		Window:    "1h",
		Severity:  "info",
		Enabled:   false,
	})

	firings, err := eval.Evaluate(ctx)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(firings) != 0 {
		t.Errorf("disabled alert should not fire, got %d firings", len(firings))
	}
}

func TestAlertEvaluator_UpdatesLastFiredAt(t *testing.T) {
	repo := newMockTelemetryRepo()
	bus := nervous.NewEventBus(64)
	eval := NewAlertEvaluator(repo, bus, testLogger())
	ctx := context.Background()

	// Add cost data.
	sess := &types.Session{AgentID: "agent-1", Status: "active"}
	sessID, _ := repo.CreateSession(ctx, sess)
	_ = repo.RecordToolCall(ctx, &types.ToolCallMetric{
		SessionID: sessID, ToolName: "x", StartedAt: time.Now(),
		Duration: 10 * time.Millisecond, Success: true, Cost: 50.0,
	})

	alertID, _ := repo.CreateAlert(ctx, &types.Alert{
		Name: "fires-once", Metric: "session_cost", Operator: "gt",
		Threshold: 1.0, Window: "1h", Severity: "info", Enabled: true,
	})

	_, _ = eval.Evaluate(ctx)

	// Check that last_fired_at was updated.
	alert, err := repo.GetAlert(ctx, alertID)
	if err != nil {
		t.Fatalf("get alert: %v", err)
	}
	if alert.LastFiredAt == nil {
		t.Error("expected last_fired_at to be set")
	}
}

func TestAlertEvaluator_ErrorRateOperator(t *testing.T) {
	repo := newMockTelemetryRepo()
	bus := nervous.NewEventBus(64)
	eval := NewAlertEvaluator(repo, bus, testLogger())
	ctx := context.Background()

	sess := &types.Session{AgentID: "agent-1", Status: "active"}
	sessID, _ := repo.CreateSession(ctx, sess)

	// 5 successes and 5 errors -> 50% error rate.
	for i := 0; i < 5; i++ {
		_ = repo.RecordToolCall(ctx, &types.ToolCallMetric{
			SessionID: sessID, ToolName: "x", StartedAt: time.Now(),
			Duration: 10 * time.Millisecond, Success: true, Cost: 0.01,
		})
		_ = repo.RecordToolCall(ctx, &types.ToolCallMetric{
			SessionID: sessID, ToolName: "x", StartedAt: time.Now(),
			Duration: 10 * time.Millisecond, Success: false, Cost: 0.01,
		})
	}

	// Alert: error_rate > 0.3 should fire.
	_, _ = repo.CreateAlert(ctx, &types.Alert{
		Name: "high-error-rate", Metric: "error_rate", Operator: "gt",
		Threshold: 0.3, Window: "1h", Severity: "warning", Enabled: true,
	})

	firings, err := eval.Evaluate(ctx)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(firings) != 1 {
		t.Errorf("expected 1 firing for error_rate breach, got %d", len(firings))
	}
}

// ---------------------------------------------------------------------------
// breached() unit tests
// ---------------------------------------------------------------------------

func TestBreached(t *testing.T) {
	tests := []struct {
		value     float64
		operator  string
		threshold float64
		want      bool
	}{
		{10, "gt", 5, true},
		{5, "gt", 5, false},
		{3, "gt", 5, false},
		{3, "lt", 5, true},
		{5, "lt", 5, false},
		{5, "gte", 5, true},
		{4, "gte", 5, false},
		{5, "lte", 5, true},
		{6, "lte", 5, false},
		{5, "eq", 5, true},
		{6, "eq", 5, false},
		{5, "invalid", 5, false},
	}

	for _, tt := range tests {
		got := breached(tt.value, tt.operator, tt.threshold)
		if got != tt.want {
			t.Errorf("breached(%f, %q, %f) = %v, want %v",
				tt.value, tt.operator, tt.threshold, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// parseWindow() unit tests
// ---------------------------------------------------------------------------

func TestParseWindow(t *testing.T) {
	tests := []struct {
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"1h", 1 * time.Hour, false},
		{"24h", 24 * time.Hour, false},
		{"30m", 30 * time.Minute, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"1d", 24 * time.Hour, false},
		{"", 0, true},
		{"invalid", 0, true},
	}

	for _, tt := range tests {
		got, err := parseWindow(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseWindow(%q): err=%v, wantErr=%v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("parseWindow(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
