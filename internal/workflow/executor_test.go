package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// ---------------------------------------------------------------------------
// In-memory mock repo
// ---------------------------------------------------------------------------

type memWorkflowRepo struct {
	mu        sync.Mutex
	workflows map[string]*repo.Workflow
	steps     map[string]*repo.WorkflowStep     // stepID -> step
	runs      map[string]*repo.WorkflowRun       // runID -> run
	runSteps  map[string]*repo.WorkflowRunStep    // runStepID -> runStep
	triggers  map[string]*repo.WorkflowTrigger    // triggerID -> trigger
}

func newMemWorkflowRepo() *memWorkflowRepo {
	return &memWorkflowRepo{
		workflows: make(map[string]*repo.Workflow),
		steps:     make(map[string]*repo.WorkflowStep),
		runs:      make(map[string]*repo.WorkflowRun),
		runSteps:  make(map[string]*repo.WorkflowRunStep),
		triggers:  make(map[string]*repo.WorkflowTrigger),
	}
}

func (m *memWorkflowRepo) CreateWorkflow(_ context.Context, wf *repo.Workflow) (string, error) {
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

func (m *memWorkflowRepo) GetWorkflow(_ context.Context, id string) (*repo.Workflow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	wf, ok := m.workflows[id]
	if !ok {
		return nil, fmt.Errorf("workflow %q not found", id)
	}
	return wf, nil
}

func (m *memWorkflowRepo) ListWorkflows(_ context.Context) ([]*repo.Workflow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*repo.Workflow
	for _, wf := range m.workflows {
		out = append(out, wf)
	}
	return out, nil
}

func (m *memWorkflowRepo) UpdateWorkflow(_ context.Context, wf *repo.Workflow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.workflows[wf.ID]; !ok {
		return fmt.Errorf("workflow %q not found", wf.ID)
	}
	wf.UpdatedAt = time.Now()
	m.workflows[wf.ID] = wf
	return nil
}

func (m *memWorkflowRepo) DeleteWorkflow(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.workflows[id]; !ok {
		return fmt.Errorf("workflow %q not found", id)
	}
	delete(m.workflows, id)
	return nil
}

func (m *memWorkflowRepo) CreateStep(_ context.Context, step *repo.WorkflowStep) (string, error) {
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

func (m *memWorkflowRepo) GetSteps(_ context.Context, workflowID string) ([]*repo.WorkflowStep, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*repo.WorkflowStep
	for _, s := range m.steps {
		if s.WorkflowID == workflowID {
			out = append(out, s)
		}
	}
	// Sort by position for deterministic order.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[i].Position > out[j].Position {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

func (m *memWorkflowRepo) UpdateStep(_ context.Context, step *repo.WorkflowStep) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.steps[step.ID]; !ok {
		return fmt.Errorf("step %q not found", step.ID)
	}
	step.UpdatedAt = time.Now()
	m.steps[step.ID] = step
	return nil
}

func (m *memWorkflowRepo) DeleteStep(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.steps[id]; !ok {
		return fmt.Errorf("step %q not found", id)
	}
	delete(m.steps, id)
	return nil
}

func (m *memWorkflowRepo) CreateRun(_ context.Context, run *repo.WorkflowRun) (string, error) {
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

func (m *memWorkflowRepo) GetRun(_ context.Context, id string) (*repo.WorkflowRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run, ok := m.runs[id]
	if !ok {
		return nil, fmt.Errorf("run %q not found", id)
	}
	return run, nil
}

func (m *memWorkflowRepo) ListRuns(_ context.Context, workflowID string) ([]*repo.WorkflowRun, error) {
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

func (m *memWorkflowRepo) UpdateRunStatus(_ context.Context, id string, status string, errMsg string) error {
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

func (m *memWorkflowRepo) CreateRunStep(_ context.Context, rs *repo.WorkflowRunStep) (string, error) {
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

func (m *memWorkflowRepo) GetRunSteps(_ context.Context, runID string) ([]*repo.WorkflowRunStep, error) {
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

func (m *memWorkflowRepo) UpdateRunStepStatus(_ context.Context, id string, status string, output string, errMsg string) error {
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

func (m *memWorkflowRepo) CreateTrigger(_ context.Context, trigger *repo.WorkflowTrigger) (string, error) {
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

func (m *memWorkflowRepo) ListTriggers(_ context.Context, workflowID string) ([]*repo.WorkflowTrigger, error) {
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

func (m *memWorkflowRepo) DeleteTrigger(_ context.Context, id string) error {
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

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func testExecutor(repoImpl repo.WorkflowRepo) *Executor {
	bus := nervous.NewEventBus(64)
	return NewExecutor(repoImpl, bus, testLogger())
}

// createTestWorkflow creates a workflow with the given steps and returns
// the workflow ID and a map of step name -> step ID.
func createTestWorkflow(t *testing.T, ctx context.Context, r *memWorkflowRepo, name string, steps []repo.WorkflowStep) (string, map[string]string) {
	t.Helper()

	wf := &repo.Workflow{Name: name, Enabled: true}
	wfID, err := r.CreateWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}

	nameToID := make(map[string]string, len(steps))
	for i := range steps {
		steps[i].WorkflowID = wfID
		steps[i].Position = i
		if len(steps[i].Action) == 0 {
			steps[i].Action = json.RawMessage(`{}`)
		}
		id, err := r.CreateStep(ctx, &steps[i])
		if err != nil {
			t.Fatalf("create step: %v", err)
		}
		nameToID[steps[i].Name] = id
	}

	return wfID, nameToID
}

// waitForRunStatus polls until the run reaches the expected status or times out.
func waitForRunStatus(t *testing.T, ctx context.Context, r *memWorkflowRepo, runID string, expected string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		run, err := r.GetRun(ctx, runID)
		if err != nil {
			t.Fatalf("get run: %v", err)
		}
		if run.Status == expected {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	run, _ := r.GetRun(ctx, runID)
	t.Fatalf("run %q did not reach status %q within %v (current: %q)", runID, expected, timeout, run.Status)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestTopoSort_LinearChain(t *testing.T) {
	stepA := &repo.WorkflowStep{ID: "a", DependsOn: ""}
	stepB := &repo.WorkflowStep{ID: "b", DependsOn: "a"}
	stepC := &repo.WorkflowStep{ID: "c", DependsOn: "b"}

	order, err := topoSort([]*repo.WorkflowStep{stepA, stepB, stepC})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(order) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(order))
	}

	// a must come before b, b before c.
	indexOf := make(map[string]int, 3)
	for i, id := range order {
		indexOf[id] = i
	}
	if indexOf["a"] >= indexOf["b"] {
		t.Errorf("a should come before b: %v", order)
	}
	if indexOf["b"] >= indexOf["c"] {
		t.Errorf("b should come before c: %v", order)
	}
}

func TestTopoSort_Parallel(t *testing.T) {
	stepA := &repo.WorkflowStep{ID: "a", DependsOn: ""}
	stepB := &repo.WorkflowStep{ID: "b", DependsOn: ""}
	stepC := &repo.WorkflowStep{ID: "c", DependsOn: "a,b"}

	order, err := topoSort([]*repo.WorkflowStep{stepA, stepB, stepC})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(order) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(order))
	}

	indexOf := make(map[string]int, 3)
	for i, id := range order {
		indexOf[id] = i
	}
	if indexOf["a"] >= indexOf["c"] {
		t.Errorf("a should come before c: %v", order)
	}
	if indexOf["b"] >= indexOf["c"] {
		t.Errorf("b should come before c: %v", order)
	}
}

func TestTopoSort_CycleDetection(t *testing.T) {
	stepA := &repo.WorkflowStep{ID: "a", DependsOn: "c"}
	stepB := &repo.WorkflowStep{ID: "b", DependsOn: "a"}
	stepC := &repo.WorkflowStep{ID: "c", DependsOn: "b"}

	_, err := topoSort([]*repo.WorkflowStep{stepA, stepB, stepC})
	if err == nil {
		t.Error("expected cycle detection error")
	}
}

func TestGroupByLevel(t *testing.T) {
	// a, b have no deps (level 0); c depends on a, b (level 1); d depends on c (level 2)
	steps := []*repo.WorkflowStep{
		{ID: "a", DependsOn: ""},
		{ID: "b", DependsOn: ""},
		{ID: "c", DependsOn: "a,b"},
		{ID: "d", DependsOn: "c"},
	}

	order, err := topoSort(steps)
	if err != nil {
		t.Fatalf("topo sort: %v", err)
	}

	levels := groupByLevel(order, steps)
	if len(levels) != 3 {
		t.Fatalf("expected 3 levels, got %d: %v", len(levels), levels)
	}

	// Level 0 should contain a and b.
	if len(levels[0]) != 2 {
		t.Errorf("level 0 should have 2 steps, got %d", len(levels[0]))
	}
	// Level 1 should contain c.
	if len(levels[1]) != 1 || levels[1][0] != "c" {
		t.Errorf("level 1 should be [c], got %v", levels[1])
	}
	// Level 2 should contain d.
	if len(levels[2]) != 1 || levels[2][0] != "d" {
		t.Errorf("level 2 should be [d], got %v", levels[2])
	}
}

func TestParseDependsOn(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"a", 1},
		{"a,b", 2},
		{"a, b, c", 3},
		{" , , ", 0},
	}

	for _, tt := range tests {
		got := parseDependsOn(tt.input)
		if len(got) != tt.want {
			t.Errorf("parseDependsOn(%q) = %d items, want %d", tt.input, len(got), tt.want)
		}
	}
}

func TestValidateDAG_Valid(t *testing.T) {
	steps := []*repo.WorkflowStep{
		{ID: "a", DependsOn: ""},
		{ID: "b", DependsOn: "a"},
	}
	if err := validateDAG(steps); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateDAG_UnknownDep(t *testing.T) {
	steps := []*repo.WorkflowStep{
		{ID: "a", DependsOn: "unknown"},
	}
	if err := validateDAG(steps); err == nil {
		t.Error("expected error for unknown dependency")
	}
}

func TestExecutor_SimpleLinearWorkflow(t *testing.T) {
	r := newMemWorkflowRepo()
	exec := testExecutor(r)
	ctx := context.Background()

	wfID, stepIDs := createTestWorkflow(t, ctx, r, "simple", []repo.WorkflowStep{
		{Name: "step-a", StepType: "tool"},
		{Name: "step-b", StepType: "tool", DependsOn: ""}, // Will be updated below.
	})

	// Set step-b to depend on step-a.
	stepB := r.steps[stepIDs["step-b"]]
	stepB.DependsOn = stepIDs["step-a"]

	runID, err := exec.StartRun(ctx, wfID, nil)
	if err != nil {
		t.Fatalf("start run: %v", err)
	}

	waitForRunStatus(t, ctx, r, runID, types.WorkflowStatusCompleted, 5*time.Second)

	// Verify all steps completed.
	runSteps, _ := r.GetRunSteps(ctx, runID)
	for _, rs := range runSteps {
		if rs.Status != types.StepStatusCompleted {
			t.Errorf("step %s has status %q, want %q", rs.StepID, rs.Status, types.StepStatusCompleted)
		}
	}
}

func TestExecutor_ParallelSteps(t *testing.T) {
	r := newMemWorkflowRepo()
	exec := testExecutor(r)
	ctx := context.Background()

	// a and b are independent; c depends on both.
	wfID, stepIDs := createTestWorkflow(t, ctx, r, "parallel", []repo.WorkflowStep{
		{Name: "step-a", StepType: "tool"},
		{Name: "step-b", StepType: "tool"},
		{Name: "step-c", StepType: "tool"},
	})

	// Set step-c to depend on a and b.
	stepC := r.steps[stepIDs["step-c"]]
	stepC.DependsOn = stepIDs["step-a"] + "," + stepIDs["step-b"]

	runID, err := exec.StartRun(ctx, wfID, nil)
	if err != nil {
		t.Fatalf("start run: %v", err)
	}

	waitForRunStatus(t, ctx, r, runID, types.WorkflowStatusCompleted, 5*time.Second)
}

func TestExecutor_ConditionSkip(t *testing.T) {
	r := newMemWorkflowRepo()
	exec := testExecutor(r)
	ctx := context.Background()

	wfID, _ := createTestWorkflow(t, ctx, r, "conditional", []repo.WorkflowStep{
		{Name: "step-a", StepType: "tool"},
		{Name: "step-b", StepType: "tool", Condition: "ctx.skip == true"},
	})

	// Run with skip=false so step-b's condition evaluates to false.
	runID, err := exec.StartRun(ctx, wfID, map[string]interface{}{"skip": false})
	if err != nil {
		t.Fatalf("start run: %v", err)
	}

	waitForRunStatus(t, ctx, r, runID, types.WorkflowStatusCompleted, 5*time.Second)

	// step-b should be skipped.
	runSteps, _ := r.GetRunSteps(ctx, runID)
	for _, rs := range runSteps {
		step := r.steps[rs.StepID]
		if step.Name == "step-b" && rs.Status != types.StepStatusSkipped {
			t.Errorf("step-b should be skipped, got %q", rs.Status)
		}
	}
}

func TestExecutor_ConditionPass(t *testing.T) {
	r := newMemWorkflowRepo()
	exec := testExecutor(r)
	ctx := context.Background()

	wfID, _ := createTestWorkflow(t, ctx, r, "conditional-pass", []repo.WorkflowStep{
		{Name: "step-a", StepType: "tool", Condition: "ctx.run_it == true"},
	})

	// Run with run_it=true so step-a's condition passes.
	runID, err := exec.StartRun(ctx, wfID, map[string]interface{}{"run_it": true})
	if err != nil {
		t.Fatalf("start run: %v", err)
	}

	waitForRunStatus(t, ctx, r, runID, types.WorkflowStatusCompleted, 5*time.Second)

	// step-a should be completed.
	runSteps, _ := r.GetRunSteps(ctx, runID)
	for _, rs := range runSteps {
		if rs.Status != types.StepStatusCompleted {
			t.Errorf("step should be completed, got %q", rs.Status)
		}
	}
}

func TestExecutor_ApprovalGate(t *testing.T) {
	r := newMemWorkflowRepo()
	exec := testExecutor(r)
	ctx := context.Background()

	wfID, stepIDs := createTestWorkflow(t, ctx, r, "approval", []repo.WorkflowStep{
		{Name: "step-a", StepType: "tool", RequiresApproval: true},
	})

	runID, err := exec.StartRun(ctx, wfID, nil)
	if err != nil {
		t.Fatalf("start run: %v", err)
	}

	// Wait for the step to reach waiting_approval status.
	deadline := time.Now().Add(5 * time.Second)
	stepAID := stepIDs["step-a"]
	for time.Now().Before(deadline) {
		runSteps, _ := r.GetRunSteps(ctx, runID)
		for _, rs := range runSteps {
			if rs.StepID == stepAID && rs.Status == types.StepStatusWaitingApproval {
				goto approved
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("step did not reach waiting_approval status")

approved:
	// Approve the step.
	if err := exec.Approval().ApproveStep(runID, stepAID); err != nil {
		t.Fatalf("approve step: %v", err)
	}

	waitForRunStatus(t, ctx, r, runID, types.WorkflowStatusCompleted, 5*time.Second)
}

func TestExecutor_CancelRun(t *testing.T) {
	r := newMemWorkflowRepo()
	exec := testExecutor(r)
	ctx := context.Background()

	// Create a workflow with an approval step that will block.
	wfID, _ := createTestWorkflow(t, ctx, r, "cancel-test", []repo.WorkflowStep{
		{Name: "blocking-step", StepType: "tool", RequiresApproval: true},
	})

	runID, err := exec.StartRun(ctx, wfID, nil)
	if err != nil {
		t.Fatalf("start run: %v", err)
	}

	// Wait for the step to reach waiting_approval.
	time.Sleep(100 * time.Millisecond)

	// Cancel the run.
	if err := exec.CancelRun(ctx, runID); err != nil {
		t.Fatalf("cancel run: %v", err)
	}

	// Verify run is cancelled.
	run, _ := r.GetRun(ctx, runID)
	if run.Status != types.WorkflowStatusCancelled {
		t.Errorf("run should be cancelled, got %q", run.Status)
	}
}

func TestExecutor_DisabledWorkflow(t *testing.T) {
	r := newMemWorkflowRepo()
	exec := testExecutor(r)
	ctx := context.Background()

	wf := &repo.Workflow{Name: "disabled", Enabled: false}
	wfID, _ := r.CreateWorkflow(ctx, wf)

	_, err := exec.StartRun(ctx, wfID, nil)
	if err == nil {
		t.Error("expected error for disabled workflow")
	}
}

func TestExecutor_EmptyWorkflow(t *testing.T) {
	r := newMemWorkflowRepo()
	exec := testExecutor(r)
	ctx := context.Background()

	wf := &repo.Workflow{Name: "empty", Enabled: true}
	wfID, _ := r.CreateWorkflow(ctx, wf)

	_, err := exec.StartRun(ctx, wfID, nil)
	if err == nil {
		t.Error("expected error for workflow with no steps")
	}
}

func TestExecutor_EventsPublished(t *testing.T) {
	r := newMemWorkflowRepo()
	bus := nervous.NewEventBus(64)
	logger := testLogger()
	exec := NewExecutor(r, bus, logger)
	ctx := context.Background()

	// Subscribe to workflow events.
	sub := bus.SubscribeTypes("test-sub",
		types.EventWorkflowStarted,
		types.EventWorkflowCompleted,
		types.EventWorkflowStepStart,
		types.EventWorkflowStepDone,
	)

	wfID, _ := createTestWorkflow(t, ctx, r, "events", []repo.WorkflowStep{
		{Name: "step-a", StepType: "tool"},
	})

	runID, err := exec.StartRun(ctx, wfID, nil)
	if err != nil {
		t.Fatalf("start run: %v", err)
	}

	waitForRunStatus(t, ctx, r, runID, types.WorkflowStatusCompleted, 5*time.Second)

	// Collect events.
	var events []types.NervousEvent
	timeout := time.After(2 * time.Second)
	for {
		select {
		case ev := <-sub.Ch:
			events = append(events, ev)
			// We expect at least 4 events: started, step_start, step_done, completed.
			if len(events) >= 4 {
				goto verify
			}
		case <-timeout:
			goto verify
		}
	}

verify:
	bus.Unsubscribe("test-sub")

	if len(events) < 4 {
		t.Fatalf("expected at least 4 events, got %d", len(events))
	}

	// Verify first event is workflow.started.
	if events[0].Type != types.EventWorkflowStarted {
		t.Errorf("first event should be workflow.started, got %s", events[0].Type)
	}

	// Verify last event is workflow.completed.
	if events[len(events)-1].Type != types.EventWorkflowCompleted {
		t.Errorf("last event should be workflow.completed, got %s", events[len(events)-1].Type)
	}
}

func TestBuildEvalContext(t *testing.T) {
	runCtx := map[string]interface{}{"env": "prod"}
	stepOutputs := map[string]string{"step-a": "ok"}

	result := buildEvalContext(runCtx, stepOutputs)

	if result["env"] != "prod" {
		t.Error("env should be 'prod'")
	}

	stepsMap, ok := result["steps"].(map[string]interface{})
	if !ok {
		t.Fatal("steps should be a map")
	}
	if stepsMap["step-a"] != "ok" {
		t.Error("step-a output should be 'ok'")
	}
}
