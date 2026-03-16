package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// Executor is the DAG-based workflow execution engine. It resolves step
// dependencies via topological sort, runs independent steps in parallel,
// evaluates CEL conditions before each step, and pauses at approval gates.
type Executor struct {
	repo     repo.WorkflowRepo
	bus      *nervous.EventBus
	logger   *slog.Logger
	cel      *CELEvaluator
	approval *ApprovalManager

	mu      sync.Mutex
	cancels map[string]context.CancelFunc // runID -> cancelFunc
}

// NewExecutor creates a workflow Executor with all required dependencies.
func NewExecutor(
	workflowRepo repo.WorkflowRepo,
	bus *nervous.EventBus,
	logger *slog.Logger,
) *Executor {
	return &Executor{
		repo:     workflowRepo,
		bus:      bus,
		logger:   logger,
		cel:      NewCELEvaluator(),
		approval: NewApprovalManager(bus, logger),
		cancels:  make(map[string]context.CancelFunc),
	}
}

// Approval returns the approval manager for external approval handling.
func (e *Executor) Approval() *ApprovalManager {
	return e.approval
}

// StartRun begins executing a workflow run asynchronously. It creates run step
// records for each step, then launches a background goroutine that walks the
// DAG. Returns the run ID immediately.
func (e *Executor) StartRun(ctx context.Context, workflowID string, runContext map[string]interface{}) (string, error) {
	// Fetch workflow and steps.
	wf, err := e.repo.GetWorkflow(ctx, workflowID)
	if err != nil {
		return "", fmt.Errorf("workflow.Executor.StartRun: %w", err)
	}
	if !wf.Enabled {
		return "", fmt.Errorf("workflow.Executor.StartRun: workflow %q is disabled", workflowID)
	}

	steps, err := e.repo.GetSteps(ctx, workflowID)
	if err != nil {
		return "", fmt.Errorf("workflow.Executor.StartRun: %w", err)
	}
	if len(steps) == 0 {
		return "", fmt.Errorf("workflow.Executor.StartRun: workflow %q has no steps", workflowID)
	}

	// Validate the DAG before creating the run.
	if err := validateDAG(steps); err != nil {
		return "", fmt.Errorf("workflow.Executor.StartRun: %w", err)
	}

	// Serialize run context.
	ctxJSON, err := json.Marshal(runContext)
	if err != nil {
		return "", fmt.Errorf("workflow.Executor.StartRun: %w", err)
	}

	// Create the run record.
	now := time.Now()
	run := &repo.WorkflowRun{
		WorkflowID: workflowID,
		Status:     types.WorkflowStatusPending,
		StartedAt:  &now,
		Context:    ctxJSON,
	}
	runID, err := e.repo.CreateRun(ctx, run)
	if err != nil {
		return "", fmt.Errorf("workflow.Executor.StartRun: %w", err)
	}

	// Create run step records for each step.
	runStepMap := make(map[string]string) // stepID -> runStepID
	for _, step := range steps {
		rs := &repo.WorkflowRunStep{
			RunID:  runID,
			StepID: step.ID,
			Status: types.StepStatusPending,
		}
		rsID, err := e.repo.CreateRunStep(ctx, rs)
		if err != nil {
			return "", fmt.Errorf("workflow.Executor.StartRun: %w", err)
		}
		runStepMap[step.ID] = rsID
	}

	// Mark run as running.
	if err := e.repo.UpdateRunStatus(ctx, runID, types.WorkflowStatusRunning, ""); err != nil {
		return "", fmt.Errorf("workflow.Executor.StartRun: %w", err)
	}

	// Publish workflow started event.
	e.publishEvent(types.EventWorkflowStarted, runID, workflowID, "")

	// Create a cancellable context for this run.
	runCtx, cancel := context.WithCancel(ctx)
	e.mu.Lock()
	e.cancels[runID] = cancel
	e.mu.Unlock()

	// Execute in background.
	go e.executeDAG(runCtx, runID, workflowID, steps, runStepMap, runContext)

	return runID, nil
}

// CancelRun cancels an active workflow run.
func (e *Executor) CancelRun(ctx context.Context, runID string) error {
	e.mu.Lock()
	cancel, ok := e.cancels[runID]
	if ok {
		delete(e.cancels, runID)
	}
	e.mu.Unlock()

	if !ok {
		return fmt.Errorf("workflow.Executor.CancelRun: run %q is not active", runID)
	}

	cancel()
	e.approval.CancelAll(runID)

	if err := e.repo.UpdateRunStatus(ctx, runID, types.WorkflowStatusCancelled, "cancelled by user"); err != nil {
		return fmt.Errorf("workflow.Executor.CancelRun: %w", err)
	}

	return nil
}

// executeDAG walks the step DAG in dependency order, running steps in parallel
// when their dependencies are satisfied.
func (e *Executor) executeDAG(
	ctx context.Context,
	runID string,
	workflowID string,
	steps []*repo.WorkflowStep,
	runStepMap map[string]string,
	runContext map[string]interface{},
) {
	defer func() {
		e.mu.Lock()
		delete(e.cancels, runID)
		e.mu.Unlock()
	}()

	// Build lookup maps.
	stepByID := make(map[string]*repo.WorkflowStep, len(steps))
	for _, s := range steps {
		stepByID[s.ID] = s
	}

	// Resolve execution order via topological sort.
	order, err := topoSort(steps)
	if err != nil {
		e.failRun(ctx, runID, workflowID, fmt.Sprintf("topo sort: %v", err))
		return
	}

	// Track completed steps and their statuses.
	var mu sync.Mutex
	completed := make(map[string]string)          // stepID -> status
	stepOutputs := make(map[string]string)         // stepID -> output
	runFailed := false

	// Process steps in topological order. Steps at the same dependency level
	// can execute in parallel.
	levels := groupByLevel(order, steps)

	for _, level := range levels {
		if runFailed {
			break
		}

		// Check for cancellation before each level.
		select {
		case <-ctx.Done():
			e.failRun(context.Background(), runID, workflowID, "cancelled")
			return
		default:
		}

		var wg sync.WaitGroup
		for _, stepID := range level {
			step := stepByID[stepID]
			runStepID := runStepMap[stepID]

			wg.Add(1)
			go func(step *repo.WorkflowStep, runStepID string) {
				defer wg.Done()

				status, output, stepErr := e.executeStep(ctx, runID, step, runStepID, runContext, stepOutputs, &mu)

				mu.Lock()
				completed[step.ID] = status
				if output != "" {
					stepOutputs[step.ID] = output
				}
				if status == types.StepStatusFailed {
					runFailed = true
				}
				mu.Unlock()

				_ = stepErr // Already logged and persisted in executeStep.
			}(step, runStepID)
		}
		wg.Wait()
	}

	// Determine final run status.
	if runFailed {
		e.failRun(ctx, runID, workflowID, "one or more steps failed")
		return
	}

	// Check cancellation one final time.
	select {
	case <-ctx.Done():
		e.failRun(context.Background(), runID, workflowID, "cancelled")
		return
	default:
	}

	// All steps completed (or were skipped) successfully.
	if err := e.repo.UpdateRunStatus(ctx, runID, types.WorkflowStatusCompleted, ""); err != nil {
		e.logger.Error("failed to update run status to completed",
			"run_id", runID, "error", err)
	}
	e.publishEvent(types.EventWorkflowCompleted, runID, workflowID, "")
}

// executeStep runs a single step, handling conditions, approvals, and status updates.
func (e *Executor) executeStep(
	ctx context.Context,
	runID string,
	step *repo.WorkflowStep,
	runStepID string,
	runContext map[string]interface{},
	stepOutputs map[string]string,
	mu *sync.Mutex,
) (string, string, error) {
	// Check if any dependency failed -- if so, skip this step.
	mu.Lock()
	deps := parseDependsOn(step.DependsOn)
	for _, depID := range deps {
		if status, ok := stepOutputs[depID]; ok && status == types.StepStatusFailed {
			mu.Unlock()
			if err := e.repo.UpdateRunStepStatus(ctx, runStepID, types.StepStatusSkipped, "", "dependency failed"); err != nil {
				e.logger.Error("failed to update workflow step status", "run_step_id", runStepID, "target_status", types.StepStatusSkipped, "error", err)
			}
			return types.StepStatusSkipped, "", nil
		}
	}
	mu.Unlock()

	// Evaluate CEL condition if present.
	if step.Condition != "" {
		mu.Lock()
		evalCtx := buildEvalContext(runContext, stepOutputs)
		mu.Unlock()

		result, err := e.cel.Evaluate(step.Condition, evalCtx)
		if err != nil {
			if updErr := e.repo.UpdateRunStepStatus(ctx, runStepID, types.StepStatusFailed, "", fmt.Sprintf("condition eval: %v", err)); updErr != nil {
				e.logger.Error("failed to update workflow step status", "run_step_id", runStepID, "target_status", types.StepStatusFailed, "error", updErr)
			}
			e.publishStepEvent(types.EventWorkflowStepDone, runID, step.ID, step.Name, types.StepStatusFailed)
			return types.StepStatusFailed, "", err
		}
		if !result {
			if err := e.repo.UpdateRunStepStatus(ctx, runStepID, types.StepStatusSkipped, "", "condition evaluated to false"); err != nil {
				e.logger.Error("failed to update workflow step status", "run_step_id", runStepID, "target_status", types.StepStatusSkipped, "error", err)
			}
			e.publishStepEvent(types.EventWorkflowStepDone, runID, step.ID, step.Name, types.StepStatusSkipped)
			return types.StepStatusSkipped, "", nil
		}
	}

	// Mark step as running.
	if err := e.repo.UpdateRunStepStatus(ctx, runStepID, types.StepStatusRunning, "", ""); err != nil {
		e.logger.Error("failed to update workflow step status", "run_step_id", runStepID, "target_status", types.StepStatusRunning, "error", err)
	}
	e.publishStepEvent(types.EventWorkflowStepStart, runID, step.ID, step.Name, types.StepStatusRunning)

	// Handle approval gate.
	if step.RequiresApproval {
		if err := e.repo.UpdateRunStepStatus(ctx, runStepID, types.StepStatusWaitingApproval, "", ""); err != nil {
			e.logger.Error("failed to update workflow step status", "run_step_id", runStepID, "target_status", types.StepStatusWaitingApproval, "error", err)
		}

		approvalCh := e.approval.WaitForApproval(runID, step.ID, step.Name)
		select {
		case <-approvalCh:
			// Approved, continue execution.
			if err := e.repo.UpdateRunStepStatus(ctx, runStepID, types.StepStatusRunning, "", ""); err != nil {
				e.logger.Error("failed to update workflow step status", "run_step_id", runStepID, "target_status", types.StepStatusRunning, "error", err)
			}
		case <-ctx.Done():
			if err := e.repo.UpdateRunStepStatus(ctx, runStepID, types.StepStatusFailed, "", "cancelled while waiting for approval"); err != nil {
				e.logger.Error("failed to update workflow step status", "run_step_id", runStepID, "target_status", types.StepStatusFailed, "error", err)
			}
			return types.StepStatusFailed, "", ctx.Err()
		}
	}

	// Execute the step action.
	output, err := e.runStepAction(ctx, step)
	if err != nil {
		errMsg := err.Error()
		if updErr := e.repo.UpdateRunStepStatus(ctx, runStepID, types.StepStatusFailed, output, errMsg); updErr != nil {
			e.logger.Error("failed to update workflow step status", "run_step_id", runStepID, "target_status", types.StepStatusFailed, "error", updErr)
		}
		e.publishStepEvent(types.EventWorkflowStepDone, runID, step.ID, step.Name, types.StepStatusFailed)
		e.logger.Error("workflow step failed",
			"run_id", runID, "step_id", step.ID, "step_name", step.Name, "error", errMsg)
		return types.StepStatusFailed, output, err
	}

	if err := e.repo.UpdateRunStepStatus(ctx, runStepID, types.StepStatusCompleted, output, ""); err != nil {
		e.logger.Error("failed to update workflow step status", "run_step_id", runStepID, "target_status", types.StepStatusCompleted, "error", err)
	}
	e.publishStepEvent(types.EventWorkflowStepDone, runID, step.ID, step.Name, types.StepStatusCompleted)

	return types.StepStatusCompleted, output, nil
}

// runStepAction executes the step's action payload. For now, this is a
// placeholder that returns the action JSON as output. In production, this
// would dispatch to tool invocations, HTTP calls, etc.
func (e *Executor) runStepAction(_ context.Context, step *repo.WorkflowStep) (string, error) {
	// Parse the action to determine step type behavior.
	var action map[string]interface{}
	if len(step.Action) > 0 {
		if err := json.Unmarshal(step.Action, &action); err != nil {
			return "", fmt.Errorf("workflow.Executor.runStepAction: %w", err)
		}
	}

	// For step type "condition", the step itself is just the condition evaluation
	// which already happened above. Return success.
	if step.StepType == "condition" {
		return "condition passed", nil
	}

	// For step type "approval", the approval gate already handled above.
	if step.StepType == "approval" {
		return "approved", nil
	}

	// For step type "tool", return the action as the output.
	// The actual tool dispatch will be wired in Phase 4 when we have
	// the MCP tool dispatcher integrated.
	output, err := json.Marshal(action)
	if err != nil {
		return "", fmt.Errorf("workflow.Executor.runStepAction: %w", err)
	}
	return string(output), nil
}

// failRun marks a workflow run as failed and publishes the failure event.
func (e *Executor) failRun(ctx context.Context, runID, workflowID, errMsg string) {
	if err := e.repo.UpdateRunStatus(ctx, runID, types.WorkflowStatusFailed, errMsg); err != nil {
		e.logger.Error("failed to update run status to failed",
			"run_id", runID, "error", err)
	}
	e.publishEvent(types.EventWorkflowFailed, runID, workflowID, errMsg)
}

// publishEvent publishes a workflow-level event on the bus.
func (e *Executor) publishEvent(eventType types.EventType, runID, workflowID, errMsg string) {
	payload, err := json.Marshal(map[string]string{
		"run_id":      runID,
		"workflow_id": workflowID,
		"error":       errMsg,
	})
	if err != nil {
		e.logger.Error("failed to marshal workflow event payload", "error", err)
		return
	}
	e.bus.Publish(types.NervousEvent{
		Type:      eventType,
		Scope:     "workflow",
		Source:    "workflow.executor",
		Payload:   payload,
		Timestamp: time.Now(),
	})
}

// publishStepEvent publishes a step-level event on the bus.
func (e *Executor) publishStepEvent(eventType types.EventType, runID, stepID, stepName, status string) {
	payload, err := json.Marshal(map[string]string{
		"run_id":    runID,
		"step_id":   stepID,
		"step_name": stepName,
		"status":    status,
	})
	if err != nil {
		e.logger.Error("failed to marshal workflow step event payload", "error", err)
		return
	}
	e.bus.Publish(types.NervousEvent{
		Type:      eventType,
		Scope:     "workflow",
		Source:    "workflow.executor",
		Payload:   payload,
		Timestamp: time.Now(),
	})
}

// ---------------------------------------------------------------------------
// DAG helpers
// ---------------------------------------------------------------------------

// parseDependsOn splits a comma-separated dependency string into step IDs.
// Returns nil for empty strings.
func parseDependsOn(deps string) []string {
	if deps == "" {
		return nil
	}
	parts := strings.Split(deps, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// validateDAG checks that the step dependency graph is a valid DAG
// (no cycles, all referenced dependencies exist).
func validateDAG(steps []*repo.WorkflowStep) error {
	stepIDs := make(map[string]struct{}, len(steps))
	for _, s := range steps {
		stepIDs[s.ID] = struct{}{}
	}

	// Verify all dependencies reference existing steps.
	for _, s := range steps {
		for _, dep := range parseDependsOn(s.DependsOn) {
			if _, ok := stepIDs[dep]; !ok {
				return fmt.Errorf("step %q depends on unknown step %q", s.ID, dep)
			}
		}
	}

	// Check for cycles via topological sort.
	if _, err := topoSort(steps); err != nil {
		return fmt.Errorf("workflow.Executor.validateDAG: %w", err)
	}
	return nil
}

// topoSort performs a topological sort of workflow steps using Kahn's algorithm.
// Returns the step IDs in execution order. Returns an error if a cycle is detected.
func topoSort(steps []*repo.WorkflowStep) ([]string, error) {
	// Build adjacency and in-degree maps.
	inDegree := make(map[string]int, len(steps))
	adjacency := make(map[string][]string, len(steps))

	for _, s := range steps {
		if _, ok := inDegree[s.ID]; !ok {
			inDegree[s.ID] = 0
		}
		for _, dep := range parseDependsOn(s.DependsOn) {
			adjacency[dep] = append(adjacency[dep], s.ID)
			inDegree[s.ID]++
		}
	}

	// Seed the queue with zero in-degree nodes.
	queue := make([]string, 0)
	for _, s := range steps {
		if inDegree[s.ID] == 0 {
			queue = append(queue, s.ID)
		}
	}

	var order []string
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		order = append(order, node)

		for _, neighbor := range adjacency[node] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	if len(order) != len(steps) {
		return nil, fmt.Errorf("cycle detected in workflow DAG")
	}

	return order, nil
}

// groupByLevel groups topologically sorted step IDs into execution levels.
// Steps within the same level have no mutual dependencies and can run in parallel.
func groupByLevel(order []string, steps []*repo.WorkflowStep) [][]string {
	// Build a map from step ID to its dependencies.
	depMap := make(map[string][]string, len(steps))
	for _, s := range steps {
		depMap[s.ID] = parseDependsOn(s.DependsOn)
	}

	// Assign levels: a step's level is max(level of deps) + 1.
	levelOf := make(map[string]int, len(order))
	maxLevel := 0

	for _, id := range order {
		level := 0
		for _, dep := range depMap[id] {
			if depLevel, ok := levelOf[dep]; ok && depLevel+1 > level {
				level = depLevel + 1
			}
		}
		levelOf[id] = level
		if level > maxLevel {
			maxLevel = level
		}
	}

	// Group by level.
	levels := make([][]string, maxLevel+1)
	for _, id := range order {
		l := levelOf[id]
		levels[l] = append(levels[l], id)
	}

	return levels
}

// buildEvalContext creates the variable map for CEL condition evaluation.
// It includes the run context variables and step output references.
func buildEvalContext(runContext map[string]interface{}, stepOutputs map[string]string) map[string]interface{} {
	evalCtx := make(map[string]interface{}, len(runContext)+len(stepOutputs))
	for k, v := range runContext {
		evalCtx[k] = v
	}
	// Make step outputs available under "steps" key.
	steps := make(map[string]interface{}, len(stepOutputs))
	for k, v := range stepOutputs {
		steps[k] = v
	}
	evalCtx["steps"] = steps
	return evalCtx
}
