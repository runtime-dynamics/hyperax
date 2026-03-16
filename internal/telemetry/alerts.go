package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// AlertEvaluator periodically evaluates all enabled alerts against current
// telemetry metrics and fires events when thresholds are breached.
type AlertEvaluator struct {
	repo   repo.TelemetryRepo
	bus    *nervous.EventBus
	logger *slog.Logger
}

// NewAlertEvaluator creates an AlertEvaluator.
func NewAlertEvaluator(repo repo.TelemetryRepo, bus *nervous.EventBus, logger *slog.Logger) *AlertEvaluator {
	return &AlertEvaluator{
		repo:   repo,
		bus:    bus,
		logger: logger,
	}
}

// Evaluate checks all enabled alerts against current metrics and returns any
// that have breached their thresholds. For each firing alert, it updates the
// last_fired_at timestamp and publishes an EventTelemetryAlert event.
func (e *AlertEvaluator) Evaluate(ctx context.Context) ([]*types.AlertFiring, error) {
	alerts, err := e.repo.ListAlerts(ctx)
	if err != nil {
		return nil, fmt.Errorf("telemetry.AlertEvaluator.Evaluate: %w", err)
	}

	var firings []*types.AlertFiring

	for _, alert := range alerts {
		if !alert.Enabled {
			continue
		}

		window, err := parseWindow(alert.Window)
		if err != nil {
			e.logger.Warn("invalid alert window", "alert_id", alert.ID, "window", alert.Window, "error", err)
			continue
		}

		since := time.Now().Add(-window)

		value, err := e.computeMetricValue(ctx, alert.Metric, since)
		if err != nil {
			e.logger.Warn("failed to compute metric", "alert_id", alert.ID, "metric", alert.Metric, "error", err)
			continue
		}

		if !breached(value, alert.Operator, alert.Threshold) {
			continue
		}

		now := time.Now()
		firing := &types.AlertFiring{
			AlertID:   alert.ID,
			AlertName: alert.Name,
			Value:     value,
			Threshold: alert.Threshold,
			Severity:  alert.Severity,
			FiredAt:   now,
		}
		firings = append(firings, firing)

		// Update last_fired_at in the database.
		if err := e.repo.UpdateAlertFired(ctx, alert.ID, now); err != nil {
			e.logger.Error("failed to update alert fired time", "alert_id", alert.ID, "error", err)
		}

		e.logger.Warn("alert fired",
			"alert_id", alert.ID,
			"alert_name", alert.Name,
			"metric", alert.Metric,
			"value", value,
			"threshold", alert.Threshold,
			"severity", alert.Severity,
		)

		// Publish alert event to the EventBus.
		if e.bus != nil {
			payload, _ := json.Marshal(firing)
			e.bus.Publish(types.NervousEvent{
				Type:      types.EventTelemetryAlert,
				Source:    "telemetry.alert_evaluator",
				Scope:     alert.Severity,
				Payload:   payload,
				Timestamp: now,
			})
		}
	}

	return firings, nil
}

// Run starts a background loop that evaluates alerts at the given interval.
// It blocks until the context is cancelled.
func (e *AlertEvaluator) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := e.Evaluate(ctx); err != nil {
				e.logger.Error("alert evaluation failed", "error", err)
			}
		}
	}
}

// computeMetricValue retrieves the current value for the named metric within
// the given time window.
func (e *AlertEvaluator) computeMetricValue(ctx context.Context, metric string, since time.Time) (float64, error) {
	summary, err := e.repo.GetMetricsSummary(ctx, since)
	if err != nil {
		return 0, err
	}

	switch metric {
	case "session_cost":
		if v, ok := summary["total_cost"].(float64); ok {
			return v, nil
		}
		return 0, nil

	case "tool_calls":
		if v, ok := summary["total_calls"].(int64); ok {
			return float64(v), nil
		}
		return 0, nil

	case "error_rate":
		if v, ok := summary["error_rate"].(float64); ok {
			return v, nil
		}
		return 0, nil

	case "duration":
		if v, ok := summary["avg_duration_ms"].(float64); ok {
			return v, nil
		}
		return 0, nil

	default:
		return 0, fmt.Errorf("unknown metric: %q", metric)
	}
}

// parseWindow converts a window string like "1h", "24h", "7d" into a time.Duration.
func parseWindow(window string) (time.Duration, error) {
	if len(window) == 0 {
		return 0, fmt.Errorf("empty window")
	}

	// Try standard Go duration parsing first.
	d, err := time.ParseDuration(window)
	if err == nil {
		return d, nil
	}

	// Handle day suffix "d" which Go's time.ParseDuration does not support.
	if window[len(window)-1] == 'd' {
		var days int
		if _, err := fmt.Sscanf(window, "%dd", &days); err == nil && days > 0 {
			return time.Duration(days) * 24 * time.Hour, nil
		}
	}

	return 0, fmt.Errorf("invalid window format: %q", window)
}

// breached checks whether value breaches threshold according to the given operator.
func breached(value float64, operator string, threshold float64) bool {
	switch operator {
	case "gt":
		return value > threshold
	case "lt":
		return value < threshold
	case "gte":
		return value >= threshold
	case "lte":
		return value <= threshold
	case "eq":
		return value == threshold
	default:
		return false
	}
}
