package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/repo"
)

// PipelineRepo implements repo.PipelineRepo for SQLite.
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
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		pipeline.ID, pipeline.Name, pipeline.Description,
		pipeline.WorkspaceName, pipeline.ProjectName,
		pipeline.Swimlanes, pipeline.SetupCommands, pipeline.Environment,
	)
	if err != nil {
		return "", fmt.Errorf("sqlite.PipelineRepo.CreatePipeline: %w", err)
	}
	return pipeline.ID, nil
}

// GetPipeline retrieves a single pipeline by ID.
func (r *PipelineRepo) GetPipeline(ctx context.Context, id string) (*repo.Pipeline, error) {
	p := &repo.Pipeline{}
	var createdAt, updatedAt string
	var description, projectName sql.NullString

	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, COALESCE(description, ''), workspace_name, COALESCE(project_name, ''),
		        swimlanes, setup_commands, environment, created_at, updated_at
		 FROM pipelines WHERE id = ?`, id,
	).Scan(
		&p.ID, &p.Name, &description, &p.WorkspaceName, &projectName,
		&p.Swimlanes, &p.SetupCommands, &p.Environment, &createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("pipeline %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite.PipelineRepo.GetPipeline: %w", err)
	}

	p.Description = description.String
	p.ProjectName = projectName.String
	if p.CreatedAt, err = parseSQLiteTime(createdAt, "sqlite.PipelineRepo.GetPipeline"); err != nil {
		return nil, err
	}
	if p.UpdatedAt, err = parseSQLiteTime(updatedAt, "sqlite.PipelineRepo.GetPipeline"); err != nil {
		return nil, err
	}
	return p, nil
}

// ListPipelines returns all pipelines for a given workspace.
func (r *PipelineRepo) ListPipelines(ctx context.Context, workspaceName string) ([]*repo.Pipeline, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, COALESCE(description, ''), workspace_name, COALESCE(project_name, ''),
		        swimlanes, setup_commands, environment, created_at, updated_at
		 FROM pipelines WHERE workspace_name = ? ORDER BY name`, workspaceName,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.PipelineRepo.ListPipelines: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var pipelines []*repo.Pipeline
	for rows.Next() {
		p := &repo.Pipeline{}
		var createdAt, updatedAt string
		var description, projectName sql.NullString

		if err := rows.Scan(
			&p.ID, &p.Name, &description, &p.WorkspaceName, &projectName,
			&p.Swimlanes, &p.SetupCommands, &p.Environment, &createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("sqlite.PipelineRepo.ListPipelines: %w", err)
		}

		p.Description = description.String
		p.ProjectName = projectName.String
		var parseErr error
		if p.CreatedAt, parseErr = parseSQLiteTime(createdAt, "sqlite.PipelineRepo.ListPipelines"); parseErr != nil {
			return nil, parseErr
		}
		if p.UpdatedAt, parseErr = parseSQLiteTime(updatedAt, "sqlite.PipelineRepo.ListPipelines"); parseErr != nil {
			return nil, parseErr
		}
		pipelines = append(pipelines, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.PipelineRepo.ListPipelines: %w", err)
	}
	return pipelines, nil
}

// CreateJob inserts a new pipeline job in "pending" status and returns its generated ID.
func (r *PipelineRepo) CreateJob(ctx context.Context, pipelineID, workspaceName string) (string, error) {
	id := uuid.New().String()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO pipeline_jobs (id, pipeline_id, status, workspace_name, started_at)
		 VALUES (?, ?, 'pending', ?, datetime('now'))`,
		id, pipelineID, workspaceName,
	)
	if err != nil {
		return "", fmt.Errorf("sqlite.PipelineRepo.CreateJob: %w", err)
	}
	return id, nil
}

// GetJob retrieves a single pipeline job by ID.
func (r *PipelineRepo) GetJob(ctx context.Context, id string) (*repo.PipelineJob, error) {
	j := &repo.PipelineJob{}
	var startedAt, completedAt, jobError, result sql.NullString

	err := r.db.QueryRowContext(ctx,
		`SELECT id, pipeline_id, status, workspace_name, started_at, completed_at, COALESCE(error, ''), COALESCE(result, '')
		 FROM pipeline_jobs WHERE id = ?`, id,
	).Scan(
		&j.ID, &j.PipelineID, &j.Status, &j.WorkspaceName,
		&startedAt, &completedAt, &jobError, &result,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("job %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite.PipelineRepo.GetJob: %w", err)
	}

	if startedAt.Valid {
		t, parseErr := parseSQLiteTime(startedAt.String, "sqlite.PipelineRepo.GetJob.startedAt")
		if parseErr != nil {
			return nil, parseErr
		}
		j.StartedAt = &t
	}
	if completedAt.Valid {
		t, parseErr := parseSQLiteTime(completedAt.String, "sqlite.PipelineRepo.GetJob.completedAt")
		if parseErr != nil {
			return nil, parseErr
		}
		j.CompletedAt = &t
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
			`UPDATE pipeline_jobs SET status = ?, result = ?, completed_at = datetime('now') WHERE id = ?`,
			status, result, id,
		)
	} else {
		res, err = r.db.ExecContext(ctx,
			`UPDATE pipeline_jobs SET status = ?, result = ? WHERE id = ?`,
			status, result, id,
		)
	}
	if err != nil {
		return fmt.Errorf("sqlite.PipelineRepo.UpdateJobStatus: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.PipelineRepo.UpdateJobStatus: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("job %q not found", id)
	}
	return nil
}

// ListJobs returns all jobs for a given pipeline, ordered by most recent first.
func (r *PipelineRepo) ListJobs(ctx context.Context, pipelineID string) ([]*repo.PipelineJob, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, pipeline_id, status, workspace_name, started_at, completed_at, COALESCE(error, ''), COALESCE(result, '')
		 FROM pipeline_jobs WHERE pipeline_id = ? ORDER BY started_at DESC`, pipelineID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.PipelineRepo.ListJobs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var jobs []*repo.PipelineJob
	for rows.Next() {
		j := &repo.PipelineJob{}
		var startedAt, completedAt, jobError, result sql.NullString

		if err := rows.Scan(
			&j.ID, &j.PipelineID, &j.Status, &j.WorkspaceName,
			&startedAt, &completedAt, &jobError, &result,
		); err != nil {
			return nil, fmt.Errorf("sqlite.PipelineRepo.ListJobs: %w", err)
		}

		if startedAt.Valid {
			t, parseErr := parseSQLiteTime(startedAt.String, "sqlite.PipelineRepo.ListJobs.startedAt")
			if parseErr != nil {
				return nil, parseErr
			}
			j.StartedAt = &t
		}
		if completedAt.Valid {
			t, parseErr := parseSQLiteTime(completedAt.String, "sqlite.PipelineRepo.ListJobs.completedAt")
			if parseErr != nil {
				return nil, parseErr
			}
			j.CompletedAt = &t
		}
		j.Error = jobError.String
		j.Result = result.String
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.PipelineRepo.ListJobs: %w", err)
	}
	return jobs, nil
}

// CreateStepResult inserts a new step result and returns its generated ID.
func (r *PipelineRepo) CreateStepResult(ctx context.Context, result *repo.StepResult) (string, error) {
	if result.ID == "" {
		result.ID = uuid.New().String()
	}

	var startedAt, completedAt sql.NullString
	if result.StartedAt != nil {
		startedAt = sql.NullString{String: result.StartedAt.Format("2006-01-02 15:04:05"), Valid: true}
	}
	if result.CompletedAt != nil {
		completedAt = sql.NullString{String: result.CompletedAt.Format("2006-01-02 15:04:05"), Valid: true}
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO step_results (id, job_id, swimlane_id, step_id, step_name, status, exit_code, started_at, completed_at, duration_ms, output_log, error)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		result.ID, result.JobID, result.SwimlaneID, result.StepID, result.StepName,
		result.Status, result.ExitCode, startedAt, completedAt, result.DurationMS,
		result.OutputLog, result.Error,
	)
	if err != nil {
		return "", fmt.Errorf("sqlite.PipelineRepo.CreateStepResult: %w", err)
	}
	return result.ID, nil
}

// ListStepResults returns all step results for a given job, ordered by step name.
func (r *PipelineRepo) ListStepResults(ctx context.Context, jobID string) ([]*repo.StepResult, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, job_id, swimlane_id, step_id, step_name, status, exit_code,
		        started_at, completed_at, duration_ms, COALESCE(output_log, ''), COALESCE(error, '')
		 FROM step_results WHERE job_id = ? ORDER BY step_name`, jobID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.PipelineRepo.ListStepResults: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []*repo.StepResult
	for rows.Next() {
		s := &repo.StepResult{}
		var exitCode, durationMS sql.NullInt64
		var startedAt, completedAt, outputLog, stepError sql.NullString

		if err := rows.Scan(
			&s.ID, &s.JobID, &s.SwimlaneID, &s.StepID, &s.StepName, &s.Status,
			&exitCode, &startedAt, &completedAt, &durationMS, &outputLog, &stepError,
		); err != nil {
			return nil, fmt.Errorf("sqlite.PipelineRepo.ListStepResults: %w", err)
		}

		if exitCode.Valid {
			code := int(exitCode.Int64)
			s.ExitCode = &code
		}
		if startedAt.Valid {
			t, parseErr := parseSQLiteTime(startedAt.String, "sqlite.PipelineRepo.ListStepResults.startedAt")
			if parseErr != nil {
				return nil, parseErr
			}
			s.StartedAt = &t
		}
		if completedAt.Valid {
			t, parseErr := parseSQLiteTime(completedAt.String, "sqlite.PipelineRepo.ListStepResults.completedAt")
			if parseErr != nil {
				return nil, parseErr
			}
			s.CompletedAt = &t
		}
		if durationMS.Valid {
			ms := int(durationMS.Int64)
			s.DurationMS = &ms
		}
		s.OutputLog = outputLog.String
		s.Error = stepError.String
		results = append(results, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.PipelineRepo.ListStepResults: %w", err)
	}
	return results, nil
}

// SearchPipelines returns pipelines matching a name/description query and optional
// workspace filter. When query is empty all pipelines are returned (optionally
// scoped by workspace). The search uses case-insensitive LIKE matching.
func (r *PipelineRepo) SearchPipelines(ctx context.Context, query string, workspaceName string) ([]*repo.Pipeline, error) {
	var (
		sqlStr string
		args   []any
	)

	switch {
	case query != "" && workspaceName != "":
		pattern := "%" + query + "%"
		sqlStr = `SELECT id, name, COALESCE(description, ''), workspace_name, COALESCE(project_name, ''),
		          swimlanes, setup_commands, environment, created_at, updated_at
		          FROM pipelines WHERE workspace_name = ? AND (name LIKE ? OR description LIKE ?) ORDER BY name`
		args = []any{workspaceName, pattern, pattern}
	case query != "":
		pattern := "%" + query + "%"
		sqlStr = `SELECT id, name, COALESCE(description, ''), workspace_name, COALESCE(project_name, ''),
		          swimlanes, setup_commands, environment, created_at, updated_at
		          FROM pipelines WHERE name LIKE ? OR description LIKE ? ORDER BY name`
		args = []any{pattern, pattern}
	case workspaceName != "":
		sqlStr = `SELECT id, name, COALESCE(description, ''), workspace_name, COALESCE(project_name, ''),
		          swimlanes, setup_commands, environment, created_at, updated_at
		          FROM pipelines WHERE workspace_name = ? ORDER BY name`
		args = []any{workspaceName}
	default:
		sqlStr = `SELECT id, name, COALESCE(description, ''), workspace_name, COALESCE(project_name, ''),
		          swimlanes, setup_commands, environment, created_at, updated_at
		          FROM pipelines ORDER BY name`
	}

	rows, err := r.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite.PipelineRepo.SearchPipelines: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var pipelines []*repo.Pipeline
	for rows.Next() {
		p := &repo.Pipeline{}
		var createdAt, updatedAt string
		var description, projectName sql.NullString

		if err := rows.Scan(
			&p.ID, &p.Name, &description, &p.WorkspaceName, &projectName,
			&p.Swimlanes, &p.SetupCommands, &p.Environment, &createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("sqlite.PipelineRepo.SearchPipelines: %w", err)
		}

		p.Description = description.String
		p.ProjectName = projectName.String
		var parseErr error
		if p.CreatedAt, parseErr = parseSQLiteTime(createdAt, "sqlite.PipelineRepo.SearchPipelines"); parseErr != nil {
			return nil, parseErr
		}
		if p.UpdatedAt, parseErr = parseSQLiteTime(updatedAt, "sqlite.PipelineRepo.SearchPipelines"); parseErr != nil {
			return nil, parseErr
		}
		pipelines = append(pipelines, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.PipelineRepo.SearchPipelines: %w", err)
	}
	return pipelines, nil
}

// ListJobsFiltered returns jobs for a pipeline with optional status filter and limit.
// When filter.Status is empty all statuses are included. When filter.Limit is 0 no
// limit is applied.
func (r *PipelineRepo) ListJobsFiltered(ctx context.Context, pipelineID string, filter repo.JobFilter) ([]*repo.PipelineJob, error) {
	var (
		sqlStr string
		args   []any
	)

	if filter.Status != "" {
		sqlStr = `SELECT id, pipeline_id, status, workspace_name, started_at, completed_at, COALESCE(error, ''), COALESCE(result, '')
		          FROM pipeline_jobs WHERE pipeline_id = ? AND status = ? ORDER BY started_at DESC`
		args = []any{pipelineID, filter.Status}
	} else {
		sqlStr = `SELECT id, pipeline_id, status, workspace_name, started_at, completed_at, COALESCE(error, ''), COALESCE(result, '')
		          FROM pipeline_jobs WHERE pipeline_id = ? ORDER BY started_at DESC`
		args = []any{pipelineID}
	}

	if filter.Limit > 0 {
		sqlStr += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := r.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite.PipelineRepo.ListJobsFiltered: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var jobs []*repo.PipelineJob
	for rows.Next() {
		j := &repo.PipelineJob{}
		var startedAt, completedAt, jobError, result sql.NullString

		if err := rows.Scan(
			&j.ID, &j.PipelineID, &j.Status, &j.WorkspaceName,
			&startedAt, &completedAt, &jobError, &result,
		); err != nil {
			return nil, fmt.Errorf("sqlite.PipelineRepo.ListJobsFiltered: %w", err)
		}

		if startedAt.Valid {
			t, parseErr := parseSQLiteTime(startedAt.String, "sqlite.PipelineRepo.ListJobsFiltered.startedAt")
			if parseErr != nil {
				return nil, parseErr
			}
			j.StartedAt = &t
		}
		if completedAt.Valid {
			t, parseErr := parseSQLiteTime(completedAt.String, "sqlite.PipelineRepo.ListJobsFiltered.completedAt")
			if parseErr != nil {
				return nil, parseErr
			}
			j.CompletedAt = &t
		}
		j.Error = jobError.String
		j.Result = result.String
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.PipelineRepo.ListJobsFiltered: %w", err)
	}
	return jobs, nil
}

// CreateAssignment inserts a new pipeline assignment and returns its generated ID.
func (r *PipelineRepo) CreateAssignment(ctx context.Context, assignment *repo.PipelineAssignment) (string, error) {
	if assignment.ID == "" {
		assignment.ID = uuid.New().String()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO pipeline_assignments (id, pipeline_id, workspace_id, project_id)
		 VALUES (?, ?, ?, ?)`,
		assignment.ID, assignment.PipelineID, assignment.WorkspaceID, assignment.ProjectID,
	)
	if err != nil {
		return "", fmt.Errorf("sqlite.PipelineRepo.CreateAssignment: %w", err)
	}
	return assignment.ID, nil
}

// ListAssignments returns pipeline assignments filtered by optional workspace and
// pipeline criteria. Both filters are optional; when empty they are not applied.
func (r *PipelineRepo) ListAssignments(ctx context.Context, workspaceID string, pipelineID string) ([]*repo.PipelineAssignment, error) {
	var (
		sqlStr string
		args   []any
	)

	switch {
	case workspaceID != "" && pipelineID != "":
		sqlStr = `SELECT id, pipeline_id, workspace_id, COALESCE(project_id, ''), assigned_at
		          FROM pipeline_assignments WHERE workspace_id = ? AND pipeline_id = ? ORDER BY assigned_at DESC`
		args = []any{workspaceID, pipelineID}
	case workspaceID != "":
		sqlStr = `SELECT id, pipeline_id, workspace_id, COALESCE(project_id, ''), assigned_at
		          FROM pipeline_assignments WHERE workspace_id = ? ORDER BY assigned_at DESC`
		args = []any{workspaceID}
	case pipelineID != "":
		sqlStr = `SELECT id, pipeline_id, workspace_id, COALESCE(project_id, ''), assigned_at
		          FROM pipeline_assignments WHERE pipeline_id = ? ORDER BY assigned_at DESC`
		args = []any{pipelineID}
	default:
		sqlStr = `SELECT id, pipeline_id, workspace_id, COALESCE(project_id, ''), assigned_at
		          FROM pipeline_assignments ORDER BY assigned_at DESC`
	}

	rows, err := r.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite.PipelineRepo.ListAssignments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var assignments []*repo.PipelineAssignment
	for rows.Next() {
		a := &repo.PipelineAssignment{}
		var assignedAt string
		var projectID sql.NullString

		if err := rows.Scan(&a.ID, &a.PipelineID, &a.WorkspaceID, &projectID, &assignedAt); err != nil {
			return nil, fmt.Errorf("sqlite.PipelineRepo.ListAssignments: %w", err)
		}

		a.ProjectID = projectID.String
		var parseErr error
		if a.AssignedAt, parseErr = parseSQLiteTime(assignedAt, "sqlite.PipelineRepo.ListAssignments"); parseErr != nil {
			return nil, parseErr
		}
		assignments = append(assignments, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.PipelineRepo.ListAssignments: %w", err)
	}
	return assignments, nil
}

// DeleteAssignment removes a pipeline assignment by ID.
func (r *PipelineRepo) DeleteAssignment(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM pipeline_assignments WHERE id = ?`, id,
	)
	if err != nil {
		return fmt.Errorf("sqlite.PipelineRepo.DeleteAssignment: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.PipelineRepo.DeleteAssignment: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("assignment %q not found", id)
	}
	return nil
}
