package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/hyperax/hyperax/internal/config"
	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/plugin"
	"github.com/hyperax/hyperax/internal/secrets"
	"github.com/hyperax/hyperax/pkg/types"
)

// actionClearancePlugin maps each plugin action to its minimum ABAC clearance.
var actionClearancePlugin = map[string]int{
	"install":           2,
	"list":              0,
	"enable":            2,
	"disable":           2,
	"get_info":          0,
	"uninstall":         2,
	"upgrade":           2,
	"configure":         1,
	"get_config":        0,
	"link_secret":       2,
	"configure_adapter": 2,
	"request_approval":  2,
	"approve":           2,
	"list_catalog":      0,
	"search_catalog":    0,
	"refresh_catalog":   2,
	"list_versions":     0,
}

// PluginHandler implements the consolidated "plugin" MCP tool,
// combining plugin lifecycle management and catalog browsing.
type PluginHandler struct {
	manager      *plugin.PluginManager
	configStore  *config.ConfigStore
	secretReg    *secrets.Registry
	approvalGate *plugin.ApprovalGate
	toolRegistry *mcp.ToolRegistry
	catalog      *plugin.CatalogManager
	logger       *slog.Logger
}

// NewPluginHandler creates a PluginHandler backed by the given PluginManager.
// configStore and secretReg may be nil (config/secret tools will return errors).
func NewPluginHandler(manager *plugin.PluginManager, configStore *config.ConfigStore, secretReg *secrets.Registry) *PluginHandler {
	return &PluginHandler{
		manager:     manager,
		configStore: configStore,
		secretReg:   secretReg,
	}
}

// SetApprovalGate sets the approval gate for challenge-response approval.
func (h *PluginHandler) SetApprovalGate(gate *plugin.ApprovalGate) {
	h.approvalGate = gate
}

// SetToolRegistry sets the tool registry for internal tool calls.
func (h *PluginHandler) SetToolRegistry(registry *mcp.ToolRegistry) {
	h.toolRegistry = registry
}

// SetCatalogDeps wires in catalog browsing dependencies.
func (h *PluginHandler) SetCatalogDeps(catalog *plugin.CatalogManager, logger *slog.Logger) {
	h.catalog = catalog
	h.logger = logger
}

// RegisterTools registers the consolidated "plugin" tool with the MCP registry.
func (h *PluginHandler) RegisterTools(registry *mcp.ToolRegistry) {
	registry.Register(
		"plugin",
		"Plugin lifecycle toolkit: install, enable, disable, configure, upgrade, uninstall, "+
			"approval, catalog browsing. "+
			"Actions: install | list | enable | disable | get_info | uninstall | upgrade | "+
			"configure | get_config | link_secret | configure_adapter | request_approval | "+
			"approve | list_catalog | search_catalog | refresh_catalog | list_versions",
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"action":          {"type": "string", "enum": ["install", "list", "enable", "disable", "get_info", "uninstall", "upgrade", "configure", "get_config", "link_secret", "configure_adapter", "request_approval", "approve", "list_catalog", "search_catalog", "refresh_catalog", "list_versions"], "description": "Action to perform"},
				"name":            {"type": "string", "description": "Plugin name"},
				"path":            {"type": "string", "description": "Local plugin directory path"},
				"manifest_url":    {"type": "string", "description": "Remote manifest URL"},
				"source":          {"type": "string", "description": "GitHub source (e.g. github.com/org/repo@version)"},
				"variable":        {"type": "string", "description": "Variable name (configure/link_secret)"},
				"value":           {"type": "string", "description": "Variable value (configure)"},
				"plugin_name":     {"type": "string", "description": "Plugin name (link_secret)"},
				"secret_key":      {"type": "string", "description": "Secret key name (link_secret)"},
				"secret_scope":    {"type": "string", "description": "Secret scope (link_secret, default: global)"},
				"adapter_key":     {"type": "string", "description": "Adapter config key"},
				"adapter_value":   {"type": "string", "description": "Adapter config value"},
				"channel_id":      {"type": "string", "description": "Channel ID for approval challenge"},
				"code":            {"type": "string", "description": "Challenge code (approve)"},
				"category":        {"type": "string", "description": "Filter by integration category (catalog)"},
				"verified_only":   {"type": "boolean", "description": "Only return verified plugins (catalog)"},
				"query":           {"type": "string", "description": "Search keyword (catalog)"}
			},
			"required": ["action"]
		}`),
		h.dispatch,
	)
}

// dispatch routes the consolidated "plugin" tool to the correct handler method.
func (h *PluginHandler) dispatch(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var envelope struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return nil, fmt.Errorf("handlers.PluginHandler.dispatch: %w", err)
	}

	if err := checkActionClearance(ctx, envelope.Action, actionClearancePlugin); err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	switch envelope.Action {
	case "install":
		return h.installPlugin(ctx, params)
	case "list":
		return h.listPlugins(ctx, params)
	case "enable":
		return h.enablePlugin(ctx, params)
	case "disable":
		return h.disablePlugin(ctx, params)
	case "get_info":
		return h.getPluginInfo(ctx, params)
	case "uninstall":
		return h.uninstallPlugin(ctx, params)
	case "upgrade":
		return h.upgradePlugin(ctx, params)
	case "configure":
		return h.configurePlugin(ctx, params)
	case "get_config":
		return h.getPluginConfig(ctx, params)
	case "link_secret":
		return h.linkPluginSecret(ctx, params)
	case "configure_adapter":
		return h.configurePluginAdapter(ctx, params)
	case "request_approval":
		return h.requestPluginApproval(ctx, params)
	case "approve":
		return h.approvePlugin(ctx, params)
	case "list_catalog":
		return h.listCatalog(ctx, params)
	case "search_catalog":
		return h.searchCatalog(ctx, params)
	case "refresh_catalog":
		return h.refreshCatalog(ctx, params)
	case "list_versions":
		return h.listPluginVersions(ctx, params)
	default:
		return types.NewErrorResult(fmt.Sprintf("unknown plugin action %q", envelope.Action)), nil
	}
}

// ── Plugin lifecycle action handlers ──

func (h *PluginHandler) installPlugin(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Path        string `json:"path"`
		ManifestURL string `json:"manifest_url"`
		Source      string `json:"source"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PluginHandler.installPlugin: %w", err)
	}

	// Validate exactly one source is provided.
	sources := 0
	if args.Path != "" {
		sources++
	}
	if args.ManifestURL != "" {
		sources++
	}
	if args.Source != "" {
		sources++
	}
	if sources == 0 {
		return types.NewErrorResult("one of path, manifest_url, or source is required"), nil
	}
	if sources > 1 {
		return types.NewErrorResult("provide only one of path, manifest_url, or source"), nil
	}

	var manifest *types.PluginManifest
	var source string
	var manifestPath string

	switch {
	case args.Path != "":
		m, err := plugin.ParseManifest(filepath.Join(args.Path, "hyperax-plugin.yaml"))
		if err != nil {
			return types.NewErrorResult(fmt.Sprintf("failed to read local manifest: %v", err)), nil
		}
		manifest = m
		source = "local:" + args.Path
		manifestPath = args.Path

	case args.ManifestURL != "":
		m, err := h.fetchRemoteManifest(ctx, args.ManifestURL)
		if err != nil {
			return types.NewErrorResult(fmt.Sprintf("failed to fetch remote manifest: %v", err)), nil
		}
		manifest = m
		source = "remote:" + args.ManifestURL

	case args.Source != "":
		src, err := plugin.ParseSource(args.Source)
		if err != nil {
			return types.NewErrorResult(fmt.Sprintf("invalid source: %v", err)), nil
		}

		var ghToken string
		if h.secretReg != nil {
			ghToken, err = secrets.ResolveSecretRef(ctx, h.secretReg, "secret:GITHUB_PLUGIN_TOKEN:system")
			if err != nil {
				slog.Warn("failed to resolve GitHub plugin token, proceeding without auth", "error", err)
			}
		}

		logger := slog.Default()
		m, err := plugin.FetchRelease(ctx, *src, ghToken, h.manager.PluginDir(), logger)
		if err != nil {
			return types.NewErrorResult(fmt.Sprintf("failed to fetch from GitHub: %v", err)), nil
		}
		manifest = m
		versionStr := src.Version
		if versionStr == "" && manifest.Version != "" {
			versionStr = manifest.Version
		}
		source = fmt.Sprintf("github:%s/%s@%s", src.Owner, src.Repo, versionStr)
	}

	if err := h.manager.LoadPlugin(ctx, *manifest); err != nil {
		return types.NewErrorResult(fmt.Sprintf("failed to load plugin: %v", err)), nil
	}

	h.manager.RegisterInstall(manifest.Name, source, manifestPath, args.ManifestURL)

	slog.Info("plugin installed",
		"plugin", manifest.Name,
		"version", manifest.Version,
		"source", source,
	)

	return types.NewToolResult(map[string]any{
		"message": fmt.Sprintf("Plugin %q v%s installed successfully with %d tools.",
			manifest.Name, manifest.Version, len(manifest.Tools)),
		"name":        manifest.Name,
		"version":     manifest.Version,
		"tools":       len(manifest.Tools),
		"source":      source,
		"integration": string(manifest.Integration),
	}), nil
}

func (h *PluginHandler) fetchRemoteManifest(ctx context.Context, rawURL string) (*types.PluginManifest, error) {
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return nil, fmt.Errorf("manifest_url must be an HTTP or HTTPS URL")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/x-yaml, text/yaml, text/plain, */*")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Warn("failed to close response body", "error", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, rawURL)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	manifest, err := plugin.ParseManifestFromBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parse remote manifest: %w", err)
	}

	return manifest, nil
}

func (h *PluginHandler) getPluginInfo(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Name     string `json:"name"`
		PluginID string `json:"plugin_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PluginHandler.getPluginInfo: %w", err)
	}
	lookup := args.Name
	if lookup == "" {
		lookup = args.PluginID
	}
	if lookup == "" {
		return types.NewErrorResult("name or plugin_id is required"), nil
	}

	state, manifest, err := h.manager.GetPluginInfo(lookup)
	if err != nil {
		plugins := h.manager.ListPlugins()
		for _, p := range plugins {
			if p.ID == lookup {
				state2, manifest2, err2 := h.manager.GetPluginInfo(p.Name)
				if err2 == nil {
					state = state2
					manifest = manifest2
					err = nil
				}
				break
			}
		}
		if err != nil {
			return types.NewErrorResult(fmt.Sprintf("plugin not found: %v", err)), nil
		}
	}

	toolNames := make([]string, len(manifest.Tools))
	for i, t := range manifest.Tools {
		toolNames[i] = t.Name
	}

	configValues := make(map[string]string)
	if h.configStore != nil {
		for _, v := range manifest.Variables {
			key := "plugin." + manifest.Name + ".var." + v.Name
			val, err := h.configStore.Resolve(ctx, key, "", "")
			if err == nil {
				configValues[v.Name] = val
			}
		}
	}

	approved := false
	if h.configStore != nil {
		approvalKey := plugin.ApprovalConfigKey(manifest.Name)
		val, err := h.configStore.Resolve(ctx, approvalKey, "", "")
		if err == nil && val == "true" {
			approved = true
		}
	}

	return types.NewToolResult(map[string]any{
		"id":                state.ID,
		"name":              state.Name,
		"version":           state.Version,
		"description":       manifest.Description,
		"author":            manifest.Author,
		"type":              string(state.Type),
		"status":            state.Status,
		"enabled":           state.Enabled,
		"tools":             toolNames,
		"permissions":       manifest.Permissions,
		"source_repo":       manifest.SourceRepo,
		"entrypoint":        manifest.Entrypoint,
		"source_hash":       types.PluginSourceHash(manifest.SourceRepo),
		"integration":       string(manifest.Integration),
		"variables":         manifest.Variables,
		"config":            configValues,
		"approval_required": manifest.ApprovalRequired,
		"approved":          approved,
		"resources":         manifest.Resources,
		"env":               manifest.Env,
	}), nil
}

func (h *PluginHandler) listPlugins(_ context.Context, _ json.RawMessage) (*types.ToolResult, error) {
	plugins := h.manager.ListPlugins()
	if len(plugins) == 0 {
		return types.NewToolResult([]any{}), nil
	}
	return types.NewToolResult(plugins), nil
}

func (h *PluginHandler) enablePlugin(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PluginHandler.enablePlugin: %w", err)
	}
	if args.Name == "" {
		return types.NewErrorResult("name is required"), nil
	}

	if err := h.manager.EnablePlugin(ctx, args.Name); err != nil {
		return types.NewErrorResult(fmt.Sprintf("enable plugin: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"name":   args.Name,
		"status": "enabled",
	}), nil
}

func (h *PluginHandler) disablePlugin(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PluginHandler.disablePlugin: %w", err)
	}
	if args.Name == "" {
		return types.NewErrorResult("name is required"), nil
	}

	if err := h.manager.DisablePlugin(ctx, args.Name); err != nil {
		return types.NewErrorResult(fmt.Sprintf("disable plugin: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"name":   args.Name,
		"status": "disabled",
	}), nil
}

func (h *PluginHandler) uninstallPlugin(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PluginHandler.uninstallPlugin: %w", err)
	}
	if args.Name == "" {
		return types.NewErrorResult("name is required"), nil
	}

	if err := h.manager.UnloadPlugin(ctx, args.Name); err != nil {
		return types.NewErrorResult(fmt.Sprintf("uninstall plugin: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"name":   args.Name,
		"status": "uninstalled",
	}), nil
}

func (h *PluginHandler) upgradePlugin(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Name        string `json:"name"`
		Path        string `json:"path"`
		ManifestURL string `json:"manifest_url"`
		Source      string `json:"source"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PluginHandler.upgradePlugin: %w", err)
	}
	if args.Name == "" {
		return types.NewErrorResult("name is required"), nil
	}

	oldState, getErr := h.manager.GetPlugin(args.Name)
	pluginLoaded := getErr == nil
	var wasEnabled bool
	var oldVersion string

	if pluginLoaded {
		wasEnabled = oldState.Enabled
		oldVersion = oldState.Version
	} else {
		rec := h.manager.GetRegistryRecord(args.Name)
		if rec == nil {
			return types.NewErrorResult(fmt.Sprintf("plugin %q is not installed", args.Name)), nil
		}
		wasEnabled = false
		oldVersion = "0.0.0"
	}

	var manifest *types.PluginManifest
	var source string
	var manifestPath string

	sources := 0
	if args.Path != "" {
		sources++
	}
	if args.ManifestURL != "" {
		sources++
	}
	if args.Source != "" {
		sources++
	}
	if sources > 1 {
		return types.NewErrorResult("provide only one of path, manifest_url, or source"), nil
	}

	if pluginLoaded {
		if wasEnabled {
			if err := h.manager.DisablePlugin(ctx, args.Name); err != nil {
				return types.NewErrorResult(fmt.Sprintf("failed to disable before upgrade: %v", err)), nil
			}
		}
		if err := h.manager.UnloadPlugin(ctx, args.Name); err != nil {
			return types.NewErrorResult(fmt.Sprintf("failed to unload old version: %v", err)), nil
		}
	} else {
		h.manager.CleanupBrokenPlugin(ctx, args.Name)
	}

	switch {
	case args.Path != "":
		m, err := plugin.ParseManifest(filepath.Join(args.Path, "hyperax-plugin.yaml"))
		if err != nil {
			return types.NewErrorResult(fmt.Sprintf("failed to read local manifest: %v", err)), nil
		}
		manifest = m
		source = "local:" + args.Path
		manifestPath = args.Path

	case args.ManifestURL != "":
		m, err := h.fetchRemoteManifest(ctx, args.ManifestURL)
		if err != nil {
			return types.NewErrorResult(fmt.Sprintf("failed to fetch remote manifest: %v", err)), nil
		}
		manifest = m
		source = "remote:" + args.ManifestURL

	case args.Source != "":
		src, err := plugin.ParseSource(args.Source)
		if err != nil {
			return types.NewErrorResult(fmt.Sprintf("invalid source: %v", err)), nil
		}
		var ghToken string
		if h.secretReg != nil {
			ghToken, err = secrets.ResolveSecretRef(ctx, h.secretReg, "secret:GITHUB_PLUGIN_TOKEN:system")
			if err != nil {
				slog.Warn("failed to resolve GitHub plugin token for upgrade, proceeding without auth", "error", err)
			}
		}
		logger := slog.Default()
		m, err := plugin.FetchRelease(ctx, *src, ghToken, h.manager.PluginDir(), logger)
		if err != nil {
			return types.NewErrorResult(fmt.Sprintf("failed to fetch from GitHub: %v", err)), nil
		}
		manifest = m
		versionStr := src.Version
		if versionStr == "" && manifest.Version != "" {
			versionStr = manifest.Version
		}
		source = fmt.Sprintf("github:%s/%s@%s", src.Owner, src.Repo, versionStr)

	default:
		return types.NewErrorResult("one of path, manifest_url, or source is required for upgrade"), nil
	}

	if manifest.Name != args.Name {
		return types.NewErrorResult(fmt.Sprintf(
			"manifest name %q does not match plugin %q", manifest.Name, args.Name)), nil
	}

	if err := h.manager.LoadPlugin(ctx, *manifest); err != nil {
		return types.NewErrorResult(fmt.Sprintf("failed to load new version: %v", err)), nil
	}

	h.manager.RegisterInstall(manifest.Name, source, manifestPath, args.ManifestURL)

	if wasEnabled {
		if err := h.manager.EnablePlugin(ctx, args.Name); err != nil {
			slog.Warn("upgrade: failed to re-enable plugin",
				"plugin", args.Name, "error", err)
			return types.NewToolResult(map[string]any{
				"name":         manifest.Name,
				"old_version":  oldVersion,
				"new_version":  manifest.Version,
				"status":       "upgraded_but_not_enabled",
				"enable_error": err.Error(),
				"message": fmt.Sprintf("Plugin %q upgraded from v%s to v%s but failed to re-enable: %v",
					args.Name, oldVersion, manifest.Version, err),
			}), nil
		}
	}

	slog.Info("plugin upgraded",
		"plugin", manifest.Name,
		"old_version", oldVersion,
		"new_version", manifest.Version,
		"re_enabled", wasEnabled,
	)

	return types.NewToolResult(map[string]any{
		"name":        manifest.Name,
		"old_version": oldVersion,
		"new_version": manifest.Version,
		"status":      "upgraded",
		"enabled":     wasEnabled,
		"message": fmt.Sprintf("Plugin %q upgraded from v%s to v%s.",
			manifest.Name, oldVersion, manifest.Version),
	}), nil
}

func (h *PluginHandler) configurePlugin(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Name     string `json:"name"`
		Variable string `json:"variable"`
		Value    string `json:"value"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PluginHandler.configurePlugin: %w", err)
	}
	if args.Name == "" || args.Variable == "" {
		return types.NewErrorResult("name and variable are required"), nil
	}
	if h.configStore == nil {
		return types.NewErrorResult("config store not available"), nil
	}

	manifest, err := h.manager.GetManifest(args.Name)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("plugin not found: %v", err)), nil
	}

	var found *types.PluginVariable
	for i := range manifest.Variables {
		if manifest.Variables[i].Name == args.Variable {
			found = &manifest.Variables[i]
			break
		}
	}
	if found == nil {
		return types.NewErrorResult(fmt.Sprintf("variable %q not declared in plugin %q manifest", args.Variable, args.Name)), nil
	}
	if found.Secret {
		return types.NewErrorResult("use link_secret action to set secret variables"), nil
	}

	key := "plugin." + args.Name + ".var." + args.Variable
	scope := types.ConfigScope{Type: "global"}
	if err := h.configStore.Set(ctx, key, args.Value, scope, "plugin-config"); err != nil {
		return types.NewErrorResult(fmt.Sprintf("set config: %v", err)), nil
	}

	notified := false
	if found.Dynamic {
		if err := h.manager.NotifyConfigChanged(args.Name, args.Variable, args.Value); err != nil {
			return types.NewToolResult(map[string]any{
				"plugin":       args.Name,
				"variable":     args.Variable,
				"value":        args.Value,
				"dynamic":      true,
				"notify_error": err.Error(),
			}), nil
		}
		notified = true
	}

	return types.NewToolResult(map[string]any{
		"plugin":   args.Name,
		"variable": args.Variable,
		"value":    args.Value,
		"dynamic":  found.Dynamic,
		"notified": notified,
	}), nil
}

func (h *PluginHandler) configurePluginAdapter(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Name         string `json:"name"`
		AdapterKey   string `json:"adapter_key"`
		AdapterValue string `json:"adapter_value"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PluginHandler.configurePluginAdapter: %w", err)
	}
	if args.Name == "" || args.AdapterKey == "" {
		return types.NewErrorResult("name and adapter_key are required"), nil
	}
	if h.configStore == nil {
		return types.NewErrorResult("config store not available"), nil
	}

	if _, err := h.manager.GetManifest(args.Name); err != nil {
		return types.NewErrorResult(fmt.Sprintf("plugin not found: %v", err)), nil
	}

	key := "plugin." + args.Name + ".adapter." + args.AdapterKey
	scope := types.ConfigScope{Type: "global"}
	if err := h.configStore.Set(ctx, key, args.AdapterValue, scope, "plugin-adapter-config"); err != nil {
		return types.NewErrorResult(fmt.Sprintf("set adapter config: %v", err)), nil
	}

	slog.Info("plugin adapter configured",
		"plugin", args.Name,
		"adapter_key", args.AdapterKey,
	)

	return types.NewToolResult(map[string]any{
		"plugin":        args.Name,
		"adapter_key":   args.AdapterKey,
		"adapter_value": args.AdapterValue,
		"status":        "configured",
	}), nil
}

func (h *PluginHandler) getPluginConfig(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PluginHandler.getPluginConfig: %w", err)
	}
	if args.Name == "" {
		return types.NewErrorResult("name is required"), nil
	}
	if h.configStore == nil {
		return types.NewErrorResult("config store not available"), nil
	}

	manifest, err := h.manager.GetManifest(args.Name)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("plugin not found: %v", err)), nil
	}

	values := make(map[string]any)
	for _, v := range manifest.Variables {
		key := "plugin." + args.Name + ".var." + v.Name
		val, err := h.configStore.Resolve(ctx, key, "", "")
		if err == nil {
			values[v.Name] = val
		} else if v.Default != nil {
			values[v.Name] = v.Default
		} else {
			values[v.Name] = nil
		}
	}

	return types.NewToolResult(map[string]any{
		"plugin": args.Name,
		"config": values,
	}), nil
}

func (h *PluginHandler) linkPluginSecret(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		PluginName  string `json:"plugin_name"`
		Name        string `json:"name"`
		Variable    string `json:"variable"`
		SecretKey   string `json:"secret_key"`
		SecretScope string `json:"secret_scope"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PluginHandler.linkPluginSecret: %w", err)
	}
	// Accept either plugin_name or name for the plugin identifier.
	pluginName := args.PluginName
	if pluginName == "" {
		pluginName = args.Name
	}
	if pluginName == "" || args.Variable == "" || args.SecretKey == "" {
		return types.NewErrorResult("plugin_name (or name), variable, and secret_key are required"), nil
	}
	if h.configStore == nil {
		return types.NewErrorResult("config store not available"), nil
	}

	manifest, err := h.manager.GetManifest(pluginName)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("plugin not found: %v", err)), nil
	}

	var found *types.PluginVariable
	for i := range manifest.Variables {
		if manifest.Variables[i].Name == args.Variable {
			found = &manifest.Variables[i]
			break
		}
	}
	if found == nil {
		return types.NewErrorResult(fmt.Sprintf("variable %q not declared in plugin %q manifest", args.Variable, pluginName)), nil
	}
	if !found.Secret {
		return types.NewErrorResult(fmt.Sprintf("variable %q is not a secret variable, use configure action instead", args.Variable)), nil
	}

	scope := args.SecretScope
	if scope == "" {
		scope = "global"
	}
	ref := fmt.Sprintf("secret:%s:%s", args.SecretKey, scope)

	key := "plugin." + pluginName + ".var." + args.Variable
	configScope := types.ConfigScope{Type: "global"}
	if err := h.configStore.Set(ctx, key, ref, configScope, "plugin-secret-link"); err != nil {
		return types.NewErrorResult(fmt.Sprintf("set config: %v", err)), nil
	}

	return types.NewToolResult(map[string]any{
		"plugin":     pluginName,
		"variable":   args.Variable,
		"secret_ref": ref,
	}), nil
}

func (h *PluginHandler) requestPluginApproval(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Name      string `json:"name"`
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PluginHandler.requestPluginApproval: %w", err)
	}
	if args.Name == "" || args.ChannelID == "" {
		return types.NewErrorResult("name and channel_id are required"), nil
	}
	if h.approvalGate == nil {
		return types.NewErrorResult("approval gate not configured"), nil
	}

	manifest, err := h.manager.GetManifest(args.Name)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("plugin not found: %v", err)), nil
	}
	if !manifest.ApprovalRequired {
		return types.NewErrorResult(fmt.Sprintf("plugin %q does not require approval", args.Name)), nil
	}

	if h.approvalGate.IsApproved(ctx, args.Name) {
		return types.NewToolResult(map[string]any{
			"plugin":  args.Name,
			"status":  "already_approved",
			"message": fmt.Sprintf("Plugin %q is already approved", args.Name),
		}), nil
	}

	code, err := h.approvalGate.GenerateChallenge(args.Name, args.ChannelID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("generate challenge: %v", err)), nil
	}

	sendToolName := fmt.Sprintf("plugin_%s_send_message", args.Name)

	if h.toolRegistry != nil && h.toolRegistry.HasTool(sendToolName) {
		sendParams, marshalErr := json.Marshal(map[string]string{
			"channel_id": args.ChannelID,
			"content":    fmt.Sprintf("**Hyperax Plugin Verification**\n\nYour approval code is: `%s`\n\nEnter this code in the Hyperax UI to approve the %s plugin connection.\nThis code expires in 10 minutes.", code, args.Name),
		})
		if marshalErr != nil {
			return types.NewErrorResult(fmt.Sprintf("failed to marshal send params: %v", marshalErr)), nil
		}

		_, toolErr := h.toolRegistry.Dispatch(ctx, sendToolName, sendParams)
		if toolErr != nil {
			slog.Warn("failed to send challenge code via plugin tool",
				"plugin", args.Name,
				"tool", sendToolName,
				"error", toolErr.Error(),
			)
			return types.NewToolResult(map[string]any{
				"plugin":  args.Name,
				"status":  "challenge_generated",
				"code":    code,
				"warning": fmt.Sprintf("Could not send code via %s: %v. Code shown here for manual verification.", sendToolName, toolErr),
			}), nil
		}

		return types.NewToolResult(map[string]any{
			"plugin":     args.Name,
			"status":     "challenge_sent",
			"channel_id": args.ChannelID,
			"message":    fmt.Sprintf("Challenge code sent to channel %s. Enter the code in the UI to complete approval.", args.ChannelID),
		}), nil
	}

	return types.NewToolResult(map[string]any{
		"plugin":  args.Name,
		"status":  "challenge_generated",
		"code":    code,
		"message": "No send tool available for this plugin. Use this code for manual verification.",
	}), nil
}

func (h *PluginHandler) approvePlugin(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Name string `json:"name"`
		Code string `json:"code"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PluginHandler.approvePlugin: %w", err)
	}
	if args.Name == "" {
		return types.NewErrorResult("name is required"), nil
	}
	if h.configStore == nil {
		return types.NewErrorResult("config store not available"), nil
	}

	manifest, err := h.manager.GetManifest(args.Name)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("plugin not found: %v", err)), nil
	}

	if manifest.ApprovalRequired && h.approvalGate != nil {
		if args.Code == "" {
			if h.approvalGate.HasPendingChallenge(args.Name) {
				return types.NewErrorResult("a challenge is pending — enter the code sent to your channel"), nil
			}
			return types.NewErrorResult("this plugin requires challenge-response approval — use request_approval action first"), nil
		}

		if !h.approvalGate.ValidateChallenge(args.Name, args.Code) {
			return types.NewErrorResult("invalid or expired challenge code"), nil
		}
	}

	key := plugin.ApprovalConfigKey(args.Name)
	scope := types.ConfigScope{Type: "global"}
	if err := h.configStore.Set(ctx, key, "true", scope, "plugin-approval"); err != nil {
		return types.NewErrorResult(fmt.Sprintf("set approval: %v", err)), nil
	}

	return types.NewToolResult(map[string]any{
		"plugin":   args.Name,
		"approved": true,
	}), nil
}

// ── Catalog action handlers ──

func (h *PluginHandler) listCatalog(_ context.Context, params json.RawMessage) (*types.ToolResult, error) {
	if h.catalog == nil {
		return types.NewErrorResult("plugin catalog not available"), nil
	}

	var args struct {
		Category     string `json:"category"`
		VerifiedOnly bool   `json:"verified_only"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &args); err != nil {
			return nil, fmt.Errorf("handlers.PluginHandler.listCatalog: %w", err)
		}
	}
	entries := h.catalog.List(args.Category, args.VerifiedOnly)
	return types.NewToolResult(entries), nil
}

func (h *PluginHandler) searchCatalog(_ context.Context, params json.RawMessage) (*types.ToolResult, error) {
	if h.catalog == nil {
		return types.NewErrorResult("plugin catalog not available"), nil
	}

	var args struct {
		Query    string `json:"query"`
		Category string `json:"category"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PluginHandler.searchCatalog: %w", err)
	}
	entries := h.catalog.Search(args.Query, args.Category)
	return types.NewToolResult(entries), nil
}

func (h *PluginHandler) refreshCatalog(ctx context.Context, _ json.RawMessage) (*types.ToolResult, error) {
	if h.catalog == nil {
		return types.NewErrorResult("plugin catalog not available"), nil
	}

	var ghToken string
	if h.secretReg != nil {
		var tokenErr error
		ghToken, tokenErr = secrets.ResolveSecretRef(ctx, h.secretReg, "secret:GITHUB_PLUGIN_TOKEN:system")
		if tokenErr != nil {
			slog.Warn("failed to resolve GitHub plugin token for catalog refresh, proceeding without auth", "error", tokenErr)
		}
	}

	added, updated, err := h.catalog.Refresh(ctx, "runtime-dynamics", "hax-plugin-", ghToken)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("refresh catalog: %v", err)), nil
	}
	return types.NewToolResult(map[string]any{
		"added":   added,
		"updated": updated,
		"message": "Catalog refreshed from GitHub",
	}), nil
}

func (h *PluginHandler) listPluginVersions(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	if h.catalog == nil {
		return types.NewErrorResult("plugin catalog not available"), nil
	}

	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.PluginHandler.listPluginVersions: %w", err)
	}
	if args.Name == "" {
		return types.NewErrorResult("name is required"), nil
	}

	var ghToken string
	if h.secretReg != nil {
		var tokenErr error
		ghToken, tokenErr = secrets.ResolveSecretRef(ctx, h.secretReg, "secret:GITHUB_PLUGIN_TOKEN:system")
		if tokenErr != nil {
			slog.Warn("failed to resolve GitHub plugin token for version list, proceeding without auth", "error", tokenErr)
		}
	}

	versions, err := h.catalog.ListVersions(ctx, args.Name, ghToken)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("list versions: %v", err)), nil
	}

	return types.NewToolResult(map[string]any{
		"name":     args.Name,
		"versions": versions,
		"count":    len(versions),
	}), nil
}
