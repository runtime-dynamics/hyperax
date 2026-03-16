package lifecycle

import "fmt"

// State represents an agent lifecycle state.
type State string

const (
	StatePending        State = "pending"
	StateOnboarding     State = "onboarding"
	StateActive         State = "active"
	StateSuspended      State = "suspended"
	StateHalted         State = "halted"
	StateDraining       State = "draining"
	StateDecommissioned State = "decommissioned"
	StateError          State = "error"
	StateRecovering     State = "recovering"
	StateRehydrating    State = "rehydrating"
)

// transitions defines the set of valid state transitions.
// Key: source state. Value: set of allowed destination states.
var transitions = map[State]map[State]bool{
	StatePending: {
		StateOnboarding: true,
	},
	StateOnboarding: {
		StateActive: true,
		StateError:  true,
	},
	StateActive: {
		StateSuspended: true,
		StateHalted:    true,
		StateDraining:  true,
		StateError:     true,
	},
	StateSuspended: {
		StateActive: true,
		StateError:  true,
	},
	StateHalted: {
		StateRecovering: true,
		StateError:      true,
	},
	StateDraining: {
		StateDecommissioned: true,
		StateError:          true,
	},
	StateError: {
		StateRecovering:  true,
		StateRehydrating: true,
	},
	StateRecovering: {
		StateActive: true,
		StateError:  true,
	},
	StateRehydrating: {
		StateActive:     true,
		StateOnboarding: true, // fallback when no checkpoint exists
		StateError:      true,
	},
}

// transientStates are states that should be monitored for stall detection.
// An agent stuck in one of these states beyond a timeout is considered stalled.
var transientStates = []State{
	StateOnboarding,
	StateRecovering,
	StateDraining,
	StateRehydrating,
}

// ValidateTransition checks whether transitioning from `from` to `to` is
// permitted. Returns nil if valid, or a descriptive error if the transition
// is not allowed.
func ValidateTransition(from, to State) error {
	allowed, ok := transitions[from]
	if !ok {
		return fmt.Errorf("unknown source state %q", from)
	}
	if !allowed[to] {
		return fmt.Errorf("invalid transition from %q to %q", from, to)
	}
	return nil
}

// TransientStates returns the set of states that should be monitored for
// stall detection. An agent remaining in a transient state beyond the
// configured timeout indicates a problem.
func TransientStates() []State {
	result := make([]State, len(transientStates))
	copy(result, transientStates)
	return result
}

// IsTransient returns true if the given state is a transient state.
func IsTransient(s State) bool {
	for _, ts := range transientStates {
		if ts == s {
			return true
		}
	}
	return false
}
