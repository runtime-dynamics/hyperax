package types

import "time"

// SensorCadenceType identifies a cadence as a sensor (mechanical polling) rather
// than an agent-driven cadence. Sensor cadences execute HTTP requests or shell
// commands directly and evaluate responses against match criteria to inject events.
const SensorCadenceType = "sensor"

// SensorAction defines what a sensor cadence executes on each tick.
type SensorAction struct {
	// Type is the action type: "http" or "shell".
	Type string `json:"type"` // "http" | "shell"

	// URL is the HTTP endpoint for "http" actions.
	URL string `json:"url,omitempty"`

	// Method is the HTTP method (GET, POST, etc.). Defaults to "GET".
	Method string `json:"method,omitempty"`

	// Headers are additional HTTP headers. Values starting with "secret:" are
	// resolved from the SecretRepo at execution time.
	Headers map[string]string `json:"headers,omitempty"`

	// Body is the request body for POST/PUT requests.
	Body string `json:"body,omitempty"`

	// Command is the shell command for "shell" actions.
	Command string `json:"command,omitempty"`

	// TimeoutSeconds is the maximum execution time. Defaults to 10.
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

// MatchCriteria defines a JSONPath-based evaluation rule applied to sensor responses.
// When the condition is met, the sensor injects an event into the EventBus.
type MatchCriteria struct {
	// JSONPath is the JSONPath expression to extract a value from the response.
	// Example: "$.status", "$.data.items[0].count", "$.healthy"
	JSONPath string `json:"jsonpath"`

	// Operator is the comparison operator.
	// Supported: eq, ne, gt, lt, gte, lte, contains, matches (regex).
	Operator string `json:"operator"`

	// Value is the comparison target. Type coercion is applied based on the
	// extracted value type: string, float64, bool.
	Value string `json:"value"`
}

// SensorEventConfig defines the event to inject when match criteria are met.
type SensorEventConfig struct {
	// EventType is the type of event to publish on match.
	EventType EventType `json:"event_type"`

	// PayloadFrom is an optional JSONPath expression to extract a subset of
	// the response as the event payload. If empty, the full response is used.
	PayloadFrom string `json:"payload_from,omitempty"`
}

// SensorCadence is a mechanical polling cadence that executes HTTP or shell
// commands, evaluates responses against match criteria, and injects events.
// It extends the base Cadence with sensor-specific configuration.
type SensorCadence struct {
	// ID is the unique identifier.
	ID string `json:"id"`

	// Name is a human-readable name for the sensor.
	Name string `json:"name"`

	// Schedule is the cron expression controlling execution frequency.
	Schedule string `json:"schedule"`

	// Enabled controls whether the sensor is active.
	Enabled bool `json:"enabled"`

	// Action defines the HTTP or shell command to execute.
	Action SensorAction `json:"action"`

	// Criteria is the list of match criteria to evaluate against responses.
	// All criteria must match (AND logic) for the event to fire.
	Criteria []MatchCriteria `json:"criteria"`

	// Event defines the event to inject when criteria are met.
	Event SensorEventConfig `json:"event"`

	// LastFired is the last time this sensor executed successfully.
	LastFired *time.Time `json:"last_fired,omitempty"`

	// LastResult holds the response body from the most recent execution.
	LastResult string `json:"last_result,omitempty"`

	// LastMatched indicates whether the last execution matched the criteria.
	LastMatched bool `json:"last_matched"`

	// NextFire is the computed next execution time.
	NextFire *time.Time `json:"next_fire,omitempty"`

	// CreatedAt is the creation timestamp.
	CreatedAt time.Time `json:"created_at"`
}

// Sensor event types for the Nervous System.
const (
	EventSensorFire    EventType = "sensor.fire"
	EventSensorMatch   EventType = "sensor.match"
	EventSensorError   EventType = "sensor.error"
	EventSensorTimeout EventType = "sensor.timeout"
)
