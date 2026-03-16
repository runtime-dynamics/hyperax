package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/storage"
	"github.com/hyperax/hyperax/pkg/types"
)

// actionClearanceAudit maps each audit action to its minimum ABAC clearance.
var actionClearanceAudit = map[string]int{
	"list":        0, // was list_audits
	"get_items":   0, // was get_audit_items
	"get_progress": 0, // was get_audit_progress
	"update_item": 1, // was update_audit_item
	"complete_item": 1, // was complete_audit_item
	"get_detail":  0, // was get_audit_item_detail
}

// AuditHandler implements the consolidated "audit" MCP tool.
type AuditHandler struct {
	store *storage.Store
}

// NewAuditHandler creates an AuditHandler.
func NewAuditHandler(store *storage.Store) *AuditHandler {
	return &AuditHandler{store: store}
}

// RegisterTools registers the consolidated audit tool with the MCP registry.
func (h *AuditHandler) RegisterTools(registry *mcp.ToolRegistry) {
	registry.Register(
		"audit",
		"Audit management: list audits, get items, check progress, update/complete items. "+
			"Actions: list | get_items | get_progress | update_item | complete_item | get_detail",
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"action":         {"type": "string", "enum": ["list", "get_items", "get_progress", "update_item", "complete_item", "get_detail"], "description": "Action to perform"},
				"workspace_name": {"type": "string", "description": "Workspace name (list action)"},
				"audit_id":       {"type": "string", "description": "Audit ID (get_items, get_progress actions)"},
				"item_id":        {"type": "string", "description": "Audit item ID (update_item, complete_item, get_detail actions)"},
				"status":         {"type": "string", "description": "Status: pending, pass, fail, skip (update_item, complete_item actions)"},
				"findings":       {"type": "string", "description": "Findings or notes as JSON string (update_item, complete_item actions)"}
			},
			"required": ["action"]
		}`),
		h.dispatch,
	)
}

// dispatch routes the consolidated "audit" tool to the correct handler method.
func (h *AuditHandler) dispatch(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var envelope struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return nil, fmt.Errorf("handlers.AuditHandler.dispatch: %w", err)
	}

	if err := checkActionClearance(ctx, envelope.Action, actionClearanceAudit); err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	switch envelope.Action {
	case "list":
		return h.listAudits(ctx, params)
	case "get_items":
		return h.getAuditItems(ctx, params)
	case "get_progress":
		return h.getAuditProgress(ctx, params)
	case "update_item":
		return h.updateAuditItem(ctx, params)
	case "complete_item":
		return h.completeAuditItem(ctx, params)
	case "get_detail":
		return h.getAuditItemDetail(ctx, params)
	default:
		return types.NewErrorResult(fmt.Sprintf("unknown audit action %q: valid actions are list, get_items, get_progress, update_item, complete_item, get_detail", envelope.Action)), nil
	}
}

func (h *AuditHandler) listAudits(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.AuditHandler.listAudits: %w", err)
	}

	if args.WorkspaceName == "" {
		return types.NewErrorResult("workspace_name is required"), nil
	}

	if h.store.Audits == nil {
		return types.NewErrorResult("audit repository not available"), nil
	}

	audits, err := h.store.Audits.ListAudits(ctx, args.WorkspaceName)
	if err != nil {
		return nil, fmt.Errorf("handlers.AuditHandler.listAudits: %w", err)
	}

	if len(audits) == 0 {
		return types.NewToolResult("No audits found."), nil
	}

	type auditSummary struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Status    string `json:"status"`
		AuditType string `json:"audit_type"`
	}

	summaries := make([]auditSummary, len(audits))
	for i, a := range audits {
		summaries[i] = auditSummary{
			ID:        a.ID,
			Name:      a.Name,
			Status:    a.Status,
			AuditType: a.AuditType,
		}
	}
	return types.NewToolResult(summaries), nil
}

func (h *AuditHandler) getAuditItems(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		AuditID string `json:"audit_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.AuditHandler.getAuditItems: %w", err)
	}

	if args.AuditID == "" {
		return types.NewErrorResult("audit_id is required"), nil
	}

	if h.store.Audits == nil {
		return types.NewErrorResult("audit repository not available"), nil
	}

	items, err := h.store.Audits.GetAuditItems(ctx, args.AuditID)
	if err != nil {
		return nil, fmt.Errorf("handlers.AuditHandler.getAuditItems: %w", err)
	}

	if len(items) == 0 {
		return types.NewToolResult("No audit items found."), nil
	}

	type itemSummary struct {
		ID         string `json:"id"`
		ItemType   string `json:"item_type"`
		FilePath   string `json:"file_path,omitempty"`
		SymbolName string `json:"symbol_name,omitempty"`
		Status     string `json:"status"`
	}

	summaries := make([]itemSummary, len(items))
	for i, item := range items {
		summaries[i] = itemSummary{
			ID:         item.ID,
			ItemType:   item.ItemType,
			FilePath:   item.FilePath,
			SymbolName: item.SymbolName,
			Status:     item.Status,
		}
	}
	return types.NewToolResult(summaries), nil
}

func (h *AuditHandler) getAuditProgress(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		AuditID string `json:"audit_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.AuditHandler.getAuditProgress: %w", err)
	}

	if args.AuditID == "" {
		return types.NewErrorResult("audit_id is required"), nil
	}

	if h.store.Audits == nil {
		return types.NewErrorResult("audit repository not available"), nil
	}

	progress, err := h.store.Audits.GetAuditProgress(ctx, args.AuditID)
	if err != nil {
		return nil, fmt.Errorf("handlers.AuditHandler.getAuditProgress: %w", err)
	}

	return types.NewToolResult(progress), nil
}

func (h *AuditHandler) updateAuditItem(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ItemID   string `json:"item_id"`
		Status   string `json:"status"`
		Findings string `json:"findings"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.AuditHandler.updateAuditItem: %w", err)
	}

	if args.ItemID == "" || args.Status == "" {
		return types.NewErrorResult("item_id and status are required"), nil
	}

	if !isValidAuditStatus(args.Status) {
		return types.NewErrorResult(fmt.Sprintf("invalid status %q: must be pending, pass, fail, or skip", args.Status)), nil
	}

	if h.store.Audits == nil {
		return types.NewErrorResult("audit repository not available"), nil
	}

	if err := h.store.Audits.UpdateAuditItem(ctx, args.ItemID, args.Status, args.Findings); err != nil {
		return types.NewErrorResult(fmt.Sprintf("update audit item: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"item_id": args.ItemID,
		"status":  args.Status,
		"message": "Audit item updated.",
	}), nil
}

func (h *AuditHandler) completeAuditItem(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ItemID   string `json:"item_id"`
		Status   string `json:"status"`
		Findings string `json:"findings"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.AuditHandler.completeAuditItem: %w", err)
	}

	if args.ItemID == "" || args.Status == "" {
		return types.NewErrorResult("item_id and status are required"), nil
	}

	// complete_item only accepts terminal statuses.
	if args.Status != "pass" && args.Status != "fail" && args.Status != "skip" {
		return types.NewErrorResult(fmt.Sprintf("invalid completion status %q: must be pass, fail, or skip", args.Status)), nil
	}

	if h.store.Audits == nil {
		return types.NewErrorResult("audit repository not available"), nil
	}

	if err := h.store.Audits.UpdateAuditItem(ctx, args.ItemID, args.Status, args.Findings); err != nil {
		return types.NewErrorResult(fmt.Sprintf("complete audit item: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"item_id": args.ItemID,
		"status":  args.Status,
		"message": "Audit item completed.",
	}), nil
}

func (h *AuditHandler) getAuditItemDetail(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ItemID string `json:"item_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.AuditHandler.getAuditItemDetail: %w", err)
	}

	if args.ItemID == "" {
		return types.NewErrorResult("item_id is required"), nil
	}

	if h.store.Audits == nil {
		return types.NewErrorResult("audit repository not available"), nil
	}

	item, err := h.store.Audits.GetAuditItem(ctx, args.ItemID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get audit item: %v", err)), nil
	}

	detail := map[string]interface{}{
		"id":           item.ID,
		"audit_id":     item.AuditID,
		"item_type":    item.ItemType,
		"file_path":    item.FilePath,
		"symbol_name":  item.SymbolName,
		"status":       item.Status,
		"context_data": item.ContextData,
		"findings":     item.Findings,
		"reviewed_at":  nil,
	}
	if item.ReviewedAt != nil {
		detail["reviewed_at"] = item.ReviewedAt.Format("2006-01-02 15:04:05")
	}

	return types.NewToolResult(detail), nil
}

// isValidAuditStatus returns true if the status is a recognized audit item status.
func isValidAuditStatus(status string) bool {
	switch status {
	case "pending", "pass", "fail", "skip":
		return true
	default:
		return false
	}
}
