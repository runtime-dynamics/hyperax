package sqlite

import (
	"context"
	"testing"

	"github.com/hyperax/hyperax/internal/repo"
)

func newPipelineRepo(t *testing.T) (*PipelineRepo, context.Context) {
	t.Helper()
	db, ctx := setupTestDB(t)
	return &PipelineRepo{db: db.db}, ctx
}

func TestPipelineRepo_CreateAndGet(t *testing.T) {
	r, ctx := newPipelineRepo(t)

	p := &repo.Pipeline{
		Name:          "lint",
		Description:   "Run linters",
		WorkspaceName: "ws1",
		Swimlanes:     `[{"id":"s1","name":"lint"}]`,
		SetupCommands: `["go mod tidy"]`,
		Environment:   `{"GO111MODULE":"on"}`,
	}

	id, err := r.CreatePipeline(ctx, p)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty id")
	}

	got, err := r.GetPipeline(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.Name != "lint" {
		t.Errorf("name = %q, want %q", got.Name, "lint")
	}
	if got.Description != "Run linters" {
		t.Errorf("description = %q, want %q", got.Description, "Run linters")
	}
	if got.WorkspaceName != "ws1" {
		t.Errorf("workspace_name = %q, want %q", got.WorkspaceName, "ws1")
	}
	if got.Swimlanes != `[{"id":"s1","name":"lint"}]` {
		t.Errorf("swimlanes = %q", got.Swimlanes)
	}
}

func TestPipelineRepo_GetNotFound(t *testing.T) {
	r, ctx := newPipelineRepo(t)

	_, err := r.GetPipeline(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent pipeline")
	}
}

func TestPipelineRepo_ListPipelines(t *testing.T) {
	r, ctx := newPipelineRepo(t)

	// Empty initially
	list, err := r.ListPipelines(ctx, "ws1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0 pipelines, got %d", len(list))
	}

	// Add two pipelines
	_, _ = r.CreatePipeline(ctx, &repo.Pipeline{Name: "lint", WorkspaceName: "ws1", Swimlanes: "[]", SetupCommands: "[]", Environment: "{}"})
	_, _ = r.CreatePipeline(ctx, &repo.Pipeline{Name: "test", WorkspaceName: "ws1", Swimlanes: "[]", SetupCommands: "[]", Environment: "{}"})
	_, _ = r.CreatePipeline(ctx, &repo.Pipeline{Name: "other", WorkspaceName: "ws2", Swimlanes: "[]", SetupCommands: "[]", Environment: "{}"})

	list, err = r.ListPipelines(ctx, "ws1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 pipelines for ws1, got %d", len(list))
	}
}

func TestPipelineRepo_CreateJobAndGet(t *testing.T) {
	r, ctx := newPipelineRepo(t)

	pipelineID, err := r.CreatePipeline(ctx, &repo.Pipeline{
		Name: "build", WorkspaceName: "ws1",
		Swimlanes: "[]", SetupCommands: "[]", Environment: "{}",
	})
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}

	jobID, err := r.CreateJob(ctx, pipelineID, "ws1")
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if jobID == "" {
		t.Fatal("expected non-empty job id")
	}

	job, err := r.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}

	if job.PipelineID != pipelineID {
		t.Errorf("pipeline_id = %q, want %q", job.PipelineID, pipelineID)
	}
	if job.Status != "pending" {
		t.Errorf("status = %q, want %q", job.Status, "pending")
	}
	if job.WorkspaceName != "ws1" {
		t.Errorf("workspace_name = %q, want %q", job.WorkspaceName, "ws1")
	}
}

func TestPipelineRepo_GetJobNotFound(t *testing.T) {
	r, ctx := newPipelineRepo(t)

	_, err := r.GetJob(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent job")
	}
}

func TestPipelineRepo_UpdateJobStatus(t *testing.T) {
	r, ctx := newPipelineRepo(t)

	pipelineID, err := r.CreatePipeline(ctx, &repo.Pipeline{
		Name: "deploy", WorkspaceName: "ws1",
		Swimlanes: "[]", SetupCommands: "[]", Environment: "{}",
	})
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}
	jobID, err := r.CreateJob(ctx, pipelineID, "ws1")
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	// Transition to running
	if err := r.UpdateJobStatus(ctx, jobID, "running", ""); err != nil {
		t.Fatalf("update to running: %v", err)
	}
	job, err := r.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != "running" {
		t.Errorf("status = %q, want %q", job.Status, "running")
	}

	// Transition to completed (should set completed_at)
	if err := r.UpdateJobStatus(ctx, jobID, "completed", "all passed"); err != nil {
		t.Fatalf("update to completed: %v", err)
	}
	job, err = r.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != "completed" {
		t.Errorf("status = %q, want %q", job.Status, "completed")
	}
	if job.Result != "all passed" {
		t.Errorf("result = %q, want %q", job.Result, "all passed")
	}
	if job.CompletedAt == nil {
		t.Error("expected completed_at to be set")
	}
}

func TestPipelineRepo_UpdateJobStatus_NotFound(t *testing.T) {
	r, ctx := newPipelineRepo(t)

	err := r.UpdateJobStatus(ctx, "nonexistent", "running", "")
	if err == nil {
		t.Error("expected error for nonexistent job")
	}
}

func TestPipelineRepo_ListJobs(t *testing.T) {
	r, ctx := newPipelineRepo(t)

	pipelineID, err := r.CreatePipeline(ctx, &repo.Pipeline{
		Name: "ci", WorkspaceName: "ws1",
		Swimlanes: "[]", SetupCommands: "[]", Environment: "{}",
	})
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}

	if _, err := r.CreateJob(ctx, pipelineID, "ws1"); err != nil {
		t.Fatalf("create job 1: %v", err)
	}
	if _, err := r.CreateJob(ctx, pipelineID, "ws1"); err != nil {
		t.Fatalf("create job 2: %v", err)
	}

	jobs, err := r.ListJobs(ctx, pipelineID)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Errorf("expected 2 jobs, got %d", len(jobs))
	}
}

func TestPipelineRepo_StepResults(t *testing.T) {
	r, ctx := newPipelineRepo(t)

	pipelineID, err := r.CreatePipeline(ctx, &repo.Pipeline{
		Name: "ci", WorkspaceName: "ws1",
		Swimlanes: "[]", SetupCommands: "[]", Environment: "{}",
	})
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}
	jobID, err := r.CreateJob(ctx, pipelineID, "ws1")
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	exitCode := 0
	sr := &repo.StepResult{
		JobID:      jobID,
		SwimlaneID: "swim-1",
		StepID:     "step-1",
		StepName:   "go vet",
		Status:     "pass",
		ExitCode:   &exitCode,
		OutputLog:  "no issues found",
	}

	stepID, err := r.CreateStepResult(ctx, sr)
	if err != nil {
		t.Fatalf("create step result: %v", err)
	}
	if stepID == "" {
		t.Fatal("expected non-empty step result id")
	}

	// Add a second step result
	failCode := 1
	sr2 := &repo.StepResult{
		JobID:      jobID,
		SwimlaneID: "swim-1",
		StepID:     "step-2",
		StepName:   "go test",
		Status:     "fail",
		ExitCode:   &failCode,
		OutputLog:  "FAIL: TestFoo",
		Error:      "test failed",
	}
	if _, err := r.CreateStepResult(ctx, sr2); err != nil {
		t.Fatalf("create step result 2: %v", err)
	}

	results, err := r.ListStepResults(ctx, jobID)
	if err != nil {
		t.Fatalf("list step results: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 step results, got %d", len(results))
	}

	// Verify first result fields (ordered by step_name: "go test" < "go vet")
	first := results[0]
	if first.StepName != "go test" {
		t.Errorf("first step name = %q, want %q", first.StepName, "go test")
	}
	if first.Status != "fail" {
		t.Errorf("first status = %q, want %q", first.Status, "fail")
	}
	if first.ExitCode == nil || *first.ExitCode != 1 {
		t.Errorf("first exit_code = %v, want 1", first.ExitCode)
	}

	second := results[1]
	if second.StepName != "go vet" {
		t.Errorf("second step name = %q, want %q", second.StepName, "go vet")
	}
	if second.OutputLog != "no issues found" {
		t.Errorf("second output_log = %q", second.OutputLog)
	}
}

func TestPipelineRepo_ListStepResults_Empty(t *testing.T) {
	r, ctx := newPipelineRepo(t)

	pipelineID, err := r.CreatePipeline(ctx, &repo.Pipeline{
		Name: "ci", WorkspaceName: "ws1",
		Swimlanes: "[]", SetupCommands: "[]", Environment: "{}",
	})
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}
	jobID, err := r.CreateJob(ctx, pipelineID, "ws1")
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	results, err := r.ListStepResults(ctx, jobID)
	if err != nil {
		t.Fatalf("list step results: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 step results, got %d", len(results))
	}
}

// --- SearchPipelines tests ---

func TestPipelineRepo_SearchPipelines_ByQuery(t *testing.T) {
	r, ctx := newPipelineRepo(t)

	_, _ = r.CreatePipeline(ctx, &repo.Pipeline{Name: "go-lint", Description: "Run Go linters", WorkspaceName: "ws1", Swimlanes: "[]", SetupCommands: "[]", Environment: "{}"})
	_, _ = r.CreatePipeline(ctx, &repo.Pipeline{Name: "go-test", Description: "Run Go tests", WorkspaceName: "ws1", Swimlanes: "[]", SetupCommands: "[]", Environment: "{}"})
	_, _ = r.CreatePipeline(ctx, &repo.Pipeline{Name: "deploy", Description: "Deploy to staging", WorkspaceName: "ws2", Swimlanes: "[]", SetupCommands: "[]", Environment: "{}"})

	// Search by name pattern across all workspaces.
	results, err := r.SearchPipelines(ctx, "go-", "")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results for query 'go-', got %d", len(results))
	}

	// Search by description pattern.
	results, err = r.SearchPipelines(ctx, "staging", "")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for query 'staging', got %d", len(results))
	}
}

func TestPipelineRepo_SearchPipelines_ByWorkspace(t *testing.T) {
	r, ctx := newPipelineRepo(t)

	_, _ = r.CreatePipeline(ctx, &repo.Pipeline{Name: "lint", WorkspaceName: "ws1", Swimlanes: "[]", SetupCommands: "[]", Environment: "{}"})
	_, _ = r.CreatePipeline(ctx, &repo.Pipeline{Name: "test", WorkspaceName: "ws1", Swimlanes: "[]", SetupCommands: "[]", Environment: "{}"})
	_, _ = r.CreatePipeline(ctx, &repo.Pipeline{Name: "deploy", WorkspaceName: "ws2", Swimlanes: "[]", SetupCommands: "[]", Environment: "{}"})

	// Filter by workspace only.
	results, err := r.SearchPipelines(ctx, "", "ws1")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results for ws1, got %d", len(results))
	}
}

func TestPipelineRepo_SearchPipelines_ByQueryAndWorkspace(t *testing.T) {
	r, ctx := newPipelineRepo(t)

	_, _ = r.CreatePipeline(ctx, &repo.Pipeline{Name: "lint", WorkspaceName: "ws1", Swimlanes: "[]", SetupCommands: "[]", Environment: "{}"})
	_, _ = r.CreatePipeline(ctx, &repo.Pipeline{Name: "lint", WorkspaceName: "ws2", Swimlanes: "[]", SetupCommands: "[]", Environment: "{}"})

	// Search with both query and workspace.
	results, err := r.SearchPipelines(ctx, "lint", "ws1")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for lint+ws1, got %d", len(results))
	}
}

func TestPipelineRepo_SearchPipelines_All(t *testing.T) {
	r, ctx := newPipelineRepo(t)

	_, _ = r.CreatePipeline(ctx, &repo.Pipeline{Name: "a", WorkspaceName: "ws1", Swimlanes: "[]", SetupCommands: "[]", Environment: "{}"})
	_, _ = r.CreatePipeline(ctx, &repo.Pipeline{Name: "b", WorkspaceName: "ws2", Swimlanes: "[]", SetupCommands: "[]", Environment: "{}"})

	// No filters returns all.
	results, err := r.SearchPipelines(ctx, "", "")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestPipelineRepo_SearchPipelines_NoMatch(t *testing.T) {
	r, ctx := newPipelineRepo(t)

	_, _ = r.CreatePipeline(ctx, &repo.Pipeline{Name: "lint", WorkspaceName: "ws1", Swimlanes: "[]", SetupCommands: "[]", Environment: "{}"})

	results, err := r.SearchPipelines(ctx, "nonexistent", "")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// --- ListJobsFiltered tests ---

func TestPipelineRepo_ListJobsFiltered_NoFilter(t *testing.T) {
	r, ctx := newPipelineRepo(t)

	pipelineID, err := r.CreatePipeline(ctx, &repo.Pipeline{
		Name: "ci", WorkspaceName: "ws1",
		Swimlanes: "[]", SetupCommands: "[]", Environment: "{}",
	})
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}

	job1, err := r.CreateJob(ctx, pipelineID, "ws1")
	if err != nil {
		t.Fatalf("create job 1: %v", err)
	}
	job2, err := r.CreateJob(ctx, pipelineID, "ws1")
	if err != nil {
		t.Fatalf("create job 2: %v", err)
	}
	if err := r.UpdateJobStatus(ctx, job1, "completed", "ok"); err != nil {
		t.Fatalf("update job 1: %v", err)
	}
	if err := r.UpdateJobStatus(ctx, job2, "failed", "err"); err != nil {
		t.Fatalf("update job 2: %v", err)
	}

	jobs, err := r.ListJobsFiltered(ctx, pipelineID, repo.JobFilter{})
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if len(jobs) != 2 {
		t.Errorf("expected 2 jobs, got %d", len(jobs))
	}
}

func TestPipelineRepo_ListJobsFiltered_ByStatus(t *testing.T) {
	r, ctx := newPipelineRepo(t)

	pipelineID, err := r.CreatePipeline(ctx, &repo.Pipeline{
		Name: "ci", WorkspaceName: "ws1",
		Swimlanes: "[]", SetupCommands: "[]", Environment: "{}",
	})
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}

	job1, err := r.CreateJob(ctx, pipelineID, "ws1")
	if err != nil {
		t.Fatalf("create job 1: %v", err)
	}
	job2, err := r.CreateJob(ctx, pipelineID, "ws1")
	if err != nil {
		t.Fatalf("create job 2: %v", err)
	}
	if err := r.UpdateJobStatus(ctx, job1, "completed", "ok"); err != nil {
		t.Fatalf("update job 1: %v", err)
	}
	if err := r.UpdateJobStatus(ctx, job2, "failed", "err"); err != nil {
		t.Fatalf("update job 2: %v", err)
	}

	jobs, err := r.ListJobsFiltered(ctx, pipelineID, repo.JobFilter{Status: "completed"})
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("expected 1 completed job, got %d", len(jobs))
	}
	if jobs[0].ID != job1 {
		t.Errorf("expected job %q, got %q", job1, jobs[0].ID)
	}
}

func TestPipelineRepo_ListJobsFiltered_WithLimit(t *testing.T) {
	r, ctx := newPipelineRepo(t)

	pipelineID, err := r.CreatePipeline(ctx, &repo.Pipeline{
		Name: "ci", WorkspaceName: "ws1",
		Swimlanes: "[]", SetupCommands: "[]", Environment: "{}",
	})
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}

	if _, err := r.CreateJob(ctx, pipelineID, "ws1"); err != nil {
		t.Fatalf("create job 1: %v", err)
	}
	if _, err := r.CreateJob(ctx, pipelineID, "ws1"); err != nil {
		t.Fatalf("create job 2: %v", err)
	}
	if _, err := r.CreateJob(ctx, pipelineID, "ws1"); err != nil {
		t.Fatalf("create job 3: %v", err)
	}

	jobs, err := r.ListJobsFiltered(ctx, pipelineID, repo.JobFilter{Limit: 2})
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if len(jobs) != 2 {
		t.Errorf("expected 2 jobs with limit, got %d", len(jobs))
	}
}

func TestPipelineRepo_ListJobsFiltered_StatusAndLimit(t *testing.T) {
	r, ctx := newPipelineRepo(t)

	pipelineID, err := r.CreatePipeline(ctx, &repo.Pipeline{
		Name: "ci", WorkspaceName: "ws1",
		Swimlanes: "[]", SetupCommands: "[]", Environment: "{}",
	})
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}

	j1, err := r.CreateJob(ctx, pipelineID, "ws1")
	if err != nil {
		t.Fatalf("create job 1: %v", err)
	}
	j2, err := r.CreateJob(ctx, pipelineID, "ws1")
	if err != nil {
		t.Fatalf("create job 2: %v", err)
	}
	j3, err := r.CreateJob(ctx, pipelineID, "ws1")
	if err != nil {
		t.Fatalf("create job 3: %v", err)
	}
	_ = r.UpdateJobStatus(ctx, j1, "completed", "ok")
	_ = r.UpdateJobStatus(ctx, j2, "completed", "ok")
	_ = r.UpdateJobStatus(ctx, j3, "failed", "err")

	jobs, err := r.ListJobsFiltered(ctx, pipelineID, repo.JobFilter{Status: "completed", Limit: 1})
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("expected 1 job with status+limit, got %d", len(jobs))
	}
	if jobs[0].Status != "completed" {
		t.Errorf("expected completed, got %q", jobs[0].Status)
	}
}

// --- Assignment tests ---

func TestPipelineRepo_CreateAndListAssignments(t *testing.T) {
	r, ctx := newPipelineRepo(t)

	pipelineID, err := r.CreatePipeline(ctx, &repo.Pipeline{
		Name: "ci", WorkspaceName: "ws1",
		Swimlanes: "[]", SetupCommands: "[]", Environment: "{}",
	})
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}

	assignment := &repo.PipelineAssignment{
		PipelineID:  pipelineID,
		WorkspaceID: "ws1",
		ProjectID:   "proj-1",
	}

	id, err := r.CreateAssignment(ctx, assignment)
	if err != nil {
		t.Fatalf("create assignment: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty assignment id")
	}

	// List all assignments.
	assignments, err := r.ListAssignments(ctx, "", "")
	if err != nil {
		t.Fatalf("list assignments: %v", err)
	}
	if len(assignments) != 1 {
		t.Fatalf("expected 1 assignment, got %d", len(assignments))
	}
	if assignments[0].PipelineID != pipelineID {
		t.Errorf("pipeline_id = %q, want %q", assignments[0].PipelineID, pipelineID)
	}
	if assignments[0].WorkspaceID != "ws1" {
		t.Errorf("workspace_id = %q, want %q", assignments[0].WorkspaceID, "ws1")
	}
	if assignments[0].ProjectID != "proj-1" {
		t.Errorf("project_id = %q, want %q", assignments[0].ProjectID, "proj-1")
	}
}

func TestPipelineRepo_ListAssignments_FilterByWorkspace(t *testing.T) {
	r, ctx := newPipelineRepo(t)

	pid1, err := r.CreatePipeline(ctx, &repo.Pipeline{Name: "a", WorkspaceName: "ws1", Swimlanes: "[]", SetupCommands: "[]", Environment: "{}"})
	if err != nil {
		t.Fatalf("create pipeline 1: %v", err)
	}
	pid2, err := r.CreatePipeline(ctx, &repo.Pipeline{Name: "b", WorkspaceName: "ws2", Swimlanes: "[]", SetupCommands: "[]", Environment: "{}"})
	if err != nil {
		t.Fatalf("create pipeline 2: %v", err)
	}

	if _, err := r.CreateAssignment(ctx, &repo.PipelineAssignment{PipelineID: pid1, WorkspaceID: "ws1"}); err != nil {
		t.Fatalf("create assignment 1: %v", err)
	}
	if _, err := r.CreateAssignment(ctx, &repo.PipelineAssignment{PipelineID: pid2, WorkspaceID: "ws2"}); err != nil {
		t.Fatalf("create assignment 2: %v", err)
	}

	assignments, err := r.ListAssignments(ctx, "ws1", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(assignments) != 1 {
		t.Errorf("expected 1 assignment for ws1, got %d", len(assignments))
	}
}

func TestPipelineRepo_ListAssignments_FilterByPipeline(t *testing.T) {
	r, ctx := newPipelineRepo(t)

	pid1, err := r.CreatePipeline(ctx, &repo.Pipeline{Name: "a", WorkspaceName: "ws1", Swimlanes: "[]", SetupCommands: "[]", Environment: "{}"})
	if err != nil {
		t.Fatalf("create pipeline 1: %v", err)
	}
	pid2, err := r.CreatePipeline(ctx, &repo.Pipeline{Name: "b", WorkspaceName: "ws1", Swimlanes: "[]", SetupCommands: "[]", Environment: "{}"})
	if err != nil {
		t.Fatalf("create pipeline 2: %v", err)
	}

	if _, err := r.CreateAssignment(ctx, &repo.PipelineAssignment{PipelineID: pid1, WorkspaceID: "ws1"}); err != nil {
		t.Fatalf("create assignment 1: %v", err)
	}
	if _, err := r.CreateAssignment(ctx, &repo.PipelineAssignment{PipelineID: pid2, WorkspaceID: "ws1"}); err != nil {
		t.Fatalf("create assignment 2: %v", err)
	}

	assignments, err := r.ListAssignments(ctx, "", pid1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(assignments) != 1 {
		t.Errorf("expected 1 assignment for pipeline, got %d", len(assignments))
	}
}

func TestPipelineRepo_ListAssignments_FilterBoth(t *testing.T) {
	r, ctx := newPipelineRepo(t)

	pid, err := r.CreatePipeline(ctx, &repo.Pipeline{Name: "a", WorkspaceName: "ws1", Swimlanes: "[]", SetupCommands: "[]", Environment: "{}"})
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}

	if _, err := r.CreateAssignment(ctx, &repo.PipelineAssignment{PipelineID: pid, WorkspaceID: "ws1"}); err != nil {
		t.Fatalf("create assignment 1: %v", err)
	}
	if _, err := r.CreateAssignment(ctx, &repo.PipelineAssignment{PipelineID: pid, WorkspaceID: "ws2"}); err != nil {
		t.Fatalf("create assignment 2: %v", err)
	}

	assignments, err := r.ListAssignments(ctx, "ws1", pid)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(assignments) != 1 {
		t.Errorf("expected 1 assignment for ws1+pipeline, got %d", len(assignments))
	}
}

func TestPipelineRepo_DeleteAssignment(t *testing.T) {
	r, ctx := newPipelineRepo(t)

	pid, err := r.CreatePipeline(ctx, &repo.Pipeline{Name: "a", WorkspaceName: "ws1", Swimlanes: "[]", SetupCommands: "[]", Environment: "{}"})
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}
	id, err := r.CreateAssignment(ctx, &repo.PipelineAssignment{PipelineID: pid, WorkspaceID: "ws1"})
	if err != nil {
		t.Fatalf("create assignment: %v", err)
	}

	if err := r.DeleteAssignment(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Verify it's gone.
	assignments, err := r.ListAssignments(ctx, "", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(assignments) != 0 {
		t.Errorf("expected 0 assignments after delete, got %d", len(assignments))
	}
}

func TestPipelineRepo_DeleteAssignment_NotFound(t *testing.T) {
	r, ctx := newPipelineRepo(t)

	err := r.DeleteAssignment(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent assignment")
	}
}
