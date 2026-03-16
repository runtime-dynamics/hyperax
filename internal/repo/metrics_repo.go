package repo

import (
	"context"
	"time"
)

// ToolMetric captures tool usage statistics.
type ToolMetric struct {
	ToolName        string
	CallCount       int64
	LastUsed        *time.Time
	TotalDurationMS int64
}

// MetricsRepo handles tool usage metrics.
type MetricsRepo interface {
	RecordToolMetric(ctx context.Context, toolName string, durationMS int64) error
	GetToolMetrics(ctx context.Context) ([]*ToolMetric, error)
}
