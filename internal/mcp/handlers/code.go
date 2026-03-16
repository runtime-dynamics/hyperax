package handlers

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"

	superctx "github.com/hyperax/hyperax/internal/context"
	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/search"
	"github.com/hyperax/hyperax/internal/storage"
	"github.com/hyperax/hyperax/internal/workspace"
	"github.com/hyperax/hyperax/pkg/types"
)

// actionClearanceCode maps each code action to its minimum ABAC clearance.
var actionClearanceCode = map[string]int{
	"get_file":       0, // was get_file_content
	"search":         0, // was search_code
	"outline":        0, // was get_code_outline
	"replace":        1, // was replace_lines
	"dependencies":   0, // was get_dependencies
	"search_imports": 0, // was search_imports
	"generate_context": 0, // was generate_context
	"detect_context":   0, // was detect_context_files
}

// MemoryResolver queries memories relevant to a file path and returns a
// brief context summary. This abstraction decouples the CodeHandler from
// the concrete MemoryStore implementation.
type MemoryResolver func(ctx context.Context, workspaceID, filePath string) ([]string, error)

// CodeHandler implements the consolidated "code" MCP tool.
type CodeHandler struct {
	store          *storage.Store
	db             *sql.DB
	memoryResolver MemoryResolver
	bus            *nervous.EventBus
	logger         *slog.Logger
	ctxGen         *superctx.SuperContextGenerator
}

// NewCodeHandler creates a CodeHandler. The optional db parameter enables
// the search_imports action. bus and logger are used by the context generation
// actions (generate_context, detect_context).
func NewCodeHandler(store *storage.Store, db *sql.DB) *CodeHandler {
	return &CodeHandler{store: store, db: db}
}

// SetContextDeps wires the dependencies needed for generate_context and
// detect_context actions. Call after construction if context tools are desired.
func (h *CodeHandler) SetContextDeps(bus *nervous.EventBus, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	h.bus = bus
	h.logger = logger
	h.ctxGen = superctx.NewSuperContextGenerator(h.store, bus, logger)
}

// SetMemoryResolver configures memory-first resolution for file content.
func (h *CodeHandler) SetMemoryResolver(fn MemoryResolver) {
	h.memoryResolver = fn
}

// RegisterTools registers the consolidated code tool with the MCP registry.
func (h *CodeHandler) RegisterTools(registry *mcp.ToolRegistry) {
	registry.Register(
		"code",
		"Code intelligence: read files, search code, get outlines, replace lines, list dependencies, "+
			"search imports, generate context, detect context files. "+
			"Actions: get_file | search | outline | replace | dependencies | search_imports | generate_context | detect_context",
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"action":         {"type": "string", "enum": ["get_file", "search", "outline", "replace", "dependencies", "search_imports", "generate_context", "detect_context"], "description": "Action to perform"},
				"workspace_name": {"type": "string", "description": "Workspace name"},
				"workspace_id":   {"type": "string", "description": "Workspace name/ID (generate_context, detect_context)"},
				"path":           {"type": "string", "description": "Relative path within workspace"},
				"offset":         {"type": "integer", "description": "Start line, 1-based (get_file, default 1)"},
				"limit":          {"type": "integer", "description": "Max lines/results (get_file default 500, search default 50, -1 for all)"},
				"query":          {"type": "string", "description": "Search query (search action)"},
				"kind":           {"type": "string", "description": "Symbol kind filter (search action)"},
				"start_line":     {"type": "integer", "description": "First line to replace (replace action)"},
				"end_line":       {"type": "integer", "description": "Last line to replace (replace action)"},
				"new_content":    {"type": "string", "description": "Replacement text (replace action)"},
				"module_name":    {"type": "string", "description": "Module/package name (search_imports action)"},
				"format":         {"type": "string", "description": "Output format: claude, gemini, codex (generate_context)"},
				"write_file":     {"type": "boolean", "description": "Write to disk (generate_context)"}
			},
			"required": ["action"]
		}`),
		h.dispatch,
	)
}

// dispatch routes the consolidated "code" tool to the correct handler method.
func (h *CodeHandler) dispatch(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var envelope struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return nil, fmt.Errorf("handlers.CodeHandler.dispatch: %w", err)
	}

	if err := checkActionClearance(ctx, envelope.Action, actionClearanceCode); err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	switch envelope.Action {
	case "get_file":
		return h.getFileContent(ctx, params)
	case "search":
		return h.searchCode(ctx, params)
	case "outline":
		return h.getCodeOutline(ctx, params)
	case "replace":
		return h.replaceLines(ctx, params)
	case "dependencies":
		return h.getDependencies(ctx, params)
	case "search_imports":
		return h.searchImports(ctx, params)
	case "generate_context":
		return h.generateContext(ctx, params)
	case "detect_context":
		return h.detectContextFiles(ctx, params)
	default:
		return types.NewErrorResult(fmt.Sprintf("unknown code action %q", envelope.Action)), nil
	}
}

// resolveWorkspacePath looks up the workspace by name and validates the
// relative path against the workspace sandbox to prevent directory traversal.
func (h *CodeHandler) resolveWorkspacePath(ctx context.Context, workspaceName, relPath string) (absPath string, ws *types.WorkspaceInfo, errResult *types.ToolResult) {
	wsInfo, err := h.store.Workspaces.GetWorkspace(ctx, workspaceName)
	if err != nil {
		return "", nil, types.NewErrorResult(fmt.Sprintf("workspace %q not found", workspaceName))
	}

	validated, err := workspace.ValidatePath(wsInfo.RootPath, relPath)
	if err != nil {
		return "", nil, types.NewErrorResult(err.Error())
	}

	return validated, wsInfo, nil
}

// ---------- get_file ----------

func (h *CodeHandler) getFileContent(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Path          string `json:"path"`
		Offset        int    `json:"offset"`
		Limit         int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.CodeHandler.getFileContent: %w", err)
	}
	if args.Offset <= 0 {
		args.Offset = 1
	}
	unlimited := args.Limit == -1
	if args.Limit <= 0 && !unlimited {
		args.Limit = 500
	}

	absPath, _, errResult := h.resolveWorkspacePath(ctx, args.WorkspaceName, args.Path)
	if errResult != nil {
		return errResult, nil
	}

	f, err := os.Open(absPath)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("open file: %v", err)), nil
	}
	defer func() { _ = f.Close() }()

	var sb strings.Builder
	scanner := bufio.NewScanner(f)
	lineNum := 0
	emitted := 0
	truncated := false

	for scanner.Scan() {
		lineNum++
		if lineNum < args.Offset {
			continue
		}
		if !unlimited && emitted >= args.Limit {
			truncated = true
			continue
		}
		fmt.Fprintf(&sb, "%4d| %s\n", lineNum, scanner.Text())
		emitted++
	}
	if err := scanner.Err(); err != nil {
		return types.NewErrorResult(fmt.Sprintf("read file: %v", err)), nil
	}

	if emitted == 0 {
		return types.NewToolResult(fmt.Sprintf("File %s: no lines in range (offset=%d, total=%d)", args.Path, args.Offset, lineNum)), nil
	}

	header := fmt.Sprintf("File: %s (lines %d-%d of %d)\n", args.Path, args.Offset, args.Offset+emitted-1, lineNum)

	var memoryCtx string
	if h.memoryResolver != nil && args.Offset == 1 {
		ws, wsErr := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
		if wsErr == nil {
			notes, recallErr := h.memoryResolver(ctx, ws.ID, args.Path)
			if recallErr == nil && len(notes) > 0 {
				var mb strings.Builder
				mb.WriteString("--- Memory Context ---\n")
				for _, note := range notes {
					mb.WriteString("  • ")
					mb.WriteString(note)
					mb.WriteString("\n")
				}
				mb.WriteString("----------------------\n")
				memoryCtx = mb.String()
			}
		}
	}

	if truncated {
		remaining := lineNum - (args.Offset + emitted - 1)
		nextOffset := args.Offset + emitted
		fmt.Fprintf(&sb, "\n--- Showing %d of %d lines (%d remaining) ---\n", emitted, lineNum, remaining)
		fmt.Fprintf(&sb, "To continue reading: code(action=\"get_file\", path, offset=%d)\n", nextOffset)
		fmt.Fprintf(&sb, "To read the entire file: code(action=\"get_file\", path, limit=-1)\n")
		fmt.Fprintf(&sb, "For file structure: code(action=\"outline\", path)\n")
	}

	return types.NewToolResult(header + memoryCtx + sb.String()), nil
}

// ---------- search ----------

func (h *CodeHandler) searchCode(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Query         string `json:"query"`
		Kind          string `json:"kind"`
		Limit         int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.CodeHandler.searchCode: %w", err)
	}
	if args.Limit <= 0 {
		args.Limit = 50
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	if h.store.Search == nil {
		return types.NewErrorResult("search repo not available"), nil
	}

	var sb strings.Builder
	hasResults := false

	symbols, err := h.store.Search.SearchSymbols(ctx, []string{ws.Name}, args.Query, args.Kind, args.Limit)
	if err != nil {
		return nil, fmt.Errorf("handlers.CodeHandler.searchCode: %w", err)
	}

	if len(symbols) > 0 {
		hasResults = true
		fmt.Fprintf(&sb, "Found %d symbol(s) matching %q:\n", len(symbols), args.Query)
		for _, sym := range symbols {
			sig := sym.Signature
			if sig == "" {
				sig = sym.Name
			}
			fmt.Fprintf(&sb, "  [%s] %s  (lines %d-%d)\n", sym.Kind, sig, sym.StartLine, sym.EndLine)
		}
	}

	codeChunks, contentErr := h.store.Search.SearchCodeContent(ctx, []string{ws.Name}, args.Query, args.Limit)
	if contentErr == nil && len(codeChunks) > 0 {
		hasResults = true
		if len(symbols) > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "Found %d code content match(es) for %q:\n", len(codeChunks), args.Query)
		for _, c := range codeChunks {
			snippet := search.ExtractSnippet(c.Content, args.Query, 150)
			fmt.Fprintf(&sb, "  [code_content] %s | %s\n    %s\n",
				c.FilePath, c.SectionHeader, snippet)
		}
	}

	if !hasResults {
		return types.NewToolResult(fmt.Sprintf("No symbols or code content matching %q found.", args.Query)), nil
	}

	return types.NewToolResult(sb.String()), nil
}

// ---------- outline ----------

func (h *CodeHandler) getCodeOutline(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Path          string `json:"path"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.CodeHandler.getCodeOutline: %w", err)
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	if h.store.Symbols == nil {
		return types.NewErrorResult("symbol repo not available"), nil
	}

	symbols, err := h.store.Symbols.GetFileSymbols(ctx, ws.ID, args.Path)
	if err != nil {
		return nil, fmt.Errorf("handlers.CodeHandler.getCodeOutline: %w", err)
	}

	if len(symbols) == 0 {
		return types.NewToolResult(fmt.Sprintf("No symbols indexed for %s.", args.Path)), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Outline: %s\n", args.Path)
	for _, sym := range symbols {
		indent := "  "
		if sym.Kind == "method" || sym.Kind == "field" {
			indent = "    "
		}
		sig := sym.Signature
		if sig == "" {
			sig = sym.Name
		}
		fmt.Fprintf(&sb, "%s%s [%s] L%d-%d\n", indent, sig, sym.Kind, sym.StartLine, sym.EndLine)
	}
	return types.NewToolResult(sb.String()), nil
}

// ---------- replace ----------

func (h *CodeHandler) replaceLines(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Path          string `json:"path"`
		StartLine     int    `json:"start_line"`
		EndLine       int    `json:"end_line"`
		NewContent    string `json:"new_content"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.CodeHandler.replaceLines: %w", err)
	}

	if args.StartLine < 1 || args.EndLine < args.StartLine {
		return types.NewErrorResult(fmt.Sprintf("invalid line range: %d-%d", args.StartLine, args.EndLine)), nil
	}

	absPath, _, errResult := h.resolveWorkspacePath(ctx, args.WorkspaceName, args.Path)
	if errResult != nil {
		return errResult, nil
	}

	lines, err := readFileLines(absPath)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("read file: %v", err)), nil
	}

	if args.StartLine > len(lines) {
		return types.NewErrorResult(fmt.Sprintf("start_line %d exceeds file length %d", args.StartLine, len(lines))), nil
	}
	if args.EndLine > len(lines) {
		args.EndLine = len(lines)
	}

	var newLines []string
	newLines = append(newLines, lines[:args.StartLine-1]...)

	replacementLines := strings.Split(args.NewContent, "\n")
	newLines = append(newLines, replacementLines...)
	newLines = append(newLines, lines[args.EndLine:]...)

	if err := writeFileLines(absPath, newLines); err != nil {
		return types.NewErrorResult(fmt.Sprintf("write file: %v", err)), nil
	}

	contextAbove := 4
	contextBelow := 4
	ctxStart := args.StartLine - contextAbove
	if ctxStart < 1 {
		ctxStart = 1
	}
	replacementEnd := args.StartLine + len(replacementLines) - 1
	ctxEnd := replacementEnd + contextBelow
	if ctxEnd > len(newLines) {
		ctxEnd = len(newLines)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Replaced lines %d-%d in %s:\n", args.StartLine, args.EndLine, args.Path)
	for i := ctxStart; i <= ctxEnd; i++ {
		marker := " "
		if i >= args.StartLine && i <= replacementEnd {
			marker = ">"
		}
		fmt.Fprintf(&sb, "%s%4d| %s\n", marker, i, newLines[i-1])
	}
	return types.NewToolResult(sb.String()), nil
}

// ---------- dependencies ----------

var goImportSingle = regexp.MustCompile(`^\s*import\s+"([^"]+)"`)
var goImportBlockStart = regexp.MustCompile(`^\s*import\s*\(`)
var goImportEntry = regexp.MustCompile(`^\s*(?:(\w+)\s+)?"([^"]+)"`)

func (h *CodeHandler) getDependencies(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Path          string `json:"path"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.CodeHandler.getDependencies: %w", err)
	}

	absPath, _, errResult := h.resolveWorkspacePath(ctx, args.WorkspaceName, args.Path)
	if errResult != nil {
		return errResult, nil
	}

	imports, err := parseGoImports(absPath)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("parse imports: %v", err)), nil
	}

	if len(imports) == 0 {
		return types.NewToolResult(fmt.Sprintf("No imports found in %s.", args.Path)), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Imports in %s:\n", args.Path)
	for _, imp := range imports {
		sb.WriteString("  " + imp + "\n")
	}
	return types.NewToolResult(sb.String()), nil
}

func parseGoImports(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer func() { _ = f.Close() }()

	var imports []string
	scanner := bufio.NewScanner(f)
	inBlock := false

	for scanner.Scan() {
		line := scanner.Text()

		if inBlock {
			trimmed := strings.TrimSpace(line)
			if trimmed == ")" {
				inBlock = false
				continue
			}
			if m := goImportEntry.FindStringSubmatch(line); m != nil {
				alias := m[1]
				pkg := m[2]
				if alias != "" {
					imports = append(imports, alias+" "+pkg)
				} else {
					imports = append(imports, pkg)
				}
			}
			continue
		}

		if goImportBlockStart.MatchString(line) {
			inBlock = true
			continue
		}

		if m := goImportSingle.FindStringSubmatch(line); m != nil {
			imports = append(imports, m[1])
		}
	}
	return imports, scanner.Err()
}

// ---------- search_imports ----------

func (h *CodeHandler) searchImports(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		ModuleName    string `json:"module_name"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.CodeHandler.searchImports: %w", err)
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	if h.db == nil {
		return types.NewErrorResult("search_imports: database connection not available"), nil
	}

	rows, err := h.db.QueryContext(ctx,
		`SELECT fh.file_path, i.module_name, COALESCE(i.alias, '')
		 FROM imports i
		 JOIN file_hashes fh ON i.file_id = fh.file_id
		 WHERE fh.workspace_id = ? AND i.module_name LIKE ?
		 ORDER BY fh.file_path`,
		ws.ID, "%"+args.ModuleName+"%",
	)
	if err != nil {
		return nil, fmt.Errorf("handlers.CodeHandler.searchImports: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type importHit struct {
		FilePath   string
		ModuleName string
		Alias      string
	}

	var hits []importHit
	for rows.Next() {
		var hit importHit
		if err := rows.Scan(&hit.FilePath, &hit.ModuleName, &hit.Alias); err != nil {
			return nil, fmt.Errorf("handlers.CodeHandler.searchImports: scan: %w", err)
		}
		hits = append(hits, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("handlers.CodeHandler.searchImports: iterate: %w", err)
	}

	if len(hits) == 0 {
		return types.NewToolResult(fmt.Sprintf("No files importing %q found.", args.ModuleName)), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Files importing %q:\n", args.ModuleName)
	for _, hit := range hits {
		if hit.Alias != "" {
			fmt.Fprintf(&sb, "  %s  (as %s, module: %s)\n", hit.FilePath, hit.Alias, hit.ModuleName)
		} else {
			fmt.Fprintf(&sb, "  %s  (module: %s)\n", hit.FilePath, hit.ModuleName)
		}
	}
	return types.NewToolResult(sb.String()), nil
}

// ---------- generate_context ----------

func (h *CodeHandler) generateContext(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceID string `json:"workspace_id"`
		Format      string `json:"format"`
		WriteFile   bool   `json:"write_file"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.CodeHandler.generateContext: %w", err)
	}
	if args.WorkspaceID == "" {
		return types.NewErrorResult("workspace_id is required"), nil
	}
	if args.Format == "" {
		args.Format = "claude"
	}

	if h.ctxGen == nil {
		return types.NewErrorResult("context generation not available (missing bus/logger dependencies)"), nil
	}

	if h.store.Workspaces == nil {
		return types.NewErrorResult("workspace repo not available"), nil
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceID)), nil
	}

	content, err := h.ctxGen.Generate(ctx, ws.Name, ws.RootPath)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("generate context: %v", err)), nil
	}

	if args.WriteFile {
		if writeErr := h.ctxGen.WriteContextFile(ctx, ws.Name, ws.RootPath); writeErr != nil {
			return types.NewErrorResult(fmt.Sprintf("write context file: %v", writeErr)), nil
		}
		h.logger.Info("wrote context file via MCP tool", "workspace", ws.Name, "format", args.Format)
	}

	type generateResult struct {
		WorkspaceID string `json:"workspace_id"`
		Format      string `json:"format"`
		Written     bool   `json:"written"`
		Content     string `json:"content"`
	}

	return types.NewToolResult(generateResult{
		WorkspaceID: ws.Name,
		Format:      args.Format,
		Written:     args.WriteFile,
		Content:     content,
	}), nil
}

// ---------- detect_context ----------

func (h *CodeHandler) detectContextFiles(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.CodeHandler.detectContextFiles: %w", err)
	}
	if args.WorkspaceID == "" {
		return types.NewErrorResult("workspace_id is required"), nil
	}

	if h.store.Workspaces == nil {
		return types.NewErrorResult("workspace repo not available"), nil
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceID)), nil
	}

	detected, err := superctx.DetectContextFiles(ws.RootPath)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("detect context files: %v", err)), nil
	}

	if len(detected) == 0 {
		return types.NewToolResult("No agent context files detected in workspace."), nil
	}

	type detectedFile struct {
		Type string `json:"type"`
		Path string `json:"path"`
	}

	var results []detectedFile
	for ct, path := range detected {
		results = append(results, detectedFile{
			Type: string(ct),
			Path: path,
		})
	}

	return types.NewToolResult(results), nil
}

// ---------- file helpers ----------

func readFileLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func writeFileLines(path string, lines []string) error {
	tmp := path + ".hax.tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}

	w := bufio.NewWriter(f)
	for _, line := range lines {
		if _, err := w.WriteString(line + "\n"); err != nil {
			f.Close()
			os.Remove(tmp)
			return fmt.Errorf("write line: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("flush: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close: %w", err)
	}
	return os.Rename(tmp, path)
}
