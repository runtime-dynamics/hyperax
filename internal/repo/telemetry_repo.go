package repo

import (
	"context"
	"time"

	"github.com/hyperax/hyperax/pkg/types"
)

// TelemetryRepo handles session lifecycle, tool call metrics, and alert management.
// It provides the persistence layer for the telemetry subsystem.
type TelemetryRepo interface {
	// Sessions

	// CreateSession inserts a new session and returns its generated ID.
	CreateSession(ctx context.Context, session *types.Session) (string, error)

	// GetSession retrieves a session by its ID.
	GetSession(ctx context.Context, id string) (*types.Session, error)

	// ListSessions returns sessions, optionally filtered by agentID.
	// Pass an empty agentID to list all sessions. Limit controls the maximum
	// number of results (0 means no limit).
	ListSessions(ctx context.Context, agentID string, limit int) ([]*types.Session, error)

	// EndSession marks a session as completed by setting ended_at and status.
	EndSession(ctx context.Context, id string) error

	// UpdateSessionStats updates the running tool call count, total cost,
	// and accumulated token counts for an active session.
	UpdateSessionStats(ctx context.Context, id string, toolCalls int, totalCost float64, promptTokens, completionTokens, totalTokens int) error

	// Tool call metrics

	// RecordToolCall inserts a single tool call metric record.
	RecordToolCall(ctx context.Context, metric *types.ToolCallMetric) error

	// GetSessionMetrics returns all tool call metrics for a given session.
	GetSessionMetrics(ctx context.Context, sessionID string) ([]*types.ToolCallMetric, error)

	// GetCostReport aggregates tool call costs grouped by tool name since the
	// given timestamp, returning per-tool totals, averages, and call counts.
	GetCostReport(ctx context.Context, since time.Time) ([]*types.CostEstimate, error)

	// GetMetricsSummary returns an aggregate summary of all tool call metrics
	// since the given timestamp: total calls, total errors, total cost,
	// avg duration, and per-tool breakdowns.
	GetMetricsSummary(ctx context.Context, since time.Time) (map[string]interface{}, error)

	// Alerts

	// CreateAlert inserts a new alert rule and returns its generated ID.
	CreateAlert(ctx context.Context, alert *types.Alert) (string, error)

	// GetAlert retrieves an alert by its ID.
	GetAlert(ctx context.Context, id string) (*types.Alert, error)

	// ListAlerts returns all configured alerts.
	ListAlerts(ctx context.Context) ([]*types.Alert, error)

	// UpdateAlert modifies an existing alert rule.
	UpdateAlert(ctx context.Context, alert *types.Alert) error

	// DeleteAlert removes an alert by its ID.
	DeleteAlert(ctx context.Context, id string) error

	// UpdateAlertFired records the time an alert last fired.
	UpdateAlertFired(ctx context.Context, id string, firedAt time.Time) error
}
