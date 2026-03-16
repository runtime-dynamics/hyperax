package nervous

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/hyperax/hyperax/pkg/types"
)

// NewEvent creates a NervousEvent with the current timestamp.
// SequenceID is set to 0 -- the EventBus stamps the actual value on Publish.
func NewEvent(eventType types.EventType, source, scope string, payload any) types.NervousEvent {
	var raw json.RawMessage
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			slog.Error("failed to marshal event payload — event will have empty payload", "event_type", eventType, "error", err)
		} else {
			raw = data
		}
	}
	return types.NervousEvent{
		Type:      eventType,
		Scope:     scope,
		Source:    source,
		Payload:   raw,
		Timestamp: time.Now(),
	}
}

// NewEventWithTrace creates a NervousEvent with an explicit trace ID.
func NewEventWithTrace(eventType types.EventType, source, scope, traceID string, payload any) types.NervousEvent {
	e := NewEvent(eventType, source, scope, payload)
	e.TraceID = traceID
	return e
}
