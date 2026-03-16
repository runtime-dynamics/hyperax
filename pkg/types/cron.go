package types

import (
	"encoding/json"
	"time"
)

// CronJobType enumerates the supported cron job payload types.
type CronJobType string

const (
	CronJobTypePipeline CronJobType = "pipeline"
	CronJobTypeTool     CronJobType = "tool"
	CronJobTypeWebhook  CronJobType = "webhook"
)

// CronJob defines a scheduled job.
type CronJob struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Schedule    string          `json:"schedule"`
	JobType     CronJobType     `json:"job_type"`
	Payload     json.RawMessage `json:"payload"`
	Enabled     bool            `json:"enabled"`
	MaxRetries  int             `json:"max_retries"`
	NextRunAt   *time.Time      `json:"next_run_at,omitempty"`
	LastRunAt   *time.Time      `json:"last_run_at,omitempty"`
	LastStatus  string          `json:"last_status,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// CronExecution records a single execution of a cron job.
type CronExecution struct {
	ID         string    `json:"id"`
	CronJobID  string    `json:"cron_job_id"`
	Status     string    `json:"status"` // running, completed, failed
	StartedAt  time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`
	Error      string    `json:"error,omitempty"`
	Attempt    int       `json:"attempt"`
}

// FailedJob represents a cron job that exhausted retries and landed in the DLQ.
type FailedJob struct {
	ID        string    `json:"id"`
	CronJobID string    `json:"cron_job_id"`
	FailedAt  time.Time `json:"failed_at"`
	Attempts  int       `json:"attempts"`
	LastError string    `json:"last_error"`
	Payload   any       `json:"payload"`
}
