package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/hyperax/hyperax/pkg/types"
)

// TelemetryRepo implements repo.TelemetryRepo for MySQL.
type TelemetryRepo struct {
	db *sql.DB
}

// CreateSession inserts a new session and returns its generated ID.
func (r *TelemetryRepo) CreateSession(ctx context.Context, session *types.Session) (string, error) {
	if session.ID == "" {
		session.ID = fmt.Sprintf("sess-%d", time.Now().UnixNano())
	}
	if session.Status == "" {
		session.Status = "active"
	}
	if session.Metadata == "" {
		session.Metadata = "{}"
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, provider_id, model, started_at, tool_calls, total_cost, status, metadata)
		 VALUES (?, ?, ?, ?, NOW(), ?, ?, ?, ?)`,
		session.ID, session.AgentID, session.ProviderID, session.Model,
		session.ToolCalls, session.TotalCost, session.Status, session.Metadata,
	)
	if err != nil {
		return "", fmt.Errorf("mysql.TelemetryRepo.CreateSession: %w", err)
	}
	return session.ID, nil
}

// GetSession retrieves a session by its ID.
func (r *TelemetryRepo) GetSession(ctx context.Context, id string) (*types.Session, error) {
	s := &types.Session{}
	var endedAt sql.NullTime

	err := r.db.QueryRowContext(ctx,
		`SELECT id, agent_id, COALESCE(provider_id, ''), COALESCE(model, ''),
		        started_at, ended_at, tool_calls, total_cost,
		        status, COALESCE(metadata, '{}'), created_at
		 FROM sessions WHERE id = ?`, id,
	).Scan(
		&s.ID, &s.AgentID, &s.ProviderID, &s.Model, &s.StartedAt, &endedAt,
		&s.ToolCalls, &s.TotalCost, &s.Status, &s.Metadata, &s.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("session %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("mysql.TelemetryRepo.GetSession: %w", err)
	}

	if endedAt.Valid {
		s.EndedAt = &endedAt.Time
		s.Duration = endedAt.Time.Sub(s.StartedAt)
	}
	return s, nil
}

// ListSessions returns sessions, optionally filtered by agentID.
func (r *TelemetryRepo) ListSessions(ctx context.Context, agentID string, limit int) ([]*types.Session, error) {
	query := `SELECT id, agent_id, COALESCE(provider_id, ''), COALESCE(model, ''),
	                 started_at, ended_at, tool_calls, total_cost,
	                 status, COALESCE(metadata, '{}'), created_at
	          FROM sessions`
	var args []interface{}

	if agentID != "" {
		query += " WHERE agent_id = ?"
		args = append(args, agentID)
	}

	query += " ORDER BY started_at DESC"

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("mysql.TelemetryRepo.ListSessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var sessions []*types.Session
	for rows.Next() {
		s := &types.Session{}
		var endedAt sql.NullTime

		if err := rows.Scan(
			&s.ID, &s.AgentID, &s.ProviderID, &s.Model, &s.StartedAt, &endedAt,
			&s.ToolCalls, &s.TotalCost, &s.Status, &s.Metadata, &s.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("mysql.TelemetryRepo.ListSessions: %w", err)
		}

		if endedAt.Valid {
			s.EndedAt = &endedAt.Time
			s.Duration = endedAt.Time.Sub(s.StartedAt)
		}
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql.TelemetryRepo.ListSessions: %w", err)
	}
	return sessions, nil
}

// EndSession marks a session as completed.
func (r *TelemetryRepo) EndSession(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE sessions SET ended_at = NOW(), status = 'completed'
		 WHERE id = ? AND status = 'active'`,
		id,
	)
	if err != nil {
		return fmt.Errorf("mysql.TelemetryRepo.EndSession: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mysql.TelemetryRepo.EndSession: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("session %q not found or already ended", id)
	}
	return nil
}

// UpdateSessionStats updates the running tool call count and total cost.
func (r *TelemetryRepo) UpdateSessionStats(ctx context.Context, id string, toolCalls int, totalCost float64) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE sessions SET tool_calls = ?, total_cost = ? WHERE id = ?`,
		toolCalls, totalCost, id,
	)
	if err != nil {
		return fmt.Errorf("mysql.TelemetryRepo.UpdateSessionStats: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mysql.TelemetryRepo.UpdateSessionStats: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("session %q not found", id)
	}
	return nil
}

// RecordToolCall inserts a single tool call metric record.
func (r *TelemetryRepo) RecordToolCall(ctx context.Context, metric *types.ToolCallMetric) error {
	if metric.ID == "" {
		metric.ID = fmt.Sprintf("tc-%d", time.Now().UnixNano())
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO tool_call_metrics (id, session_id, tool_name, provider_id, started_at, duration_ms,
		                                success, error_msg, input_size, output_size, cost)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		metric.ID, metric.SessionID, metric.ToolName, metric.ProviderID,
		metric.StartedAt,
		metric.Duration.Milliseconds(),
		metric.Success, metric.ErrorMsg,
		metric.InputSize, metric.OutputSize, metric.Cost,
	)
	if err != nil {
		return fmt.Errorf("mysql.TelemetryRepo.RecordToolCall: %w", err)
	}
	return nil
}

// GetSessionMetrics returns all tool call metrics for a given session.
func (r *TelemetryRepo) GetSessionMetrics(ctx context.Context, sessionID string) ([]*types.ToolCallMetric, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, session_id, tool_name, started_at, duration_ms,
		        success, COALESCE(error_msg, ''), input_size, output_size, cost
		 FROM tool_call_metrics
		 WHERE session_id = ?
		 ORDER BY started_at ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("mysql.TelemetryRepo.GetSessionMetrics: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var metrics []*types.ToolCallMetric
	for rows.Next() {
		m := &types.ToolCallMetric{}
		var durationMS int64

		if err := rows.Scan(
			&m.ID, &m.SessionID, &m.ToolName, &m.StartedAt, &durationMS,
			&m.Success, &m.ErrorMsg, &m.InputSize, &m.OutputSize, &m.Cost,
		); err != nil {
			return nil, fmt.Errorf("mysql.TelemetryRepo.GetSessionMetrics: %w", err)
		}

		m.Duration = time.Duration(durationMS) * time.Millisecond
		metrics = append(metrics, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql.TelemetryRepo.GetSessionMetrics: %w", err)
	}
	return metrics, nil
}

// GetCostReport aggregates tool call costs grouped by provider and tool name
// since the given timestamp. Each row includes the provider_id so the frontend
// can show per-provider subtotals alongside the per-tool breakdown.
func (r *TelemetryRepo) GetCostReport(ctx context.Context, since time.Time) ([]*types.CostEstimate, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT COALESCE(provider_id, '') as provider_id,
		        tool_name,
		        COUNT(*) as call_count,
		        COALESCE(SUM(cost), 0.0) as total_cost,
		        COALESCE(AVG(cost), 0.0) as avg_cost,
		        COALESCE(AVG(duration_ms), 0) as avg_duration_ms
		 FROM tool_call_metrics
		 WHERE started_at >= ?
		 GROUP BY provider_id, tool_name
		 ORDER BY provider_id, total_cost DESC`,
		since,
	)
	if err != nil {
		return nil, fmt.Errorf("mysql.TelemetryRepo.GetCostReport: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var estimates []*types.CostEstimate
	for rows.Next() {
		e := &types.CostEstimate{}
		var avgDurationMS float64

		if err := rows.Scan(&e.ProviderID, &e.ToolName, &e.CallCount, &e.TotalCost, &e.AvgCost, &avgDurationMS); err != nil {
			return nil, fmt.Errorf("mysql.TelemetryRepo.GetCostReport: %w", err)
		}
		e.AvgDuration = time.Duration(int64(avgDurationMS)) * time.Millisecond
		estimates = append(estimates, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql.TelemetryRepo.GetCostReport: %w", err)
	}
	return estimates, nil
}

// GetMetricsSummary returns an aggregate summary of all tool call metrics since the given timestamp.
func (r *TelemetryRepo) GetMetricsSummary(ctx context.Context, since time.Time) (map[string]interface{}, error) {
	var totalCalls int64
	var totalErrors int64
	var totalCost float64
	var avgDurationMS float64

	err := r.db.QueryRowContext(ctx,
		`SELECT COALESCE(COUNT(*), 0),
		        COALESCE(SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(cost), 0.0),
		        COALESCE(AVG(duration_ms), 0.0)
		 FROM tool_call_metrics
		 WHERE started_at >= ?`,
		since,
	).Scan(&totalCalls, &totalErrors, &totalCost, &avgDurationMS)
	if err != nil {
		return nil, fmt.Errorf("mysql.TelemetryRepo.GetMetricsSummary: %w", err)
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT tool_name, COUNT(*) as calls,
		        COALESCE(SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END), 0) as errors,
		        COALESCE(SUM(cost), 0.0) as cost,
		        COALESCE(AVG(duration_ms), 0.0) as avg_ms
		 FROM tool_call_metrics
		 WHERE started_at >= ?
		 GROUP BY tool_name
		 ORDER BY calls DESC`,
		since,
	)
	if err != nil {
		return nil, fmt.Errorf("mysql.TelemetryRepo.GetMetricsSummary: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tools []map[string]interface{}
	for rows.Next() {
		var name string
		var calls, errors int64
		var cost, avgMS float64

		if err := rows.Scan(&name, &calls, &errors, &cost, &avgMS); err != nil {
			return nil, fmt.Errorf("mysql.TelemetryRepo.GetMetricsSummary: %w", err)
		}

		tools = append(tools, map[string]interface{}{
			"tool_name":       name,
			"call_count":      calls,
			"error_count":     errors,
			"total_cost":      cost,
			"avg_duration_ms": avgMS,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql.TelemetryRepo.GetMetricsSummary: %w", err)
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
		"avg_duration_ms": avgDurationMS,
		"tools":           tools,
	}, nil
}

// CreateAlert inserts a new alert rule and returns its generated ID.
func (r *TelemetryRepo) CreateAlert(ctx context.Context, alert *types.Alert) (string, error) {
	if alert.ID == "" {
		alert.ID = fmt.Sprintf("alert-%d", time.Now().UnixNano())
	}
	if alert.Window == "" {
		alert.Window = "1h"
	}
	if alert.Severity == "" {
		alert.Severity = "info"
	}

	_, err := r.db.ExecContext(ctx,
		"INSERT INTO alerts (id, name, metric, operator, threshold, `window`, severity, enabled)"+
			" VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		alert.ID, alert.Name, alert.Metric, alert.Operator,
		alert.Threshold, alert.Window, alert.Severity, alert.Enabled,
	)
	if err != nil {
		return "", fmt.Errorf("mysql.TelemetryRepo.CreateAlert: %w", err)
	}
	return alert.ID, nil
}

// GetAlert retrieves an alert by its ID.
func (r *TelemetryRepo) GetAlert(ctx context.Context, id string) (*types.Alert, error) {
	a := &types.Alert{}
	var lastFiredAt sql.NullTime

	err := r.db.QueryRowContext(ctx,
		"SELECT id, name, metric, operator, threshold, `window`, severity,"+
			" enabled, last_fired_at, created_at, updated_at"+
			" FROM alerts WHERE id = ?", id,
	).Scan(
		&a.ID, &a.Name, &a.Metric, &a.Operator, &a.Threshold,
		&a.Window, &a.Severity, &a.Enabled, &lastFiredAt, &a.CreatedAt, &a.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("alert %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("mysql.TelemetryRepo.GetAlert: %w", err)
	}

	if lastFiredAt.Valid {
		a.LastFiredAt = &lastFiredAt.Time
	}
	return a, nil
}

// ListAlerts returns all configured alerts ordered by name.
func (r *TelemetryRepo) ListAlerts(ctx context.Context) ([]*types.Alert, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT id, name, metric, operator, threshold, `window`, severity,"+
			" enabled, last_fired_at, created_at, updated_at"+
			" FROM alerts ORDER BY name",
	)
	if err != nil {
		return nil, fmt.Errorf("mysql.TelemetryRepo.ListAlerts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var alerts []*types.Alert
	for rows.Next() {
		a := &types.Alert{}
		var lastFiredAt sql.NullTime

		if err := rows.Scan(
			&a.ID, &a.Name, &a.Metric, &a.Operator, &a.Threshold,
			&a.Window, &a.Severity, &a.Enabled, &lastFiredAt, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("mysql.TelemetryRepo.ListAlerts: %w", err)
		}

		if lastFiredAt.Valid {
			a.LastFiredAt = &lastFiredAt.Time
		}
		alerts = append(alerts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql.TelemetryRepo.ListAlerts: %w", err)
	}
	return alerts, nil
}

// UpdateAlert modifies an existing alert rule.
func (r *TelemetryRepo) UpdateAlert(ctx context.Context, alert *types.Alert) error {
	res, err := r.db.ExecContext(ctx,
		"UPDATE alerts SET"+
			" name = ?, metric = ?, operator = ?, threshold = ?,"+
			" `window` = ?, severity = ?, enabled = ?,"+
			" updated_at = NOW()"+
			" WHERE id = ?",
		alert.Name, alert.Metric, alert.Operator, alert.Threshold,
		alert.Window, alert.Severity, alert.Enabled, alert.ID,
	)
	if err != nil {
		return fmt.Errorf("mysql.TelemetryRepo.UpdateAlert: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mysql.TelemetryRepo.UpdateAlert: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("alert %q not found", alert.ID)
	}
	return nil
}

// DeleteAlert removes an alert by its ID.
func (r *TelemetryRepo) DeleteAlert(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM alerts WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("mysql.TelemetryRepo.DeleteAlert: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mysql.TelemetryRepo.DeleteAlert: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("alert %q not found", id)
	}
	return nil
}

// UpdateAlertFired records the time an alert last fired.
func (r *TelemetryRepo) UpdateAlertFired(ctx context.Context, id string, firedAt time.Time) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE alerts SET last_fired_at = ?, updated_at = NOW() WHERE id = ?`,
		firedAt, id,
	)
	if err != nil {
		return fmt.Errorf("mysql.TelemetryRepo.UpdateAlertFired: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mysql.TelemetryRepo.UpdateAlertFired: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("alert %q not found", id)
	}
	return nil
}
