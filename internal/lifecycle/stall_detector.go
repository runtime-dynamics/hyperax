package lifecycle

import (
	"context"
	"log/slog"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

const (
	// DefaultStallTimeout is the default duration after which an agent in a
	// transient state is considered stalled.
	DefaultStallTimeout = 5 * time.Minute

	// DefaultCheckInterval is the default interval between stall checks.
	DefaultCheckInterval = 60 * time.Second
)

// StallDetector periodically checks for agents stuck in transient lifecycle
// states beyond a configurable timeout. When a stall is detected, it publishes
// an EventLifecycleStalled event on the EventBus.
type StallDetector struct {
	repo          repo.LifecycleRepo
	bus           *nervous.EventBus
	logger        *slog.Logger
	stallTimeout  time.Duration
	checkInterval time.Duration
}

// StallDetectorOption configures a StallDetector.
type StallDetectorOption func(*StallDetector)

// WithStallTimeout sets the timeout after which a transient state is
// considered stalled.
func WithStallTimeout(d time.Duration) StallDetectorOption {
	return func(sd *StallDetector) {
		if d > 0 {
			sd.stallTimeout = d
		}
	}
}

// WithCheckInterval sets the interval between stall detection checks.
func WithCheckInterval(d time.Duration) StallDetectorOption {
	return func(sd *StallDetector) {
		if d > 0 {
			sd.checkInterval = d
		}
	}
}

// NewStallDetector creates a StallDetector with the given dependencies
// and optional configuration.
func NewStallDetector(lifecycleRepo repo.LifecycleRepo, bus *nervous.EventBus, logger *slog.Logger, opts ...StallDetectorOption) *StallDetector {
	sd := &StallDetector{
		repo:          lifecycleRepo,
		bus:           bus,
		logger:        logger,
		stallTimeout:  DefaultStallTimeout,
		checkInterval: DefaultCheckInterval,
	}
	for _, opt := range opts {
		opt(sd)
	}
	return sd
}

// Start runs the stall detection loop. It checks every checkInterval for
// agents in transient states that have been there longer than stallTimeout.
// Blocks until ctx is cancelled.
func (sd *StallDetector) Start(ctx context.Context) {
	ticker := time.NewTicker(sd.checkInterval)
	defer ticker.Stop()

	sd.logger.Info("stall detector started",
		"timeout", sd.stallTimeout,
		"interval", sd.checkInterval,
	)

	for {
		select {
		case <-ctx.Done():
			sd.logger.Info("stall detector stopped")
			return
		case <-ticker.C:
			sd.check(ctx)
		}
	}
}

// check queries all agent states and emits events for stalled agents.
func (sd *StallDetector) check(ctx context.Context) {
	agents, err := sd.repo.ListAgentStates(ctx)
	if err != nil {
		sd.logger.Error("stall detector: list agent states failed", "error", err)
		return
	}

	now := time.Now()
	for _, agent := range agents {
		state := State(agent.State)
		if !IsTransient(state) {
			continue
		}

		stuckDuration := now.Sub(agent.UpdatedAt)
		if stuckDuration <= sd.stallTimeout {
			continue
		}

		sd.logger.Warn("stall detected",
			"agent_id", agent.AgentID,
			"state", agent.State,
			"stuck_for", stuckDuration,
		)

		sd.bus.Publish(nervous.NewEvent(
			types.EventLifecycleStalled,
			"stall_detector",
			"global",
			map[string]any{
				"agent_id":  agent.AgentID,
				"state":     agent.State,
				"stuck_for": stuckDuration.String(),
			},
		))
	}
}
