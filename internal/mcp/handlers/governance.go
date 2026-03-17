package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hyperax/hyperax/internal/guard"
	"github.com/hyperax/hyperax/internal/interject"
	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/pkg/types"
)

// actionClearanceGovernance maps each governance action to its minimum ABAC clearance.
var actionClearanceGovernance = map[string]int{
	// Guard actions
	"get_pending_actions": 0,
	"approve_action":      2,
	"reject_action":       2,
	"get_action_detail":   0,
	"get_action_history":  0,
	// Interjection / Andon Cord actions
	"pull_andon_cord":          0,
	"get_active_interjections": 0,
	"get_interjection":         0,
	"resolve_interjection":     2,
	"get_interjection_history": 0,
	"get_safe_mode_status":     0,
	"request_temporary_bypass": 3,
	"list_dlq":                 1,
	"replay_dlq":               3,
}

// GovernanceHandler implements the consolidated "governance" MCP tool,
// combining guard actions (approve/reject pending tool calls) with
// interjection management (Andon Cord, Safe Mode, DLQ, sieve bypass).
type GovernanceHandler struct {
	guardMgr *guard.ActionManager
	ijMgr    *interject.Manager
}

// NewGovernanceHandler creates a GovernanceHandler.
func NewGovernanceHandler(guardMgr *guard.ActionManager, ijMgr *interject.Manager) *GovernanceHandler {
	return &GovernanceHandler{
		guardMgr: guardMgr,
		ijMgr:    ijMgr,
	}
}

// RegisterTools registers the consolidated "governance" tool with the MCP registry.
func (h *GovernanceHandler) RegisterTools(registry *mcp.ToolRegistry) {
	registry.Register(
		"governance",
		"Governance toolkit: guard actions (approve/reject pending tool calls), "+
			"interjection management (Andon Cord, Safe Mode, DLQ, sieve bypass). "+
			"Actions: get_pending_actions | approve_action | reject_action | get_action_detail | "+
			"get_action_history | pull_andon_cord | get_active_interjections | get_interjection | "+
			"resolve_interjection | get_interjection_history | get_safe_mode_status | "+
			"request_temporary_bypass | list_dlq | replay_dlq",
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"action":              {"type": "string", "enum": ["get_pending_actions", "approve_action", "reject_action", "get_action_detail", "get_action_history", "pull_andon_cord", "get_active_interjections", "get_interjection", "resolve_interjection", "get_interjection_history", "get_safe_mode_status", "request_temporary_bypass", "list_dlq", "replay_dlq"], "description": "Action to perform"},
				"id":                  {"type": "string", "description": "Action/interjection/DLQ entry ID"},
				"notes":               {"type": "string", "description": "Approval/rejection notes"},
				"limit":               {"type": "integer", "description": "Max results (default 50)"},
				"scope":               {"type": "string", "description": "Blast radius: 'agent', 'workspace', or 'global'"},
				"severity":            {"type": "string", "description": "Severity level", "enum": ["warning", "critical", "fatal"]},
				"source":              {"type": "string", "description": "Source that triggered the interjection"},
				"reason":              {"type": "string", "description": "Reason for pulling the Andon Cord or bypass"},
				"created_by":          {"type": "string", "description": "Persona ID of the caller"},
				"remediation_persona": {"type": "string", "description": "Persona ID for automated remediation dispatch"},
				"trace_id":            {"type": "string", "description": "OTel trace ID for correlation"},
				"expires_minutes":     {"type": "integer", "description": "Auto-expire after N minutes (0 = no expiry)"},
				"resolution":          {"type": "string", "description": "Resolution description"},
				"resolution_action":   {"type": "string", "description": "Action to take on resolution", "enum": ["resume", "abort", "retry"]},
				"resolved_by":         {"type": "string", "description": "Persona ID of the resolver"},
				"pattern":             {"type": "string", "description": "Sieve pattern to bypass"},
				"granted_by":          {"type": "string", "description": "Persona ID granting the bypass"},
				"duration_minutes":    {"type": "integer", "description": "How long the bypass lasts in minutes"},
				"interjection_id":     {"type": "string", "description": "Interjection ID (for DLQ listing)"}
			},
			"required": ["action"]
		}`),
		h.dispatch,
	)
}

// dispatch routes the consolidated "governance" tool to the correct handler method.
func (h *GovernanceHandler) dispatch(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var envelope struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return nil, fmt.Errorf("handlers.GovernanceHandler.dispatch: %w", err)
	}

	if err := checkActionClearance(ctx, envelope.Action, actionClearanceGovernance); err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	switch envelope.Action {
	// Guard actions
	case "get_pending_actions":
		return h.getPendingActions(ctx, params)
	case "approve_action":
		return h.approveAction(ctx, params)
	case "reject_action":
		return h.rejectAction(ctx, params)
	case "get_action_detail":
		return h.getActionDetail(ctx, params)
	case "get_action_history":
		return h.getActionHistory(ctx, params)
	// Interjection actions
	case "pull_andon_cord":
		return h.pullAndonCord(ctx, params)
	case "get_active_interjections":
		return h.getActiveInterjections(ctx, params)
	case "get_interjection":
		return h.getInterjection(ctx, params)
	case "resolve_interjection":
		return h.resolveInterjection(ctx, params)
	case "get_interjection_history":
		return h.getInterjectionHistory(ctx, params)
	case "get_safe_mode_status":
		return h.getSafeModeStatus(ctx, params)
	case "request_temporary_bypass":
		return h.requestTemporaryBypass(ctx, params)
	case "list_dlq":
		return h.listDLQ(ctx, params)
	case "replay_dlq":
		return h.replayDLQ(ctx, params)
	default:
		return types.NewErrorResult(fmt.Sprintf("unknown governance action %q", envelope.Action)), nil
	}
}

// ── Guard action handlers ──

func (h *GovernanceHandler) getPendingActions(_ context.Context, _ json.RawMessage) (*types.ToolResult, error) {
	actions := h.guardMgr.PendingActions()
	data, err := json.Marshal(map[string]any{
		"actions": actions,
		"count":   len(actions),
	})
	if err != nil {
		return nil, fmt.Errorf("handlers.GovernanceHandler.getPendingActions: %w", err)
	}
	return &types.ToolResult{
		Content: []types.ToolContent{{Text: string(data)}},
	}, nil
}

func (h *GovernanceHandler) approveAction(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var p struct {
		ID    string `json:"id"`
		Notes string `json:"notes"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("handlers.GovernanceHandler.approveAction: %w", err)
	}
	if p.ID == "" {
		return nil, fmt.Errorf("id is required")
	}

	auth := mcp.AuthFromContext(ctx)
	action, err := h.guardMgr.Approve(p.ID, auth.PersonaID, p.Notes)
	if err != nil {
		return nil, fmt.Errorf("handlers.GovernanceHandler.approveAction: %w", err)
	}

	data, err := json.Marshal(map[string]any{
		"id":        action.ID,
		"status":    action.Status,
		"tool_name": action.ToolName,
	})
	if err != nil {
		return nil, fmt.Errorf("handlers.GovernanceHandler.approveAction: %w", err)
	}
	return &types.ToolResult{
		Content: []types.ToolContent{{Text: string(data)}},
	}, nil
}

func (h *GovernanceHandler) rejectAction(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var p struct {
		ID    string `json:"id"`
		Notes string `json:"notes"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("handlers.GovernanceHandler.rejectAction: %w", err)
	}
	if p.ID == "" {
		return nil, fmt.Errorf("id is required")
	}

	auth := mcp.AuthFromContext(ctx)
	action, err := h.guardMgr.Reject(p.ID, auth.PersonaID, p.Notes)
	if err != nil {
		return nil, fmt.Errorf("handlers.GovernanceHandler.rejectAction: %w", err)
	}

	data, err := json.Marshal(map[string]any{
		"id":        action.ID,
		"status":    action.Status,
		"tool_name": action.ToolName,
	})
	if err != nil {
		return nil, fmt.Errorf("handlers.GovernanceHandler.rejectAction: %w", err)
	}
	return &types.ToolResult{
		Content: []types.ToolContent{{Text: string(data)}},
	}, nil
}

func (h *GovernanceHandler) getActionDetail(_ context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("handlers.GovernanceHandler.getActionDetail: %w", err)
	}
	if p.ID == "" {
		return nil, fmt.Errorf("id is required")
	}

	action, err := h.guardMgr.GetAction(p.ID)
	if err != nil {
		return nil, fmt.Errorf("handlers.GovernanceHandler.getActionDetail: %w", err)
	}

	data, err := json.Marshal(action)
	if err != nil {
		return nil, fmt.Errorf("handlers.GovernanceHandler.getActionDetail: %w", err)
	}
	return &types.ToolResult{
		Content: []types.ToolContent{{Text: string(data)}},
	}, nil
}

func (h *GovernanceHandler) getActionHistory(_ context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var p struct {
		Limit int `json:"limit"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("handlers.GovernanceHandler.getActionHistory: %w", err)
	}
	if p.Limit <= 0 {
		p.Limit = 50
	}

	actions := h.guardMgr.History(p.Limit)
	data, err := json.Marshal(map[string]any{
		"actions": actions,
		"count":   len(actions),
	})
	if err != nil {
		return nil, fmt.Errorf("handlers.GovernanceHandler.getActionHistory: %w", err)
	}
	return &types.ToolResult{
		Content: []types.ToolContent{{Text: string(data)}},
	}, nil
}

// ── Interjection action handlers ──

func (h *GovernanceHandler) pullAndonCord(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Scope              string `json:"scope"`
		Severity           string `json:"severity"`
		Source             string `json:"source"`
		Reason             string `json:"reason"`
		CreatedBy          string `json:"created_by"`
		RemediationPersona string `json:"remediation_persona"`
		TraceID            string `json:"trace_id"`
		ExpiresMinutes     int    `json:"expires_minutes"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.GovernanceHandler.pullAndonCord: %w", err)
	}

	if args.Scope == "" {
		return types.NewErrorResult("scope is required"), nil
	}
	if args.Severity == "" {
		return types.NewErrorResult("severity is required"), nil
	}
	if args.Source == "" {
		return types.NewErrorResult("source is required"), nil
	}
	if args.Reason == "" {
		return types.NewErrorResult("reason is required"), nil
	}

	switch args.Severity {
	case "warning", "critical", "fatal":
	default:
		return types.NewErrorResult(fmt.Sprintf("invalid severity %q: must be warning, critical, or fatal", args.Severity)), nil
	}

	if h.ijMgr == nil {
		return types.NewErrorResult("interjection manager not available"), nil
	}

	ij := &types.Interjection{
		Scope:              args.Scope,
		Severity:           args.Severity,
		Source:             args.Source,
		Reason:             args.Reason,
		CreatedBy:          args.CreatedBy,
		RemediationPersona: args.RemediationPersona,
		TraceID:            args.TraceID,
	}

	if args.ExpiresMinutes > 0 {
		t := time.Now().Add(time.Duration(args.ExpiresMinutes) * time.Minute)
		ij.ExpiresAt = &t
	}

	id, err := h.ijMgr.Halt(ctx, ij)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("halt failed: %v", err)), nil
	}

	msg := fmt.Sprintf("Andon Cord pulled: id=%s scope=%s severity=%s.", id, args.Scope, args.Severity)
	if args.Severity == "critical" || args.Severity == "fatal" {
		msg += " Safe Mode ENGAGED."
	}

	return types.NewToolResult(map[string]any{
		"id":      id,
		"message": msg,
	}), nil
}

func (h *GovernanceHandler) getActiveInterjections(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Scope string `json:"scope"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.GovernanceHandler.getActiveInterjections: %w", err)
	}

	if h.ijMgr == nil {
		return types.NewErrorResult("interjection manager not available"), nil
	}

	var interjections []*types.Interjection
	var err error
	if args.Scope == "" {
		interjections, err = h.ijMgr.GetAllActive(ctx)
	} else {
		interjections, err = h.ijMgr.GetActive(ctx, args.Scope)
	}
	if err != nil {
		return nil, fmt.Errorf("handlers.GovernanceHandler.getActiveInterjections: %w", err)
	}

	if len(interjections) == 0 {
		return types.NewToolResult(map[string]any{
			"interjections": []*types.Interjection{},
			"count":         0,
		}), nil
	}

	return types.NewToolResult(map[string]any{
		"interjections": interjections,
		"count":         len(interjections),
	}), nil
}

func (h *GovernanceHandler) getInterjection(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.GovernanceHandler.getInterjection: %w", err)
	}
	if args.ID == "" {
		return types.NewErrorResult("id is required"), nil
	}

	if h.ijMgr == nil {
		return types.NewErrorResult("interjection manager not available"), nil
	}

	ij, err := h.ijMgr.GetByID(ctx, args.ID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("not found: %v", err)), nil
	}

	return types.NewToolResult(ij), nil
}

func (h *GovernanceHandler) resolveInterjection(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ID               string `json:"id"`
		Resolution       string `json:"resolution"`
		ResolutionAction string `json:"resolution_action"`
		ResolvedBy       string `json:"resolved_by"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.GovernanceHandler.resolveInterjection: %w", err)
	}
	if args.ID == "" {
		return types.NewErrorResult("id is required"), nil
	}
	if args.Resolution == "" {
		return types.NewErrorResult("resolution is required"), nil
	}

	if h.ijMgr == nil {
		return types.NewErrorResult("interjection manager not available"), nil
	}

	action := &types.ResolutionAction{
		InterjectionID: args.ID,
		ResolvedBy:     args.ResolvedBy,
		Resolution:     args.Resolution,
		Action:         args.ResolutionAction,
	}

	if err := h.ijMgr.Resolve(ctx, action); err != nil {
		return types.NewErrorResult(fmt.Sprintf("resolve failed: %v", err)), nil
	}

	return types.NewToolResult(map[string]any{
		"id":         args.ID,
		"status":     "resolved",
		"action":     action.Action,
		"resolution": args.Resolution,
	}), nil
}

func (h *GovernanceHandler) getInterjectionHistory(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Scope string `json:"scope"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.GovernanceHandler.getInterjectionHistory: %w", err)
	}
	if args.Scope == "" {
		return types.NewErrorResult("scope is required"), nil
	}

	if h.ijMgr == nil {
		return types.NewErrorResult("interjection manager not available"), nil
	}

	history, err := h.ijMgr.GetHistory(ctx, args.Scope, args.Limit)
	if err != nil {
		return nil, fmt.Errorf("handlers.GovernanceHandler.getInterjectionHistory: %w", err)
	}

	if len(history) == 0 {
		return types.NewToolResult(map[string]any{
			"interjections": []*types.Interjection{},
			"count":         0,
			"scope":         args.Scope,
		}), nil
	}

	return types.NewToolResult(map[string]any{
		"interjections": history,
		"count":         len(history),
		"scope":         args.Scope,
	}), nil
}

func (h *GovernanceHandler) getSafeModeStatus(_ context.Context, _ json.RawMessage) (*types.ToolResult, error) {
	if h.ijMgr == nil {
		return types.NewErrorResult("interjection manager not available"), nil
	}

	states := h.ijMgr.SafeMode().GetAllStates()
	if len(states) == 0 {
		return types.NewToolResult(map[string]any{
			"active": false,
			"scopes": []*interject.SafeModeState{},
			"count":  0,
		}), nil
	}

	return types.NewToolResult(map[string]any{
		"active": true,
		"scopes": states,
		"count":  len(states),
	}), nil
}

func (h *GovernanceHandler) requestTemporaryBypass(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Scope           string `json:"scope"`
		Pattern         string `json:"pattern"`
		GrantedBy       string `json:"granted_by"`
		DurationMinutes int    `json:"duration_minutes"`
		Reason          string `json:"reason"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.GovernanceHandler.requestTemporaryBypass: %w", err)
	}
	if args.Scope == "" {
		return types.NewErrorResult("scope is required"), nil
	}
	if args.Pattern == "" {
		return types.NewErrorResult("pattern is required"), nil
	}
	if args.GrantedBy == "" {
		return types.NewErrorResult("granted_by is required"), nil
	}
	if args.DurationMinutes <= 0 {
		return types.NewErrorResult("duration_minutes must be > 0"), nil
	}
	// Maximum bypass duration is 60 minutes (1 hour).
	if args.DurationMinutes > 60 {
		return types.NewErrorResult("duration_minutes cannot exceed 60 (max 1 hour)"), nil
	}

	if h.ijMgr == nil {
		return types.NewErrorResult("interjection manager not available"), nil
	}

	// Only ClearanceChiefOfStaff (Level 3) can grant sieve bypasses.
	clearance, err := h.ijMgr.GetClearanceLevel(ctx, args.GrantedBy)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("clearance lookup failed for %q: %v", args.GrantedBy, err)), nil
	}
	if clearance < 3 {
		return types.NewErrorResult(fmt.Sprintf("insufficient clearance: %q has level %d, bypass requires level 3 (ChiefOfStaff)", args.GrantedBy, clearance)), nil
	}

	bypass := &types.SieveBypass{
		Scope:     args.Scope,
		Pattern:   args.Pattern,
		GrantedBy: args.GrantedBy,
		ExpiresAt: time.Now().Add(time.Duration(args.DurationMinutes) * time.Minute),
		Reason:    args.Reason,
	}

	id, err := h.ijMgr.GrantBypass(ctx, bypass)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("grant bypass failed: %v", err)), nil
	}

	return types.NewToolResult(map[string]any{
		"id":      id,
		"message": fmt.Sprintf("Sieve bypass granted: scope=%s pattern=%q expires in %d minutes.", args.Scope, args.Pattern, args.DurationMinutes),
	}), nil
}

func (h *GovernanceHandler) listDLQ(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		InterjectionID string `json:"interjection_id"`
		Limit          int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.GovernanceHandler.listDLQ: %w", err)
	}
	if args.InterjectionID == "" {
		return types.NewErrorResult("interjection_id is required"), nil
	}

	if h.ijMgr == nil {
		return types.NewErrorResult("interjection manager not available"), nil
	}

	entries, err := h.ijMgr.ListDLQ(ctx, args.InterjectionID, args.Limit)
	if err != nil {
		return nil, fmt.Errorf("handlers.GovernanceHandler.listDLQ: %w", err)
	}

	if len(entries) == 0 {
		return types.NewToolResult(map[string]any{
			"entries":         []interface{}{},
			"count":           0,
			"interjection_id": args.InterjectionID,
		}), nil
	}

	return types.NewToolResult(map[string]any{
		"entries":         entries,
		"count":           len(entries),
		"interjection_id": args.InterjectionID,
	}), nil
}

func (h *GovernanceHandler) replayDLQ(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.GovernanceHandler.replayDLQ: %w", err)
	}
	if args.ID == "" {
		return types.NewErrorResult("id is required"), nil
	}

	if h.ijMgr == nil {
		return types.NewErrorResult("interjection manager not available"), nil
	}

	if err := h.ijMgr.ReplayDLQ(ctx, args.ID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("replay failed: %v", err)), nil
	}

	return types.NewToolResult(map[string]any{
		"id":     args.ID,
		"status": "replayed",
	}), nil
}
