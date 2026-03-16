//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// TestE2E_InterjectionCascadeAndRecovery exercises the full interjection
// lifecycle: trigger a critical halt on a parent agent, verify the cascade
// propagates to child agents via the hierarchy, verify the remediation
// persona is dispatched, and verify resolution clears Safe Mode.
//
// Hierarchy under test:
//
//	supervisor-alpha
//	├── worker-beta
//	└── worker-gamma
func TestE2E_InterjectionCascadeAndRecovery(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	// --- 1. Create agents so GetClearanceLevel lookups succeed ---
	// The interjection manager validates clearance via the agents table.
	adminAgentID, err := h.app.Store.Agents.Create(ctx, &repo.Agent{
		Name:           "AdminOps",
		Personality:    "Admin operator for halt tests",
		ClearanceLevel: 2,
		SystemPrompt:   "You are an admin operator.",
	})
	if err != nil {
		t.Fatalf("create admin agent: %v", err)
	}

	remediationAgentID, err := h.app.Store.Agents.Create(ctx, &repo.Agent{
		Name:           "RemediationBot",
		Personality:    "Automated remediation handler",
		ClearanceLevel: 3,
		SystemPrompt:   "You handle remediation.",
	})
	if err != nil {
		t.Fatalf("create remediation agent: %v", err)
	}

	// --- 2. Set up agent hierarchy: supervisor-alpha -> {worker-beta, worker-gamma} ---
	err = h.app.Store.CommHub.SetRelationship(ctx, &types.AgentRelationship{
		ID:           "rel-alpha-beta",
		ParentAgent:  "supervisor-alpha",
		ChildAgent:   "worker-beta",
		Relationship: "supervisor",
	})
	if err != nil {
		t.Fatalf("set relationship alpha->beta: %v", err)
	}

	err = h.app.Store.CommHub.SetRelationship(ctx, &types.AgentRelationship{
		ID:           "rel-alpha-gamma",
		ParentAgent:  "supervisor-alpha",
		ChildAgent:   "worker-gamma",
		Relationship: "supervisor",
	})
	if err != nil {
		t.Fatalf("set relationship alpha->gamma: %v", err)
	}

	// --- 3. Wire remediation dispatch tracker ---
	// Track remediation dispatches to verify the persona was notified.
	var remediationMu sync.Mutex
	var remediationCalls []struct {
		PersonaID      string
		InterjectionID string
		Description    string
	}

	h.app.InterjectionMgr.SetRemediationDispatch(func(_ context.Context, personaID, interjectionID, description string) error {
		remediationMu.Lock()
		defer remediationMu.Unlock()
		remediationCalls = append(remediationCalls, struct {
			PersonaID      string
			InterjectionID string
			Description    string
		}{personaID, interjectionID, description})
		return nil
	})

	// --- 4. Subscribe to EventBus to capture interjection events ---
	getEvents, waitFor := collectEvents(t, h.bus)

	// --- 5. Trigger a critical halt on supervisor-alpha ---
	haltIJ := &types.Interjection{
		Scope:              "supervisor-alpha",
		Severity:           string(types.SeverityCritical),
		Source:             "e2e-test",
		Reason:             "E2E cascade test: simulated critical failure",
		CreatedBy:          adminAgentID,
		RemediationPersona: remediationAgentID,
	}

	parentID, err := h.app.InterjectionMgr.Halt(ctx, haltIJ)
	if err != nil {
		t.Fatalf("halt: %v", err)
	}
	if parentID == "" {
		t.Fatal("halt returned empty interjection ID")
	}

	// --- 6. Wait for cascade events to propagate ---
	if !waitFor(types.EventInterjectCascade, 5*time.Second) {
		t.Fatal("timed out waiting for interject.cascade event")
	}

	// Small grace period for all events to settle.
	time.Sleep(100 * time.Millisecond)

	// --- 7. Verify events ---
	events := getEvents()

	// 7a. Must have exactly 1 halt event for the parent.
	haltEvents := filterEvents(events, types.EventInterjectHalt)
	if len(haltEvents) != 1 {
		t.Errorf("expected 1 interject.halt event, got %d", len(haltEvents))
	} else {
		var payload map[string]any
		if err := json.Unmarshal(haltEvents[0].Payload, &payload); err == nil {
			if payload["scope"] != "supervisor-alpha" {
				t.Errorf("halt event scope=%v, want supervisor-alpha", payload["scope"])
			}
			if payload["severity"] != string(types.SeverityCritical) {
				t.Errorf("halt event severity=%v, want critical", payload["severity"])
			}
		}
	}

	// 7b. Must have cascade events for both children.
	cascadeEvents := filterEvents(events, types.EventInterjectCascade)
	if len(cascadeEvents) < 2 {
		t.Errorf("expected >= 2 interject.cascade events (one per child), got %d", len(cascadeEvents))
	}

	cascadedAgents := make(map[string]bool)
	for _, ev := range cascadeEvents {
		var payload map[string]any
		if err := json.Unmarshal(ev.Payload, &payload); err == nil {
			if child, ok := payload["child_agent"].(string); ok {
				cascadedAgents[child] = true
			}
		}
	}
	if !cascadedAgents["worker-beta"] {
		t.Error("cascade did not reach worker-beta")
	}
	if !cascadedAgents["worker-gamma"] {
		t.Error("cascade did not reach worker-gamma")
	}

	// 7c. Safe Mode events must fire for all three scopes.
	safeModeEvents := filterEvents(events, types.EventInterjectSafeMode)
	engagedScopes := make(map[string]bool)
	for _, ev := range safeModeEvents {
		var payload map[string]any
		if err := json.Unmarshal(ev.Payload, &payload); err == nil {
			if payload["action"] == "engaged" {
				if scope, ok := payload["scope"].(string); ok {
					engagedScopes[scope] = true
				}
			}
		}
	}
	for _, scope := range []string{"supervisor-alpha", "worker-beta", "worker-gamma"} {
		if !engagedScopes[scope] {
			t.Errorf("safe mode not engaged for scope %q", scope)
		}
	}

	// --- 8. Verify Safe Mode is active for all three agents ---
	for _, scope := range []string{"supervisor-alpha", "worker-beta", "worker-gamma"} {
		if !h.app.InterjectionMgr.SafeMode().IsHalted(scope) {
			t.Errorf("SafeMode.IsHalted(%q) = false, want true", scope)
		}
	}

	// --- 9. Verify remediation persona was dispatched ---
	remediationMu.Lock()
	remCallCount := len(remediationCalls)
	var firstRemCall struct {
		PersonaID      string
		InterjectionID string
		Description    string
	}
	if remCallCount > 0 {
		firstRemCall = remediationCalls[0]
	}
	remediationMu.Unlock()

	if remCallCount == 0 {
		t.Error("remediation dispatch was never called")
	} else {
		if firstRemCall.PersonaID != remediationAgentID {
			t.Errorf("remediation persona=%q, want %q", firstRemCall.PersonaID, remediationAgentID)
		}
		if firstRemCall.InterjectionID != parentID {
			t.Errorf("remediation interjection_id=%q, want %q", firstRemCall.InterjectionID, parentID)
		}
	}

	// --- 10. Resolve the parent interjection ---
	resolveAction := &types.ResolutionAction{
		InterjectionID: parentID,
		ResolvedBy:     adminAgentID,
		Resolution:     "E2E test: issue resolved, resuming operations",
		Action:         "resume",
	}

	if err := h.app.InterjectionMgr.Resolve(ctx, resolveAction); err != nil {
		t.Fatalf("resolve parent interjection: %v", err)
	}

	// Wait for resolve event.
	if !waitFor(types.EventInterjectResolve, 5*time.Second) {
		t.Fatal("timed out waiting for interject.resolve event")
	}
	time.Sleep(50 * time.Millisecond)

	// --- 11. Verify parent Safe Mode is disengaged ---
	if h.app.InterjectionMgr.SafeMode().IsHalted("supervisor-alpha") {
		t.Error("SafeMode should be disengaged for supervisor-alpha after resolution")
	}

	// --- 12. Resolve child interjections ---
	// Find the child interjection IDs from the cascade events.
	childIDs := make(map[string]string) // agent -> interjection ID
	allEvents := getEvents()
	for _, ev := range allEvents {
		if ev.Type == types.EventInterjectCascade {
			var payload map[string]any
			if err := json.Unmarshal(ev.Payload, &payload); err == nil {
				childAgent, _ := payload["child_agent"].(string)
				childIJID, _ := payload["child_interjection_id"].(string)
				if childAgent != "" && childIJID != "" {
					childIDs[childAgent] = childIJID
				}
			}
		}
	}

	for agent, ijID := range childIDs {
		childResolve := &types.ResolutionAction{
			InterjectionID: ijID,
			ResolvedBy:     adminAgentID,
			Resolution:     "E2E test: child resolved",
			Action:         "resume",
		}
		if err := h.app.InterjectionMgr.Resolve(ctx, childResolve); err != nil {
			t.Errorf("resolve child interjection for %s: %v", agent, err)
		}
	}

	// Grace period for disengage events.
	time.Sleep(100 * time.Millisecond)

	// --- 13. Verify all Safe Mode states are disengaged ---
	for _, scope := range []string{"supervisor-alpha", "worker-beta", "worker-gamma"} {
		if h.app.InterjectionMgr.SafeMode().IsHalted(scope) {
			t.Errorf("SafeMode still halted for %q after full resolution", scope)
		}
	}

	// --- 14. Verify resolve events fired ---
	finalEvents := getEvents()
	resolveEvents := filterEvents(finalEvents, types.EventInterjectResolve)
	if len(resolveEvents) < 3 {
		t.Errorf("expected >= 3 interject.resolve events (parent + 2 children), got %d", len(resolveEvents))
	}

	// --- 15. Verify disengage Safe Mode events fired ---
	allSafeModeEvents := filterEvents(finalEvents, types.EventInterjectSafeMode)
	disengagedScopes := make(map[string]bool)
	for _, ev := range allSafeModeEvents {
		var payload map[string]any
		if err := json.Unmarshal(ev.Payload, &payload); err == nil {
			if payload["action"] == "disengaged" {
				if scope, ok := payload["scope"].(string); ok {
					disengagedScopes[scope] = true
				}
			}
		}
	}
	for _, scope := range []string{"supervisor-alpha", "worker-beta", "worker-gamma"} {
		if !disengagedScopes[scope] {
			t.Errorf("safe mode disengage event missing for scope %q", scope)
		}
	}
}

// filterEvents returns events matching the given type.
func filterEvents(events []types.NervousEvent, eventType types.EventType) []types.NervousEvent {
	var result []types.NervousEvent
	for _, ev := range events {
		if ev.Type == eventType {
			result = append(result, ev)
		}
	}
	return result
}
