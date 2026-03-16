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
	// DefaultHeartbeatInterval is the interval at which active agents write heartbeats.
	DefaultHeartbeatInterval = 10 * time.Second

	// DefaultLeaseTTL is the maximum age of a heartbeat before the agent is
	// considered stale and transitioned to the error state.
	DefaultLeaseTTL = 30 * time.Second

	// DefaultLeaseCheckInterval is the interval between lease expiry checks.
	DefaultLeaseCheckInterval = 15 * time.Second
)

// HeartbeatMonitor manages heartbeat leases for active agents. It runs two
// concurrent loops:
//   - A writer loop that writes heartbeats for all locally registered agents
//     at a configurable interval (default 10s).
//   - A checker loop that detects agents whose heartbeat has expired beyond
//     the lease TTL (default 30s) and transitions them to the error state.
type HeartbeatMonitor struct {
	repo              repo.LifecycleRepo
	bus               *nervous.EventBus
	logger            *slog.Logger
	heartbeatInterval time.Duration
	leaseTTL          time.Duration
	checkInterval     time.Duration

	// localAgents tracks agent IDs that this process is responsible for
	// heartbeating. Add agents via Register, remove via Deregister.
	localAgents map[string]struct{}
}

// HeartbeatOption configures a HeartbeatMonitor.
type HeartbeatOption func(*HeartbeatMonitor)

// WithHeartbeatInterval sets the interval between heartbeat writes.
func WithHeartbeatInterval(d time.Duration) HeartbeatOption {
	return func(hm *HeartbeatMonitor) {
		if d > 0 {
			hm.heartbeatInterval = d
		}
	}
}

// WithLeaseTTL sets the maximum heartbeat age before an agent is considered stale.
func WithLeaseTTL(d time.Duration) HeartbeatOption {
	return func(hm *HeartbeatMonitor) {
		if d > 0 {
			hm.leaseTTL = d
		}
	}
}

// WithLeaseCheckInterval sets the interval between lease expiry checks.
func WithLeaseCheckInterval(d time.Duration) HeartbeatOption {
	return func(hm *HeartbeatMonitor) {
		if d > 0 {
			hm.checkInterval = d
		}
	}
}

// NewHeartbeatMonitor creates a HeartbeatMonitor with the given dependencies
// and optional configuration.
func NewHeartbeatMonitor(
	lifecycleRepo repo.LifecycleRepo,
	bus *nervous.EventBus,
	logger *slog.Logger,
	opts ...HeartbeatOption,
) *HeartbeatMonitor {
	hm := &HeartbeatMonitor{
		repo:              lifecycleRepo,
		bus:               bus,
		logger:            logger,
		heartbeatInterval: DefaultHeartbeatInterval,
		leaseTTL:          DefaultLeaseTTL,
		checkInterval:     DefaultLeaseCheckInterval,
		localAgents:       make(map[string]struct{}),
	}
	for _, opt := range opts {
		opt(hm)
	}
	return hm
}

// Register adds an agent ID to the set of locally managed agents whose
// heartbeats this monitor will write.
func (hm *HeartbeatMonitor) Register(agentID string) {
	hm.localAgents[agentID] = struct{}{}
}

// Deregister removes an agent ID from the set of locally managed agents.
func (hm *HeartbeatMonitor) Deregister(agentID string) {
	delete(hm.localAgents, agentID)
}

// Start runs both the heartbeat writer and the lease checker loops.
// Blocks until ctx is cancelled.
func (hm *HeartbeatMonitor) Start(ctx context.Context) {
	hm.logger.Info("heartbeat monitor started",
		"interval", hm.heartbeatInterval,
		"ttl", hm.leaseTTL,
		"check_interval", hm.checkInterval,
		"local_agents", len(hm.localAgents),
	)

	writerTicker := time.NewTicker(hm.heartbeatInterval)
	defer writerTicker.Stop()

	checkerTicker := time.NewTicker(hm.checkInterval)
	defer checkerTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			hm.logger.Info("heartbeat monitor stopped")
			return
		case <-writerTicker.C:
			hm.writeHeartbeats(ctx)
		case <-checkerTicker.C:
			hm.checkLeases(ctx)
		}
	}
}

// writeHeartbeats writes a heartbeat for each locally registered agent.
func (hm *HeartbeatMonitor) writeHeartbeats(ctx context.Context) {
	for agentID := range hm.localAgents {
		if err := hm.repo.WriteHeartbeat(ctx, agentID); err != nil {
			hm.logger.Error("heartbeat write failed",
				"agent_id", agentID,
				"error", err,
			)
		}
	}
}

// checkLeases queries for agents with expired heartbeats and transitions them
// to the error state. A lifecycle.transition event is logged for each
// expired agent, and a lifecycle.stalled event is published on the EventBus.
func (hm *HeartbeatMonitor) checkLeases(ctx context.Context) {
	staleAgents, err := hm.repo.GetStaleAgents(ctx, hm.leaseTTL)
	if err != nil {
		hm.logger.Error("lease check failed", "error", err)
		return
	}

	for _, agentID := range staleAgents {
		currentState, err := hm.repo.GetState(ctx, agentID)
		if err != nil {
			hm.logger.Error("get state for lease expiry failed",
				"agent_id", agentID,
				"error", err,
			)
			continue
		}

		// Only transition agents in active-like states. Agents already in
		// error, decommissioned, or halted states should not be re-transitioned.
		if !shouldTransitionOnLeaseExpiry(State(currentState)) {
			continue
		}

		// Validate the FSM transition before attempting it.
		if err := ValidateTransition(State(currentState), StateError); err != nil {
			hm.logger.Warn("cannot transition stale agent to error",
				"agent_id", agentID,
				"current_state", currentState,
				"error", err,
			)
			continue
		}

		transition := &repo.LifecycleTransition{
			AgentID:   agentID,
			FromState: currentState,
			ToState:   string(StateError),
			Reason:    "heartbeat lease expired",
		}
		if err := hm.repo.LogTransition(ctx, transition); err != nil {
			hm.logger.Error("transition on lease expiry failed",
				"agent_id", agentID,
				"error", err,
			)
			continue
		}

		hm.logger.Warn("agent lease expired, transitioned to error",
			"agent_id", agentID,
			"from_state", currentState,
		)

		hm.bus.Publish(nervous.NewEvent(
			types.EventLifecycleStalled,
			"heartbeat_monitor",
			"global",
			map[string]any{
				"agent_id":   agentID,
				"from_state": currentState,
				"to_state":   string(StateError),
				"reason":     "heartbeat lease expired",
			},
		))
	}
}

// shouldTransitionOnLeaseExpiry returns true if an agent in the given state
// should be transitioned to error when its heartbeat lease expires.
// Agents in terminal or already-errored states are left alone.
func shouldTransitionOnLeaseExpiry(s State) bool {
	switch s {
	case StateActive, StateOnboarding, StateRecovering, StateRehydrating, StateSuspended, StateDraining:
		return true
	default:
		return false
	}
}
