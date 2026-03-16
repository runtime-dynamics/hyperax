//go:build !noguard

package guard

import (
	"context"
	"encoding/json"
	"time"
)

// Guard is the interface that guard plugins implement.
type Guard interface {
	Name() string
	Evaluate(ctx context.Context, req *EvalRequest) (bool, error) // true=approve, false=deny
	Timeout() time.Duration
}

// EvalRequest contains the tool call details for guard evaluation.
type EvalRequest struct {
	ToolName      string          `json:"tool_name"`
	ToolAction    string          `json:"tool_action"`
	ToolParams    json.RawMessage `json:"tool_params"`
	CallerPersona string          `json:"caller_persona"`
	Clearance     int             `json:"clearance"`
	GuardBypass   bool            `json:"guard_bypass"`
	TraceID       string          `json:"trace_id"`
}

// Context key for autonomous execution detection.
type contextKey string

const autonomousKey contextKey = "guard_autonomous"

// WithAutonomousContext marks the context as an autonomous (executor loop) call.
func WithAutonomousContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, autonomousKey, true)
}

// IsAutonomous reports whether this context originates from the autonomous executor loop.
func IsAutonomous(ctx context.Context) bool {
	v, _ := ctx.Value(autonomousKey).(bool)
	return v
}
