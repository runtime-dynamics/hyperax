package repo

import (
	"context"
	"time"
)

// ProjectPlan is a top-level project container.
type ProjectPlan struct {
	ID            string
	Name          string
	Description   string
	WorkspaceName string
	Status        string
	Priority      string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Milestone groups related tasks within a project plan.
type Milestone struct {
	ID                string
	ProjectID         string
	Name              string
	Description       string
	Status            string
	Priority          string
	DueDate           *time.Time
	OrderIndex        int
	AssigneeAgentID string
}

// Task is an individual work item within a milestone.
type Task struct {
	ID                string
	MilestoneID       string
	ProjectID         string // Resolved from milestone→project join; empty for non-workspace queries.
	Name              string
	Description       string
	Status            string
	Priority          string
	OrderIndex        int
	AssigneeAgentID string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Comment is a polymorphic comment attached to any entity.
type Comment struct {
	ID         string
	EntityType string
	EntityID   string
	Content    string
	Author     string
	CreatedAt  time.Time
}

// ProjectRepo handles project plans, milestones, tasks, and comments.
type ProjectRepo interface {
	CreateProjectPlan(ctx context.Context, plan *ProjectPlan) (string, error)
	GetProjectPlan(ctx context.Context, id string) (*ProjectPlan, error)
	ListProjectPlans(ctx context.Context, workspaceName string) ([]*ProjectPlan, error)
	DeleteProjectPlan(ctx context.Context, id string) error
	UpdateProjectStatus(ctx context.Context, id string, status string) error
	MoveProjectWorkspace(ctx context.Context, id string, targetWorkspace string) error
	CreateMilestone(ctx context.Context, milestone *Milestone) (string, error)
	GetMilestone(ctx context.Context, id string) (*Milestone, error)
	ListMilestones(ctx context.Context, projectID string) ([]*Milestone, error)
	AssignMilestone(ctx context.Context, milestoneID string, agentID string) error
	UnassignMilestone(ctx context.Context, milestoneID string) error
	CreateTask(ctx context.Context, task *Task) (string, error)
	GetTask(ctx context.Context, id string) (*Task, error)
	UpdateTaskStatus(ctx context.Context, id string, status string) error
	ListTasks(ctx context.Context, milestoneID string) ([]*Task, error)
	AssignTask(ctx context.Context, taskID string, agentID string) error
	UnassignTask(ctx context.Context, taskID string) error
	DeleteMilestone(ctx context.Context, id string) error
	DeleteTask(ctx context.Context, id string) error
	PurgeOrphans(ctx context.Context) (int64, error)
	ReconcileCompletionStatus(ctx context.Context) (milestonesCompleted int, projectsCompleted int, err error)
	ListTasksByAgent(ctx context.Context, agentID string, status string, projectID string) ([]*Task, error)
	GetNextTask(ctx context.Context, agentID string) (*Task, error)
	AddComment(ctx context.Context, comment *Comment) (string, error)
	ListComments(ctx context.Context, entityType, entityID string) ([]*Comment, error)
}
