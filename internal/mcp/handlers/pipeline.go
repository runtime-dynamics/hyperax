package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/hyperax/hyperax/internal/cron"
	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/pipeline"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/internal/storage"
	"github.com/hyperax/hyperax/internal/workflow"
	"github.com/hyperax/hyperax/pkg/types"
)

// actionClearancePipeline maps each pipeline action to its minimum ABAC clearance.
var actionClearancePipeline = map[string]int{
	// Pipeline actions
	"create_pipeline":           1,
	"list_pipelines":            0,
	"get_pipeline":              0,
	"run_pipeline":              1,
	"pipeline_job_status":       0,
	"pipeline_job_log":          0,
	"list_pipeline_jobs":        0,
	"cancel_pipeline_job":       1,
	"discover_pipelines":        0,
	"assign_pipeline":           1,
	"list_pipeline_assignments": 0,

	// Workflow actions
	"create_workflow":       1,
	"list_workflows":        0,
	"run_workflow":          1,
	"get_workflow_status":   0,
	"approve_workflow_step": 1,
	"cancel_workflow_run":   1,

	// Cron actions
	"create_cron_job":  1,
	"list_cron_jobs":   0,
	"update_cron_job":  1,
	"delete_cron_job":  1,
	"get_cron_history": 0,
	"trigger_cron_job": 1,
}

// PipelineHandler implements the consolidated "pipeline" MCP tool.
// It provides pipeline execution, workflow orchestration, and cron scheduling
// through a single action-dispatched tool.
type PipelineHandler struct {
	store    *storage.Store
	executor *pipeline.Executor
	bus      *nervous.EventBus
	logger   *slog.Logger

	// Workflow deps (set via SetWorkflowDeps)
	wfExecutor *workflow.Executor

	// Cron deps (set via SetCronDeps)
	scheduler *cron.Scheduler
}

// NewPipelineHandler creates a PipelineHandler with an Executor wired to
// the event bus and logger.
func NewPipelineHandler(store *storage.Store, bus *nervous.EventBus, logger *slog.Logger) *PipelineHandler {
	var exec *pipeline.Executor
	if store.Pipelines != nil && bus != nil && logger != nil {
		exec = pipeline.NewExecutor(store.Pipelines, bus, logger)
	}
	return &PipelineHandler{
		store:    store,
		executor: exec,
		bus:      bus,
		logger:   logger,
	}
}

// SetWorkflowDeps injects workflow execution dependencies.
func (h *PipelineHandler) SetWorkflowDeps(executor *workflow.Executor) {
	h.wfExecutor = executor
}

// SetCronDeps injects cron scheduling dependencies.
func (h *PipelineHandler) SetCronDeps(scheduler *cron.Scheduler) {
	h.scheduler = scheduler
}

// RegisterTools registers the consolidated pipeline tool.
func (h *PipelineHandler) RegisterTools(registry *mcp.ToolRegistry) {
	registry.Register(
		"pipeline",
		"Pipeline execution, workflow orchestration, and cron scheduling. "+
			"Actions: create_pipeline | list_pipelines | get_pipeline | run_pipeline | "+
			"pipeline_job_status | pipeline_job_log | list_pipeline_jobs | cancel_pipeline_job | "+
			"discover_pipelines | assign_pipeline | list_pipeline_assignments | "+
			"create_workflow | list_workflows | run_workflow | get_workflow_status | "+
			"approve_workflow_step | cancel_workflow_run | "+
			"create_cron_job | list_cron_jobs | update_cron_job | delete_cron_job | "+
			"get_cron_history | trigger_cron_job",
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": [
					"create_pipeline", "list_pipelines", "get_pipeline", "run_pipeline",
					"pipeline_job_status", "pipeline_job_log", "list_pipeline_jobs", "cancel_pipeline_job",
					"discover_pipelines", "assign_pipeline", "list_pipeline_assignments",
					"create_workflow", "list_workflows", "run_workflow", "get_workflow_status",
					"approve_workflow_step", "cancel_workflow_run",
					"create_cron_job", "list_cron_jobs", "update_cron_job", "delete_cron_job",
					"get_cron_history", "trigger_cron_job"
				], "description": "Action to perform"},

				"workspace_name":   {"type": "string", "description": "Workspace name (pipeline actions)"},
				"pipeline_id":      {"type": "string", "description": "Pipeline ID"},
				"name":             {"type": "string", "description": "Name (pipeline, workflow, cron job)"},
				"description":      {"type": "string", "description": "Description (pipeline, workflow)"},
				"swimlanes":        {"type": "string", "description": "Swimlanes definition JSON (create_pipeline)"},
				"setup_commands":   {"type": "string", "description": "Setup commands JSON (create_pipeline)"},
				"environment":      {"type": "string", "description": "Environment variables JSON (create_pipeline)"},
				"job_id":           {"type": "string", "description": "Job ID (pipeline_job_status, pipeline_job_log, cancel_pipeline_job)"},
				"tail":             {"type": "integer", "description": "Number of log tail lines (pipeline_job_log)"},
				"status":           {"type": "string", "description": "Status filter (list_pipeline_jobs)"},
				"limit":            {"type": "integer", "description": "Maximum results"},
				"query":            {"type": "string", "description": "Search query (discover_pipelines)"},
				"workspace_id":     {"type": "string", "description": "Workspace ID (discover_pipelines, assign_pipeline)"},
				"project_id":       {"type": "string", "description": "Project ID (assign_pipeline)"},

				"workflow_id":      {"type": "string", "description": "Workflow ID (run_workflow)"},
				"run_id":           {"type": "string", "description": "Workflow run ID"},
				"step_id":          {"type": "string", "description": "Step ID (approve_workflow_step)"},
				"steps":            {"type": "array", "description": "Workflow steps (create_workflow)", "items": {
					"type": "object",
					"properties": {
						"name":              {"type": "string"},
						"step_type":         {"type": "string"},
						"action":            {"type": "object"},
						"depends_on":        {"type": "string"},
						"condition":         {"type": "string"},
						"requires_approval": {"type": "boolean"}
					},
					"required": ["name"]
				}},
				"context":          {"type": "object", "description": "Execution context variables (run_workflow)"},

				"id":               {"type": "string", "description": "Cron job ID"},
				"schedule":         {"type": "string", "description": "Cron expression (create/update_cron_job)"},
				"job_type":         {"type": "string", "description": "Job type: pipeline, tool, webhook (cron)"},
				"payload":          {"type": "string", "description": "JSON payload (cron)"},
				"max_retries":      {"type": "integer", "description": "Max retry attempts (cron)"},
				"enabled":          {"type": "boolean", "description": "Enable/disable (cron)"}
			},
			"required": ["action"]
		}`),
		h.dispatch,
	)
}

// dispatch routes the consolidated "pipeline" tool to the correct handler method.
func (h *PipelineHandler) dispatch(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var envelope struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.dispatch: %w", err)
	}

	if err := checkActionClearance(ctx, envelope.Action, actionClearancePipeline); err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	switch envelope.Action {
	// Pipeline actions
	case "create_pipeline":
		return h.createPipeline(ctx, params)
	case "list_pipelines":
		return h.listPipelines(ctx, params)
	case "get_pipeline":
		return h.getPipeline(ctx, params)
	case "run_pipeline":
		return h.runPipeline(ctx, params)
	case "pipeline_job_status":
		return h.pipelineJobStatus(ctx, params)
	case "pipeline_job_log":
		return h.pipelineJobLog(ctx, params)
	case "list_pipeline_jobs":
		return h.listPipelineJobs(ctx, params)
	case "cancel_pipeline_job":
		return h.cancelPipelineJob(ctx, params)
	case "discover_pipelines":
		return h.discoverPipelines(ctx, params)
	case "assign_pipeline":
		return h.assignPipeline(ctx, params)
	case "list_pipeline_assignments":
		return h.listPipelineAssignments(ctx, params)

	// Workflow actions
	case "create_workflow":
		return h.createWorkflow(ctx, params)
	case "list_workflows":
		return h.listWorkflows(ctx, params)
	case "run_workflow":
		return h.runWorkflow(ctx, params)
	case "get_workflow_status":
		return h.getWorkflowStatus(ctx, params)
	case "approve_workflow_step":
		return h.approveWorkflowStep(ctx, params)
	case "cancel_workflow_run":
		return h.cancelWorkflowRun(ctx, params)

	// Cron actions
	case "create_cron_job":
		return h.createCronJob(ctx, params)
	case "list_cron_jobs":
		return h.listCronJobs(ctx, params)
	case "update_cron_job":
		return h.updateCronJob(ctx, params)
	case "delete_cron_job":
		return h.deleteCronJob(ctx, params)
	case "get_cron_history":
		return h.getCronHistory(ctx, params)
	case "trigger_cron_job":
		return h.triggerCronJob(ctx, params)

	default:
		return types.NewErrorResult(fmt.Sprintf("unknown pipeline action %q", envelope.Action)), nil
	}
}

// ── Pipeline actions ────────────────────────────────────────────────────────

func (h *PipelineHandler) createPipeline(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Name          string `json:"name"`
		Description   string `json:"description"`
		Swimlanes     string `json:"swimlanes"`
		SetupCommands string `json:"setup_commands"`
		Environment   string `json:"environment"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.createPipeline: %w", err)
	}

	if args.WorkspaceName == "" || args.Name == "" {
		return types.NewErrorResult("workspace_name and name are required"), nil
	}

	if h.store.Pipelines == nil {
		return types.NewErrorResult("pipeline repository not available"), nil
	}

	pipeline := &repo.Pipeline{
		Name:          args.Name,
		Description:   args.Description,
		WorkspaceName: args.WorkspaceName,
		Swimlanes:     defaultIfEmpty(args.Swimlanes, "[]"),
		SetupCommands: defaultIfEmpty(args.SetupCommands, "[]"),
		Environment:   defaultIfEmpty(args.Environment, "{}"),
	}

	id, err := h.store.Pipelines.CreatePipeline(ctx, pipeline)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("create pipeline: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"id":      id,
		"message": fmt.Sprintf("Pipeline %q created.", args.Name),
	}), nil
}

func (h *PipelineHandler) listPipelines(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.listPipelines: %w", err)
	}

	if args.WorkspaceName == "" {
		return types.NewErrorResult("workspace_name is required"), nil
	}

	if h.store.Pipelines == nil {
		return types.NewErrorResult("pipeline repository not available"), nil
	}

	pipelines, err := h.store.Pipelines.ListPipelines(ctx, args.WorkspaceName)
	if err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.listPipelines: %w", err)
	}

	if len(pipelines) == 0 {
		return types.NewToolResult("No pipelines found."), nil
	}

	type pipelineSummary struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}

	summaries := make([]pipelineSummary, len(pipelines))
	for i, p := range pipelines {
		summaries[i] = pipelineSummary{
			ID:          p.ID,
			Name:        p.Name,
			Description: p.Description,
		}
	}
	return types.NewToolResult(summaries), nil
}

func (h *PipelineHandler) getPipeline(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		PipelineID string `json:"pipeline_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.getPipeline: %w", err)
	}

	if args.PipelineID == "" {
		return types.NewErrorResult("pipeline_id is required"), nil
	}

	if h.store.Pipelines == nil {
		return types.NewErrorResult("pipeline repository not available"), nil
	}

	pipeline, err := h.store.Pipelines.GetPipeline(ctx, args.PipelineID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get pipeline: %v", err)), nil
	}

	return types.NewToolResult(pipeline), nil
}

func (h *PipelineHandler) runPipeline(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		PipelineID    string `json:"pipeline_id"`
		WorkspaceName string `json:"workspace_name"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.runPipeline: %w", err)
	}

	if args.PipelineID == "" || args.WorkspaceName == "" {
		return types.NewErrorResult("pipeline_id and workspace_name are required"), nil
	}

	if h.store.Pipelines == nil {
		return types.NewErrorResult("pipeline repository not available"), nil
	}

	// Verify pipeline exists before creating a job.
	_, err := h.store.Pipelines.GetPipeline(ctx, args.PipelineID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("pipeline not found: %v", err)), nil
	}

	jobID, err := h.store.Pipelines.CreateJob(ctx, args.PipelineID, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("create job: %v", err)), nil
	}

	// Resolve workspace root directory for command execution.
	workDir, err := h.resolveWorkDir(ctx, args.WorkspaceName)
	if err != nil {
		h.logWarn("could not resolve workspace root, using current directory",
			"workspace", args.WorkspaceName, "error", err)
		workDir = "."
	}

	// Launch execution asynchronously.
	if h.executor != nil {
		go func() {
			if execErr := h.executor.Execute(context.Background(), jobID, workDir); execErr != nil {
				h.logError("pipeline execution failed",
					"job_id", jobID, "error", execErr)
			}
		}()
	}

	return types.NewToolResult(map[string]string{
		"job_id":  jobID,
		"status":  "pending",
		"message": "Pipeline job created and execution started.",
	}), nil
}

// resolveWorkDir looks up the workspace's root path from the workspace repository.
func (h *PipelineHandler) resolveWorkDir(ctx context.Context, workspaceName string) (string, error) {
	if h.store.Workspaces == nil {
		return "", fmt.Errorf("workspace repository not available")
	}
	ws, err := h.store.Workspaces.GetWorkspace(ctx, workspaceName)
	if err != nil {
		return "", fmt.Errorf("get workspace %q: %w", workspaceName, err)
	}
	if ws.RootPath == "" {
		return "", fmt.Errorf("workspace %q has no root path", workspaceName)
	}
	return ws.RootPath, nil
}

// logWarn writes a warning log if a logger is available.
func (h *PipelineHandler) logWarn(msg string, args ...any) {
	if h.logger != nil {
		h.logger.Warn(msg, args...)
	}
}

// logError writes an error log if a logger is available.
func (h *PipelineHandler) logError(msg string, args ...any) {
	if h.logger != nil {
		h.logger.Error(msg, args...)
	}
}

func (h *PipelineHandler) pipelineJobStatus(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.pipelineJobStatus: %w", err)
	}

	if args.JobID == "" {
		return types.NewErrorResult("job_id is required"), nil
	}

	if h.store.Pipelines == nil {
		return types.NewErrorResult("pipeline repository not available"), nil
	}

	job, err := h.store.Pipelines.GetJob(ctx, args.JobID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get job: %v", err)), nil
	}

	steps, err := h.store.Pipelines.ListStepResults(ctx, args.JobID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("list step results: %v", err)), nil
	}

	type stepSummary struct {
		StepName string `json:"step_name"`
		Status   string `json:"status"`
		ExitCode *int   `json:"exit_code,omitempty"`
		Error    string `json:"error,omitempty"`
	}

	stepSummaries := make([]stepSummary, len(steps))
	for i, s := range steps {
		stepSummaries[i] = stepSummary{
			StepName: s.StepName,
			Status:   s.Status,
			ExitCode: s.ExitCode,
			Error:    s.Error,
		}
	}

	result := map[string]any{
		"job_id": job.ID,
		"status": job.Status,
		"error":  job.Error,
		"steps":  stepSummaries,
	}
	return types.NewToolResult(result), nil
}

func (h *PipelineHandler) pipelineJobLog(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		JobID string `json:"job_id"`
		Tail  int    `json:"tail"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.pipelineJobLog: %w", err)
	}

	if args.JobID == "" {
		return types.NewErrorResult("job_id is required"), nil
	}

	if h.store.Pipelines == nil {
		return types.NewErrorResult("pipeline repository not available"), nil
	}

	steps, err := h.store.Pipelines.ListStepResults(ctx, args.JobID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("list step results: %v", err)), nil
	}

	if len(steps) == 0 {
		return types.NewToolResult("No step results yet."), nil
	}

	type stepOutput struct {
		ID          string  `json:"id"`
		JobID       string  `json:"job_id"`
		SwimlaneID  string  `json:"swimlane_id"`
		StepID      string  `json:"step_id"`
		StepName    string  `json:"step_name"`
		Status      string  `json:"status"`
		ExitCode    *int    `json:"exit_code"`
		StartedAt   *string `json:"started_at,omitempty"`
		CompletedAt *string `json:"completed_at,omitempty"`
		DurationMS  *int    `json:"duration_ms,omitempty"`
		OutputLog   string  `json:"output_log"`
		Error       string  `json:"error,omitempty"`
	}

	results := make([]stepOutput, len(steps))
	for i, s := range steps {
		outLog := s.OutputLog
		// Read from file if output was persisted to disk.
		if filePath, ok := strings.CutPrefix(outLog, "file:"); ok {
			if data, readErr := os.ReadFile(filePath); readErr == nil {
				outLog = string(data)
			} else {
				outLog = fmt.Sprintf("[log file not found: %s]", filePath)
			}
		}

		// Apply tail filtering if requested.
		if args.Tail > 0 && outLog != "" {
			outLog = tailLines(outLog, args.Tail)
		}

		so := stepOutput{
			ID:         s.ID,
			JobID:      s.JobID,
			SwimlaneID: s.SwimlaneID,
			StepID:     s.StepID,
			StepName:   s.StepName,
			Status:     s.Status,
			ExitCode:   s.ExitCode,
			OutputLog:  outLog,
			Error:      s.Error,
		}
		if s.StartedAt != nil {
			ts := s.StartedAt.Format("2006-01-02 15:04:05")
			so.StartedAt = &ts
		}
		if s.CompletedAt != nil {
			ts := s.CompletedAt.Format("2006-01-02 15:04:05")
			so.CompletedAt = &ts
		}
		if s.DurationMS != nil {
			so.DurationMS = s.DurationMS
		}
		results[i] = so
	}

	return types.NewToolResult(results), nil
}

func (h *PipelineHandler) listPipelineJobs(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		PipelineID string `json:"pipeline_id"`
		Status     string `json:"status"`
		Limit      int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.listPipelineJobs: %w", err)
	}

	if args.PipelineID == "" {
		return types.NewErrorResult("pipeline_id is required"), nil
	}

	if h.store.Pipelines == nil {
		return types.NewErrorResult("pipeline repository not available"), nil
	}

	filter := repo.JobFilter{
		Status: args.Status,
		Limit:  args.Limit,
	}

	jobs, err := h.store.Pipelines.ListJobsFiltered(ctx, args.PipelineID, filter)
	if err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.listPipelineJobs: %w", err)
	}

	if len(jobs) == 0 {
		return types.NewToolResult("No jobs found."), nil
	}

	type jobSummary struct {
		ID          string  `json:"id"`
		Status      string  `json:"status"`
		StartedAt   *string `json:"started_at,omitempty"`
		CompletedAt *string `json:"completed_at,omitempty"`
		Error       string  `json:"error,omitempty"`
	}

	summaries := make([]jobSummary, len(jobs))
	for i, j := range jobs {
		s := jobSummary{
			ID:     j.ID,
			Status: j.Status,
			Error:  j.Error,
		}
		if j.StartedAt != nil {
			ts := j.StartedAt.Format("2006-01-02 15:04:05")
			s.StartedAt = &ts
		}
		if j.CompletedAt != nil {
			ts := j.CompletedAt.Format("2006-01-02 15:04:05")
			s.CompletedAt = &ts
		}
		summaries[i] = s
	}
	return types.NewToolResult(summaries), nil
}

func (h *PipelineHandler) cancelPipelineJob(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.cancelPipelineJob: %w", err)
	}

	if args.JobID == "" {
		return types.NewErrorResult("job_id is required"), nil
	}

	if h.store.Pipelines == nil {
		return types.NewErrorResult("pipeline repository not available"), nil
	}

	// Verify job exists and is cancellable.
	job, err := h.store.Pipelines.GetJob(ctx, args.JobID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get job: %v", err)), nil
	}

	if job.Status != "pending" && job.Status != "running" {
		return types.NewErrorResult(fmt.Sprintf("job is already %s, cannot cancel", job.Status)), nil
	}

	// Signal the executor to abort the running process, if one exists.
	if h.executor != nil {
		h.executor.CancelJob(args.JobID)
	}

	if err := h.store.Pipelines.UpdateJobStatus(ctx, args.JobID, "cancelled", ""); err != nil {
		return types.NewErrorResult(fmt.Sprintf("cancel job: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"job_id":  args.JobID,
		"status":  "cancelled",
		"message": "Job cancelled.",
	}), nil
}

func (h *PipelineHandler) discoverPipelines(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Query       string `json:"query"`
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.discoverPipelines: %w", err)
	}

	if h.store.Pipelines == nil {
		return types.NewErrorResult("pipeline repository not available"), nil
	}

	pipelines, err := h.store.Pipelines.SearchPipelines(ctx, args.Query, args.WorkspaceID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("search pipelines: %v", err)), nil
	}

	if len(pipelines) == 0 {
		return types.NewToolResult("No pipelines found."), nil
	}

	type pipelineResult struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Description   string `json:"description,omitempty"`
		WorkspaceName string `json:"workspace_name"`
		ProjectName   string `json:"project_name,omitempty"`
	}

	results := make([]pipelineResult, len(pipelines))
	for i, p := range pipelines {
		results[i] = pipelineResult{
			ID:            p.ID,
			Name:          p.Name,
			Description:   p.Description,
			WorkspaceName: p.WorkspaceName,
			ProjectName:   p.ProjectName,
		}
	}
	return types.NewToolResult(results), nil
}

func (h *PipelineHandler) assignPipeline(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		PipelineID  string `json:"pipeline_id"`
		WorkspaceID string `json:"workspace_id"`
		ProjectID   string `json:"project_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.assignPipeline: %w", err)
	}

	if args.PipelineID == "" || args.WorkspaceID == "" {
		return types.NewErrorResult("pipeline_id and workspace_id are required"), nil
	}

	if h.store.Pipelines == nil {
		return types.NewErrorResult("pipeline repository not available"), nil
	}

	// Verify the pipeline exists before creating the assignment.
	_, err := h.store.Pipelines.GetPipeline(ctx, args.PipelineID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("pipeline not found: %v", err)), nil
	}

	assignment := &repo.PipelineAssignment{
		PipelineID:  args.PipelineID,
		WorkspaceID: args.WorkspaceID,
		ProjectID:   args.ProjectID,
	}

	id, err := h.store.Pipelines.CreateAssignment(ctx, assignment)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("create assignment: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"id":      id,
		"message": fmt.Sprintf("Pipeline assigned to workspace %q.", args.WorkspaceID),
	}), nil
}

func (h *PipelineHandler) listPipelineAssignments(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceID string `json:"workspace_id"`
		PipelineID  string `json:"pipeline_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.listPipelineAssignments: %w", err)
	}

	if h.store.Pipelines == nil {
		return types.NewErrorResult("pipeline repository not available"), nil
	}

	assignments, err := h.store.Pipelines.ListAssignments(ctx, args.WorkspaceID, args.PipelineID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("list assignments: %v", err)), nil
	}

	if len(assignments) == 0 {
		return types.NewToolResult("No assignments found."), nil
	}

	type assignmentSummary struct {
		ID          string `json:"id"`
		PipelineID  string `json:"pipeline_id"`
		WorkspaceID string `json:"workspace_id"`
		ProjectID   string `json:"project_id,omitempty"`
		AssignedAt  string `json:"assigned_at"`
	}

	summaries := make([]assignmentSummary, len(assignments))
	for i, a := range assignments {
		summaries[i] = assignmentSummary{
			ID:          a.ID,
			PipelineID:  a.PipelineID,
			WorkspaceID: a.WorkspaceID,
			ProjectID:   a.ProjectID,
			AssignedAt:  a.AssignedAt.Format("2006-01-02 15:04:05"),
		}
	}
	return types.NewToolResult(summaries), nil
}

// ── Workflow actions ────────────────────────────────────────────────────────

func (h *PipelineHandler) createWorkflow(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Steps       []struct {
			Name             string          `json:"name"`
			StepType         string          `json:"step_type"`
			Action           json.RawMessage `json:"action"`
			DependsOn        string          `json:"depends_on"`
			Condition        string          `json:"condition"`
			RequiresApproval bool            `json:"requires_approval"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.createWorkflow: %w", err)
	}
	if args.Name == "" {
		return types.NewErrorResult("name is required"), nil
	}

	if h.store.Workflows == nil {
		return types.NewErrorResult("workflow repo not available"), nil
	}

	// Create the workflow definition.
	wf := &repo.Workflow{
		Name:        args.Name,
		Description: args.Description,
		Enabled:     true,
	}
	wfID, err := h.store.Workflows.CreateWorkflow(ctx, wf)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("create workflow: %v", err)), nil
	}

	// Create steps. We need to map step names to IDs for dependency resolution.
	stepNameToID := make(map[string]string, len(args.Steps))
	var stepIDs []string

	// First pass: create steps to get IDs.
	for i, s := range args.Steps {
		stepType := s.StepType
		if stepType == "" {
			stepType = "tool"
		}

		action := s.Action
		if len(action) == 0 {
			action = json.RawMessage("{}")
		}

		step := &repo.WorkflowStep{
			WorkflowID:       wfID,
			Name:             s.Name,
			StepType:         stepType,
			Action:           action,
			DependsOn:        "", // Resolved in second pass.
			Condition:        s.Condition,
			RequiresApproval: s.RequiresApproval,
			Position:         i,
		}
		stepID, err := h.store.Workflows.CreateStep(ctx, step)
		if err != nil {
			return types.NewErrorResult(fmt.Sprintf("create step %q: %v", s.Name, err)), nil
		}
		stepNameToID[s.Name] = stepID
		stepIDs = append(stepIDs, stepID)
	}

	// Second pass: resolve dependency names to IDs and update steps.
	for i, s := range args.Steps {
		if s.DependsOn == "" {
			continue
		}

		depNames := strings.Split(s.DependsOn, ",")
		var depIDs []string
		for _, name := range depNames {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			depID, ok := stepNameToID[name]
			if !ok {
				return types.NewErrorResult(fmt.Sprintf("step %q depends on unknown step %q", s.Name, name)), nil
			}
			depIDs = append(depIDs, depID)
		}

		if len(depIDs) > 0 {
			step := &repo.WorkflowStep{
				ID:               stepIDs[i],
				WorkflowID:       wfID,
				Name:             s.Name,
				StepType:         args.Steps[i].StepType,
				Action:           args.Steps[i].Action,
				DependsOn:        strings.Join(depIDs, ","),
				Condition:        s.Condition,
				RequiresApproval: s.RequiresApproval,
				Position:         i,
			}
			if step.StepType == "" {
				step.StepType = "tool"
			}
			if len(step.Action) == 0 {
				step.Action = json.RawMessage("{}")
			}
			if err := h.store.Workflows.UpdateStep(ctx, step); err != nil {
				return types.NewErrorResult(fmt.Sprintf("update step deps: %v", err)), nil
			}
		}
	}

	return types.NewToolResult(map[string]interface{}{
		"id":      wfID,
		"name":    args.Name,
		"steps":   len(args.Steps),
		"message": fmt.Sprintf("Workflow %q created with %d steps.", args.Name, len(args.Steps)),
	}), nil
}

func (h *PipelineHandler) listWorkflows(ctx context.Context, _ json.RawMessage) (*types.ToolResult, error) {
	if h.store.Workflows == nil {
		return types.NewErrorResult("workflow repo not available"), nil
	}

	workflows, err := h.store.Workflows.ListWorkflows(ctx)
	if err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.listWorkflows: %w", err)
	}

	if len(workflows) == 0 {
		return types.NewToolResult([]interface{}{}), nil
	}

	type workflowSummary struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Enabled     bool   `json:"enabled"`
	}

	summaries := make([]workflowSummary, len(workflows))
	for i, wf := range workflows {
		summaries[i] = workflowSummary{
			ID:          wf.ID,
			Name:        wf.Name,
			Description: wf.Description,
			Enabled:     wf.Enabled,
		}
	}
	return types.NewToolResult(summaries), nil
}

func (h *PipelineHandler) runWorkflow(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkflowID string                 `json:"workflow_id"`
		Context    map[string]interface{} `json:"context"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.runWorkflow: %w", err)
	}
	if args.WorkflowID == "" {
		return types.NewErrorResult("workflow_id is required"), nil
	}

	if h.wfExecutor == nil {
		return types.NewErrorResult("workflow executor not available"), nil
	}

	runContext := args.Context
	if runContext == nil {
		runContext = make(map[string]interface{})
	}

	runID, err := h.wfExecutor.StartRun(ctx, args.WorkflowID, runContext)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("start workflow run: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"run_id":  runID,
		"message": fmt.Sprintf("Workflow run started (run_id=%s).", runID),
	}), nil
}

func (h *PipelineHandler) getWorkflowStatus(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.getWorkflowStatus: %w", err)
	}
	if args.RunID == "" {
		return types.NewErrorResult("run_id is required"), nil
	}

	if h.store.Workflows == nil {
		return types.NewErrorResult("workflow repo not available"), nil
	}

	run, err := h.store.Workflows.GetRun(ctx, args.RunID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get run: %v", err)), nil
	}

	runSteps, err := h.store.Workflows.GetRunSteps(ctx, args.RunID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get run steps: %v", err)), nil
	}

	// Build a step ID -> step name lookup.
	steps, err := h.store.Workflows.GetSteps(ctx, run.WorkflowID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get steps: %v", err)), nil
	}
	stepNames := make(map[string]string, len(steps))
	for _, s := range steps {
		stepNames[s.ID] = s.Name
	}

	// Build response.
	type stepStatus struct {
		StepID   string `json:"step_id"`
		StepName string `json:"step_name"`
		Status   string `json:"status"`
		Output   string `json:"output,omitempty"`
		Error    string `json:"error,omitempty"`
	}

	stepStatuses := make([]stepStatus, 0, len(runSteps))
	for _, rs := range runSteps {
		stepStatuses = append(stepStatuses, stepStatus{
			StepID:   rs.StepID,
			StepName: stepNames[rs.StepID],
			Status:   rs.Status,
			Output:   rs.Output,
			Error:    rs.Error,
		})
	}

	result := map[string]interface{}{
		"run_id":      run.ID,
		"workflow_id": run.WorkflowID,
		"status":      run.Status,
		"error":       run.Error,
		"steps":       stepStatuses,
	}

	return types.NewToolResult(result), nil
}

func (h *PipelineHandler) approveWorkflowStep(_ context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		RunID  string `json:"run_id"`
		StepID string `json:"step_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.approveWorkflowStep: %w", err)
	}
	if args.RunID == "" {
		return types.NewErrorResult("run_id is required"), nil
	}
	if args.StepID == "" {
		return types.NewErrorResult("step_id is required"), nil
	}

	if h.wfExecutor == nil {
		return types.NewErrorResult("workflow executor not available"), nil
	}

	if err := h.wfExecutor.Approval().ApproveStep(args.RunID, args.StepID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("approve step: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"message": fmt.Sprintf("Step %q approved for run %q.", args.StepID, args.RunID),
	}), nil
}

func (h *PipelineHandler) cancelWorkflowRun(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.cancelWorkflowRun: %w", err)
	}
	if args.RunID == "" {
		return types.NewErrorResult("run_id is required"), nil
	}

	if h.wfExecutor == nil {
		return types.NewErrorResult("workflow executor not available"), nil
	}

	if err := h.wfExecutor.CancelRun(ctx, args.RunID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("cancel run: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"message": fmt.Sprintf("Workflow run %q cancelled.", args.RunID),
	}), nil
}

// ── Cron actions ────────────────────────────────────────────────────────────

func (h *PipelineHandler) createCronJob(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Name       string `json:"name"`
		Schedule   string `json:"schedule"`
		JobType    string `json:"job_type"`
		Payload    string `json:"payload"`
		MaxRetries *int   `json:"max_retries"`
		Enabled    *bool  `json:"enabled"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.createCronJob: %w", err)
	}

	if args.Name == "" || args.Schedule == "" {
		return types.NewErrorResult("name and schedule are required"), nil
	}

	if h.store.Cron == nil {
		return types.NewErrorResult("cron repository not available"), nil
	}

	// Validate the cron expression parses successfully.
	sched, err := cron.Parse(args.Schedule)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("invalid schedule: %v", err)), nil
	}

	jobType := args.JobType
	if jobType == "" {
		jobType = "tool"
	}

	payload := json.RawMessage("{}")
	if args.Payload != "" {
		if !json.Valid([]byte(args.Payload)) {
			return types.NewErrorResult("payload must be valid JSON"), nil
		}
		payload = json.RawMessage(args.Payload)
	}

	maxRetries := 3
	if args.MaxRetries != nil {
		maxRetries = *args.MaxRetries
	}

	enabled := true
	if args.Enabled != nil {
		enabled = *args.Enabled
	}

	// Compute the initial next_run_at.
	now := timeNow()
	nextRun := sched.NextAfter(now)

	job := &repo.CronJob{
		Name:       args.Name,
		Schedule:   args.Schedule,
		JobType:    jobType,
		Payload:    payload,
		Enabled:    enabled,
		MaxRetries: maxRetries,
		NextRunAt:  &nextRun,
	}

	id, err := h.store.Cron.CreateJob(ctx, job)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("create cron job: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"id":      id,
		"message": fmt.Sprintf("Cron job %q created with schedule %q.", args.Name, args.Schedule),
	}), nil
}

func (h *PipelineHandler) listCronJobs(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	if h.store.Cron == nil {
		return types.NewErrorResult("cron repository not available"), nil
	}

	jobs, err := h.store.Cron.ListJobs(ctx)
	if err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.listCronJobs: %w", err)
	}

	if len(jobs) == 0 {
		return types.NewToolResult([]interface{}{}), nil
	}

	type cronJobSummary struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Schedule   string `json:"schedule"`
		JobType    string `json:"job_type"`
		Enabled    bool   `json:"enabled"`
		NextRunAt  string `json:"next_run_at,omitempty"`
		LastRunAt  string `json:"last_run_at,omitempty"`
		LastStatus string `json:"last_status,omitempty"`
	}

	summaries := make([]cronJobSummary, len(jobs))
	for i, j := range jobs {
		s := cronJobSummary{
			ID:         j.ID,
			Name:       j.Name,
			Schedule:   j.Schedule,
			JobType:    j.JobType,
			Enabled:    j.Enabled,
			LastStatus: j.LastStatus,
		}
		if j.NextRunAt != nil {
			s.NextRunAt = j.NextRunAt.Format(time.RFC3339)
		}
		if j.LastRunAt != nil {
			s.LastRunAt = j.LastRunAt.Format(time.RFC3339)
		}
		summaries[i] = s
	}
	return types.NewToolResult(summaries), nil
}

func (h *PipelineHandler) updateCronJob(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Schedule   string `json:"schedule"`
		JobType    string `json:"job_type"`
		Payload    string `json:"payload"`
		MaxRetries *int   `json:"max_retries"`
		Enabled    *bool  `json:"enabled"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.updateCronJob: %w", err)
	}

	if args.ID == "" {
		return types.NewErrorResult("id is required"), nil
	}

	if h.store.Cron == nil {
		return types.NewErrorResult("cron repository not available"), nil
	}

	// Fetch the existing job to merge fields.
	existing, err := h.store.Cron.GetJob(ctx, args.ID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("cron job not found: %v", err)), nil
	}

	if args.Name != "" {
		existing.Name = args.Name
	}
	if args.Schedule != "" {
		// Validate the new schedule.
		sched, err := cron.Parse(args.Schedule)
		if err != nil {
			return types.NewErrorResult(fmt.Sprintf("invalid schedule: %v", err)), nil
		}
		existing.Schedule = args.Schedule

		// Recompute next_run_at for the new schedule.
		nextRun := sched.NextAfter(timeNow())
		existing.NextRunAt = &nextRun
	}
	if args.JobType != "" {
		existing.JobType = args.JobType
	}
	if args.Payload != "" {
		if !json.Valid([]byte(args.Payload)) {
			return types.NewErrorResult("payload must be valid JSON"), nil
		}
		existing.Payload = json.RawMessage(args.Payload)
	}
	if args.MaxRetries != nil {
		existing.MaxRetries = *args.MaxRetries
	}
	if args.Enabled != nil {
		existing.Enabled = *args.Enabled
	}

	if err := h.store.Cron.UpdateJob(ctx, existing); err != nil {
		return types.NewErrorResult(fmt.Sprintf("update cron job: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"id":     existing.ID,
		"name":   existing.Name,
		"status": "updated",
	}), nil
}

func (h *PipelineHandler) deleteCronJob(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.deleteCronJob: %w", err)
	}

	if args.ID == "" {
		return types.NewErrorResult("id is required"), nil
	}

	if h.store.Cron == nil {
		return types.NewErrorResult("cron repository not available"), nil
	}

	if err := h.store.Cron.DeleteJob(ctx, args.ID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("delete cron job: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"id":     args.ID,
		"status": "deleted",
	}), nil
}

func (h *PipelineHandler) getCronHistory(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ID    string `json:"id"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.getCronHistory: %w", err)
	}

	if args.ID == "" {
		return types.NewErrorResult("id is required"), nil
	}

	if h.store.Cron == nil {
		return types.NewErrorResult("cron repository not available"), nil
	}

	limit := args.Limit
	if limit <= 0 {
		limit = 10
	}

	execs, err := h.store.Cron.GetHistory(ctx, args.ID, limit)
	if err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.getCronHistory: %w", err)
	}

	if len(execs) == 0 {
		return types.NewToolResult([]interface{}{}), nil
	}

	type execSummary struct {
		ID         string `json:"id"`
		Status     string `json:"status"`
		StartedAt  string `json:"started_at"`
		FinishedAt string `json:"finished_at,omitempty"`
		DurationMS int64  `json:"duration_ms,omitempty"`
		Error      string `json:"error,omitempty"`
		Attempt    int    `json:"attempt"`
	}

	summaries := make([]execSummary, len(execs))
	for i, e := range execs {
		s := execSummary{
			ID:         e.ID,
			Status:     e.Status,
			StartedAt:  e.StartedAt.Format("2006-01-02 15:04:05"),
			DurationMS: e.DurationMS,
			Error:      e.Error,
			Attempt:    e.Attempt,
		}
		if e.FinishedAt != nil {
			s.FinishedAt = e.FinishedAt.Format("2006-01-02 15:04:05")
		}
		summaries[i] = s
	}
	return types.NewToolResult(summaries), nil
}

func (h *PipelineHandler) triggerCronJob(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PipelineHandler.triggerCronJob: %w", err)
	}

	if args.ID == "" {
		return types.NewErrorResult("id is required"), nil
	}

	if h.store.Cron == nil {
		return types.NewErrorResult("cron repository not available"), nil
	}

	if h.scheduler == nil {
		return types.NewErrorResult("cron scheduler not available"), nil
	}

	if err := h.scheduler.DispatchJob(ctx, args.ID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("trigger cron job: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"id":      args.ID,
		"message": "Cron job triggered immediately.",
	}), nil
}

// ── Shared helpers ──────────────────────────────────────────────────────────

// tailLines returns the last n lines from the input string.
func tailLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if n >= len(lines) {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n") + "\n"
}

// defaultIfEmpty returns val if non-empty, otherwise returns def.
func defaultIfEmpty(val, def string) string {
	if val == "" {
		return def
	}
	return val
}

// timeNow is a package-level variable to allow test overrides.
var timeNow = func() time.Time { return time.Now() }
