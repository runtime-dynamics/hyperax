package pulse

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/cron"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

const (
	// defaultTickInterval is the default interval between schedule checks.
	defaultTickInterval = 5 * time.Second

	// defaultBackpressureThreshold is the default subscriber queue depth
	// that triggers backpressure for Background cadences.
	defaultBackpressureThreshold = 100
)

// AgentOrderSender is the function signature for routing Agent Order cadence
// messages through CommHub. It is injected by the application wiring layer
// to decouple the Pulse Engine from the CommHub package.
type AgentOrderSender func(ctx context.Context, targetAgent, cadenceName, cadenceID string, payload any) error

// Engine is the Pulse Engine — a priority-aware event loop that manages
// cadences (named periodic tasks). It fires events on the EventBus when
// cadences are due, supports three priority levels, implements backpressure
// deferral for Background cadences, and provides singleflight deduplication
// so that a cadence is skipped if its previous invocation is still running.
//
// Cadences operate in two modes: Event mode (default) publishes pulse.fire
// events on the EventBus, while Agent Order mode delivers messages to a
// target agent through CommHub.
type Engine struct {
	cadences    map[string]*types.Cadence
	mu          sync.RWMutex
	bus         *nervous.EventBus
	logger      *slog.Logger
	tick        time.Duration
	backpressure *BackpressureMonitor
	startedAt   time.Time

	// nowFunc is used to obtain the current time. It defaults to time.Now
	// but can be overridden in tests for deterministic scheduling.
	nowFunc func() time.Time

	// lastHeartbeat stores the Unix nanosecond timestamp of the most recent
	// successful tick. The fail-closed watchdog reads this atomically to
	// detect a stalled Pulse Engine.
	lastHeartbeat atomic.Int64

	// agentOrderSender routes Agent Order cadence messages through CommHub.
	// When nil, Agent Order cadences log a warning and fall back to event mode.
	agentOrderSender AgentOrderSender
}

// NewEngine creates a Pulse Engine wired to the given EventBus.
// The engine does not start processing until Run is called.
func NewEngine(bus *nervous.EventBus, logger *slog.Logger) *Engine {
	return &Engine{
		cadences:    make(map[string]*types.Cadence),
		bus:         bus,
		logger:      logger.With("component", "pulse"),
		tick:        defaultTickInterval,
		backpressure: NewBackpressureMonitor(defaultBackpressureThreshold),
		nowFunc:     time.Now,
	}
}

// SetAgentOrderSender configures the function used to deliver Agent Order
// cadence messages through CommHub. When not set, Agent Order cadences
// fall back to the default event mode with a warning log.
func (e *Engine) SetAgentOrderSender(fn AgentOrderSender) {
	e.agentOrderSender = fn
}

// Run starts the pulse loop. It blocks until ctx is cancelled.
// On each tick it evaluates all enabled cadences and fires those
// whose NextFire time has arrived, subject to singleflight and
// backpressure constraints.
func (e *Engine) Run(ctx context.Context) {
	e.startedAt = e.nowFunc()
	e.lastHeartbeat.Store(e.startedAt.UnixNano())
	e.logger.Info("pulse engine started", "tick_interval", e.tick)

	ticker := time.NewTicker(e.tick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			e.logger.Info("pulse engine stopped")
			return
		case <-ticker.C:
			e.processTick()
		}
	}
}

// processTick evaluates all cadences and fires those that are due.
// It also updates the heartbeat timestamp read by the fail-closed watchdog.
func (e *Engine) processTick() {
	now := e.nowFunc()
	e.lastHeartbeat.Store(now.UnixNano())

	e.mu.RLock()
	// Take a snapshot of cadence IDs to avoid holding the lock during fire.
	ids := make([]string, 0, len(e.cadences))
	for id := range e.cadences {
		ids = append(ids, id)
	}
	e.mu.RUnlock()

	for _, id := range ids {
		e.evaluateCadence(id, now)
	}
}

// evaluateCadence checks a single cadence and fires it if due.
func (e *Engine) evaluateCadence(id string, now time.Time) {
	e.mu.Lock()
	c, ok := e.cadences[id]
	if !ok {
		e.mu.Unlock()
		return
	}

	// Skip disabled cadences.
	if !c.Enabled {
		e.mu.Unlock()
		return
	}

	// Skip if no NextFire is set (should not happen for valid cadences).
	if c.NextFire == nil {
		e.mu.Unlock()
		return
	}

	// Not yet due.
	if now.Before(*c.NextFire) {
		e.mu.Unlock()
		return
	}

	// Singleflight: skip if the previous invocation is still running.
	if c.Running {
		e.mu.Unlock()
		e.logger.Debug("cadence skipped (singleflight)", "id", id, "name", c.Name)
		e.publishSkipped(c.Name, id, "singleflight: previous invocation still running")
		return
	}

	// Backpressure: defer Background cadences when the system is overloaded.
	if c.Priority == types.PriorityBackground && e.backpressure.Check() {
		e.mu.Unlock()
		e.logger.Debug("cadence deferred (backpressure)", "id", id, "name", c.Name)
		e.publishBackpressure(c.Name, id)
		return
	}

	// Mark as running and record fire time.
	c.Running = true
	fired := now
	c.LastFired = &fired

	// Compute next fire time.
	sched, err := cron.Parse(c.Schedule)
	if err != nil {
		// This should not happen since we validated on create, but be defensive.
		c.Running = false
		e.mu.Unlock()
		e.logger.Error("cadence schedule parse error", "id", id, "error", err)
		e.publishError(c.Name, id, fmt.Sprintf("schedule parse error: %v", err))
		return
	}
	next := sched.NextAfter(now)
	c.NextFire = &next

	// Snapshot fields for the event while we hold the lock.
	name := c.Name
	priority := c.Priority
	payload := c.Payload
	mode := c.Mode
	targetAgent := c.TargetAgent
	e.mu.Unlock()

	// Dispatch based on cadence mode.
	if mode == types.ModeAgentOrder && targetAgent != "" {
		e.dispatchAgentOrder(id, name, targetAgent, payload)
	} else {
		e.publishFire(name, id, priority, payload)
	}

	// Mark as no longer running. In a real system this would be done by
	// the consumer acknowledging completion, but for Phase 1 in-memory
	// cadences we mark it immediately after fire.
	e.mu.Lock()
	if c2, ok := e.cadences[id]; ok {
		c2.Running = false
	}
	e.mu.Unlock()
}

// dispatchAgentOrder routes an Agent Order cadence message through CommHub.
// If no AgentOrderSender is configured, it falls back to the default event
// mode with a warning log.
func (e *Engine) dispatchAgentOrder(cadenceID, cadenceName, targetAgent string, payload any) {
	if e.agentOrderSender == nil {
		e.logger.Warn("agent order cadence has no sender configured, falling back to event mode",
			"id", cadenceID,
			"name", cadenceName,
			"target_agent", targetAgent,
		)
		e.publishFire(cadenceName, cadenceID, types.PriorityStandard, payload)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.agentOrderSender(ctx, targetAgent, cadenceName, cadenceID, payload); err != nil {
		e.logger.Error("agent order dispatch failed",
			"id", cadenceID,
			"name", cadenceName,
			"target_agent", targetAgent,
			"error", err,
		)
		e.publishError(cadenceName, cadenceID, fmt.Sprintf("agent order dispatch failed: %v", err))
		return
	}

	e.logger.Info("agent order dispatched",
		"id", cadenceID,
		"name", cadenceName,
		"target_agent", targetAgent,
	)
}

// publishFire publishes a pulse.fire event on the bus.
func (e *Engine) publishFire(name, id string, priority types.PulsePriority, payload any) {
	data := map[string]any{
		"cadence_id":   id,
		"cadence_name": name,
		"priority":     string(priority),
	}
	if payload != nil {
		data["payload"] = payload
	}
	raw, err := json.Marshal(data)
	if err != nil {
		e.logger.Error("publishFire: failed to marshal payload", "cadence_id", id, "error", err)
		return
	}
	e.bus.Publish(types.NervousEvent{
		Type:      types.EventPulseFire,
		Scope:     "system",
		Source:    "pulse",
		Payload:   raw,
		Timestamp: e.nowFunc(),
	})
}

// publishSkipped publishes a pulse.skipped event on the bus.
func (e *Engine) publishSkipped(name, id, reason string) {
	raw, err := json.Marshal(map[string]string{
		"cadence_id":   id,
		"cadence_name": name,
		"reason":       reason,
	})
	if err != nil {
		e.logger.Error("publishSkipped: failed to marshal payload", "cadence_id", id, "error", err)
		return
	}
	e.bus.Publish(types.NervousEvent{
		Type:      types.EventPulseSkipped,
		Scope:     "system",
		Source:    "pulse",
		Payload:   raw,
		Timestamp: e.nowFunc(),
	})
}

// publishBackpressure publishes a pulse.backpressure event on the bus.
func (e *Engine) publishBackpressure(name, id string) {
	raw, err := json.Marshal(map[string]string{
		"cadence_id":   id,
		"cadence_name": name,
		"reason":       "background cadence deferred due to backpressure",
	})
	if err != nil {
		e.logger.Error("publishBackpressure: failed to marshal payload", "cadence_id", id, "error", err)
		return
	}
	e.bus.Publish(types.NervousEvent{
		Type:      types.EventPulseBackpressure,
		Scope:     "system",
		Source:    "pulse",
		Payload:   raw,
		Timestamp: e.nowFunc(),
	})
}

// publishError publishes a pulse.error event on the bus.
func (e *Engine) publishError(name, id, message string) {
	raw, err := json.Marshal(map[string]string{
		"cadence_id":   id,
		"cadence_name": name,
		"error":        message,
	})
	if err != nil {
		e.logger.Error("publishError: failed to marshal payload", "cadence_id", id, "error", err)
		return
	}
	e.bus.Publish(types.NervousEvent{
		Type:      types.EventPulseError,
		Scope:     "system",
		Source:    "pulse",
		Payload:   raw,
		Timestamp: e.nowFunc(),
	})
}

// CreateCadence adds a new cadence to the engine. The schedule is parsed
// immediately to compute the first NextFire time. Returns the created cadence
// or an error if the schedule is invalid or the name is empty.
func (e *Engine) CreateCadence(name, schedule string, priority types.PulsePriority, payload any) (*types.Cadence, error) {
	return e.CreateCadenceWithMode(name, schedule, priority, types.ModeEvent, "", payload)
}

// CreateCadenceWithMode adds a cadence with an explicit execution mode.
// When mode is ModeAgentOrder, targetAgent must be non-empty. The schedule
// is parsed immediately to compute the first NextFire time.
func (e *Engine) CreateCadenceWithMode(name, schedule string, priority types.PulsePriority, mode types.CadenceMode, targetAgent string, payload any) (*types.Cadence, error) {
	if name == "" {
		return nil, fmt.Errorf("cadence name is required")
	}
	if schedule == "" {
		return nil, fmt.Errorf("cadence schedule is required")
	}
	if !priority.Valid() {
		return nil, fmt.Errorf("invalid priority %q: must be background, standard, or urgent", priority)
	}
	if !mode.Valid() {
		return nil, fmt.Errorf("invalid mode %q: must be event or agent_order", mode)
	}
	// Default to event mode.
	if mode == "" {
		mode = types.ModeEvent
	}
	if mode == types.ModeAgentOrder && targetAgent == "" {
		return nil, fmt.Errorf("target_agent is required for agent_order mode")
	}

	sched, err := cron.Parse(schedule)
	if err != nil {
		return nil, fmt.Errorf("invalid schedule %q: %w", schedule, err)
	}

	now := e.nowFunc()
	next := sched.NextAfter(now)

	c := &types.Cadence{
		ID:          uuid.New().String(),
		Name:        name,
		Schedule:    schedule,
		Priority:    priority,
		Mode:        mode,
		TargetAgent: targetAgent,
		Enabled:     true,
		Payload:     payload,
		NextFire:    &next,
	}

	e.mu.Lock()
	e.cadences[c.ID] = c
	e.mu.Unlock()

	e.logger.Info("cadence created",
		"id", c.ID,
		"name", name,
		"schedule", schedule,
		"priority", priority,
		"mode", mode,
		"target_agent", targetAgent,
		"next_fire", next,
	)
	return c, nil
}

// UpdateCadence updates the name, schedule, and priority of an existing cadence.
// If the schedule changes, NextFire is recomputed. Returns an error if the
// cadence is not found or the new schedule is invalid.
func (e *Engine) UpdateCadence(id string, name, schedule string, priority types.PulsePriority) error {
	if id == "" {
		return fmt.Errorf("cadence id is required")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	c, ok := e.cadences[id]
	if !ok {
		return fmt.Errorf("cadence %q not found", id)
	}

	if name != "" {
		c.Name = name
	}

	if schedule != "" && schedule != c.Schedule {
		sched, err := cron.Parse(schedule)
		if err != nil {
			return fmt.Errorf("invalid schedule %q: %w", schedule, err)
		}
		c.Schedule = schedule
		now := e.nowFunc()
		next := sched.NextAfter(now)
		c.NextFire = &next
	}

	if priority != "" {
		if !priority.Valid() {
			return fmt.Errorf("invalid priority %q: must be background, standard, or urgent", priority)
		}
		c.Priority = priority
	}

	e.logger.Info("cadence updated", "id", id, "name", c.Name, "schedule", c.Schedule, "priority", c.Priority)
	return nil
}

// DeleteCadence removes a cadence by ID. Returns an error if not found.
func (e *Engine) DeleteCadence(id string) error {
	if id == "" {
		return fmt.Errorf("cadence id is required")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if _, ok := e.cadences[id]; !ok {
		return fmt.Errorf("cadence %q not found", id)
	}
	delete(e.cadences, id)

	e.logger.Info("cadence deleted", "id", id)
	return nil
}

// ListCadences returns a snapshot copy of all cadences. The caller may
// safely read the returned slice without holding any locks.
func (e *Engine) ListCadences() []types.Cadence {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make([]types.Cadence, 0, len(e.cadences))
	for _, c := range e.cadences {
		result = append(result, *c)
	}
	return result
}

// GetCadence returns a copy of a single cadence by ID.
// Returns an error if the cadence is not found.
func (e *Engine) GetCadence(id string) (*types.Cadence, error) {
	if id == "" {
		return nil, fmt.Errorf("cadence id is required")
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	c, ok := e.cadences[id]
	if !ok {
		return nil, fmt.Errorf("cadence %q not found", id)
	}

	copy := *c
	return &copy, nil
}

// FireEvent manually publishes an event on the EventBus. This is used by
// the fire_event MCP tool to let agents inject arbitrary events into the
// nervous system.
func (e *Engine) FireEvent(eventType types.EventType, source string, payload any) error {
	var raw json.RawMessage
	if payload != nil {
		var err error
		raw, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("pulse.Engine.FireEvent: marshal payload: %w", err)
		}
	}

	e.bus.Publish(types.NervousEvent{
		Type:      eventType,
		Scope:     "system",
		Source:    source,
		Payload:   raw,
		Timestamp: e.nowFunc(),
	})

	e.logger.Info("event fired manually", "type", eventType, "source", source)
	return nil
}

// GetStatus returns engine health information including cadence counts,
// backpressure state, uptime, and tick interval.
func (e *Engine) GetStatus() map[string]any {
	e.mu.RLock()
	total := len(e.cadences)
	enabled := 0
	running := 0
	for _, c := range e.cadences {
		if c.Enabled {
			enabled++
		}
		if c.Running {
			running++
		}
	}
	e.mu.RUnlock()

	status := map[string]any{
		"total_cadences":   total,
		"enabled_cadences": enabled,
		"running_cadences": running,
		"backpressure":     e.backpressure.Check(),
		"tick_interval":    e.tick.String(),
	}

	if !e.startedAt.IsZero() {
		status["uptime"] = e.nowFunc().Sub(e.startedAt).String()
		status["started_at"] = e.startedAt.Format(time.RFC3339)
	}

	return status
}

// Backpressure returns the engine's backpressure monitor, allowing external
// callers to set or query the backpressure state.
func (e *Engine) Backpressure() *BackpressureMonitor {
	return e.backpressure
}

// LastHeartbeat returns the time of the most recent successful tick.
// Returns zero time if the engine has not started yet.
func (e *Engine) LastHeartbeat() time.Time {
	ns := e.lastHeartbeat.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}
