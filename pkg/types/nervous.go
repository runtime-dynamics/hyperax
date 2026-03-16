package types

import "time"

// DomainEvent is a persisted event for query and replay.
// It represents the canonical record of an event that flowed through the
// Nervous System, stored in SQLite for auditing, replay, and analytics.
type DomainEvent struct {
	ID         string    `json:"id"`
	EventType  EventType `json:"event_type"`
	Source     string    `json:"source"`
	Scope      string    `json:"scope"`
	Payload    string    `json:"payload"`     // JSON string
	TraceID    string    `json:"trace_id"`
	SequenceID uint64    `json:"sequence_id"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"` // 7-day retention default
}

// EventHandler is a declarative subscription configured from the dashboard.
// When an event matching EventFilter is published, the Action is executed
// with ActionPayload as its configuration.
type EventHandler struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	EventFilter   string    `json:"event_filter"`   // glob pattern like "pipeline.*"
	Action        string    `json:"action"`          // "tool_call", "webhook", "log"
	ActionPayload string    `json:"action_payload"`  // JSON config for the action
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// EventSubscription is the runtime subscription state for a subscriber
// connected to the EventBus with a particular filter.
type EventSubscription struct {
	ID           string    `json:"id"`
	SubscriberID string    `json:"subscriber_id"`
	EventFilter  string    `json:"event_filter"`
	CreatedAt    time.Time `json:"created_at"`
}
