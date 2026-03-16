//go:build noguard

package guard

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

// Guard is the interface that guard plugins implement (stub).
type Guard interface {
	Name() string
	Evaluate(ctx context.Context, req *EvalRequest) (bool, error)
	Timeout() time.Duration
}

// EvalRequest contains the tool call details for guard evaluation (stub).
type EvalRequest struct {
	ToolName      string          `json:"tool_name"`
	ToolAction    string          `json:"tool_action"`
	ToolParams    json.RawMessage `json:"tool_params"`
	CallerPersona string          `json:"caller_persona"`
	Clearance     int             `json:"clearance"`
	GuardBypass   bool            `json:"guard_bypass"`
	TraceID       string          `json:"trace_id"`
}

// ConfigResolver resolves guard configuration values (stub).
type ConfigResolver interface {
	Resolve(ctx context.Context, key, scope, scopeID string) (string, error)
}

// ToolActionLookup returns the RequiredAction string for a given tool name (stub).
type ToolActionLookup func(toolName string) string

// AuthExtractor extracts the AuthContext from a request context (stub).
type AuthExtractor func(ctx context.Context) types.AuthContext

// WithAutonomousContext is a no-op in the stub build.
func WithAutonomousContext(ctx context.Context) context.Context {
	return ctx
}

// IsAutonomous always returns false in the stub build.
func IsAutonomous(_ context.Context) bool {
	return false
}

// ActionDecision carries the approval/rejection decision (stub).
type ActionDecision struct {
	Approved  bool
	DecidedBy string
	Notes     string
}

// ActionManager is a no-op stub.
type ActionManager struct{}

// NewActionManager creates a no-op ActionManager (stub build).
func NewActionManager(_ *nervous.EventBus, _ *slog.Logger) *ActionManager {
	return &ActionManager{}
}

// CreatePending immediately returns an approved decision (stub build).
func (m *ActionManager) CreatePending(_ *EvalRequest, _ string, _ time.Duration) (string, <-chan ActionDecision) {
	ch := make(chan ActionDecision, 1)
	ch <- ActionDecision{Approved: true}
	close(ch)
	return "", ch
}

// Approve is a no-op in the stub build.
func (m *ActionManager) Approve(_, _, _ string) (*types.GuardAction, error) { return nil, nil }

// Reject is a no-op in the stub build.
func (m *ActionManager) Reject(_, _, _ string) (*types.GuardAction, error) { return nil, nil }

// PendingActions returns nil in the stub build.
func (m *ActionManager) PendingActions() []types.GuardAction { return nil }

// GetAction returns nil in the stub build.
func (m *ActionManager) GetAction(_ string) (*types.GuardAction, error) { return nil, nil }

// History returns nil in the stub build.
func (m *ActionManager) History(_ int) []types.GuardAction { return nil }

// GuardMiddleware is a no-op stub.
type GuardMiddleware struct{}

// NewGuardMiddleware creates a no-op GuardMiddleware (stub build).
// NOTE: This binary was compiled with the "noguard" build tag. The guard/action
// approval system is disabled. Tool calls will NOT require approval, and guard
// plugins will not be evaluated. To enable guards, use the standard build.
func NewGuardMiddleware(_ ToolActionLookup, _ AuthExtractor, _ *ActionManager, _ ConfigResolver, logger *slog.Logger) *GuardMiddleware {
	if logger != nil {
		logger.Warn("guard system disabled (compiled with noguard build tag)")
	}
	return &GuardMiddleware{}
}

// RegisterGuard is a no-op in the stub build.
func (m *GuardMiddleware) RegisterGuard(_ Guard) {}

// UnregisterGuard is a no-op in the stub build.
func (m *GuardMiddleware) UnregisterGuard(_ string) bool { return false }

// WrapDispatch returns the original dispatch function unchanged (stub build).
func (m *GuardMiddleware) WrapDispatch(
	original func(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error),
) func(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error) {
	return original
}

// ApproveWritesGuard stub.
type ApproveWritesGuard struct{}

// NewApproveWritesGuard creates a no-op ApproveWritesGuard (stub build).
func NewApproveWritesGuard(_ time.Duration) *ApproveWritesGuard {
	return &ApproveWritesGuard{}
}

// Name returns the guard identifier (stub build).
func (g *ApproveWritesGuard) Name() string { return "approve_writes" }

// Evaluate always approves in the stub build.
func (g *ApproveWritesGuard) Evaluate(_ context.Context, _ *EvalRequest) (bool, error) {
	return true, nil
}

// Timeout returns zero in the stub build.
func (g *ApproveWritesGuard) Timeout() time.Duration { return 0 }
