package lifecycle

import (
	"context"
	"fmt"
	"testing"

	"github.com/hyperax/hyperax/internal/commhub"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// mockAgentRepo is a minimal mock for agent lookups.
type mockAgentRepo struct {
	agents map[string]*repo.Agent
}

func newMockAgentRepo() *mockAgentRepo {
	return &mockAgentRepo{agents: make(map[string]*repo.Agent)}
}

func (m *mockAgentRepo) Create(_ context.Context, a *repo.Agent) (string, error) {
	if a.ID == "" {
		a.ID = fmt.Sprintf("a-%d", len(m.agents)+1)
	}
	m.agents[a.ID] = a
	return a.ID, nil
}

func (m *mockAgentRepo) Get(_ context.Context, id string) (*repo.Agent, error) {
	a, ok := m.agents[id]
	if !ok {
		return nil, fmt.Errorf("agent %q not found", id)
	}
	return a, nil
}

func (m *mockAgentRepo) GetByName(_ context.Context, name string) (*repo.Agent, error) {
	for _, a := range m.agents {
		if a.Name == name {
			return a, nil
		}
	}
	return nil, fmt.Errorf("agent %q not found", name)
}

func (m *mockAgentRepo) List(_ context.Context) ([]*repo.Agent, error) {
	var result []*repo.Agent
	for _, a := range m.agents {
		result = append(result, a)
	}
	return result, nil
}

func (m *mockAgentRepo) ListByPersona(_ context.Context, _ string) ([]*repo.Agent, error) {
	return nil, nil
}

func (m *mockAgentRepo) Update(_ context.Context, _ string, _ *repo.Agent) error { return nil }
func (m *mockAgentRepo) Delete(_ context.Context, _ string) error                { return nil }
func (m *mockAgentRepo) SetAgentError(_ context.Context, agentID, reason string) error {
	a, ok := m.agents[agentID]
	if !ok {
		return fmt.Errorf("agent %q not found", agentID)
	}
	a.Status = repo.AgentStatusError
	a.StatusReason = reason
	return nil
}

// mockCommHubRepo tracks relationships and permissions for testing.
type mockCommHubRepo struct {
	relationships []*types.AgentRelationship
	permissions   []*types.CommPermission
}

func (m *mockCommHubRepo) SetRelationship(_ context.Context, rel *types.AgentRelationship) error {
	m.relationships = append(m.relationships, rel)
	return nil
}

func (m *mockCommHubRepo) GetRelationship(_ context.Context, _, _ string) (*types.AgentRelationship, error) {
	return nil, fmt.Errorf("not found")
}

func (m *mockCommHubRepo) GetChildren(_ context.Context, _ string) ([]*types.AgentRelationship, error) {
	return nil, nil
}

func (m *mockCommHubRepo) GetParent(_ context.Context, _ string) (*types.AgentRelationship, error) {
	return nil, fmt.Errorf("not found")
}

func (m *mockCommHubRepo) GetFullHierarchy(_ context.Context) ([]*types.AgentRelationship, error) {
	return m.relationships, nil
}

func (m *mockCommHubRepo) DeleteRelationship(_ context.Context, _ string) error { return nil }

func (m *mockCommHubRepo) LogMessage(_ context.Context, _ *types.CommLogEntry) error { return nil }
func (m *mockCommHubRepo) GetCommLog(_ context.Context, _ string, _ int) ([]*types.CommLogEntry, error) {
	return nil, nil
}
func (m *mockCommHubRepo) GetCommLogBetween(_ context.Context, _, _ string, _ int) ([]*types.CommLogEntry, error) {
	return nil, nil
}

func (m *mockCommHubRepo) GrantPermission(_ context.Context, perm *types.CommPermission) error {
	m.permissions = append(m.permissions, perm)
	return nil
}

func (m *mockCommHubRepo) RevokePermission(_ context.Context, _, _ string) error { return nil }
func (m *mockCommHubRepo) CheckPermission(_ context.Context, _, _ string) (bool, error) {
	return true, nil
}
func (m *mockCommHubRepo) ListPermissions(_ context.Context, _ string) ([]*types.CommPermission, error) {
	return m.permissions, nil
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

func (m *mockCommHubRepo) LogMessageWithSession(_ context.Context, _ *types.CommLogEntry) error {
	return nil
}

func (m *mockCommHubRepo) GetCommLogBySession(_ context.Context, _ string, _ int) ([]*types.CommLogEntry, error) {
	return nil, nil
}

func (m *mockCommHubRepo) RenameAgentRefs(_ context.Context, _, _ string) error {
	return nil
}

// mockMemoryRepo returns empty recall results.
type mockMemoryRepo struct{}

func (m *mockMemoryRepo) Store(_ context.Context, _ *types.Memory) (string, error)  { return "m-1", nil }
func (m *mockMemoryRepo) Get(_ context.Context, _ string) (*types.Memory, error)     { return nil, nil }
func (m *mockMemoryRepo) Delete(_ context.Context, _ string) error                   { return nil }
func (m *mockMemoryRepo) Recall(_ context.Context, _ string, _ types.MemoryScope, _, _ string, _ int) ([]*types.Memory, error) {
	return []*types.Memory{{ID: "mem-1", Content: "global knowledge"}}, nil
}
func (m *mockMemoryRepo) TouchAccess(_ context.Context, _ string) error { return nil }
func (m *mockMemoryRepo) ListConsolidationCandidates(_ context.Context, _ types.MemoryScope, _ int, _ int) ([]*types.Memory, error) {
	return nil, nil
}
func (m *mockMemoryRepo) MarkConsolidated(_ context.Context, _ []string, _ string) error { return nil }
func (m *mockMemoryRepo) MarkContested(_ context.Context, _, _ string) error              { return nil }
func (m *mockMemoryRepo) Count(_ context.Context, _ types.MemoryScope, _ string) (int, error) {
	return 0, nil
}
func (m *mockMemoryRepo) CountByType(_ context.Context, _ types.MemoryScope, _ string) (map[types.MemoryType]int, error) {
	return nil, nil
}
func (m *mockMemoryRepo) StoreAnnotation(_ context.Context, _ *types.MemoryAnnotation) (string, error) {
	return "a-1", nil
}
func (m *mockMemoryRepo) GetAnnotations(_ context.Context, _ string) ([]*types.MemoryAnnotation, error) {
	return nil, nil
}

// mockProjectRepo returns tasks for a persona.
type mockOnboardProjectRepo struct {
	tasks []*repo.Task
}

func (m *mockOnboardProjectRepo) CreateProjectPlan(_ context.Context, _ *repo.ProjectPlan) (string, error) {
	return "pp-1", nil
}
func (m *mockOnboardProjectRepo) GetProjectPlan(_ context.Context, _ string) (*repo.ProjectPlan, error) {
	return nil, nil
}
func (m *mockOnboardProjectRepo) ListProjectPlans(_ context.Context, _ string) ([]*repo.ProjectPlan, error) {
	return nil, nil
}
func (m *mockOnboardProjectRepo) DeleteProjectPlan(_ context.Context, _ string) error { return nil }
func (m *mockOnboardProjectRepo) UpdateProjectStatus(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockOnboardProjectRepo) MoveProjectWorkspace(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockOnboardProjectRepo) CreateMilestone(_ context.Context, _ *repo.Milestone) (string, error) {
	return "ms-1", nil
}
func (m *mockOnboardProjectRepo) GetMilestone(_ context.Context, _ string) (*repo.Milestone, error) {
	return nil, nil
}
func (m *mockOnboardProjectRepo) ListMilestones(_ context.Context, _ string) ([]*repo.Milestone, error) {
	return nil, nil
}
func (m *mockOnboardProjectRepo) AssignMilestone(_ context.Context, _, _ string) error { return nil }
func (m *mockOnboardProjectRepo) UnassignMilestone(_ context.Context, _ string) error  { return nil }
func (m *mockOnboardProjectRepo) CreateTask(_ context.Context, _ *repo.Task) (string, error) {
	return "t-1", nil
}
func (m *mockOnboardProjectRepo) GetTask(_ context.Context, _ string) (*repo.Task, error) {
	return nil, nil
}
func (m *mockOnboardProjectRepo) UpdateTaskStatus(_ context.Context, _, _ string) error { return nil }
func (m *mockOnboardProjectRepo) ListTasks(_ context.Context, _ string) ([]*repo.Task, error) {
	return m.tasks, nil
}
func (m *mockOnboardProjectRepo) AssignTask(_ context.Context, _, _ string) error   { return nil }
func (m *mockOnboardProjectRepo) UnassignTask(_ context.Context, _ string) error    { return nil }
func (m *mockOnboardProjectRepo) DeleteMilestone(_ context.Context, _ string) error { return nil }
func (m *mockOnboardProjectRepo) DeleteTask(_ context.Context, _ string) error      { return nil }
func (m *mockOnboardProjectRepo) PurgeOrphans(_ context.Context) (int64, error)     { return 0, nil }
func (m *mockOnboardProjectRepo) ListTasksByAgent(_ context.Context, agentID, _, _ string) ([]*repo.Task, error) {
	var result []*repo.Task
	for _, t := range m.tasks {
		if t.AssigneeAgentID == agentID {
			result = append(result, t)
		}
	}
	return result, nil
}
func (m *mockOnboardProjectRepo) ReconcileCompletionStatus(_ context.Context) (int, int, error) {
	return 0, 0, nil
}
func (m *mockOnboardProjectRepo) AddComment(_ context.Context, _ *repo.Comment) (string, error) {
	return "c-1", nil
}
func (m *mockOnboardProjectRepo) ListComments(_ context.Context, _, _ string) ([]*repo.Comment, error) {
	return nil, nil
}
func (m *mockOnboardProjectRepo) GetNextTask(_ context.Context, _ string) (*repo.Task, error) {
	return nil, nil
}

func TestOnboarder_FullOnboarding(t *testing.T) {
	bus := nervous.NewEventBus(64)
	hub := commhub.NewCommHub(bus, discardLogger())

	lifecycleRepo := newHeartbeatMockRepo()
	lifecycleRepo.agents = []*repo.AgentState{
		{AgentID: "agent-1", State: string(StatePending)},
	}

	agentRepo := newMockAgentRepo()
	agentRepo.agents["agent-1"] = &repo.Agent{
		ID:   "agent-1",
		Name: "TestAssistant",
	}

	commHubRepo := &mockCommHubRepo{}

	projectRepo := &mockOnboardProjectRepo{
		tasks: []*repo.Task{
			{ID: "task-1", Name: "Do something", AssigneeAgentID: "persona-1", Status: "pending"},
			{ID: "task-2", Name: "Other task", AssigneeAgentID: "persona-other", Status: "pending"},
		},
	}

	deps := OnboardingDeps{
		LifecycleRepo: lifecycleRepo,
		AgentRepo:     agentRepo,
		CommHubRepo:   commHubRepo,
		MemoryRepo:    &mockMemoryRepo{},
		ProjectRepo:   projectRepo,
		Hub:           hub,
		Bus:           bus,
		Logger:        discardLogger(),
	}

	onboarder := NewOnboarder(deps)

	result, err := onboarder.Onboard(context.Background(), "agent-1", "persona-1", "parent-agent", "workspace-1")
	if err != nil {
		t.Fatalf("Onboard failed: %v", err)
	}

	// Verify result.
	if result.PersonaID != "persona-1" {
		t.Errorf("expected persona persona-1, got %s", result.PersonaID)
	}
	if !result.InboxCreated {
		t.Error("expected inbox to be created")
	}
	if result.Relationships != 1 {
		t.Errorf("expected 1 relationship, got %d", result.Relationships)
	}
	if result.Permissions < 2 {
		t.Errorf("expected at least 2 permissions, got %d", result.Permissions)
	}
	if result.MemoriesLoaded == 0 {
		t.Error("expected some memories to be loaded")
	}
	if result.TasksAssigned != 1 {
		t.Errorf("expected 1 task assigned, got %d", result.TasksAssigned)
	}

	// Verify agent state is active.
	state, _ := lifecycleRepo.GetState(context.Background(), "agent-1")
	if state != string(StateActive) {
		t.Errorf("expected active state, got %s", state)
	}

	// Verify hierarchy was created.
	if len(commHubRepo.relationships) != 1 {
		t.Errorf("expected 1 relationship in CommHubRepo, got %d", len(commHubRepo.relationships))
	}
	if commHubRepo.relationships[0].Relationship != "supervisor" {
		t.Errorf("expected supervisor relationship, got %s", commHubRepo.relationships[0].Relationship)
	}
}

func TestOnboarder_NoParentAgent(t *testing.T) {
	bus := nervous.NewEventBus(64)
	hub := commhub.NewCommHub(bus, discardLogger())

	lifecycleRepo := newHeartbeatMockRepo()
	lifecycleRepo.agents = []*repo.AgentState{
		{AgentID: "agent-1", State: string(StatePending)},
	}

	agentRepo := newMockAgentRepo()
	agentRepo.agents["agent-1"] = &repo.Agent{ID: "agent-1", Name: "TopLevel"}

	deps := OnboardingDeps{
		LifecycleRepo: lifecycleRepo,
		AgentRepo:     agentRepo,
		CommHubRepo:   &mockCommHubRepo{},
		Hub:           hub,
		Bus:           bus,
		Logger:        discardLogger(),
	}

	onboarder := NewOnboarder(deps)
	result, err := onboarder.Onboard(context.Background(), "agent-1", "p-1", "", "")
	if err != nil {
		t.Fatalf("Onboard failed: %v", err)
	}

	// No parent → no supervisor relationship.
	if result.Relationships != 0 {
		t.Errorf("expected 0 relationships (no parent), got %d", result.Relationships)
	}
	// Should still get system permission.
	if result.Permissions < 1 {
		t.Errorf("expected at least 1 permission (system), got %d", result.Permissions)
	}
}

func TestOnboarder_InvalidPersonaFails(t *testing.T) {
	bus := nervous.NewEventBus(64)
	hub := commhub.NewCommHub(bus, discardLogger())

	lifecycleRepo := newHeartbeatMockRepo()
	lifecycleRepo.agents = []*repo.AgentState{
		{AgentID: "agent-1", State: string(StatePending)},
	}

	deps := OnboardingDeps{
		LifecycleRepo: lifecycleRepo,
		AgentRepo:     newMockAgentRepo(), // empty — agent-1 not found
		Hub:           hub,
		Bus:           bus,
		Logger:        discardLogger(),
	}

	onboarder := NewOnboarder(deps)
	_, err := onboarder.Onboard(context.Background(), "agent-1", "nonexistent", "", "")
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}

	// Agent should be in error state after failed onboarding.
	state, _ := lifecycleRepo.GetState(context.Background(), "agent-1")
	if state != string(StateError) {
		t.Errorf("expected error state after failed onboarding, got %s", state)
	}
}

func TestOnboarder_NilDepsGraceful(t *testing.T) {
	bus := nervous.NewEventBus(64)
	hub := commhub.NewCommHub(bus, discardLogger())

	lifecycleRepo := newHeartbeatMockRepo()
	lifecycleRepo.agents = []*repo.AgentState{
		{AgentID: "agent-1", State: string(StatePending)},
	}

	agentRepo := newMockAgentRepo()
	agentRepo.agents["agent-1"] = &repo.Agent{ID: "agent-1", Name: "Test"}

	// No CommHubRepo, MemoryRepo, or ProjectRepo — all nil.
	deps := OnboardingDeps{
		LifecycleRepo: lifecycleRepo,
		AgentRepo:     agentRepo,
		Hub:           hub,
		Bus:           bus,
		Logger:        discardLogger(),
	}

	onboarder := NewOnboarder(deps)
	result, err := onboarder.Onboard(context.Background(), "agent-1", "p-1", "", "")
	if err != nil {
		t.Fatalf("Onboard with nil deps failed: %v", err)
	}

	if result.Relationships != 0 {
		t.Errorf("expected 0 relationships with nil CommHubRepo, got %d", result.Relationships)
	}
	if result.MemoriesLoaded != 0 {
		t.Errorf("expected 0 memories with nil MemoryRepo, got %d", result.MemoriesLoaded)
	}
	if result.TasksAssigned != 0 {
		t.Errorf("expected 0 tasks with nil ProjectRepo, got %d", result.TasksAssigned)
	}

	state, _ := lifecycleRepo.GetState(context.Background(), "agent-1")
	if state != string(StateActive) {
		t.Errorf("expected active state, got %s", state)
	}
}

func TestOnboarder_StepMessagesRoutedThroughCommHub(t *testing.T) {
	bus := nervous.NewEventBus(64)
	hub := commhub.NewCommHub(bus, discardLogger())

	lifecycleRepo := newHeartbeatMockRepo()
	lifecycleRepo.agents = []*repo.AgentState{
		{AgentID: "agent-routed", State: string(StatePending)},
	}

	agentRepo := newMockAgentRepo()
	agentRepo.agents["agent-routed"] = &repo.Agent{
		ID:   "agent-routed",
		Name: "RoutedAssistant",
	}

	deps := OnboardingDeps{
		LifecycleRepo: lifecycleRepo,
		AgentRepo:     agentRepo,
		CommHubRepo:   &mockCommHubRepo{},
		Hub:           hub,
		Bus:           bus,
		Logger:        discardLogger(),
	}

	onboarder := NewOnboarder(deps)
	_, err := onboarder.Onboard(context.Background(), "agent-routed", "p-routed", "", "")
	if err != nil {
		t.Fatalf("Onboard failed: %v", err)
	}

	// The agent's inbox should have received step progression messages
	// routed through the CommHub sieve. Expected messages:
	// 1. identity bootstrap (from stepIdentity)
	// 2. identity_done step message
	// 3. relationships_done step message
	// 4. context_done step message
	// 5. tasks_done step message
	// 6. completed step message
	msgs := hub.Receive("agent-routed", 100)
	if len(msgs) < 5 {
		t.Errorf("expected at least 5 CommHub messages for step progression, got %d", len(msgs))
	}

	// Verify at least one message is an onboarding_progress type.
	foundProgress := false
	for _, m := range msgs {
		if m.From == "system:onboarding" && m.ContentType == "application/json" {
			foundProgress = true
			break
		}
	}
	if !foundProgress {
		t.Error("expected at least one onboarding_progress message from system:onboarding")
	}
}

func TestOnboarder_TypedEventsPublished(t *testing.T) {
	bus := nervous.NewEventBus(64)
	hub := commhub.NewCommHub(bus, discardLogger())

	lifecycleRepo := newHeartbeatMockRepo()
	lifecycleRepo.agents = []*repo.AgentState{
		{AgentID: "agent-evt", State: string(StatePending)},
	}

	agentRepo := newMockAgentRepo()
	agentRepo.agents["agent-evt"] = &repo.Agent{ID: "agent-evt", Name: "EvtTest"}

	// Subscribe to onboarding events.
	sub := bus.Subscribe("test-onboard-events", nil)

	deps := OnboardingDeps{
		LifecycleRepo: lifecycleRepo,
		AgentRepo:     agentRepo,
		CommHubRepo:   &mockCommHubRepo{},
		Hub:           hub,
		Bus:           bus,
		Logger:        discardLogger(),
	}

	onboarder := NewOnboarder(deps)
	_, err := onboarder.Onboard(context.Background(), "agent-evt", "p-evt", "", "")
	if err != nil {
		t.Fatalf("Onboard failed: %v", err)
	}

	// Drain all events from the subscription channel.
	var eventTypes []types.EventType
	for {
		select {
		case e := <-sub.Ch:
			eventTypes = append(eventTypes, e.Type)
		default:
			goto done
		}
	}
done:

	// Verify typed onboarding events were published.
	expectedTypes := []types.EventType{
		types.EventOnboardingStarted,
		types.EventOnboardingIdentityDone,
		types.EventOnboardingRelationshipDone,
		types.EventOnboardingContextDone,
		types.EventOnboardingTasksDone,
		types.EventOnboardingCompleted,
	}

	for _, expected := range expectedTypes {
		found := false
		for _, actual := range eventTypes {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing expected event type %s in published events", expected)
		}
	}
}
