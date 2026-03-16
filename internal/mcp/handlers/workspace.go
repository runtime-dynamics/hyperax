package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hyperax/hyperax/internal/federation"
	"github.com/hyperax/hyperax/internal/index"
	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/storage"
	"github.com/hyperax/hyperax/internal/workspace"
	"github.com/hyperax/hyperax/pkg/types"
)

// actionClearanceWorkspace maps each workspace action to its minimum ABAC clearance.
var actionClearanceWorkspace = map[string]int{
	"list":             0, // was list_workspaces
	"register":         1, // was register_workspace
	"delete":           1, // was delete_workspace
	"get_structure":    0, // was get_project_structure
	"list_files":       0, // was list_files_in_dir
	"reindex":          1, // was trigger_reindex
	"git_info":         0, // was get_git_info
	"diff":             0, // was diff_file
	"recent_changes":   0, // was get_recent_changes
	"connect_mcp":      2, // was connect_mcp_server
	"disconnect_mcp":   2, // was disconnect_mcp_server
	"refresh_mcp":      2, // was refresh_mcp_connection
	"list_mcp_connections": 0, // was list_mcp_connections
}

// WorkspaceHandler implements the consolidated "workspace" MCP tool.
type WorkspaceHandler struct {
	store        *storage.Store
	indexer      *index.Indexer
	indexWatcher *index.IndexWatcher
	fedManager   *federation.Manager
}

// NewWorkspaceHandler creates a WorkspaceHandler.
func NewWorkspaceHandler(store *storage.Store, indexer *index.Indexer, indexWatcher *index.IndexWatcher) *WorkspaceHandler {
	return &WorkspaceHandler{store: store, indexer: indexer, indexWatcher: indexWatcher}
}

// SetFederationManager wires the federation manager for MCP federation actions.
func (h *WorkspaceHandler) SetFederationManager(mgr *federation.Manager) {
	h.fedManager = mgr
}

// RegisterTools registers the consolidated workspace tool with the MCP registry.
func (h *WorkspaceHandler) RegisterTools(registry *mcp.ToolRegistry) {
	registry.Register(
		"workspace",
		"Workspace management: list, register, delete workspaces; browse structure and files; "+
			"reindex; git info, diff, recent changes; MCP federation. "+
			"Actions: list | register | delete | get_structure | list_files | reindex | "+
			"git_info | diff | recent_changes | connect_mcp | disconnect_mcp | refresh_mcp | list_mcp_connections",
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"action":          {"type": "string", "enum": ["list", "register", "delete", "get_structure", "list_files", "reindex", "git_info", "diff", "recent_changes", "connect_mcp", "disconnect_mcp", "refresh_mcp", "list_mcp_connections"], "description": "Action to perform"},
				"workspace_name":  {"type": "string", "description": "Workspace name"},
				"name":            {"type": "string", "description": "Workspace name (register action)"},
				"root_path":       {"type": "string", "description": "Absolute path to workspace root (register action)"},
				"max_depth":       {"type": "integer", "description": "Max directory depth (get_structure, default 3)"},
				"include_files":   {"type": "boolean", "description": "Include files in tree (get_structure, default true)"},
				"path":            {"type": "string", "description": "Relative path within workspace"},
				"limit":           {"type": "integer", "description": "Max commits (recent_changes, default 10)"},
				"endpoint":        {"type": "string", "description": "Remote MCP server URL (connect_mcp)"},
				"auth_token":      {"type": "string", "description": "Bearer token (connect_mcp)"},
				"connection_id":   {"type": "string", "description": "Connection ID (disconnect_mcp, refresh_mcp)"}
			},
			"required": ["action"]
		}`),
		h.dispatch,
	)
}

func (h *WorkspaceHandler) dispatch(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var envelope struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return nil, fmt.Errorf("handlers.WorkspaceHandler.dispatch: %w", err)
	}

	if err := checkActionClearance(ctx, envelope.Action, actionClearanceWorkspace); err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	switch envelope.Action {
	case "list":
		return h.listWorkspaces(ctx, params)
	case "register":
		return h.registerWorkspace(ctx, params)
	case "delete":
		return h.deleteWorkspace(ctx, params)
	case "get_structure":
		return h.getProjectStructure(ctx, params)
	case "list_files":
		return h.listFilesInDir(ctx, params)
	case "reindex":
		return h.triggerReindex(ctx, params)
	case "git_info":
		return h.getGitInfo(ctx, params)
	case "diff":
		return h.diffFile(ctx, params)
	case "recent_changes":
		return h.getRecentChanges(ctx, params)
	case "connect_mcp":
		return h.connectMCPServer(ctx, params)
	case "disconnect_mcp":
		return h.disconnectMCPServer(ctx, params)
	case "refresh_mcp":
		return h.refreshMCPConnection(ctx, params)
	case "list_mcp_connections":
		return h.listMCPConnections(ctx, params)
	default:
		return types.NewErrorResult(fmt.Sprintf("unknown workspace action %q", envelope.Action)), nil
	}
}

// ---------- workspace management ----------

func (h *WorkspaceHandler) listWorkspaces(ctx context.Context, _ json.RawMessage) (*types.ToolResult, error) {
	workspaces, err := h.store.Workspaces.ListWorkspaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("handlers.WorkspaceHandler.listWorkspaces: %w", err)
	}
	if len(workspaces) == 0 {
		return types.NewToolResult([]interface{}{}), nil
	}
	return types.NewToolResult(workspaces), nil
}

func (h *WorkspaceHandler) registerWorkspace(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Name     string `json:"name"`
		RootPath string `json:"root_path"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.WorkspaceHandler.registerWorkspace: %w", err)
	}
	if args.Name == "" {
		return types.NewErrorResult("name is required"), nil
	}
	if args.RootPath == "" {
		return types.NewErrorResult("root_path is required"), nil
	}

	info, err := os.Stat(args.RootPath)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("root_path %q does not exist: %v", args.RootPath, err)), nil
	}
	if !info.IsDir() {
		return types.NewErrorResult(fmt.Sprintf("root_path %q is not a directory", args.RootPath)), nil
	}

	wsID := generateWorkspaceID(args.RootPath)

	existing, existErr := h.store.Workspaces.GetWorkspace(ctx, args.Name)
	if existErr == nil && existing != nil && existing.ID != wsID {
		_ = h.store.Workspaces.DeleteWorkspace(ctx, args.Name)
	}

	ws := &types.WorkspaceInfo{
		ID:       wsID,
		Name:     args.Name,
		RootPath: args.RootPath,
	}
	if err := h.store.Workspaces.CreateWorkspace(ctx, ws); err != nil {
		return types.NewErrorResult(fmt.Sprintf("create workspace: %v", err)), nil
	}

	if h.indexWatcher != nil {
		_ = h.indexWatcher.AddWorkspace(ctx, args.RootPath)
	}

	return types.NewToolResult(ws), nil
}

func (h *WorkspaceHandler) deleteWorkspace(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.WorkspaceHandler.deleteWorkspace: %w", err)
	}
	if args.WorkspaceName == "" {
		return types.NewErrorResult("workspace_name is required"), nil
	}

	if err := h.store.Workspaces.DeleteWorkspace(ctx, args.WorkspaceName); err != nil {
		return types.NewErrorResult(fmt.Sprintf("delete workspace: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"message": fmt.Sprintf("Workspace %q deleted.", args.WorkspaceName),
	}), nil
}

func (h *WorkspaceHandler) getProjectStructure(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		MaxDepth      int    `json:"max_depth"`
		IncludeFiles  *bool  `json:"include_files"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.WorkspaceHandler.getProjectStructure: %w", err)
	}
	if args.MaxDepth <= 0 {
		args.MaxDepth = 3
	}
	includeFiles := true
	if args.IncludeFiles != nil {
		includeFiles = *args.IncludeFiles
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	tree := buildTree(ws.RootPath, "", args.MaxDepth, includeFiles)
	return types.NewToolResult(tree), nil
}

func (h *WorkspaceHandler) listFilesInDir(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Path          string `json:"path"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.WorkspaceHandler.listFilesInDir: %w", err)
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	fullPath, err := workspace.ValidatePath(ws.RootPath, args.Path)
	if err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("read dir: %v", err)), nil
	}

	type fileEntry struct {
		Name  string `json:"name"`
		IsDir bool   `json:"is_dir"`
		Size  int64  `json:"size,omitempty"`
	}

	files := make([]fileEntry, 0, len(entries))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		fe := fileEntry{Name: e.Name(), IsDir: e.IsDir()}
		if !e.IsDir() {
			info, err := e.Info()
			if err == nil {
				fe.Size = info.Size()
			}
		}
		files = append(files, fe)
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].IsDir != files[j].IsDir {
			return files[i].IsDir
		}
		return files[i].Name < files[j].Name
	})

	return types.NewToolResult(files), nil
}

func (h *WorkspaceHandler) triggerReindex(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.WorkspaceHandler.triggerReindex: %w", err)
	}

	if h.indexer == nil {
		return types.NewErrorResult("indexer not available"), nil
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	result, err := h.indexer.IndexWorkspace(ctx, ws.Name, ws.RootPath)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("reindex failed: %v", err)), nil
	}

	type reindexResult struct {
		Workspace    string `json:"workspace"`
		FilesScanned int    `json:"files_scanned"`
		FilesSkipped int    `json:"files_skipped"`
		SymbolsFound int    `json:"symbols_found"`
		DocsChunked  int    `json:"docs_chunked"`
		Duration     string `json:"duration"`
	}

	return types.NewToolResult(reindexResult{
		Workspace:    ws.Name,
		FilesScanned: result.FilesScanned,
		FilesSkipped: result.FilesSkipped,
		SymbolsFound: result.SymbolsFound,
		DocsChunked:  result.DocsChunked,
		Duration:     result.Duration.String(),
	}), nil
}

// ---------- git operations ----------

func (h *WorkspaceHandler) getGitInfo(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.WorkspaceHandler.getGitInfo: %w", err)
	}
	if args.WorkspaceName == "" {
		return types.NewErrorResult("workspace_name is required"), nil
	}

	if h.store.Git == nil {
		return types.NewErrorResult("git repo not available"), nil
	}

	info, err := h.store.Git.GetInfo(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get git info: %v", err)), nil
	}

	dirty := "clean"
	if info.IsDirty {
		dirty = "dirty"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Git info for workspace %q:\n", info.WorkspaceName)
	fmt.Fprintf(&sb, "  Branch:     %s\n", info.Branch)
	fmt.Fprintf(&sb, "  Commit:     %s\n", info.CommitHash)
	fmt.Fprintf(&sb, "  Remote:     %s\n", info.RemoteURL)
	fmt.Fprintf(&sb, "  Status:     %s\n", dirty)

	return types.NewToolResult(sb.String()), nil
}

func (h *WorkspaceHandler) getRecentChanges(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Limit         int    `json:"limit"`
		Path          string `json:"path"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.WorkspaceHandler.getRecentChanges: %w", err)
	}
	if args.WorkspaceName == "" {
		return types.NewErrorResult("workspace_name is required"), nil
	}

	if h.store.Git == nil {
		return types.NewErrorResult("git repo not available"), nil
	}

	if args.Limit == 0 {
		args.Limit = 10
	}

	changes, err := h.store.Git.GetRecentChanges(ctx, args.WorkspaceName, args.Limit, args.Path)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get recent changes: %v", err)), nil
	}

	if len(changes) == 0 {
		return types.NewToolResult("No recent changes found."), nil
	}

	return types.NewToolResult(changes), nil
}

func (h *WorkspaceHandler) diffFile(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Path          string `json:"path"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.WorkspaceHandler.diffFile: %w", err)
	}
	if args.WorkspaceName == "" {
		return types.NewErrorResult("workspace_name is required"), nil
	}
	if args.Path == "" {
		return types.NewErrorResult("path is required"), nil
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	cmd := exec.CommandContext(ctx, "git", "diff", args.Path)
	cmd.Dir = ws.RootPath
	out, err := cmd.Output()
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("git diff: %v", err)), nil
	}

	diff := strings.TrimSpace(string(out))
	if diff == "" {
		return types.NewToolResult(fmt.Sprintf("No changes for %s.", args.Path)), nil
	}

	return types.NewToolResult(diff), nil
}

// ---------- MCP federation ----------

func (h *WorkspaceHandler) connectMCPServer(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Endpoint  string `json:"endpoint"`
		AuthToken string `json:"auth_token"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.WorkspaceHandler.connectMCPServer: %w", err)
	}
	if args.Endpoint == "" {
		return types.NewErrorResult("endpoint is required"), nil
	}
	if h.fedManager == nil {
		return types.NewErrorResult("federation manager not available"), nil
	}

	conn, err := h.fedManager.Connect(ctx, args.Endpoint, args.AuthToken)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("connect failed: %v", err)), nil
	}

	return types.NewToolResult(map[string]any{
		"connection_id": conn.ID,
		"endpoint":      conn.Endpoint,
		"tools":         conn.Tools,
		"tool_count":    len(conn.Tools),
	}), nil
}

func (h *WorkspaceHandler) disconnectMCPServer(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ConnectionID string `json:"connection_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.WorkspaceHandler.disconnectMCPServer: %w", err)
	}
	if args.ConnectionID == "" {
		return types.NewErrorResult("connection_id is required"), nil
	}
	if h.fedManager == nil {
		return types.NewErrorResult("federation manager not available"), nil
	}

	if err := h.fedManager.Disconnect(args.ConnectionID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("disconnect failed: %v", err)), nil
	}

	return types.NewToolResult(map[string]any{
		"connection_id": args.ConnectionID,
		"status":        "disconnected",
	}), nil
}

func (h *WorkspaceHandler) refreshMCPConnection(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ConnectionID string `json:"connection_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.WorkspaceHandler.refreshMCPConnection: %w", err)
	}
	if args.ConnectionID == "" {
		return types.NewErrorResult("connection_id is required"), nil
	}
	if h.fedManager == nil {
		return types.NewErrorResult("federation manager not available"), nil
	}

	conn, err := h.fedManager.Refresh(ctx, args.ConnectionID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("refresh failed: %v", err)), nil
	}

	return types.NewToolResult(map[string]any{
		"connection_id": conn.ID,
		"endpoint":      conn.Endpoint,
		"tools":         conn.Tools,
		"tool_count":    len(conn.Tools),
		"status":        "refreshed",
	}), nil
}

func (h *WorkspaceHandler) listMCPConnections(_ context.Context, _ json.RawMessage) (*types.ToolResult, error) {
	if h.fedManager == nil {
		return types.NewErrorResult("federation manager not available"), nil
	}

	conns := h.fedManager.List()

	items := make([]map[string]any, 0, len(conns))
	for _, c := range conns {
		items = append(items, map[string]any{
			"id":           c.ID,
			"endpoint":     c.Endpoint,
			"tool_count":   len(c.Tools),
			"connected_at": c.ConnectedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	return types.NewToolResult(map[string]any{
		"connections": items,
		"count":       len(items),
	}), nil
}

// ---------- helpers ----------

func generateWorkspaceID(rootPath string) string {
	cmd := exec.Command("git", "-C", rootPath, "remote", "get-url", "origin")
	out, err := cmd.Output()
	var source string
	if err == nil {
		source = strings.TrimSpace(string(out))
	}
	if source == "" {
		abs, absErr := filepath.Abs(rootPath)
		if absErr == nil {
			source = abs
		} else {
			source = rootPath
		}
	}
	if source == "" {
		source = "fallback:" + rootPath
	}
	hash := sha256.Sum256([]byte(source))
	return "ws-" + hex.EncodeToString(hash[:8])
}

func buildTree(rootPath, prefix string, maxDepth int, includeFiles bool) string {
	if maxDepth <= 0 {
		return ""
	}

	entries, err := os.ReadDir(rootPath)
	if err != nil {
		return fmt.Sprintf("%s[error: %v]\n", prefix, err)
	}

	var dirs, files []os.DirEntry
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "__pycache__" {
			continue
		}
		if e.IsDir() {
			dirs = append(dirs, e)
		} else if includeFiles {
			files = append(files, e)
		}
	}

	var sb strings.Builder
	all := append(dirs, files...)
	for i, e := range all {
		connector := "├── "
		childPrefix := prefix + "│   "
		if i == len(all)-1 {
			connector = "└── "
			childPrefix = prefix + "    "
		}

		sb.WriteString(prefix + connector + e.Name() + "\n")

		if e.IsDir() {
			child := buildTree(filepath.Join(rootPath, e.Name()), childPrefix, maxDepth-1, includeFiles)
			sb.WriteString(child)
		}
	}

	return sb.String()
}
