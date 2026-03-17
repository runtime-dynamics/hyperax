//go:build !noguard

package guard

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

// ActionDecision carries the approval/rejection decision for a pending action.
type ActionDecision struct {
	Approved  bool
	DecidedBy string
	Notes     string
}

// pendingAction wraps a GuardAction with its decision channel and timeout timer.
type pendingAction struct {
	Action types.GuardAction
	Ch     chan ActionDecision
	Timer  *time.Timer
}

// ActionManager manages pending guard actions, providing channel-based blocking
// for autonomous callers and polling for direct MCP callers.
type ActionManager struct {
	mu      sync.Mutex
	pending map[string]*pendingAction
	history []types.GuardAction
	bus     *nervous.EventBus
	logger  *slog.Logger
}

// NewActionManager creates an ActionManager wired to the event bus.
func NewActionManager(bus *nervous.EventBus, logger *slog.Logger) *ActionManager {
	return &ActionManager{
		pending: make(map[string]*pendingAction),
		history: make([]types.GuardAction, 0, 256),
		bus:     bus,
		logger:  logger,
	}
}

// generateActionID creates a random 16-hex-char action ID.
func generateActionID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// CreatePending creates a pending action and returns its decision channel and ID.
// The action will automatically time out after the specified duration.
func (m *ActionManager) CreatePending(req *EvalRequest, guardName string, timeout time.Duration) (string, <-chan ActionDecision) {
	id := generateActionID()
	now := time.Now()

	action := types.GuardAction{
		ID:            id,
		ToolName:      req.ToolName,
		ToolAction:    req.ToolAction,
		ToolParams:    string(req.ToolParams),
		GuardName:     guardName,
		CallerPersona: req.CallerPersona,
		Status:        types.GuardStatusPending,
		CreatedAt:     now,
		ExpiresAt:     now.Add(timeout),
		TraceID:       req.TraceID,
	}

	ch := make(chan ActionDecision, 1)
	timer := time.AfterFunc(timeout, func() {
		m.timeoutAction(id)
	})

	pa := &pendingAction{
		Action: action,
		Ch:     ch,
		Timer:  timer,
	}

	m.mu.Lock()
	m.pending[id] = pa
	m.mu.Unlock()

	m.logger.Info("guard action pending",
		"id", id,
		"tool", req.ToolName,
		"guard", guardName,
		"timeout", timeout,
	)

	// Publish pending event.
	m.publishEvent(types.EventGuardPending, action)

	return id, ch
}

// Approve approves a pending action, unblocking the caller.
func (m *ActionManager) Approve(id, decidedBy, notes string) (*types.GuardAction, error) {
	return m.decide(id, true, decidedBy, notes)
}

// Reject rejects a pending action, unblocking the caller.
func (m *ActionManager) Reject(id, decidedBy, notes string) (*types.GuardAction, error) {
	return m.decide(id, false, decidedBy, notes)
}

func (m *ActionManager) decide(id string, approved bool, decidedBy, notes string) (*types.GuardAction, error) {
	m.mu.Lock()
	pa, ok := m.pending[id]
	if !ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("guard.ActionManager.decide: no pending action with id %q", id)
	}
	delete(m.pending, id)
	pa.Timer.Stop()

	now := time.Now()
	if approved {
		pa.Action.Status = types.GuardStatusApproved
	} else {
		pa.Action.Status = types.GuardStatusRejected
	}
	pa.Action.DecidedBy = decidedBy
	pa.Action.Notes = notes
	pa.Action.DecidedAt = &now

	m.history = append(m.history, pa.Action)
	if len(m.history) > 1000 {
		m.history = m.history[len(m.history)-500:]
	}
	m.mu.Unlock()

	// Signal the caller.
	pa.Ch <- ActionDecision{
		Approved:  approved,
		DecidedBy: decidedBy,
		Notes:     notes,
	}
	close(pa.Ch)

	eventType := types.EventGuardApproved
	if !approved {
		eventType = types.EventGuardRejected
	}
	m.publishEvent(eventType, pa.Action)
	m.logger.Info("guard action decided",
		"id", id,
		"approved", approved,
		"decided_by", decidedBy,
	)

	return &pa.Action, nil
}

func (m *ActionManager) timeoutAction(id string) {
	m.mu.Lock()
	pa, ok := m.pending[id]
	if !ok {
		m.mu.Unlock()
		return
	}
	delete(m.pending, id)

	now := time.Now()
	pa.Action.Status = types.GuardStatusTimeout
	pa.Action.DecidedAt = &now
	m.history = append(m.history, pa.Action)
	if len(m.history) > 1000 {
		m.history = m.history[len(m.history)-500:]
	}
	m.mu.Unlock()

	// Signal the caller with a rejection (timeout = rejection).
	pa.Ch <- ActionDecision{Approved: false, Notes: "timeout"}
	close(pa.Ch)

	m.publishEvent(types.EventGuardTimeout, pa.Action)
	m.logger.Warn("guard action timed out", "id", id, "tool", pa.Action.ToolName)
}

// PendingActions returns all currently pending actions.
func (m *ActionManager) PendingActions() []types.GuardAction {
	m.mu.Lock()
	defer m.mu.Unlock()

	actions := make([]types.GuardAction, 0, len(m.pending))
	for _, pa := range m.pending {
		actions = append(actions, pa.Action)
	}
	return actions
}

// GetAction returns a specific action by ID (pending or from history).
func (m *ActionManager) GetAction(id string) (*types.GuardAction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if pa, ok := m.pending[id]; ok {
		action := pa.Action
		return &action, nil
	}

	for i := len(m.history) - 1; i >= 0; i-- {
		if m.history[i].ID == id {
			action := m.history[i]
			return &action, nil
		}
	}

	return nil, fmt.Errorf("guard.ActionManager.GetAction: action %q not found", id)
}

// History returns the most recent decided actions (up to limit).
func (m *ActionManager) History(limit int) []types.GuardAction {
	m.mu.Lock()
	defer m.mu.Unlock()

	if limit <= 0 || limit > len(m.history) {
		limit = len(m.history)
	}

	result := make([]types.GuardAction, limit)
	// Return most recent first.
	for i := 0; i < limit; i++ {
		result[i] = m.history[len(m.history)-1-i]
	}
	return result
}

func (m *ActionManager) publishEvent(eventType types.EventType, action types.GuardAction) {
	if m.bus == nil {
		return
	}
	payload, err := json.Marshal(action)
	if err != nil {
		return
	}
	m.bus.Publish(types.NervousEvent{
		Type:      eventType,
		Scope:     "guard",
		Source:    "guard.action_manager",
		Payload:   payload,
		TraceID:   action.TraceID,
		Timestamp: time.Now(),
	})
}
