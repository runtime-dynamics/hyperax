package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/repo"
)

// WorkflowRepo implements repo.WorkflowRepo for MySQL.
type WorkflowRepo struct {
	db *sql.DB
}

// ---------------------------------------------------------------------------
// Workflow CRUD
// ---------------------------------------------------------------------------

// CreateWorkflow inserts a new workflow definition and returns its generated ID.
func (r *WorkflowRepo) CreateWorkflow(ctx context.Context, wf *repo.Workflow) (string, error) {
	if wf.ID == "" {
		wf.ID = uuid.New().String()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO workflows (id, name, description, enabled)
		 VALUES (?, ?, ?, ?)`,
		wf.ID, wf.Name, wf.Description, wf.Enabled,
	)
	if err != nil {
		return "", fmt.Errorf("mysql.WorkflowRepo.CreateWorkflow: %w", err)
	}
	return wf.ID, nil
}

// GetWorkflow retrieves a workflow definition by its ID.
func (r *WorkflowRepo) GetWorkflow(ctx context.Context, id string) (*repo.Workflow, error) {
	wf := &repo.Workflow{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, description, enabled, created_at, updated_at
		 FROM workflows WHERE id = ?`, id,
	).Scan(&wf.ID, &wf.Name, &wf.Description, &wf.Enabled, &wf.CreatedAt, &wf.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("workflow %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("mysql.WorkflowRepo.GetWorkflow: %w", err)
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
		return nil, fmt.Errorf("mysql.WorkflowRepo.ListWorkflows: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var workflows []*repo.Workflow
	for rows.Next() {
		wf := &repo.Workflow{}
		if err := rows.Scan(&wf.ID, &wf.Name, &wf.Description, &wf.Enabled, &wf.CreatedAt, &wf.UpdatedAt); err != nil {
			return nil, fmt.Errorf("mysql.WorkflowRepo.ListWorkflows: %w", err)
		}
		workflows = append(workflows, wf)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql.WorkflowRepo.ListWorkflows: %w", err)
	}
	return workflows, nil
}

// UpdateWorkflow modifies an existing workflow definition.
func (r *WorkflowRepo) UpdateWorkflow(ctx context.Context, wf *repo.Workflow) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE workflows SET
		    name = ?, description = ?, enabled = ?,
		    updated_at = NOW()
		 WHERE id = ?`,
		wf.Name, wf.Description, wf.Enabled, wf.ID,
	)
	if err != nil {
		return fmt.Errorf("mysql.WorkflowRepo.UpdateWorkflow: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mysql.WorkflowRepo.UpdateWorkflow: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("workflow %q not found", wf.ID)
	}
	return nil
}

// DeleteWorkflow removes a workflow definition by its ID.
func (r *WorkflowRepo) DeleteWorkflow(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM workflows WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("mysql.WorkflowRepo.DeleteWorkflow: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mysql.WorkflowRepo.DeleteWorkflow: %w", err)
	}
	if affected == 0 {
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

	_, err := r.db.ExecContext(ctx,
		"INSERT INTO workflow_steps (id, workflow_id, name, step_type, action, depends_on, `condition`, requires_approval, position)"+
			" VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		step.ID, step.WorkflowID, step.Name, step.StepType,
		action, step.DependsOn, step.Condition, step.RequiresApproval, step.Position,
	)
	if err != nil {
		return "", fmt.Errorf("mysql.WorkflowRepo.CreateStep: %w", err)
	}
	return step.ID, nil
}

// GetSteps returns all steps for a workflow, ordered by position.
func (r *WorkflowRepo) GetSteps(ctx context.Context, workflowID string) ([]*repo.WorkflowStep, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT id, workflow_id, name, step_type, action, depends_on, `condition`,"+
			" requires_approval, position, created_at, updated_at"+
			" FROM workflow_steps"+
			" WHERE workflow_id = ?"+
			" ORDER BY position", workflowID,
	)
	if err != nil {
		return nil, fmt.Errorf("mysql.WorkflowRepo.GetSteps: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var steps []*repo.WorkflowStep
	for rows.Next() {
		s := &repo.WorkflowStep{}
		var action string

		if err := rows.Scan(
			&s.ID, &s.WorkflowID, &s.Name, &s.StepType, &action,
			&s.DependsOn, &s.Condition, &s.RequiresApproval, &s.Position,
			&s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("mysql.WorkflowRepo.GetSteps: %w", err)
		}

		s.Action = json.RawMessage(action)
		steps = append(steps, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql.WorkflowRepo.GetSteps: %w", err)
	}
	return steps, nil
}

// UpdateStep modifies an existing workflow step.
func (r *WorkflowRepo) UpdateStep(ctx context.Context, step *repo.WorkflowStep) error {
	action := string(step.Action)
	if action == "" {
		action = "{}"
	}

	res, err := r.db.ExecContext(ctx,
		"UPDATE workflow_steps SET"+
			" name = ?, step_type = ?, action = ?, depends_on = ?,"+
			" `condition` = ?, requires_approval = ?, position = ?,"+
			" updated_at = NOW()"+
			" WHERE id = ?",
		step.Name, step.StepType, action, step.DependsOn,
		step.Condition, step.RequiresApproval, step.Position,
		step.ID,
	)
	if err != nil {
		return fmt.Errorf("mysql.WorkflowRepo.UpdateStep: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mysql.WorkflowRepo.UpdateStep: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("workflow step %q not found", step.ID)
	}
	return nil
}

// DeleteStep removes a workflow step by its ID.
func (r *WorkflowRepo) DeleteStep(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM workflow_steps WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("mysql.WorkflowRepo.DeleteStep: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mysql.WorkflowRepo.DeleteStep: %w", err)
	}
	if affected == 0 {
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

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO workflow_runs (id, workflow_id, status, started_at, finished_at, error, context)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.WorkflowID, run.Status,
		run.StartedAt, run.FinishedAt, run.Error, runCtx,
	)
	if err != nil {
		return "", fmt.Errorf("mysql.WorkflowRepo.CreateRun: %w", err)
	}
	return run.ID, nil
}

// GetRun retrieves a workflow run by its ID.
func (r *WorkflowRepo) GetRun(ctx context.Context, id string) (*repo.WorkflowRun, error) {
	run := &repo.WorkflowRun{}
	var startedAt, finishedAt sql.NullTime
	var errMsg sql.NullString
	var runCtx string

	err := r.db.QueryRowContext(ctx,
		`SELECT id, workflow_id, status, started_at, finished_at,
		        COALESCE(error, ''), context, created_at, updated_at
		 FROM workflow_runs WHERE id = ?`, id,
	).Scan(
		&run.ID, &run.WorkflowID, &run.Status,
		&startedAt, &finishedAt, &errMsg,
		&runCtx, &run.CreatedAt, &run.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("workflow run %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("mysql.WorkflowRepo.GetRun: %w", err)
	}

	if errMsg.Valid {
		run.Error = errMsg.String
	}
	if startedAt.Valid {
		run.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		run.FinishedAt = &finishedAt.Time
	}
	run.Context = json.RawMessage(runCtx)
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
		return nil, fmt.Errorf("mysql.WorkflowRepo.ListRuns: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var runs []*repo.WorkflowRun
	for rows.Next() {
		run := &repo.WorkflowRun{}
		var startedAt, finishedAt sql.NullTime
		var errMsg sql.NullString
		var runCtx string

		if err := rows.Scan(
			&run.ID, &run.WorkflowID, &run.Status,
			&startedAt, &finishedAt, &errMsg,
			&runCtx, &run.CreatedAt, &run.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("mysql.WorkflowRepo.ListRuns: %w", err)
		}

		if errMsg.Valid {
			run.Error = errMsg.String
		}
		if startedAt.Valid {
			run.StartedAt = &startedAt.Time
		}
		if finishedAt.Valid {
			run.FinishedAt = &finishedAt.Time
		}
		run.Context = json.RawMessage(runCtx)
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql.WorkflowRepo.ListRuns: %w", err)
	}
	return runs, nil
}

// UpdateRunStatus sets the status and optional error message for a workflow run.
func (r *WorkflowRepo) UpdateRunStatus(ctx context.Context, id string, status string, errMsg string) error {
	var q string
	var args []interface{}

	switch status {
	case "running":
		q = `UPDATE workflow_runs SET status = ?, started_at = NOW(), updated_at = NOW() WHERE id = ?`
		args = []interface{}{status, id}
	case "completed", "failed", "cancelled":
		q = `UPDATE workflow_runs SET status = ?, error = ?, finished_at = NOW(), updated_at = NOW() WHERE id = ?`
		args = []interface{}{status, errMsg, id}
	default:
		q = `UPDATE workflow_runs SET status = ?, error = ?, updated_at = NOW() WHERE id = ?`
		args = []interface{}{status, errMsg, id}
	}

	res, err := r.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("mysql.WorkflowRepo.UpdateRunStatus: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mysql.WorkflowRepo.UpdateRunStatus: %w", err)
	}
	if affected == 0 {
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
		return "", fmt.Errorf("mysql.WorkflowRepo.CreateRunStep: %w", err)
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
		return nil, fmt.Errorf("mysql.WorkflowRepo.GetRunSteps: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var steps []*repo.WorkflowRunStep
	for rows.Next() {
		rs := &repo.WorkflowRunStep{}
		var startedAt, finishedAt sql.NullTime

		if err := rows.Scan(
			&rs.ID, &rs.RunID, &rs.StepID, &rs.Status,
			&startedAt, &finishedAt,
			&rs.Output, &rs.Error,
			&rs.CreatedAt, &rs.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("mysql.WorkflowRepo.GetRunSteps: %w", err)
		}

		if startedAt.Valid {
			rs.StartedAt = &startedAt.Time
		}
		if finishedAt.Valid {
			rs.FinishedAt = &finishedAt.Time
		}
		steps = append(steps, rs)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql.WorkflowRepo.GetRunSteps: %w", err)
	}
	return steps, nil
}

// UpdateRunStepStatus sets the status, output, and optional error for a run step.
func (r *WorkflowRepo) UpdateRunStepStatus(ctx context.Context, id string, status string, output string, errMsg string) error {
	var q string
	var args []interface{}

	switch status {
	case "running":
		q = `UPDATE workflow_run_steps SET status = ?, started_at = NOW(), updated_at = NOW() WHERE id = ?`
		args = []interface{}{status, id}
	case "completed", "failed", "skipped":
		q = `UPDATE workflow_run_steps SET status = ?, output = ?, error = ?, finished_at = NOW(), updated_at = NOW() WHERE id = ?`
		args = []interface{}{status, output, errMsg, id}
	default:
		q = `UPDATE workflow_run_steps SET status = ?, output = ?, error = ?, updated_at = NOW() WHERE id = ?`
		args = []interface{}{status, output, errMsg, id}
	}

	res, err := r.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("mysql.WorkflowRepo.UpdateRunStepStatus: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mysql.WorkflowRepo.UpdateRunStepStatus: %w", err)
	}
	if affected == 0 {
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

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO workflow_triggers (id, workflow_id, trigger_type, config, enabled)
		 VALUES (?, ?, ?, ?, ?)`,
		trigger.ID, trigger.WorkflowID, trigger.TriggerType, config, trigger.Enabled,
	)
	if err != nil {
		return "", fmt.Errorf("mysql.WorkflowRepo.CreateTrigger: %w", err)
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
		return nil, fmt.Errorf("mysql.WorkflowRepo.ListTriggers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var triggers []*repo.WorkflowTrigger
	for rows.Next() {
		tr := &repo.WorkflowTrigger{}
		var config string

		if err := rows.Scan(
			&tr.ID, &tr.WorkflowID, &tr.TriggerType, &config,
			&tr.Enabled, &tr.CreatedAt, &tr.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("mysql.WorkflowRepo.ListTriggers: %w", err)
		}

		tr.Config = json.RawMessage(config)
		triggers = append(triggers, tr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql.WorkflowRepo.ListTriggers: %w", err)
	}
	return triggers, nil
}

// DeleteTrigger removes a workflow trigger by its ID.
func (r *WorkflowRepo) DeleteTrigger(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM workflow_triggers WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("mysql.WorkflowRepo.DeleteTrigger: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mysql.WorkflowRepo.DeleteTrigger: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("workflow trigger %q not found", id)
	}
	return nil
}
