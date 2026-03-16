package pulse

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/hyperax/hyperax/internal/interject"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

const (
	// defaultWatchdogInterval is how often the watchdog checks the heartbeat.
	defaultWatchdogInterval = 5 * time.Second

	// defaultStaleThreshold is the maximum age of a heartbeat before the
	// watchdog considers the Pulse Engine stalled and triggers a global halt.
	defaultStaleThreshold = 15 * time.Second

	// watchdogSourceType identifies watchdog-created interjections.
	watchdogSourceType = "watchdog"

	// watchdogClearance is the minimum clearance required to resolve a
	// watchdog interjection. Maps to ClearanceChiefOfStaff (Level 3).
	watchdogClearance = 3
)

// Watchdog monitors the Pulse Engine heartbeat and triggers a global
// SafeMode halt if the heartbeat becomes stale. This implements the
// fail-closed safety guarantee: if the Pulse Engine stops ticking for
// longer than the stale threshold, all agent activity is halted until
// a Level 3 (ChiefOfStaff) clearance holder resolves the interjection.
type Watchdog struct {
	engine     *Engine
	ijMgr      *interject.Manager
	bus        *nervous.EventBus
	logger     *slog.Logger
	interval   time.Duration
	threshold  time.Duration
	triggered  bool
	nowFunc    func() time.Time
}

// NewWatchdog creates a fail-closed watchdog that monitors the Pulse Engine
// heartbeat. It does not start monitoring until Run is called.
func NewWatchdog(engine *Engine, ijMgr *interject.Manager, bus *nervous.EventBus, logger *slog.Logger) *Watchdog {
	return &Watchdog{
		engine:    engine,
		ijMgr:     ijMgr,
		bus:       bus,
		logger:    logger.With("component", "watchdog"),
		interval:  defaultWatchdogInterval,
		threshold: defaultStaleThreshold,
		nowFunc:   time.Now,
	}
}

// Run starts the watchdog loop. It blocks until ctx is cancelled.
// On each tick it reads the Pulse Engine's last heartbeat timestamp
// and compares it to the current time. If the heartbeat is stale
// beyond the threshold, a global SafeMode halt is triggered.
func (w *Watchdog) Run(ctx context.Context) {
	w.logger.Info("watchdog started",
		"interval", w.interval,
		"stale_threshold", w.threshold,
	)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("watchdog stopped")
			return
		case <-ticker.C:
			w.check()
		}
	}
}

// check reads the heartbeat and triggers or recovers accordingly.
func (w *Watchdog) check() {
	last := w.engine.LastHeartbeat()
	now := w.nowFunc()

	// Engine hasn't started yet — skip check.
	if last.IsZero() {
		return
	}

	age := now.Sub(last)

	if age > w.threshold {
		if !w.triggered {
			w.trigger(age)
		}
		return
	}

	// Heartbeat is fresh. If we were previously triggered, log recovery.
	if w.triggered {
		w.recover()
	}
}

// trigger creates a global SafeMode halt via the interjection manager.
func (w *Watchdog) trigger(age time.Duration) {
	w.triggered = true

	reason := fmt.Sprintf(
		"Pulse Engine heartbeat stale for %s (threshold: %s). Fail-closed safety halt engaged.",
		age.Round(time.Second), w.threshold,
	)

	w.logger.Error("pulse engine heartbeat stale — triggering global halt",
		"age", age.Round(time.Second),
		"threshold", w.threshold,
	)

	// Publish watchdog event on the nervous system.
	if w.bus != nil {
		payload, _ := json.Marshal(map[string]any{
			"age_seconds":       age.Seconds(),
			"threshold_seconds": w.threshold.Seconds(),
		})
		w.bus.Publish(types.NervousEvent{
			Type:      types.EventWatchdogTriggered,
			Scope:     string(types.ScopeGlobal),
			Source:    watchdogSourceType,
			Payload:   payload,
			Timestamp: w.nowFunc(),
		})
	}

	// Create a global halt interjection. The high source clearance (Level 3)
	// ensures only a ChiefOfStaff can resolve it.
	if w.ijMgr != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		ij := &types.Interjection{
			Scope:           string(types.ScopeGlobal),
			Severity:        string(types.SeverityFatal),
			Source:          watchdogSourceType,
			Reason:          reason,
			SourceClearance: watchdogClearance,
		}

		id, err := w.ijMgr.Halt(ctx, ij)
		if err != nil {
			w.logger.Error("watchdog failed to create halt interjection", "error", err)
			return
		}
		w.logger.Warn("watchdog interjection created", "interjection_id", id)
	}
}

// recover logs that the heartbeat has resumed after a watchdog trigger.
// The SafeMode halt remains active until manually resolved by a Level 3 holder.
func (w *Watchdog) recover() {
	w.triggered = false

	w.logger.Info("pulse engine heartbeat recovered — SafeMode still active until resolved")

	if w.bus != nil {
		w.bus.Publish(types.NervousEvent{
			Type:      types.EventWatchdogRecovered,
			Scope:     string(types.ScopeGlobal),
			Source:    watchdogSourceType,
			Timestamp: w.nowFunc(),
		})
	}
}
