package repo

import (
	"context"
	"encoding/json"
	"time"
)

// Workflow defines a workflow definition stored in the workflows table.
type Workflow struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// WorkflowStep defines a single step within a workflow definition.
type WorkflowStep struct {
	ID               string    `json:"id"`
	WorkflowID       string    `json:"workflow_id"`
	Name             string    `json:"name"`
	StepType         string    `json:"step_type"`          // "tool", "condition", "approval"
	Action           json.RawMessage `json:"action"`       // JSON payload describing the action
	DependsOn        string    `json:"depends_on"`         // Comma-separated step IDs
	Condition        string    `json:"condition"`           // CEL expression string
	RequiresApproval bool      `json:"requires_approval"`
	Position         int       `json:"position"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// WorkflowRun records a single execution of a workflow.
type WorkflowRun struct {
	ID         string          `json:"id"`
	WorkflowID string         `json:"workflow_id"`
	Status     string          `json:"status"` // "pending", "running", "completed", "failed", "cancelled"
	StartedAt  *time.Time      `json:"started_at,omitempty"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
	Error      string          `json:"error,omitempty"`
	Context    json.RawMessage `json:"context"` // Execution context data (variables, inputs)
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// WorkflowRunStep records the status of a single step within a workflow run.
type WorkflowRunStep struct {
	ID         string     `json:"id"`
	RunID      string     `json:"run_id"`
	StepID     string     `json:"step_id"`
	Status     string     `json:"status"` // "pending", "running", "completed", "failed", "skipped", "waiting_approval"
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Output     string     `json:"output,omitempty"`
	Error      string     `json:"error,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// WorkflowTrigger defines an automatic trigger for a workflow.
type WorkflowTrigger struct {
	ID          string          `json:"id"`
	WorkflowID  string         `json:"workflow_id"`
	TriggerType string         `json:"trigger_type"` // "manual", "event", "cron", "webhook"
	Config      json.RawMessage `json:"config"`
	Enabled     bool            `json:"enabled"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// WorkflowRepo is the data-access interface for workflow definitions,
// steps, runs, run steps, and triggers.
type WorkflowRepo interface {
	// Workflow CRUD
	CreateWorkflow(ctx context.Context, wf *Workflow) (string, error)
	GetWorkflow(ctx context.Context, id string) (*Workflow, error)
	ListWorkflows(ctx context.Context) ([]*Workflow, error)
	UpdateWorkflow(ctx context.Context, wf *Workflow) error
	DeleteWorkflow(ctx context.Context, id string) error

	// Step CRUD
	CreateStep(ctx context.Context, step *WorkflowStep) (string, error)
	GetSteps(ctx context.Context, workflowID string) ([]*WorkflowStep, error)
	UpdateStep(ctx context.Context, step *WorkflowStep) error
	DeleteStep(ctx context.Context, id string) error

	// Run management
	CreateRun(ctx context.Context, run *WorkflowRun) (string, error)
	GetRun(ctx context.Context, id string) (*WorkflowRun, error)
	ListRuns(ctx context.Context, workflowID string) ([]*WorkflowRun, error)
	UpdateRunStatus(ctx context.Context, id string, status string, errMsg string) error

	// Run step management
	CreateRunStep(ctx context.Context, rs *WorkflowRunStep) (string, error)
	GetRunSteps(ctx context.Context, runID string) ([]*WorkflowRunStep, error)
	UpdateRunStepStatus(ctx context.Context, id string, status string, output string, errMsg string) error

	// Trigger management
	CreateTrigger(ctx context.Context, trigger *WorkflowTrigger) (string, error)
	ListTriggers(ctx context.Context, workflowID string) ([]*WorkflowTrigger, error)
	DeleteTrigger(ctx context.Context, id string) error
}
