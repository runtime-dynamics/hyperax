package repo

import (
	"context"
	"time"
)

// Pipeline is a pipeline definition.
type Pipeline struct {
	ID            string
	Name          string
	Description   string
	WorkspaceName string
	ProjectName   string
	Swimlanes     string // JSON
	SetupCommands string // JSON
	Environment   string // JSON
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// PipelineJob is an execution instance of a pipeline.
type PipelineJob struct {
	ID            string
	PipelineID    string
	Status        string
	WorkspaceName string
	StartedAt     *time.Time
	CompletedAt   *time.Time
	Error         string
	Result        string
}

// StepResult captures per-step execution results.
type StepResult struct {
	ID          string
	JobID       string
	SwimlaneID  string
	StepID      string
	StepName    string
	Status      string
	ExitCode    *int
	StartedAt   *time.Time
	CompletedAt *time.Time
	DurationMS  *int
	OutputLog   string
	Error       string
}

// PipelineAssignment links a pipeline to a workspace and optional project.
type PipelineAssignment struct {
	ID         string
	PipelineID string
	WorkspaceID string
	ProjectID  string
	AssignedAt time.Time
}

// JobFilter holds optional filter criteria for listing pipeline jobs.
type JobFilter struct {
	Status string // Filter by job status (empty means no filter).
	Limit  int    // Maximum number of results (0 means no limit).
}

// PipelineRepo handles pipeline definitions, jobs, step results, and assignments.
type PipelineRepo interface {
	CreatePipeline(ctx context.Context, pipeline *Pipeline) (string, error)
	GetPipeline(ctx context.Context, id string) (*Pipeline, error)
	ListPipelines(ctx context.Context, workspaceName string) ([]*Pipeline, error)
	SearchPipelines(ctx context.Context, query string, workspaceName string) ([]*Pipeline, error)
	CreateJob(ctx context.Context, pipelineID, workspaceName string) (string, error)
	GetJob(ctx context.Context, id string) (*PipelineJob, error)
	UpdateJobStatus(ctx context.Context, id string, status string, result string) error
	ListJobs(ctx context.Context, pipelineID string) ([]*PipelineJob, error)
	ListJobsFiltered(ctx context.Context, pipelineID string, filter JobFilter) ([]*PipelineJob, error)
	CreateStepResult(ctx context.Context, result *StepResult) (string, error)
	ListStepResults(ctx context.Context, jobID string) ([]*StepResult, error)
	CreateAssignment(ctx context.Context, assignment *PipelineAssignment) (string, error)
	ListAssignments(ctx context.Context, workspaceID string, pipelineID string) ([]*PipelineAssignment, error)
	DeleteAssignment(ctx context.Context, id string) error
}
