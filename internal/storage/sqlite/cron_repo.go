package sqlite

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

// CronRepo implements repo.CronRepo for SQLite.
type CronRepo struct {
	db *sql.DB
}

// CreateJob inserts a new cron job and returns its generated ID.
func (r *CronRepo) CreateJob(ctx context.Context, job *repo.CronJob) (string, error) {
	if job.ID == "" {
		job.ID = uuid.New().String()
	}

	enabled := 0
	if job.Enabled {
		enabled = 1
	}

	payload := string(job.Payload)
	if payload == "" {
		payload = "{}"
	}

	var nextRunAt, lastRunAt sql.NullString
	if job.NextRunAt != nil {
		nextRunAt = sql.NullString{String: job.NextRunAt.Format("2006-01-02 15:04:05"), Valid: true}
	}
	if job.LastRunAt != nil {
		lastRunAt = sql.NullString{String: job.LastRunAt.Format("2006-01-02 15:04:05"), Valid: true}
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO cron_jobs (id, name, schedule, job_type, payload, enabled, max_retries, next_run_at, last_run_at, last_status)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.Name, job.Schedule, job.JobType, payload,
		enabled, job.MaxRetries, nextRunAt, lastRunAt, job.LastStatus,
	)
	if err != nil {
		return "", fmt.Errorf("sqlite.CronRepo.CreateJob: %w", err)
	}

	return job.ID, nil
}

// GetJob retrieves a cron job by its ID.
func (r *CronRepo) GetJob(ctx context.Context, id string) (*repo.CronJob, error) {
	job := &repo.CronJob{}
	var enabled int
	var payload string
	var nextRunAt, lastRunAt, lastStatus sql.NullString
	var createdAt, updatedAt string

	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, schedule, job_type, payload, enabled, max_retries,
		        next_run_at, last_run_at, COALESCE(last_status, ''),
		        created_at, updated_at
		 FROM cron_jobs WHERE id = ?`, id,
	).Scan(
		&job.ID, &job.Name, &job.Schedule, &job.JobType, &payload,
		&enabled, &job.MaxRetries,
		&nextRunAt, &lastRunAt, &lastStatus,
		&createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("cron job %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite.CronRepo.GetJob: %w", err)
	}

	job.Enabled = enabled == 1
	job.Payload = json.RawMessage(payload)
	if lastStatus.Valid {
		job.LastStatus = lastStatus.String
	}
	if nextRunAt.Valid {
		t, parseErr := parseSQLiteTime(nextRunAt.String, "sqlite.CronRepo.GetJob.nextRunAt")
		if parseErr != nil {
			return nil, parseErr
		}
		job.NextRunAt = &t
	}
	if lastRunAt.Valid {
		t, parseErr := parseSQLiteTime(lastRunAt.String, "sqlite.CronRepo.GetJob.lastRunAt")
		if parseErr != nil {
			return nil, parseErr
		}
		job.LastRunAt = &t
	}
	if job.CreatedAt, err = parseSQLiteTime(createdAt, "sqlite.CronRepo.GetJob"); err != nil {
		return nil, err
	}
	if job.UpdatedAt, err = parseSQLiteTime(updatedAt, "sqlite.CronRepo.GetJob"); err != nil {
		return nil, err
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
		return nil, fmt.Errorf("sqlite.CronRepo.ListJobs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var jobs []*repo.CronJob
	for rows.Next() {
		job := &repo.CronJob{}
		var enabled int
		var payload string
		var nextRunAt, lastRunAt, lastStatus sql.NullString
		var createdAt, updatedAt string

		if err := rows.Scan(
			&job.ID, &job.Name, &job.Schedule, &job.JobType, &payload,
			&enabled, &job.MaxRetries,
			&nextRunAt, &lastRunAt, &lastStatus,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("sqlite.CronRepo.ListJobs: %w", err)
		}

		job.Enabled = enabled == 1
		job.Payload = json.RawMessage(payload)
		if lastStatus.Valid {
			job.LastStatus = lastStatus.String
		}
		var parseErr error
		if nextRunAt.Valid {
			t, pErr := parseSQLiteTime(nextRunAt.String, "sqlite.CronRepo.ListJobs.nextRunAt")
			if pErr != nil {
				return nil, pErr
			}
			job.NextRunAt = &t
		}
		if lastRunAt.Valid {
			t, pErr := parseSQLiteTime(lastRunAt.String, "sqlite.CronRepo.ListJobs.lastRunAt")
			if pErr != nil {
				return nil, pErr
			}
			job.LastRunAt = &t
		}
		if job.CreatedAt, parseErr = parseSQLiteTime(createdAt, "sqlite.CronRepo.ListJobs"); parseErr != nil {
			return nil, parseErr
		}
		if job.UpdatedAt, parseErr = parseSQLiteTime(updatedAt, "sqlite.CronRepo.ListJobs"); parseErr != nil {
			return nil, parseErr
		}

		jobs = append(jobs, job)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.CronRepo.ListJobs: %w", err)
	}
	return jobs, nil
}

// UpdateJob modifies an existing cron job.
func (r *CronRepo) UpdateJob(ctx context.Context, job *repo.CronJob) error {
	enabled := 0
	if job.Enabled {
		enabled = 1
	}

	payload := string(job.Payload)
	if payload == "" {
		payload = "{}"
	}

	var nextRunAt, lastRunAt sql.NullString
	if job.NextRunAt != nil {
		nextRunAt = sql.NullString{String: job.NextRunAt.Format("2006-01-02 15:04:05"), Valid: true}
	}
	if job.LastRunAt != nil {
		lastRunAt = sql.NullString{String: job.LastRunAt.Format("2006-01-02 15:04:05"), Valid: true}
	}

	res, err := r.db.ExecContext(ctx,
		`UPDATE cron_jobs SET
		    name = ?, schedule = ?, job_type = ?, payload = ?,
		    enabled = ?, max_retries = ?,
		    next_run_at = ?, last_run_at = ?, last_status = ?,
		    updated_at = datetime('now')
		 WHERE id = ?`,
		job.Name, job.Schedule, job.JobType, payload,
		enabled, job.MaxRetries,
		nextRunAt, lastRunAt, job.LastStatus,
		job.ID,
	)
	if err != nil {
		return fmt.Errorf("sqlite.CronRepo.UpdateJob: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.CronRepo.UpdateJob: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("cron job %q not found", job.ID)
	}

	return nil
}

// DeleteJob removes a cron job by its ID. Associated executions are cascade-deleted.
func (r *CronRepo) DeleteJob(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM cron_jobs WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("sqlite.CronRepo.DeleteJob: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.CronRepo.DeleteJob: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("cron job %q not found", id)
	}

	return nil
}

// GetDueJobs returns all enabled cron jobs whose next_run_at is at or before now.
func (r *CronRepo) GetDueJobs(ctx context.Context, now time.Time) ([]*repo.CronJob, error) {
	nowStr := now.Format("2006-01-02 15:04:05")

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, schedule, job_type, payload, enabled, max_retries,
		        next_run_at, last_run_at, COALESCE(last_status, ''),
		        created_at, updated_at
		 FROM cron_jobs
		 WHERE enabled = 1 AND next_run_at IS NOT NULL AND next_run_at <= ?
		 ORDER BY next_run_at`, nowStr,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.CronRepo.GetDueJobs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var jobs []*repo.CronJob
	for rows.Next() {
		job := &repo.CronJob{}
		var enabled int
		var payload string
		var nextRunAt, lastRunAt, lastStatus sql.NullString
		var createdAt, updatedAt string

		if err := rows.Scan(
			&job.ID, &job.Name, &job.Schedule, &job.JobType, &payload,
			&enabled, &job.MaxRetries,
			&nextRunAt, &lastRunAt, &lastStatus,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("sqlite.CronRepo.GetDueJobs: %w", err)
		}

		job.Enabled = enabled == 1
		job.Payload = json.RawMessage(payload)
		if lastStatus.Valid {
			job.LastStatus = lastStatus.String
		}
		var parseErr error
		if nextRunAt.Valid {
			t, pErr := parseSQLiteTime(nextRunAt.String, "sqlite.CronRepo.GetDueJobs.nextRunAt")
			if pErr != nil {
				return nil, pErr
			}
			job.NextRunAt = &t
		}
		if lastRunAt.Valid {
			t, pErr := parseSQLiteTime(lastRunAt.String, "sqlite.CronRepo.GetDueJobs.lastRunAt")
			if pErr != nil {
				return nil, pErr
			}
			job.LastRunAt = &t
		}
		if job.CreatedAt, parseErr = parseSQLiteTime(createdAt, "sqlite.CronRepo.GetDueJobs"); parseErr != nil {
			return nil, parseErr
		}
		if job.UpdatedAt, parseErr = parseSQLiteTime(updatedAt, "sqlite.CronRepo.GetDueJobs"); parseErr != nil {
			return nil, parseErr
		}

		jobs = append(jobs, job)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.CronRepo.GetDueJobs: %w", err)
	}
	return jobs, nil
}

// UpdateNextRun sets the next_run_at timestamp for a cron job.
func (r *CronRepo) UpdateNextRun(ctx context.Context, id string, nextRun time.Time) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE cron_jobs SET next_run_at = ?, updated_at = datetime('now') WHERE id = ?`,
		nextRun.Format("2006-01-02 15:04:05"), id,
	)
	if err != nil {
		return fmt.Errorf("sqlite.CronRepo.UpdateNextRun: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.CronRepo.UpdateNextRun: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("cron job %q not found", id)
	}

	return nil
}

// UpdateLastRun sets the last_run_at and last_status for a cron job.
func (r *CronRepo) UpdateLastRun(ctx context.Context, id string, lastRun time.Time, status string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE cron_jobs SET last_run_at = ?, last_status = ?, updated_at = datetime('now') WHERE id = ?`,
		lastRun.Format("2006-01-02 15:04:05"), status, id,
	)
	if err != nil {
		return fmt.Errorf("sqlite.CronRepo.UpdateLastRun: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.CronRepo.UpdateLastRun: %w", err)
	}
	if rows == 0 {
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
		 VALUES (?, ?, ?, ?, ?)`,
		exec.ID, exec.CronJobID, exec.Status,
		exec.StartedAt.Format("2006-01-02 15:04:05"), exec.Attempt,
	)
	if err != nil {
		return "", fmt.Errorf("sqlite.CronRepo.CreateExecution: %w", err)
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
		 WHERE cron_job_id = ?
		 ORDER BY started_at DESC
		 LIMIT ?`, jobID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.CronRepo.GetHistory: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var execs []*repo.CronExecution
	for rows.Next() {
		e := &repo.CronExecution{}
		var startedAt string
		var finishedAt sql.NullString

		if err := rows.Scan(
			&e.ID, &e.CronJobID, &e.Status, &startedAt,
			&finishedAt, &e.DurationMS, &e.Error, &e.Attempt,
		); err != nil {
			return nil, fmt.Errorf("sqlite.CronRepo.GetHistory: %w", err)
		}

		var parseErr error
		if e.StartedAt, parseErr = parseSQLiteTime(startedAt, "sqlite.CronRepo.GetHistory"); parseErr != nil {
			return nil, parseErr
		}
		if finishedAt.Valid {
			t, pErr := parseSQLiteTime(finishedAt.String, "sqlite.CronRepo.GetHistory.finishedAt")
			if pErr != nil {
				return nil, pErr
			}
			e.FinishedAt = &t
		}

		execs = append(execs, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.CronRepo.GetHistory: %w", err)
	}
	return execs, nil
}

// CompleteExecution marks an execution as completed or failed, setting
// finished_at and duration_ms.
func (r *CronRepo) CompleteExecution(ctx context.Context, id string, status string, errMsg string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE cron_executions SET
		    status = ?, error = ?,
		    finished_at = datetime('now'),
		    duration_ms = CAST((julianday(datetime('now')) - julianday(started_at)) * 86400000 AS INTEGER)
		 WHERE id = ?`,
		status, errMsg, id,
	)
	if err != nil {
		return fmt.Errorf("sqlite.CronRepo.CompleteExecution: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.CronRepo.CompleteExecution: %w", err)
	}
	if rows == 0 {
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
		 VALUES (?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.CronJobID,
		entry.FailedAt.Format("2006-01-02 15:04:05"),
		entry.Attempts, entry.LastError, payload,
	)
	if err != nil {
		return fmt.Errorf("sqlite.CronRepo.AddToDLQ: %w", err)
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
		return nil, fmt.Errorf("sqlite.CronRepo.ListDLQ: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []*repo.CronDLQEntry
	for rows.Next() {
		e := &repo.CronDLQEntry{}
		var failedAt, payload string

		if err := rows.Scan(
			&e.ID, &e.CronJobID, &failedAt, &e.Attempts, &e.LastError, &payload,
		); err != nil {
			return nil, fmt.Errorf("sqlite.CronRepo.ListDLQ: %w", err)
		}

		var parseErr error
		if e.FailedAt, parseErr = parseSQLiteTime(failedAt, "sqlite.CronRepo.ListDLQ"); parseErr != nil {
			return nil, parseErr
		}
		e.Payload = json.RawMessage(payload)

		entries = append(entries, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.CronRepo.ListDLQ: %w", err)
	}
	return entries, nil
}

// RetryFromDLQ re-enables a cron job referenced by a DLQ entry. It looks up
// the DLQ entry, finds the corresponding cron job, sets its enabled flag
// back to true, and removes the DLQ entry. If the cron job no longer exists,
// only the DLQ entry is removed.
func (r *CronRepo) RetryFromDLQ(ctx context.Context, id string) error {
	// Look up the DLQ entry to find the associated cron job.
	var cronJobID string
	err := r.db.QueryRowContext(ctx,
		`SELECT cron_job_id FROM cron_dlq WHERE id = ?`, id,
	).Scan(&cronJobID)
	if err == sql.ErrNoRows {
		return fmt.Errorf("DLQ entry %q not found", id)
	}
	if err != nil {
		return fmt.Errorf("sqlite.CronRepo.RetryFromDLQ: %w", err)
	}

	// Re-enable the cron job if it still exists.
	if _, err := r.db.ExecContext(ctx,
		`UPDATE cron_jobs SET enabled = 1, updated_at = datetime('now') WHERE id = ?`,
		cronJobID,
	); err != nil {
		slog.Error("failed to re-enable cron job after DLQ recovery", "job_id", cronJobID, "error", err)
		return fmt.Errorf("sqlite.CronRepo.RetryFromDLQ: re-enable cron job %s: %w", cronJobID, err)
	}

	// Remove the DLQ entry.
	_, err = r.db.ExecContext(ctx, `DELETE FROM cron_dlq WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sqlite.CronRepo.RetryFromDLQ: %w", err)
	}

	return nil
}

// DeleteFromDLQ removes a DLQ entry without retrying.
func (r *CronRepo) DeleteFromDLQ(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM cron_dlq WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sqlite.CronRepo.DeleteFromDLQ: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.CronRepo.DeleteFromDLQ: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("DLQ entry %q not found", id)
	}

	return nil
}
