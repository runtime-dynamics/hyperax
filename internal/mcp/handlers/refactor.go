package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/refactor"
	"github.com/hyperax/hyperax/internal/storage"
	"github.com/hyperax/hyperax/internal/workspace"
	"github.com/hyperax/hyperax/pkg/types"
)

// actionClearanceRefactor maps each refactor action to its minimum ABAC clearance.
var actionClearanceRefactor = map[string]int{
	"begin_transaction":    1,
	"commit_transaction":   1,
	"rollback_transaction": 1,
	"extract_block":        1,
	"insert_block":         1,
	"delete_block":         1,
	"move_symbol":          1,
	"rename_symbol":        1,
	"ensure_imports":       1,
	"create_file":          1,
}

// RefactorHandler implements the consolidated "refactor" MCP tool.
type RefactorHandler struct {
	store  *storage.Store
	txMgr  *refactor.TransactionManager
	logger *slog.Logger
}

// NewRefactorHandler creates a RefactorHandler.
func NewRefactorHandler(store *storage.Store, txMgr *refactor.TransactionManager, logger *slog.Logger) *RefactorHandler {
	return &RefactorHandler{
		store:  store,
		txMgr:  txMgr,
		logger: logger,
	}
}

// RegisterTools registers the consolidated refactor tool with the MCP registry.
func (h *RefactorHandler) RegisterTools(registry *mcp.ToolRegistry) {
	registry.Register(
		"refactor",
		"Code refactoring toolkit: manage transactions, extract/insert/delete code blocks, "+
			"move and rename symbols, manage imports, create files. "+
			"Actions: begin_transaction | commit_transaction | rollback_transaction | extract_block | "+
			"insert_block | delete_block | move_symbol | rename_symbol | ensure_imports | create_file",
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"action":          {"type": "string", "enum": ["begin_transaction", "commit_transaction", "rollback_transaction", "extract_block", "insert_block", "delete_block", "move_symbol", "rename_symbol", "ensure_imports", "create_file"], "description": "Action to perform"},
				"transaction_id":  {"type": "string", "description": "Transaction ID"},
				"workspace_name":  {"type": "string", "description": "Workspace name"},
				"path":            {"type": "string", "description": "Relative path within workspace"},
				"start_line":      {"type": "integer", "description": "First line (1-based)"},
				"end_line":        {"type": "integer", "description": "Last line (1-based, inclusive)"},
				"after_line":      {"type": "integer", "description": "Line to insert after (0 = prepend)"},
				"content":         {"type": "string", "description": "Code content to insert/create"},
				"src_path":        {"type": "string", "description": "Source file path (move_symbol)"},
				"dest_path":       {"type": "string", "description": "Destination file path (move_symbol)"},
				"old_name":        {"type": "string", "description": "Current symbol name (rename_symbol)"},
				"new_name":        {"type": "string", "description": "New symbol name (rename_symbol)"},
				"imports":         {"type": "array", "items": {"type": "string"}, "description": "Import paths (ensure_imports)"}
			},
			"required": ["action"]
		}`),
		h.dispatch,
	)
}

// dispatch routes the consolidated "refactor" tool to the correct handler method.
func (h *RefactorHandler) dispatch(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var envelope struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return nil, fmt.Errorf("handlers.RefactorHandler.dispatch: %w", err)
	}

	if err := checkActionClearance(ctx, envelope.Action, actionClearanceRefactor); err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	switch envelope.Action {
	case "begin_transaction":
		return h.beginTransaction(ctx, params)
	case "commit_transaction":
		return h.commitTransaction(ctx, params)
	case "rollback_transaction":
		return h.rollbackTransaction(ctx, params)
	case "extract_block":
		return h.extractCodeBlock(ctx, params)
	case "insert_block":
		return h.insertCodeBlock(ctx, params)
	case "delete_block":
		return h.deleteCodeBlock(ctx, params)
	case "move_symbol":
		return h.moveSymbol(ctx, params)
	case "rename_symbol":
		return h.renameSymbol(ctx, params)
	case "ensure_imports":
		return h.ensureImports(ctx, params)
	case "create_file":
		return h.createFile(ctx, params)
	default:
		return types.NewErrorResult(fmt.Sprintf("unknown refactor action %q", envelope.Action)), nil
	}
}

func (h *RefactorHandler) resolveWorkspacePath(ctx context.Context, workspaceName, relPath string) (string, *types.ToolResult) {
	wsInfo, err := h.store.Workspaces.GetWorkspace(ctx, workspaceName)
	if err != nil {
		return "", types.NewErrorResult(fmt.Sprintf("workspace %q not found", workspaceName))
	}

	validated, err := workspace.ValidatePath(wsInfo.RootPath, relPath)
	if err != nil {
		return "", types.NewErrorResult(err.Error())
	}

	return validated, nil
}

func (h *RefactorHandler) snapshotIfTransaction(txID, absPath string) *types.ToolResult {
	if txID == "" {
		return nil
	}
	if err := h.txMgr.SnapshotFile(txID, absPath); err != nil {
		return types.NewErrorResult(fmt.Sprintf("snapshot file: %v", err))
	}
	return nil
}

func (h *RefactorHandler) markModifiedIfTransaction(txID, absPath string) {
	if txID == "" {
		return
	}
	_ = h.txMgr.MarkModified(txID, absPath)
}

func (h *RefactorHandler) beginTransaction(_ context.Context, _ json.RawMessage) (*types.ToolResult, error) {
	txID, err := h.txMgr.Begin()
	if err != nil {
		return nil, fmt.Errorf("handlers.RefactorHandler.beginTransaction: %w", err)
	}

	return types.NewToolResult(fmt.Sprintf("Transaction started: %s", txID)), nil
}

func (h *RefactorHandler) commitTransaction(_ context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		TransactionID string `json:"transaction_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.RefactorHandler.commitTransaction: %w", err)
	}

	if err := h.txMgr.Commit(args.TransactionID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("commit failed: %v", err)), nil
	}

	return types.NewToolResult(fmt.Sprintf("Transaction %s committed.", args.TransactionID)), nil
}

func (h *RefactorHandler) rollbackTransaction(_ context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		TransactionID string `json:"transaction_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.RefactorHandler.rollbackTransaction: %w", err)
	}

	if err := h.txMgr.Rollback(args.TransactionID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("rollback failed: %v", err)), nil
	}

	return types.NewToolResult(fmt.Sprintf("Transaction %s rolled back. All files restored.", args.TransactionID)), nil
}

func (h *RefactorHandler) extractCodeBlock(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Path          string `json:"path"`
		StartLine     int    `json:"start_line"`
		EndLine       int    `json:"end_line"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.RefactorHandler.extractCodeBlock: %w", err)
	}

	absPath, errResult := h.resolveWorkspacePath(ctx, args.WorkspaceName, args.Path)
	if errResult != nil {
		return errResult, nil
	}

	code, err := refactor.ExtractCodeBlock(absPath, args.StartLine, args.EndLine)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("extract: %v", err)), nil
	}

	header := fmt.Sprintf("Extracted lines %d-%d from %s:\n", args.StartLine, args.EndLine, args.Path)
	return types.NewToolResult(header + code), nil
}

func (h *RefactorHandler) insertCodeBlock(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Path          string `json:"path"`
		AfterLine     int    `json:"after_line"`
		Content       string `json:"content"`
		TransactionID string `json:"transaction_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.RefactorHandler.insertCodeBlock: %w", err)
	}

	absPath, errResult := h.resolveWorkspacePath(ctx, args.WorkspaceName, args.Path)
	if errResult != nil {
		return errResult, nil
	}

	if errResult := h.snapshotIfTransaction(args.TransactionID, absPath); errResult != nil {
		return errResult, nil
	}

	if err := refactor.InsertCodeBlock(absPath, args.AfterLine, args.Content); err != nil {
		return types.NewErrorResult(fmt.Sprintf("insert: %v", err)), nil
	}

	h.markModifiedIfTransaction(args.TransactionID, absPath)

	lineCount := len(strings.Split(args.Content, "\n"))
	return types.NewToolResult(fmt.Sprintf("Inserted %d line(s) after line %d in %s.", lineCount, args.AfterLine, args.Path)), nil
}

func (h *RefactorHandler) deleteCodeBlock(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Path          string `json:"path"`
		StartLine     int    `json:"start_line"`
		EndLine       int    `json:"end_line"`
		TransactionID string `json:"transaction_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.RefactorHandler.deleteCodeBlock: %w", err)
	}

	absPath, errResult := h.resolveWorkspacePath(ctx, args.WorkspaceName, args.Path)
	if errResult != nil {
		return errResult, nil
	}

	if errResult := h.snapshotIfTransaction(args.TransactionID, absPath); errResult != nil {
		return errResult, nil
	}

	if err := refactor.DeleteCodeBlock(absPath, args.StartLine, args.EndLine); err != nil {
		return types.NewErrorResult(fmt.Sprintf("delete: %v", err)), nil
	}

	h.markModifiedIfTransaction(args.TransactionID, absPath)

	deleted := args.EndLine - args.StartLine + 1
	return types.NewToolResult(fmt.Sprintf("Deleted %d line(s) (%d-%d) from %s.", deleted, args.StartLine, args.EndLine, args.Path)), nil
}

func (h *RefactorHandler) moveSymbol(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		SrcPath       string `json:"src_path"`
		StartLine     int    `json:"start_line"`
		EndLine       int    `json:"end_line"`
		DestPath      string `json:"dest_path"`
		AfterLine     int    `json:"after_line"`
		TransactionID string `json:"transaction_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.RefactorHandler.moveSymbol: %w", err)
	}

	srcAbs, errResult := h.resolveWorkspacePath(ctx, args.WorkspaceName, args.SrcPath)
	if errResult != nil {
		return errResult, nil
	}
	destAbs, errResult := h.resolveWorkspacePath(ctx, args.WorkspaceName, args.DestPath)
	if errResult != nil {
		return errResult, nil
	}

	if errResult := h.snapshotIfTransaction(args.TransactionID, srcAbs); errResult != nil {
		return errResult, nil
	}
	if errResult := h.snapshotIfTransaction(args.TransactionID, destAbs); errResult != nil {
		return errResult, nil
	}

	if err := refactor.MoveSymbol(srcAbs, args.StartLine, args.EndLine, destAbs, args.AfterLine); err != nil {
		return types.NewErrorResult(fmt.Sprintf("move symbol: %v", err)), nil
	}

	h.markModifiedIfTransaction(args.TransactionID, srcAbs)
	h.markModifiedIfTransaction(args.TransactionID, destAbs)

	moved := args.EndLine - args.StartLine + 1
	return types.NewToolResult(fmt.Sprintf(
		"Moved %d line(s) from %s:%d-%d to %s after line %d.",
		moved, args.SrcPath, args.StartLine, args.EndLine, args.DestPath, args.AfterLine,
	)), nil
}

func (h *RefactorHandler) ensureImports(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string   `json:"workspace_name"`
		Path          string   `json:"path"`
		Imports       []string `json:"imports"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.RefactorHandler.ensureImports: %w", err)
	}

	absPath, errResult := h.resolveWorkspacePath(ctx, args.WorkspaceName, args.Path)
	if errResult != nil {
		return errResult, nil
	}

	if err := refactor.EnsureImports(absPath, args.Imports); err != nil {
		return types.NewErrorResult(fmt.Sprintf("ensure imports: %v", err)), nil
	}

	return types.NewToolResult(fmt.Sprintf(
		"Ensured %d import(s) in %s.", len(args.Imports), args.Path,
	)), nil
}

func (h *RefactorHandler) createFile(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Path          string `json:"path"`
		Content       string `json:"content"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.RefactorHandler.createFile: %w", err)
	}

	absPath, errResult := h.resolveWorkspacePath(ctx, args.WorkspaceName, args.Path)
	if errResult != nil {
		return errResult, nil
	}

	if _, err := os.Stat(absPath); err == nil {
		return types.NewErrorResult(fmt.Sprintf("file already exists: %s", args.Path)), nil
	}

	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return types.NewErrorResult(fmt.Sprintf("create directories: %v", err)), nil
	}

	if err := os.WriteFile(absPath, []byte(args.Content), 0644); err != nil {
		return types.NewErrorResult(fmt.Sprintf("write file: %v", err)), nil
	}

	return types.NewToolResult(fmt.Sprintf("Created %s (%d bytes).", args.Path, len(args.Content))), nil
}

func (h *RefactorHandler) renameSymbol(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Path          string `json:"path"`
		OldName       string `json:"old_name"`
		NewName       string `json:"new_name"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.RefactorHandler.renameSymbol: %w", err)
	}

	absPath, errResult := h.resolveWorkspacePath(ctx, args.WorkspaceName, args.Path)
	if errResult != nil {
		return errResult, nil
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("read file: %v", err)), nil
	}

	original := string(data)
	replaced := strings.ReplaceAll(original, args.OldName, args.NewName)

	if replaced == original {
		return types.NewToolResult(fmt.Sprintf("No occurrences of %q found in %s.", args.OldName, args.Path)), nil
	}

	if err := os.WriteFile(absPath, []byte(replaced), 0644); err != nil {
		return types.NewErrorResult(fmt.Sprintf("write file: %v", err)), nil
	}

	count := strings.Count(original, args.OldName)
	return types.NewToolResult(fmt.Sprintf(
		"Renamed %q to %q in %s (%d occurrence(s)).", args.OldName, args.NewName, args.Path, count,
	)), nil
}
