package repo

import (
	"context"
	"encoding/json"
	"time"
)

// CronJob defines a scheduled job stored in the cron_jobs table.
type CronJob struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Schedule   string          `json:"schedule"`
	JobType    string          `json:"job_type"` // "pipeline", "tool", "webhook"
	Payload    json.RawMessage `json:"payload"`
	Enabled    bool            `json:"enabled"`
	MaxRetries int             `json:"max_retries"`
	NextRunAt  *time.Time      `json:"next_run_at,omitempty"`
	LastRunAt  *time.Time      `json:"last_run_at,omitempty"`
	LastStatus string          `json:"last_status,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// CronExecution records a single execution of a cron job.
type CronExecution struct {
	ID         string     `json:"id"`
	CronJobID  string     `json:"cron_job_id"`
	Status     string     `json:"status"` // "running", "completed", "failed"
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	DurationMS int64      `json:"duration_ms,omitempty"`
	Error      string     `json:"error,omitempty"`
	Attempt    int        `json:"attempt"`
}

// CronDLQEntry represents a cron job that exhausted retries and landed
// in the dead-letter queue.
type CronDLQEntry struct {
	ID        string          `json:"id"`
	CronJobID string          `json:"cron_job_id"`
	FailedAt  time.Time       `json:"failed_at"`
	Attempts  int             `json:"attempts"`
	LastError string          `json:"last_error"`
	Payload   json.RawMessage `json:"payload"`
}

// CronRepo is the data-access interface for cron jobs, executions, and the DLQ.
type CronRepo interface {
	// Job CRUD
	CreateJob(ctx context.Context, job *CronJob) (string, error)
	GetJob(ctx context.Context, id string) (*CronJob, error)
	ListJobs(ctx context.Context) ([]*CronJob, error)
	UpdateJob(ctx context.Context, job *CronJob) error
	DeleteJob(ctx context.Context, id string) error

	// Scheduling queries
	GetDueJobs(ctx context.Context, now time.Time) ([]*CronJob, error)
	UpdateNextRun(ctx context.Context, id string, nextRun time.Time) error
	UpdateLastRun(ctx context.Context, id string, lastRun time.Time, status string) error

	// Execution tracking
	CreateExecution(ctx context.Context, exec *CronExecution) (string, error)
	GetHistory(ctx context.Context, jobID string, limit int) ([]*CronExecution, error)
	CompleteExecution(ctx context.Context, id string, status string, errMsg string) error

	// Dead-letter queue
	AddToDLQ(ctx context.Context, entry *CronDLQEntry) error
	ListDLQ(ctx context.Context) ([]*CronDLQEntry, error)
	RetryFromDLQ(ctx context.Context, id string) error
	DeleteFromDLQ(ctx context.Context, id string) error
}
