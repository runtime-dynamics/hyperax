package repo

import (
	"context"
	"time"
)

// Spec is a structured specification document with auto-incrementing number per workspace.
type Spec struct {
	ID            string
	SpecNumber    int
	Title         string
	Description   string
	Status        string // draft, approved, in_progress, completed, archived
	ProjectID     string // linked project plan created from this spec
	WorkspaceName string
	CreatedBy     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// SpecMilestone is a milestone within a spec.
type SpecMilestone struct {
	ID          string
	SpecID      string
	Title       string
	Description string
	OrderIndex  int
	MilestoneID string // linked project milestone
	CreatedAt   time.Time
}

// SpecTask is a task within a spec milestone.
type SpecTask struct {
	ID                 string
	SpecID             string
	SpecMilestoneID    string
	Title              string
	Requirement        string
	AcceptanceCriteria string
	OrderIndex         int
	TaskID             string // linked project task
	CreatedAt          time.Time
}

// SpecAmendment tracks changes to approved specs.
type SpecAmendment struct {
	ID          string
	SpecID      string
	Title       string
	Description string
	Author      string
	CreatedAt   time.Time
}

// SpecRepo handles specification documents and their child entities.
type SpecRepo interface {
	// NextSpecNumber returns the next auto-increment spec number for a workspace.
	NextSpecNumber(ctx context.Context, workspaceName string) (int, error)

	// CreateSpec inserts a new spec and returns its ID.
	CreateSpec(ctx context.Context, spec *Spec) (string, error)

	// GetSpec retrieves a spec by ID.
	GetSpec(ctx context.Context, id string) (*Spec, error)

	// GetSpecByNumber retrieves a spec by workspace and spec number.
	GetSpecByNumber(ctx context.Context, workspaceName string, specNumber int) (*Spec, error)

	// ListSpecs returns all specs for a workspace, ordered by spec_number.
	ListSpecs(ctx context.Context, workspaceName string) ([]*Spec, error)

	// UpdateSpecStatus changes the status of a spec.
	UpdateSpecStatus(ctx context.Context, id string, status string) error

	// CreateSpecMilestone inserts a spec milestone and returns its ID.
	CreateSpecMilestone(ctx context.Context, ms *SpecMilestone) (string, error)

	// ListSpecMilestones returns all milestones for a spec.
	ListSpecMilestones(ctx context.Context, specID string) ([]*SpecMilestone, error)

	// CreateSpecTask inserts a spec task and returns its ID.
	CreateSpecTask(ctx context.Context, task *SpecTask) (string, error)

	// ListSpecTasks returns all tasks for a spec milestone.
	ListSpecTasks(ctx context.Context, specMilestoneID string) ([]*SpecTask, error)

	// ListAllSpecTasks returns all tasks for an entire spec.
	ListAllSpecTasks(ctx context.Context, specID string) ([]*SpecTask, error)

	// CreateAmendment adds an amendment to an approved spec.
	CreateAmendment(ctx context.Context, amendment *SpecAmendment) (string, error)

	// ListAmendments returns all amendments for a spec.
	ListAmendments(ctx context.Context, specID string) ([]*SpecAmendment, error)
}
