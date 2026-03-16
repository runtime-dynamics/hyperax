package types

import "time"

// PipelineSpec defines a pipeline's configuration.
type PipelineSpec struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Description   string            `json:"description,omitempty"`
	WorkspaceName string            `json:"workspace_name"`
	ProjectName   string            `json:"project_name,omitempty"`
	Swimlanes     []Swimlane        `json:"swimlanes"`
	SetupCommands []string          `json:"setup_commands,omitempty"`
	Environment   map[string]string `json:"environment,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

// Swimlane is a parallel execution lane within a pipeline.
type Swimlane struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Steps []Step `json:"steps"`
}

// Step is a single command in a swimlane.
type Step struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Command string `json:"command"`
	Dir     string `json:"dir,omitempty"`
}

// JobStatus represents the status of a pipeline job.
type JobStatus struct {
	ID          string       `json:"id"`
	PipelineID  string       `json:"pipeline_id"`
	Status      string       `json:"status"` // pending, running, completed, failed, cancelled
	StartedAt   *time.Time   `json:"started_at,omitempty"`
	CompletedAt *time.Time   `json:"completed_at,omitempty"`
	Error       string       `json:"error,omitempty"`
	Steps       []StepResult `json:"steps,omitempty"`
}

// StepResult records the outcome of a single step execution.
type StepResult struct {
	ID          string     `json:"id"`
	JobID       string     `json:"job_id"`
	SwimlaneID  string     `json:"swimlane_id"`
	StepID      string     `json:"step_id"`
	StepName    string     `json:"step_name"`
	Status      string     `json:"status"`
	ExitCode    *int       `json:"exit_code,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	DurationMS  int64      `json:"duration_ms,omitempty"`
	OutputLog   string     `json:"output_log,omitempty"`
	Error       string     `json:"error,omitempty"`
}
