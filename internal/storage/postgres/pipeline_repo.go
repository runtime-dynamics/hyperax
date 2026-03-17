package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/repo"
)

// PipelineRepo implements repo.PipelineRepo for PostgreSQL.
type PipelineRepo struct {
	db *sql.DB
}

// CreatePipeline inserts a new pipeline definition and returns its generated ID.
func (r *PipelineRepo) CreatePipeline(ctx context.Context, pipeline *repo.Pipeline) (string, error) {
	if pipeline.ID == "" {
		pipeline.ID = uuid.New().String()
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO pipelines (id, name, description, workspace_name, project_name, swimlanes, setup_commands, environment)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		pipeline.ID, pipeline.Name, pipeline.Description, pipeline.WorkspaceName, pipeline.ProjectName,
		pipeline.Swimlanes, pipeline.SetupCommands, pipeline.Environment,
	)
	if err != nil {
		return "", fmt.Errorf("postgres.PipelineRepo.CreatePipeline: %w", err)
	}
	return pipeline.ID, nil
}

// GetPipeline retrieves a single pipeline by ID.
func (r *PipelineRepo) GetPipeline(ctx context.Context, id string) (*repo.Pipeline, error) {
	p := &repo.Pipeline{}
	var description, projectName sql.NullString
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, COALESCE(description, ''), workspace_name, COALESCE(project_name, ''),
		        swimlanes, setup_commands, environment, created_at, updated_at
		 FROM pipelines WHERE id = $1`, id,
	).Scan(&p.ID, &p.Name, &description, &p.WorkspaceName, &projectName,
		&p.Swimlanes, &p.SetupCommands, &p.Environment, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("pipeline %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres.PipelineRepo.GetPipeline: %w", err)
	}
	p.Description = description.String
	p.ProjectName = projectName.String
	return p, nil
}

// ListPipelines returns all pipelines for a given workspace.
func (r *PipelineRepo) ListPipelines(ctx context.Context, workspaceName string) ([]*repo.Pipeline, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, COALESCE(description, ''), workspace_name, COALESCE(project_name, ''),
		        swimlanes, setup_commands, environment, created_at, updated_at
		 FROM pipelines WHERE workspace_name = $1 ORDER BY name`, workspaceName,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.PipelineRepo.ListPipelines: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanPgPipelines(rows)
}

// CreateJob inserts a new pipeline job in "pending" status.
func (r *PipelineRepo) CreateJob(ctx context.Context, pipelineID, workspaceName string) (string, error) {
	id := uuid.New().String()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO pipeline_jobs (id, pipeline_id, status, workspace_name, started_at)
		 VALUES ($1, $2, 'pending', $3, NOW())`, id, pipelineID, workspaceName,
	)
	if err != nil {
		return "", fmt.Errorf("postgres.PipelineRepo.CreateJob: %w", err)
	}
	return id, nil
}

// GetJob retrieves a single pipeline job by ID.
func (r *PipelineRepo) GetJob(ctx context.Context, id string) (*repo.PipelineJob, error) {
	j := &repo.PipelineJob{}
	var startedAt, completedAt sql.NullTime
	var jobError, result sql.NullString

	err := r.db.QueryRowContext(ctx,
		`SELECT id, pipeline_id, status, workspace_name, started_at, completed_at, COALESCE(error, ''), COALESCE(result, '')
		 FROM pipeline_jobs WHERE id = $1`, id,
	).Scan(&j.ID, &j.PipelineID, &j.Status, &j.WorkspaceName, &startedAt, &completedAt, &jobError, &result)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("job %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres.PipelineRepo.GetJob: %w", err)
	}
	if startedAt.Valid {
		j.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		j.CompletedAt = &completedAt.Time
	}
	j.Error = jobError.String
	j.Result = result.String
	return j, nil
}

// UpdateJobStatus sets the status, result, and completed_at timestamp for a job.
func (r *PipelineRepo) UpdateJobStatus(ctx context.Context, id string, status string, result string) error {
	var res sql.Result
	var err error

	if status == "completed" || status == "failed" || status == "cancelled" {
		res, err = r.db.ExecContext(ctx,
			`UPDATE pipeline_jobs SET status = $1, result = $2, completed_at = NOW() WHERE id = $3`,
			status, result, id,
		)
	} else {
		res, err = r.db.ExecContext(ctx,
			`UPDATE pipeline_jobs SET status = $1, result = $2 WHERE id = $3`,
			status, result, id,
		)
	}
	if err != nil {
		return fmt.Errorf("postgres.PipelineRepo.UpdateJobStatus: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.PipelineRepo.UpdateJobStatus: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("job %q not found", id)
	}
	return nil
}

// ListJobs returns all jobs for a given pipeline.
func (r *PipelineRepo) ListJobs(ctx context.Context, pipelineID string) ([]*repo.PipelineJob, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, pipeline_id, status, workspace_name, started_at, completed_at, COALESCE(error, ''), COALESCE(result, '')
		 FROM pipeline_jobs WHERE pipeline_id = $1 ORDER BY started_at DESC`, pipelineID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.PipelineRepo.ListJobs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanPgJobs(rows)
}

// CreateStepResult inserts a new step result.
func (r *PipelineRepo) CreateStepResult(ctx context.Context, result *repo.StepResult) (string, error) {
	if result.ID == "" {
		result.ID = uuid.New().String()
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO step_results (id, job_id, swimlane_id, step_id, step_name, status, exit_code, started_at, completed_at, duration_ms, output_log, error)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		result.ID, result.JobID, result.SwimlaneID, result.StepID, result.StepName,
		result.Status, result.ExitCode, result.StartedAt, result.CompletedAt, result.DurationMS,
		result.OutputLog, result.Error,
	)
	if err != nil {
		return "", fmt.Errorf("postgres.PipelineRepo.CreateStepResult: %w", err)
	}
	return result.ID, nil
}

// ListStepResults returns all step results for a given job.
func (r *PipelineRepo) ListStepResults(ctx context.Context, jobID string) ([]*repo.StepResult, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, job_id, swimlane_id, step_id, step_name, status, exit_code,
		        started_at, completed_at, duration_ms, COALESCE(output_log, ''), COALESCE(error, '')
		 FROM step_results WHERE job_id = $1 ORDER BY step_name`, jobID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.PipelineRepo.ListStepResults: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []*repo.StepResult
	for rows.Next() {
		s := &repo.StepResult{}
		var exitCode sql.NullInt64
		var durationMS sql.NullInt64
		var startedAt, completedAt sql.NullTime

		if err := rows.Scan(&s.ID, &s.JobID, &s.SwimlaneID, &s.StepID, &s.StepName, &s.Status,
			&exitCode, &startedAt, &completedAt, &durationMS, &s.OutputLog, &s.Error); err != nil {
			return nil, fmt.Errorf("postgres.PipelineRepo.ListStepResults: %w", err)
		}
		if exitCode.Valid {
			code := int(exitCode.Int64)
			s.ExitCode = &code
		}
		if startedAt.Valid {
			s.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			s.CompletedAt = &completedAt.Time
		}
		if durationMS.Valid {
			ms := int(durationMS.Int64)
			s.DurationMS = &ms
		}
		results = append(results, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.PipelineRepo.ListStepResults: %w", err)
	}
	return results, nil
}

// SearchPipelines returns pipelines matching a name/description query.
func (r *PipelineRepo) SearchPipelines(ctx context.Context, query string, workspaceName string) ([]*repo.Pipeline, error) {
	var sqlStr string
	var args []any

	switch {
	case query != "" && workspaceName != "":
		pattern := "%" + query + "%"
		sqlStr = `SELECT id, name, COALESCE(description, ''), workspace_name, COALESCE(project_name, ''),
		          swimlanes, setup_commands, environment, created_at, updated_at
		          FROM pipelines WHERE workspace_name = $1 AND (name ILIKE $2 OR description ILIKE $3) ORDER BY name`
		args = []any{workspaceName, pattern, pattern}
	case query != "":
		pattern := "%" + query + "%"
		sqlStr = `SELECT id, name, COALESCE(description, ''), workspace_name, COALESCE(project_name, ''),
		          swimlanes, setup_commands, environment, created_at, updated_at
		          FROM pipelines WHERE name ILIKE $1 OR description ILIKE $2 ORDER BY name`
		args = []any{pattern, pattern}
	case workspaceName != "":
		sqlStr = `SELECT id, name, COALESCE(description, ''), workspace_name, COALESCE(project_name, ''),
		          swimlanes, setup_commands, environment, created_at, updated_at
		          FROM pipelines WHERE workspace_name = $1 ORDER BY name`
		args = []any{workspaceName}
	default:
		sqlStr = `SELECT id, name, COALESCE(description, ''), workspace_name, COALESCE(project_name, ''),
		          swimlanes, setup_commands, environment, created_at, updated_at
		          FROM pipelines ORDER BY name`
	}

	rows, err := r.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres.PipelineRepo.SearchPipelines: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanPgPipelines(rows)
}

// ListJobsFiltered returns jobs with optional status filter and limit.
func (r *PipelineRepo) ListJobsFiltered(ctx context.Context, pipelineID string, filter repo.JobFilter) ([]*repo.PipelineJob, error) {
	var sqlStr string
	var args []any

	if filter.Status != "" {
		sqlStr = `SELECT id, pipeline_id, status, workspace_name, started_at, completed_at, COALESCE(error, ''), COALESCE(result, '')
		          FROM pipeline_jobs WHERE pipeline_id = $1 AND status = $2 ORDER BY started_at DESC`
		args = []any{pipelineID, filter.Status}
	} else {
		sqlStr = `SELECT id, pipeline_id, status, workspace_name, started_at, completed_at, COALESCE(error, ''), COALESCE(result, '')
		          FROM pipeline_jobs WHERE pipeline_id = $1 ORDER BY started_at DESC`
		args = []any{pipelineID}
	}

	if filter.Limit > 0 {
		sqlStr += fmt.Sprintf(" LIMIT $%d", len(args)+1)
		args = append(args, filter.Limit)
	}

	rows, err := r.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres.PipelineRepo.ListJobsFiltered: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanPgJobs(rows)
}

// CreateAssignment inserts a new pipeline assignment.
func (r *PipelineRepo) CreateAssignment(ctx context.Context, assignment *repo.PipelineAssignment) (string, error) {
	if assignment.ID == "" {
		assignment.ID = uuid.New().String()
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO pipeline_assignments (id, pipeline_id, workspace_id, project_id) VALUES ($1, $2, $3, $4)`,
		assignment.ID, assignment.PipelineID, assignment.WorkspaceID, assignment.ProjectID,
	)
	if err != nil {
		return "", fmt.Errorf("postgres.PipelineRepo.CreateAssignment: %w", err)
	}
	return assignment.ID, nil
}

// ListAssignments returns pipeline assignments filtered by optional criteria.
func (r *PipelineRepo) ListAssignments(ctx context.Context, workspaceID string, pipelineID string) ([]*repo.PipelineAssignment, error) {
	var sqlStr string
	var args []any

	switch {
	case workspaceID != "" && pipelineID != "":
		sqlStr = `SELECT id, pipeline_id, workspace_id, COALESCE(project_id, ''), assigned_at
		          FROM pipeline_assignments WHERE workspace_id = $1 AND pipeline_id = $2 ORDER BY assigned_at DESC`
		args = []any{workspaceID, pipelineID}
	case workspaceID != "":
		sqlStr = `SELECT id, pipeline_id, workspace_id, COALESCE(project_id, ''), assigned_at
		          FROM pipeline_assignments WHERE workspace_id = $1 ORDER BY assigned_at DESC`
		args = []any{workspaceID}
	case pipelineID != "":
		sqlStr = `SELECT id, pipeline_id, workspace_id, COALESCE(project_id, ''), assigned_at
		          FROM pipeline_assignments WHERE pipeline_id = $1 ORDER BY assigned_at DESC`
		args = []any{pipelineID}
	default:
		sqlStr = `SELECT id, pipeline_id, workspace_id, COALESCE(project_id, ''), assigned_at
		          FROM pipeline_assignments ORDER BY assigned_at DESC`
	}

	rows, err := r.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres.PipelineRepo.ListAssignments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var assignments []*repo.PipelineAssignment
	for rows.Next() {
		a := &repo.PipelineAssignment{}
		var projectID sql.NullString
		if err := rows.Scan(&a.ID, &a.PipelineID, &a.WorkspaceID, &projectID, &a.AssignedAt); err != nil {
			return nil, fmt.Errorf("postgres.PipelineRepo.ListAssignments: %w", err)
		}
		a.ProjectID = projectID.String
		assignments = append(assignments, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.PipelineRepo.ListAssignments: %w", err)
	}
	return assignments, nil
}

// DeleteAssignment removes a pipeline assignment by ID.
func (r *PipelineRepo) DeleteAssignment(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM pipeline_assignments WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("postgres.PipelineRepo.DeleteAssignment: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.PipelineRepo.DeleteAssignment: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("assignment %q not found", id)
	}
	return nil
}

func scanPgPipelines(rows *sql.Rows) ([]*repo.Pipeline, error) {
	var pipelines []*repo.Pipeline
	for rows.Next() {
		p := &repo.Pipeline{}
		var description, projectName sql.NullString
		if err := rows.Scan(&p.ID, &p.Name, &description, &p.WorkspaceName, &projectName,
			&p.Swimlanes, &p.SetupCommands, &p.Environment, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("postgres.scanPgPipelines: %w", err)
		}
		p.Description = description.String
		p.ProjectName = projectName.String
		pipelines = append(pipelines, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.scanPgPipelines: %w", err)
	}
	return pipelines, nil
}

func scanPgJobs(rows *sql.Rows) ([]*repo.PipelineJob, error) {
	var jobs []*repo.PipelineJob
	for rows.Next() {
		j := &repo.PipelineJob{}
		var startedAt, completedAt sql.NullTime
		var jobError, result sql.NullString

		if err := rows.Scan(&j.ID, &j.PipelineID, &j.Status, &j.WorkspaceName,
			&startedAt, &completedAt, &jobError, &result); err != nil {
			return nil, fmt.Errorf("postgres.scanPgJobs: %w", err)
		}
		if startedAt.Valid {
			t := startedAt.Time
			j.StartedAt = &t
		}
		if completedAt.Valid {
			t := completedAt.Time
			j.CompletedAt = &t
		}
		j.Error = jobError.String
		j.Result = result.String
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.scanPgJobs: %w", err)
	}
	return jobs, nil
}

// ensure unused import suppression
var _ = time.Now
