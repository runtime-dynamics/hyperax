package cron

import (
	"context"
	"log/slog"
	"time"

	"fmt"
	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// Scheduler runs the cron tick loop, checking for due jobs and dispatching
// them via the EventBus. It is the core runtime component of the cron
// subsystem: it reads due jobs from the repo, creates execution records,
// publishes nervous-system events, and advances the next_run_at pointer.
type Scheduler struct {
	repo   repo.CronRepo
	bus    *nervous.EventBus
	logger *slog.Logger
	tick   time.Duration
}

// NewScheduler creates a Scheduler with a default tick interval of 30 seconds.
func NewScheduler(repo repo.CronRepo, bus *nervous.EventBus, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		repo:   repo,
		bus:    bus,
		logger: logger,
		tick:   30 * time.Second,
	}
}

// SetTick overrides the default tick interval. Intended for testing.
func (s *Scheduler) SetTick(d time.Duration) {
	s.tick = d
}

// Run starts the scheduler loop. It blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()

	s.logger.Info("cron scheduler started", "tick", s.tick)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("cron scheduler stopped")
			return
		case <-ticker.C:
			s.checkAndDispatch(ctx)
		}
	}
}

// DispatchJob manually triggers a single cron job, bypassing the schedule.
// This is used by the trigger_cron_job MCP tool.
func (s *Scheduler) DispatchJob(ctx context.Context, jobID string) error {
	job, err := s.repo.GetJob(ctx, jobID)
	if err != nil {
		return fmt.Errorf("cron.Scheduler.DispatchJob: %w", err)
	}
	s.dispatchJob(ctx, job)
	return nil
}

// checkAndDispatch queries for due jobs and dispatches each one.
func (s *Scheduler) checkAndDispatch(ctx context.Context) {
	jobs, err := s.repo.GetDueJobs(ctx, time.Now())
	if err != nil {
		s.logger.Error("get due cron jobs", "error", err)
		return
	}

	for _, job := range jobs {
		s.dispatchJob(ctx, job)
	}
}

// dispatchJob creates an execution record, publishes a cron.fire event on the
// EventBus, and updates the job's next_run_at and last_run_at timestamps.
func (s *Scheduler) dispatchJob(ctx context.Context, job *repo.CronJob) {
	now := time.Now()

	// Create an execution record.
	exec := &repo.CronExecution{
		CronJobID: job.ID,
		Status:    "running",
		StartedAt: now,
		Attempt:   1,
	}

	execID, err := s.repo.CreateExecution(ctx, exec)
	if err != nil {
		s.logger.Error("create cron execution", "job_id", job.ID, "error", err)
		return
	}

	// Publish a cron.fire event on the nervous system.
	payload := map[string]any{
		"job_id":       job.ID,
		"job_name":     job.Name,
		"job_type":     job.JobType,
		"execution_id": execID,
		"payload":      job.Payload,
	}

	event := nervous.NewEvent(types.EventCronFire, "cron.scheduler", "cron", payload)
	s.bus.Publish(event)

	s.logger.Info("cron job dispatched",
		"job_id", job.ID,
		"job_name", job.Name,
		"execution_id", execID,
	)

	// Update last_run_at.
	if err := s.repo.UpdateLastRun(ctx, job.ID, now, "running"); err != nil {
		s.logger.Error("update cron last run", "job_id", job.ID, "error", err)
	}

	// Compute and set next_run_at from the schedule.
	sched, err := Parse(job.Schedule)
	if err != nil {
		s.logger.Error("parse cron schedule", "job_id", job.ID, "schedule", job.Schedule, "error", err)
		return
	}

	nextRun := sched.NextAfter(now)
	if err := s.repo.UpdateNextRun(ctx, job.ID, nextRun); err != nil {
		s.logger.Error("update cron next run", "job_id", job.ID, "error", err)
	}
}

// InitializeNextRuns computes and sets next_run_at for all enabled jobs
// that do not yet have a next_run_at value. This should be called once at
// startup to seed the schedule.
func (s *Scheduler) InitializeNextRuns(ctx context.Context) {
	jobs, err := s.repo.ListJobs(ctx)
	if err != nil {
		s.logger.Error("list cron jobs for initialization", "error", err)
		return
	}

	now := time.Now()
	for _, job := range jobs {
		if !job.Enabled || job.NextRunAt != nil {
			continue
		}

		sched, err := Parse(job.Schedule)
		if err != nil {
			s.logger.Error("invalid cron schedule, skipping",
				"job_id", job.ID, "schedule", job.Schedule, "error", err)
			continue
		}

		nextRun := sched.NextAfter(now)
		if err := s.repo.UpdateNextRun(ctx, job.ID, nextRun); err != nil {
			s.logger.Error("set initial next run", "job_id", job.ID, "error", err)
		}
	}
}

// GenerateID creates a new UUID string for use as an entity ID.
// Exported so that callers (e.g., the MCP handler) can create IDs
// using the same generation strategy.
func GenerateID() string {
	return uuid.New().String()
}
