package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/internal/storage"
	"github.com/hyperax/hyperax/internal/telemetry"
	"github.com/hyperax/hyperax/pkg/types"
)

// actionClearanceObservability maps each observability action to its minimum ABAC clearance.
var actionClearanceObservability = map[string]int{
	// Telemetry
	"get_session_telemetry": 0,
	"list_sessions":         0,
	"get_metrics_summary":   0,
	"get_cost_report":       0,
	"create_alert":          2,
	"list_alerts":           0,
	"delete_alert":          2,
	// Budget
	"get_budget_status":       0,
	"set_budget_threshold":    2,
	"record_energy_cost":      1,
	"list_budget_scopes":      0,
	"get_all_budget_statuses": 0,
	// Logs
	"list_logs":           0,
	"get_log_lines":       0,
	"get_log_errors":      0,
	"list_runtime_states": 0,
	"get_runtime_state":   0,
	// Metrics
	"get_metrics": 0,
}

// defaultErrorPattern matches common log severity levels used by structured
// and unstructured loggers.
var defaultErrorPattern = regexp.MustCompile(`(?i)\b(ERROR|WARN|FATAL|PANIC)\b`)

// ObservabilityHandler implements the consolidated "observability" MCP tool,
// combining telemetry, budget tracking, log inspection, and metrics.
type ObservabilityHandler struct {
	telemetryRepo repo.TelemetryRepo
	tracker       *telemetry.SessionTracker
	evaluator     *telemetry.AlertEvaluator
	collector     *telemetry.Collector
	store         *storage.Store
	logger        *slog.Logger
}

// NewObservabilityHandler creates an ObservabilityHandler.
func NewObservabilityHandler(
	store *storage.Store,
	logger *slog.Logger,
) *ObservabilityHandler {
	return &ObservabilityHandler{
		store:  store,
		logger: logger,
	}
}

// SetTelemetryDeps wires telemetry-specific dependencies.
func (h *ObservabilityHandler) SetTelemetryDeps(
	telemetryRepo repo.TelemetryRepo,
	tracker *telemetry.SessionTracker,
	evaluator *telemetry.AlertEvaluator,
	collector *telemetry.Collector,
) {
	h.telemetryRepo = telemetryRepo
	h.tracker = tracker
	h.evaluator = evaluator
	h.collector = collector
}

// RegisterTools registers the consolidated "observability" tool with the MCP registry.
func (h *ObservabilityHandler) RegisterTools(registry *mcp.ToolRegistry) {
	registry.Register(
		"observability",
		"Observability toolkit: session telemetry, cost reports, alerts, budgets, log inspection, metrics. "+
			"Actions: get_session_telemetry | list_sessions | get_metrics_summary | get_cost_report | "+
			"create_alert | list_alerts | delete_alert | get_budget_status | set_budget_threshold | "+
			"record_energy_cost | list_budget_scopes | get_all_budget_statuses | list_logs | "+
			"get_log_lines | get_log_errors | list_runtime_states | get_runtime_state | get_metrics",
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"action":       {"type": "string", "enum": ["get_session_telemetry", "list_sessions", "get_metrics_summary", "get_cost_report", "create_alert", "list_alerts", "delete_alert", "get_budget_status", "set_budget_threshold", "record_energy_cost", "list_budget_scopes", "get_all_budget_statuses", "list_logs", "get_log_lines", "get_log_errors", "list_runtime_states", "get_runtime_state", "get_metrics"], "description": "Action to perform"},
				"session_id":   {"type": "string", "description": "Session ID (telemetry)"},
				"agent_id":     {"type": "string", "description": "Filter sessions by agent ID"},
				"limit":        {"type": "integer", "description": "Max results"},
				"since":        {"type": "string", "description": "ISO 8601 timestamp or duration like '1h', '24h', '7d'"},
				"name":         {"type": "string", "description": "Alert name or runtime state getter name"},
				"metric":       {"type": "string", "description": "Metric to monitor: session_cost, tool_calls, error_rate, duration"},
				"operator":     {"type": "string", "description": "Comparison operator: gt, lt, gte, lte, eq"},
				"threshold":    {"type": "number", "description": "Alert/budget threshold value"},
				"window":       {"type": "string", "description": "Time window for alert evaluation: 1h, 24h, 7d"},
				"severity":     {"type": "string", "description": "Alert severity: info, warning, critical"},
				"alert_id":     {"type": "string", "description": "Alert ID (for delete)"},
				"scope":        {"type": "string", "description": "Budget scope"},
				"cost":         {"type": "number", "description": "Energy cost amount"},
				"workspace_id": {"type": "string", "description": "Workspace name for log scoping"},
				"path":         {"type": "string", "description": "Absolute path to log file"},
				"lines":        {"type": "integer", "description": "Number of lines to return/scan"},
				"pattern":      {"type": "string", "description": "Regex override for log error filtering"}
			},
			"required": ["action"]
		}`),
		h.dispatch,
	)
}

// dispatch routes the consolidated "observability" tool to the correct handler method.
func (h *ObservabilityHandler) dispatch(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var envelope struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return nil, fmt.Errorf("handlers.ObservabilityHandler.dispatch: %w", err)
	}

	if err := checkActionClearance(ctx, envelope.Action, actionClearanceObservability); err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	switch envelope.Action {
	// Telemetry
	case "get_session_telemetry":
		return h.getSessionTelemetry(ctx, params)
	case "list_sessions":
		return h.listSessions(ctx, params)
	case "get_metrics_summary":
		return h.getMetricsSummary(ctx, params)
	case "get_cost_report":
		return h.getCostReport(ctx, params)
	case "create_alert":
		return h.createAlert(ctx, params)
	case "list_alerts":
		return h.listAlerts(ctx, params)
	case "delete_alert":
		return h.deleteAlert(ctx, params)
	// Budget
	case "get_budget_status":
		return h.getBudgetStatus(ctx, params)
	case "set_budget_threshold":
		return h.setBudgetThreshold(ctx, params)
	case "record_energy_cost":
		return h.recordEnergyCost(ctx, params)
	case "list_budget_scopes":
		return h.listBudgetScopes(ctx, params)
	case "get_all_budget_statuses":
		return h.getAllBudgetStatuses(ctx, params)
	// Logs
	case "list_logs":
		return h.listLogs(ctx, params)
	case "get_log_lines":
		return h.getLogLines(ctx, params)
	case "get_log_errors":
		return h.getLogErrors(ctx, params)
	case "list_runtime_states":
		return h.listRuntimeStates(ctx, params)
	case "get_runtime_state":
		return h.getRuntimeState(ctx, params)
	// Metrics
	case "get_metrics":
		return h.getMetrics(ctx, params)
	default:
		return types.NewErrorResult(fmt.Sprintf("unknown observability action %q", envelope.Action)), nil
	}
}

// ── Telemetry action handlers ──

func (h *ObservabilityHandler) getSessionTelemetry(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ObservabilityHandler.getSessionTelemetry: %w", err)
	}
	if args.SessionID == "" {
		return types.NewErrorResult("session_id is required"), nil
	}

	if h.telemetryRepo == nil {
		return types.NewErrorResult("telemetry repo not available"), nil
	}

	session, err := h.telemetryRepo.GetSession(ctx, args.SessionID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get session: %v", err)), nil
	}

	metrics, err := h.telemetryRepo.GetSessionMetrics(ctx, args.SessionID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get session metrics: %v", err)), nil
	}

	result := map[string]interface{}{
		"session":     session,
		"provider_id": session.ProviderID,
		"model":       session.Model,
		"metrics":     metrics,
		"summary": map[string]interface{}{
			"tool_calls": session.ToolCalls,
			"total_cost": session.TotalCost,
			"status":     session.Status,
		},
	}

	return types.NewToolResult(result), nil
}

func (h *ObservabilityHandler) listSessions(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		AgentID string `json:"agent_id"`
		Limit   int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ObservabilityHandler.listSessions: %w", err)
	}
	if args.Limit <= 0 {
		args.Limit = 20
	}

	if h.telemetryRepo == nil {
		return types.NewErrorResult("telemetry repo not available"), nil
	}

	sessions, err := h.telemetryRepo.ListSessions(ctx, args.AgentID, args.Limit)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("list sessions: %v", err)), nil
	}

	if len(sessions) == 0 {
		return types.NewToolResult([]interface{}{}), nil
	}

	return types.NewToolResult(sessions), nil
}

func (h *ObservabilityHandler) getMetricsSummary(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Since string `json:"since"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ObservabilityHandler.getMetricsSummary: %w", err)
	}

	since := parseSinceParam(args.Since, 24*time.Hour)

	// Prefer live collector data if available, fall back to DB.
	if h.collector != nil {
		summary := h.collector.Summary()
		return types.NewToolResult(summary), nil
	}

	if h.telemetryRepo == nil {
		return types.NewErrorResult("telemetry repo not available"), nil
	}

	summary, err := h.telemetryRepo.GetMetricsSummary(ctx, since)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get metrics summary: %v", err)), nil
	}

	return types.NewToolResult(summary), nil
}

func (h *ObservabilityHandler) getCostReport(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Since string `json:"since"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ObservabilityHandler.getCostReport: %w", err)
	}

	since := parseSinceParam(args.Since, 24*time.Hour)

	if h.telemetryRepo == nil {
		return types.NewErrorResult("telemetry repo not available"), nil
	}

	report, err := h.telemetryRepo.GetCostReport(ctx, since)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get cost report: %v", err)), nil
	}

	if len(report) == 0 {
		return types.NewToolResult(map[string]interface{}{
			"items":              []interface{}{},
			"provider_subtotals": map[string]interface{}{},
		}), nil
	}

	// Compute per-provider subtotals from the report items.
	providerSubtotals := make(map[string]map[string]interface{})
	for _, item := range report {
		pid := item.ProviderID
		if pid == "" {
			pid = "(unknown)"
		}
		sub, exists := providerSubtotals[pid]
		if !exists {
			sub = map[string]interface{}{
				"provider_id": pid,
				"total_cost":  0.0,
				"call_count":  int64(0),
			}
			providerSubtotals[pid] = sub
		}
		sub["total_cost"] = sub["total_cost"].(float64) + item.TotalCost
		sub["call_count"] = sub["call_count"].(int64) + item.CallCount
	}

	// Convert subtotals map to slice for JSON serialisation.
	subtotalSlice := make([]map[string]interface{}, 0, len(providerSubtotals))
	for _, sub := range providerSubtotals {
		subtotalSlice = append(subtotalSlice, sub)
	}

	return types.NewToolResult(map[string]interface{}{
		"items":              report,
		"provider_subtotals": subtotalSlice,
	}), nil
}

func (h *ObservabilityHandler) createAlert(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Name      string  `json:"name"`
		Metric    string  `json:"metric"`
		Operator  string  `json:"operator"`
		Threshold float64 `json:"threshold"`
		Window    string  `json:"window"`
		Severity  string  `json:"severity"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ObservabilityHandler.createAlert: %w", err)
	}
	if args.Name == "" {
		return types.NewErrorResult("name is required"), nil
	}
	if args.Metric == "" {
		return types.NewErrorResult("metric is required"), nil
	}
	if args.Operator == "" {
		return types.NewErrorResult("operator is required"), nil
	}

	// Validate metric name.
	validMetrics := map[string]bool{
		"session_cost": true, "tool_calls": true,
		"error_rate": true, "duration": true,
	}
	if !validMetrics[args.Metric] {
		return types.NewErrorResult(fmt.Sprintf("invalid metric %q; valid: session_cost, tool_calls, error_rate, duration", args.Metric)), nil
	}

	// Validate operator.
	validOps := map[string]bool{
		"gt": true, "lt": true, "gte": true, "lte": true, "eq": true,
	}
	if !validOps[args.Operator] {
		return types.NewErrorResult(fmt.Sprintf("invalid operator %q; valid: gt, lt, gte, lte, eq", args.Operator)), nil
	}

	if h.telemetryRepo == nil {
		return types.NewErrorResult("telemetry repo not available"), nil
	}

	alert := &types.Alert{
		Name:      args.Name,
		Metric:    args.Metric,
		Operator:  args.Operator,
		Threshold: args.Threshold,
		Window:    args.Window,
		Severity:  args.Severity,
		Enabled:   true,
	}
	if alert.Window == "" {
		alert.Window = "1h"
	}
	if alert.Severity == "" {
		alert.Severity = "info"
	}

	id, err := h.telemetryRepo.CreateAlert(ctx, alert)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("create alert: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"id":      id,
		"message": fmt.Sprintf("Alert %q created.", args.Name),
	}), nil
}

func (h *ObservabilityHandler) listAlerts(ctx context.Context, _ json.RawMessage) (*types.ToolResult, error) {
	if h.telemetryRepo == nil {
		return types.NewErrorResult("telemetry repo not available"), nil
	}

	alerts, err := h.telemetryRepo.ListAlerts(ctx)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("list alerts: %v", err)), nil
	}

	if len(alerts) == 0 {
		return types.NewToolResult([]interface{}{}), nil
	}

	return types.NewToolResult(alerts), nil
}

func (h *ObservabilityHandler) deleteAlert(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		AlertID string `json:"alert_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ObservabilityHandler.deleteAlert: %w", err)
	}
	if args.AlertID == "" {
		return types.NewErrorResult("alert_id is required"), nil
	}

	if h.telemetryRepo == nil {
		return types.NewErrorResult("telemetry repo not available"), nil
	}

	if err := h.telemetryRepo.DeleteAlert(ctx, args.AlertID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("delete alert: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"id":      args.AlertID,
		"status":  "deleted",
		"message": fmt.Sprintf("Alert %q deleted.", args.AlertID),
	}), nil
}

// ── Budget action handlers ──

func (h *ObservabilityHandler) getBudgetStatus(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Scope string `json:"scope"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ObservabilityHandler.getBudgetStatus: %w", err)
	}
	if args.Scope == "" {
		return types.NewErrorResult("scope is required"), nil
	}

	if h.store.Budgets == nil {
		return types.NewErrorResult("budget repo not available"), nil
	}

	cost, err := h.store.Budgets.GetCumulativeEnergyCost(ctx, args.Scope)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get cumulative energy cost: %v", err)), nil
	}

	threshold, err := h.store.Budgets.GetBudgetThreshold(ctx, args.Scope)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			threshold = 0
		} else {
			return types.NewErrorResult(fmt.Sprintf("get budget threshold: %v", err)), nil
		}
	}

	remaining := threshold - cost
	percentUsed := 0.0
	if threshold > 0 {
		percentUsed = (cost / threshold) * 100.0
	}

	return types.NewToolResult(map[string]interface{}{
		"scope":        args.Scope,
		"cost":         cost,
		"threshold":    threshold,
		"remaining":    remaining,
		"percent_used": percentUsed,
	}), nil
}

func (h *ObservabilityHandler) setBudgetThreshold(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Scope     string  `json:"scope"`
		Threshold float64 `json:"threshold"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ObservabilityHandler.setBudgetThreshold: %w", err)
	}
	if args.Scope == "" {
		return types.NewErrorResult("scope is required"), nil
	}
	if args.Threshold <= 0 {
		return types.NewErrorResult("threshold must be a positive number"), nil
	}

	if h.store.Budgets == nil {
		return types.NewErrorResult("budget repo not available"), nil
	}

	if err := h.store.Budgets.SetBudgetThreshold(ctx, args.Scope, args.Threshold); err != nil {
		return types.NewErrorResult(fmt.Sprintf("set budget threshold: %v", err)), nil
	}

	return types.NewToolResult(map[string]interface{}{
		"scope":     args.Scope,
		"threshold": args.Threshold,
		"status":    "updated",
	}), nil
}

func (h *ObservabilityHandler) recordEnergyCost(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Scope string  `json:"scope"`
		Cost  float64 `json:"cost"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ObservabilityHandler.recordEnergyCost: %w", err)
	}
	if args.Scope == "" {
		return types.NewErrorResult("scope is required"), nil
	}
	if args.Cost <= 0 {
		return types.NewErrorResult("cost must be a positive number"), nil
	}

	if h.store.Budgets == nil {
		return types.NewErrorResult("budget repo not available"), nil
	}

	if err := h.store.Budgets.RecordEnergyCost(ctx, args.Scope, args.Cost, "", ""); err != nil {
		return types.NewErrorResult(fmt.Sprintf("record energy cost: %v", err)), nil
	}

	return types.NewToolResult(map[string]interface{}{
		"scope":  args.Scope,
		"cost":   args.Cost,
		"status": "recorded",
	}), nil
}

func (h *ObservabilityHandler) listBudgetScopes(ctx context.Context, _ json.RawMessage) (*types.ToolResult, error) {
	if h.store.Budgets == nil {
		return types.NewErrorResult("budget repo not available"), nil
	}

	scopes, err := h.store.Budgets.ListThresholdScopes(ctx)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("list threshold scopes: %v", err)), nil
	}

	if scopes == nil {
		scopes = []string{}
	}

	return types.NewToolResult(map[string]interface{}{
		"scopes": scopes,
		"count":  len(scopes),
	}), nil
}

func (h *ObservabilityHandler) getAllBudgetStatuses(ctx context.Context, _ json.RawMessage) (*types.ToolResult, error) {
	if h.store.Budgets == nil {
		return types.NewErrorResult("budget repo not available"), nil
	}

	scopes, err := h.store.Budgets.ListThresholdScopes(ctx)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("list threshold scopes: %v", err)), nil
	}

	type budgetStatus struct {
		Scope       string  `json:"scope"`
		Cost        float64 `json:"cost"`
		Threshold   float64 `json:"threshold"`
		Remaining   float64 `json:"remaining"`
		PercentUsed float64 `json:"percent_used"`
	}

	statuses := make([]budgetStatus, 0, len(scopes))
	for _, scope := range scopes {
		cost, costErr := h.store.Budgets.GetCumulativeEnergyCost(ctx, scope)
		if costErr != nil {
			continue
		}

		threshold, threshErr := h.store.Budgets.GetBudgetThreshold(ctx, scope)
		if threshErr != nil {
			if strings.Contains(threshErr.Error(), "not found") {
				threshold = 0
			} else {
				continue
			}
		}

		remaining := threshold - cost
		percentUsed := 0.0
		if threshold > 0 {
			percentUsed = (cost / threshold) * 100.0
		}

		statuses = append(statuses, budgetStatus{
			Scope:       scope,
			Cost:        cost,
			Threshold:   threshold,
			Remaining:   remaining,
			PercentUsed: percentUsed,
		})
	}

	return types.NewToolResult(map[string]interface{}{
		"statuses": statuses,
		"count":    len(statuses),
	}), nil
}

// ── Log action handlers ──

type logEntry struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
}

func (h *ObservabilityHandler) listLogs(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ObservabilityHandler.listLogs: %w", err)
	}

	var entries []logEntry

	// 1. If a workspace is specified, scan its .hyperax/logs/ directory.
	if args.WorkspaceID != "" {
		ws, err := h.resolveLogWorkspace(ctx, args.WorkspaceID)
		if err != nil {
			return types.NewErrorResult(err.Error()), nil
		}
		logsDir := filepath.Join(ws.RootPath, ".hyperax", "logs")
		entries = append(entries, scanLogDir(logsDir)...)
	}

	// 2. Scan /var/log/ for common log files (non-recursive, top-level only).
	entries = append(entries, scanLogDir("/var/log")...)

	// 3. If workspaces repo is available and no specific workspace was given,
	//    scan all workspace log directories.
	if args.WorkspaceID == "" && h.store.Workspaces != nil {
		workspaces, err := h.store.Workspaces.ListWorkspaces(ctx)
		if err == nil {
			for _, ws := range workspaces {
				logsDir := filepath.Join(ws.RootPath, ".hyperax", "logs")
				entries = append(entries, scanLogDir(logsDir)...)
			}
		}
	}

	if len(entries) == 0 {
		return types.NewToolResult("No log files found."), nil
	}

	return types.NewToolResult(entries), nil
}

func (h *ObservabilityHandler) getLogLines(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Path        string `json:"path"`
		Lines       int    `json:"lines"`
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ObservabilityHandler.getLogLines: %w", err)
	}
	if args.Path == "" {
		return types.NewErrorResult("path is required"), nil
	}
	if args.Lines <= 0 {
		args.Lines = 50
	}

	// Sandbox validation: if workspace_id is provided, ensure the path
	// resides within that workspace's root.
	if args.WorkspaceID != "" {
		if err := h.validateLogPathInWorkspace(ctx, args.WorkspaceID, args.Path); err != nil {
			return types.NewErrorResult(err.Error()), nil
		}
	}

	lines, err := tailLogFile(args.Path, args.Lines)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("read log: %v", err)), nil
	}

	if len(lines) == 0 {
		return types.NewToolResult("(empty file)"), nil
	}

	return types.NewToolResult(strings.Join(lines, "\n")), nil
}

func (h *ObservabilityHandler) getLogErrors(_ context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Path    string `json:"path"`
		Lines   int    `json:"lines"`
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ObservabilityHandler.getLogErrors: %w", err)
	}
	if args.Path == "" {
		return types.NewErrorResult("path is required"), nil
	}
	if args.Lines <= 0 {
		args.Lines = 200
	}

	// Determine the filter pattern.
	pat := defaultErrorPattern
	if args.Pattern != "" {
		compiled, err := regexp.Compile(args.Pattern)
		if err != nil {
			return types.NewErrorResult(fmt.Sprintf("invalid pattern: %v", err)), nil
		}
		pat = compiled
	}

	lines, err := tailLogFile(args.Path, args.Lines)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("read log: %v", err)), nil
	}

	var matched []string
	for _, line := range lines {
		if pat.MatchString(line) {
			matched = append(matched, line)
		}
	}

	if len(matched) == 0 {
		return types.NewToolResult("No matching lines found."), nil
	}

	return types.NewToolResult(strings.Join(matched, "\n")), nil
}

type runtimeStateEntry struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

func (h *ObservabilityHandler) listRuntimeStates(ctx context.Context, _ json.RawMessage) (*types.ToolResult, error) {
	if h.store.Config == nil {
		return types.NewErrorResult("config repo not available"), nil
	}

	values, err := h.store.Config.ListValues(ctx, types.ConfigScope{Type: "global"})
	if err != nil {
		return nil, fmt.Errorf("handlers.ObservabilityHandler.listRuntimeStates: %w", err)
	}

	var entries []runtimeStateEntry
	for _, v := range values {
		if strings.HasPrefix(v.Key, "runtime_state.") {
			name := strings.TrimPrefix(v.Key, "runtime_state.")
			entries = append(entries, runtimeStateEntry{
				Name:    name,
				Command: v.Value,
			})
		}
	}

	if len(entries) == 0 {
		return types.NewToolResult("No runtime state getters configured."), nil
	}

	return types.NewToolResult(entries), nil
}

func (h *ObservabilityHandler) getRuntimeState(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ObservabilityHandler.getRuntimeState: %w", err)
	}
	if args.Name == "" {
		return types.NewErrorResult("name is required"), nil
	}

	if h.store.Config == nil {
		return types.NewErrorResult("config repo not available"), nil
	}

	key := "runtime_state." + args.Name
	command, err := h.store.Config.GetValue(ctx, key, types.ConfigScope{Type: "global"})
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("runtime state %q not found: %v", args.Name, err)), nil
	}
	if command == "" {
		return types.NewErrorResult(fmt.Sprintf("runtime state %q has empty command", args.Name)), nil
	}

	// Execute with a short timeout to prevent runaway processes.
	execCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// #nosec G204 -- command comes from the trusted config store, not user input
	cmd := exec.CommandContext(execCtx, "sh", "-c", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("command failed: %v\noutput: %s", err, string(output))), nil
	}

	return types.NewToolResult(strings.TrimRight(string(output), "\n")), nil
}

// ── Metrics action handlers ──

func (h *ObservabilityHandler) getMetrics(ctx context.Context, _ json.RawMessage) (*types.ToolResult, error) {
	if h.store.Metrics == nil {
		return types.NewErrorResult("metrics repo not available"), nil
	}

	metrics, err := h.store.Metrics.GetToolMetrics(ctx)
	if err != nil {
		return nil, fmt.Errorf("handlers.ObservabilityHandler.getMetrics: %w", err)
	}

	if len(metrics) == 0 {
		return types.NewToolResult("No tool metrics recorded yet."), nil
	}

	var sb strings.Builder
	sb.WriteString("Tool usage metrics:\n")
	for _, m := range metrics {
		lastUsed := "never"
		if m.LastUsed != nil {
			lastUsed = m.LastUsed.Format("2006-01-02 15:04:05")
		}
		avgMS := int64(0)
		if m.CallCount > 0 {
			avgMS = m.TotalDurationMS / m.CallCount
		}
		fmt.Fprintf(&sb, "  - %s: calls=%d, total_ms=%d, avg_ms=%d, last_used=%s\n",
			m.ToolName, m.CallCount, m.TotalDurationMS, avgMS, lastUsed)
	}
	return types.NewToolResult(sb.String()), nil
}

// ── Internal helpers ──

// parseSinceParam interprets a "since" parameter as either an ISO 8601 timestamp
// or a duration string (e.g., "1h", "24h", "7d"). Returns a time.Time.
func parseSinceParam(since string, defaultDuration time.Duration) time.Time {
	if since == "" {
		return time.Now().Add(-defaultDuration)
	}

	// Try ISO 8601 / RFC3339 first.
	if t, err := time.Parse(time.RFC3339, since); err == nil {
		return t
	}

	// Try Go duration ("1h", "24h").
	if d, err := time.ParseDuration(since); err == nil {
		return time.Now().Add(-d)
	}

	// Try day suffix "7d".
	if len(since) > 1 && since[len(since)-1] == 'd' {
		var days int
		if _, err := fmt.Sscanf(since, "%dd", &days); err == nil && days > 0 {
			return time.Now().Add(-time.Duration(days) * 24 * time.Hour)
		}
	}

	return time.Now().Add(-defaultDuration)
}

// resolveLogWorkspace fetches workspace info by name.
func (h *ObservabilityHandler) resolveLogWorkspace(ctx context.Context, name string) (*types.WorkspaceInfo, error) {
	if h.store.Workspaces == nil {
		return nil, fmt.Errorf("workspace repo not available")
	}
	ws, err := h.store.Workspaces.GetWorkspace(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("workspace %q not found: %v", name, err)
	}
	return ws, nil
}

// validateLogPathInWorkspace ensures an absolute file path resides within the
// root directory of the named workspace.
func (h *ObservabilityHandler) validateLogPathInWorkspace(ctx context.Context, wsName, absPath string) error {
	ws, err := h.resolveLogWorkspace(ctx, wsName)
	if err != nil {
		return err
	}

	cleanRoot, err := filepath.Abs(ws.RootPath)
	if err != nil {
		return fmt.Errorf("resolve workspace root: %v", err)
	}
	cleanPath, err := filepath.Abs(absPath)
	if err != nil {
		return fmt.Errorf("resolve path: %v", err)
	}

	if !strings.HasPrefix(cleanPath, cleanRoot+string(os.PathSeparator)) && cleanPath != cleanRoot {
		return fmt.Errorf("path %q escapes workspace root %q", absPath, cleanRoot)
	}
	return nil
}

// scanLogDir returns logEntry items for all regular files in a directory.
func scanLogDir(dir string) []logEntry {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var result []logEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		result = append(result, logEntry{
			Path:     filepath.Join(dir, e.Name()),
			Size:     info.Size(),
			Modified: info.ModTime().Format(time.RFC3339),
		})
	}
	return result
}

// tailLogFile reads the last n lines from the file at path.
func tailLogFile(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %s", path)
		}
		return nil, fmt.Errorf("open: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			slog.Warn("failed to close file", "error", cerr)
		}
	}()

	var allLines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		allLines = append(allLines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	if len(allLines) <= n {
		return allLines, nil
	}
	return allLines[len(allLines)-n:], nil
}
