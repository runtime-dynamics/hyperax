package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/repo"
)

// ProjectRepo implements repo.ProjectRepo for PostgreSQL.
type ProjectRepo struct {
	db *sql.DB
}

// CreateProjectPlan inserts a new project plan and returns its generated ID.
func (r *ProjectRepo) CreateProjectPlan(ctx context.Context, plan *repo.ProjectPlan) (string, error) {
	if plan.ID == "" {
		plan.ID = uuid.New().String()
	}
	if plan.Status == "" {
		plan.Status = "pending"
	}
	if plan.Priority == "" {
		plan.Priority = "medium"
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO project_plans (id, name, description, workspace_name, status, priority)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		plan.ID, plan.Name, plan.Description, plan.WorkspaceName, plan.Status, plan.Priority,
	)
	if err != nil {
		return "", fmt.Errorf("postgres.ProjectRepo.CreateProjectPlan: %w", err)
	}
	return plan.ID, nil
}

// GetProjectPlan retrieves a single project plan by ID.
func (r *ProjectRepo) GetProjectPlan(ctx context.Context, id string) (*repo.ProjectPlan, error) {
	plan := &repo.ProjectPlan{}
	var description sql.NullString

	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, COALESCE(description, ''), workspace_name, status, priority, created_at, updated_at
		 FROM project_plans WHERE id = $1`, id,
	).Scan(&plan.ID, &plan.Name, &description, &plan.WorkspaceName, &plan.Status, &plan.Priority, &plan.CreatedAt, &plan.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("project plan %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres.ProjectRepo.GetProjectPlan: %w", err)
	}
	plan.Description = description.String
	return plan, nil
}

// ListProjectPlans returns all project plans for a given workspace.
func (r *ProjectRepo) ListProjectPlans(ctx context.Context, workspaceName string) ([]*repo.ProjectPlan, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, COALESCE(description, ''), workspace_name, status, priority, created_at, updated_at
		 FROM project_plans WHERE workspace_name = $1 ORDER BY created_at`, workspaceName,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.ProjectRepo.ListProjectPlans: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var plans []*repo.ProjectPlan
	for rows.Next() {
		plan := &repo.ProjectPlan{}
		var description sql.NullString
		if err := rows.Scan(&plan.ID, &plan.Name, &description, &plan.WorkspaceName, &plan.Status, &plan.Priority, &plan.CreatedAt, &plan.UpdatedAt); err != nil {
			return nil, fmt.Errorf("postgres.ProjectRepo.ListProjectPlans: %w", err)
		}
		plan.Description = description.String
		plans = append(plans, plan)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.ProjectRepo.ListProjectPlans: %w", err)
	}
	return plans, nil
}

// DeleteProjectPlan removes a project plan and all its milestones/tasks.
// Uses explicit cascading deletes as a safety net alongside DB CASCADE constraints.
func (r *ProjectRepo) DeleteProjectPlan(ctx context.Context, id string) error {
	// Explicit cascade: tasks → milestones → project, in case DB CASCADE doesn't fire.
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM tasks WHERE milestone_id IN (SELECT id FROM milestones WHERE project_id = $1)`, id); err != nil {
		return fmt.Errorf("postgres.ProjectRepo.DeleteProjectPlan(tasks): %w", err)
	}
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM milestones WHERE project_id = $1`, id); err != nil {
		return fmt.Errorf("postgres.ProjectRepo.DeleteProjectPlan(milestones): %w", err)
	}
	// Delete orphaned comments referencing any of these entities.
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM comments WHERE entity_type = 'project' AND entity_id = $1`, id); err != nil {
		return fmt.Errorf("postgres.ProjectRepo.DeleteProjectPlan(comments): %w", err)
	}

	result, err := r.db.ExecContext(ctx, `DELETE FROM project_plans WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("postgres.ProjectRepo.DeleteProjectPlan: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.ProjectRepo.DeleteProjectPlan: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("project plan %q not found", id)
	}
	return nil
}

// UpdateProjectStatus changes the status of a project plan.
func (r *ProjectRepo) UpdateProjectStatus(ctx context.Context, id string, status string) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE project_plans SET status = $1, updated_at = NOW() WHERE id = $2`, status, id,
	)
	if err != nil {
		return fmt.Errorf("postgres.ProjectRepo.UpdateProjectStatus: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.ProjectRepo.UpdateProjectStatus: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("project plan %q not found", id)
	}
	return nil
}

// MoveProjectWorkspace changes the workspace_name of a project plan.
func (r *ProjectRepo) MoveProjectWorkspace(ctx context.Context, id string, targetWorkspace string) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE project_plans SET workspace_name = $1, updated_at = NOW() WHERE id = $2`, targetWorkspace, id,
	)
	if err != nil {
		return fmt.Errorf("postgres.ProjectRepo.MoveProjectWorkspace: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.ProjectRepo.MoveProjectWorkspace: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("project plan %q not found", id)
	}
	return nil
}

// CreateMilestone inserts a new milestone and returns its generated ID.
func (r *ProjectRepo) CreateMilestone(ctx context.Context, milestone *repo.Milestone) (string, error) {
	if milestone.ID == "" {
		milestone.ID = uuid.New().String()
	}
	if milestone.Status == "" {
		milestone.Status = "pending"
	}
	if milestone.Priority == "" {
		milestone.Priority = "medium"
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO milestones (id, project_id, name, description, status, priority, due_date, order_index, assignee_agent_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		milestone.ID, milestone.ProjectID, milestone.Name, milestone.Description,
		milestone.Status, milestone.Priority, milestone.DueDate, milestone.OrderIndex, milestone.AssigneeAgentID,
	)
	if err != nil {
		return "", fmt.Errorf("postgres.ProjectRepo.CreateMilestone: %w", err)
	}
	return milestone.ID, nil
}

// GetMilestone retrieves a single milestone by ID.
func (r *ProjectRepo) GetMilestone(ctx context.Context, id string) (*repo.Milestone, error) {
	ms := &repo.Milestone{}
	var description sql.NullString
	var dueDate sql.NullTime
	var assigneeID sql.NullString

	err := r.db.QueryRowContext(ctx,
		`SELECT id, project_id, name, COALESCE(description, ''), status, priority, due_date, order_index, COALESCE(assignee_agent_id, '')
		 FROM milestones WHERE id = $1`, id,
	).Scan(&ms.ID, &ms.ProjectID, &ms.Name, &description, &ms.Status, &ms.Priority, &dueDate, &ms.OrderIndex, &assigneeID)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("milestone %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres.ProjectRepo.GetMilestone: %w", err)
	}
	ms.Description = description.String
	ms.AssigneeAgentID = assigneeID.String
	if dueDate.Valid {
		t := dueDate.Time
		ms.DueDate = &t
	}
	return ms, nil
}

// ListMilestones returns all milestones for a given project, ordered by order_index.
func (r *ProjectRepo) ListMilestones(ctx context.Context, projectID string) ([]*repo.Milestone, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, project_id, name, COALESCE(description, ''), status, priority, due_date, order_index, COALESCE(assignee_agent_id, '')
		 FROM milestones WHERE project_id = $1 ORDER BY order_index`, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.ProjectRepo.ListMilestones: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var milestones []*repo.Milestone
	for rows.Next() {
		ms := &repo.Milestone{}
		var description sql.NullString
		var dueDate sql.NullTime
		var assigneeID sql.NullString
		if err := rows.Scan(&ms.ID, &ms.ProjectID, &ms.Name, &description, &ms.Status, &ms.Priority, &dueDate, &ms.OrderIndex, &assigneeID); err != nil {
			return nil, fmt.Errorf("postgres.ProjectRepo.ListMilestones: %w", err)
		}
		ms.Description = description.String
		ms.AssigneeAgentID = assigneeID.String
		if dueDate.Valid {
			t := dueDate.Time
			ms.DueDate = &t
		}
		milestones = append(milestones, ms)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.ProjectRepo.ListMilestones: %w", err)
	}
	return milestones, nil
}

// AssignMilestone sets the assignee_agent_id on a milestone.
func (r *ProjectRepo) AssignMilestone(ctx context.Context, milestoneID string, agentID string) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE milestones SET assignee_agent_id = $1 WHERE id = $2`, agentID, milestoneID,
	)
	if err != nil {
		return fmt.Errorf("postgres.ProjectRepo.AssignMilestone: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.ProjectRepo.AssignMilestone: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("milestone %q not found", milestoneID)
	}
	return nil
}

// UnassignMilestone clears the assignee_agent_id on a milestone.
func (r *ProjectRepo) UnassignMilestone(ctx context.Context, milestoneID string) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE milestones SET assignee_agent_id = '' WHERE id = $1`, milestoneID,
	)
	if err != nil {
		return fmt.Errorf("postgres.ProjectRepo.UnassignMilestone: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.ProjectRepo.UnassignMilestone: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("milestone %q not found", milestoneID)
	}
	return nil
}

// CreateTask inserts a new task and returns its generated ID.
func (r *ProjectRepo) CreateTask(ctx context.Context, task *repo.Task) (string, error) {
	if task.ID == "" {
		task.ID = uuid.New().String()
	}
	if task.Status == "" {
		task.Status = "pending"
	}
	if task.Priority == "" {
		task.Priority = "medium"
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO tasks (id, milestone_id, name, description, status, priority, order_index, assignee_agent_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		task.ID, task.MilestoneID, task.Name, task.Description,
		task.Status, task.Priority, task.OrderIndex, task.AssigneeAgentID,
	)
	if err != nil {
		return "", fmt.Errorf("postgres.ProjectRepo.CreateTask: %w", err)
	}
	return task.ID, nil
}

// GetTask retrieves a single task by ID.
func (r *ProjectRepo) GetTask(ctx context.Context, id string) (*repo.Task, error) {
	task := &repo.Task{}
	var description, assigneeID sql.NullString

	err := r.db.QueryRowContext(ctx,
		`SELECT id, milestone_id, name, COALESCE(description, ''), status, priority, order_index, COALESCE(assignee_agent_id, ''), created_at, updated_at
		 FROM tasks WHERE id = $1`, id,
	).Scan(&task.ID, &task.MilestoneID, &task.Name, &description, &task.Status, &task.Priority, &task.OrderIndex, &assigneeID, &task.CreatedAt, &task.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("task %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres.ProjectRepo.GetTask: %w", err)
	}
	task.Description = description.String
	task.AssigneeAgentID = assigneeID.String
	return task, nil
}

// UpdateTaskStatus updates the status and updated_at timestamp of a task.
func (r *ProjectRepo) UpdateTaskStatus(ctx context.Context, id string, status string) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE tasks SET status = $1, updated_at = NOW() WHERE id = $2`, status, id,
	)
	if err != nil {
		return fmt.Errorf("postgres.ProjectRepo.UpdateTaskStatus: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.ProjectRepo.UpdateTaskStatus: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("task %q not found", id)
	}
	return nil
}

// ListTasks returns all tasks for a given milestone, ordered by order_index.
func (r *ProjectRepo) ListTasks(ctx context.Context, milestoneID string) ([]*repo.Task, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, milestone_id, name, COALESCE(description, ''), status, priority, order_index, COALESCE(assignee_agent_id, ''), created_at, updated_at
		 FROM tasks WHERE milestone_id = $1 ORDER BY order_index`, milestoneID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.ProjectRepo.ListTasks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanPgTasks(rows)
}

// ListTasksByWorkspace returns tasks across all milestones/projects in a workspace.
func (r *ProjectRepo) ListTasksByWorkspace(ctx context.Context, workspaceName string, status string) ([]*repo.Task, error) {
	query := `SELECT t.id, t.milestone_id, p.id, t.name, COALESCE(t.description, ''), t.status, t.priority, t.order_index, COALESCE(t.assignee_agent_id, ''), t.created_at, t.updated_at
		 FROM tasks t
		 INNER JOIN milestones m ON t.milestone_id = m.id
		 INNER JOIN project_plans p ON m.project_id = p.id
		 WHERE p.workspace_name = $1
		 AND p.status NOT IN ('completed', 'archived')`

	args := []any{workspaceName}
	if status != "" {
		query += " AND t.status = $2"
		args = append(args, status)
	}
	query += " ORDER BY t.created_at"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres.ProjectRepo.ListTasksByWorkspace: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tasks []*repo.Task
	for rows.Next() {
		task := &repo.Task{}
		var description, assigneeID sql.NullString
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&task.ID, &task.MilestoneID, &task.ProjectID, &task.Name, &description, &task.Status, &task.Priority, &task.OrderIndex, &assigneeID, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("postgres.ProjectRepo.ListTasksByWorkspace: %w", err)
		}
		task.Description = description.String
		task.AssigneeAgentID = assigneeID.String
		task.CreatedAt = createdAt
		task.UpdatedAt = updatedAt
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.ProjectRepo.ListTasksByWorkspace: %w", err)
	}
	return tasks, nil
}

// AssignTask sets the assignee_agent_id on a task.
func (r *ProjectRepo) AssignTask(ctx context.Context, taskID string, agentID string) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE tasks SET assignee_agent_id = $1, updated_at = NOW() WHERE id = $2`, agentID, taskID,
	)
	if err != nil {
		return fmt.Errorf("postgres.ProjectRepo.AssignTask: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.ProjectRepo.AssignTask: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("task %q not found", taskID)
	}
	return nil
}

// UnassignTask clears the assignee_agent_id on a task.
func (r *ProjectRepo) UnassignTask(ctx context.Context, taskID string) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE tasks SET assignee_agent_id = '', updated_at = NOW() WHERE id = $1`, taskID,
	)
	if err != nil {
		return fmt.Errorf("postgres.ProjectRepo.UnassignTask: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.ProjectRepo.UnassignTask: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("task %q not found", taskID)
	}
	return nil
}

// DeleteMilestone removes a milestone and all its tasks (via CASCADE).
func (r *ProjectRepo) DeleteMilestone(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM milestones WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("postgres.ProjectRepo.DeleteMilestone: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.ProjectRepo.DeleteMilestone: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("milestone %q not found", id)
	}
	return nil
}

// DeleteTask removes a single task.
func (r *ProjectRepo) DeleteTask(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("postgres.ProjectRepo.DeleteTask: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.ProjectRepo.DeleteTask: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("task %q not found", id)
	}
	return nil
}

// PurgeOrphans removes milestones with no parent project and tasks with no
// parent milestone. Returns the total number of rows removed.
func (r *ProjectRepo) PurgeOrphans(ctx context.Context) (int64, error) {
	var total int64

	res, err := r.db.ExecContext(ctx,
		`DELETE FROM tasks WHERE milestone_id NOT IN (SELECT id FROM milestones)`)
	if err != nil {
		return 0, fmt.Errorf("postgres.ProjectRepo.PurgeOrphans(tasks): %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("postgres.ProjectRepo.PurgeOrphans(tasks): %w", err)
	}
	total += n

	res, err = r.db.ExecContext(ctx,
		`DELETE FROM milestones WHERE project_id NOT IN (SELECT id FROM project_plans)`)
	if err != nil {
		return total, fmt.Errorf("postgres.ProjectRepo.PurgeOrphans(milestones): %w", err)
	}
	n, err = res.RowsAffected()
	if err != nil {
		return total, fmt.Errorf("postgres.ProjectRepo.PurgeOrphans(milestones): %w", err)
	}
	total += n

	res, err = r.db.ExecContext(ctx,
		`DELETE FROM comments WHERE
			(entity_type = 'project' AND entity_id NOT IN (SELECT id FROM project_plans)) OR
			(entity_type = 'milestone' AND entity_id NOT IN (SELECT id FROM milestones)) OR
			(entity_type = 'task' AND entity_id NOT IN (SELECT id FROM tasks))`)
	if err != nil {
		return total, fmt.Errorf("postgres.ProjectRepo.PurgeOrphans(comments): %w", err)
	}
	n, err = res.RowsAffected()
	if err != nil {
		return total, fmt.Errorf("postgres.ProjectRepo.PurgeOrphans(comments): %w", err)
	}
	total += n

	return total, nil
}

// ReconcileCompletionStatus auto-completes milestones where all tasks are
// completed and projects where all milestones are completed.
func (r *ProjectRepo) ReconcileCompletionStatus(ctx context.Context) (int, int, error) {
	msResult, err := r.db.ExecContext(ctx, `
		UPDATE milestones SET status = 'completed'
		WHERE status != 'completed'
		  AND (SELECT COUNT(*) FROM tasks WHERE milestone_id = milestones.id) > 0
		  AND (SELECT COUNT(*) FROM tasks WHERE milestone_id = milestones.id AND status != 'completed') = 0`)
	if err != nil {
		return 0, 0, fmt.Errorf("postgres.ProjectRepo.ReconcileCompletionStatus(milestones): %w", err)
	}
	msCount, err := msResult.RowsAffected()
	if err != nil {
		return 0, 0, fmt.Errorf("postgres.ProjectRepo.ReconcileCompletionStatus(milestones): %w", err)
	}

	pjResult, err := r.db.ExecContext(ctx, `
		UPDATE project_plans SET status = 'completed', updated_at = NOW()
		WHERE status != 'completed'
		  AND (SELECT COUNT(*) FROM milestones WHERE project_id = project_plans.id) > 0
		  AND (SELECT COUNT(*) FROM milestones WHERE project_id = project_plans.id AND status != 'completed') = 0`)
	if err != nil {
		return int(msCount), 0, fmt.Errorf("postgres.ProjectRepo.ReconcileCompletionStatus(projects): %w", err)
	}
	pjCount, err := pjResult.RowsAffected()
	if err != nil {
		return int(msCount), 0, fmt.Errorf("postgres.ProjectRepo.ReconcileCompletionStatus(projects): %w", err)
	}

	return int(msCount), int(pjCount), nil
}

// ListTasksByAgent returns tasks assigned to a persona, optionally filtered.
func (r *ProjectRepo) ListTasksByAgent(ctx context.Context, agentID string, status string, projectID string) ([]*repo.Task, error) {
	query := `SELECT t.id, t.milestone_id, t.name, COALESCE(t.description, ''), t.status, t.priority, t.order_index, COALESCE(t.assignee_agent_id, ''), t.created_at, t.updated_at
		 FROM tasks t
		 INNER JOIN milestones m ON t.milestone_id = m.id
		 INNER JOIN project_plans p ON m.project_id = p.id
		 WHERE t.assignee_agent_id = $1
		 AND p.status NOT IN ('completed', 'archived')`

	args := []any{agentID}
	paramIdx := 2
	if status != "" {
		query += fmt.Sprintf(" AND t.status = $%d", paramIdx)
		args = append(args, status)
		paramIdx++
	}
	if projectID != "" {
		query += fmt.Sprintf(" AND p.id = $%d", paramIdx)
		args = append(args, projectID)
	}
	query += " ORDER BY t.created_at"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres.ProjectRepo.ListTasksByAgent: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanPgTasks(rows)
}

// GetNextTask returns the single highest-priority, oldest pending/in-progress
// task for the given agent. When agentID is empty it returns the next unassigned
// pending task (for CoS triage). Returns (nil, nil) when no tasks match.
func (r *ProjectRepo) GetNextTask(ctx context.Context, agentID string) (*repo.Task, error) {
	var query string
	var args []any

	if agentID != "" {
		query = `SELECT t.id, t.milestone_id, p.id, t.name, COALESCE(t.description, ''), t.status, t.priority, t.order_index, COALESCE(t.assignee_agent_id, ''), t.created_at, t.updated_at
			FROM tasks t
			INNER JOIN milestones m ON t.milestone_id = m.id
			INNER JOIN project_plans p ON m.project_id = p.id
			WHERE t.assignee_agent_id = $1 AND t.status IN ('pending', 'in-progress')
			AND p.status NOT IN ('completed', 'archived')
			ORDER BY CASE t.priority
				WHEN 'critical' THEN 0
				WHEN 'high'     THEN 1
				WHEN 'medium'   THEN 2
				WHEN 'low'      THEN 3
				ELSE 4
			END, t.created_at ASC
			LIMIT 1`
		args = []any{agentID}
	} else {
		query = `SELECT t.id, t.milestone_id, p.id, t.name, COALESCE(t.description, ''), t.status, t.priority, t.order_index, COALESCE(t.assignee_agent_id, ''), t.created_at, t.updated_at
			FROM tasks t
			INNER JOIN milestones m ON t.milestone_id = m.id
			INNER JOIN project_plans p ON m.project_id = p.id
			WHERE (t.assignee_agent_id IS NULL OR t.assignee_agent_id = '') AND t.status = 'pending'
			AND p.status NOT IN ('completed', 'archived')
			ORDER BY CASE t.priority
				WHEN 'critical' THEN 0
				WHEN 'high'     THEN 1
				WHEN 'medium'   THEN 2
				WHEN 'low'      THEN 3
				ELSE 4
			END, t.created_at ASC
			LIMIT 1`
	}

	task := &repo.Task{}
	var description, assigneeID sql.NullString

	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&task.ID, &task.MilestoneID, &task.ProjectID, &task.Name, &description,
		&task.Status, &task.Priority, &task.OrderIndex, &assigneeID,
		&task.CreatedAt, &task.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("postgres.ProjectRepo.GetNextTask: %w", err)
	}

	task.Description = description.String
	task.AssigneeAgentID = assigneeID.String
	return task, nil
}

// AddComment inserts a new comment and returns its generated ID.
func (r *ProjectRepo) AddComment(ctx context.Context, comment *repo.Comment) (string, error) {
	if comment.ID == "" {
		comment.ID = uuid.New().String()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO comments (id, entity_type, entity_id, content, author)
		 VALUES ($1, $2, $3, $4, $5)`,
		comment.ID, comment.EntityType, comment.EntityID, comment.Content, comment.Author,
	)
	if err != nil {
		return "", fmt.Errorf("postgres.ProjectRepo.AddComment: %w", err)
	}
	return comment.ID, nil
}

// ListComments returns all comments for a given entity.
func (r *ProjectRepo) ListComments(ctx context.Context, entityType, entityID string) ([]*repo.Comment, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, entity_type, entity_id, content, COALESCE(author, ''), created_at
		 FROM comments WHERE entity_type = $1 AND entity_id = $2 ORDER BY created_at`,
		entityType, entityID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.ProjectRepo.ListComments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var comments []*repo.Comment
	for rows.Next() {
		c := &repo.Comment{}
		if err := rows.Scan(&c.ID, &c.EntityType, &c.EntityID, &c.Content, &c.Author, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("postgres.ProjectRepo.ListComments: %w", err)
		}
		comments = append(comments, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.ProjectRepo.ListComments: %w", err)
	}
	return comments, nil
}

// scanPgTasks scans task rows for PostgreSQL (time.Time scanned directly).
func scanPgTasks(rows *sql.Rows) ([]*repo.Task, error) {
	var tasks []*repo.Task
	for rows.Next() {
		task := &repo.Task{}
		var description, assigneeID sql.NullString
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&task.ID, &task.MilestoneID, &task.Name, &description, &task.Status, &task.Priority, &task.OrderIndex, &assigneeID, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("postgres.scanPgTasks: %w", err)
		}
		task.Description = description.String
		task.AssigneeAgentID = assigneeID.String
		task.CreatedAt = createdAt
		task.UpdatedAt = updatedAt
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.scanPgTasks: %w", err)
	}
	return tasks, nil
}
