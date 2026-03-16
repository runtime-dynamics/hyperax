package sqlite

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/repo"
)

func newCronRepo(t *testing.T) (*CronRepo, context.Context) {
	t.Helper()
	db, ctx := setupTestDB(t)
	return &CronRepo{db: db.db}, ctx
}

func TestCronRepo_CreateAndGetJob(t *testing.T) {
	r, ctx := newCronRepo(t)

	nextRun := time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)
	job := &repo.CronJob{
		Name:       "daily-backup",
		Schedule:   "0 0 * * *",
		JobType:    "pipeline",
		Payload:    json.RawMessage(`{"pipeline_id":"abc"}`),
		Enabled:    true,
		MaxRetries: 3,
		NextRunAt:  &nextRun,
	}

	id, err := r.CreateJob(ctx, job)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty ID")
	}

	got, err := r.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.Name != "daily-backup" {
		t.Errorf("name = %q, want %q", got.Name, "daily-backup")
	}
	if got.Schedule != "0 0 * * *" {
		t.Errorf("schedule = %q, want %q", got.Schedule, "0 0 * * *")
	}
	if got.JobType != "pipeline" {
		t.Errorf("job_type = %q, want %q", got.JobType, "pipeline")
	}
	if !got.Enabled {
		t.Error("expected enabled = true")
	}
	if got.MaxRetries != 3 {
		t.Errorf("max_retries = %d, want 3", got.MaxRetries)
	}
	if got.NextRunAt == nil {
		t.Error("expected non-nil next_run_at")
	}
	if got.CreatedAt.IsZero() {
		t.Error("expected non-zero created_at")
	}
}

func TestCronRepo_GetJobNotFound(t *testing.T) {
	r, ctx := newCronRepo(t)

	_, err := r.GetJob(ctx, "nonexistent-id")
	if err == nil {
		t.Error("expected error for nonexistent cron job")
	}
}

func TestCronRepo_ListJobs(t *testing.T) {
	r, ctx := newCronRepo(t)

	// Empty initially.
	list, err := r.ListJobs(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0, got %d", len(list))
	}

	// Add two jobs.
	_, err = r.CreateJob(ctx, &repo.CronJob{
		Name: "alpha", Schedule: "@hourly", JobType: "tool",
		Payload: json.RawMessage("{}"), Enabled: true,
	})
	if err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	_, err = r.CreateJob(ctx, &repo.CronJob{
		Name: "beta", Schedule: "@daily", JobType: "tool",
		Payload: json.RawMessage("{}"), Enabled: true,
	})
	if err != nil {
		t.Fatalf("create beta: %v", err)
	}

	list, err = r.ListJobs(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2, got %d", len(list))
	}

	// Should be ordered by name.
	if list[0].Name != "alpha" {
		t.Errorf("first = %q, want %q", list[0].Name, "alpha")
	}
	if list[1].Name != "beta" {
		t.Errorf("second = %q, want %q", list[1].Name, "beta")
	}
}

func TestCronRepo_UpdateJob(t *testing.T) {
	r, ctx := newCronRepo(t)

	id, err := r.CreateJob(ctx, &repo.CronJob{
		Name: "original", Schedule: "@hourly", JobType: "tool",
		Payload: json.RawMessage("{}"), Enabled: true, MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated := &repo.CronJob{
		ID:         id,
		Name:       "renamed",
		Schedule:   "*/5 * * * *",
		JobType:    "pipeline",
		Payload:    json.RawMessage(`{"key":"value"}`),
		Enabled:    false,
		MaxRetries: 5,
		LastStatus: "completed",
	}
	if err := r.UpdateJob(ctx, updated); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := r.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.Name != "renamed" {
		t.Errorf("name = %q, want %q", got.Name, "renamed")
	}
	if got.Schedule != "*/5 * * * *" {
		t.Errorf("schedule = %q, want %q", got.Schedule, "*/5 * * * *")
	}
	if got.Enabled {
		t.Error("expected enabled = false")
	}
	if got.MaxRetries != 5 {
		t.Errorf("max_retries = %d, want 5", got.MaxRetries)
	}
}

func TestCronRepo_UpdateJobNotFound(t *testing.T) {
	r, ctx := newCronRepo(t)

	err := r.UpdateJob(ctx, &repo.CronJob{
		ID: "nonexistent", Name: "x", Schedule: "@hourly",
		JobType: "tool", Payload: json.RawMessage("{}"),
	})
	if err == nil {
		t.Error("expected error for nonexistent cron job update")
	}
}

func TestCronRepo_DeleteJob(t *testing.T) {
	r, ctx := newCronRepo(t)

	id, err := r.CreateJob(ctx, &repo.CronJob{
		Name: "doomed", Schedule: "@hourly", JobType: "tool",
		Payload: json.RawMessage("{}"), Enabled: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := r.DeleteJob(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err = r.GetJob(ctx, id)
	if err == nil {
		t.Error("expected error after deletion")
	}
}

func TestCronRepo_DeleteJobNotFound(t *testing.T) {
	r, ctx := newCronRepo(t)

	err := r.DeleteJob(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent cron job delete")
	}
}

func TestCronRepo_GetDueJobs(t *testing.T) {
	r, ctx := newCronRepo(t)

	past := time.Date(2026, 3, 7, 0, 0, 0, 0, time.UTC)
	future := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)

	// Due job.
	_, err := r.CreateJob(ctx, &repo.CronJob{
		Name: "due", Schedule: "@hourly", JobType: "tool",
		Payload: json.RawMessage("{}"), Enabled: true, NextRunAt: &past,
	})
	if err != nil {
		t.Fatalf("create due: %v", err)
	}

	// Future job — not due.
	_, err = r.CreateJob(ctx, &repo.CronJob{
		Name: "future", Schedule: "@hourly", JobType: "tool",
		Payload: json.RawMessage("{}"), Enabled: true, NextRunAt: &future,
	})
	if err != nil {
		t.Fatalf("create future: %v", err)
	}

	// Disabled job with past next_run_at — should not be returned.
	_, err = r.CreateJob(ctx, &repo.CronJob{
		Name: "disabled", Schedule: "@hourly", JobType: "tool",
		Payload: json.RawMessage("{}"), Enabled: false, NextRunAt: &past,
	})
	if err != nil {
		t.Fatalf("create disabled: %v", err)
	}

	now := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	due, err := r.GetDueJobs(ctx, now)
	if err != nil {
		t.Fatalf("get due: %v", err)
	}

	if len(due) != 1 {
		t.Fatalf("expected 1 due job, got %d", len(due))
	}
	if due[0].Name != "due" {
		t.Errorf("due job name = %q, want %q", due[0].Name, "due")
	}
}

func TestCronRepo_UpdateNextRun(t *testing.T) {
	r, ctx := newCronRepo(t)

	id, err := r.CreateJob(ctx, &repo.CronJob{
		Name: "job", Schedule: "@hourly", JobType: "tool",
		Payload: json.RawMessage("{}"), Enabled: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	nextRun := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	if err := r.UpdateNextRun(ctx, id, nextRun); err != nil {
		t.Fatalf("update next run: %v", err)
	}

	got, err := r.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.NextRunAt == nil {
		t.Fatal("expected non-nil next_run_at")
	}
	if got.NextRunAt.Format("2006-01-02 15:04:05") != "2026-03-09 12:00:00" {
		t.Errorf("next_run_at = %v, want 2026-03-09 12:00:00", got.NextRunAt)
	}
}

func TestCronRepo_UpdateLastRun(t *testing.T) {
	r, ctx := newCronRepo(t)

	id, err := r.CreateJob(ctx, &repo.CronJob{
		Name: "job", Schedule: "@hourly", JobType: "tool",
		Payload: json.RawMessage("{}"), Enabled: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	lastRun := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)
	if err := r.UpdateLastRun(ctx, id, lastRun, "completed"); err != nil {
		t.Fatalf("update last run: %v", err)
	}

	got, err := r.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.LastRunAt == nil {
		t.Fatal("expected non-nil last_run_at")
	}
	if got.LastStatus != "completed" {
		t.Errorf("last_status = %q, want %q", got.LastStatus, "completed")
	}
}

func TestCronRepo_CreateAndGetHistory(t *testing.T) {
	r, ctx := newCronRepo(t)

	jobID, err := r.CreateJob(ctx, &repo.CronJob{
		Name: "job", Schedule: "@hourly", JobType: "tool",
		Payload: json.RawMessage("{}"), Enabled: true,
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	exec := &repo.CronExecution{
		CronJobID: jobID,
		Status:    "running",
		StartedAt: time.Now(),
		Attempt:   1,
	}
	execID, err := r.CreateExecution(ctx, exec)
	if err != nil {
		t.Fatalf("create execution: %v", err)
	}
	if execID == "" {
		t.Fatal("expected non-empty execution ID")
	}

	history, err := r.GetHistory(ctx, jobID, 10)
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(history))
	}
	if history[0].Status != "running" {
		t.Errorf("status = %q, want %q", history[0].Status, "running")
	}
}

func TestCronRepo_CompleteExecution(t *testing.T) {
	r, ctx := newCronRepo(t)

	jobID, _ := r.CreateJob(ctx, &repo.CronJob{
		Name: "job", Schedule: "@hourly", JobType: "tool",
		Payload: json.RawMessage("{}"), Enabled: true,
	})

	exec := &repo.CronExecution{
		CronJobID: jobID,
		Status:    "running",
		StartedAt: time.Now(),
		Attempt:   1,
	}
	execID, _ := r.CreateExecution(ctx, exec)

	if err := r.CompleteExecution(ctx, execID, "completed", ""); err != nil {
		t.Fatalf("complete execution: %v", err)
	}

	history, _ := r.GetHistory(ctx, jobID, 10)
	if len(history) != 1 {
		t.Fatalf("expected 1, got %d", len(history))
	}
	if history[0].Status != "completed" {
		t.Errorf("status = %q, want %q", history[0].Status, "completed")
	}
	if history[0].FinishedAt == nil {
		t.Error("expected non-nil finished_at")
	}
}

func TestCronRepo_CompleteExecutionFailed(t *testing.T) {
	r, ctx := newCronRepo(t)

	jobID, _ := r.CreateJob(ctx, &repo.CronJob{
		Name: "job", Schedule: "@hourly", JobType: "tool",
		Payload: json.RawMessage("{}"), Enabled: true,
	})

	exec := &repo.CronExecution{
		CronJobID: jobID,
		Status:    "running",
		StartedAt: time.Now(),
		Attempt:   1,
	}
	execID, _ := r.CreateExecution(ctx, exec)

	if err := r.CompleteExecution(ctx, execID, "failed", "timeout exceeded"); err != nil {
		t.Fatalf("complete execution: %v", err)
	}

	history, _ := r.GetHistory(ctx, jobID, 10)
	if history[0].Status != "failed" {
		t.Errorf("status = %q, want %q", history[0].Status, "failed")
	}
	if history[0].Error != "timeout exceeded" {
		t.Errorf("error = %q, want %q", history[0].Error, "timeout exceeded")
	}
}

func TestCronRepo_DLQ(t *testing.T) {
	r, ctx := newCronRepo(t)

	jobID, _ := r.CreateJob(ctx, &repo.CronJob{
		Name: "failing-job", Schedule: "@hourly", JobType: "tool",
		Payload: json.RawMessage("{}"), Enabled: true,
	})

	entry := &repo.CronDLQEntry{
		CronJobID: jobID,
		FailedAt:  time.Now(),
		Attempts:  3,
		LastError: "max retries exceeded",
		Payload:   json.RawMessage(`{"pipeline_id":"xyz"}`),
	}

	if err := r.AddToDLQ(ctx, entry); err != nil {
		t.Fatalf("add to DLQ: %v", err)
	}

	dlq, err := r.ListDLQ(ctx)
	if err != nil {
		t.Fatalf("list DLQ: %v", err)
	}
	if len(dlq) != 1 {
		t.Fatalf("expected 1 DLQ entry, got %d", len(dlq))
	}
	if dlq[0].CronJobID != jobID {
		t.Errorf("cron_job_id = %q, want %q", dlq[0].CronJobID, jobID)
	}
	if dlq[0].Attempts != 3 {
		t.Errorf("attempts = %d, want 3", dlq[0].Attempts)
	}
}

func TestCronRepo_RetryFromDLQ(t *testing.T) {
	r, ctx := newCronRepo(t)

	// Create a disabled job to simulate retry scenario.
	jobID, _ := r.CreateJob(ctx, &repo.CronJob{
		Name: "retryable", Schedule: "@hourly", JobType: "tool",
		Payload: json.RawMessage("{}"), Enabled: false,
	})

	entry := &repo.CronDLQEntry{
		CronJobID: jobID,
		FailedAt:  time.Now(),
		Attempts:  3,
		LastError: "connection timeout",
		Payload:   json.RawMessage("{}"),
	}
	if err := r.AddToDLQ(ctx, entry); err != nil {
		t.Fatalf("add to DLQ: %v", err)
	}

	dlq, _ := r.ListDLQ(ctx)
	dlqID := dlq[0].ID

	if err := r.RetryFromDLQ(ctx, dlqID); err != nil {
		t.Fatalf("retry from DLQ: %v", err)
	}

	// DLQ should be empty.
	dlq, _ = r.ListDLQ(ctx)
	if len(dlq) != 0 {
		t.Errorf("expected 0 DLQ entries, got %d", len(dlq))
	}

	// Job should be re-enabled.
	job, _ := r.GetJob(ctx, jobID)
	if !job.Enabled {
		t.Error("expected job to be re-enabled after retry")
	}
}

func TestCronRepo_DeleteFromDLQ(t *testing.T) {
	r, ctx := newCronRepo(t)

	entry := &repo.CronDLQEntry{
		CronJobID: "some-job",
		FailedAt:  time.Now(),
		Attempts:  1,
		LastError: "error",
		Payload:   json.RawMessage("{}"),
	}
	if err := r.AddToDLQ(ctx, entry); err != nil {
		t.Fatalf("add to DLQ: %v", err)
	}

	dlq, _ := r.ListDLQ(ctx)
	dlqID := dlq[0].ID

	if err := r.DeleteFromDLQ(ctx, dlqID); err != nil {
		t.Fatalf("delete from DLQ: %v", err)
	}

	dlq, _ = r.ListDLQ(ctx)
	if len(dlq) != 0 {
		t.Errorf("expected 0 DLQ entries, got %d", len(dlq))
	}
}

func TestCronRepo_DeleteFromDLQNotFound(t *testing.T) {
	r, ctx := newCronRepo(t)

	err := r.DeleteFromDLQ(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent DLQ entry delete")
	}
}

func TestCronRepo_GetHistoryEmpty(t *testing.T) {
	r, ctx := newCronRepo(t)

	jobID, _ := r.CreateJob(ctx, &repo.CronJob{
		Name: "no-history", Schedule: "@hourly", JobType: "tool",
		Payload: json.RawMessage("{}"), Enabled: true,
	})

	history, err := r.GetHistory(ctx, jobID, 10)
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("expected 0 history, got %d", len(history))
	}
}

func TestCronRepo_GetHistoryLimit(t *testing.T) {
	r, ctx := newCronRepo(t)

	jobID, _ := r.CreateJob(ctx, &repo.CronJob{
		Name: "many-execs", Schedule: "@hourly", JobType: "tool",
		Payload: json.RawMessage("{}"), Enabled: true,
	})

	// Create 5 executions.
	for i := 0; i < 5; i++ {
		_, _ = r.CreateExecution(ctx, &repo.CronExecution{
			CronJobID: jobID,
			Status:    "completed",
			StartedAt: time.Now(),
			Attempt:   i + 1,
		})
	}

	history, err := r.GetHistory(ctx, jobID, 3)
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if len(history) != 3 {
		t.Errorf("expected 3 history entries (limit), got %d", len(history))
	}
}

func TestCronRepo_CascadeDeleteExecutions(t *testing.T) {
	r, ctx := newCronRepo(t)

	jobID, _ := r.CreateJob(ctx, &repo.CronJob{
		Name: "cascading", Schedule: "@hourly", JobType: "tool",
		Payload: json.RawMessage("{}"), Enabled: true,
	})

	_, _ = r.CreateExecution(ctx, &repo.CronExecution{
		CronJobID: jobID,
		Status:    "running",
		StartedAt: time.Now(),
		Attempt:   1,
	})

	// Deleting the job should cascade-delete executions.
	if err := r.DeleteJob(ctx, jobID); err != nil {
		t.Fatalf("delete job: %v", err)
	}

	history, _ := r.GetHistory(ctx, jobID, 10)
	if len(history) != 0 {
		t.Errorf("expected 0 executions after cascade delete, got %d", len(history))
	}
}
