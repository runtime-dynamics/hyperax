//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hyperax/hyperax/internal/app"
	"github.com/hyperax/hyperax/internal/config"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/internal/storage/sqlite"
	"github.com/hyperax/hyperax/internal/web"
	"github.com/hyperax/hyperax/pkg/types"
)

// newInterjectionHarness creates a minimal harness for interjection tests.
// Returns the application and bus with the full router wiring
// (necessary for InterjectionMgr to have its ChildResolver wired).
func newInterjectionHarness(t *testing.T) (*app.HyperaxApp, *nervous.EventBus) {
	t.Helper()

	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// In-memory SQLite databases are per-connection. Constrain the pool to
	// a single connection so all queries hit the same migrated database.
	db.SqlDB().SetMaxOpenConns(1)

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	store := db.NewStore()
	bus := nervous.NewEventBus(256)
	tmpDir := t.TempDir()

	bootstrap := &config.BootstrapConfig{
		ListenAddr:      ":0",
		DataDir:         tmpDir,
		OrgWorkspaceDir: tmpDir,
		Storage: config.BootstrapStorage{
			Backend: "sqlite",
			DSN:     ":memory:",
		},
		LogLevel: "warn",
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application := app.New(bootstrap, store, bus, logger)

	mockUI := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>test</html>")},
	}

	// BuildRouter wires InterjectionMgr.SetChildResolver, SetRemediationDispatch, etc.
	_ = web.BuildRouter(application, mockUI, db.SqlDB())

	t.Cleanup(func() {
		_ = store.Close()
	})

	return application, bus
}

// TestE2E_InterjectionCascadeRemediationRecovery tests the full interjection
// lifecycle: create an agent hierarchy (parent -> 2 children), trigger a
// critical interjection on the parent, verify cascade to children, verify
// remediation persona dispatch, resolve the interjection, and confirm
// recovery (safe mode disengaged).
func TestE2E_InterjectionCascadeRemediationRecovery(t *testing.T) {
	application, bus := newInterjectionHarness(t)
	ctx := context.Background()
	store := application.Store

	// --- 1. Subscribe to events ---
	getEvents, _ := collectEvents(t, bus)

	// --- 2. Create agents for the hierarchy ---
	parentID, err := store.Agents.Create(ctx, &repo.Agent{
		Name:           "parent-agent",
		Personality:    "Parent supervisor agent",
		ClearanceLevel: 2,
	})
	if err != nil {
		t.Fatalf("create parent agent: %v", err)
	}

	child1ID, err := store.Agents.Create(ctx, &repo.Agent{
		Name:           "child-agent-1",
		Personality:    "First child agent",
		ClearanceLevel: 1,
	})
	if err != nil {
		t.Fatalf("create child-1 agent: %v", err)
	}

	child2ID, err := store.Agents.Create(ctx, &repo.Agent{
		Name:           "child-agent-2",
		Personality:    "Second child agent",
		ClearanceLevel: 1,
	})
	if err != nil {
		t.Fatalf("create child-2 agent: %v", err)
	}

	// Remediation agent (clearance 3 — ChiefOfStaff level)
	remediationID, err := store.Agents.Create(ctx, &repo.Agent{
		Name:           "remediation-agent",
		Personality:    "Automated remediation agent",
		ClearanceLevel: 3,
	})
	if err != nil {
		t.Fatalf("create remediation agent: %v", err)
	}

	// --- 3. Set up agent hierarchy (parent -> child1, parent -> child2) ---
	if store.CommHub == nil {
		t.Fatal("CommHub repo is nil")
	}

	if err := store.CommHub.SetRelationship(ctx, &types.AgentRelationship{
		ID:           "rel-parent-child1",
		ParentAgent:  parentID,
		ChildAgent:   child1ID,
		Relationship: "supervisor",
	}); err != nil {
		t.Fatalf("set parent->child1 relationship: %v", err)
	}

	if err := store.CommHub.SetRelationship(ctx, &types.AgentRelationship{
		ID:           "rel-parent-child2",
		ParentAgent:  parentID,
		ChildAgent:   child2ID,
		Relationship: "supervisor",
	}); err != nil {
		t.Fatalf("set parent->child2 relationship: %v", err)
	}

	// Verify hierarchy.
	children, err := store.CommHub.GetChildren(ctx, parentID)
	if err != nil {
		t.Fatalf("get children: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(children))
	}

	// --- 4. Trigger a critical interjection on the parent ---
	if application.InterjectionMgr == nil {
		t.Fatal("InterjectionMgr is nil")
	}

	parentIJID, err := application.InterjectionMgr.Halt(ctx, &types.Interjection{
		Scope:              parentID,
		Severity:           string(types.SeverityCritical),
		Source:             "e2e-test",
		Reason:             "E2E test: critical issue detected in parent agent",
		CreatedBy:          parentID,
		RemediationPersona: remediationID,
	})
	if err != nil {
		t.Fatalf("halt parent: %v", err)
	}
	if parentIJID == "" {
		t.Fatal("parent interjection ID is empty")
	}

	// Small delay for cascade propagation.
	time.Sleep(50 * time.Millisecond)

	// --- 5. Verify Safe Mode is engaged for parent and children ---
	safeMode := application.InterjectionMgr.SafeMode()

	if !safeMode.IsHalted(parentID) {
		t.Error("parent agent should be in Safe Mode after critical halt")
	}
	if !safeMode.IsHalted(child1ID) {
		t.Error("child-1 should be in Safe Mode after cascade")
	}
	if !safeMode.IsHalted(child2ID) {
		t.Error("child-2 should be in Safe Mode after cascade")
	}

	// --- 6. Verify cascade events on the EventBus ---
	events := getEvents()
	haltCount := 0
	cascadeCount := 0
	for _, ev := range events {
		switch ev.Type {
		case types.EventInterjectHalt:
			haltCount++
		case types.EventInterjectCascade:
			cascadeCount++
		}
	}

	if haltCount < 1 {
		t.Errorf("expected at least 1 interject.halt event, got %d", haltCount)
	}
	if cascadeCount < 2 {
		t.Errorf("expected at least 2 interject.cascade events (one per child), got %d", cascadeCount)
	}

	// --- 7. Verify cascaded interjections exist in the database ---
	child1Active, err := application.InterjectionMgr.GetActive(ctx, child1ID)
	if err != nil {
		t.Fatalf("get active for child-1: %v", err)
	}
	if len(child1Active) == 0 {
		t.Error("expected active interjection for child-1 after cascade")
	}

	child2Active, err := application.InterjectionMgr.GetActive(ctx, child2ID)
	if err != nil {
		t.Fatalf("get active for child-2: %v", err)
	}
	if len(child2Active) == 0 {
		t.Error("expected active interjection for child-2 after cascade")
	}

	// Verify cascade interjection metadata.
	for _, ij := range child1Active {
		if ij.Source != "cascade:e2e-test" {
			t.Errorf("child-1 cascade source=%q, want cascade:e2e-test", ij.Source)
		}
	}

	// --- 8. Resolve the parent interjection ---
	err = application.InterjectionMgr.Resolve(ctx, &types.ResolutionAction{
		InterjectionID: parentIJID,
		ResolvedBy:     remediationID,
		Resolution:     "Root cause identified and fixed by remediation agent",
		Action:         "resume",
	})
	if err != nil {
		t.Fatalf("resolve parent interjection: %v", err)
	}

	// --- 9. Resolve child interjections ---
	for _, ij := range child1Active {
		err = application.InterjectionMgr.Resolve(ctx, &types.ResolutionAction{
			InterjectionID: ij.ID,
			ResolvedBy:     remediationID,
			Resolution:     "Parent issue resolved, cascaded halt cleared",
			Action:         "resume",
		})
		if err != nil {
			t.Fatalf("resolve child-1 interjection %s: %v", ij.ID, err)
		}
	}

	for _, ij := range child2Active {
		err = application.InterjectionMgr.Resolve(ctx, &types.ResolutionAction{
			InterjectionID: ij.ID,
			ResolvedBy:     remediationID,
			Resolution:     "Parent issue resolved, cascaded halt cleared",
			Action:         "resume",
		})
		if err != nil {
			t.Fatalf("resolve child-2 interjection %s: %v", ij.ID, err)
		}
	}

	time.Sleep(50 * time.Millisecond)

	// --- 10. Verify Safe Mode is disengaged after resolution ---
	if safeMode.IsHalted(parentID) {
		t.Error("parent agent should NOT be in Safe Mode after resolution")
	}
	if safeMode.IsHalted(child1ID) {
		t.Error("child-1 should NOT be in Safe Mode after resolution")
	}
	if safeMode.IsHalted(child2ID) {
		t.Error("child-2 should NOT be in Safe Mode after resolution")
	}

	// --- 11. Verify resolve events on EventBus ---
	events = getEvents()
	resolveCount := 0
	safeModeDisengageCount := 0
	for _, ev := range events {
		switch ev.Type {
		case types.EventInterjectResolve:
			resolveCount++
		case types.EventInterjectSafeMode:
			var payload map[string]any
			if err := json.Unmarshal(ev.Payload, &payload); err == nil {
				if payload["action"] == "disengaged" {
					safeModeDisengageCount++
				}
			}
		}
	}

	if resolveCount < 3 {
		t.Errorf("expected at least 3 interject.resolve events, got %d", resolveCount)
	}

	if safeModeDisengageCount < 3 {
		t.Errorf("expected at least 3 safe mode disengage events, got %d", safeModeDisengageCount)
	}

	// --- 12. Verify history shows resolved interjections ---
	parentHistory, err := application.InterjectionMgr.GetHistory(ctx, parentID, 10)
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if len(parentHistory) == 0 {
		t.Error("expected resolved interjection in parent history")
	}
	for _, ij := range parentHistory {
		if ij.Status != string(types.StatusResolved) {
			t.Errorf("history interjection status=%q, want resolved", ij.Status)
		}
	}
}

// TestE2E_InterjectionGlobalHaltAffectsAllScopes verifies that a global-scope
// interjection puts all agent scopes into Safe Mode.
func TestE2E_InterjectionGlobalHaltAffectsAllScopes(t *testing.T) {
	application, bus := newInterjectionHarness(t)
	ctx := context.Background()

	collectEvents(t, bus)

	agentID, err := application.Store.Agents.Create(ctx, &repo.Agent{
		Name:           "test-agent",
		ClearanceLevel: 1,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	ijID, err := application.InterjectionMgr.Halt(ctx, &types.Interjection{
		Scope:    string(types.ScopeGlobal),
		Severity: string(types.SeverityFatal),
		Source:   "e2e-test-global",
		Reason:   "Global emergency stop",
	})
	if err != nil {
		t.Fatalf("global halt: %v", err)
	}

	safeMode := application.InterjectionMgr.SafeMode()
	if !safeMode.IsHalted(string(types.ScopeGlobal)) {
		t.Error("global scope should be halted")
	}
	if !safeMode.IsHalted(agentID) {
		t.Error("agent scope should be halted by global halt")
	}
	if !safeMode.IsHalted("any-random-scope") {
		t.Error("any scope should be halted by global halt")
	}

	err = application.InterjectionMgr.Resolve(ctx, &types.ResolutionAction{
		InterjectionID: ijID,
		Resolution:     "Emergency resolved",
		Action:         "resume",
	})
	if err != nil {
		t.Fatalf("resolve global halt: %v", err)
	}

	if safeMode.IsHalted(string(types.ScopeGlobal)) {
		t.Error("global scope should NOT be halted after resolution")
	}
}

// TestE2E_InterjectionClearanceValidation verifies that a low-clearance
// agent cannot resolve a high-clearance interjection.
func TestE2E_InterjectionClearanceValidation(t *testing.T) {
	application, bus := newInterjectionHarness(t)
	ctx := context.Background()

	collectEvents(t, bus)

	adminID, err := application.Store.Agents.Create(ctx, &repo.Agent{
		Name:           "admin-agent",
		ClearanceLevel: 2,
	})
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}

	observerID, err := application.Store.Agents.Create(ctx, &repo.Agent{
		Name:           "observer-agent",
		ClearanceLevel: 0,
	})
	if err != nil {
		t.Fatalf("create observer: %v", err)
	}

	ijID, err := application.InterjectionMgr.Halt(ctx, &types.Interjection{
		Scope:     adminID,
		Severity:  string(types.SeverityCritical),
		Source:    "clearance-test",
		Reason:    "Testing clearance enforcement",
		CreatedBy: adminID,
	})
	if err != nil {
		t.Fatalf("halt: %v", err)
	}

	err = application.InterjectionMgr.Resolve(ctx, &types.ResolutionAction{
		InterjectionID: ijID,
		ResolvedBy:     observerID,
		Resolution:     "Observer trying to resolve",
		Action:         "resume",
	})
	if err == nil {
		t.Error("expected error: observer should not be able to resolve admin interjection")
	}

	active, err := application.InterjectionMgr.GetActive(ctx, adminID)
	if err != nil {
		t.Fatalf("get active: %v", err)
	}
	if len(active) == 0 {
		t.Error("interjection should still be active after failed observer resolve")
	}
}
