package types

import "time"

// PulsePriority determines how a cadence is scheduled relative to system load.
// Background cadences are deferred under backpressure, Standard cadences run
// normally, and Urgent cadences are never deferred.
type PulsePriority string

const (
	PriorityBackground PulsePriority = "background"
	PriorityStandard   PulsePriority = "standard"
	PriorityUrgent     PulsePriority = "urgent"
)

// ValidPriority reports whether p is a recognized priority level.
func (p PulsePriority) Valid() bool {
	switch p {
	case PriorityBackground, PriorityStandard, PriorityUrgent:
		return true
	default:
		return false
	}
}

// CadenceMode determines how a cadence fires when it becomes due.
// ModeEvent (default) publishes a pulse.fire event on the EventBus.
// ModeAgentOrder routes a message to a target agent through CommHub.
type CadenceMode string

const (
	// ModeEvent is the default mode: publishes a pulse.fire event on the
	// nervous system EventBus.
	ModeEvent CadenceMode = "event"

	// ModeAgentOrder delivers a message to a target agent via CommHub when
	// the cadence fires. The message passes through the Context Sieve with
	// TrustInternal trust level and includes the cadence payload.
	ModeAgentOrder CadenceMode = "agent_order"
)

// Valid reports whether m is a recognized cadence mode.
func (m CadenceMode) Valid() bool {
	switch m {
	case ModeEvent, ModeAgentOrder, "":
		return true
	default:
		return false
	}
}

// Cadence represents a named periodic task managed by the Pulse Engine.
// Each cadence has a cron schedule, a priority level, and singleflight
// semantics (only one invocation at a time).
//
// When Mode is ModeAgentOrder, the cadence delivers a message to TargetAgent
// via CommHub instead of publishing a pulse.fire event on the EventBus.
type Cadence struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Schedule    string        `json:"schedule"`              // cron expression
	Priority    PulsePriority `json:"priority"`              // background | standard | urgent
	Mode        CadenceMode   `json:"mode"`                  // event (default) | agent_order
	TargetAgent string        `json:"target_agent,omitempty"` // required when mode=agent_order
	Enabled     bool          `json:"enabled"`               // whether the cadence is active
	Payload     any           `json:"payload,omitempty"`     // arbitrary data attached to fire events
	LastFired   *time.Time    `json:"last_fired,omitempty"`   // timestamp of last successful fire
	NextFire    *time.Time    `json:"next_fire,omitempty"`    // computed next fire time
	Running     bool          `json:"running"`                // singleflight: true while an invocation is in-flight
}
