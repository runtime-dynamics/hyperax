package interject

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hyperax/hyperax/pkg/types"
)

// mockInterjectionRepo is a minimal in-memory mock for testing.
type mockInterjectionRepo struct {
	interjections map[string]*types.Interjection
	clearances    map[string]int
	bypasses      map[string]*types.SieveBypass
	dlq           map[string]*types.DLQEntry
	idCounter     int
}

func newMockRepo() *mockInterjectionRepo {
	return &mockInterjectionRepo{
		interjections: make(map[string]*types.Interjection),
		clearances:    make(map[string]int),
		bypasses:      make(map[string]*types.SieveBypass),
		dlq:           make(map[string]*types.DLQEntry),
	}
}

func (m *mockInterjectionRepo) Create(_ context.Context, ij *types.Interjection) (string, error) {
	m.idCounter++
	if ij.ID == "" {
		ij.ID = "ij-" + itoa(m.idCounter)
	}
	ij.Status = "active"
	ij.CreatedAt = time.Now()
	m.interjections[ij.ID] = ij
	return ij.ID, nil
}

func (m *mockInterjectionRepo) GetByID(_ context.Context, id string) (*types.Interjection, error) {
	ij, ok := m.interjections[id]
	if !ok {
		return nil, &notFoundError{id: id}
	}
	return ij, nil
}

func (m *mockInterjectionRepo) GetActive(_ context.Context, scope string) ([]*types.Interjection, error) {
	var result []*types.Interjection
	for _, ij := range m.interjections {
		if ij.Scope == scope && ij.Status == "active" {
			result = append(result, ij)
		}
	}
	return result, nil
}

func (m *mockInterjectionRepo) GetAllActive(_ context.Context) ([]*types.Interjection, error) {
	var result []*types.Interjection
	for _, ij := range m.interjections {
		if ij.Status == "active" {
			result = append(result, ij)
		}
	}
	return result, nil
}

func (m *mockInterjectionRepo) GetHistory(_ context.Context, scope string, limit int) ([]*types.Interjection, error) {
	var result []*types.Interjection
	for _, ij := range m.interjections {
		if ij.Scope == scope && ij.Status != "active" {
			result = append(result, ij)
			if limit > 0 && len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (m *mockInterjectionRepo) Resolve(_ context.Context, id string, action *types.ResolutionAction) error {
	ij, ok := m.interjections[id]
	if !ok {
		return &notFoundError{id: id}
	}
	ij.Status = "resolved"
	ij.Resolution = action.Resolution
	ij.Action = action.Action
	ij.ResolvedBy = action.ResolvedBy
	ij.ResolverClearance = action.ResolverClearance
	now := time.Now()
	ij.ResolvedAt = &now
	return nil
}

func (m *mockInterjectionRepo) Expire(_ context.Context, id string) error {
	ij, ok := m.interjections[id]
	if !ok {
		return &notFoundError{id: id}
	}
	ij.Status = "expired"
	return nil
}

func (m *mockInterjectionRepo) GetClearanceLevel(_ context.Context, personaID string) (int, error) {
	level, ok := m.clearances[personaID]
	if !ok {
		return 0, &notFoundError{id: personaID}
	}
	return level, nil
}

func (m *mockInterjectionRepo) CreateBypass(_ context.Context, b *types.SieveBypass) (string, error) {
	m.idCounter++
	if b.ID == "" {
		b.ID = "bp-" + itoa(m.idCounter)
	}
	m.bypasses[b.ID] = b
	return b.ID, nil
}

func (m *mockInterjectionRepo) GetActiveBypass(_ context.Context, scope string) ([]*types.SieveBypass, error) {
	var result []*types.SieveBypass
	now := time.Now()
	for _, b := range m.bypasses {
		if b.Scope == scope && !b.Revoked && b.ExpiresAt.After(now) {
			result = append(result, b)
		}
	}
	return result, nil
}

func (m *mockInterjectionRepo) RevokeBypass(_ context.Context, id string) error {
	if b, ok := m.bypasses[id]; ok {
		b.Revoked = true
	}
	return nil
}

func (m *mockInterjectionRepo) ExpireBypasses(_ context.Context) (int, error) {
	count := 0
	now := time.Now()
	for _, b := range m.bypasses {
		if !b.Revoked && b.ExpiresAt.Before(now) {
			b.Revoked = true
			count++
		}
	}
	return count, nil
}

func (m *mockInterjectionRepo) EnqueueDLQ(_ context.Context, entry *types.DLQEntry) (string, error) {
	m.idCounter++
	if entry.ID == "" {
		entry.ID = "dlq-" + itoa(m.idCounter)
	}
	entry.Status = "queued"
	entry.QueuedAt = time.Now()
	m.dlq[entry.ID] = entry
	return entry.ID, nil
}

func (m *mockInterjectionRepo) ListDLQ(_ context.Context, interjectionID string, limit int) ([]*types.DLQEntry, error) {
	var result []*types.DLQEntry
	for _, e := range m.dlq {
		if e.InterjectionID == interjectionID && e.Status == "queued" {
			result = append(result, e)
			if limit > 0 && len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (m *mockInterjectionRepo) ReplayDLQ(_ context.Context, id string) error {
	if e, ok := m.dlq[id]; ok {
		e.Status = "replayed"
	}
	return nil
}

func (m *mockInterjectionRepo) DismissDLQ(_ context.Context, id string) error {
	if e, ok := m.dlq[id]; ok {
		e.Status = "dismissed"
	}
	return nil
}

func (m *mockInterjectionRepo) CountDLQ(_ context.Context, interjectionID string) (int, error) {
	count := 0
	for _, e := range m.dlq {
		if e.InterjectionID == interjectionID && e.Status == "queued" {
			count++
		}
	}
	return count, nil
}

type notFoundError struct{ id string }

func (e *notFoundError) Error() string { return "not found: " + e.id }

func itoa(n int) string {
	return string(rune('0'+n%10)) + ""
}

// --- Tests ---

func TestManager_Halt(t *testing.T) {
	repo := newMockRepo()
	mgr := NewManager(repo, nil, testLogger())

	ij := &types.Interjection{
		Scope:    "workspace",
		Severity: "critical",
		Source:   "test-agent",
		Reason:   "test halt",
	}

	id, err := mgr.Halt(context.Background(), ij)
	if err != nil {
		t.Fatalf("Halt() error: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty ID")
	}

	// Should be in safe mode now.
	if !mgr.SafeMode().IsHalted("workspace") {
		t.Error("expected workspace to be halted")
	}
}

func TestManager_HaltWarningNoSafeMode(t *testing.T) {
	repo := newMockRepo()
	mgr := NewManager(repo, nil, testLogger())

	ij := &types.Interjection{
		Scope:    "workspace",
		Severity: "warning",
		Source:   "test",
		Reason:   "just a warning",
	}

	_, err := mgr.Halt(context.Background(), ij)
	if err != nil {
		t.Fatalf("Halt() error: %v", err)
	}

	// Warning should NOT engage safe mode.
	if mgr.SafeMode().IsHalted("workspace") {
		t.Error("warning should not engage safe mode")
	}
}

func TestManager_Resolve(t *testing.T) {
	repo := newMockRepo()
	mgr := NewManager(repo, nil, testLogger())

	ij := &types.Interjection{
		Scope:    "workspace",
		Severity: "critical",
		Source:   "test",
		Reason:   "test halt",
	}
	id, err := mgr.Halt(context.Background(), ij)
	if err != nil {
		t.Fatalf("Halt() error: %v", err)
	}

	action := &types.ResolutionAction{
		InterjectionID: id,
		Resolution:     "fixed the issue",
		Action:         "resume",
	}

	if err := mgr.Resolve(context.Background(), action); err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	// Safe mode should be disengaged.
	if mgr.SafeMode().IsHalted("workspace") {
		t.Error("expected workspace safe mode to be disengaged after resolve")
	}
}

func TestManager_ResolveClearanceCheck(t *testing.T) {
	repo := newMockRepo()
	repo.clearances["agent-1"] = 1
	repo.clearances["agent-2"] = 0
	mgr := NewManager(repo, nil, testLogger())

	ij := &types.Interjection{
		Scope:     "workspace",
		Severity:  "critical",
		Source:    "test",
		Reason:    "need clearance",
		CreatedBy: "agent-1",
	}
	id, err := mgr.Halt(context.Background(), ij)
	if err != nil {
		t.Fatalf("Halt() error: %v", err)
	}

	// Agent-2 has clearance 0, interjection source has clearance 1.
	action := &types.ResolutionAction{
		InterjectionID: id,
		Resolution:     "attempt resolve",
		Action:         "resume",
		ResolvedBy:     "agent-2",
	}

	resolveErr := mgr.Resolve(context.Background(), action)
	if resolveErr == nil {
		t.Fatal("expected clearance error, got nil")
	}
	if !containsStr(resolveErr.Error(), "insufficient clearance") {
		t.Errorf("expected insufficient clearance error, got: %v", err)
	}
}

func TestManager_IsHaltedGlobalAffectsAll(t *testing.T) {
	repo := newMockRepo()
	mgr := NewManager(repo, nil, testLogger())

	ij := &types.Interjection{
		Scope:    "global",
		Severity: "fatal",
		Source:   "test",
		Reason:   "global halt",
	}
	if _, err := mgr.Halt(context.Background(), ij); err != nil {
		t.Fatalf("Halt() error: %v", err)
	}

	// Any scope should report halted.
	if !mgr.IsHalted(context.Background(), "workspace") {
		t.Error("expected workspace halted by global halt")
	}
	if !mgr.IsHalted(context.Background(), "agent") {
		t.Error("expected agent halted by global halt")
	}
}

func TestManager_RecoverOnStartup(t *testing.T) {
	repo := newMockRepo()
	// Simulate an active interjection from a previous run.
	repo.interjections["prev-1"] = &types.Interjection{
		ID:       "prev-1",
		Scope:    "workspace",
		Severity: "critical",
		Source:   "previous-run",
		Reason:   "leftover halt",
		Status:   "active",
	}

	mgr := NewManager(repo, nil, testLogger())

	if err := mgr.RecoverOnStartup(context.Background()); err != nil {
		t.Fatalf("RecoverOnStartup() error: %v", err)
	}

	if !mgr.SafeMode().IsHalted("workspace") {
		t.Error("expected safe mode to be re-engaged on startup")
	}
}

func TestSafeMode_IdempotentEngage(t *testing.T) {
	repo := newMockRepo()
	mgr := NewManager(repo, nil, testLogger())

	mgr.SafeMode().Engage("workspace", "ij-1")
	mgr.SafeMode().Engage("workspace", "ij-1") // duplicate
	mgr.SafeMode().Engage("workspace", "ij-2") // second halt

	state := mgr.SafeMode().GetState("workspace")
	if state == nil {
		t.Fatal("expected state for workspace")
	}
	if len(state.InterjectionIDs) != 2 {
		t.Errorf("expected 2 interjection IDs, got %d", len(state.InterjectionIDs))
	}
}

func TestManager_DLQ(t *testing.T) {
	repo := newMockRepo()
	mgr := NewManager(repo, nil, testLogger())

	entry := &types.DLQEntry{
		InterjectionID: "ij-1",
		MessageType:    "comm.message",
		Payload:        `{"test": true}`,
		Source:         "agent-1",
		Scope:          "workspace",
	}

	id, err := mgr.EnqueueDLQ(context.Background(), entry)
	if err != nil {
		t.Fatalf("EnqueueDLQ() error: %v", err)
	}

	entries, err := mgr.ListDLQ(context.Background(), "ij-1", 10)
	if err != nil {
		t.Fatalf("ListDLQ() error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 DLQ entry, got %d", len(entries))
	}

	if err := mgr.ReplayDLQ(context.Background(), id); err != nil {
		t.Fatalf("ReplayDLQ() error: %v", err)
	}

	// After replay, entry should not appear in queued list.
	entries, err = mgr.ListDLQ(context.Background(), "ij-1", 10)
	if err != nil {
		t.Fatalf("ListDLQ() after replay error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 queued DLQ entries after replay, got %d", len(entries))
	}
}

func TestManager_BypassGrantAndRevoke(t *testing.T) {
	repo := newMockRepo()
	mgr := NewManager(repo, nil, testLogger())

	bypass := &types.SieveBypass{
		Scope:     "workspace",
		Pattern:   "*.critical",
		GrantedBy: "admin",
		ExpiresAt: time.Now().Add(1 * time.Hour),
		Reason:    "testing",
	}

	id, err := mgr.GrantBypass(context.Background(), bypass)
	if err != nil {
		t.Fatalf("GrantBypass() error: %v", err)
	}

	active, err := mgr.GetActiveBypasses(context.Background(), "workspace")
	if err != nil {
		t.Fatalf("GetActiveBypasses() error: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("expected 1 active bypass, got %d", len(active))
	}

	if err := mgr.RevokeBypass(context.Background(), id); err != nil {
		t.Fatalf("RevokeBypass() error: %v", err)
	}

	active, err = mgr.GetActiveBypasses(context.Background(), "workspace")
	if err != nil {
		t.Fatalf("GetActiveBypasses() after revoke error: %v", err)
	}
	if len(active) != 0 {
		t.Errorf("expected 0 active bypasses after revoke, got %d", len(active))
	}
}

func TestManager_CascadeHalt(t *testing.T) {
	repo := newMockRepo()
	mgr := NewManager(repo, nil, testLogger())

	// Set up a hierarchy: parent -> [child-a, child-b], child-a -> [grandchild-1].
	hierarchy := map[string][]string{
		"parent":  {"child-a", "child-b"},
		"child-a": {"grandchild-1"},
	}
	mgr.SetChildResolver(func(_ context.Context, parentAgent string) ([]string, error) {
		children, ok := hierarchy[parentAgent]
		if !ok {
			return nil, &notFoundError{id: parentAgent}
		}
		return children, nil
	})

	ij := &types.Interjection{
		Scope:     "parent",
		Severity:  "critical",
		Source:    "watchdog",
		Reason:    "budget exceeded",
		CreatedBy: "parent",
	}

	id, err := mgr.Halt(context.Background(), ij)
	if err != nil {
		t.Fatalf("Halt() error: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty ID")
	}

	// Parent should be halted.
	if !mgr.SafeMode().IsHalted("parent") {
		t.Error("expected parent to be halted")
	}

	// Children should be halted via cascade.
	if !mgr.SafeMode().IsHalted("child-a") {
		t.Error("expected child-a to be halted via cascade")
	}
	if !mgr.SafeMode().IsHalted("child-b") {
		t.Error("expected child-b to be halted via cascade")
	}

	// Grandchild should be halted via recursive cascade.
	if !mgr.SafeMode().IsHalted("grandchild-1") {
		t.Error("expected grandchild-1 to be halted via cascade")
	}

	// Count total interjections created: 1 parent + 3 cascaded = 4.
	all, err := mgr.GetAllActive(context.Background())
	if err != nil {
		t.Fatalf("GetAllActive() error: %v", err)
	}
	if len(all) != 4 {
		t.Errorf("expected 4 active interjections (1 parent + 3 cascaded), got %d", len(all))
	}

	// Verify cascaded interjections reference the source.
	for _, ij := range all {
		if ij.Scope != "parent" {
			if ij.Source != "cascade:watchdog" {
				t.Errorf("cascaded interjection scope=%s has source=%q, expected cascade:watchdog", ij.Scope, ij.Source)
			}
		}
	}
}

func TestManager_CascadeHaltNilResolver(t *testing.T) {
	repo := newMockRepo()
	mgr := NewManager(repo, nil, testLogger())
	// No child resolver set — cascade should be a no-op.

	ij := &types.Interjection{
		Scope:     "parent",
		Severity:  "critical",
		Source:    "test",
		Reason:    "test cascade with nil resolver",
		CreatedBy: "parent",
	}

	_, err := mgr.Halt(context.Background(), ij)
	if err != nil {
		t.Fatalf("Halt() error: %v", err)
	}

	// Only the parent interjection should exist.
	all, err := mgr.GetAllActive(context.Background())
	if err != nil {
		t.Fatalf("GetAllActive() error: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("expected 1 interjection (no cascade), got %d", len(all))
	}
}

func TestManager_CascadeHaltWarningNoCascade(t *testing.T) {
	repo := newMockRepo()
	mgr := NewManager(repo, nil, testLogger())

	called := false
	mgr.SetChildResolver(func(_ context.Context, _ string) ([]string, error) {
		called = true
		return []string{"child-a"}, nil
	})

	ij := &types.Interjection{
		Scope:    "parent",
		Severity: "warning",
		Source:   "test",
		Reason:   "just a warning",
	}

	_, err := mgr.Halt(context.Background(), ij)
	if err != nil {
		t.Fatalf("Halt() error: %v", err)
	}

	// Warnings should NOT cascade — only critical/fatal.
	if called {
		t.Error("expected child resolver NOT to be called for warning severity")
	}

	// Only the parent interjection should exist.
	all, err := mgr.GetAllActive(context.Background())
	if err != nil {
		t.Fatalf("GetAllActive() error: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("expected 1 interjection (no cascade for warning), got %d", len(all))
	}
}

func TestManager_RemediationDispatchExplicit(t *testing.T) {
	repo := newMockRepo()
	mgr := NewManager(repo, nil, testLogger())

	var dispatched string
	var dispatchedDesc string
	mgr.SetRemediationDispatch(func(_ context.Context, personaID, interjectionID, description string) error {
		dispatched = personaID
		dispatchedDesc = description
		return nil
	})

	ij := &types.Interjection{
		Scope:              "workspace",
		Severity:           "critical",
		Source:             "budget-monitor",
		Reason:             "spending exceeded 95%",
		RemediationPersona: "ops-bot",
	}

	id, err := mgr.Halt(context.Background(), ij)
	if err != nil {
		t.Fatalf("Halt() error: %v", err)
	}

	if dispatched != "ops-bot" {
		t.Errorf("expected remediation dispatch to ops-bot, got %q", dispatched)
	}
	if !containsSubstr(dispatchedDesc, id) {
		t.Errorf("expected description to contain interjection ID %s", id)
	}
}

func TestManager_RemediationDispatchFromConfig(t *testing.T) {
	repo := newMockRepo()
	mgr := NewManager(repo, nil, testLogger())

	var dispatched string
	mgr.SetRemediationDispatch(func(_ context.Context, personaID, _, _ string) error {
		dispatched = personaID
		return nil
	})
	mgr.SetConfigLookup(func(_ context.Context, key string) (string, error) {
		if key == "safety.remediation_persona" {
			return "default-fixer", nil
		}
		return "", fmt.Errorf("not found")
	})

	ij := &types.Interjection{
		Scope:    "workspace",
		Severity: "fatal",
		Source:   "watchdog",
		Reason:   "heartbeat stale",
		// No explicit RemediationPersona — should fall back to config.
	}

	_, err := mgr.Halt(context.Background(), ij)
	if err != nil {
		t.Fatalf("Halt() error: %v", err)
	}

	if dispatched != "default-fixer" {
		t.Errorf("expected remediation dispatch to default-fixer (from config), got %q", dispatched)
	}
}

func TestManager_RemediationNoDispatchWhenNotConfigured(t *testing.T) {
	repo := newMockRepo()
	mgr := NewManager(repo, nil, testLogger())

	called := false
	mgr.SetRemediationDispatch(func(_ context.Context, _, _, _ string) error {
		called = true
		return nil
	})
	// No config lookup set, no explicit persona.

	ij := &types.Interjection{
		Scope:    "workspace",
		Severity: "critical",
		Source:   "test",
		Reason:   "no remediation configured",
	}

	_, err := mgr.Halt(context.Background(), ij)
	if err != nil {
		t.Fatalf("Halt() error: %v", err)
	}

	if called {
		t.Error("expected no remediation dispatch when no persona configured")
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
