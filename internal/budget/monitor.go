package budget

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hyperax/hyperax/internal/interject"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

const (
	// Default check interval for budget evaluation.
	defaultCheckInterval = 30 * time.Second

	// Fixed threshold percentages.
	thresholdWarning   = 80.0
	thresholdCritical  = 95.0
	thresholdExhausted = 100.0
)

// Option configures a Monitor.
type Option func(*Monitor)

// WithCheckInterval sets the evaluation interval. The default is 30 seconds.
func WithCheckInterval(d time.Duration) Option {
	return func(m *Monitor) {
		if d > 0 {
			m.interval = d
		}
	}
}

// Monitor periodically evaluates budget thresholds and triggers Andon Cord
// interjections when spending exceeds configured limits. It prevents runaway
// agent sessions from burning through API budgets.
//
// Threshold levels:
//   - 80%  → warning event (no halt)
//   - 95%  → critical interjection (agents pause)
//   - 100% → fatal interjection (full stop)
type Monitor struct {
	repo   repo.BudgetRepo
	ijMgr  *interject.Manager // nil = interjections disabled
	bus    *nervous.EventBus
	logger *slog.Logger

	interval time.Duration

	// Track which scopes already have active interjections to avoid duplicates.
	mu           sync.Mutex
	activeAlerts map[string]string // scope → interjection_id
}

// NewMonitor creates a budget threshold monitor.
// Pass a nil ijMgr to disable interjection creation (events are still published).
func NewMonitor(budgetRepo repo.BudgetRepo, ijMgr *interject.Manager, bus *nervous.EventBus, logger *slog.Logger, opts ...Option) *Monitor {
	m := &Monitor{
		repo:         budgetRepo,
		ijMgr:        ijMgr,
		bus:          bus,
		logger:       logger.With("component", "budget.monitor"),
		interval:     defaultCheckInterval,
		activeAlerts: make(map[string]string),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Run starts the background evaluation loop. It blocks until ctx is cancelled.
func (m *Monitor) Run(ctx context.Context) {
	m.logger.Info("budget monitor started", "interval", m.interval)

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("budget monitor stopped")
			return
		case <-ticker.C:
			if err := m.Evaluate(ctx); err != nil {
				m.logger.Error("budget evaluation failed", "error", err)
			}
		}
	}
}

// Evaluate checks all scopes with configured thresholds and emits events or
// interjections when budget utilization crosses the warning, critical, or
// exhausted thresholds.
func (m *Monitor) Evaluate(ctx context.Context) error {
	scopes, err := m.repo.ListThresholdScopes(ctx)
	if err != nil {
		return fmt.Errorf("list threshold scopes: %w", err)
	}

	for _, scope := range scopes {
		if err := m.evaluateScope(ctx, scope); err != nil {
			m.logger.Warn("scope evaluation failed", "scope", scope, "error", err)
		}
	}

	return nil
}

// evaluateScope checks a single scope's budget utilization and takes action.
func (m *Monitor) evaluateScope(ctx context.Context, scope string) error {
	threshold, err := m.repo.GetBudgetThreshold(ctx, scope)
	if err != nil {
		return fmt.Errorf("get threshold: %w", err)
	}
	if threshold <= 0 {
		return nil
	}

	cost, err := m.repo.GetCumulativeEnergyCost(ctx, scope)
	if err != nil {
		return fmt.Errorf("get cumulative cost: %w", err)
	}

	percentUsed := (cost / threshold) * 100

	switch {
	case percentUsed >= thresholdExhausted:
		m.publishEvent(types.EventBudgetExhausted, scope, cost, threshold, percentUsed)
		m.triggerInterjection(ctx, scope, "fatal", cost, threshold, percentUsed)

	case percentUsed >= thresholdCritical:
		m.publishEvent(types.EventBudgetCritical, scope, cost, threshold, percentUsed)
		m.triggerInterjection(ctx, scope, "critical", cost, threshold, percentUsed)

	case percentUsed >= thresholdWarning:
		m.publishEvent(types.EventBudgetWarning, scope, cost, threshold, percentUsed)
		// Warning: event only, no interjection.
	}

	return nil
}

// publishEvent emits a budget event on the Nervous System EventBus.
func (m *Monitor) publishEvent(eventType types.EventType, scope string, cost, threshold, percentUsed float64) {
	if m.bus == nil {
		return
	}
	m.bus.Publish(nervous.NewEvent(
		eventType,
		"budget.monitor",
		scope,
		map[string]any{
			"scope":        scope,
			"cost":         cost,
			"threshold":    threshold,
			"percent_used": percentUsed,
		},
	))
}

// triggerInterjection creates an Andon Cord halt for the given scope and severity.
// It skips creation if an interjection is already active for this scope.
func (m *Monitor) triggerInterjection(ctx context.Context, scope, severity string, cost, threshold, percentUsed float64) {
	if m.ijMgr == nil {
		return
	}

	m.mu.Lock()
	if _, exists := m.activeAlerts[scope]; exists {
		m.mu.Unlock()
		m.logger.Debug("interjection already active for scope, skipping", "scope", scope)
		return
	}
	m.mu.Unlock()

	ij := &types.Interjection{
		Scope:      scope,
		Severity:   severity,
		Source:     "budget.monitor",
		Reason:     fmt.Sprintf("Budget threshold breached for scope %q: %.1f%% used (%.2f / %.2f)", scope, percentUsed, cost, threshold),
		CreatedBy:  "system",
		TrustLevel: "internal",
	}

	id, err := m.ijMgr.Halt(ctx, ij)
	if err != nil {
		m.logger.Error("failed to create budget interjection",
			"scope", scope,
			"severity", severity,
			"error", err,
		)
		return
	}

	m.mu.Lock()
	m.activeAlerts[scope] = id
	m.mu.Unlock()

	m.logger.Warn("budget interjection created",
		"interjection_id", id,
		"scope", scope,
		"severity", severity,
		"percent_used", percentUsed,
	)
}

// ClearAlert removes the active alert tracking for a scope. This should be
// called when an interjection is resolved so that the monitor can create a
// new one if the budget is breached again.
func (m *Monitor) ClearAlert(scope string) {
	m.mu.Lock()
	delete(m.activeAlerts, scope)
	m.mu.Unlock()
}

// ActiveAlerts returns a snapshot of currently tracked alert interjection IDs by scope.
func (m *Monitor) ActiveAlerts() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()

	snapshot := make(map[string]string, len(m.activeAlerts))
	for k, v := range m.activeAlerts {
		snapshot[k] = v
	}
	return snapshot
}
