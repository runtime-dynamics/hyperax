package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/repo"
)

// SpecRepo implements repo.SpecRepo for SQLite.
type SpecRepo struct {
	db *sql.DB
}

// NextSpecNumber returns the next auto-increment spec number for a workspace.
func (r *SpecRepo) NextSpecNumber(ctx context.Context, workspaceName string) (int, error) {
	var maxNum sql.NullInt64
	err := r.db.QueryRowContext(ctx,
		`SELECT MAX(spec_number) FROM specs WHERE workspace_name = ?`,
		workspaceName,
	).Scan(&maxNum)
	if err != nil {
		return 0, fmt.Errorf("sqlite.SpecRepo.NextSpecNumber: %w", err)
	}
	if !maxNum.Valid {
		return 1, nil
	}
	return int(maxNum.Int64) + 1, nil
}

// CreateSpec inserts a new spec and returns its generated ID.
func (r *SpecRepo) CreateSpec(ctx context.Context, spec *repo.Spec) (string, error) {
	if spec.ID == "" {
		spec.ID = uuid.New().String()
	}
	if spec.Status == "" {
		spec.Status = "draft"
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO specs (id, spec_number, title, description, status, project_id, workspace_name, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		spec.ID, spec.SpecNumber, spec.Title, spec.Description, spec.Status,
		spec.ProjectID, spec.WorkspaceName, spec.CreatedBy,
	)
	if err != nil {
		return "", fmt.Errorf("sqlite.SpecRepo.CreateSpec: %w", err)
	}
	return spec.ID, nil
}

// GetSpec retrieves a spec by ID.
func (r *SpecRepo) GetSpec(ctx context.Context, id string) (*repo.Spec, error) {
	s := &repo.Spec{}
	var description, projectID, createdBy sql.NullString
	var createdAt, updatedAt string

	err := r.db.QueryRowContext(ctx,
		`SELECT id, spec_number, title, COALESCE(description, ''), status,
		        COALESCE(project_id, ''), workspace_name, COALESCE(created_by, ''),
		        created_at, updated_at
		 FROM specs WHERE id = ?`,
		id,
	).Scan(&s.ID, &s.SpecNumber, &s.Title, &description, &s.Status,
		&projectID, &s.WorkspaceName, &createdBy, &createdAt, &updatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("spec %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite.SpecRepo.GetSpec: %w", err)
	}

	s.Description = description.String
	s.ProjectID = projectID.String
	s.CreatedBy = createdBy.String
	if s.CreatedAt, err = parseSQLiteTime(createdAt, "sqlite.SpecRepo.GetSpec"); err != nil {
		return nil, err
	}
	if s.UpdatedAt, err = parseSQLiteTime(updatedAt, "sqlite.SpecRepo.GetSpec"); err != nil {
		return nil, err
	}
	return s, nil
}

// GetSpecByNumber retrieves a spec by workspace and spec number.
func (r *SpecRepo) GetSpecByNumber(ctx context.Context, workspaceName string, specNumber int) (*repo.Spec, error) {
	s := &repo.Spec{}
	var description, projectID, createdBy sql.NullString
	var createdAt, updatedAt string

	err := r.db.QueryRowContext(ctx,
		`SELECT id, spec_number, title, COALESCE(description, ''), status,
		        COALESCE(project_id, ''), workspace_name, COALESCE(created_by, ''),
		        created_at, updated_at
		 FROM specs WHERE workspace_name = ? AND spec_number = ?`,
		workspaceName, specNumber,
	).Scan(&s.ID, &s.SpecNumber, &s.Title, &description, &s.Status,
		&projectID, &s.WorkspaceName, &createdBy, &createdAt, &updatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("spec #%d not found in workspace %q", specNumber, workspaceName)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite.SpecRepo.GetSpecByNumber: %w", err)
	}

	s.Description = description.String
	s.ProjectID = projectID.String
	s.CreatedBy = createdBy.String
	if s.CreatedAt, err = parseSQLiteTime(createdAt, "sqlite.SpecRepo.GetSpecByNumber"); err != nil {
		return nil, err
	}
	if s.UpdatedAt, err = parseSQLiteTime(updatedAt, "sqlite.SpecRepo.GetSpecByNumber"); err != nil {
		return nil, err
	}
	return s, nil
}

// ListSpecs returns all specs for a workspace, ordered by spec_number.
func (r *SpecRepo) ListSpecs(ctx context.Context, workspaceName string) ([]*repo.Spec, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, spec_number, title, COALESCE(description, ''), status,
		        COALESCE(project_id, ''), workspace_name, COALESCE(created_by, ''),
		        created_at, updated_at
		 FROM specs WHERE workspace_name = ? ORDER BY spec_number`,
		workspaceName,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.SpecRepo.ListSpecs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var specs []*repo.Spec
	for rows.Next() {
		s := &repo.Spec{}
		var description, projectID, createdBy sql.NullString
		var createdAt, updatedAt string
		if err := rows.Scan(&s.ID, &s.SpecNumber, &s.Title, &description, &s.Status,
			&projectID, &s.WorkspaceName, &createdBy, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("sqlite.SpecRepo.ListSpecs: %w", err)
		}
		s.Description = description.String
		s.ProjectID = projectID.String
		s.CreatedBy = createdBy.String
		if s.CreatedAt, err = parseSQLiteTime(createdAt, "sqlite.SpecRepo.ListSpecs"); err != nil {
			return nil, err
		}
		if s.UpdatedAt, err = parseSQLiteTime(updatedAt, "sqlite.SpecRepo.ListSpecs"); err != nil {
			return nil, err
		}
		specs = append(specs, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.SpecRepo.ListSpecs: %w", err)
	}
	return specs, nil
}

// UpdateSpecStatus changes the status of a spec.
func (r *SpecRepo) UpdateSpecStatus(ctx context.Context, id string, status string) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE specs SET status = ?, updated_at = datetime('now') WHERE id = ?`,
		status, id,
	)
	if err != nil {
		return fmt.Errorf("sqlite.SpecRepo.UpdateSpecStatus: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.SpecRepo.UpdateSpecStatus: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("spec %q not found", id)
	}
	return nil
}

// CreateSpecMilestone inserts a spec milestone and returns its generated ID.
func (r *SpecRepo) CreateSpecMilestone(ctx context.Context, ms *repo.SpecMilestone) (string, error) {
	if ms.ID == "" {
		ms.ID = uuid.New().String()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO spec_milestones (id, spec_id, title, description, order_index, milestone_id)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		ms.ID, ms.SpecID, ms.Title, ms.Description, ms.OrderIndex, ms.MilestoneID,
	)
	if err != nil {
		return "", fmt.Errorf("sqlite.SpecRepo.CreateSpecMilestone: %w", err)
	}
	return ms.ID, nil
}

// ListSpecMilestones returns all milestones for a spec, ordered by order_index.
func (r *SpecRepo) ListSpecMilestones(ctx context.Context, specID string) ([]*repo.SpecMilestone, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, spec_id, title, COALESCE(description, ''), order_index,
		        COALESCE(milestone_id, ''), created_at
		 FROM spec_milestones WHERE spec_id = ? ORDER BY order_index`,
		specID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.SpecRepo.ListSpecMilestones: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var milestones []*repo.SpecMilestone
	for rows.Next() {
		ms := &repo.SpecMilestone{}
		var description, milestoneID sql.NullString
		var createdAt string
		if err := rows.Scan(&ms.ID, &ms.SpecID, &ms.Title, &description,
			&ms.OrderIndex, &milestoneID, &createdAt); err != nil {
			return nil, fmt.Errorf("sqlite.SpecRepo.ListSpecMilestones: %w", err)
		}
		ms.Description = description.String
		ms.MilestoneID = milestoneID.String
		if ms.CreatedAt, err = parseSQLiteTime(createdAt, "sqlite.SpecRepo.ListSpecMilestones"); err != nil {
			return nil, err
		}
		milestones = append(milestones, ms)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.SpecRepo.ListSpecMilestones: %w", err)
	}
	return milestones, nil
}

// CreateSpecTask inserts a spec task and returns its generated ID.
func (r *SpecRepo) CreateSpecTask(ctx context.Context, task *repo.SpecTask) (string, error) {
	if task.ID == "" {
		task.ID = uuid.New().String()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO spec_tasks (id, spec_id, spec_milestone_id, title, requirement, acceptance_criteria, order_index, task_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.SpecID, task.SpecMilestoneID, task.Title,
		task.Requirement, task.AcceptanceCriteria, task.OrderIndex, task.TaskID,
	)
	if err != nil {
		return "", fmt.Errorf("sqlite.SpecRepo.CreateSpecTask: %w", err)
	}
	return task.ID, nil
}

// ListSpecTasks returns all tasks for a spec milestone, ordered by order_index.
func (r *SpecRepo) ListSpecTasks(ctx context.Context, specMilestoneID string) ([]*repo.SpecTask, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, spec_id, spec_milestone_id, title, COALESCE(requirement, ''),
		        COALESCE(acceptance_criteria, ''), order_index, COALESCE(task_id, ''), created_at
		 FROM spec_tasks WHERE spec_milestone_id = ? ORDER BY order_index`,
		specMilestoneID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.SpecRepo.ListSpecTasks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tasks []*repo.SpecTask
	for rows.Next() {
		t := &repo.SpecTask{}
		var requirement, acceptance, taskID sql.NullString
		var createdAt string
		if err := rows.Scan(&t.ID, &t.SpecID, &t.SpecMilestoneID, &t.Title,
			&requirement, &acceptance, &t.OrderIndex, &taskID, &createdAt); err != nil {
			return nil, fmt.Errorf("sqlite.SpecRepo.ListSpecTasks: %w", err)
		}
		t.Requirement = requirement.String
		t.AcceptanceCriteria = acceptance.String
		t.TaskID = taskID.String
		if t.CreatedAt, err = parseSQLiteTime(createdAt, "sqlite.SpecRepo.ListSpecTasks"); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.SpecRepo.ListSpecTasks: %w", err)
	}
	return tasks, nil
}

// ListAllSpecTasks returns all tasks for an entire spec, ordered by milestone then task order.
func (r *SpecRepo) ListAllSpecTasks(ctx context.Context, specID string) ([]*repo.SpecTask, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT st.id, st.spec_id, st.spec_milestone_id, st.title,
		        COALESCE(st.requirement, ''), COALESCE(st.acceptance_criteria, ''),
		        st.order_index, COALESCE(st.task_id, ''), st.created_at
		 FROM spec_tasks st
		 INNER JOIN spec_milestones sm ON st.spec_milestone_id = sm.id
		 WHERE st.spec_id = ?
		 ORDER BY sm.order_index, st.order_index`,
		specID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.SpecRepo.ListAllSpecTasks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tasks []*repo.SpecTask
	for rows.Next() {
		t := &repo.SpecTask{}
		var requirement, acceptance, taskID sql.NullString
		var createdAt string
		if err := rows.Scan(&t.ID, &t.SpecID, &t.SpecMilestoneID, &t.Title,
			&requirement, &acceptance, &t.OrderIndex, &taskID, &createdAt); err != nil {
			return nil, fmt.Errorf("sqlite.SpecRepo.ListAllSpecTasks: %w", err)
		}
		t.Requirement = requirement.String
		t.AcceptanceCriteria = acceptance.String
		t.TaskID = taskID.String
		if t.CreatedAt, err = parseSQLiteTime(createdAt, "sqlite.SpecRepo.ListAllSpecTasks"); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.SpecRepo.ListAllSpecTasks: %w", err)
	}
	return tasks, nil
}

// CreateAmendment adds an amendment to a spec and returns its generated ID.
func (r *SpecRepo) CreateAmendment(ctx context.Context, amendment *repo.SpecAmendment) (string, error) {
	if amendment.ID == "" {
		amendment.ID = uuid.New().String()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO spec_amendments (id, spec_id, title, description, author)
		 VALUES (?, ?, ?, ?, ?)`,
		amendment.ID, amendment.SpecID, amendment.Title, amendment.Description, amendment.Author,
	)
	if err != nil {
		return "", fmt.Errorf("sqlite.SpecRepo.CreateAmendment: %w", err)
	}

	// Touch the spec's updated_at timestamp.
	if _, err := r.db.ExecContext(ctx,
		`UPDATE specs SET updated_at = datetime('now') WHERE id = ?`,
		amendment.SpecID,
	); err != nil {
		return "", fmt.Errorf("sqlite.SpecRepo.CreateAmendment: update spec timestamp: %w", err)
	}

	return amendment.ID, nil
}

// ListAmendments returns all amendments for a spec, ordered by creation time.
func (r *SpecRepo) ListAmendments(ctx context.Context, specID string) ([]*repo.SpecAmendment, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, spec_id, title, description, COALESCE(author, ''), created_at
		 FROM spec_amendments WHERE spec_id = ? ORDER BY created_at`,
		specID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.SpecRepo.ListAmendments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var amendments []*repo.SpecAmendment
	for rows.Next() {
		a := &repo.SpecAmendment{}
		var author sql.NullString
		var createdAt string
		if err := rows.Scan(&a.ID, &a.SpecID, &a.Title, &a.Description, &author, &createdAt); err != nil {
			return nil, fmt.Errorf("sqlite.SpecRepo.ListAmendments: %w", err)
		}
		a.Author = author.String
		if a.CreatedAt, err = parseSQLiteTime(createdAt, "sqlite.SpecRepo.ListAmendments"); err != nil {
			return nil, err
		}
		amendments = append(amendments, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.SpecRepo.ListAmendments: %w", err)
	}
	return amendments, nil
}
