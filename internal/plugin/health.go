package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

const (
	// defaultHealthInterval is how often the health checker runs.
	defaultHealthInterval = 30 * time.Second

	// defaultMaxFailures is the consecutive failure count before auto-disable.
	defaultMaxFailures = 3
)

// HealthChecker performs periodic liveness checks on loaded plugins.
// In Phase 1, the check verifies that a plugin is in the "enabled" state.
// Future phases will add HTTP endpoint pings, Wasm runtime checks, etc.
type HealthChecker struct {
	manager     *PluginManager
	logger      *slog.Logger
	interval    time.Duration
	maxFailures int
}

// NewHealthChecker creates a HealthChecker with default interval and failure threshold.
func NewHealthChecker(manager *PluginManager, logger *slog.Logger) *HealthChecker {
	return &HealthChecker{
		manager:     manager,
		logger:      logger.With("component", "plugin-health"),
		interval:    defaultHealthInterval,
		maxFailures: defaultMaxFailures,
	}
}

// Run starts the periodic health check loop. It blocks until ctx is cancelled.
// Each tick iterates over all loaded plugins and calls CheckPlugin for enabled ones.
func (hc *HealthChecker) Run(ctx context.Context) {
	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()

	hc.logger.Info("health checker started", "interval", hc.interval.String(), "max_failures", hc.maxFailures)

	for {
		select {
		case <-ctx.Done():
			hc.logger.Info("health checker stopped")
			return
		case <-ticker.C:
			hc.checkAll(ctx)
		}
	}
}

// checkAll iterates all plugins and runs a health check on each enabled one.
func (hc *HealthChecker) checkAll(ctx context.Context) {
	plugins := hc.manager.ListPlugins()
	for _, state := range plugins {
		if !state.Enabled {
			continue
		}
		if err := hc.CheckPlugin(ctx, state.Name); err != nil {
			hc.logger.Warn("plugin health check failed",
				"plugin", state.Name,
				"error", err.Error(),
				"failure_count", state.FailureCount,
			)
		}
	}
}

// CheckPlugin verifies the health of a single plugin by name.
//
// Phase 1 behaviour: checks that the plugin exists and is in "enabled" state.
// If the plugin has accumulated maxFailures consecutive failures, it is
// automatically disabled to prevent cascading issues.
func (hc *HealthChecker) CheckPlugin(ctx context.Context, name string) error {
	hc.manager.mu.Lock()
	defer hc.manager.mu.Unlock()

	lp, ok := hc.manager.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}

	now := time.Now()
	lp.State.LastHealthAt = &now

	// Phase 1: simple state-based health check.
	if lp.State.Enabled && lp.State.Status == "enabled" {
		lp.State.HealthStatus = "healthy"
		lp.State.FailureCount = 0
		return nil
	}

	// Plugin is loaded but not in expected state.
	lp.State.FailureCount++
	lp.State.HealthStatus = "unhealthy"

	if lp.State.FailureCount >= hc.maxFailures {
		hc.logger.Error("plugin exceeded max failures, auto-disabling",
			"plugin", name,
			"failures", lp.State.FailureCount,
		)
		lp.State.Enabled = false
		lp.State.Status = "disabled"
		lp.State.HealthStatus = "unhealthy"
	}

	return fmt.Errorf("plugin %q health check failed (status=%s, failures=%d)", name, lp.State.Status, lp.State.FailureCount)
}
