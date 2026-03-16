package lifecycle

import (
	"testing"
)

func TestValidateTransition_ValidTransitions(t *testing.T) {
	valid := []struct {
		from, to State
	}{
		{StatePending, StateOnboarding},
		{StateOnboarding, StateActive},
		{StateOnboarding, StateError},
		{StateActive, StateSuspended},
		{StateActive, StateHalted},
		{StateActive, StateDraining},
		{StateActive, StateError},
		{StateSuspended, StateActive},
		{StateSuspended, StateError},
		{StateHalted, StateRecovering},
		{StateHalted, StateError},
		{StateDraining, StateDecommissioned},
		{StateDraining, StateError},
		{StateError, StateRecovering},
		{StateRecovering, StateActive},
		{StateRecovering, StateError},
		{StateRehydrating, StateActive},
		{StateRehydrating, StateError},
	}

	for _, tc := range valid {
		if err := ValidateTransition(tc.from, tc.to); err != nil {
			t.Errorf("expected valid transition %s -> %s, got error: %v", tc.from, tc.to, err)
		}
	}
}

func TestValidateTransition_InvalidTransitions(t *testing.T) {
	invalid := []struct {
		from, to State
	}{
		{StatePending, StateActive},
		{StatePending, StateError},
		{StateOnboarding, StateSuspended},
		{StateActive, StatePending},
		{StateActive, StateDecommissioned},
		{StateSuspended, StateHalted},
		{StateHalted, StateActive},
		{StateDraining, StateActive},
		{StateDecommissioned, StateActive},
		{StateError, StateActive},
		{StateError, StateSuspended},
		{StateRecovering, StatePending},
	}

	for _, tc := range invalid {
		if err := ValidateTransition(tc.from, tc.to); err == nil {
			t.Errorf("expected invalid transition %s -> %s to return error", tc.from, tc.to)
		}
	}
}

func TestValidateTransition_UnknownSourceState(t *testing.T) {
	err := ValidateTransition(State("unknown"), StateActive)
	if err == nil {
		t.Error("expected error for unknown source state")
	}
}

func TestTransientStates(t *testing.T) {
	states := TransientStates()
	if len(states) != 4 {
		t.Errorf("expected 4 transient states, got %d", len(states))
	}

	expected := map[State]bool{
		StateOnboarding:  true,
		StateRecovering:  true,
		StateDraining:    true,
		StateRehydrating: true,
	}

	for _, s := range states {
		if !expected[s] {
			t.Errorf("unexpected transient state: %s", s)
		}
	}
}

func TestIsTransient(t *testing.T) {
	if !IsTransient(StateOnboarding) {
		t.Error("StateOnboarding should be transient")
	}
	if !IsTransient(StateRecovering) {
		t.Error("StateRecovering should be transient")
	}
	if !IsTransient(StateDraining) {
		t.Error("StateDraining should be transient")
	}
	if !IsTransient(StateRehydrating) {
		t.Error("StateRehydrating should be transient")
	}

	if IsTransient(StateActive) {
		t.Error("StateActive should NOT be transient")
	}
	if IsTransient(StatePending) {
		t.Error("StatePending should NOT be transient")
	}
	if IsTransient(StateError) {
		t.Error("StateError should NOT be transient")
	}
}

func TestTransientStates_IsCopy(t *testing.T) {
	states := TransientStates()
	states[0] = State("mutated")

	fresh := TransientStates()
	if fresh[0] == State("mutated") {
		t.Error("TransientStates should return a copy, not the internal slice")
	}
}

func TestDecommissioned_NoTransitionsOut(t *testing.T) {
	allowed, ok := transitions[StateDecommissioned]
	if ok && len(allowed) > 0 {
		t.Error("decommissioned state should have no outgoing transitions (terminal state)")
	}
}
