//go:build !noguard

package guard

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/hyperax/hyperax/pkg/types"
)

// ConfigResolver resolves guard configuration values.
type ConfigResolver interface {
	Resolve(ctx context.Context, key, scope, scopeID string) (string, error)
}

// ToolActionLookup returns the RequiredAction string for a given tool name.
// Returns "" if the tool is not found.
type ToolActionLookup func(toolName string) string

// AuthExtractor extracts the AuthContext from a request context.
type AuthExtractor func(ctx context.Context) types.AuthContext

// GuardMiddleware intercepts tool dispatch calls and evaluates registered guards.
type GuardMiddleware struct {
	lookupAction   ToolActionLookup
	extractAuth    AuthExtractor
	actionManager  *ActionManager
	guards         []Guard
	skipList       map[string]bool
	configResolver ConfigResolver
	logger         *slog.Logger
}

// NewGuardMiddleware creates a guard middleware. The lookupAction function
// resolves a tool name to its ABAC action type, and extractAuth extracts
// the authenticated identity from the request context.
func NewGuardMiddleware(
	lookupAction ToolActionLookup,
	extractAuth AuthExtractor,
	actionManager *ActionManager,
	configResolver ConfigResolver,
	logger *slog.Logger,
) *GuardMiddleware {
	return &GuardMiddleware{
		lookupAction:   lookupAction,
		extractAuth:    extractAuth,
		actionManager:  actionManager,
		guards:         make([]Guard, 0),
		skipList: map[string]bool{
			"get_pending_actions": true,
			"approve_action":     true,
			"reject_action":      true,
			"get_action_detail":  true,
			"get_action_history": true,
		},
		configResolver: configResolver,
		logger:         logger,
	}
}

// RegisterGuard adds a guard to the middleware.
func (m *GuardMiddleware) RegisterGuard(g Guard) {
	m.guards = append(m.guards, g)
	m.logger.Info("guard registered", "name", g.Name())
}

// UnregisterGuard removes a guard by name. Returns true if a guard was removed.
func (m *GuardMiddleware) UnregisterGuard(name string) bool {
	for i, g := range m.guards {
		if g.Name() == name {
			m.guards = append(m.guards[:i], m.guards[i+1:]...)
			m.logger.Info("guard unregistered", "name", name)
			return true
		}
	}
	return false
}

// WrapDispatch returns a new dispatch function that evaluates guards before
// delegating to the original. Follows the same middleware pattern as ABAC.
func (m *GuardMiddleware) WrapDispatch(
	original func(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error),
) func(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error) {
	return func(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error) {
		// 1. Check if guard system is enabled.
		if !m.isEnabled(ctx) {
			return original(ctx, name, params)
		}

		// 2. Skip guard management tools to prevent deadlocks.
		if m.skipList[name] {
			return original(ctx, name, params)
		}

		// 3. Look up tool schema for action type.
		toolAction := m.lookupAction(name)
		if toolAction == "" {
			toolAction = "view"
		}

		// 4. Auto-approve observer views/reads if configured.
		if m.autoApproveReads(ctx) && (toolAction == "view" || toolAction == "read") {
			return original(ctx, name, params)
		}

		// 5. Check guard bypass.
		auth := m.extractAuth(ctx)
		if auth.GuardBypass && m.allowBypass(ctx) {
			return original(ctx, name, params)
		}

		// 6. Build eval request.
		req := &EvalRequest{
			ToolName:      name,
			ToolAction:    toolAction,
			ToolParams:    params,
			CallerPersona: auth.PersonaID,
			Clearance:     auth.ClearanceLevel,
			GuardBypass:   auth.GuardBypass,
		}

		// 7. Evaluate all guards. If all approve, pass through.
		var denyingGuard Guard
		for _, g := range m.guards {
			approved, err := g.Evaluate(ctx, req)
			if err != nil {
				m.logger.Warn("guard evaluation error",
					"guard", g.Name(),
					"tool", name,
					"error", err,
				)
				// Treat errors as denial (fail-closed).
				denyingGuard = g
				break
			}
			if !approved {
				denyingGuard = g
				break
			}
		}

		if denyingGuard == nil {
			// All guards approved.
			return original(ctx, name, params)
		}

		// 8. A guard denied — create pending action.
		timeout := denyingGuard.Timeout()
		if timeout <= 0 {
			timeout = 5 * time.Minute
		}
		actionID, ch := m.actionManager.CreatePending(req, denyingGuard.Name(), timeout)

		// 9. Autonomous vs Direct handling.
		if IsAutonomous(ctx) {
			// Block until approved/rejected/timeout.
			select {
			case decision := <-ch:
				if decision.Approved {
					return original(ctx, name, params)
				}
				return nil, fmt.Errorf("guard %q rejected tool %q: %s", denyingGuard.Name(), name, decision.Notes)
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		// Direct MCP call — return pending status immediately.
		result := map[string]any{
			"status":     "pending_approval",
			"action_id":  actionID,
			"guard":      denyingGuard.Name(),
			"tool":       name,
			"expires_at": time.Now().Add(timeout).Format(time.RFC3339),
		}
		resultJSON, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("guard.GuardMiddleware: marshal pending result: %w", err)
		}
		return &types.ToolResult{
			Content: []types.ToolContent{{Text: string(resultJSON)}},
		}, nil
	}
}

func (m *GuardMiddleware) isEnabled(ctx context.Context) bool {
	if m.configResolver == nil {
		return false
	}
	val, err := m.configResolver.Resolve(ctx, "guard.enabled", "", "")
	if err != nil {
		return false
	}
	return val == "true" || val == "1"
}

func (m *GuardMiddleware) autoApproveReads(ctx context.Context) bool {
	if m.configResolver == nil {
		return true
	}
	val, err := m.configResolver.Resolve(ctx, "guard.auto_approve_reads", "", "")
	if err != nil {
		return true // default: auto-approve reads
	}
	return val != "false" && val != "0"
}

func (m *GuardMiddleware) allowBypass(ctx context.Context) bool {
	if m.configResolver == nil {
		return false
	}
	val, err := m.configResolver.Resolve(ctx, "guard.allow_bypass", "", "")
	if err != nil {
		return false
	}
	return val == "true" || val == "1"
}
