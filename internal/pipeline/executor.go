package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// swimlane is a sequential group of steps parsed from a pipeline's JSON definition.
type swimlane struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Steps []step `json:"steps"`
}

// step is a single command within a swimlane.
type step struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Command string `json:"command"`
}

// Executor runs pipeline jobs by executing shell commands defined in
// the pipeline's swimlane JSON. Each job runs sequentially through
// swimlanes, and each swimlane runs its steps sequentially.
// Any non-zero exit code from a step halts execution and marks the job as failed.
type Executor struct {
	repo   repo.PipelineRepo
	bus    *nervous.EventBus
	logger *slog.Logger

	// mu protects the cancels map for concurrent goroutine access.
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

// NewExecutor creates a pipeline Executor wired to the given repository,
// event bus, and logger.
func NewExecutor(pipelineRepo repo.PipelineRepo, bus *nervous.EventBus, logger *slog.Logger) *Executor {
	return &Executor{
		repo:    pipelineRepo,
		bus:     bus,
		logger:  logger,
		cancels: make(map[string]context.CancelFunc),
	}
}

// Execute loads the pipeline definition for the given job and runs every
// swimlane and step in sequence. It updates the job status in the repository
// and publishes events to the Nervous System bus throughout execution.
//
// workDir is the filesystem directory used as the working directory for all
// shell commands. Callers should set this to the workspace root.
//
// The method is safe to call from a goroutine. It stores a cancel function
// so that CancelJob can abort an in-progress execution.
func (e *Executor) Execute(ctx context.Context, jobID string, workDir string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	e.trackCancel(jobID, cancel)
	defer e.removeCancel(jobID)

	// Load job and pipeline definition.
	job, err := e.repo.GetJob(ctx, jobID)
	if err != nil {
		return fmt.Errorf("pipeline.Executor.Execute: load job %q: %w", jobID, err)
	}

	pipeline, err := e.repo.GetPipeline(ctx, job.PipelineID)
	if err != nil {
		return fmt.Errorf("pipeline.Executor.Execute: load pipeline for job %q: %w", jobID, err)
	}

	// Mark job as running.
	if err := e.repo.UpdateJobStatus(ctx, jobID, "running", ""); err != nil {
		return fmt.Errorf("pipeline.Executor.Execute: mark job running: %w", err)
	}

	e.publishEvent(types.EventPipelineStart, pipeline.WorkspaceName, map[string]string{
		"job_id":      jobID,
		"pipeline_id": pipeline.ID,
		"pipeline":    pipeline.Name,
	})

	e.logger.Info("pipeline job started",
		"job_id", jobID,
		"pipeline", pipeline.Name,
		"work_dir", workDir,
	)

	// Parse environment variables.
	env, err := parseEnvironment(pipeline.Environment)
	if err != nil {
		return e.failJob(ctx, jobID, pipeline.WorkspaceName, fmt.Errorf("parse environment: %w", err))
	}

	// Run setup commands first.
	if err := e.runSetupCommands(ctx, jobID, pipeline, workDir, env); err != nil {
		return e.failJob(ctx, jobID, pipeline.WorkspaceName, err)
	}

	// Parse and run swimlanes.
	swimlanes, err := parseSwimlanes(pipeline.Swimlanes)
	if err != nil {
		return e.failJob(ctx, jobID, pipeline.WorkspaceName, fmt.Errorf("parse swimlanes: %w", err))
	}

	for _, sl := range swimlanes {
		for _, st := range sl.Steps {
			if err := e.executeStep(ctx, jobID, sl.ID, st, workDir, env); err != nil {
				return e.failJob(ctx, jobID, pipeline.WorkspaceName, err)
			}
		}
	}

	// Mark job completed.
	if err := e.repo.UpdateJobStatus(ctx, jobID, "completed", "all steps passed"); err != nil {
		return fmt.Errorf("pipeline.Executor.Execute: mark job completed: %w", err)
	}

	e.publishEvent(types.EventPipelineComplete, pipeline.WorkspaceName, map[string]string{
		"job_id":      jobID,
		"pipeline_id": pipeline.ID,
		"pipeline":    pipeline.Name,
		"status":      "completed",
	})

	e.logger.Info("pipeline job completed",
		"job_id", jobID,
		"pipeline", pipeline.Name,
	)

	return nil
}

// CancelJob aborts a running job by cancelling its context.
// Returns true if a running execution was found and cancelled.
func (e *Executor) CancelJob(jobID string) bool {
	e.mu.Lock()
	cancel, ok := e.cancels[jobID]
	e.mu.Unlock()

	if ok {
		cancel()
	}
	return ok
}

// runSetupCommands parses and executes the setup_commands JSON array.
// Each setup command runs as a virtual step in the "_setup" swimlane.
func (e *Executor) runSetupCommands(ctx context.Context, jobID string, pipeline *repo.Pipeline, workDir string, env []string) error {
	var commands []string
	if err := json.Unmarshal([]byte(pipeline.SetupCommands), &commands); err != nil {
		return fmt.Errorf("pipeline.Executor.runSetupCommands: parse setup_commands: %w", err)
	}

	for i, cmd := range commands {
		st := step{
			ID:      fmt.Sprintf("setup_%d", i),
			Name:    fmt.Sprintf("setup: %s", cmd),
			Command: cmd,
		}
		if err := e.executeStep(ctx, jobID, "_setup", st, workDir, env); err != nil {
			return fmt.Errorf("pipeline.Executor.runSetupCommands: %w", err)
		}
	}
	return nil
}

// executeStep runs a single command, records the StepResult in the repository,
// and publishes a pipeline.log event on completion.
func (e *Executor) executeStep(ctx context.Context, jobID, swimlaneID string, st step, workDir string, env []string) error {
	startedAt := time.Now()

	e.logger.Info("step starting",
		"job_id", jobID,
		"swimlane", swimlaneID,
		"step", st.Name,
		"command", st.Command,
	)

	cmd := exec.CommandContext(ctx, "sh", "-c", st.Command)
	cmd.Dir = workDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// Kill the entire process group so child processes (e.g., sleep)
		// are cleaned up when the context is cancelled.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	if len(env) > 0 {
		cmd.Env = env
	}

	output, cmdErr := cmd.CombinedOutput()

	// Write step output to a log file on disk to keep the DB lean.
	logDir := filepath.Join(workDir, ".hyperax", "logs", "pipeline", jobID)
	logPath := filepath.Join(logDir, st.ID+".log")
	outputRef := string(output) // fallback: store raw output
	if mkErr := os.MkdirAll(logDir, 0o755); mkErr != nil {
		e.logger.Warn("failed to create pipeline log directory", "path", logDir, "error", mkErr)
	} else if wErr := os.WriteFile(logPath, output, 0o644); wErr != nil {
		e.logger.Warn("failed to write pipeline step log", "path", logPath, "error", wErr)
	} else {
		outputRef = "file:" + logPath
	}

	completedAt := time.Now()
	durationMS := int(completedAt.Sub(startedAt).Milliseconds())

	exitCode := 0
	status := "passed"
	var stepError string

	if cmdErr != nil {
		status = "failed"
		stepError = cmdErr.Error()
		if exitErr, ok := cmdErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			// Non-exit errors (e.g. context cancelled, binary not found).
			exitCode = -1
		}
	}

	result := &repo.StepResult{
		ID:          uuid.New().String(),
		JobID:       jobID,
		SwimlaneID:  swimlaneID,
		StepID:      st.ID,
		StepName:    st.Name,
		Status:      status,
		ExitCode:    &exitCode,
		StartedAt:   &startedAt,
		CompletedAt: &completedAt,
		DurationMS:  &durationMS,
		OutputLog:   outputRef,
		Error:       stepError,
	}

	// Use a background context for recording the result so that a cancelled
	// parent context does not prevent persisting the step outcome.
	if _, err := e.repo.CreateStepResult(context.Background(), result); err != nil {
		e.logger.Error("failed to record step result",
			"job_id", jobID,
			"step", st.Name,
			"error", err,
		)
	}

	e.publishEvent(types.EventPipelineLog, "", map[string]any{
		"job_id":     jobID,
		"swimlane":   swimlaneID,
		"step":       st.Name,
		"status":     status,
		"exit_code":  exitCode,
		"duration_ms": durationMS,
	})

	e.logger.Info("step completed",
		"job_id", jobID,
		"step", st.Name,
		"status", status,
		"exit_code", exitCode,
		"duration_ms", durationMS,
	)

	if cmdErr != nil {
		return fmt.Errorf("pipeline.Executor.executeStep: step %q failed (exit %d): %w", st.Name, exitCode, cmdErr)
	}
	return nil
}

// failJob marks a job as failed, publishes a completion event, and returns the
// original error for the caller. It uses a background context for the status
// update so that a cancelled parent context does not prevent recording the failure.
// Note: the original error is NOT logged here to avoid double-logging — callers
// are responsible for handling the returned error.
func (e *Executor) failJob(_ context.Context, jobID, scope string, originalErr error) error {
	errMsg := originalErr.Error()

	if updateErr := e.repo.UpdateJobStatus(context.Background(), jobID, "failed", errMsg); updateErr != nil {
		e.logger.Error("pipeline.Executor.failJob: failed to update job status",
			"job_id", jobID,
			"update_error", updateErr,
		)
	}

	e.publishEvent(types.EventPipelineComplete, scope, map[string]string{
		"job_id": jobID,
		"status": "failed",
		"error":  errMsg,
	})

	return originalErr
}

// publishEvent sends an event through the Nervous System bus if one is configured.
func (e *Executor) publishEvent(eventType types.EventType, scope string, payload any) {
	if e.bus == nil {
		return
	}
	e.bus.Publish(nervous.NewEvent(eventType, "pipeline.executor", scope, payload))
}

// trackCancel stores a cancel function so CancelJob can abort a running execution.
func (e *Executor) trackCancel(jobID string, cancel context.CancelFunc) {
	e.mu.Lock()
	e.cancels[jobID] = cancel
	e.mu.Unlock()
}

// removeCancel deletes the cancel function after a job finishes.
func (e *Executor) removeCancel(jobID string) {
	e.mu.Lock()
	delete(e.cancels, jobID)
	e.mu.Unlock()
}

// parseSwimlanes decodes the swimlanes JSON array from a pipeline definition.
func parseSwimlanes(raw string) ([]swimlane, error) {
	var lanes []swimlane
	if err := json.Unmarshal([]byte(raw), &lanes); err != nil {
		return nil, fmt.Errorf("pipeline.parseSwimlanes: %w", err)
	}
	return lanes, nil
}

// parseEnvironment decodes the environment JSON object from a pipeline definition
// and returns it as a slice of "KEY=VALUE" strings suitable for exec.Cmd.Env.
// If the JSON is empty or "{}", an empty slice is returned (meaning the parent
// process environment will be inherited).
func parseEnvironment(raw string) ([]string, error) {
	var envMap map[string]string
	if err := json.Unmarshal([]byte(raw), &envMap); err != nil {
		return nil, fmt.Errorf("pipeline.parseEnvironment: %w", err)
	}
	if len(envMap) == 0 {
		return nil, nil
	}

	env := make([]string, 0, len(envMap))
	for k, v := range envMap {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	return env, nil
}
