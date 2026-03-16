package interject

import (
	"log/slog"
	"sync"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

// timeNow is a function reference for time.Now, overridable in tests.
var timeNow = time.Now

// SafeModeState holds the state of Safe Mode for a single scope.
type SafeModeState struct {
	Scope           string    `json:"scope"`
	Active          bool      `json:"active"`
	InterjectionIDs []string  `json:"interjection_ids"`
	EngagedAt       time.Time `json:"engaged_at"`
}

// SafeModeController coordinates the Safe Mode state across scopes.
// When engaged, it signals subsystems (CommHub, Pulse, Pipeline) to
// enter a protective state. The controller maintains an in-memory map
// of halted scopes and the interjections that caused them.
//
// Subsystems integrate by calling IsHalted(scope) before performing work:
//   - CommHub: blocks Standard/Background messages when halted
//   - Pulse Engine: defers cadences for halted scopes
//   - Pipeline Executor: prevents new job starts in halted scopes
type SafeModeController struct {
	mgr    *Manager
	bus    *nervous.EventBus
	logger *slog.Logger
	states map[string]*SafeModeState
	mu     sync.RWMutex
}

// NewSafeModeController creates a SafeModeController.
func NewSafeModeController(mgr *Manager, bus *nervous.EventBus, logger *slog.Logger) *SafeModeController {
	return &SafeModeController{
		mgr:    mgr,
		bus:    bus,
		logger: logger.With("component", "safemode"),
		states: make(map[string]*SafeModeState),
	}
}

// Engage activates Safe Mode for a scope. If the scope is already halted,
// the interjection ID is appended (idempotent — multiple halts allowed).
func (c *SafeModeController) Engage(scope string, interjectionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, exists := c.states[scope]
	if exists {
		// Already halted — append the new interjection ID.
		for _, id := range state.InterjectionIDs {
			if id == interjectionID {
				return // already tracked
			}
		}
		state.InterjectionIDs = append(state.InterjectionIDs, interjectionID)
		c.logger.Warn("safe mode reinforced",
			"scope", scope,
			"interjection_id", interjectionID,
			"total_halts", len(state.InterjectionIDs),
		)
		return
	}

	c.states[scope] = &SafeModeState{
		Scope:           scope,
		Active:          true,
		InterjectionIDs: []string{interjectionID},
		EngagedAt:       timeNow(),
	}

	c.logger.Warn("safe mode engaged",
		"scope", scope,
		"interjection_id", interjectionID,
	)

	// Publish Safe Mode engaged event.
	if c.bus != nil {
		c.bus.Publish(nervous.NewEvent(
			types.EventInterjectSafeMode,
			"safemode",
			scope,
			map[string]any{
				"scope":           scope,
				"action":          "engaged",
				"interjection_id": interjectionID,
			},
		))
	}
}

// Disengage deactivates Safe Mode for a scope.
func (c *SafeModeController) Disengage(scope string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.states[scope]; !exists {
		return
	}

	delete(c.states, scope)

	c.logger.Info("safe mode disengaged", "scope", scope)
}

// IsHalted returns true if the given scope is in Safe Mode.
// Also returns true if a global halt is active (global affects all scopes).
func (c *SafeModeController) IsHalted(scope string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if state, ok := c.states[scope]; ok && state.Active {
		return true
	}
	// Global halt affects all scopes.
	if scope != string(types.ScopeGlobal) {
		if state, ok := c.states[string(types.ScopeGlobal)]; ok && state.Active {
			return true
		}
	}
	return false
}

// GetState returns the SafeModeState for a scope, or nil if not halted.
func (c *SafeModeController) GetState(scope string) *SafeModeState {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if state, ok := c.states[scope]; ok {
		cp := *state
		cp.InterjectionIDs = make([]string, len(state.InterjectionIDs))
		copy(cp.InterjectionIDs, state.InterjectionIDs)
		return &cp
	}
	return nil
}

// GetAllStates returns all currently active Safe Mode states.
func (c *SafeModeController) GetAllStates() []*SafeModeState {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var results []*SafeModeState
	for _, state := range c.states {
		cp := *state
		cp.InterjectionIDs = make([]string, len(state.InterjectionIDs))
		copy(cp.InterjectionIDs, state.InterjectionIDs)
		results = append(results, &cp)
	}
	return results
}

// HaltedScopes returns all scope names currently in Safe Mode.
func (c *SafeModeController) HaltedScopes() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	scopes := make([]string, 0, len(c.states))
	for scope := range c.states {
		scopes = append(scopes, scope)
	}
	return scopes
}
