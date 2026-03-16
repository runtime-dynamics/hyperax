package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/internal/storage"
	"github.com/hyperax/hyperax/internal/workflow"
	"github.com/hyperax/hyperax/pkg/types"
)

// ---------------------------------------------------------------------------
// In-memory mock workflow repo for handler tests
// ---------------------------------------------------------------------------

type mockWorkflowRepo struct {
	mu        sync.Mutex
	workflows map[string]*repo.Workflow
	steps     map[string]*repo.WorkflowStep
	runs      map[string]*repo.WorkflowRun
	runSteps  map[string]*repo.WorkflowRunStep
	triggers  map[string]*repo.WorkflowTrigger
}

func newMockWorkflowRepo() *mockWorkflowRepo {
	return &mockWorkflowRepo{
		workflows: make(map[string]*repo.Workflow),
		steps:     make(map[string]*repo.WorkflowStep),
		runs:      make(map[string]*repo.WorkflowRun),
		runSteps:  make(map[string]*repo.WorkflowRunStep),
		triggers:  make(map[string]*repo.WorkflowTrigger),
	}
}

func (m *mockWorkflowRepo) CreateWorkflow(_ context.Context, wf *repo.Workflow) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if wf.ID == "" {
		wf.ID = uuid.New().String()
	}
	wf.CreatedAt = time.Now()
	wf.UpdatedAt = time.Now()
	m.workflows[wf.ID] = wf
	return wf.ID, nil
}

func (m *mockWorkflowRepo) GetWorkflow(_ context.Context, id string) (*repo.Workflow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	wf, ok := m.workflows[id]
	if !ok {
		return nil, fmt.Errorf("workflow %q not found", id)
	}
	return wf, nil
}

func (m *mockWorkflowRepo) ListWorkflows(_ context.Context) ([]*repo.Workflow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*repo.Workflow
	for _, wf := range m.workflows {
		out = append(out, wf)
	}
	return out, nil
}

func (m *mockWorkflowRepo) UpdateWorkflow(_ context.Context, wf *repo.Workflow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.workflows[wf.ID]; !ok {
		return fmt.Errorf("workflow %q not found", wf.ID)
	}
	wf.UpdatedAt = time.Now()
	m.workflows[wf.ID] = wf
	return nil
}

func (m *mockWorkflowRepo) DeleteWorkflow(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.workflows[id]; !ok {
		return fmt.Errorf("workflow %q not found", id)
	}
	delete(m.workflows, id)
	return nil
}

func (m *mockWorkflowRepo) CreateStep(_ context.Context, step *repo.WorkflowStep) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if step.ID == "" {
		step.ID = uuid.New().String()
	}
	step.CreatedAt = time.Now()
	step.UpdatedAt = time.Now()
	m.steps[step.ID] = step
	return step.ID, nil
}

func (m *mockWorkflowRepo) GetSteps(_ context.Context, workflowID string) ([]*repo.WorkflowStep, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*repo.WorkflowStep
	for _, s := range m.steps {
		if s.WorkflowID == workflowID {
			out = append(out, s)
		}
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[i].Position > out[j].Position {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

func (m *mockWorkflowRepo) UpdateStep(_ context.Context, step *repo.WorkflowStep) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.steps[step.ID]; !ok {
		return fmt.Errorf("step %q not found", step.ID)
	}
	step.UpdatedAt = time.Now()
	m.steps[step.ID] = step
	return nil
}

func (m *mockWorkflowRepo) DeleteStep(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.steps[id]; !ok {
		return fmt.Errorf("step %q not found", id)
	}
	delete(m.steps, id)
	return nil
}

func (m *mockWorkflowRepo) CreateRun(_ context.Context, run *repo.WorkflowRun) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if run.ID == "" {
		run.ID = uuid.New().String()
	}
	run.CreatedAt = time.Now()
	run.UpdatedAt = time.Now()
	m.runs[run.ID] = run
	return run.ID, nil
}

func (m *mockWorkflowRepo) GetRun(_ context.Context, id string) (*repo.WorkflowRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run, ok := m.runs[id]
	if !ok {
		return nil, fmt.Errorf("run %q not found", id)
	}
	return run, nil
}

func (m *mockWorkflowRepo) ListRuns(_ context.Context, workflowID string) ([]*repo.WorkflowRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*repo.WorkflowRun
	for _, r := range m.runs {
		if r.WorkflowID == workflowID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *mockWorkflowRepo) UpdateRunStatus(_ context.Context, id string, status string, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	run, ok := m.runs[id]
	if !ok {
		return fmt.Errorf("run %q not found", id)
	}
	run.Status = status
	run.Error = errMsg
	now := time.Now()
	run.UpdatedAt = now
	switch status {
	case "running":
		run.StartedAt = &now
	case "completed", "failed", "cancelled":
		run.FinishedAt = &now
	}
	return nil
}

func (m *mockWorkflowRepo) CreateRunStep(_ context.Context, rs *repo.WorkflowRunStep) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rs.ID == "" {
		rs.ID = uuid.New().String()
	}
	rs.CreatedAt = time.Now()
	rs.UpdatedAt = time.Now()
	m.runSteps[rs.ID] = rs
	return rs.ID, nil
}

func (m *mockWorkflowRepo) GetRunSteps(_ context.Context, runID string) ([]*repo.WorkflowRunStep, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*repo.WorkflowRunStep
	for _, rs := range m.runSteps {
		if rs.RunID == runID {
			out = append(out, rs)
		}
	}
	return out, nil
}

func (m *mockWorkflowRepo) UpdateRunStepStatus(_ context.Context, id string, status string, output string, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rs, ok := m.runSteps[id]
	if !ok {
		return fmt.Errorf("run step %q not found", id)
	}
	rs.Status = status
	rs.Output = output
	rs.Error = errMsg
	now := time.Now()
	rs.UpdatedAt = now
	switch status {
	case "running":
		rs.StartedAt = &now
	case "completed", "failed", "skipped":
		rs.FinishedAt = &now
	}
	return nil
}

func (m *mockWorkflowRepo) CreateTrigger(_ context.Context, trigger *repo.WorkflowTrigger) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if trigger.ID == "" {
		trigger.ID = uuid.New().String()
	}
	trigger.CreatedAt = time.Now()
	trigger.UpdatedAt = time.Now()
	m.triggers[trigger.ID] = trigger
	return trigger.ID, nil
}

func (m *mockWorkflowRepo) ListTriggers(_ context.Context, workflowID string) ([]*repo.WorkflowTrigger, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*repo.WorkflowTrigger
	for _, tr := range m.triggers {
		if tr.WorkflowID == workflowID {
			out = append(out, tr)
		}
	}
	return out, nil
}

func (m *mockWorkflowRepo) DeleteTrigger(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.triggers[id]; !ok {
		return fmt.Errorf("trigger %q not found", id)
	}
	delete(m.triggers, id)
	return nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func setupWorkflowHandler(t *testing.T) (*PipelineHandler, *mockWorkflowRepo, context.Context) {
	t.Helper()

	mockRepo := newMockWorkflowRepo()
	store := &storage.Store{
		Workflows: mockRepo,
	}

	bus := nervous.NewEventBus(64)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	exec := workflow.NewExecutor(mockRepo, bus, logger)
	handler := NewPipelineHandler(store, bus, logger)
	handler.SetWorkflowDeps(exec)

	return handler, mockRepo, context.Background()
}

func callWorkflowTool(t *testing.T, fn func(context.Context, json.RawMessage) (*types.ToolResult, error), ctx context.Context, args any) *types.ToolResult {
	t.Helper()
	data, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	result, err := fn(ctx, data)
	if err != nil {
		t.Fatalf("tool error: %v", err)
	}
	return result
}

func wfResultText(r *types.ToolResult) string {
	if len(r.Content) == 0 {
		return ""
	}
	return r.Content[0].Text
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestPipelineHandler_RegisterTools(t *testing.T) {
	h, _, _ := setupWorkflowHandler(t)

	registry := mcp.NewToolRegistry()
	h.RegisterTools(registry)

	// Consolidated handler registers a single "pipeline" tool.
	if registry.ToolCount() != 1 {
		t.Errorf("expected 1 tool (pipeline), got %d", registry.ToolCount())
	}

	schemas := registry.Schemas()
	if len(schemas) == 0 || schemas[0].Name != "pipeline" {
		t.Errorf("expected tool named 'pipeline', got %v", schemas)
	}
}

func TestWorkflowHandler_CreateWorkflow(t *testing.T) {
	h, _, ctx := setupWorkflowHandler(t)

	type args struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Steps       []struct {
			Name     string `json:"name"`
			StepType string `json:"step_type"`
		} `json:"steps"`
	}

	result := callWorkflowTool(t, h.createWorkflow, ctx, args{
		Name:        "test-workflow",
		Description: "A test workflow",
		Steps: []struct {
			Name     string `json:"name"`
			StepType string `json:"step_type"`
		}{
			{Name: "step-1", StepType: "tool"},
			{Name: "step-2", StepType: "tool"},
		},
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", wfResultText(result))
	}

	text := wfResultText(result)
	if !strings.Contains(text, "test-workflow") {
		t.Errorf("expected workflow name in result, got: %s", text)
	}
}

func TestWorkflowHandler_CreateWorkflow_MissingName(t *testing.T) {
	h, _, ctx := setupWorkflowHandler(t)

	result := callWorkflowTool(t, h.createWorkflow, ctx, map[string]string{
		"description": "no name",
	})

	if !result.IsError {
		t.Error("expected error for missing name")
	}
}

func TestWorkflowHandler_ListWorkflows_Empty(t *testing.T) {
	h, _, ctx := setupWorkflowHandler(t)

	result := callWorkflowTool(t, h.listWorkflows, ctx, map[string]string{})

	if result.IsError {
		t.Fatalf("unexpected error: %s", wfResultText(result))
	}

	text := wfResultText(result)
	if text != "[]" {
		t.Errorf("expected empty JSON array for no workflows, got: %s", text)
	}
}

func TestWorkflowHandler_ListWorkflows_WithWorkflows(t *testing.T) {
	h, mockRepo, ctx := setupWorkflowHandler(t)

	// Create a workflow directly in the repo.
	wf := &repo.Workflow{Name: "my-wf", Description: "desc", Enabled: true}
	_, _ = mockRepo.CreateWorkflow(ctx, wf)

	result := callWorkflowTool(t, h.listWorkflows, ctx, map[string]string{})

	if result.IsError {
		t.Fatalf("unexpected error: %s", wfResultText(result))
	}

	text := wfResultText(result)
	if !strings.Contains(text, "my-wf") {
		t.Errorf("expected 'my-wf' in result, got: %s", text)
	}
}

func TestWorkflowHandler_RunWorkflow(t *testing.T) {
	h, mockRepo, ctx := setupWorkflowHandler(t)

	// Create a workflow with one step.
	wf := &repo.Workflow{Name: "run-me", Enabled: true}
	wfID, _ := mockRepo.CreateWorkflow(ctx, wf)

	step := &repo.WorkflowStep{
		WorkflowID: wfID,
		Name:       "step-1",
		StepType:   "tool",
		Action:     json.RawMessage(`{}`),
		Position:   0,
	}
	_, _ = mockRepo.CreateStep(ctx, step)

	result := callWorkflowTool(t, h.runWorkflow, ctx, map[string]string{
		"workflow_id": wfID,
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", wfResultText(result))
	}

	text := wfResultText(result)
	if !strings.Contains(text, "run_id") {
		t.Errorf("expected run_id in result, got: %s", text)
	}
}

func TestWorkflowHandler_RunWorkflow_MissingID(t *testing.T) {
	h, _, ctx := setupWorkflowHandler(t)

	result := callWorkflowTool(t, h.runWorkflow, ctx, map[string]string{})

	if !result.IsError {
		t.Error("expected error for missing workflow_id")
	}
}

func TestWorkflowHandler_GetWorkflowStatus(t *testing.T) {
	h, mockRepo, ctx := setupWorkflowHandler(t)

	// Create a workflow, a run, and a run step.
	wf := &repo.Workflow{Name: "status-test", Enabled: true}
	wfID, _ := mockRepo.CreateWorkflow(ctx, wf)

	step := &repo.WorkflowStep{
		WorkflowID: wfID,
		Name:       "step-1",
		StepType:   "tool",
		Action:     json.RawMessage(`{}`),
		Position:   0,
	}
	stepID, _ := mockRepo.CreateStep(ctx, step)

	run := &repo.WorkflowRun{
		WorkflowID: wfID,
		Status:     "running",
		Context:    json.RawMessage(`{}`),
	}
	runID, _ := mockRepo.CreateRun(ctx, run)

	rs := &repo.WorkflowRunStep{
		RunID:  runID,
		StepID: stepID,
		Status: "running",
	}
	_, _ = mockRepo.CreateRunStep(ctx, rs)

	result := callWorkflowTool(t, h.getWorkflowStatus, ctx, map[string]string{
		"run_id": runID,
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", wfResultText(result))
	}

	text := wfResultText(result)
	if !strings.Contains(text, "running") {
		t.Errorf("expected 'running' status in result, got: %s", text)
	}
}

func TestWorkflowHandler_GetWorkflowStatus_MissingRunID(t *testing.T) {
	h, _, ctx := setupWorkflowHandler(t)

	result := callWorkflowTool(t, h.getWorkflowStatus, ctx, map[string]string{})

	if !result.IsError {
		t.Error("expected error for missing run_id")
	}
}

func TestWorkflowHandler_ApproveStep_MissingArgs(t *testing.T) {
	h, _, ctx := setupWorkflowHandler(t)

	// Missing both args.
	result := callWorkflowTool(t, h.approveWorkflowStep, ctx, map[string]string{})
	if !result.IsError {
		t.Error("expected error for missing run_id")
	}

	// Missing step_id.
	result = callWorkflowTool(t, h.approveWorkflowStep, ctx, map[string]string{
		"run_id": "some-run",
	})
	if !result.IsError {
		t.Error("expected error for missing step_id")
	}
}

func TestWorkflowHandler_CancelRun_MissingRunID(t *testing.T) {
	h, _, ctx := setupWorkflowHandler(t)

	result := callWorkflowTool(t, h.cancelWorkflowRun, ctx, map[string]string{})

	if !result.IsError {
		t.Error("expected error for missing run_id")
	}
}

func TestWorkflowHandler_CreateWorkflowWithDeps(t *testing.T) {
	h, _, ctx := setupWorkflowHandler(t)

	type stepDef struct {
		Name      string `json:"name"`
		StepType  string `json:"step_type"`
		DependsOn string `json:"depends_on"`
	}
	type args struct {
		Name  string    `json:"name"`
		Steps []stepDef `json:"steps"`
	}

	result := callWorkflowTool(t, h.createWorkflow, ctx, args{
		Name: "dep-workflow",
		Steps: []stepDef{
			{Name: "build", StepType: "tool"},
			{Name: "test", StepType: "tool", DependsOn: "build"},
			{Name: "deploy", StepType: "tool", DependsOn: "test"},
		},
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", wfResultText(result))
	}

	text := wfResultText(result)
	if !strings.Contains(text, "3") {
		t.Errorf("expected 3 steps in result, got: %s", text)
	}
}

func TestPipelineHandler_NilWorkflowRepo(t *testing.T) {
	store := &storage.Store{} // Workflows is nil.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	h := NewPipelineHandler(store, nil, logger)
	ctx := context.Background()

	result := callWorkflowTool(t, h.createWorkflow, ctx, map[string]string{"name": "test"})
	if !result.IsError {
		t.Error("expected error when workflow repo is nil")
	}

	result = callWorkflowTool(t, h.listWorkflows, ctx, map[string]string{})
	if !result.IsError {
		t.Error("expected error when workflow repo is nil")
	}
}

func TestPipelineHandler_NilExecutor(t *testing.T) {
	mockRepo := newMockWorkflowRepo()
	store := &storage.Store{Workflows: mockRepo}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	h := NewPipelineHandler(store, nil, logger) // no workflow executor set
	ctx := context.Background()

	result := callWorkflowTool(t, h.runWorkflow, ctx, map[string]string{"workflow_id": "some-id"})
	if !result.IsError {
		t.Error("expected error when executor is nil")
	}

	result = callWorkflowTool(t, h.approveWorkflowStep, ctx, map[string]string{
		"run_id": "r", "step_id": "s",
	})
	if !result.IsError {
		t.Error("expected error when executor is nil")
	}

	result = callWorkflowTool(t, h.cancelWorkflowRun, ctx, map[string]string{"run_id": "r"})
	if !result.IsError {
		t.Error("expected error when executor is nil")
	}
}
