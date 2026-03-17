package pulse

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
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
// the engine recovers (auto-healed) or a Level 3 holder manually resolves.
type Watchdog struct {
	engine      *Engine
	ijMgr       *interject.Manager
	bus         *nervous.EventBus
	logger      *slog.Logger
	interval    time.Duration
	threshold   time.Duration
	triggered   atomic.Bool
	startupHeal atomic.Bool // ensures stale interjections from previous runs are healed once
	nowFunc     func() time.Time
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
		if !w.triggered.Load() {
			w.trigger(age)
		}
		return
	}

	// Heartbeat is fresh. Auto-resolve any active watchdog interjections.
	if w.triggered.Load() {
		w.recover()
		return
	}

	// On first fresh heartbeat after startup, heal any stale watchdog
	// interjections left over from a previous run. This runs once.
	if !w.startupHeal.Load() {
		w.startupHeal.Store(true)
		w.healStaleInterjections()
	}
}

// trigger creates a global SafeMode halt via the interjection manager.
// It deduplicates against existing active watchdog interjections to prevent
// repeated engine crash/restart cycles from flooding the interjection list.
func (w *Watchdog) trigger(age time.Duration) {
	w.triggered.Store(true)

	w.logger.Error("pulse engine heartbeat stale — triggering global halt",
		"age", age.Round(time.Second),
		"threshold", w.threshold,
	)

	// Publish watchdog event on the nervous system.
	if w.bus != nil {
		payload, err := json.Marshal(map[string]any{
			"age_seconds":       age.Seconds(),
			"threshold_seconds": w.threshold.Seconds(),
		})
		if err != nil {
			w.logger.Error("watchdog: failed to marshal event payload", "error", err)
		} else {
			w.bus.Publish(types.NervousEvent{
				Type:      types.EventWatchdogTriggered,
				Scope:     string(types.ScopeGlobal),
				Source:    watchdogSourceType,
				Payload:   payload,
				Timestamp: w.nowFunc(),
			})
		}
	}

	// Create a global halt interjection. The high source clearance (Level 3)
	// ensures only a ChiefOfStaff can resolve it.
	if w.ijMgr != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Deduplicate: skip if an active watchdog interjection already exists.
		active, err := w.ijMgr.GetActive(ctx, string(types.ScopeGlobal))
		if err == nil {
			for _, existing := range active {
				if existing.Source == watchdogSourceType && existing.Severity == string(types.SeverityFatal) {
					w.logger.Warn("watchdog halt already active, skipping duplicate",
						"existing_interjection_id", existing.ID,
						"age", age.Round(time.Second),
					)
					return
				}
			}
		}

		reason := fmt.Sprintf(
			"Pulse Engine heartbeat stale for %s (threshold: %s). Fail-closed safety halt engaged.",
			age.Round(time.Second), w.threshold,
		)

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

// recover auto-resolves any active watchdog interjections when the Pulse
// Engine heartbeat is healthy again. The watchdog owns the full lifecycle
// of its interjections: it creates them on stale heartbeat and heals them
// on recovery. This prevents stale watchdog halts from blocking the platform
// indefinitely after engine restarts.
func (w *Watchdog) recover() {
	if w.ijMgr != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		active, err := w.ijMgr.GetActive(ctx, string(types.ScopeGlobal))
		if err != nil {
			w.logger.Error("watchdog recovery: failed to query active interjections", "error", err)
			return
		}

		for _, existing := range active {
			if existing.Source != watchdogSourceType {
				continue
			}
			resolveErr := w.ijMgr.Resolve(ctx, &types.ResolutionAction{
				InterjectionID: existing.ID,
				ResolvedBy:     "watchdog",
				Resolution:     "Pulse Engine heartbeat recovered — auto-resolved by watchdog.",
				Action:         "resume",
			})
			if resolveErr != nil {
				w.logger.Error("watchdog recovery: failed to auto-resolve interjection",
					"interjection_id", existing.ID,
					"error", resolveErr,
				)
				continue
			}
			w.logger.Info("watchdog auto-resolved stale interjection",
				"interjection_id", existing.ID,
			)
		}
	}

	w.triggered.Store(false)

	if w.bus != nil {
		w.bus.Publish(types.NervousEvent{
			Type:      types.EventWatchdogRecovered,
			Scope:     string(types.ScopeGlobal),
			Source:    watchdogSourceType,
			Timestamp: w.nowFunc(),
		})
	}
}

// healStaleInterjections resolves watchdog interjections left over from a
// previous app run. Called once on the first fresh heartbeat after startup.
// This handles the case where RecoverOnStartup re-engaged safe mode for
// old watchdog halts but the engine is now healthy.
func (w *Watchdog) healStaleInterjections() {
	if w.ijMgr == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	active, err := w.ijMgr.GetActive(ctx, string(types.ScopeGlobal))
	if err != nil {
		w.logger.Error("watchdog startup heal: failed to query active interjections", "error", err)
		return
	}

	healed := 0
	for _, existing := range active {
		if existing.Source != watchdogSourceType {
			continue
		}
		resolveErr := w.ijMgr.Resolve(ctx, &types.ResolutionAction{
			InterjectionID: existing.ID,
			ResolvedBy:     "watchdog",
			Resolution:     "Pulse Engine healthy on startup — auto-healed by watchdog.",
			Action:         "resume",
		})
		if resolveErr != nil {
			w.logger.Error("watchdog startup heal: failed to resolve stale interjection",
				"interjection_id", existing.ID,
				"error", resolveErr,
			)
			continue
		}
		healed++
		w.logger.Info("watchdog healed stale interjection from previous run",
			"interjection_id", existing.ID,
		)
	}

	if healed > 0 {
		w.logger.Info("watchdog startup heal complete", "healed_count", healed)
	}
}
