package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// DispatchFunc dispatches an MCP tool call by name and returns the result.
type DispatchFunc func(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error)

// Executor listens for cron.fire events on the EventBus and executes the
// job payload based on the job_type. It completes the execution record and
// publishes cron.complete or cron.failed events with diagnostic output.
type Executor struct {
	repo     repo.CronRepo
	bus      *nervous.EventBus
	dispatch DispatchFunc
	logger   *slog.Logger
}

// NewExecutor creates a cron Executor.
func NewExecutor(repo repo.CronRepo, bus *nervous.EventBus, dispatch DispatchFunc, logger *slog.Logger) *Executor {
	return &Executor{
		repo:     repo,
		bus:      bus,
		dispatch: dispatch,
		logger:   logger,
	}
}

// Run subscribes to cron.fire events and processes them until ctx is cancelled.
func (e *Executor) Run(ctx context.Context) {
	sub := e.bus.SubscribeTypes("cron.executor", types.EventCronFire)
	defer e.bus.Unsubscribe("cron.executor")

	e.logger.Info("cron executor started")

	for {
		select {
		case <-ctx.Done():
			e.logger.Info("cron executor stopped")
			return
		case event, ok := <-sub.Ch:
			if !ok {
				return
			}
			e.handleFire(ctx, event)
		}
	}
}

// firePayload is the structure published by the cron scheduler's dispatchJob.
type firePayload struct {
	JobID       string          `json:"job_id"`
	JobName     string          `json:"job_name"`
	JobType     string          `json:"job_type"`
	ExecutionID string          `json:"execution_id"`
	Payload     json.RawMessage `json:"payload"`
}

// handleFire processes a single cron.fire event.
func (e *Executor) handleFire(ctx context.Context, event types.NervousEvent) {
	var fp firePayload
	if err := json.Unmarshal(event.Payload, &fp); err != nil {
		e.logger.Warn("cron executor: failed to parse event payload", "error", err)
		return
	}

	e.logger.Info("cron executor: executing job",
		"job_id", fp.JobID,
		"job_name", fp.JobName,
		"job_type", fp.JobType,
		"execution_id", fp.ExecutionID,
	)

	start := time.Now()

	var result string
	var execErr error

	switch fp.JobType {
	case "tool":
		result, execErr = e.executeTool(ctx, fp.Payload)
	case "pipeline":
		result, execErr = e.executePipeline(ctx, fp.Payload)
	case "webhook":
		result, execErr = e.executeWebhook(ctx, fp.Payload)
	default:
		execErr = fmt.Errorf("unsupported job type: %q", fp.JobType)
	}

	elapsed := time.Since(start)

	// Complete the execution record.
	if fp.ExecutionID != "" && e.repo != nil {
		status := "completed"
		errMsg := ""
		if execErr != nil {
			status = "failed"
			errMsg = execErr.Error()
		}
		if completeErr := e.repo.CompleteExecution(ctx, fp.ExecutionID, status, errMsg); completeErr != nil {
			e.logger.Error("cron executor: failed to complete execution record",
				"execution_id", fp.ExecutionID, "error", completeErr)
		}
	}

	// Update last_status on the job itself.
	if fp.JobID != "" && e.repo != nil {
		lastStatus := "completed"
		if execErr != nil {
			lastStatus = "failed"
		}
		if err := e.repo.UpdateLastRun(ctx, fp.JobID, start, lastStatus); err != nil {
			e.logger.Error("cron executor: failed to update job last_status",
				"job_id", fp.JobID, "error", err)
		}
	}

	// Publish completion or failure event with diagnostics.
	if execErr != nil {
		e.logger.Warn("cron executor: job failed",
			"job_id", fp.JobID,
			"job_name", fp.JobName,
			"execution_id", fp.ExecutionID,
			"elapsed", elapsed,
			"error", execErr,
		)

		e.bus.Publish(nervous.NewEvent(types.EventCronFailed, "cron.executor", "cron", map[string]any{
			"job_id":       fp.JobID,
			"job_name":     fp.JobName,
			"job_type":     fp.JobType,
			"execution_id": fp.ExecutionID,
			"elapsed_ms":   elapsed.Milliseconds(),
			"error":        execErr.Error(),
		}))
	} else {
		e.logger.Info("cron executor: job completed",
			"job_id", fp.JobID,
			"job_name", fp.JobName,
			"execution_id", fp.ExecutionID,
			"elapsed", elapsed,
			"result_len", len(result),
		)

		e.bus.Publish(nervous.NewEvent(types.EventCronComplete, "cron.executor", "cron", map[string]any{
			"job_id":       fp.JobID,
			"job_name":     fp.JobName,
			"job_type":     fp.JobType,
			"execution_id": fp.ExecutionID,
			"elapsed_ms":   elapsed.Milliseconds(),
			"result":       truncate(result, 500),
		}))
	}
}

// executeTool dispatches an MCP tool call.
// Expected payload: {"tool": "tool_name", "params": {...}}
func (e *Executor) executeTool(ctx context.Context, payload json.RawMessage) (string, error) {
	if e.dispatch == nil {
		return "", fmt.Errorf("cron.Executor.executeTool: tool dispatch function not configured")
	}

	var toolPayload struct {
		Tool   string          `json:"tool"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(payload, &toolPayload); err != nil {
		return "", fmt.Errorf("cron.Executor.executeTool: %w", err)
	}
	if toolPayload.Tool == "" {
		return "", fmt.Errorf("cron.Executor.executeTool: tool payload missing 'tool' field")
	}

	// Default to empty object if params is missing or null.
	params := toolPayload.Params
	if len(params) == 0 || string(params) == "null" {
		params = json.RawMessage(`{}`)
	}

	e.logger.Info("cron executor: dispatching tool",
		"tool", toolPayload.Tool,
	)

	result, err := e.dispatch(ctx, toolPayload.Tool, params)
	if err != nil {
		return "", fmt.Errorf("cron.Executor.executeTool: tool %q dispatch: %w", toolPayload.Tool, err)
	}
	if result != nil && result.IsError {
		return "", fmt.Errorf("cron.Executor.executeTool: tool %q returned error: %s", toolPayload.Tool, formatToolResult(result))
	}

	return formatToolResult(result), nil
}

// executePipeline triggers a pipeline run via the run_pipeline MCP tool.
// Expected payload: {"pipeline_id": "...", "workspace_name": "..."}
func (e *Executor) executePipeline(ctx context.Context, payload json.RawMessage) (string, error) {
	if e.dispatch == nil {
		return "", fmt.Errorf("cron.Executor.executePipeline: tool dispatch function not configured")
	}

	e.logger.Info("cron executor: dispatching pipeline",
		"payload", string(payload),
	)

	result, err := e.dispatch(ctx, "run_pipeline", payload)
	if err != nil {
		return "", fmt.Errorf("cron.Executor.executePipeline: %w", err)
	}
	if result != nil && result.IsError {
		return "", fmt.Errorf("cron.Executor.executePipeline: run_pipeline returned error: %s", formatToolResult(result))
	}

	return formatToolResult(result), nil
}

// executeWebhook POSTs the payload to a URL.
// Expected payload: {"url": "...", "method": "POST", "headers": {...}, "body": "..."}
func (e *Executor) executeWebhook(ctx context.Context, payload json.RawMessage) (string, error) {
	var whPayload struct {
		URL     string            `json:"url"`
		Method  string            `json:"method"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	}
	if err := json.Unmarshal(payload, &whPayload); err != nil {
		return "", fmt.Errorf("cron.Executor.executeWebhook: %w", err)
	}
	if whPayload.URL == "" {
		return "", fmt.Errorf("cron.Executor.executeWebhook: webhook payload missing 'url' field")
	}
	if whPayload.Method == "" {
		whPayload.Method = http.MethodPost
	}

	e.logger.Info("cron executor: calling webhook",
		"url", whPayload.URL,
		"method", whPayload.Method,
	)

	req, err := http.NewRequestWithContext(ctx, whPayload.Method, whPayload.URL, strings.NewReader(whPayload.Body))
	if err != nil {
		return "", fmt.Errorf("cron.Executor.executeWebhook: %w", err)
	}
	for k, v := range whPayload.Headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("cron.Executor.executeWebhook: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("cron.Executor.executeWebhook: webhook returned status %d", resp.StatusCode)
	}

	return fmt.Sprintf("webhook %s %s → %d", whPayload.Method, whPayload.URL, resp.StatusCode), nil
}

// formatToolResult extracts the text content from a ToolResult.
func formatToolResult(result *types.ToolResult) string {
	if result == nil {
		return ""
	}
	if len(result.Content) == 0 {
		return ""
	}

	var parts []string
	for _, c := range result.Content {
		if c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// truncate shortens a string to the given max length.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
