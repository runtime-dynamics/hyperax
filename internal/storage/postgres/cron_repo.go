package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/repo"
)

// CronRepo implements repo.CronRepo for PostgreSQL.
type CronRepo struct {
	db *sql.DB
}

// CreateJob inserts a new cron job and returns its generated ID.
func (r *CronRepo) CreateJob(ctx context.Context, job *repo.CronJob) (string, error) {
	if job.ID == "" {
		job.ID = uuid.New().String()
	}

	payload := string(job.Payload)
	if payload == "" {
		payload = "{}"
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO cron_jobs (id, name, schedule, job_type, payload, enabled, max_retries, next_run_at, last_run_at, last_status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		job.ID, job.Name, job.Schedule, job.JobType, payload,
		job.Enabled, job.MaxRetries, job.NextRunAt, job.LastRunAt, job.LastStatus,
	)
	if err != nil {
		return "", fmt.Errorf("postgres.CronRepo.CreateJob: %w", err)
	}
	return job.ID, nil
}

// GetJob retrieves a cron job by its ID.
func (r *CronRepo) GetJob(ctx context.Context, id string) (*repo.CronJob, error) {
	job := &repo.CronJob{}
	var payload string
	var nextRunAt, lastRunAt sql.NullTime
	var lastStatus sql.NullString

	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, schedule, job_type, payload, enabled, max_retries,
		        next_run_at, last_run_at, COALESCE(last_status, ''),
		        created_at, updated_at
		 FROM cron_jobs WHERE id = $1`, id,
	).Scan(
		&job.ID, &job.Name, &job.Schedule, &job.JobType, &payload,
		&job.Enabled, &job.MaxRetries,
		&nextRunAt, &lastRunAt, &lastStatus,
		&job.CreatedAt, &job.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("cron job %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres.CronRepo.GetJob: %w", err)
	}

	job.Payload = json.RawMessage(payload)
	if lastStatus.Valid {
		job.LastStatus = lastStatus.String
	}
	if nextRunAt.Valid {
		job.NextRunAt = &nextRunAt.Time
	}
	if lastRunAt.Valid {
		job.LastRunAt = &lastRunAt.Time
	}
	return job, nil
}

// ListJobs returns all cron jobs ordered by name.
func (r *CronRepo) ListJobs(ctx context.Context) ([]*repo.CronJob, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, schedule, job_type, payload, enabled, max_retries,
		        next_run_at, last_run_at, COALESCE(last_status, ''),
		        created_at, updated_at
		 FROM cron_jobs ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.CronRepo.ListJobs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanPgCronJobs(rows)
}

// UpdateJob modifies an existing cron job.
func (r *CronRepo) UpdateJob(ctx context.Context, job *repo.CronJob) error {
	payload := string(job.Payload)
	if payload == "" {
		payload = "{}"
	}

	res, err := r.db.ExecContext(ctx,
		`UPDATE cron_jobs SET
		    name = $1, schedule = $2, job_type = $3, payload = $4,
		    enabled = $5, max_retries = $6,
		    next_run_at = $7, last_run_at = $8, last_status = $9,
		    updated_at = NOW()
		 WHERE id = $10`,
		job.Name, job.Schedule, job.JobType, payload,
		job.Enabled, job.MaxRetries,
		job.NextRunAt, job.LastRunAt, job.LastStatus,
		job.ID,
	)
	if err != nil {
		return fmt.Errorf("postgres.CronRepo.UpdateJob: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.CronRepo.UpdateJob: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("cron job %q not found", job.ID)
	}
	return nil
}

// DeleteJob removes a cron job by its ID. Associated executions are cascade-deleted.
func (r *CronRepo) DeleteJob(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM cron_jobs WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("postgres.CronRepo.DeleteJob: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.CronRepo.DeleteJob: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("cron job %q not found", id)
	}
	return nil
}

// GetDueJobs returns all enabled cron jobs whose next_run_at is at or before now.
func (r *CronRepo) GetDueJobs(ctx context.Context, now time.Time) ([]*repo.CronJob, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, schedule, job_type, payload, enabled, max_retries,
		        next_run_at, last_run_at, COALESCE(last_status, ''),
		        created_at, updated_at
		 FROM cron_jobs
		 WHERE enabled = TRUE AND next_run_at IS NOT NULL AND next_run_at <= $1
		 ORDER BY next_run_at`, now,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.CronRepo.GetDueJobs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanPgCronJobs(rows)
}

// UpdateNextRun sets the next_run_at timestamp for a cron job.
func (r *CronRepo) UpdateNextRun(ctx context.Context, id string, nextRun time.Time) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE cron_jobs SET next_run_at = $1, updated_at = NOW() WHERE id = $2`,
		nextRun, id,
	)
	if err != nil {
		return fmt.Errorf("postgres.CronRepo.UpdateNextRun: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.CronRepo.UpdateNextRun: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("cron job %q not found", id)
	}
	return nil
}

// UpdateLastRun sets the last_run_at and last_status for a cron job.
func (r *CronRepo) UpdateLastRun(ctx context.Context, id string, lastRun time.Time, status string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE cron_jobs SET last_run_at = $1, last_status = $2, updated_at = NOW() WHERE id = $3`,
		lastRun, status, id,
	)
	if err != nil {
		return fmt.Errorf("postgres.CronRepo.UpdateLastRun: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.CronRepo.UpdateLastRun: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("cron job %q not found", id)
	}
	return nil
}

// CreateExecution inserts a new execution record and returns its ID.
func (r *CronRepo) CreateExecution(ctx context.Context, exec *repo.CronExecution) (string, error) {
	if exec.ID == "" {
		exec.ID = uuid.New().String()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO cron_executions (id, cron_job_id, status, started_at, attempt)
		 VALUES ($1, $2, $3, $4, $5)`,
		exec.ID, exec.CronJobID, exec.Status, exec.StartedAt, exec.Attempt,
	)
	if err != nil {
		return "", fmt.Errorf("postgres.CronRepo.CreateExecution: %w", err)
	}
	return exec.ID, nil
}

// GetHistory returns the most recent executions for a cron job, ordered by
// started_at descending, limited to the given count.
func (r *CronRepo) GetHistory(ctx context.Context, jobID string, limit int) ([]*repo.CronExecution, error) {
	if limit <= 0 {
		limit = 10
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, cron_job_id, status, started_at,
		        finished_at, COALESCE(duration_ms, 0), COALESCE(error, ''), attempt
		 FROM cron_executions
		 WHERE cron_job_id = $1
		 ORDER BY started_at DESC
		 LIMIT $2`, jobID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.CronRepo.GetHistory: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var execs []*repo.CronExecution
	for rows.Next() {
		e := &repo.CronExecution{}
		var finishedAt sql.NullTime

		if err := rows.Scan(
			&e.ID, &e.CronJobID, &e.Status, &e.StartedAt,
			&finishedAt, &e.DurationMS, &e.Error, &e.Attempt,
		); err != nil {
			return nil, fmt.Errorf("postgres.CronRepo.GetHistory: %w", err)
		}

		if finishedAt.Valid {
			e.FinishedAt = &finishedAt.Time
		}
		execs = append(execs, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.CronRepo.GetHistory: %w", err)
	}
	return execs, nil
}

// CompleteExecution marks an execution as completed or failed, setting
// finished_at and duration_ms.
func (r *CronRepo) CompleteExecution(ctx context.Context, id string, status string, errMsg string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE cron_executions SET
		    status = $1, error = $2,
		    finished_at = NOW(),
		    duration_ms = EXTRACT(EPOCH FROM (NOW() - started_at))::BIGINT * 1000
		 WHERE id = $3`,
		status, errMsg, id,
	)
	if err != nil {
		return fmt.Errorf("postgres.CronRepo.CompleteExecution: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.CronRepo.CompleteExecution: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("cron execution %q not found", id)
	}
	return nil
}

// AddToDLQ adds a failed job entry to the dead-letter queue.
func (r *CronRepo) AddToDLQ(ctx context.Context, entry *repo.CronDLQEntry) error {
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}

	payload := string(entry.Payload)
	if payload == "" {
		payload = "{}"
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO cron_dlq (id, cron_job_id, failed_at, attempts, last_error, payload)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		entry.ID, entry.CronJobID, entry.FailedAt, entry.Attempts, entry.LastError, payload,
	)
	if err != nil {
		return fmt.Errorf("postgres.CronRepo.AddToDLQ: %w", err)
	}
	return nil
}

// ListDLQ returns all entries in the dead-letter queue, ordered by failed_at descending.
func (r *CronRepo) ListDLQ(ctx context.Context) ([]*repo.CronDLQEntry, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, cron_job_id, failed_at, attempts, COALESCE(last_error, ''), payload
		 FROM cron_dlq ORDER BY failed_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.CronRepo.ListDLQ: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []*repo.CronDLQEntry
	for rows.Next() {
		e := &repo.CronDLQEntry{}
		var payload string

		if err := rows.Scan(
			&e.ID, &e.CronJobID, &e.FailedAt, &e.Attempts, &e.LastError, &payload,
		); err != nil {
			return nil, fmt.Errorf("postgres.CronRepo.ListDLQ: %w", err)
		}
		e.Payload = json.RawMessage(payload)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.CronRepo.ListDLQ: %w", err)
	}
	return entries, nil
}

// RetryFromDLQ re-enables a cron job referenced by a DLQ entry.
func (r *CronRepo) RetryFromDLQ(ctx context.Context, id string) error {
	var cronJobID string
	err := r.db.QueryRowContext(ctx,
		`SELECT cron_job_id FROM cron_dlq WHERE id = $1`, id,
	).Scan(&cronJobID)
	if err == sql.ErrNoRows {
		return fmt.Errorf("DLQ entry %q not found", id)
	}
	if err != nil {
		return fmt.Errorf("postgres.CronRepo.RetryFromDLQ: %w", err)
	}

	// Re-enable the cron job if it still exists.
	if _, err := r.db.ExecContext(ctx,
		`UPDATE cron_jobs SET enabled = TRUE, updated_at = NOW() WHERE id = $1`,
		cronJobID,
	); err != nil {
		slog.Error("failed to re-enable cron job after DLQ recovery", "job_id", cronJobID, "error", err)
		return fmt.Errorf("postgres.CronRepo.RetryFromDLQ: re-enable cron job %s: %w", cronJobID, err)
	}

	// Remove the DLQ entry.
	_, err = r.db.ExecContext(ctx, `DELETE FROM cron_dlq WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("postgres.CronRepo.RetryFromDLQ: %w", err)
	}
	return nil
}

// DeleteFromDLQ removes a DLQ entry without retrying.
func (r *CronRepo) DeleteFromDLQ(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM cron_dlq WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("postgres.CronRepo.DeleteFromDLQ: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.CronRepo.DeleteFromDLQ: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("DLQ entry %q not found", id)
	}
	return nil
}

// scanPgCronJobs scans rows into CronJob slices.
func scanPgCronJobs(rows *sql.Rows) ([]*repo.CronJob, error) {
	var jobs []*repo.CronJob
	for rows.Next() {
		job := &repo.CronJob{}
		var payload string
		var nextRunAt, lastRunAt sql.NullTime
		var lastStatus sql.NullString

		if err := rows.Scan(
			&job.ID, &job.Name, &job.Schedule, &job.JobType, &payload,
			&job.Enabled, &job.MaxRetries,
			&nextRunAt, &lastRunAt, &lastStatus,
			&job.CreatedAt, &job.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("postgres.scanPgCronJobs: %w", err)
		}

		job.Payload = json.RawMessage(payload)
		if lastStatus.Valid {
			job.LastStatus = lastStatus.String
		}
		if nextRunAt.Valid {
			job.NextRunAt = &nextRunAt.Time
		}
		if lastRunAt.Valid {
			job.LastRunAt = &lastRunAt.Time
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.scanPgCronJobs: %w", err)
	}
	return jobs, nil
}
