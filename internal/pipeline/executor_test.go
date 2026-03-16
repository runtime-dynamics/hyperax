package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// --- mock PipelineRepo ---

// mockPipelineRepo is an in-memory implementation of repo.PipelineRepo
// used exclusively for unit testing the Executor.
type mockPipelineRepo struct {
	mu          sync.Mutex
	pipelines   map[string]*repo.Pipeline
	jobs        map[string]*repo.PipelineJob
	stepResults map[string][]*repo.StepResult // keyed by job ID
}

func newMockRepo() *mockPipelineRepo {
	return &mockPipelineRepo{
		pipelines:   make(map[string]*repo.Pipeline),
		jobs:        make(map[string]*repo.PipelineJob),
		stepResults: make(map[string][]*repo.StepResult),
	}
}

func (m *mockPipelineRepo) CreatePipeline(_ context.Context, p *repo.Pipeline) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	m.pipelines[p.ID] = p
	return p.ID, nil
}

func (m *mockPipelineRepo) GetPipeline(_ context.Context, id string) (*repo.Pipeline, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pipelines[id]
	if !ok {
		return nil, fmt.Errorf("pipeline %q not found", id)
	}
	return p, nil
}

func (m *mockPipelineRepo) ListPipelines(_ context.Context, workspace string) ([]*repo.Pipeline, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*repo.Pipeline
	for _, p := range m.pipelines {
		if p.WorkspaceName == workspace {
			out = append(out, p)
		}
	}
	return out, nil
}

func (m *mockPipelineRepo) CreateJob(_ context.Context, pipelineID, workspace string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := uuid.New().String()
	now := time.Now()
	m.jobs[id] = &repo.PipelineJob{
		ID:            id,
		PipelineID:    pipelineID,
		Status:        "pending",
		WorkspaceName: workspace,
		StartedAt:     &now,
	}
	return id, nil
}

func (m *mockPipelineRepo) GetJob(_ context.Context, id string) (*repo.PipelineJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return nil, fmt.Errorf("job %q not found", id)
	}
	return j, nil
}

func (m *mockPipelineRepo) UpdateJobStatus(_ context.Context, id, status, result string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return fmt.Errorf("job %q not found", id)
	}
	j.Status = status
	j.Result = result
	if status == "completed" || status == "failed" || status == "cancelled" {
		now := time.Now()
		j.CompletedAt = &now
	}
	return nil
}

func (m *mockPipelineRepo) ListJobs(_ context.Context, pipelineID string) ([]*repo.PipelineJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*repo.PipelineJob
	for _, j := range m.jobs {
		if j.PipelineID == pipelineID {
			out = append(out, j)
		}
	}
	return out, nil
}

func (m *mockPipelineRepo) CreateStepResult(_ context.Context, sr *repo.StepResult) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sr.ID == "" {
		sr.ID = uuid.New().String()
	}
	m.stepResults[sr.JobID] = append(m.stepResults[sr.JobID], sr)
	return sr.ID, nil
}

func (m *mockPipelineRepo) ListStepResults(_ context.Context, jobID string) ([]*repo.StepResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stepResults[jobID], nil
}

func (m *mockPipelineRepo) SearchPipelines(_ context.Context, _ string, _ string) ([]*repo.Pipeline, error) {
	return nil, nil
}

func (m *mockPipelineRepo) ListJobsFiltered(_ context.Context, pipelineID string, filter repo.JobFilter) ([]*repo.PipelineJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*repo.PipelineJob
	for _, j := range m.jobs {
		if j.PipelineID == pipelineID {
			if filter.Status != "" && j.Status != filter.Status {
				continue
			}
			out = append(out, j)
		}
	}
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (m *mockPipelineRepo) CreateAssignment(_ context.Context, _ *repo.PipelineAssignment) (string, error) {
	return uuid.New().String(), nil
}

func (m *mockPipelineRepo) ListAssignments(_ context.Context, _ string, _ string) ([]*repo.PipelineAssignment, error) {
	return nil, nil
}

func (m *mockPipelineRepo) DeleteAssignment(_ context.Context, _ string) error {
	return nil
}

// --- helpers ---

// createTestPipeline creates a pipeline and a pending job in the mock repo.
// It returns the pipeline ID, job ID, and the mock repo.
func createTestPipeline(t *testing.T, swimlanes, setupCmds, env string) (*mockPipelineRepo, string, string) {
	t.Helper()

	mockRepo := newMockRepo()
	ctx := context.Background()

	pipelineID, err := mockRepo.CreatePipeline(ctx, &repo.Pipeline{
		Name:          "test-pipeline",
		WorkspaceName: "ws-test",
		Swimlanes:     swimlanes,
		SetupCommands: setupCmds,
		Environment:   env,
	})
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}

	jobID, err := mockRepo.CreateJob(ctx, pipelineID, "ws-test")
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	return mockRepo, pipelineID, jobID
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// readStepOutput resolves a step's OutputLog which may be either inline text
// or a "file:/path" reference to a log file written by the executor.
func readStepOutput(t *testing.T, outputLog string) string {
	t.Helper()
	if strings.HasPrefix(outputLog, "file:") {
		path := strings.TrimPrefix(outputLog, "file:")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read step log file %q: %v", path, err)
		}
		return string(data)
	}
	return outputLog
}

// --- tests ---

func TestExecute_SuccessfulPipeline(t *testing.T) {
	swimlanes := `[
		{
			"id": "build",
			"name": "Build",
			"steps": [
				{"id": "s1", "name": "Echo Hello", "command": "echo hello"},
				{"id": "s2", "name": "True", "command": "true"}
			]
		}
	]`

	mockRepo, _, jobID := createTestPipeline(t, swimlanes, `[]`, `{}`)
	bus := nervous.NewEventBus(64)
	executor := NewExecutor(mockRepo, bus, testLogger())

	workDir := t.TempDir()
	err := executor.Execute(context.Background(), jobID, workDir)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// Verify job status is completed.
	job, err := mockRepo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != "completed" {
		t.Errorf("job status = %q, want %q", job.Status, "completed")
	}
	if job.Result != "all steps passed" {
		t.Errorf("job result = %q, want %q", job.Result, "all steps passed")
	}

	// Verify step results were recorded.
	steps, err := mockRepo.ListStepResults(context.Background(), jobID)
	if err != nil {
		t.Fatalf("list step results: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 step results, got %d", len(steps))
	}
	for _, s := range steps {
		if s.Status != "passed" {
			t.Errorf("step %q status = %q, want %q", s.StepName, s.Status, "passed")
		}
	}
}

func TestExecute_FailedStepStopsExecution(t *testing.T) {
	swimlanes := `[
		{
			"id": "build",
			"name": "Build",
			"steps": [
				{"id": "s1", "name": "Will Fail", "command": "exit 42"},
				{"id": "s2", "name": "Should Not Run", "command": "echo unreachable"}
			]
		}
	]`

	mockRepo, _, jobID := createTestPipeline(t, swimlanes, `[]`, `{}`)
	bus := nervous.NewEventBus(64)
	executor := NewExecutor(mockRepo, bus, testLogger())

	workDir := t.TempDir()
	err := executor.Execute(context.Background(), jobID, workDir)
	if err == nil {
		t.Fatal("expected error from failed step, got nil")
	}

	// Verify job status is failed.
	job, err := mockRepo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != "failed" {
		t.Errorf("job status = %q, want %q", job.Status, "failed")
	}

	// Only one step should have run.
	steps, err := mockRepo.ListStepResults(context.Background(), jobID)
	if err != nil {
		t.Fatalf("list step results: %v", err)
	}
	if len(steps) != 1 {
		t.Errorf("expected 1 step result (second should not run), got %d", len(steps))
	}
	if steps[0].ExitCode == nil || *steps[0].ExitCode != 42 {
		t.Errorf("exit code = %v, want 42", steps[0].ExitCode)
	}
}

func TestExecute_CapturesOutput(t *testing.T) {
	swimlanes := `[
		{
			"id": "echo",
			"name": "Echo",
			"steps": [
				{"id": "s1", "name": "Greeting", "command": "echo 'hello world'"}
			]
		}
	]`

	mockRepo, _, jobID := createTestPipeline(t, swimlanes, `[]`, `{}`)
	bus := nervous.NewEventBus(64)
	executor := NewExecutor(mockRepo, bus, testLogger())

	workDir := t.TempDir()
	if err := executor.Execute(context.Background(), jobID, workDir); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	steps, err := mockRepo.ListStepResults(context.Background(), jobID)
	if err != nil {
		t.Fatalf("list step results: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}

	output := readStepOutput(t, steps[0].OutputLog)
	if strings.TrimSpace(output) != "hello world" {
		t.Errorf("output = %q, want %q", strings.TrimSpace(output), "hello world")
	}
}

func TestExecute_EnvironmentVariablesApplied(t *testing.T) {
	swimlanes := `[
		{
			"id": "env",
			"name": "Env Test",
			"steps": [
				{"id": "s1", "name": "Print Env", "command": "echo $MY_VAR"}
			]
		}
	]`
	env := `{"MY_VAR": "test_value_123"}`

	mockRepo, _, jobID := createTestPipeline(t, swimlanes, `[]`, env)
	bus := nervous.NewEventBus(64)
	executor := NewExecutor(mockRepo, bus, testLogger())

	workDir := t.TempDir()
	if err := executor.Execute(context.Background(), jobID, workDir); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	steps, err := mockRepo.ListStepResults(context.Background(), jobID)
	if err != nil {
		t.Fatalf("list step results: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}

	output := readStepOutput(t, steps[0].OutputLog)
	if strings.TrimSpace(output) != "test_value_123" {
		t.Errorf("output = %q, want %q", strings.TrimSpace(output), "test_value_123")
	}
}

func TestExecute_ContextCancellationStopsExecution(t *testing.T) {
	swimlanes := `[
		{
			"id": "slow",
			"name": "Slow",
			"steps": [
				{"id": "s1", "name": "Sleep", "command": "sleep 60"}
			]
		}
	]`

	mockRepo, _, jobID := createTestPipeline(t, swimlanes, `[]`, `{}`)
	bus := nervous.NewEventBus(64)
	executor := NewExecutor(mockRepo, bus, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	workDir := t.TempDir()

	// Cancel after a short delay to allow execution to start.
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	err := executor.Execute(ctx, jobID, workDir)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}

	// Verify job is marked as failed.
	job, err := mockRepo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != "failed" {
		t.Errorf("job status = %q, want %q", job.Status, "failed")
	}
}

func TestExecute_SetupCommandsRunBeforeSwimlanes(t *testing.T) {
	// The setup command creates a file; the swimlane step checks for it.
	swimlanes := `[
		{
			"id": "verify",
			"name": "Verify",
			"steps": [
				{"id": "s1", "name": "Check File", "command": "cat setup_marker.txt"}
			]
		}
	]`
	setupCmds := `["echo 'setup_done' > setup_marker.txt"]`

	mockRepo, _, jobID := createTestPipeline(t, swimlanes, setupCmds, `{}`)
	bus := nervous.NewEventBus(64)
	executor := NewExecutor(mockRepo, bus, testLogger())

	workDir := t.TempDir()
	if err := executor.Execute(context.Background(), jobID, workDir); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Verify job completed (the cat would fail if the file didn't exist).
	job, err := mockRepo.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != "completed" {
		t.Errorf("job status = %q, want %q", job.Status, "completed")
	}

	// The setup step plus the swimlane step should produce 2 step results.
	steps, err := mockRepo.ListStepResults(context.Background(), jobID)
	if err != nil {
		t.Fatalf("list step results: %v", err)
	}
	if len(steps) != 2 {
		t.Errorf("expected 2 step results (1 setup + 1 swimlane), got %d", len(steps))
	}

	// Verify the check step output contains the marker.
	for _, s := range steps {
		if s.StepName == "Check File" {
			output := readStepOutput(t, s.OutputLog)
			if !strings.Contains(output, "setup_done") {
				t.Errorf("check step output = %q, expected to contain 'setup_done'", output)
			}
		}
	}
}

func TestExecute_MultipleSwimlanes(t *testing.T) {
	swimlanes := `[
		{
			"id": "first",
			"name": "First Lane",
			"steps": [
				{"id": "a1", "name": "Step A", "command": "echo a"}
			]
		},
		{
			"id": "second",
			"name": "Second Lane",
			"steps": [
				{"id": "b1", "name": "Step B", "command": "echo b"},
				{"id": "b2", "name": "Step C", "command": "echo c"}
			]
		}
	]`

	mockRepo, _, jobID := createTestPipeline(t, swimlanes, `[]`, `{}`)
	bus := nervous.NewEventBus(64)
	executor := NewExecutor(mockRepo, bus, testLogger())

	workDir := t.TempDir()
	if err := executor.Execute(context.Background(), jobID, workDir); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	steps, err := mockRepo.ListStepResults(context.Background(), jobID)
	if err != nil {
		t.Fatalf("list step results: %v", err)
	}
	if len(steps) != 3 {
		t.Fatalf("expected 3 step results across 2 swimlanes, got %d", len(steps))
	}
}

func TestExecute_EventsPublished(t *testing.T) {
	swimlanes := `[
		{
			"id": "build",
			"name": "Build",
			"steps": [
				{"id": "s1", "name": "Echo", "command": "echo test"}
			]
		}
	]`

	mockRepo, _, jobID := createTestPipeline(t, swimlanes, `[]`, `{}`)
	bus := nervous.NewEventBus(64)

	// Subscribe to all pipeline events before execution.
	sub := bus.SubscribeTypes("test-pipeline-events",
		types.EventPipelineStart,
		types.EventPipelineLog,
		types.EventPipelineComplete,
	)
	defer bus.Unsubscribe("test-pipeline-events")

	executor := NewExecutor(mockRepo, bus, testLogger())

	workDir := t.TempDir()
	if err := executor.Execute(context.Background(), jobID, workDir); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Drain events: expect start (1) + log per step (1) + complete (1) = 3.
	var events []types.NervousEvent
	timeout := time.After(2 * time.Second)
	for i := 0; i < 3; i++ {
		select {
		case e := <-sub.Ch:
			events = append(events, e)
		case <-timeout:
			t.Fatalf("timeout waiting for events; received %d of 3", len(events))
		}
	}

	if events[0].Type != types.EventPipelineStart {
		t.Errorf("event[0] type = %s, want %s", events[0].Type, types.EventPipelineStart)
	}
	if events[1].Type != types.EventPipelineLog {
		t.Errorf("event[1] type = %s, want %s", events[1].Type, types.EventPipelineLog)
	}
	if events[2].Type != types.EventPipelineComplete {
		t.Errorf("event[2] type = %s, want %s", events[2].Type, types.EventPipelineComplete)
	}
}

func TestCancelJob(t *testing.T) {
	swimlanes := `[
		{
			"id": "slow",
			"name": "Slow",
			"steps": [
				{"id": "s1", "name": "Sleep", "command": "sleep 60"}
			]
		}
	]`

	mockRepo, _, jobID := createTestPipeline(t, swimlanes, `[]`, `{}`)
	bus := nervous.NewEventBus(64)
	executor := NewExecutor(mockRepo, bus, testLogger())

	workDir := t.TempDir()

	done := make(chan error, 1)
	go func() {
		done <- executor.Execute(context.Background(), jobID, workDir)
	}()

	// Wait for the job to start, then cancel it.
	time.Sleep(300 * time.Millisecond)
	cancelled := executor.CancelJob(jobID)
	if !cancelled {
		t.Error("CancelJob returned false, expected true")
	}

	select {
	case err := <-done:
		if err == nil {
			t.Error("expected error after cancellation, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for cancelled execution to finish")
	}
}
