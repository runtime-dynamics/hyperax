package telemetry

import (
	"testing"
	"time"
)

func TestCollector_Record(t *testing.T) {
	c := NewCollector()
	c.Record("search_code", 100*time.Millisecond, true)
	c.Record("search_code", 200*time.Millisecond, true)
	c.Record("search_code", 300*time.Millisecond, false)

	summary := c.Summary()

	totalCalls, _ := summary["total_calls"].(int64)
	if totalCalls != 3 {
		t.Errorf("total_calls = %d, want 3", totalCalls)
	}

	totalErrors, _ := summary["total_errors"].(int64)
	if totalErrors != 1 {
		t.Errorf("total_errors = %d, want 1", totalErrors)
	}
}

func TestCollector_ErrorRate(t *testing.T) {
	c := NewCollector()
	c.Record("tool_a", 10*time.Millisecond, true)
	c.Record("tool_a", 10*time.Millisecond, false)

	summary := c.Summary()
	errorRate, _ := summary["error_rate"].(float64)

	if errorRate != 0.5 {
		t.Errorf("error_rate = %f, want 0.5", errorRate)
	}
}

func TestCollector_EmptySummary(t *testing.T) {
	c := NewCollector()
	summary := c.Summary()

	totalCalls, _ := summary["total_calls"].(int64)
	if totalCalls != 0 {
		t.Errorf("total_calls = %d, want 0", totalCalls)
	}

	errorRate, _ := summary["error_rate"].(float64)
	if errorRate != 0.0 {
		t.Errorf("error_rate = %f, want 0.0", errorRate)
	}

	tools, _ := summary["tools"].([]map[string]interface{})
	if len(tools) != 0 {
		t.Errorf("tools should be empty, got %d", len(tools))
	}
}

func TestCollector_Reset(t *testing.T) {
	c := NewCollector()
	c.Record("tool_a", 10*time.Millisecond, true)
	c.Record("tool_b", 20*time.Millisecond, false)
	c.Reset()

	summary := c.Summary()
	totalCalls, _ := summary["total_calls"].(int64)
	if totalCalls != 0 {
		t.Errorf("total_calls after reset = %d, want 0", totalCalls)
	}
}

func TestCollector_MultipleTools(t *testing.T) {
	c := NewCollector()
	c.Record("tool_a", 10*time.Millisecond, true)
	c.Record("tool_a", 20*time.Millisecond, true)
	c.Record("tool_b", 50*time.Millisecond, true)

	summary := c.Summary()
	totalCalls, _ := summary["total_calls"].(int64)
	if totalCalls != 3 {
		t.Errorf("total_calls = %d, want 3", totalCalls)
	}

	tools, ok := summary["tools"].([]map[string]interface{})
	if !ok || len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}
}

func TestCollector_Percentiles(t *testing.T) {
	c := NewCollector()

	// Record 100 calls with increasing durations.
	for i := 1; i <= 100; i++ {
		c.Record("perf_tool", time.Duration(i)*time.Millisecond, true)
	}

	summary := c.Summary()
	tools, ok := summary["tools"].([]map[string]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	tool := tools[0]

	p50, _ := tool["p50_ms"].(int64)
	p95, _ := tool["p95_ms"].(int64)
	p99, _ := tool["p99_ms"].(int64)

	// p50 should be around 50ms, p95 around 95ms, p99 around 99ms.
	if p50 < 45 || p50 > 55 {
		t.Errorf("p50 = %d, expected ~50", p50)
	}
	if p95 < 90 || p95 > 100 {
		t.Errorf("p95 = %d, expected ~95", p95)
	}
	if p99 < 95 || p99 > 105 {
		t.Errorf("p99 = %d, expected ~99", p99)
	}
}

func TestPercentile_EmptySlice(t *testing.T) {
	result := percentile(nil, 0.5)
	if result != 0 {
		t.Errorf("percentile of empty slice = %v, want 0", result)
	}
}

func TestPercentile_SingleElement(t *testing.T) {
	result := percentile([]time.Duration{42 * time.Millisecond}, 0.99)
	if result != 42*time.Millisecond {
		t.Errorf("percentile of single element = %v, want 42ms", result)
	}
}

func TestAvg_EmptySlice(t *testing.T) {
	result := avg(nil)
	if result != 0 {
		t.Errorf("avg of empty slice = %v, want 0", result)
	}
}

func TestAvg_Values(t *testing.T) {
	durations := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		30 * time.Millisecond,
	}
	result := avg(durations)
	if result != 20*time.Millisecond {
		t.Errorf("avg = %v, want 20ms", result)
	}
}
