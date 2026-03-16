package telemetry

import (
	"sort"
	"sync"
	"time"
)

// Collector aggregates real-time tool call statistics in memory. It provides
// a low-overhead way to track call counts, error counts, and duration
// distributions without hitting the database. Call Summary() to snapshot
// the current state, and Reset() to clear all counters.
type Collector struct {
	mu        sync.RWMutex
	counts    map[string]int64           // tool_name -> total call count
	errors    map[string]int64           // tool_name -> error count
	durations map[string][]time.Duration // tool_name -> recorded durations
}

// NewCollector creates an empty Collector.
func NewCollector() *Collector {
	return &Collector{
		counts:    make(map[string]int64),
		errors:    make(map[string]int64),
		durations: make(map[string][]time.Duration),
	}
}

// Record tracks a single tool call invocation.
func (c *Collector) Record(toolName string, duration time.Duration, success bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.counts[toolName]++
	if !success {
		c.errors[toolName]++
	}
	c.durations[toolName] = append(c.durations[toolName], duration)
}

// Summary returns a snapshot of the collected metrics including per-tool
// breakdowns with call counts, error counts, and duration percentiles.
func (c *Collector) Summary() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var totalCalls int64
	var totalErrors int64

	var tools []map[string]interface{}
	for name, count := range c.counts {
		errCount := c.errors[name]
		totalCalls += count
		totalErrors += errCount

		durations := c.durations[name]
		toolSummary := map[string]interface{}{
			"tool_name":   name,
			"call_count":  count,
			"error_count": errCount,
		}

		if len(durations) > 0 {
			toolSummary["p50_ms"] = percentile(durations, 0.50).Milliseconds()
			toolSummary["p95_ms"] = percentile(durations, 0.95).Milliseconds()
			toolSummary["p99_ms"] = percentile(durations, 0.99).Milliseconds()
			toolSummary["avg_ms"] = avg(durations).Milliseconds()
		}

		tools = append(tools, toolSummary)
	}

	// Sort tools by call count descending for stable output.
	sort.Slice(tools, func(i, j int) bool {
		ci, _ := tools[i]["call_count"].(int64)
		cj, _ := tools[j]["call_count"].(int64)
		return ci > cj
	})

	errorRate := 0.0
	if totalCalls > 0 {
		errorRate = float64(totalErrors) / float64(totalCalls)
	}

	return map[string]interface{}{
		"total_calls":  totalCalls,
		"total_errors": totalErrors,
		"error_rate":   errorRate,
		"tools":        tools,
	}
}

// Reset clears all collected metrics.
func (c *Collector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.counts = make(map[string]int64)
	c.errors = make(map[string]int64)
	c.durations = make(map[string][]time.Duration)
}

// percentile computes the p-th percentile from a slice of durations.
// p should be in [0.0, 1.0]. The durations slice is copied and sorted
// to avoid mutating the caller's data.
func percentile(durations []time.Duration, p float64) time.Duration {
	if len(durations) == 0 {
		return 0
	}

	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	idx := int(float64(len(sorted)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}

	return sorted[idx]
}

// avg computes the arithmetic mean of a slice of durations.
func avg(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}

	var total time.Duration
	for _, d := range durations {
		total += d
	}
	return total / time.Duration(len(durations))
}
