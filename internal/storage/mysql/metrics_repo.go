package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/hyperax/hyperax/internal/repo"
)

// MetricsRepo implements repo.MetricsRepo for MySQL.
type MetricsRepo struct {
	db *sql.DB
}

// RecordToolMetric upserts a tool metric row: increments call_count, adds to
// total_duration_ms, and updates last_used to the current time.
func (r *MetricsRepo) RecordToolMetric(ctx context.Context, toolName string, durationMS int64) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO tool_metrics (tool_name, call_count, total_duration_ms, last_used)
		 VALUES (?, 1, ?, NOW())
		 ON DUPLICATE KEY UPDATE
		   call_count = call_count + 1,
		   total_duration_ms = total_duration_ms + VALUES(total_duration_ms),
		   last_used = VALUES(last_used)`,
		toolName, durationMS,
	)
	if err != nil {
		return fmt.Errorf("mysql.MetricsRepo.RecordToolMetric: %w", err)
	}
	return nil
}

// GetToolMetrics returns all recorded tool metrics ordered by call_count descending.
func (r *MetricsRepo) GetToolMetrics(ctx context.Context) ([]*repo.ToolMetric, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT tool_name, call_count, last_used, total_duration_ms
		 FROM tool_metrics ORDER BY call_count DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("mysql.MetricsRepo.GetToolMetrics: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var metrics []*repo.ToolMetric
	for rows.Next() {
		m := &repo.ToolMetric{}
		var lastUsed sql.NullTime

		if err := rows.Scan(&m.ToolName, &m.CallCount, &lastUsed, &m.TotalDurationMS); err != nil {
			return nil, fmt.Errorf("mysql.MetricsRepo.GetToolMetrics: %w", err)
		}

		if lastUsed.Valid {
			m.LastUsed = &lastUsed.Time
		}

		metrics = append(metrics, m)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql.MetricsRepo.GetToolMetrics: %w", err)
	}
	return metrics, nil
}
