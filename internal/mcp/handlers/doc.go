package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/hyperax/hyperax/internal/index"
	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/internal/storage"
	"github.com/hyperax/hyperax/internal/workspace"
	"github.com/hyperax/hyperax/pkg/types"
)

// defaultPreviewLines is the number of lines returned by get_content when
// no explicit limit is provided.
const defaultPreviewLines = 25

// allowedDocTags enumerates the valid tag values for document tagging.
var allowedDocTags = map[string]bool{
	"architecture": true,
	"standards":    true,
}

// actionClearanceDoc maps each doc action to its minimum ABAC clearance.
var actionClearanceDoc = map[string]int{
	// Docs core
	"search":    0, // was search_docs
	"list":      0, // was list_docs
	"get_content": 0, // was get_doc_content
	"get_toc":   0, // was get_doc_toc
	"get_section": 0, // was get_doc_section
	"create":    1, // was create_doc
	"update":    1, // was update_doc_content
	"delete":    1, // was delete_doc
	"move":      1, // was move_doc
	"append":    1, // was append_doc
	"validate":  0, // was validate_doc
	"fix":       1, // was fix_doc
	"get_standard": 0, // was get_doc_standard

	// External docs
	"add_source":       2, // was add_external_doc_source
	"remove_source":    2, // was remove_external_doc_source
	"list_sources":     0, // was list_external_doc_sources
	"tag":              1, // was tag_document
	"untag":            1, // was untag_document
	"list_tags":        0, // was list_doc_tags
	"workspace_status": 0, // was get_workspace_doc_status

	// Standards
	"get_standards":       0, // was get_standards
	"standards_toc":       0, // was get_standards_toc
	"standard_section":    0, // was get_standard_section
	"architecture":        0, // was get_architecture
	"architecture_toc":    0, // was get_architecture_toc
	"architecture_section": 0, // was get_architecture_section

	// Specs
	"create_spec":       1, // was create_spec
	"list_specs":        0, // was list_specs
	"get_spec":          0, // was get_spec
	"amend_spec":        1, // was amend_spec
	"update_spec_status": 1, // was update_spec_status
}

// DocHandler implements the consolidated "doc" MCP tool, absorbing docs,
// external_docs, standards, and specs handler functionality.
type DocHandler struct {
	store   *storage.Store
	indexer *index.Indexer
}

// NewDocHandler creates a DocHandler.
func NewDocHandler(store *storage.Store, indexer *index.Indexer) *DocHandler {
	return &DocHandler{store: store, indexer: indexer}
}

// RegisterTools registers the consolidated "doc" tool.
func (h *DocHandler) RegisterTools(registry *mcp.ToolRegistry) {
	registry.Register(
		"doc",
		"Documentation management: search, list, read, create, update, delete, move, append, validate, fix docs; "+
			"external doc sources and tagging; standards and architecture reading; specification management. "+
			"Actions: search | list | get_content | get_toc | get_section | create | update | delete | move | "+
			"append | validate | fix | get_standard | add_source | remove_source | list_sources | tag | untag | "+
			"list_tags | workspace_status | get_standards | standards_toc | standard_section | architecture | "+
			"architecture_toc | architecture_section | create_spec | list_specs | get_spec | amend_spec | update_spec_status",
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"action":          {"type": "string", "enum": [
					"search", "list", "get_content", "get_toc", "get_section",
					"create", "update", "delete", "move", "append", "validate", "fix", "get_standard",
					"add_source", "remove_source", "list_sources", "tag", "untag", "list_tags", "workspace_status",
					"get_standards", "standards_toc", "standard_section",
					"architecture", "architecture_toc", "architecture_section",
					"create_spec", "list_specs", "get_spec", "amend_spec", "update_spec_status"
				], "description": "Action to perform"},
				"workspace_name":  {"type": "string", "description": "Workspace name"},
				"path":            {"type": "string", "description": "Relative path within workspace"},
				"query":           {"type": "string", "description": "Search query (search action)"},
				"limit":           {"type": "integer", "description": "Maximum results or lines to return"},
				"section":         {"type": "string", "description": "Heading text (get_section, standard_section, architecture_section)"},
				"content":         {"type": "string", "description": "Document content (create, update, append)"},
				"old_path":        {"type": "string", "description": "Current relative path (move)"},
				"new_path":        {"type": "string", "description": "Destination relative path (move)"},
				"name":            {"type": "string", "description": "Display name (add_source)"},
				"source_id":       {"type": "string", "description": "External source ID (remove_source)"},
				"file_path":       {"type": "string", "description": "File path for tagging (tag)"},
				"tag":             {"type": "string", "description": "Tag name: architecture or standards (tag, untag)", "enum": ["architecture", "standards"]},
				"title":           {"type": "string", "description": "Title (create_spec, amend_spec)"},
				"description":     {"type": "string", "description": "Description (create_spec, amend_spec)"},
				"created_by":      {"type": "string", "description": "Author (create_spec)"},
				"milestones":      {"type": "array", "description": "Milestones (create_spec)", "items": {"type": "object"}},
				"spec_id":         {"type": "string", "description": "Spec ID (get_spec, amend_spec, update_spec_status)"},
				"spec_number":     {"type": "integer", "description": "Spec number (get_spec)"},
				"status":          {"type": "string", "description": "New status (update_spec_status)"},
				"author":          {"type": "string", "description": "Amendment author (amend_spec)"}
			},
			"required": ["action"]
		}`),
		h.dispatch,
	)
}

// dispatch routes the consolidated "doc" tool to the correct handler method.
func (h *DocHandler) dispatch(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var envelope struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.dispatch: %w", err)
	}

	if err := checkActionClearance(ctx, envelope.Action, actionClearanceDoc); err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	switch envelope.Action {
	// Docs core
	case "search":
		return h.searchDocs(ctx, params)
	case "list":
		return h.listDocs(ctx, params)
	case "get_content":
		return h.getDocContent(ctx, params)
	case "get_toc":
		return h.getDocToc(ctx, params)
	case "get_section":
		return h.getDocSection(ctx, params)
	case "create":
		return h.createDoc(ctx, params)
	case "update":
		return h.updateDocContent(ctx, params)
	case "delete":
		return h.deleteDoc(ctx, params)
	case "move":
		return h.moveDoc(ctx, params)
	case "append":
		return h.appendDoc(ctx, params)
	case "validate":
		return h.validateDoc(ctx, params)
	case "fix":
		return h.fixDoc(ctx, params)
	case "get_standard":
		return h.getDocStandard(ctx, params)

	// External docs
	case "add_source":
		return h.addExternalDocSource(ctx, params)
	case "remove_source":
		return h.removeExternalDocSource(ctx, params)
	case "list_sources":
		return h.listExternalDocSources(ctx, params)
	case "tag":
		return h.tagDocument(ctx, params)
	case "untag":
		return h.untagDocument(ctx, params)
	case "list_tags":
		return h.listDocTags(ctx, params)
	case "workspace_status":
		return h.getWorkspaceDocStatus(ctx, params)

	// Standards
	case "get_standards":
		return h.getTaggedDocContent(ctx, params, "standards")
	case "standards_toc":
		return h.getTaggedDocToc(ctx, params, "standards")
	case "standard_section":
		return h.getTaggedDocSection(ctx, params, "standards")
	case "architecture":
		return h.getTaggedDocContent(ctx, params, "architecture")
	case "architecture_toc":
		return h.getTaggedDocToc(ctx, params, "architecture")
	case "architecture_section":
		return h.getTaggedDocSection(ctx, params, "architecture")

	// Specs
	case "create_spec":
		return h.createSpec(ctx, params)
	case "list_specs":
		return h.listSpecs(ctx, params)
	case "get_spec":
		return h.getSpec(ctx, params)
	case "amend_spec":
		return h.amendSpec(ctx, params)
	case "update_spec_status":
		return h.updateSpecStatus(ctx, params)

	default:
		return types.NewErrorResult(fmt.Sprintf("unknown doc action %q", envelope.Action)), nil
	}
}

// ─── Docs core methods ─────────────────────────────────────────────────────

func (h *DocHandler) searchDocs(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Query         string `json:"query"`
		Limit         int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.searchDocs: %w", err)
	}
	if args.Limit <= 0 {
		args.Limit = 10
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	if h.store.Search == nil {
		return types.NewErrorResult("search repo not available"), nil
	}

	chunks, err := h.store.Search.SearchDocs(ctx, []string{ws.Name}, args.Query, args.Limit)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("search failed: %v", err)), nil
	}

	if len(chunks) == 0 {
		return types.NewToolResult("No matching documentation found."), nil
	}

	type result struct {
		FilePath      string `json:"file_path"`
		SectionHeader string `json:"section_header"`
		Snippet       string `json:"snippet"`
	}

	results := make([]result, 0, len(chunks))
	for _, c := range chunks {
		snippet := c.Content
		if len(snippet) > 300 {
			snippet = snippet[:300] + "..."
		}
		results = append(results, result{
			FilePath:      c.FilePath,
			SectionHeader: c.SectionHeader,
			Snippet:       snippet,
		})
	}

	return types.NewToolResult(results), nil
}

func (h *DocHandler) listDocs(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.listDocs: %w", err)
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	type docFile struct {
		Name     string `json:"name"`
		Path     string `json:"path"`
		Size     int64  `json:"size"`
		Source   string `json:"source"`
		Readonly bool   `json:"readonly"`
	}

	var docs []docFile

	docsDir := filepath.Join(ws.RootPath, "docs")
	entries, err := os.ReadDir(docsDir)
	if err != nil && !os.IsNotExist(err) {
		return types.NewErrorResult(fmt.Sprintf("read docs dir: %v", err)), nil
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		docs = append(docs, docFile{
			Name:     e.Name(),
			Path:     filepath.Join("docs", e.Name()),
			Size:     info.Size(),
			Source:   "internal",
			Readonly: false,
		})
	}

	if h.store.ExternalDocs != nil {
		sources, extErr := h.store.ExternalDocs.ListExternalDocSources(ctx, ws.ID)
		if extErr == nil {
			for _, src := range sources {
				if walkDirErr := filepath.WalkDir(src.Path, func(path string, d os.DirEntry, walkErr error) error {
					if walkErr != nil || d.IsDir() {
						return nil
					}
					if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
						return nil
					}
					relPath, relErr := filepath.Rel(src.Path, path)
					if relErr != nil {
						return nil
					}
					info, infoErr := d.Info()
					if infoErr != nil {
						return nil
					}
					docs = append(docs, docFile{
						Name:     d.Name(),
						Path:     fmt.Sprintf("@ext/%s/%s", src.Name, relPath),
						Size:     info.Size(),
						Source:   src.Name,
						Readonly: true,
					})
					return nil
				}); walkDirErr != nil {
					slog.Warn("failed to walk external doc source", "path", src.Path, "error", walkDirErr)
				}
			}
		}
	}

	if len(docs) == 0 {
		return types.NewToolResult("No markdown files found."), nil
	}

	return types.NewToolResult(docs), nil
}

func (h *DocHandler) getDocContent(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Path          string `json:"path"`
		Limit         *int   `json:"limit"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.getDocContent: %w", err)
	}

	isPreview := false
	effectiveLimit := 0
	if args.Limit == nil {
		effectiveLimit = defaultPreviewLines
		isPreview = true
	} else if *args.Limit == -1 {
		effectiveLimit = 0
	} else if *args.Limit > 0 {
		effectiveLimit = *args.Limit
	} else {
		effectiveLimit = defaultPreviewLines
		isPreview = true
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	var fullPath string
	if strings.HasPrefix(args.Path, "@ext/") {
		resolved, resolveErr := h.resolveExternalPath(ctx, ws.ID, args.Path)
		if resolveErr != nil {
			return types.NewErrorResult(resolveErr.Error()), nil
		}
		fullPath = resolved
	} else {
		var pathErr error
		fullPath, pathErr = docValidatePath(ws.RootPath, args.Path)
		if pathErr != nil {
			return types.NewErrorResult(pathErr.Error()), nil
		}
	}

	f, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return types.NewErrorResult(fmt.Sprintf("file not found: %s", args.Path)), nil
		}
		return types.NewErrorResult(fmt.Sprintf("open file: %v", err)), nil
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			slog.Warn("failed to close file", "error", cerr)
		}
	}()

	var sb strings.Builder
	scanner := bufio.NewScanner(f)
	lineCount := 0
	totalLines := 0
	truncated := false
	for scanner.Scan() {
		totalLines++
		if effectiveLimit > 0 && totalLines > effectiveLimit {
			truncated = true
			continue
		}
		lineCount++
		sb.WriteString(scanner.Text())
		sb.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return types.NewErrorResult(fmt.Sprintf("read file: %v", err)), nil
	}

	if truncated && isPreview {
		fmt.Fprintf(&sb, "\n--- Preview: showing %d of %d lines ---\n", lineCount, totalLines)
		fmt.Fprintf(&sb, "To read the full document, use: get_content(path, limit=-1)\n")
		fmt.Fprintf(&sb, "For targeted reading, prefer:\n")
		fmt.Fprintf(&sb, "  - get_toc(path) to see all sections\n")
		fmt.Fprintf(&sb, "  - get_section(path, section) to read a specific section\n")
	} else if truncated {
		fmt.Fprintf(&sb, "\n... truncated at %d of %d lines", effectiveLimit, totalLines)
	}

	return types.NewToolResult(sb.String()), nil
}

func (h *DocHandler) getDocToc(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Path          string `json:"path"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.getDocToc: %w", err)
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	fullPath, err := docValidatePath(ws.RootPath, args.Path)
	if err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	headings, err := docExtractHeadings(fullPath)
	if err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	if len(headings) == 0 {
		return types.NewToolResult("No headings found in document."), nil
	}

	return types.NewToolResult(headings), nil
}

func (h *DocHandler) getDocSection(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Path          string `json:"path"`
		Section       string `json:"section"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.getDocSection: %w", err)
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	fullPath, err := docValidatePath(ws.RootPath, args.Path)
	if err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	content, err := docExtractSection(fullPath, args.Section)
	if err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	return types.NewToolResult(content), nil
}

func (h *DocHandler) createDoc(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Path          string `json:"path"`
		Content       string `json:"content"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.createDoc: %w", err)
	}

	if strings.HasPrefix(args.Path, "@ext/") {
		return types.NewErrorResult("external documents are read-only"), nil
	}

	if !strings.HasSuffix(strings.ToLower(args.Path), ".md") {
		return types.NewErrorResult("path must end with .md"), nil
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	fullPath, err := docValidatePath(ws.RootPath, args.Path)
	if err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return types.NewErrorResult(fmt.Sprintf("create directory: %v", err)), nil
	}

	if _, err := os.Stat(fullPath); err == nil {
		return types.NewErrorResult(fmt.Sprintf("file already exists: %s", args.Path)), nil
	}

	if err := os.WriteFile(fullPath, []byte(args.Content), 0o644); err != nil {
		return types.NewErrorResult(fmt.Sprintf("write file: %v", err)), nil
	}

	return types.NewToolResult(fmt.Sprintf("Created %s", args.Path)), nil
}

func (h *DocHandler) updateDocContent(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Path          string `json:"path"`
		Content       string `json:"content"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.updateDocContent: %w", err)
	}

	if strings.HasPrefix(args.Path, "@ext/") {
		return types.NewErrorResult("external documents are read-only"), nil
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	fullPath, err := docValidatePath(ws.RootPath, args.Path)
	if err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return types.NewErrorResult(fmt.Sprintf("file not found: %s", args.Path)), nil
	}

	if err := os.WriteFile(fullPath, []byte(args.Content), 0o644); err != nil {
		return types.NewErrorResult(fmt.Sprintf("write file: %v", err)), nil
	}

	return types.NewToolResult(fmt.Sprintf("Updated %s", args.Path)), nil
}

func (h *DocHandler) deleteDoc(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Path          string `json:"path"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.deleteDoc: %w", err)
	}

	if strings.HasPrefix(args.Path, "@ext/") {
		return types.NewErrorResult("external documents are read-only"), nil
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	fullPath, err := docValidatePath(ws.RootPath, args.Path)
	if err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return types.NewErrorResult(fmt.Sprintf("file not found: %s", args.Path)), nil
	}

	if err := os.Remove(fullPath); err != nil {
		return types.NewErrorResult(fmt.Sprintf("delete file: %v", err)), nil
	}

	return types.NewToolResult(fmt.Sprintf("Deleted %s", args.Path)), nil
}

func (h *DocHandler) moveDoc(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		OldPath       string `json:"old_path"`
		NewPath       string `json:"new_path"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.moveDoc: %w", err)
	}

	if strings.HasPrefix(args.OldPath, "@ext/") || strings.HasPrefix(args.NewPath, "@ext/") {
		return types.NewErrorResult("external documents are read-only"), nil
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	oldFull, err := docValidatePath(ws.RootPath, args.OldPath)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("old_path: %v", err)), nil
	}

	newFull, err := docValidatePath(ws.RootPath, args.NewPath)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("new_path: %v", err)), nil
	}

	if _, err := os.Stat(oldFull); os.IsNotExist(err) {
		return types.NewErrorResult(fmt.Sprintf("source file not found: %s", args.OldPath)), nil
	}

	dir := filepath.Dir(newFull)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return types.NewErrorResult(fmt.Sprintf("create directory: %v", err)), nil
	}

	if err := os.Rename(oldFull, newFull); err != nil {
		return types.NewErrorResult(fmt.Sprintf("rename: %v", err)), nil
	}

	return types.NewToolResult(fmt.Sprintf("Moved %s -> %s", args.OldPath, args.NewPath)), nil
}

func (h *DocHandler) appendDoc(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Path          string `json:"path"`
		Content       string `json:"content"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.appendDoc: %w", err)
	}

	if strings.HasPrefix(args.Path, "@ext/") {
		return types.NewErrorResult("external documents are read-only"), nil
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	fullPath, err := docValidatePath(ws.RootPath, args.Path)
	if err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	existing, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return types.NewErrorResult(fmt.Sprintf("file not found: %s", args.Path)), nil
		}
		return types.NewErrorResult(fmt.Sprintf("read file: %v", err)), nil
	}

	combined := string(existing) + args.Content
	if err := os.WriteFile(fullPath, []byte(combined), 0o644); err != nil {
		return types.NewErrorResult(fmt.Sprintf("write file: %v", err)), nil
	}

	return types.NewToolResult(fmt.Sprintf("Appended to %s", args.Path)), nil
}

func (h *DocHandler) validateDoc(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Path          string `json:"path"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.validateDoc: %w", err)
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	fullPath, err := docValidatePath(ws.RootPath, args.Path)
	if err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return types.NewErrorResult(fmt.Sprintf("file not found: %s", args.Path)), nil
		}
		return types.NewErrorResult(fmt.Sprintf("read file: %v", err)), nil
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	var issues []string

	if len(strings.TrimSpace(content)) == 0 {
		issues = append(issues, "File is empty")
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 0 && trimmed[0] == '#' {
			hashes := 0
			for _, ch := range trimmed {
				if ch == '#' {
					hashes++
				} else {
					break
				}
			}
			if hashes > 0 && hashes < len(trimmed) && trimmed[hashes] != ' ' {
				issues = append(issues, fmt.Sprintf("Line %d: heading missing space after #", i+1))
			}
			if hashes > 6 {
				issues = append(issues, fmt.Sprintf("Line %d: heading level exceeds 6", i+1))
			}
		}
	}

	for i, line := range lines {
		if strings.TrimRight(line, " \t") != line {
			issues = append(issues, fmt.Sprintf("Line %d: trailing whitespace", i+1))
			if len(issues) > 20 {
				issues = append(issues, "... (additional issues truncated)")
				break
			}
		}
	}

	if len(content) > 0 && content[len(content)-1] != '\n' {
		issues = append(issues, "Missing final newline")
	}

	for i, line := range lines {
		docCheckBrokenLinks(ws.RootPath, filepath.Dir(args.Path), line, i+1, &issues)
	}

	if len(issues) == 0 {
		return types.NewToolResult("No issues found."), nil
	}

	type validationResult struct {
		IssueCount int      `json:"issue_count"`
		Issues     []string `json:"issues"`
	}

	return types.NewToolResult(validationResult{
		IssueCount: len(issues),
		Issues:     issues,
	}), nil
}

func (h *DocHandler) fixDoc(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Path          string `json:"path"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.fixDoc: %w", err)
	}

	if strings.HasPrefix(args.Path, "@ext/") {
		return types.NewErrorResult("external documents are read-only"), nil
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	fullPath, err := docValidatePath(ws.RootPath, args.Path)
	if err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return types.NewErrorResult(fmt.Sprintf("file not found: %s", args.Path)), nil
		}
		return types.NewErrorResult(fmt.Sprintf("read file: %v", err)), nil
	}

	lines := strings.Split(string(data), "\n")
	var fixes []string

	for i, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		if trimmed != line {
			lines[i] = trimmed
			fixes = append(fixes, fmt.Sprintf("Line %d: removed trailing whitespace", i+1))
		}
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 0 && trimmed[0] == '#' {
			hashes := 0
			for _, ch := range trimmed {
				if ch == '#' {
					hashes++
				} else {
					break
				}
			}
			if hashes > 0 && hashes < len(trimmed) && trimmed[hashes] != ' ' {
				fixed := trimmed[:hashes] + " " + trimmed[hashes:]
				lines[i] = fixed
				fixes = append(fixes, fmt.Sprintf("Line %d: added space after heading #", i+1))
			}
		}
	}

	result := strings.Join(lines, "\n")
	if len(result) > 0 && result[len(result)-1] != '\n' {
		result += "\n"
		fixes = append(fixes, "Added final newline")
	}

	if len(fixes) == 0 {
		return types.NewToolResult("No fixes needed."), nil
	}

	if err := os.WriteFile(fullPath, []byte(result), 0o644); err != nil {
		return types.NewErrorResult(fmt.Sprintf("write file: %v", err)), nil
	}

	type fixResult struct {
		FixCount int      `json:"fix_count"`
		Fixes    []string `json:"fixes"`
	}

	return types.NewToolResult(fixResult{
		FixCount: len(fixes),
		Fixes:    fixes,
	}), nil
}

const docStandardRules = `# Documentation Standard

These rules MUST be followed when creating or updating markdown documents.

## Structure
- Every document MUST start with a level-1 heading (# Title).
- Do NOT manually add a Table of Contents — it is automatically inserted by the system when using write tools.
- Use heading levels sequentially: do not skip from # to ### without ##.
- Heading levels must not exceed 6 (######).

## Formatting
- Use ATX-style headings (# prefix) — not underline-style.
- A space MUST follow the # characters in headings.
- No trailing whitespace on any line.
- Files MUST end with a single newline character.
- Use fenced code blocks (triple backtick) for code — not indented blocks.
- Use blank lines before and after headings, lists, and code blocks.

## Content
- Keep documents focused on a single topic or concern.
- Use relative links for cross-references to other workspace docs (e.g. [Architecture](./Architecture.md)).
- External URLs must use full https:// links.
- Do not duplicate content across documents — link instead.

## Naming
- Use PascalCase for document filenames (e.g. Architecture.md, CodingGuidelines.md).
- Files must have the .md extension.
- Place workspace docs in the docs/ directory.
`

func (h *DocHandler) getDocStandard(_ context.Context, _ json.RawMessage) (*types.ToolResult, error) {
	return types.NewToolResult(docStandardRules), nil
}

// ─── External docs methods ─────────────────────────────────────────────────

func (h *DocHandler) addExternalDocSource(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Name          string `json:"name"`
		Path          string `json:"path"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.addExternalDocSource: %w", err)
	}
	if args.Name == "" {
		return types.NewErrorResult("name is required"), nil
	}
	if args.Path == "" {
		return types.NewErrorResult("path is required"), nil
	}

	if h.store.ExternalDocs == nil {
		return types.NewErrorResult("external docs repo not available"), nil
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	info, err := os.Stat(args.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return types.NewErrorResult(fmt.Sprintf("path does not exist: %s", args.Path)), nil
		}
		return types.NewErrorResult(fmt.Sprintf("stat path: %v", err)), nil
	}
	if !info.IsDir() {
		return types.NewErrorResult(fmt.Sprintf("path is not a directory: %s", args.Path)), nil
	}

	if !docContainsMarkdownFiles(args.Path) {
		return types.NewErrorResult(fmt.Sprintf("directory contains no .md files: %s", args.Path)), nil
	}

	absPath, err := filepath.Abs(args.Path)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("resolve absolute path: %v", err)), nil
	}

	src := &repo.ExternalDocSource{
		WorkspaceID: ws.ID,
		Name:        args.Name,
		Path:        absPath,
	}

	if err := h.store.ExternalDocs.AddExternalDocSource(ctx, src); err != nil {
		return types.NewErrorResult(fmt.Sprintf("add external doc source: %v", err)), nil
	}

	indexed, indexErr := h.indexExternalSource(ctx, ws.ID, args.Name, absPath)

	result := map[string]any{
		"id":      src.ID,
		"message": fmt.Sprintf("External doc source %q added.", args.Name),
	}
	if indexErr != nil {
		result["index_warning"] = fmt.Sprintf("indexing partially failed: %v", indexErr)
	}
	result["files_indexed"] = indexed

	return types.NewToolResult(result), nil
}

func (h *DocHandler) removeExternalDocSource(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		SourceID      string `json:"source_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.removeExternalDocSource: %w", err)
	}
	if args.SourceID == "" {
		return types.NewErrorResult("source_id is required"), nil
	}

	if h.store.ExternalDocs == nil {
		return types.NewErrorResult("external docs repo not available"), nil
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	src, err := h.store.ExternalDocs.GetExternalDocSource(ctx, args.SourceID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("external doc source not found: %v", err)), nil
	}

	cleanedUp := 0
	if h.store.Search != nil {
		prefix := fmt.Sprintf("@ext/%s/", src.Name)
		cleanedUp = h.cleanupIndexedChunks(ctx, ws.ID, src.Path, prefix)
	}

	if err := h.store.ExternalDocs.RemoveExternalDocSource(ctx, args.SourceID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("remove external doc source: %v", err)), nil
	}

	return types.NewToolResult(map[string]any{
		"message":        fmt.Sprintf("External doc source %q removed.", src.Name),
		"chunks_cleaned": cleanedUp,
	}), nil
}

func (h *DocHandler) listExternalDocSources(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.listExternalDocSources: %w", err)
	}

	if h.store.ExternalDocs == nil {
		return types.NewErrorResult("external docs repo not available"), nil
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	sources, err := h.store.ExternalDocs.ListExternalDocSources(ctx, ws.ID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("list external doc sources: %v", err)), nil
	}

	if len(sources) == 0 {
		return types.NewToolResult("No external documentation sources registered."), nil
	}

	type sourceEntry struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Path      string `json:"path"`
		CreatedAt string `json:"created_at"`
	}

	entries := make([]sourceEntry, 0, len(sources))
	for _, s := range sources {
		entries = append(entries, sourceEntry{
			ID:        s.ID,
			Name:      s.Name,
			Path:      s.Path,
			CreatedAt: s.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	return types.NewToolResult(entries), nil
}

func (h *DocHandler) tagDocument(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		FilePath      string `json:"file_path"`
		Tag           string `json:"tag"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.tagDocument: %w", err)
	}
	if args.FilePath == "" {
		return types.NewErrorResult("file_path is required"), nil
	}
	if args.Tag == "" {
		return types.NewErrorResult("tag is required"), nil
	}

	if h.store.ExternalDocs == nil {
		return types.NewErrorResult("external docs repo not available"), nil
	}

	if !allowedDocTags[args.Tag] {
		return types.NewErrorResult(fmt.Sprintf("invalid tag %q: must be one of 'architecture', 'standards'", args.Tag)), nil
	}

	if !strings.HasSuffix(strings.ToLower(args.FilePath), ".md") {
		return types.NewErrorResult("file_path must end with .md"), nil
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	sourceType := "internal"
	if strings.HasPrefix(args.FilePath, "@ext/") {
		sourceType = "external"
	} else {
		fullPath, pathErr := docValidatePath(ws.RootPath, args.FilePath)
		if pathErr != nil {
			return types.NewErrorResult(fmt.Sprintf("invalid path: %v", pathErr)), nil
		}
		if _, statErr := os.Stat(fullPath); os.IsNotExist(statErr) {
			return types.NewErrorResult(fmt.Sprintf("file not found: %s", args.FilePath)), nil
		}
	}

	tag := &repo.DocTag{
		WorkspaceID: ws.ID,
		FilePath:    args.FilePath,
		Tag:         args.Tag,
		SourceType:  sourceType,
	}

	if err := h.store.ExternalDocs.TagDocument(ctx, tag); err != nil {
		return types.NewErrorResult(fmt.Sprintf("tag document: %v", err)), nil
	}

	return types.NewToolResult(fmt.Sprintf("Tagged %s as %q (source: %s).", args.FilePath, args.Tag, sourceType)), nil
}

func (h *DocHandler) untagDocument(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Tag           string `json:"tag"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.untagDocument: %w", err)
	}
	if args.Tag == "" {
		return types.NewErrorResult("tag is required"), nil
	}

	if !allowedDocTags[args.Tag] {
		return types.NewErrorResult(fmt.Sprintf("invalid tag %q: must be one of 'architecture', 'standards'", args.Tag)), nil
	}

	if h.store.ExternalDocs == nil {
		return types.NewErrorResult("external docs repo not available"), nil
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	if err := h.store.ExternalDocs.UntagDocument(ctx, ws.ID, args.Tag); err != nil {
		return types.NewErrorResult(fmt.Sprintf("untag document: %v", err)), nil
	}

	return types.NewToolResult(fmt.Sprintf("Tag %q removed from workspace.", args.Tag)), nil
}

func (h *DocHandler) listDocTags(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.listDocTags: %w", err)
	}

	if h.store.ExternalDocs == nil {
		return types.NewErrorResult("external docs repo not available"), nil
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	tags, err := h.store.ExternalDocs.ListDocTags(ctx, ws.ID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("list doc tags: %v", err)), nil
	}

	if len(tags) == 0 {
		return types.NewToolResult("No document tags assigned in this workspace."), nil
	}

	type tagEntry struct {
		Tag        string `json:"tag"`
		FilePath   string `json:"file_path"`
		SourceType string `json:"source_type"`
		CreatedAt  string `json:"created_at"`
	}

	entries := make([]tagEntry, 0, len(tags))
	for _, t := range tags {
		entries = append(entries, tagEntry{
			Tag:        t.Tag,
			FilePath:   t.FilePath,
			SourceType: t.SourceType,
			CreatedAt:  t.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	return types.NewToolResult(entries), nil
}

func (h *DocHandler) getWorkspaceDocStatus(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.getWorkspaceDocStatus: %w", err)
	}

	if h.store.ExternalDocs == nil {
		return types.NewErrorResult("external docs repo not available"), nil
	}

	ws, err := h.store.Workspaces.GetWorkspace(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("workspace %q not found", args.WorkspaceName)), nil
	}

	archTag, archErr := h.store.ExternalDocs.GetDocTag(ctx, ws.ID, "architecture")
	stdTag, stdErr := h.store.ExternalDocs.GetDocTag(ctx, ws.ID, "standards")

	type docStatus struct {
		HasArchitecture bool    `json:"has_architecture"`
		HasStandards    bool    `json:"has_standards"`
		ArchitectureDoc *string `json:"architecture_doc"`
		StandardsDoc    *string `json:"standards_doc"`
	}

	status := docStatus{}

	if archErr == nil && archTag != nil {
		status.HasArchitecture = true
		status.ArchitectureDoc = &archTag.FilePath
	}
	if stdErr == nil && stdTag != nil {
		status.HasStandards = true
		status.StandardsDoc = &stdTag.FilePath
	}

	return types.NewToolResult(status), nil
}

// ─── Standards/Architecture methods ─────────────────────────────────────────

func (h *DocHandler) resolveTaggedDocPath(ctx context.Context, workspaceName, tag string) (string, error) {
	ws, err := h.store.Workspaces.GetWorkspace(ctx, workspaceName)
	if err != nil {
		return "", fmt.Errorf("workspace %q not found", workspaceName)
	}

	if h.store.ExternalDocs == nil {
		return "", fmt.Errorf("document tagging not available")
	}

	docTag, err := h.store.ExternalDocs.GetDocTag(ctx, ws.ID, tag)
	if err != nil || docTag == nil {
		return "", fmt.Errorf("no document tagged as %q in this workspace — use tag_document to assign one", tag)
	}

	if strings.HasPrefix(docTag.FilePath, "@ext/") {
		return h.resolveExternalPath(ctx, ws.ID, docTag.FilePath)
	}
	return docValidatePath(ws.RootPath, docTag.FilePath)
}

func (h *DocHandler) getTaggedDocContent(ctx context.Context, params json.RawMessage, tag string) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.getTaggedDocContent: %w", err)
	}

	fullPath, err := h.resolveTaggedDocPath(ctx, args.WorkspaceName, tag)
	if err != nil {
		return types.NewToolResult(err.Error()), nil
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return types.NewErrorResult(fmt.Sprintf("tagged file not found: %s", fullPath)), nil
		}
		return types.NewErrorResult(fmt.Sprintf("read file: %v", err)), nil
	}

	return types.NewToolResult(string(data)), nil
}

func (h *DocHandler) getTaggedDocToc(ctx context.Context, params json.RawMessage, tag string) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.getTaggedDocToc: %w", err)
	}

	fullPath, err := h.resolveTaggedDocPath(ctx, args.WorkspaceName, tag)
	if err != nil {
		return types.NewToolResult(err.Error()), nil
	}

	headings, err := docExtractHeadings(fullPath)
	if err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	if len(headings) == 0 {
		return types.NewToolResult("No headings found in document."), nil
	}

	return types.NewToolResult(headings), nil
}

func (h *DocHandler) getTaggedDocSection(ctx context.Context, params json.RawMessage, tag string) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Section       string `json:"section"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.getTaggedDocSection: %w", err)
	}

	if args.Section == "" {
		return types.NewErrorResult("section is required"), nil
	}

	fullPath, err := h.resolveTaggedDocPath(ctx, args.WorkspaceName, tag)
	if err != nil {
		return types.NewToolResult(err.Error()), nil
	}

	content, err := docExtractSection(fullPath, args.Section)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("section %q not found in %s document", args.Section, tag)), nil
	}

	return types.NewToolResult(content), nil
}

// ─── Specs methods ─────────────────────────────────────────────────────────

func (h *DocHandler) specRepo() (repo.SpecRepo, *types.ToolResult) {
	if h.store.Specs == nil {
		return nil, types.NewErrorResult("Spec repository not available.")
	}
	return h.store.Specs, nil
}

func (h *DocHandler) projectRepo() (repo.ProjectRepo, *types.ToolResult) {
	if h.store.Projects == nil {
		return nil, types.NewErrorResult("Project repository not available.")
	}
	return h.store.Projects, nil
}

func (h *DocHandler) createSpec(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
		Title         string `json:"title"`
		Description   string `json:"description"`
		CreatedBy     string `json:"created_by"`
		Milestones    []struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			Tasks       []struct {
				Title              string `json:"title"`
				Requirement        string `json:"requirement"`
				AcceptanceCriteria string `json:"acceptance_criteria"`
			} `json:"tasks"`
		} `json:"milestones"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.createSpec: %w", err)
	}

	if args.WorkspaceName == "" || args.Title == "" || args.Description == "" {
		return types.NewErrorResult("workspace_name, title, and description are required"), nil
	}
	if len(args.Milestones) == 0 {
		return types.NewErrorResult("at least one milestone is required"), nil
	}

	specs, errResult := h.specRepo()
	if errResult != nil {
		return errResult, nil
	}
	projects, errResult := h.projectRepo()
	if errResult != nil {
		return errResult, nil
	}

	specNumber, err := specs.NextSpecNumber(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("next spec number: %v", err)), nil
	}

	specLabel := fmt.Sprintf("SPEC-%d", specNumber)

	projectName := fmt.Sprintf("%s: %s", specLabel, args.Title)
	plan := &repo.ProjectPlan{
		Name:          projectName,
		Description:   fmt.Sprintf("Auto-created from %s.\n\n%s", specLabel, args.Description),
		WorkspaceName: args.WorkspaceName,
		Priority:      "medium",
	}
	projectID, err := projects.CreateProjectPlan(ctx, plan)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("create project plan: %v", err)), nil
	}

	spec := &repo.Spec{
		SpecNumber:    specNumber,
		Title:         args.Title,
		Description:   args.Description,
		ProjectID:     projectID,
		WorkspaceName: args.WorkspaceName,
		CreatedBy:     args.CreatedBy,
	}
	specID, err := specs.CreateSpec(ctx, spec)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("create spec: %v", err)), nil
	}

	type taskCreated struct {
		Title              string `json:"title"`
		Requirement        string `json:"requirement"`
		AcceptanceCriteria string `json:"acceptance_criteria"`
		TaskID             string `json:"task_id"`
		SpecTaskID         string `json:"spec_task_id"`
	}
	type milestoneCreated struct {
		Title           string        `json:"title"`
		MilestoneID     string        `json:"milestone_id"`
		SpecMilestoneID string        `json:"spec_milestone_id"`
		Tasks           []taskCreated `json:"tasks"`
	}

	createdMilestones := make([]milestoneCreated, 0, len(args.Milestones))
	totalTasks := 0

	for msIdx, msArg := range args.Milestones {
		if msArg.Title == "" {
			return types.NewErrorResult(fmt.Sprintf("milestone %d: title is required", msIdx+1)), nil
		}

		milestone := &repo.Milestone{
			ProjectID:   projectID,
			Name:        msArg.Title,
			Description: msArg.Description,
			OrderIndex:  msIdx,
		}
		milestoneID, err := projects.CreateMilestone(ctx, milestone)
		if err != nil {
			return types.NewErrorResult(fmt.Sprintf("create milestone %q: %v", msArg.Title, err)), nil
		}

		specMs := &repo.SpecMilestone{
			SpecID:      specID,
			Title:       msArg.Title,
			Description: msArg.Description,
			OrderIndex:  msIdx,
			MilestoneID: milestoneID,
		}
		specMsID, err := specs.CreateSpecMilestone(ctx, specMs)
		if err != nil {
			return types.NewErrorResult(fmt.Sprintf("create spec milestone: %v", err)), nil
		}

		mc := milestoneCreated{
			Title:           msArg.Title,
			MilestoneID:     milestoneID,
			SpecMilestoneID: specMsID,
			Tasks:           make([]taskCreated, 0, len(msArg.Tasks)),
		}

		for taskIdx, taskArg := range msArg.Tasks {
			if taskArg.Title == "" {
				return types.NewErrorResult(fmt.Sprintf("milestone %q task %d: title is required", msArg.Title, taskIdx+1)), nil
			}

			taskDesc := ""
			if taskArg.Requirement != "" {
				taskDesc = "**Requirement:** " + taskArg.Requirement
			}
			if taskArg.AcceptanceCriteria != "" {
				if taskDesc != "" {
					taskDesc += "\n\n"
				}
				taskDesc += "**Acceptance Criteria:** " + taskArg.AcceptanceCriteria
			}

			task := &repo.Task{
				MilestoneID: milestoneID,
				Name:        taskArg.Title,
				Description: taskDesc,
				OrderIndex:  taskIdx,
			}
			taskID, err := projects.CreateTask(ctx, task)
			if err != nil {
				return types.NewErrorResult(fmt.Sprintf("create task %q: %v", taskArg.Title, err)), nil
			}

			specTask := &repo.SpecTask{
				SpecID:             specID,
				SpecMilestoneID:    specMsID,
				Title:              taskArg.Title,
				Requirement:        taskArg.Requirement,
				AcceptanceCriteria: taskArg.AcceptanceCriteria,
				OrderIndex:         taskIdx,
				TaskID:             taskID,
			}
			specTaskID, err := specs.CreateSpecTask(ctx, specTask)
			if err != nil {
				return types.NewErrorResult(fmt.Sprintf("create spec task: %v", err)), nil
			}

			mc.Tasks = append(mc.Tasks, taskCreated{
				Title:              taskArg.Title,
				Requirement:        taskArg.Requirement,
				AcceptanceCriteria: taskArg.AcceptanceCriteria,
				TaskID:             taskID,
				SpecTaskID:         specTaskID,
			})
			totalTasks++
		}

		createdMilestones = append(createdMilestones, mc)
	}

	result := map[string]any{
		"spec_id":     specID,
		"spec_number": specNumber,
		"spec_label":  specLabel,
		"title":       args.Title,
		"project_id":  projectID,
		"status":      "draft",
		"milestones":  createdMilestones,
		"message": fmt.Sprintf(
			"#%s, Title: %q, was created with %d milestone(s) and %d task(s). Project %q created. Share with agents as needed.",
			specLabel, args.Title, len(createdMilestones), totalTasks, projectName,
		),
	}
	return types.NewToolResult(result), nil
}

func (h *DocHandler) listSpecs(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceName string `json:"workspace_name"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.listSpecs: %w", err)
	}

	if args.WorkspaceName == "" {
		return types.NewErrorResult("workspace_name is required"), nil
	}

	specs, errResult := h.specRepo()
	if errResult != nil {
		return errResult, nil
	}

	specList, err := specs.ListSpecs(ctx, args.WorkspaceName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("list specs: %v", err)), nil
	}

	if len(specList) == 0 {
		return types.NewToolResult("No specifications found."), nil
	}

	type specSummary struct {
		ID         string `json:"id"`
		SpecNumber int    `json:"spec_number"`
		Label      string `json:"label"`
		Title      string `json:"title"`
		Status     string `json:"status"`
		ProjectID  string `json:"project_id,omitempty"`
		CreatedBy  string `json:"created_by,omitempty"`
	}

	summaries := make([]specSummary, len(specList))
	for i, s := range specList {
		summaries[i] = specSummary{
			ID:         s.ID,
			SpecNumber: s.SpecNumber,
			Label:      fmt.Sprintf("SPEC-%d", s.SpecNumber),
			Title:      s.Title,
			Status:     s.Status,
			ProjectID:  s.ProjectID,
			CreatedBy:  s.CreatedBy,
		}
	}
	return types.NewToolResult(summaries), nil
}

func (h *DocHandler) getSpec(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		SpecID        string `json:"spec_id"`
		WorkspaceName string `json:"workspace_name"`
		SpecNumber    int    `json:"spec_number"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.getSpec: %w", err)
	}

	if args.SpecID == "" && (args.WorkspaceName == "" || args.SpecNumber == 0) {
		return types.NewErrorResult("provide spec_id or both workspace_name and spec_number"), nil
	}

	specs, errResult := h.specRepo()
	if errResult != nil {
		return errResult, nil
	}

	var spec *repo.Spec
	var err error
	if args.SpecID != "" {
		spec, err = specs.GetSpec(ctx, args.SpecID)
	} else {
		spec, err = specs.GetSpecByNumber(ctx, args.WorkspaceName, args.SpecNumber)
	}
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get spec: %v", err)), nil
	}

	milestones, err := specs.ListSpecMilestones(ctx, spec.ID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("list milestones: %v", err)), nil
	}

	type taskView struct {
		ID                 string `json:"id"`
		Title              string `json:"title"`
		Requirement        string `json:"requirement"`
		AcceptanceCriteria string `json:"acceptance_criteria"`
		TaskID             string `json:"task_id,omitempty"`
	}
	type milestoneView struct {
		ID          string     `json:"id"`
		Title       string     `json:"title"`
		Description string     `json:"description,omitempty"`
		MilestoneID string     `json:"milestone_id,omitempty"`
		Tasks       []taskView `json:"tasks"`
	}
	type amendmentView struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Author      string `json:"author,omitempty"`
		CreatedAt   string `json:"created_at"`
	}
	type specView struct {
		ID            string          `json:"id"`
		SpecNumber    int             `json:"spec_number"`
		Label         string          `json:"label"`
		Title         string          `json:"title"`
		Description   string          `json:"description"`
		Status        string          `json:"status"`
		ProjectID     string          `json:"project_id,omitempty"`
		WorkspaceName string          `json:"workspace_name"`
		CreatedBy     string          `json:"created_by,omitempty"`
		CreatedAt     string          `json:"created_at"`
		UpdatedAt     string          `json:"updated_at"`
		Milestones    []milestoneView `json:"milestones"`
		Amendments    []amendmentView `json:"amendments,omitempty"`
	}

	view := specView{
		ID:            spec.ID,
		SpecNumber:    spec.SpecNumber,
		Label:         fmt.Sprintf("SPEC-%d", spec.SpecNumber),
		Title:         spec.Title,
		Description:   spec.Description,
		Status:        spec.Status,
		ProjectID:     spec.ProjectID,
		WorkspaceName: spec.WorkspaceName,
		CreatedBy:     spec.CreatedBy,
		CreatedAt:     spec.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt:     spec.UpdatedAt.Format("2006-01-02 15:04:05"),
		Milestones:    make([]milestoneView, 0, len(milestones)),
	}

	for _, ms := range milestones {
		tasks, err := specs.ListSpecTasks(ctx, ms.ID)
		if err != nil {
			return types.NewErrorResult(fmt.Sprintf("list tasks for milestone %q: %v", ms.Title, err)), nil
		}

		mv := milestoneView{
			ID:          ms.ID,
			Title:       ms.Title,
			Description: ms.Description,
			MilestoneID: ms.MilestoneID,
			Tasks:       make([]taskView, 0, len(tasks)),
		}

		for _, t := range tasks {
			mv.Tasks = append(mv.Tasks, taskView{
				ID:                 t.ID,
				Title:              t.Title,
				Requirement:        t.Requirement,
				AcceptanceCriteria: t.AcceptanceCriteria,
				TaskID:             t.TaskID,
			})
		}

		view.Milestones = append(view.Milestones, mv)
	}

	amendments, err := specs.ListAmendments(ctx, spec.ID)
	if err == nil && len(amendments) > 0 {
		view.Amendments = make([]amendmentView, len(amendments))
		for i, a := range amendments {
			view.Amendments[i] = amendmentView{
				ID:          a.ID,
				Title:       a.Title,
				Description: a.Description,
				Author:      a.Author,
				CreatedAt:   a.CreatedAt.Format("2006-01-02 15:04:05"),
			}
		}
	}

	return types.NewToolResult(view), nil
}

func (h *DocHandler) amendSpec(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		SpecID      string `json:"spec_id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Author      string `json:"author"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.amendSpec: %w", err)
	}

	if args.SpecID == "" || args.Title == "" || args.Description == "" {
		return types.NewErrorResult("spec_id, title, and description are required"), nil
	}

	specs, errResult := h.specRepo()
	if errResult != nil {
		return errResult, nil
	}

	spec, err := specs.GetSpec(ctx, args.SpecID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("spec not found: %v", err)), nil
	}

	amendableStatuses := map[string]bool{
		"approved":    true,
		"in_progress": true,
		"completed":   true,
	}
	if !amendableStatuses[spec.Status] {
		return types.NewErrorResult(fmt.Sprintf(
			"spec SPEC-%d is in %q status — amendments are only allowed for approved, in_progress, or completed specs",
			spec.SpecNumber, spec.Status,
		)), nil
	}

	amendment := &repo.SpecAmendment{
		SpecID:      args.SpecID,
		Title:       args.Title,
		Description: args.Description,
		Author:      args.Author,
	}

	id, err := specs.CreateAmendment(ctx, amendment)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("create amendment: %v", err)), nil
	}

	result := map[string]any{
		"id":         id,
		"spec_id":    args.SpecID,
		"spec_label": fmt.Sprintf("SPEC-%d", spec.SpecNumber),
		"title":      args.Title,
		"message":    fmt.Sprintf("Amendment %q added to SPEC-%d: %s.", args.Title, spec.SpecNumber, spec.Title),
	}
	return types.NewToolResult(result), nil
}

func (h *DocHandler) updateSpecStatus(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		SpecID string `json:"spec_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.DocHandler.updateSpecStatus: %w", err)
	}

	if args.SpecID == "" || args.Status == "" {
		return types.NewErrorResult("spec_id and status are required"), nil
	}

	validStatuses := map[string]bool{
		"draft": true, "approved": true, "in_progress": true, "completed": true, "archived": true,
	}
	if !validStatuses[args.Status] {
		return types.NewErrorResult(fmt.Sprintf(
			"invalid status %q; valid values: draft, approved, in_progress, completed, archived",
			args.Status,
		)), nil
	}

	specs, errResult := h.specRepo()
	if errResult != nil {
		return errResult, nil
	}

	spec, err := specs.GetSpec(ctx, args.SpecID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("spec not found: %v", err)), nil
	}

	if err := specs.UpdateSpecStatus(ctx, args.SpecID, args.Status); err != nil {
		return types.NewErrorResult(fmt.Sprintf("update spec status: %v", err)), nil
	}

	if spec.ProjectID != "" {
		projects, projErr := h.projectRepo()
		if projErr == nil {
			projectStatus := args.Status
			if projectStatus == "approved" {
				projectStatus = "pending"
			}
			if updateErr := projects.UpdateProjectStatus(ctx, spec.ProjectID, projectStatus); updateErr != nil {
				slog.Warn("failed to update project status after spec status change",
					"project_id", spec.ProjectID, "status", projectStatus, "error", updateErr)
			}
		}
	}

	result := map[string]any{
		"spec_id":    args.SpecID,
		"spec_label": fmt.Sprintf("SPEC-%d", spec.SpecNumber),
		"status":     args.Status,
		"message":    fmt.Sprintf("SPEC-%d status updated to %q.", spec.SpecNumber, args.Status),
	}
	return types.NewToolResult(result), nil
}

// ─── Helpers ───────────────────────────────────────────────────────────────

// docValidatePath resolves a relative path against the workspace root and
// ensures the resulting path stays within the workspace root.
func docValidatePath(root, relPath string) (string, error) {
	return workspace.ValidatePath(root, relPath)
}

// resolveExternalPath resolves an @ext/{name}/{relPath} to an absolute
// filesystem path by looking up the external doc source by name.
func (h *DocHandler) resolveExternalPath(ctx context.Context, workspaceID, extPath string) (string, error) {
	parts := strings.SplitN(strings.TrimPrefix(extPath, "@ext/"), "/", 2)
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid external path: %s", extPath)
	}
	sourceName := parts[0]
	relPath := parts[1]

	if h.store.ExternalDocs == nil {
		return "", fmt.Errorf("external docs not available")
	}

	sources, err := h.store.ExternalDocs.ListExternalDocSources(ctx, workspaceID)
	if err != nil {
		return "", fmt.Errorf("lookup external sources: %w", err)
	}

	for _, src := range sources {
		if src.Name == sourceName {
			fullPath := filepath.Join(src.Path, relPath)
			cleanPath := filepath.Clean(fullPath)
			if !strings.HasPrefix(cleanPath, filepath.Clean(src.Path)) {
				return "", fmt.Errorf("path traversal detected: %s", extPath)
			}
			return cleanPath, nil
		}
	}

	return "", fmt.Errorf("external source %q not found", sourceName)
}

// docHeading represents a parsed markdown heading.
type docHeading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	Line  int    `json:"line"`
}

// docExtractHeadings parses all markdown headings from a file.
func docExtractHeadings(path string) ([]docHeading, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %s", path)
		}
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			slog.Warn("failed to close file", "error", cerr)
		}
	}()

	var headings []docHeading
	scanner := bufio.NewScanner(f)
	lineNum := 0
	inCodeBlock := false

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock {
			continue
		}

		if len(trimmed) > 0 && trimmed[0] == '#' {
			level := 0
			for _, ch := range trimmed {
				if ch == '#' {
					level++
				} else {
					break
				}
			}
			if level > 0 && level <= 6 && level < len(trimmed) && trimmed[level] == ' ' {
				text := strings.TrimSpace(trimmed[level+1:])
				headings = append(headings, docHeading{
					Level: level,
					Text:  text,
					Line:  lineNum,
				})
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan file: %w", err)
	}

	return headings, nil
}

// docExtractSection reads a file and returns the content under the heading
// whose text matches section (case-insensitive).
func docExtractSection(path, section string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file not found: %s", path)
		}
		return "", fmt.Errorf("open file: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			slog.Warn("failed to close file", "error", cerr)
		}
	}()

	scanner := bufio.NewScanner(f)
	inCodeBlock := false
	var sb strings.Builder
	capturing := false
	captureLevel := 0

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			if capturing {
				sb.WriteString(line)
				sb.WriteByte('\n')
			}
			continue
		}

		if !inCodeBlock && len(trimmed) > 0 && trimmed[0] == '#' {
			level := docHeadingLevel(trimmed)
			text := docHeadingText(trimmed, level)

			if capturing && level > 0 && level <= captureLevel {
				break
			}

			if !capturing && level > 0 && strings.EqualFold(text, section) {
				capturing = true
				captureLevel = level
			}
		}

		if capturing {
			sb.WriteString(line)
			sb.WriteByte('\n')
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan file: %w", err)
	}

	if sb.Len() == 0 {
		return "", fmt.Errorf("section %q not found", section)
	}

	return sb.String(), nil
}

func docHeadingLevel(trimmed string) int {
	level := 0
	for _, ch := range trimmed {
		if ch == '#' {
			level++
		} else {
			break
		}
	}
	if level > 0 && level <= 6 && level < len(trimmed) && trimmed[level] == ' ' {
		return level
	}
	return 0
}

func docHeadingText(trimmed string, level int) string {
	if level <= 0 || level >= len(trimmed) {
		return ""
	}
	return strings.TrimSpace(trimmed[level+1:])
}

// docCheckBrokenLinks scans a single line for markdown links and checks
// whether referenced .md files exist.
func docCheckBrokenLinks(workspaceRoot, docDir, line string, lineNum int, issues *[]string) {
	rest := line
	for {
		idx := strings.Index(rest, "](")
		if idx < 0 {
			break
		}
		rest = rest[idx+2:]
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			break
		}
		link := rest[:end]
		rest = rest[end+1:]

		if strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") || strings.HasPrefix(link, "#") {
			continue
		}

		if anchorIdx := strings.IndexByte(link, '#'); anchorIdx >= 0 {
			link = link[:anchorIdx]
		}
		if link == "" {
			continue
		}

		if !strings.HasSuffix(strings.ToLower(link), ".md") {
			continue
		}

		target := filepath.Join(workspaceRoot, docDir, link)
		target = filepath.Clean(target)
		if _, err := os.Stat(target); os.IsNotExist(err) {
			*issues = append(*issues, fmt.Sprintf("Line %d: broken link to %s", lineNum, link))
		}
	}
}

// docContainsMarkdownFiles checks whether a directory contains at least one .md file.
func docContainsMarkdownFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			return true
		}
	}
	for _, e := range entries {
		if e.IsDir() {
			subEntries, err := os.ReadDir(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			for _, se := range subEntries {
				if !se.IsDir() && strings.HasSuffix(strings.ToLower(se.Name()), ".md") {
					return true
				}
			}
		}
	}
	return false
}

// indexExternalSource walks the external source directory and indexes all
// markdown files.
func (h *DocHandler) indexExternalSource(ctx context.Context, workspaceID, sourceName, sourcePath string) (int, error) {
	if h.indexer == nil {
		return 0, fmt.Errorf("indexer not available")
	}

	prefix := fmt.Sprintf("@ext/%s", sourceName)
	indexed := 0

	err := filepath.WalkDir(sourcePath, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}

		relPath, err := filepath.Rel(sourcePath, path)
		if err != nil {
			return nil
		}

		indexedPath := filepath.Join(prefix, relPath)

		if err := h.indexer.IndexFileAs(ctx, workspaceID, path, indexedPath); err != nil {
			return nil
		}
		indexed++
		return nil
	})

	return indexed, err
}

// cleanupIndexedChunks walks the external source directory and deletes all
// indexed doc chunks with the given prefix.
func (h *DocHandler) cleanupIndexedChunks(ctx context.Context, workspaceID, sourcePath, prefix string) int {
	cleaned := 0

	walkErr := filepath.WalkDir(sourcePath, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}

		relPath, relErr := filepath.Rel(sourcePath, path)
		if relErr != nil {
			return nil
		}

		indexedPath := filepath.Join(prefix, relPath)
		if delErr := h.store.Search.DeleteDocChunksByPath(ctx, workspaceID, indexedPath); delErr == nil {
			cleaned++
		}
		return nil
	})

	if walkErr != nil {
		if delErr := h.store.Search.DeleteDocChunksByPath(ctx, workspaceID, prefix); delErr != nil {
			slog.Warn("failed to delete doc chunks by prefix after walk error",
				"workspace_id", workspaceID, "prefix", prefix, "error", delErr)
		}
	}

	return cleaned
}
