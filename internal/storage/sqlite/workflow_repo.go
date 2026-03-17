package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/repo"
)

// WorkflowRepo implements repo.WorkflowRepo for SQLite.
type WorkflowRepo struct {
	db *sql.DB
}

// sqliteTimeFmt is the canonical time format used for all SQLite datetime columns.
const sqliteTimeFmt = "2006-01-02 15:04:05"

// ---------------------------------------------------------------------------
// Workflow CRUD
// ---------------------------------------------------------------------------

// CreateWorkflow inserts a new workflow definition and returns its generated ID.
func (r *WorkflowRepo) CreateWorkflow(ctx context.Context, wf *repo.Workflow) (string, error) {
	if wf.ID == "" {
		wf.ID = uuid.New().String()
	}

	enabled := 0
	if wf.Enabled {
		enabled = 1
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO workflows (id, name, description, enabled)
		 VALUES (?, ?, ?, ?)`,
		wf.ID, wf.Name, wf.Description, enabled,
	)
	if err != nil {
		return "", fmt.Errorf("sqlite.WorkflowRepo.CreateWorkflow: %w", err)
	}

	return wf.ID, nil
}

// GetWorkflow retrieves a workflow definition by its ID.
func (r *WorkflowRepo) GetWorkflow(ctx context.Context, id string) (*repo.Workflow, error) {
	wf := &repo.Workflow{}
	var enabled int
	var createdAt, updatedAt string

	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, description, enabled, created_at, updated_at
		 FROM workflows WHERE id = ?`, id,
	).Scan(&wf.ID, &wf.Name, &wf.Description, &enabled, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("workflow %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite.WorkflowRepo.GetWorkflow: %w", err)
	}

	wf.Enabled = enabled == 1
	if wf.CreatedAt, err = parseSQLiteTime(createdAt, "sqlite.WorkflowRepo.GetWorkflow"); err != nil {
		return nil, err
	}
	if wf.UpdatedAt, err = parseSQLiteTime(updatedAt, "sqlite.WorkflowRepo.GetWorkflow"); err != nil {
		return nil, err
	}

	return wf, nil
}

// ListWorkflows returns all workflow definitions ordered by name.
func (r *WorkflowRepo) ListWorkflows(ctx context.Context) ([]*repo.Workflow, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, description, enabled, created_at, updated_at
		 FROM workflows ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.WorkflowRepo.ListWorkflows: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var workflows []*repo.Workflow
	for rows.Next() {
		wf := &repo.Workflow{}
		var enabled int
		var createdAt, updatedAt string

		if err := rows.Scan(&wf.ID, &wf.Name, &wf.Description, &enabled, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("sqlite.WorkflowRepo.ListWorkflows: %w", err)
		}

		wf.Enabled = enabled == 1
		var parseErr error
		if wf.CreatedAt, parseErr = parseSQLiteTime(createdAt, "sqlite.WorkflowRepo.ListWorkflows"); parseErr != nil {
			return nil, parseErr
		}
		if wf.UpdatedAt, parseErr = parseSQLiteTime(updatedAt, "sqlite.WorkflowRepo.ListWorkflows"); parseErr != nil {
			return nil, parseErr
		}

		workflows = append(workflows, wf)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.WorkflowRepo.ListWorkflows: %w", err)
	}
	return workflows, nil
}

// UpdateWorkflow modifies an existing workflow definition.
func (r *WorkflowRepo) UpdateWorkflow(ctx context.Context, wf *repo.Workflow) error {
	enabled := 0
	if wf.Enabled {
		enabled = 1
	}

	res, err := r.db.ExecContext(ctx,
		`UPDATE workflows SET
		    name = ?, description = ?, enabled = ?,
		    updated_at = datetime('now')
		 WHERE id = ?`,
		wf.Name, wf.Description, enabled, wf.ID,
	)
	if err != nil {
		return fmt.Errorf("sqlite.WorkflowRepo.UpdateWorkflow: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.WorkflowRepo.UpdateWorkflow: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("workflow %q not found", wf.ID)
	}

	return nil
}

// DeleteWorkflow removes a workflow definition by its ID. Associated steps,
// runs, run steps, and triggers are cascade-deleted.
func (r *WorkflowRepo) DeleteWorkflow(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM workflows WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("sqlite.WorkflowRepo.DeleteWorkflow: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.WorkflowRepo.DeleteWorkflow: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("workflow %q not found", id)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Step CRUD
// ---------------------------------------------------------------------------

// CreateStep inserts a new workflow step and returns its generated ID.
func (r *WorkflowRepo) CreateStep(ctx context.Context, step *repo.WorkflowStep) (string, error) {
	if step.ID == "" {
		step.ID = uuid.New().String()
	}

	action := string(step.Action)
	if action == "" {
		action = "{}"
	}

	approval := 0
	if step.RequiresApproval {
		approval = 1
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO workflow_steps (id, workflow_id, name, step_type, action, depends_on, condition, requires_approval, position)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		step.ID, step.WorkflowID, step.Name, step.StepType,
		action, step.DependsOn, step.Condition, approval, step.Position,
	)
	if err != nil {
		return "", fmt.Errorf("sqlite.WorkflowRepo.CreateStep: %w", err)
	}

	return step.ID, nil
}

// GetSteps returns all steps for a workflow, ordered by position.
func (r *WorkflowRepo) GetSteps(ctx context.Context, workflowID string) ([]*repo.WorkflowStep, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, workflow_id, name, step_type, action, depends_on, condition,
		        requires_approval, position, created_at, updated_at
		 FROM workflow_steps
		 WHERE workflow_id = ?
		 ORDER BY position`, workflowID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.WorkflowRepo.GetSteps: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var steps []*repo.WorkflowStep
	for rows.Next() {
		s := &repo.WorkflowStep{}
		var action string
		var approval int
		var createdAt, updatedAt string

		if err := rows.Scan(
			&s.ID, &s.WorkflowID, &s.Name, &s.StepType, &action,
			&s.DependsOn, &s.Condition, &approval, &s.Position,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("sqlite.WorkflowRepo.GetSteps: %w", err)
		}

		s.Action = json.RawMessage(action)
		s.RequiresApproval = approval == 1
		var parseErr error
		if s.CreatedAt, parseErr = parseSQLiteTime(createdAt, "sqlite.WorkflowRepo.GetSteps"); parseErr != nil {
			return nil, parseErr
		}
		if s.UpdatedAt, parseErr = parseSQLiteTime(updatedAt, "sqlite.WorkflowRepo.GetSteps"); parseErr != nil {
			return nil, parseErr
		}

		steps = append(steps, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.WorkflowRepo.GetSteps: %w", err)
	}
	return steps, nil
}

// UpdateStep modifies an existing workflow step.
func (r *WorkflowRepo) UpdateStep(ctx context.Context, step *repo.WorkflowStep) error {
	action := string(step.Action)
	if action == "" {
		action = "{}"
	}

	approval := 0
	if step.RequiresApproval {
		approval = 1
	}

	res, err := r.db.ExecContext(ctx,
		`UPDATE workflow_steps SET
		    name = ?, step_type = ?, action = ?, depends_on = ?,
		    condition = ?, requires_approval = ?, position = ?,
		    updated_at = datetime('now')
		 WHERE id = ?`,
		step.Name, step.StepType, action, step.DependsOn,
		step.Condition, approval, step.Position,
		step.ID,
	)
	if err != nil {
		return fmt.Errorf("sqlite.WorkflowRepo.UpdateStep: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.WorkflowRepo.UpdateStep: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("workflow step %q not found", step.ID)
	}

	return nil
}

// DeleteStep removes a workflow step by its ID.
func (r *WorkflowRepo) DeleteStep(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM workflow_steps WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("sqlite.WorkflowRepo.DeleteStep: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.WorkflowRepo.DeleteStep: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("workflow step %q not found", id)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Run management
// ---------------------------------------------------------------------------

// CreateRun inserts a new workflow run and returns its generated ID.
func (r *WorkflowRepo) CreateRun(ctx context.Context, run *repo.WorkflowRun) (string, error) {
	if run.ID == "" {
		run.ID = uuid.New().String()
	}

	runCtx := string(run.Context)
	if runCtx == "" {
		runCtx = "{}"
	}

	var startedAt, finishedAt sql.NullString
	if run.StartedAt != nil {
		startedAt = sql.NullString{String: run.StartedAt.Format(sqliteTimeFmt), Valid: true}
	}
	if run.FinishedAt != nil {
		finishedAt = sql.NullString{String: run.FinishedAt.Format(sqliteTimeFmt), Valid: true}
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO workflow_runs (id, workflow_id, status, started_at, finished_at, error, context)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.WorkflowID, run.Status,
		startedAt, finishedAt, run.Error, runCtx,
	)
	if err != nil {
		return "", fmt.Errorf("sqlite.WorkflowRepo.CreateRun: %w", err)
	}

	return run.ID, nil
}

// GetRun retrieves a workflow run by its ID.
func (r *WorkflowRepo) GetRun(ctx context.Context, id string) (*repo.WorkflowRun, error) {
	run := &repo.WorkflowRun{}
	var startedAt, finishedAt, errMsg sql.NullString
	var runCtx string
	var createdAt, updatedAt string

	err := r.db.QueryRowContext(ctx,
		`SELECT id, workflow_id, status, started_at, finished_at,
		        COALESCE(error, ''), context, created_at, updated_at
		 FROM workflow_runs WHERE id = ?`, id,
	).Scan(
		&run.ID, &run.WorkflowID, &run.Status,
		&startedAt, &finishedAt, &errMsg,
		&runCtx, &createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("workflow run %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite.WorkflowRepo.GetRun: %w", err)
	}

	if errMsg.Valid {
		run.Error = errMsg.String
	}
	if startedAt.Valid {
		t, parseErr := parseSQLiteTime(startedAt.String, "sqlite.WorkflowRepo.GetRun.startedAt")
		if parseErr != nil {
			return nil, parseErr
		}
		run.StartedAt = &t
	}
	if finishedAt.Valid {
		t, parseErr := parseSQLiteTime(finishedAt.String, "sqlite.WorkflowRepo.GetRun.finishedAt")
		if parseErr != nil {
			return nil, parseErr
		}
		run.FinishedAt = &t
	}
	run.Context = json.RawMessage(runCtx)
	if run.CreatedAt, err = parseSQLiteTime(createdAt, "sqlite.WorkflowRepo.GetRun"); err != nil {
		return nil, err
	}
	if run.UpdatedAt, err = parseSQLiteTime(updatedAt, "sqlite.WorkflowRepo.GetRun"); err != nil {
		return nil, err
	}

	return run, nil
}

// ListRuns returns all runs for a workflow, ordered by created_at descending.
func (r *WorkflowRepo) ListRuns(ctx context.Context, workflowID string) ([]*repo.WorkflowRun, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, workflow_id, status, started_at, finished_at,
		        COALESCE(error, ''), context, created_at, updated_at
		 FROM workflow_runs
		 WHERE workflow_id = ?
		 ORDER BY created_at DESC`, workflowID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.WorkflowRepo.ListRuns: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var runs []*repo.WorkflowRun
	for rows.Next() {
		run := &repo.WorkflowRun{}
		var startedAt, finishedAt, errMsg sql.NullString
		var runCtx string
		var createdAt, updatedAt string

		if err := rows.Scan(
			&run.ID, &run.WorkflowID, &run.Status,
			&startedAt, &finishedAt, &errMsg,
			&runCtx, &createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("sqlite.WorkflowRepo.ListRuns: %w", err)
		}

		if errMsg.Valid {
			run.Error = errMsg.String
		}
		var parseErr error
		if startedAt.Valid {
			t, pErr := parseSQLiteTime(startedAt.String, "sqlite.WorkflowRepo.ListRuns.startedAt")
			if pErr != nil {
				return nil, pErr
			}
			run.StartedAt = &t
		}
		if finishedAt.Valid {
			t, pErr := parseSQLiteTime(finishedAt.String, "sqlite.WorkflowRepo.ListRuns.finishedAt")
			if pErr != nil {
				return nil, pErr
			}
			run.FinishedAt = &t
		}
		run.Context = json.RawMessage(runCtx)
		if run.CreatedAt, parseErr = parseSQLiteTime(createdAt, "sqlite.WorkflowRepo.ListRuns"); parseErr != nil {
			return nil, parseErr
		}
		if run.UpdatedAt, parseErr = parseSQLiteTime(updatedAt, "sqlite.WorkflowRepo.ListRuns"); parseErr != nil {
			return nil, parseErr
		}

		runs = append(runs, run)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.WorkflowRepo.ListRuns: %w", err)
	}
	return runs, nil
}

// UpdateRunStatus sets the status and optional error message for a workflow run.
// It also sets finished_at when the status transitions to a terminal state.
func (r *WorkflowRepo) UpdateRunStatus(ctx context.Context, id string, status string, errMsg string) error {
	var q string
	var args []interface{}

	switch status {
	case "running":
		q = `UPDATE workflow_runs SET status = ?, started_at = datetime('now'), updated_at = datetime('now') WHERE id = ?`
		args = []interface{}{status, id}
	case "completed", "failed", "cancelled":
		q = `UPDATE workflow_runs SET status = ?, error = ?, finished_at = datetime('now'), updated_at = datetime('now') WHERE id = ?`
		args = []interface{}{status, errMsg, id}
	default:
		q = `UPDATE workflow_runs SET status = ?, error = ?, updated_at = datetime('now') WHERE id = ?`
		args = []interface{}{status, errMsg, id}
	}

	res, err := r.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("sqlite.WorkflowRepo.UpdateRunStatus: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.WorkflowRepo.UpdateRunStatus: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("workflow run %q not found", id)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Run step management
// ---------------------------------------------------------------------------

// CreateRunStep inserts a new workflow run step and returns its generated ID.
func (r *WorkflowRepo) CreateRunStep(ctx context.Context, rs *repo.WorkflowRunStep) (string, error) {
	if rs.ID == "" {
		rs.ID = uuid.New().String()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO workflow_run_steps (id, run_id, step_id, status)
		 VALUES (?, ?, ?, ?)`,
		rs.ID, rs.RunID, rs.StepID, rs.Status,
	)
	if err != nil {
		return "", fmt.Errorf("sqlite.WorkflowRepo.CreateRunStep: %w", err)
	}

	return rs.ID, nil
}

// GetRunSteps returns all run steps for a given run, ordered by created_at.
func (r *WorkflowRepo) GetRunSteps(ctx context.Context, runID string) ([]*repo.WorkflowRunStep, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, run_id, step_id, status, started_at, finished_at,
		        COALESCE(output, ''), COALESCE(error, ''),
		        created_at, updated_at
		 FROM workflow_run_steps
		 WHERE run_id = ?
		 ORDER BY created_at`, runID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.WorkflowRepo.GetRunSteps: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var steps []*repo.WorkflowRunStep
	for rows.Next() {
		rs := &repo.WorkflowRunStep{}
		var startedAt, finishedAt sql.NullString
		var createdAt, updatedAt string

		if err := rows.Scan(
			&rs.ID, &rs.RunID, &rs.StepID, &rs.Status,
			&startedAt, &finishedAt,
			&rs.Output, &rs.Error,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("sqlite.WorkflowRepo.GetRunSteps: %w", err)
		}

		var parseErr error
		if startedAt.Valid {
			t, pErr := parseSQLiteTime(startedAt.String, "sqlite.WorkflowRepo.GetRunSteps.startedAt")
			if pErr != nil {
				return nil, pErr
			}
			rs.StartedAt = &t
		}
		if finishedAt.Valid {
			t, pErr := parseSQLiteTime(finishedAt.String, "sqlite.WorkflowRepo.GetRunSteps.finishedAt")
			if pErr != nil {
				return nil, pErr
			}
			rs.FinishedAt = &t
		}
		if rs.CreatedAt, parseErr = parseSQLiteTime(createdAt, "sqlite.WorkflowRepo.GetRunSteps"); parseErr != nil {
			return nil, parseErr
		}
		if rs.UpdatedAt, parseErr = parseSQLiteTime(updatedAt, "sqlite.WorkflowRepo.GetRunSteps"); parseErr != nil {
			return nil, parseErr
		}

		steps = append(steps, rs)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.WorkflowRepo.GetRunSteps: %w", err)
	}
	return steps, nil
}

// UpdateRunStepStatus sets the status, output, and optional error for a run step.
// It also manages started_at and finished_at timestamps.
func (r *WorkflowRepo) UpdateRunStepStatus(ctx context.Context, id string, status string, output string, errMsg string) error {
	var q string
	var args []interface{}

	switch status {
	case "running":
		q = `UPDATE workflow_run_steps SET status = ?, started_at = datetime('now'), updated_at = datetime('now') WHERE id = ?`
		args = []interface{}{status, id}
	case "completed", "failed", "skipped":
		q = `UPDATE workflow_run_steps SET status = ?, output = ?, error = ?, finished_at = datetime('now'), updated_at = datetime('now') WHERE id = ?`
		args = []interface{}{status, output, errMsg, id}
	default:
		// For "pending", "waiting_approval", etc.
		q = `UPDATE workflow_run_steps SET status = ?, output = ?, error = ?, updated_at = datetime('now') WHERE id = ?`
		args = []interface{}{status, output, errMsg, id}
	}

	res, err := r.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("sqlite.WorkflowRepo.UpdateRunStepStatus: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.WorkflowRepo.UpdateRunStepStatus: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("workflow run step %q not found", id)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Trigger management
// ---------------------------------------------------------------------------

// CreateTrigger inserts a new workflow trigger and returns its generated ID.
func (r *WorkflowRepo) CreateTrigger(ctx context.Context, trigger *repo.WorkflowTrigger) (string, error) {
	if trigger.ID == "" {
		trigger.ID = uuid.New().String()
	}

	config := string(trigger.Config)
	if config == "" {
		config = "{}"
	}

	enabled := 0
	if trigger.Enabled {
		enabled = 1
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO workflow_triggers (id, workflow_id, trigger_type, config, enabled)
		 VALUES (?, ?, ?, ?, ?)`,
		trigger.ID, trigger.WorkflowID, trigger.TriggerType, config, enabled,
	)
	if err != nil {
		return "", fmt.Errorf("sqlite.WorkflowRepo.CreateTrigger: %w", err)
	}

	return trigger.ID, nil
}

// ListTriggers returns all triggers for a workflow.
func (r *WorkflowRepo) ListTriggers(ctx context.Context, workflowID string) ([]*repo.WorkflowTrigger, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, workflow_id, trigger_type, config, enabled, created_at, updated_at
		 FROM workflow_triggers
		 WHERE workflow_id = ?
		 ORDER BY created_at`, workflowID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.WorkflowRepo.ListTriggers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var triggers []*repo.WorkflowTrigger
	for rows.Next() {
		tr := &repo.WorkflowTrigger{}
		var config string
		var enabled int
		var createdAt, updatedAt string

		if err := rows.Scan(
			&tr.ID, &tr.WorkflowID, &tr.TriggerType, &config,
			&enabled, &createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("sqlite.WorkflowRepo.ListTriggers: %w", err)
		}

		tr.Config = json.RawMessage(config)
		tr.Enabled = enabled == 1
		var parseErr error
		if tr.CreatedAt, parseErr = parseSQLiteTime(createdAt, "sqlite.WorkflowRepo.ListTriggers"); parseErr != nil {
			return nil, parseErr
		}
		if tr.UpdatedAt, parseErr = parseSQLiteTime(updatedAt, "sqlite.WorkflowRepo.ListTriggers"); parseErr != nil {
			return nil, parseErr
		}

		triggers = append(triggers, tr)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.WorkflowRepo.ListTriggers: %w", err)
	}
	return triggers, nil
}

// DeleteTrigger removes a workflow trigger by its ID.
func (r *WorkflowRepo) DeleteTrigger(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM workflow_triggers WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("sqlite.WorkflowRepo.DeleteTrigger: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.WorkflowRepo.DeleteTrigger: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("workflow trigger %q not found", id)
	}

	return nil
}
