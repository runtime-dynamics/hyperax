package sqlite

import (
	"testing"
)

func TestMetricsRepo_RecordAndGet(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &MetricsRepo{db: db.db}

	// Record a single metric
	if err := r.RecordToolMetric(ctx, "search_code", 150); err != nil {
		t.Fatalf("record: %v", err)
	}

	metrics, err := r.GetToolMetrics(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}

	m := metrics[0]
	if m.ToolName != "search_code" {
		t.Errorf("tool_name = %q, want %q", m.ToolName, "search_code")
	}
	if m.CallCount != 1 {
		t.Errorf("call_count = %d, want %d", m.CallCount, 1)
	}
	if m.TotalDurationMS != 150 {
		t.Errorf("total_duration_ms = %d, want %d", m.TotalDurationMS, 150)
	}
	if m.LastUsed == nil {
		t.Error("expected non-nil last_used")
	}
}

func TestMetricsRepo_Increment(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &MetricsRepo{db: db.db}

	// Record the same tool three times
	if err := r.RecordToolMetric(ctx, "get_file_content", 100); err != nil {
		t.Fatalf("record 1: %v", err)
	}
	if err := r.RecordToolMetric(ctx, "get_file_content", 200); err != nil {
		t.Fatalf("record 2: %v", err)
	}
	if err := r.RecordToolMetric(ctx, "get_file_content", 50); err != nil {
		t.Fatalf("record 3: %v", err)
	}

	metrics, err := r.GetToolMetrics(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric (upserted), got %d", len(metrics))
	}

	m := metrics[0]
	if m.CallCount != 3 {
		t.Errorf("call_count = %d, want %d", m.CallCount, 3)
	}
	if m.TotalDurationMS != 350 {
		t.Errorf("total_duration_ms = %d, want %d", m.TotalDurationMS, 350)
	}
}

func TestMetricsRepo_MultipleTools(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &MetricsRepo{db: db.db}

	if err := r.RecordToolMetric(ctx, "search_code", 100); err != nil {
		t.Fatalf("record search_code: %v", err)
	}
	if err := r.RecordToolMetric(ctx, "list_workspaces", 50); err != nil {
		t.Fatalf("record list_workspaces: %v", err)
	}
	if err := r.RecordToolMetric(ctx, "search_code", 200); err != nil {
		t.Fatalf("record search_code 2: %v", err)
	}

	metrics, err := r.GetToolMetrics(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(metrics) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(metrics))
	}

	// Ordered by call_count DESC, so search_code (2 calls) should be first
	if metrics[0].ToolName != "search_code" {
		t.Errorf("first metric = %q, want %q", metrics[0].ToolName, "search_code")
	}
	if metrics[0].CallCount != 2 {
		t.Errorf("search_code call_count = %d, want %d", metrics[0].CallCount, 2)
	}
	if metrics[1].ToolName != "list_workspaces" {
		t.Errorf("second metric = %q, want %q", metrics[1].ToolName, "list_workspaces")
	}
}

func TestMetricsRepo_EmptyMetrics(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &MetricsRepo{db: db.db}

	metrics, err := r.GetToolMetrics(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(metrics) != 0 {
		t.Errorf("expected 0 metrics, got %d", len(metrics))
	}
}
