package workflow

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

// approvalWaiter holds the channel that the executor blocks on while waiting
// for a manual approval decision.
type approvalWaiter struct {
	RunID  string
	StepID string
	Ch     chan struct{}
}

// ApprovalManager manages approval gates for workflow steps that require
// manual intervention before proceeding. When the executor encounters a
// step with RequiresApproval=true, it registers a waiter and blocks
// until ApproveStep is called.
type ApprovalManager struct {
	mu      sync.Mutex
	waiters map[string]*approvalWaiter // key: "runID:stepID"
	bus     *nervous.EventBus
	logger  *slog.Logger
}

// NewApprovalManager creates an ApprovalManager wired to the event bus.
func NewApprovalManager(bus *nervous.EventBus, logger *slog.Logger) *ApprovalManager {
	return &ApprovalManager{
		waiters: make(map[string]*approvalWaiter),
		bus:     bus,
		logger:  logger,
	}
}

// waiterKey builds the map key for a run/step pair.
func waiterKey(runID, stepID string) string {
	return runID + ":" + stepID
}

// WaitForApproval registers a pending approval and publishes an approval
// event on the bus. It returns a channel that will be closed when the step
// is approved. The caller should select on this channel and the context's
// Done channel to handle cancellation.
func (m *ApprovalManager) WaitForApproval(runID, stepID, stepName string) <-chan struct{} {
	key := waiterKey(runID, stepID)

	m.mu.Lock()
	w := &approvalWaiter{
		RunID:  runID,
		StepID: stepID,
		Ch:     make(chan struct{}),
	}
	m.waiters[key] = w
	m.mu.Unlock()

	m.logger.Info("workflow step waiting for approval",
		"run_id", runID, "step_id", stepID, "step_name", stepName)

	// Publish approval pending event.
	payload, err := json.Marshal(map[string]string{
		"run_id":    runID,
		"step_id":   stepID,
		"step_name": stepName,
	})
	if err != nil {
		m.logger.Error("failed to marshal approval event payload", "error", err)
		return w.Ch
	}
	m.bus.Publish(types.NervousEvent{
		Type:      types.EventWorkflowApproval,
		Scope:     "workflow",
		Source:    "workflow.approval",
		Payload:   payload,
		Timestamp: time.Now(),
	})

	return w.Ch
}

// ApproveStep approves a step that is waiting for manual approval.
// It closes the waiter channel, unblocking the executor goroutine.
// Returns an error if no waiter is found for the given run/step pair.
func (m *ApprovalManager) ApproveStep(runID, stepID string) error {
	key := waiterKey(runID, stepID)

	m.mu.Lock()
	w, ok := m.waiters[key]
	if ok {
		delete(m.waiters, key)
	}
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("workflow.ApprovalManager.ApproveStep: no pending approval for run %q step %q", runID, stepID)
	}

	m.logger.Info("workflow step approved",
		"run_id", runID, "step_id", stepID)

	close(w.Ch)
	return nil
}

// PendingApprovals returns all currently pending approval keys.
// Useful for diagnostics and the get_workflow_status tool.
func (m *ApprovalManager) PendingApprovals() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	keys := make([]string, 0, len(m.waiters))
	for k := range m.waiters {
		keys = append(keys, k)
	}
	return keys
}

// CancelAll closes all pending approval waiters without approving them.
// Called during executor shutdown or workflow cancellation.
func (m *ApprovalManager) CancelAll(runID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, w := range m.waiters {
		if w.RunID == runID {
			close(w.Ch)
			delete(m.waiters, key)
		}
	}
}
