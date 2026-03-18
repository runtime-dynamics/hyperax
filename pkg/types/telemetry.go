package types

import "time"

// Session represents an agent interaction session with telemetry data.
// Sessions track tool call counts, accumulated cost, and lifecycle status.
type Session struct {
	ID         string        `json:"id"`
	AgentID    string        `json:"agent_id"`
	ProviderID string        `json:"provider_id"` // provider used for this session
	Model      string        `json:"model"`       // model used for this session
	StartedAt  time.Time     `json:"started_at"`
	EndedAt    *time.Time    `json:"ended_at,omitempty"`
	Duration   time.Duration `json:"duration"`   // computed from StartedAt/EndedAt
	ToolCalls        int           `json:"tool_calls"`        // running count of tool invocations
	TotalCost        float64       `json:"total_cost"`        // accumulated estimated cost
	PromptTokens     int           `json:"prompt_tokens"`     // accumulated prompt/input tokens
	CompletionTokens int           `json:"completion_tokens"` // accumulated completion/output tokens
	TotalTokens      int           `json:"total_tokens"`      // accumulated total tokens (prompt + completion)
	Status           string        `json:"status"`            // "active", "completed", "abandoned"
	Metadata   string        `json:"metadata"`   // free-form JSON
	CreatedAt  time.Time     `json:"created_at"`
}

// ToolCallMetric captures a single tool invocation's performance and cost data.
type ToolCallMetric struct {
	ID         string        `json:"id"`
	SessionID  string        `json:"session_id"`
	ToolName   string        `json:"tool_name"`
	ProviderID string        `json:"provider_id,omitempty"` // provider that owns this session
	StartedAt  time.Time     `json:"started_at"`
	Duration   time.Duration `json:"duration"`
	Success    bool          `json:"success"`
	ErrorMsg   string        `json:"error_msg,omitempty"`
	InputSize  int64         `json:"input_size"`  // bytes
	OutputSize int64         `json:"output_size"` // bytes
	Cost       float64       `json:"cost"`        // estimated cost
}

// CostEstimate aggregates cost data for a specific tool over a time period.
type CostEstimate struct {
	ToolName    string        `json:"tool_name"`
	ProviderID  string        `json:"provider_id,omitempty"` // populated when grouping by provider
	CallCount   int64         `json:"call_count"`
	TotalCost   float64       `json:"total_cost"`
	AvgCost     float64       `json:"avg_cost"`
	AvgDuration time.Duration `json:"avg_duration"`
}

// Alert defines a threshold-based alerting rule that fires when a metric
// breaches the configured threshold within the specified time window.
type Alert struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Metric      string     `json:"metric"`    // "session_cost", "tool_calls", "error_rate", "duration"
	Operator    string     `json:"operator"`  // "gt", "lt", "gte", "lte", "eq"
	Threshold   float64    `json:"threshold"`
	Window      string     `json:"window"`   // "1h", "24h", "7d"
	Severity    string     `json:"severity"` // "info", "warning", "critical"
	Enabled     bool       `json:"enabled"`
	LastFiredAt *time.Time `json:"last_fired_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// AlertFiring represents an alert that has breached its threshold.
type AlertFiring struct {
	AlertID   string    `json:"alert_id"`
	AlertName string    `json:"alert_name"`
	Value     float64   `json:"value"`
	Threshold float64   `json:"threshold"`
	Severity  string    `json:"severity"`
	FiredAt   time.Time `json:"fired_at"`
}

// Telemetry event types for the Nervous System EventBus.
const (
	EventTelemetrySessionStart EventType = "telemetry.session.start"
	EventTelemetrySessionEnd   EventType = "telemetry.session.end"
	EventTelemetryToolCall     EventType = "telemetry.tool_call"
	EventTelemetryAlert        EventType = "telemetry.alert"
	EventTelemetryCostWarning  EventType = "telemetry.cost.warning"
)
