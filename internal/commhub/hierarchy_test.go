package commhub

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

// mockCommHubRepo is an in-memory implementation of repo.CommHubRepo for testing.
type mockCommHubRepo struct {
	mu            sync.RWMutex
	relationships map[string]*types.AgentRelationship
	logEntries    []*types.CommLogEntry
	permissions   map[string]*types.CommPermission // key: "agentID:targetID"
	nextID        int
}

func newMockCommHubRepo() *mockCommHubRepo {
	return &mockCommHubRepo{
		relationships: make(map[string]*types.AgentRelationship),
		permissions:   make(map[string]*types.CommPermission),
	}
}

func (m *mockCommHubRepo) SetRelationship(_ context.Context, rel *types.AgentRelationship) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rel.ID == "" {
		m.nextID++
		rel.ID = fmt.Sprintf("rel-%d", m.nextID)
	}
	m.relationships[rel.ID] = rel
	return nil
}

func (m *mockCommHubRepo) GetRelationship(_ context.Context, parentAgent, childAgent string) (*types.AgentRelationship, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, rel := range m.relationships {
		if rel.ParentAgent == parentAgent && rel.ChildAgent == childAgent {
			return rel, nil
		}
	}
	return nil, fmt.Errorf("relationship not found: %s -> %s", parentAgent, childAgent)
}

func (m *mockCommHubRepo) GetChildren(_ context.Context, parentAgent string) ([]*types.AgentRelationship, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*types.AgentRelationship
	for _, rel := range m.relationships {
		if rel.ParentAgent == parentAgent {
			result = append(result, rel)
		}
	}
	return result, nil
}

func (m *mockCommHubRepo) GetParent(_ context.Context, childAgent string) (*types.AgentRelationship, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, rel := range m.relationships {
		if rel.ChildAgent == childAgent {
			return rel, nil
		}
	}
	return nil, fmt.Errorf("no parent for agent %q", childAgent)
}

func (m *mockCommHubRepo) GetFullHierarchy(_ context.Context) ([]*types.AgentRelationship, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*types.AgentRelationship
	for _, rel := range m.relationships {
		result = append(result, rel)
	}
	return result, nil
}

func (m *mockCommHubRepo) DeleteRelationship(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.relationships[id]; !ok {
		return fmt.Errorf("relationship %q not found", id)
	}
	delete(m.relationships, id)
	return nil
}

func (m *mockCommHubRepo) LogMessage(_ context.Context, entry *types.CommLogEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry.ID == "" {
		m.nextID++
		entry.ID = fmt.Sprintf("log-%d", m.nextID)
	}
	m.logEntries = append(m.logEntries, entry)
	return nil
}

func (m *mockCommHubRepo) GetCommLog(_ context.Context, agentID string, limit int) ([]*types.CommLogEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*types.CommLogEntry
	for _, e := range m.logEntries {
		if e.FromAgent == agentID || e.ToAgent == agentID {
			result = append(result, e)
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *mockCommHubRepo) GetCommLogBetween(_ context.Context, from, to string, limit int) ([]*types.CommLogEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*types.CommLogEntry
	for _, e := range m.logEntries {
		if (e.FromAgent == from && e.ToAgent == to) || (e.FromAgent == to && e.ToAgent == from) {
			result = append(result, e)
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *mockCommHubRepo) GrantPermission(_ context.Context, perm *types.CommPermission) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if perm.ID == "" {
		m.nextID++
		perm.ID = fmt.Sprintf("perm-%d", m.nextID)
	}
	key := perm.AgentID + ":" + perm.TargetID
	m.permissions[key] = perm
	return nil
}

func (m *mockCommHubRepo) RevokePermission(_ context.Context, agentID, targetID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := agentID + ":" + targetID
	if _, ok := m.permissions[key]; !ok {
		return fmt.Errorf("permission not found for %s -> %s", agentID, targetID)
	}
	delete(m.permissions, key)
	return nil
}

func (m *mockCommHubRepo) CheckPermission(_ context.Context, agentID, targetID string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// Check exact match.
	key := agentID + ":" + targetID
	if perm, ok := m.permissions[key]; ok {
		if perm.Permission == "send" || perm.Permission == "both" {
			return true, nil
		}
	}
	// Check wildcard.
	wildKey := agentID + ":*"
	if perm, ok := m.permissions[wildKey]; ok {
		if perm.Permission == "send" || perm.Permission == "both" {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockCommHubRepo) ListPermissions(_ context.Context, agentID string) ([]*types.CommPermission, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*types.CommPermission
	for _, perm := range m.permissions {
		if perm.AgentID == agentID {
			result = append(result, perm)
		}
	}
	return result, nil
}

func (m *mockCommHubRepo) PersistOverflow(_ context.Context, _ *types.OverflowEntry) error {
	return nil
}

func (m *mockCommHubRepo) DrainOverflow(_ context.Context, _ string, _ int) ([]*types.OverflowEntry, error) {
	return nil, nil
}

func (m *mockCommHubRepo) CountOverflow(_ context.Context, _ string) (int, error) {
	return 0, nil
}

func (m *mockCommHubRepo) PurgeOverflow(_ context.Context, _ string) (int64, error) {
	return 0, nil
}

func (m *mockCommHubRepo) LogMessageWithSession(ctx context.Context, entry *types.CommLogEntry) error {
	return m.LogMessage(ctx, entry)
}

func (m *mockCommHubRepo) GetCommLogBySession(_ context.Context, _ string, _ int) ([]*types.CommLogEntry, error) {
	return nil, nil
}

func (m *mockCommHubRepo) RenameAgentRefs(_ context.Context, _, _ string) error {
	return nil
}

func newTestHierarchy() (*HierarchyManager, *mockCommHubRepo) {
	bus := nervous.NewEventBus(64)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := newMockCommHubRepo()
	hm := NewHierarchyManager(repo, bus, logger)
	return hm, repo
}

func TestHierarchy_ValidateRoute_WithExplicitPermission(t *testing.T) {
	hm, repo := newTestHierarchy()
	ctx := context.Background()

	// Grant explicit permission.
	_ = repo.GrantPermission(ctx, &types.CommPermission{
		AgentID:    "agent-a",
		TargetID:   "agent-b",
		Permission: "both",
	})

	if err := hm.ValidateRoute(ctx, "agent-a", "agent-b"); err != nil {
		t.Errorf("expected route to be permitted with explicit permission, got: %v", err)
	}
}

func TestHierarchy_ValidateRoute_WithWildcardPermission(t *testing.T) {
	hm, repo := newTestHierarchy()
	ctx := context.Background()

	// Grant wildcard permission.
	_ = repo.GrantPermission(ctx, &types.CommPermission{
		AgentID:    "agent-admin",
		TargetID:   "*",
		Permission: "send",
	})

	if err := hm.ValidateRoute(ctx, "agent-admin", "agent-anyone"); err != nil {
		t.Errorf("expected route to be permitted with wildcard, got: %v", err)
	}
}

func TestHierarchy_ValidateRoute_ParentToChild(t *testing.T) {
	hm, _ := newTestHierarchy()
	ctx := context.Background()

	// Set hierarchy: supervisor -> worker.
	_ = hm.SetRelationship(ctx, &types.AgentRelationship{
		ParentAgent:  "supervisor",
		ChildAgent:   "worker",
		Relationship: "supervisor",
	})

	if err := hm.ValidateRoute(ctx, "supervisor", "worker"); err != nil {
		t.Errorf("expected parent -> child route to be permitted, got: %v", err)
	}
}

func TestHierarchy_ValidateRoute_ChildToParent(t *testing.T) {
	hm, _ := newTestHierarchy()
	ctx := context.Background()

	// Set hierarchy: supervisor -> worker.
	_ = hm.SetRelationship(ctx, &types.AgentRelationship{
		ParentAgent:  "supervisor",
		ChildAgent:   "worker",
		Relationship: "supervisor",
	})

	if err := hm.ValidateRoute(ctx, "worker", "supervisor"); err != nil {
		t.Errorf("expected child -> parent route to be permitted, got: %v", err)
	}
}

func TestHierarchy_ValidateRoute_PeersThroughSharedParent(t *testing.T) {
	hm, _ := newTestHierarchy()
	ctx := context.Background()

	// Set hierarchy: boss -> worker-a, boss -> worker-b.
	_ = hm.SetRelationship(ctx, &types.AgentRelationship{
		ParentAgent:  "boss",
		ChildAgent:   "worker-a",
		Relationship: "supervisor",
	})
	_ = hm.SetRelationship(ctx, &types.AgentRelationship{
		ParentAgent:  "boss",
		ChildAgent:   "worker-b",
		Relationship: "supervisor",
	})

	if err := hm.ValidateRoute(ctx, "worker-a", "worker-b"); err != nil {
		t.Errorf("expected peer route to be permitted, got: %v", err)
	}
}

func TestHierarchy_ValidateRoute_Denied(t *testing.T) {
	hm, _ := newTestHierarchy()
	ctx := context.Background()

	// No relationships or permissions — route should be denied.
	err := hm.ValidateRoute(ctx, "stranger-a", "stranger-b")
	if err == nil {
		t.Error("expected route to be denied for unrelated agents")
	}
}

func TestHierarchy_ValidateRoute_DeniedPublishesBounceEvent(t *testing.T) {
	bus := nervous.NewEventBus(64)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := newMockCommHubRepo()
	hm := NewHierarchyManager(repo, bus, logger)

	sub := bus.SubscribeTypes("bounce-watcher", types.EventCommBounced)
	defer bus.Unsubscribe("bounce-watcher")

	ctx := context.Background()
	_ = hm.ValidateRoute(ctx, "no-access", "restricted")

	select {
	case event := <-sub.Ch:
		if event.Type != types.EventCommBounced {
			t.Errorf("expected %s, got %s", types.EventCommBounced, event.Type)
		}
	default:
		t.Error("expected comm.bounced event to be published on route denial")
	}
}

func TestHierarchy_ValidateRoute_NilRepoAllows(t *testing.T) {
	bus := nervous.NewEventBus(64)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	hm := NewHierarchyManager(nil, bus, logger)

	ctx := context.Background()
	if err := hm.ValidateRoute(ctx, "anyone", "anything"); err != nil {
		t.Errorf("expected nil repo to permit all routes, got: %v", err)
	}
}

func TestHierarchy_SetRelationship_PublishesEvent(t *testing.T) {
	bus := nervous.NewEventBus(64)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := newMockCommHubRepo()
	hm := NewHierarchyManager(repo, bus, logger)

	sub := bus.SubscribeTypes("hierarchy-watcher", types.EventCommHierarchyChanged)
	defer bus.Unsubscribe("hierarchy-watcher")

	ctx := context.Background()
	_ = hm.SetRelationship(ctx, &types.AgentRelationship{
		ParentAgent:  "lead",
		ChildAgent:   "agent-1",
		Relationship: "supervisor",
	})

	select {
	case event := <-sub.Ch:
		if event.Type != types.EventCommHierarchyChanged {
			t.Errorf("expected %s, got %s", types.EventCommHierarchyChanged, event.Type)
		}
	default:
		t.Error("expected hierarchy changed event to be published")
	}
}

func TestHierarchy_GetDelegateFor_DirectChild(t *testing.T) {
	hm, _ := newTestHierarchy()
	ctx := context.Background()

	// Set up: parent has a child with "coding" relationship type.
	_ = hm.SetRelationship(ctx, &types.AgentRelationship{
		ParentAgent:  "team-lead",
		ChildAgent:   "code-agent",
		Relationship: "coding",
	})

	delegate, err := hm.GetDelegateFor(ctx, "team-lead", "coding")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if delegate != "code-agent" {
		t.Errorf("expected delegate=code-agent, got %s", delegate)
	}
}

func TestHierarchy_GetDelegateFor_NotFound(t *testing.T) {
	hm, _ := newTestHierarchy()
	ctx := context.Background()

	_, err := hm.GetDelegateFor(ctx, "lone-agent", "unknown-category")
	if err == nil {
		t.Error("expected error when no delegate is found")
	}
}

func TestHierarchy_GetFullHierarchy(t *testing.T) {
	hm, _ := newTestHierarchy()
	ctx := context.Background()

	_ = hm.SetRelationship(ctx, &types.AgentRelationship{
		ParentAgent: "boss", ChildAgent: "worker-1", Relationship: "supervisor",
	})
	_ = hm.SetRelationship(ctx, &types.AgentRelationship{
		ParentAgent: "boss", ChildAgent: "worker-2", Relationship: "supervisor",
	})

	rels, err := hm.GetFullHierarchy(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rels) != 2 {
		t.Errorf("expected 2 relationships, got %d", len(rels))
	}
}
