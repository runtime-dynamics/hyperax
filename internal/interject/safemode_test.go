package interject

import (
	"log/slog"
	"os"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestSafeModeController_EngageDisengage(t *testing.T) {
	ctrl := NewSafeModeController(nil, nil, testLogger())

	ctrl.Engage("workspace-1", "ij-1")
	if !ctrl.IsHalted("workspace-1") {
		t.Error("expected workspace-1 to be halted")
	}

	ctrl.Disengage("workspace-1")
	if ctrl.IsHalted("workspace-1") {
		t.Error("expected workspace-1 to NOT be halted after disengage")
	}
}

func TestSafeModeController_GlobalAffectsAll(t *testing.T) {
	ctrl := NewSafeModeController(nil, nil, testLogger())

	ctrl.Engage("global", "ij-global")

	if !ctrl.IsHalted("workspace-1") {
		t.Error("expected workspace-1 halted by global")
	}
	if !ctrl.IsHalted("agent-1") {
		t.Error("expected agent-1 halted by global")
	}
	if !ctrl.IsHalted("global") {
		t.Error("expected global itself halted")
	}
}

func TestSafeModeController_MultipleInterjections(t *testing.T) {
	ctrl := NewSafeModeController(nil, nil, testLogger())

	ctrl.Engage("workspace-1", "ij-1")
	ctrl.Engage("workspace-1", "ij-2")

	state := ctrl.GetState("workspace-1")
	if state == nil {
		t.Fatal("expected state")
	}
	if len(state.InterjectionIDs) != 2 {
		t.Errorf("expected 2 IDs, got %d", len(state.InterjectionIDs))
	}
}

func TestSafeModeController_GetAllStates(t *testing.T) {
	ctrl := NewSafeModeController(nil, nil, testLogger())

	ctrl.Engage("ws-1", "ij-1")
	ctrl.Engage("ws-2", "ij-2")
	ctrl.Engage("global", "ij-3")

	states := ctrl.GetAllStates()
	if len(states) != 3 {
		t.Errorf("expected 3 states, got %d", len(states))
	}
}

func TestSafeModeController_HaltedScopes(t *testing.T) {
	ctrl := NewSafeModeController(nil, nil, testLogger())

	ctrl.Engage("ws-1", "ij-1")
	ctrl.Engage("ws-2", "ij-2")

	scopes := ctrl.HaltedScopes()
	if len(scopes) != 2 {
		t.Errorf("expected 2 halted scopes, got %d", len(scopes))
	}
}

func TestSafeModeController_DisengageNonexistent(t *testing.T) {
	ctrl := NewSafeModeController(nil, nil, testLogger())

	// Should not panic.
	ctrl.Disengage("nonexistent")
}

func TestSafeModeController_GetStateNil(t *testing.T) {
	ctrl := NewSafeModeController(nil, nil, testLogger())

	state := ctrl.GetState("nonexistent")
	if state != nil {
		t.Error("expected nil state for non-halted scope")
	}
}
