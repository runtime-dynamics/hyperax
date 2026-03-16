//go:build !noguard

package guard

import (
	"context"
	"time"
)

// ApproveWritesGuard is a built-in guard that requires human approval
// for all write and delete tool actions.
type ApproveWritesGuard struct {
	timeout time.Duration
}

// NewApproveWritesGuard creates a guard that denies write/delete actions.
func NewApproveWritesGuard(timeout time.Duration) *ApproveWritesGuard {
	return &ApproveWritesGuard{timeout: timeout}
}

// Name returns the guard's identifier.
func (g *ApproveWritesGuard) Name() string {
	return "approve_writes"
}

// Evaluate returns false (deny) for write, delete, admin, and execute actions,
// requiring them to go through the approval flow. Reads are approved.
func (g *ApproveWritesGuard) Evaluate(_ context.Context, req *EvalRequest) (bool, error) {
	switch req.ToolAction {
	case "write", "delete", "admin", "execute":
		return false, nil // deny — require approval
	}
	return true, nil // approve reads
}

// Timeout returns the maximum time a pending action waits before auto-rejection.
func (g *ApproveWritesGuard) Timeout() time.Duration {
	return g.timeout
}
