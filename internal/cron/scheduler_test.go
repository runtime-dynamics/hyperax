package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// mockCronRepo implements repo.CronRepo for testing the scheduler.
type mockCronRepo struct {
	jobs       map[string]*repo.CronJob
	executions []*repo.CronExecution
	dlq        []*repo.CronDLQEntry
	nextID     int
}

func newMockCronRepo() *mockCronRepo {
	return &mockCronRepo{
		jobs: make(map[string]*repo.CronJob),
	}
}

func (m *mockCronRepo) CreateJob(_ context.Context, job *repo.CronJob) (string, error) {
	m.nextID++
	if job.ID == "" {
		job.ID = fmt.Sprintf("mock-%d", m.nextID)
	}
	clone := *job
	m.jobs[job.ID] = &clone
	return job.ID, nil
}

func (m *mockCronRepo) GetJob(_ context.Context, id string) (*repo.CronJob, error) {
	j, ok := m.jobs[id]
	if !ok {
		return nil, fmt.Errorf("cron job %q not found", id)
	}
	clone := *j
	return &clone, nil
}

func (m *mockCronRepo) ListJobs(_ context.Context) ([]*repo.CronJob, error) {
	var result []*repo.CronJob
	for _, j := range m.jobs {
		clone := *j
		result = append(result, &clone)
	}
	return result, nil
}

func (m *mockCronRepo) UpdateJob(_ context.Context, job *repo.CronJob) error {
	if _, ok := m.jobs[job.ID]; !ok {
		return fmt.Errorf("cron job %q not found", job.ID)
	}
	clone := *job
	m.jobs[job.ID] = &clone
	return nil
}

func (m *mockCronRepo) DeleteJob(_ context.Context, id string) error {
	if _, ok := m.jobs[id]; !ok {
		return fmt.Errorf("cron job %q not found", id)
	}
	delete(m.jobs, id)
	return nil
}

func (m *mockCronRepo) GetDueJobs(_ context.Context, now time.Time) ([]*repo.CronJob, error) {
	var result []*repo.CronJob
	for _, j := range m.jobs {
		if j.Enabled && j.NextRunAt != nil && !j.NextRunAt.After(now) {
			clone := *j
			result = append(result, &clone)
		}
	}
	return result, nil
}

func (m *mockCronRepo) UpdateNextRun(_ context.Context, id string, nextRun time.Time) error {
	j, ok := m.jobs[id]
	if !ok {
		return fmt.Errorf("cron job %q not found", id)
	}
	t := nextRun
	j.NextRunAt = &t
	return nil
}

func (m *mockCronRepo) UpdateLastRun(_ context.Context, id string, lastRun time.Time, status string) error {
	j, ok := m.jobs[id]
	if !ok {
		return fmt.Errorf("cron job %q not found", id)
	}
	t := lastRun
	j.LastRunAt = &t
	j.LastStatus = status
	return nil
}

func (m *mockCronRepo) CreateExecution(_ context.Context, exec *repo.CronExecution) (string, error) {
	m.nextID++
	if exec.ID == "" {
		exec.ID = fmt.Sprintf("exec-%d", m.nextID)
	}
	clone := *exec
	m.executions = append(m.executions, &clone)
	return exec.ID, nil
}

func (m *mockCronRepo) GetHistory(_ context.Context, jobID string, limit int) ([]*repo.CronExecution, error) {
	var result []*repo.CronExecution
	for _, e := range m.executions {
		if e.CronJobID == jobID {
			clone := *e
			result = append(result, &clone)
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *mockCronRepo) CompleteExecution(_ context.Context, id string, status string, errMsg string) error {
	for _, e := range m.executions {
		if e.ID == id {
			e.Status = status
			e.Error = errMsg
			now := time.Now()
			e.FinishedAt = &now
			return nil
		}
	}
	return fmt.Errorf("execution %q not found", id)
}

func (m *mockCronRepo) AddToDLQ(_ context.Context, entry *repo.CronDLQEntry) error {
	m.nextID++
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("dlq-%d", m.nextID)
	}
	clone := *entry
	m.dlq = append(m.dlq, &clone)
	return nil
}

func (m *mockCronRepo) ListDLQ(_ context.Context) ([]*repo.CronDLQEntry, error) {
	var result []*repo.CronDLQEntry
	for _, e := range m.dlq {
		clone := *e
		result = append(result, &clone)
	}
	return result, nil
}

func (m *mockCronRepo) RetryFromDLQ(_ context.Context, id string) error {
	for i, e := range m.dlq {
		if e.ID == id {
			m.dlq = append(m.dlq[:i], m.dlq[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("DLQ entry %q not found", id)
}

func (m *mockCronRepo) DeleteFromDLQ(_ context.Context, id string) error {
	for i, e := range m.dlq {
		if e.ID == id {
			m.dlq = append(m.dlq[:i], m.dlq[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("DLQ entry %q not found", id)
}

func TestScheduler_DispatchesDueJobs(t *testing.T) {
	mockRepo := newMockCronRepo()
	bus := nervous.NewEventBus(64)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	s := NewScheduler(mockRepo, bus, logger)

	// Subscribe to cron.fire events.
	sub := bus.SubscribeTypes("test-cron-sub", types.EventCronFire)

	// Create a due job.
	past := time.Now().Add(-1 * time.Hour)
	_, _ = mockRepo.CreateJob(context.Background(), &repo.CronJob{
		ID:       "job-1",
		Name:     "test-job",
		Schedule: "@hourly",
		JobType:  "tool",
		Payload:  json.RawMessage(`{"action":"test"}`),
		Enabled:  true,
		NextRunAt: &past,
	})

	// Run a single check-and-dispatch cycle.
	s.checkAndDispatch(context.Background())

	// Verify an event was published.
	select {
	case event := <-sub.Ch:
		if event.Type != types.EventCronFire {
			t.Errorf("event type = %q, want %q", event.Type, types.EventCronFire)
		}
		if event.Source != "cron.scheduler" {
			t.Errorf("source = %q, want %q", event.Source, "cron.scheduler")
		}
	default:
		t.Error("expected cron.fire event, got none")
	}

	// Verify execution was created.
	if len(mockRepo.executions) != 1 {
		t.Errorf("expected 1 execution, got %d", len(mockRepo.executions))
	}

	// Verify last_run_at was updated.
	job := mockRepo.jobs["job-1"]
	if job.LastRunAt == nil {
		t.Error("expected last_run_at to be set")
	}

	// Verify next_run_at was advanced.
	if job.NextRunAt == nil || !job.NextRunAt.After(past) {
		t.Error("expected next_run_at to be advanced beyond the original time")
	}

	bus.Unsubscribe("test-cron-sub")
}

func TestScheduler_SkipsDisabledJobs(t *testing.T) {
	mockRepo := newMockCronRepo()
	bus := nervous.NewEventBus(64)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	s := NewScheduler(mockRepo, bus, logger)

	// Create a disabled job with a past next_run_at.
	past := time.Now().Add(-1 * time.Hour)
	_, _ = mockRepo.CreateJob(context.Background(), &repo.CronJob{
		ID:        "disabled-job",
		Name:      "disabled",
		Schedule:  "@hourly",
		JobType:   "tool",
		Payload:   json.RawMessage("{}"),
		Enabled:   false,
		NextRunAt: &past,
	})

	s.checkAndDispatch(context.Background())

	// No executions should be created.
	if len(mockRepo.executions) != 0 {
		t.Errorf("expected 0 executions for disabled job, got %d", len(mockRepo.executions))
	}
}

func TestScheduler_DispatchJob_Manual(t *testing.T) {
	mockRepo := newMockCronRepo()
	bus := nervous.NewEventBus(64)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	s := NewScheduler(mockRepo, bus, logger)

	sub := bus.SubscribeTypes("test-manual-sub", types.EventCronFire)

	_, _ = mockRepo.CreateJob(context.Background(), &repo.CronJob{
		ID:       "manual-job",
		Name:     "manual",
		Schedule: "@daily",
		JobType:  "tool",
		Payload:  json.RawMessage("{}"),
		Enabled:  true,
	})

	if err := s.DispatchJob(context.Background(), "manual-job"); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	select {
	case event := <-sub.Ch:
		if event.Type != types.EventCronFire {
			t.Errorf("event type = %q, want %q", event.Type, types.EventCronFire)
		}
	default:
		t.Error("expected cron.fire event from manual trigger")
	}

	if len(mockRepo.executions) != 1 {
		t.Errorf("expected 1 execution, got %d", len(mockRepo.executions))
	}

	bus.Unsubscribe("test-manual-sub")
}

func TestScheduler_DispatchJob_NotFound(t *testing.T) {
	mockRepo := newMockCronRepo()
	bus := nervous.NewEventBus(64)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	s := NewScheduler(mockRepo, bus, logger)

	err := s.DispatchJob(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent job")
	}
}

func TestScheduler_InitializeNextRuns(t *testing.T) {
	mockRepo := newMockCronRepo()
	bus := nervous.NewEventBus(64)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	s := NewScheduler(mockRepo, bus, logger)

	// Job with no next_run_at.
	_, _ = mockRepo.CreateJob(context.Background(), &repo.CronJob{
		ID:       "no-next",
		Name:     "needs-init",
		Schedule: "@hourly",
		JobType:  "tool",
		Payload:  json.RawMessage("{}"),
		Enabled:  true,
	})

	// Job that already has a next_run_at — should not be modified.
	future := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	_, _ = mockRepo.CreateJob(context.Background(), &repo.CronJob{
		ID:        "has-next",
		Name:      "already-set",
		Schedule:  "@hourly",
		JobType:   "tool",
		Payload:   json.RawMessage("{}"),
		Enabled:   true,
		NextRunAt: &future,
	})

	s.InitializeNextRuns(context.Background())

	// The first job should now have next_run_at set.
	job := mockRepo.jobs["no-next"]
	if job.NextRunAt == nil {
		t.Error("expected next_run_at to be initialized for job without one")
	}

	// The second job's next_run_at should be unchanged.
	job2 := mockRepo.jobs["has-next"]
	if job2.NextRunAt == nil || !job2.NextRunAt.Equal(future) {
		t.Error("expected next_run_at to remain unchanged for job that already had one")
	}
}
