// Package plugin implements the Hyperax plugin system.
//
// The PluginManager handles plugin lifecycle: discovery, loading, enabling,
// disabling, and unloading. For MCP-type plugins, it launches a subprocess,
// establishes a JSON-RPC 2.0 client connection, discovers tools via tools/list,
// and federates them into the main ToolRegistry with real proxy handlers.
// Non-MCP plugin types (wasm, http, native) still use placeholder handlers.
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"sort"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// CronRepoInterface is the subset of repo.CronRepo used by PluginManager
// for auto-creating and cleaning up plugin resources.
type CronRepoInterface interface {
	CreateJob(ctx context.Context, job *repo.CronJob) (string, error)
	DeleteJob(ctx context.Context, id string) error
}

// manifestFileName is the expected manifest filename in each plugin directory.
const manifestFileName = "hyperax-plugin.yaml"

// mcpInitTimeout is the maximum time allowed for MCP initialize + tools/list.
const mcpInitTimeout = 30 * time.Second

// LoadedPlugin holds the runtime state and metadata for a loaded plugin.
type LoadedPlugin struct {
	// Manifest is the parsed plugin manifest from disk.
	Manifest types.PluginManifest

	// State tracks runtime status, health, and tool count.
	State types.PluginState

	// registeredTools lists the tool names registered in the MCP registry
	// for this plugin, enabling clean deregistration on unload.
	registeredTools []string

	// subprocess is the running plugin process (MCP type only).
	subprocess *Subprocess

	// mcpClient is the JSON-RPC client connected to the subprocess (MCP type only).
	mcpClient *MCPClient

	// mcpCancel cancels the MCP client reader goroutine.
	mcpCancel context.CancelFunc
}

// PluginConfigResolver provides functions for resolving plugin variable values
// and secret references at subprocess start time.
type PluginConfigResolver struct {
	// GetVar retrieves a plugin variable value from config storage.
	// Returns ("", nil) if not found (caller should fall back to default).
	GetVar func(ctx context.Context, pluginName, varName string) (string, error)

	// ResolveSecret resolves a "secret:KEY" or "secret:KEY:SCOPE" reference to its actual value.
	ResolveSecret func(ctx context.Context, ref string) (string, error)
}

// PluginManager orchestrates plugin lifecycle within the Hyperax server.
// It is safe for concurrent use.
// SecretRegistryBridge provides callbacks for registering and unregistering
// plugin secret provider adapters with the secrets.Registry. Using callbacks
// avoids an import cycle between plugin and secrets packages. The wiring code
// in router.go supplies the concrete implementations.
type SecretRegistryBridge struct {
	// RegisterAdapter registers a PluginSecretAdapter with the secrets registry.
	// The adapter already implements secrets.Provider.
	RegisterAdapter func(adapter *PluginSecretAdapter)

	// UnregisterAdapter removes a provider by name from the secrets registry.
	UnregisterAdapter func(name string)

	// IsActive returns true if the named provider is the currently active one.
	// Used to guard against removing an in-use secret provider.
	IsActive func(name string) bool

	// HasSecrets returns true if the named provider has any stored secrets.
	// Used to prevent removing a provider that still holds data.
	HasSecrets func(ctx context.Context, name string) (bool, error)
}

// GuardBridge connects the plugin system to the guard middleware.
// Using callbacks avoids an import cycle between the plugin and guard packages.
// The wiring code in router.go supplies the concrete implementations.
type GuardBridge struct {
	// RegisterGuard registers a plugin guard evaluator with the guard middleware.
	RegisterGuard func(pluginName, toolName string, dispatch func(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error), timeout time.Duration)

	// UnregisterGuard removes a plugin guard by plugin name.
	UnregisterGuard func(pluginName string)
}

// AuditBridge connects the plugin system to the audit PluginAuditSink.
// Using callbacks avoids an import cycle between the plugin and audit packages.
// The wiring code in router.go supplies the concrete implementations.
type AuditBridge struct {
	// RegisterWriter registers a plugin audit writer with the audit sink.
	RegisterWriter func(pluginName string, dispatch func(ctx context.Context, name string, params json.RawMessage) (*types.ToolResult, error))

	// UnregisterWriter removes a plugin audit writer by plugin name.
	UnregisterWriter func(pluginName string)
}

type PluginManager struct {
	plugins         map[string]*LoadedPlugin
	mu              sync.RWMutex
	registry        *mcp.ToolRegistry
	installRegistry *Registry
	bus             *nervous.EventBus
	logger          *slog.Logger
	pluginDir       string
	eventBridge     *EventBridge
	hyperaxVersion  string
	configResolver  *PluginConfigResolver
	cronRepo        CronRepoInterface
	configKeySeeder ConfigKeySeeder
	secretBridge    *SecretRegistryBridge
	guardBridge     *GuardBridge
	auditBridge     *AuditBridge
	pluginRepo      repo.PluginRepo
}

// NewPluginManager creates a PluginManager. The pluginDir is the directory
// scanned by Discover() for plugin manifests. If pluginDir is empty, Discover
// will return immediately with no error. hyperaxVersion is the running binary
// version (e.g. "1.2.0" or "dev"), used for manifest compatibility checks.
func NewPluginManager(registry *mcp.ToolRegistry, bus *nervous.EventBus, logger *slog.Logger, pluginDir, hyperaxVersion string) *PluginManager {
	var bridge *EventBridge
	if bus != nil {
		bridge = NewEventBridge(bus, logger)
	}

	// Initialise the install registry for persistence across restarts.
	// If loading fails (e.g. permission error), log a warning and continue
	// without persistence — the server should still start.
	var instReg *Registry
	if pluginDir != "" {
		var err error
		instReg, err = NewRegistry(pluginDir)
		if err != nil {
			logger.Warn("failed to load plugin install registry, persistence disabled",
				"error", err,
			)
		}
	}

	return &PluginManager{
		plugins:         make(map[string]*LoadedPlugin),
		registry:        registry,
		installRegistry: instReg,
		bus:             bus,
		logger:          logger.With("component", "plugin-manager"),
		pluginDir:       pluginDir,
		eventBridge:     bridge,
		hyperaxVersion:  hyperaxVersion,
	}
}

// PluginDir returns the configured plugin directory path.
func (pm *PluginManager) PluginDir() string {
	return pm.pluginDir
}

// Discover scans pluginDir for subdirectories containing a hyperax-plugin.yaml
// manifest. Each valid manifest is loaded via LoadPlugin. Discovery errors for
// individual plugins are logged but do not abort the scan.
func (pm *PluginManager) Discover(ctx context.Context) error {
	if pm.pluginDir == "" {
		pm.logger.Info("plugin directory not configured, skipping discovery")
		return nil
	}

	info, err := os.Stat(pm.pluginDir)
	if err != nil {
		if os.IsNotExist(err) {
			pm.logger.Info("plugin directory does not exist, skipping discovery", "dir", pm.pluginDir)
			return nil
		}
		return fmt.Errorf("plugin.PluginManager.Discover: stat plugin directory %s: %w", pm.pluginDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("plugin.PluginManager.Discover: plugin path %s is not a directory", pm.pluginDir)
	}

	entries, err := os.ReadDir(pm.pluginDir)
	if err != nil {
		return fmt.Errorf("plugin.PluginManager.Discover: read plugin directory %s: %w", pm.pluginDir, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		manifestPath := filepath.Join(pm.pluginDir, entry.Name(), manifestFileName)
		if _, statErr := os.Stat(manifestPath); statErr != nil {
			continue // no manifest in this subdirectory
		}

		manifest, parseErr := ParseManifest(manifestPath)
		if parseErr != nil {
			pm.logger.Warn("failed to parse plugin manifest",
				"path", manifestPath,
				"error", parseErr.Error(),
			)
			continue
		}

		if loadErr := pm.LoadPlugin(ctx, *manifest); loadErr != nil {
			pm.logger.Warn("failed to load plugin during discovery",
				"plugin", manifest.Name,
				"error", loadErr.Error(),
			)
		}
	}

	return nil
}

// LoadPlugin validates a manifest, checks permissions, creates the plugin state,
// and registers tools. For MCP-type plugins, placeholder tools are registered
// initially; real proxy handlers are connected when EnablePlugin launches the
// subprocess. For other types, placeholder tools remain until those loaders are
// implemented.
func (pm *PluginManager) LoadPlugin(ctx context.Context, manifest types.PluginManifest) error {
	if err := ValidateManifest(&manifest, pm.hyperaxVersion); err != nil {
		return err
	}
	if err := ValidatePermissions(manifest.Permissions); err != nil {
		return fmt.Errorf("plugin.PluginManager.LoadPlugin: plugin %q: %w", manifest.Name, err)
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.plugins[manifest.Name]; exists {
		return fmt.Errorf("plugin.PluginManager.LoadPlugin: plugin %q is already loaded", manifest.Name)
	}

	now := time.Now()
	lp := &LoadedPlugin{
		Manifest: manifest,
		State: types.PluginState{
			ID:           uuid.New().String(),
			Name:         manifest.Name,
			Version:      manifest.Version,
			Type:         manifest.Type,
			Status:       "loaded",
			Enabled:      false,
			ToolCount:    len(manifest.Tools),
			HealthStatus: "unknown",
			LoadedAt:     now,
		},
	}

	// Register placeholder tools. Each tool dispatches to a stub that returns
	// a "not yet connected" message. For MCP plugins, these are replaced with
	// real proxy handlers when the subprocess is started in EnablePlugin.
	for _, toolDef := range manifest.Tools {
		toolName := pm.qualifiedToolName(manifest.Name, toolDef.Name)

		if pm.registry.HasTool(toolName) {
			pm.logger.Warn("skipping duplicate tool registration",
				"plugin", manifest.Name,
				"tool", toolName,
			)
			continue
		}

		inputSchema := buildInputSchema(toolDef)
		handler := pm.makePlaceholderHandler(manifest.Name, toolDef.Name)

		pm.registry.Register(toolName, toolDef.Description, inputSchema, handler)
		lp.registeredTools = append(lp.registeredTools, toolName)
	}

	pm.plugins[manifest.Name] = lp

	// Seed ConfigKeyMeta for plugin variables so they're discoverable.
	pm.seedVariableConfigKeys(ctx, manifest)

	pm.logger.Info("plugin loaded",
		"plugin", manifest.Name,
		"version", manifest.Version,
		"type", string(manifest.Type),
		"tools", len(manifest.Tools),
	)

	pm.publishEvent(types.EventPluginLoaded, manifest.Name, map[string]any{
		"name":    manifest.Name,
		"version": manifest.Version,
		"tools":   len(manifest.Tools),
	})

	return nil
}

// UnloadPlugin deregisters all tools for the named plugin, stops any running
// subprocess, and removes it from the manager.
func (pm *PluginManager) UnloadPlugin(ctx context.Context, name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	lp, ok := pm.plugins[name]
	if !ok {
		return fmt.Errorf("plugin.PluginManager.UnloadPlugin: plugin %q not found", name)
	}

	// Clean up auto-created resources (cron jobs, event handlers).
	pm.cleanupPluginResources(ctx, lp)

	// Unregister secret adapter if this is a secret_provider plugin.
	// Block removal if this provider is active and still holds secrets.
	if lp.Manifest.Integration == types.PluginIntegrationSecretProvider && pm.secretBridge != nil {
		if pm.secretBridge.IsActive != nil && pm.secretBridge.IsActive(lp.Manifest.Name) {
			if pm.secretBridge.HasSecrets != nil {
				hasSecrets, err := pm.secretBridge.HasSecrets(ctx, lp.Manifest.Name)
				if err == nil && hasSecrets {
					return fmt.Errorf("plugin.PluginManager.UnloadPlugin: cannot uninstall plugin %q: active secret provider with stored secrets", name)
				}
			}
		}
		pm.secretBridge.UnregisterAdapter(lp.Manifest.Name)
	}

	// Unregister guard evaluator if this is a guard plugin.
	if lp.Manifest.Integration == types.PluginIntegrationGuard && pm.guardBridge != nil {
		pm.guardBridge.UnregisterGuard(lp.Manifest.Name)
	}

	// Unregister audit writer if this is an audit plugin.
	if lp.Manifest.Integration == types.PluginIntegrationAudit && pm.auditBridge != nil {
		pm.auditBridge.UnregisterWriter(lp.Manifest.Name)
	}

	// Stop MCP subprocess if running.
	pm.stopMCPRuntime(lp)

	// Deregister all tools from the MCP registry.
	for _, toolName := range lp.registeredTools {
		pm.registry.Unregister(toolName)
	}

	delete(pm.plugins, name)

	// Remove from persistent install registry so the plugin is not
	// reloaded on next server startup.
	pm.DeregisterInstall(name)

	// Remove persisted plugin state from the database.
	if pm.pluginRepo != nil {
		if err := pm.pluginRepo.DeletePlugin(ctx, name); err != nil {
			pm.logger.Debug("failed to delete persisted plugin state",
				"plugin", name, "error", err)
		}
	}

	// Delete the plugin directory from disk so Discover() doesn't re-add it.
	if pm.pluginDir != "" {
		pluginPath := filepath.Join(pm.pluginDir, name)
		if info, statErr := os.Stat(pluginPath); statErr == nil && info.IsDir() {
			if rmErr := os.RemoveAll(pluginPath); rmErr != nil {
				pm.logger.Warn("failed to remove plugin directory",
					"plugin", name, "path", pluginPath, "error", rmErr)
			} else {
				pm.logger.Info("removed plugin directory", "plugin", name, "path", pluginPath)
			}
		}
	}

	pm.logger.Info("plugin unloaded", "plugin", name)
	pm.publishEvent(types.EventPluginUnloaded, name, map[string]any{
		"name": name,
	})

	return nil
}

// EnablePlugin transitions a loaded plugin to "enabled" state.
// For MCP-type plugins, this launches the subprocess, establishes the MCP
// client connection, discovers tools via tools/list, and replaces placeholder
// handlers with real proxy handlers. For other types, it simply marks enabled.
func (pm *PluginManager) EnablePlugin(ctx context.Context, name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	lp, ok := pm.plugins[name]
	if !ok {
		return fmt.Errorf("plugin.PluginManager.EnablePlugin: plugin %q not found", name)
	}
	if lp.State.Enabled {
		return fmt.Errorf("plugin.PluginManager.EnablePlugin: plugin %q is already enabled", name)
	}

	// Launch the plugin runtime based on type.
	var startErr error
	switch lp.Manifest.Type {
	case types.PluginTypeMCP:
		startErr = pm.startMCPRuntime(ctx, lp)
	case types.PluginTypeService:
		startErr = pm.startServiceRuntime(ctx, lp)
	}
	if startErr != nil {
		lp.State.Status = "error"
		lp.State.Error = startErr.Error()
		pm.publishEvent(types.EventPluginError, name, map[string]any{
			"name":  name,
			"error": startErr.Error(),
		})
		return fmt.Errorf("plugin.PluginManager.EnablePlugin: start runtime for %q: %w", name, startErr)
	}

	lp.State.Enabled = true
	lp.State.Status = "enabled"
	lp.State.Error = ""

	// Persist enabled state so the plugin auto-enables on restart.
	if pm.pluginRepo != nil {
		if err := pm.pluginRepo.SavePlugin(ctx, &lp.State); err != nil {
			pm.logger.Warn("failed to persist plugin enabled state",
				"plugin", name, "error", err)
		}
	}

	// Auto-create resources declared in manifest (cron jobs, etc.).
	pm.createPluginResources(ctx, lp)

	// If this is a secret_provider plugin, create and register a secret adapter.
	if lp.Manifest.Integration == types.PluginIntegrationSecretProvider && pm.secretBridge != nil && lp.mcpClient != nil {
		adapter := NewPluginSecretAdapter(lp.Manifest.Name, lp.mcpClient, pm.logger)
		pm.secretBridge.RegisterAdapter(adapter)
		pm.logger.Info("registered plugin secret provider", "plugin", name)
	}

	// If this is a guard plugin, register its evaluate tool with the guard middleware.
	if lp.Manifest.Integration == types.PluginIntegrationGuard && pm.guardBridge != nil {
		for _, t := range lp.Manifest.Tools {
			if t.Name == "evaluate" {
				pluginToolName := pm.qualifiedToolName(lp.Manifest.Name, t.Name)
				pm.guardBridge.RegisterGuard(lp.Manifest.Name, pluginToolName, pm.registry.Dispatch, 5*time.Minute)
				pm.logger.Info("registered plugin guard evaluator", "plugin", name, "tool", pluginToolName)
				break
			}
		}
	}

	// If this is an audit plugin, register its write_audit_event tool with the audit sink.
	if lp.Manifest.Integration == types.PluginIntegrationAudit && pm.auditBridge != nil {
		for _, t := range lp.Manifest.Tools {
			if t.Name == "write_audit_event" {
				pluginToolName := pm.qualifiedToolName(lp.Manifest.Name, t.Name)
				pm.auditBridge.RegisterWriter(lp.Manifest.Name, func(ctx context.Context, toolName string, params json.RawMessage) (*types.ToolResult, error) {
					return pm.registry.Dispatch(ctx, pluginToolName, params)
				})
				pm.logger.Info("registered plugin audit writer", "plugin", name, "tool", pluginToolName)
				break
			}
		}
	}

	pm.logger.Info("plugin enabled", "plugin", name)
	pm.publishEvent(types.EventPluginEnabled, name, map[string]any{
		"name": name,
	})

	return nil
}

// DisablePlugin transitions an enabled plugin to "disabled" state.
// For MCP-type plugins, this stops the subprocess. Tool registrations
// are replaced back to placeholders.
func (pm *PluginManager) DisablePlugin(ctx context.Context, name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	lp, ok := pm.plugins[name]
	if !ok {
		return fmt.Errorf("plugin.PluginManager.DisablePlugin: plugin %q not found", name)
	}
	if !lp.State.Enabled {
		return fmt.Errorf("plugin.PluginManager.DisablePlugin: plugin %q is already disabled", name)
	}

	// Unregister secret adapter if this is a secret_provider plugin.
	// Block disable if this provider is active and still holds secrets.
	if lp.Manifest.Integration == types.PluginIntegrationSecretProvider && pm.secretBridge != nil {
		if pm.secretBridge.IsActive != nil && pm.secretBridge.IsActive(lp.Manifest.Name) {
			if pm.secretBridge.HasSecrets != nil {
				hasSecrets, err := pm.secretBridge.HasSecrets(ctx, lp.Manifest.Name)
				if err == nil && hasSecrets {
					return fmt.Errorf("plugin.PluginManager.DisablePlugin: cannot disable plugin %q: active secret provider with stored secrets", name)
				}
			}
		}
		pm.secretBridge.UnregisterAdapter(lp.Manifest.Name)
		pm.logger.Info("unregistered plugin secret provider", "plugin", name)
	}

	// Unregister guard evaluator if this is a guard plugin.
	if lp.Manifest.Integration == types.PluginIntegrationGuard && pm.guardBridge != nil {
		pm.guardBridge.UnregisterGuard(lp.Manifest.Name)
		pm.logger.Info("unregistered plugin guard evaluator", "plugin", name)
	}

	// Unregister audit writer if this is an audit plugin.
	if lp.Manifest.Integration == types.PluginIntegrationAudit && pm.auditBridge != nil {
		pm.auditBridge.UnregisterWriter(lp.Manifest.Name)
		pm.logger.Info("unregistered plugin audit writer", "plugin", name)
	}

	// Stop MCP subprocess if running.
	pm.stopMCPRuntime(lp)

	// Re-register placeholder handlers for manifest tools.
	pm.reregisterPlaceholders(lp)

	lp.State.Enabled = false
	lp.State.Status = "disabled"

	// Persist disabled state so the plugin stays disabled on restart.
	if pm.pluginRepo != nil {
		if err := pm.pluginRepo.SavePlugin(ctx, &lp.State); err != nil {
			pm.logger.Warn("failed to persist plugin disabled state",
				"plugin", name, "error", err)
		}
	}

	pm.logger.Info("plugin disabled", "plugin", name)
	pm.publishEvent(types.EventPluginDisabled, name, map[string]any{
		"name": name,
	})

	return nil
}

// ListPlugins returns the current state of all loaded plugins.
// The returned slice is a snapshot safe for concurrent reading.
func (pm *PluginManager) ListPlugins() []types.PluginState {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	states := make([]types.PluginState, 0, len(pm.plugins))
	for _, lp := range pm.plugins {
		states = append(states, lp.State)
	}
	sort.Slice(states, func(i, j int) bool {
		return states[i].Name < states[j].Name
	})
	return states
}

// GetPlugin returns the state of a single plugin by name.
// Returns an error if the plugin is not loaded.
func (pm *PluginManager) GetPlugin(name string) (*types.PluginState, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	lp, ok := pm.plugins[name]
	if !ok {
		return nil, fmt.Errorf("plugin.PluginManager.GetPlugin: plugin %q not found", name)
	}
	state := lp.State // copy
	return &state, nil
}

// GetPluginInfo returns the state and manifest of a loaded plugin.
func (pm *PluginManager) GetPluginInfo(name string) (*types.PluginState, *types.PluginManifest, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	lp, ok := pm.plugins[name]
	if !ok {
		return nil, nil, fmt.Errorf("plugin.PluginManager.GetPluginInfo: plugin %q not found", name)
	}
	state := lp.State
	manifest := lp.Manifest
	return &state, &manifest, nil
}

// StopAll stops all running MCP subprocesses. Called during server shutdown.
func (pm *PluginManager) StopAll() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for name, lp := range pm.plugins {
		if lp.subprocess != nil {
			pm.logger.Info("stopping plugin subprocess on shutdown", "plugin", name)
			pm.stopMCPRuntime(lp)
		}
	}
}

// InstallRegistry returns the plugin install registry, or nil if not set.
func (pm *PluginManager) InstallRegistry() *Registry {
	return pm.installRegistry
}

// RegisterInstall records an installation source so the plugin can be reloaded
// after a server restart. This is a no-op when the install registry is nil.
func (pm *PluginManager) RegisterInstall(name, source, manifestPath, manifestURL string) {
	if pm.installRegistry == nil {
		return
	}
	rec := PluginRecord{
		Name:         name,
		Source:       source,
		ManifestPath: manifestPath,
		ManifestURL:  manifestURL,
	}
	if err := pm.installRegistry.Add(rec); err != nil {
		pm.logger.Warn("failed to persist plugin install",
			"plugin", name,
			"error", err,
		)
	}
}

// DeregisterInstall removes a plugin's installation record from the persistent
// registry. This is a no-op when the install registry is nil.
func (pm *PluginManager) DeregisterInstall(name string) {
	if pm.installRegistry == nil {
		return
	}
	if err := pm.installRegistry.Remove(name); err != nil {
		pm.logger.Warn("failed to remove plugin from registry",
			"plugin", name,
			"error", err,
		)
	}
}

// GetRegistryRecord returns the install registry record for a plugin, or nil
// if the plugin is not in the registry. Used to detect installed-but-failed-to-load
// plugins (e.g. broken after a Hyperax upgrade).
func (pm *PluginManager) GetRegistryRecord(name string) *PluginRecord {
	if pm.installRegistry == nil {
		return nil
	}
	return pm.installRegistry.Get(name)
}

// CleanupBrokenPlugin removes the disk directory and registry entry for a plugin
// that failed to load (not in pm.plugins). This prepares the slot for a fresh
// install/upgrade without requiring UnloadPlugin (which needs the plugin loaded).
func (pm *PluginManager) CleanupBrokenPlugin(ctx context.Context, name string) {
	// Remove the plugin directory from disk.
	if pm.pluginDir != "" {
		pluginPath := filepath.Join(pm.pluginDir, name)
		if info, statErr := os.Stat(pluginPath); statErr == nil && info.IsDir() {
			if rmErr := os.RemoveAll(pluginPath); rmErr != nil {
				pm.logger.Warn("failed to remove broken plugin directory",
					"plugin", name, "path", pluginPath, "error", rmErr)
			} else {
				pm.logger.Info("removed broken plugin directory", "plugin", name, "path", pluginPath)
			}
		}
	}

	// Remove from install registry.
	pm.DeregisterInstall(name)

	// Remove from plugin DB if present.
	if pm.pluginRepo != nil {
		if err := pm.pluginRepo.DeletePlugin(ctx, name); err != nil {
			pm.logger.Debug("failed to delete broken plugin from DB",
				"plugin", name, "error", err)
		}
	}
}

// LoadFromRegistry iterates the persistent install registry and loads any
// plugins that were not already discovered by Discover(). This allows plugins
// installed via local path or remote URL to survive server restarts.
func (pm *PluginManager) LoadFromRegistry(ctx context.Context) error {
	if pm.installRegistry == nil {
		return nil
	}

	for _, rec := range pm.installRegistry.List() {
		// Skip plugins already loaded by Discover().
		pm.mu.RLock()
		_, exists := pm.plugins[rec.Name]
		pm.mu.RUnlock()
		if exists {
			continue
		}

		if rec.ManifestPath != "" {
			manifest, err := ParseManifest(filepath.Join(rec.ManifestPath, manifestFileName))
			if err != nil {
				pm.logger.Warn("failed to load registered plugin from path",
					"plugin", rec.Name,
					"path", rec.ManifestPath,
					"error", err,
				)
				continue
			}
			if err := pm.LoadPlugin(ctx, *manifest); err != nil {
				pm.logger.Warn("failed to reload registered plugin",
					"plugin", rec.Name,
					"error", err,
				)
			}
		} else if rec.ManifestURL != "" {
			// Remote plugins would need to be re-downloaded. For now, skip
			// any that are not cached locally.
			pm.logger.Warn("remote plugin not cached locally, skipping",
				"plugin", rec.Name,
				"url", rec.ManifestURL,
			)
		}
	}

	return nil
}

// resolveEntrypoint returns an absolute or PATH-resolved entrypoint for a plugin.
// If the entrypoint is already absolute or found in PATH, it's returned as-is.
// Otherwise, the method checks the plugin's manifest directory (from the install
// registry) and the pluginDir/{name}/ subdirectory.
func (pm *PluginManager) resolveEntrypoint(manifest *types.PluginManifest) string {
	ep := manifest.Entrypoint
	if ep == "" {
		return ""
	}

	// Already absolute — use as-is.
	if filepath.IsAbs(ep) {
		return ep
	}

	// Found in PATH — use as-is.
	if _, err := exec.LookPath(ep); err == nil {
		return ep
	}

	// Try manifest directory from registry, walking up to 3 levels.
	if pm.installRegistry != nil {
		for _, rec := range pm.installRegistry.List() {
			if rec.Name == manifest.Name && rec.ManifestPath != "" {
				dir := rec.ManifestPath
				for i := 0; i < 3; i++ {
					candidate := filepath.Join(dir, ep)
					if _, err := os.Stat(candidate); err == nil {
						pm.logger.Info("resolved entrypoint from manifest ancestor",
							"plugin", manifest.Name, "path", candidate, "level", i)
						return candidate
					}
					parent := filepath.Dir(dir)
					if parent == dir {
						break // reached filesystem root
					}
					dir = parent
				}
			}
		}
	}

	// Try pluginDir/{name}/.
	if pm.pluginDir != "" {
		candidate := filepath.Join(pm.pluginDir, manifest.Name, ep)
		if _, err := os.Stat(candidate); err == nil {
			pm.logger.Info("resolved entrypoint from plugin dir",
				"plugin", manifest.Name, "path", candidate)
			return candidate
		}
	}

	// Try working directory.
	if wd, err := os.Getwd(); err == nil {
		candidate := filepath.Join(wd, ep)
		if _, statErr := os.Stat(candidate); statErr == nil {
			pm.logger.Info("resolved entrypoint from working directory",
				"plugin", manifest.Name, "path", candidate)
			return candidate
		}
	}

	// Last resort: return original (will fail with a clear error).
	return ep
}

// startMCPRuntime launches the plugin subprocess, initializes the MCP client,
// discovers tools, and federates them. Must be called with pm.mu held.
func (pm *PluginManager) startMCPRuntime(ctx context.Context, lp *LoadedPlugin) error {
	pluginName := lp.Manifest.Name

	// Resolve the entrypoint to a findable path.
	resolved := pm.resolveEntrypoint(&lp.Manifest)
	if resolved != lp.Manifest.Entrypoint {
		lp.Manifest.Entrypoint = resolved
	}

	// Use a background context for the subprocess lifecycle — the request
	// context (ctx) is short-lived and would kill the subprocess when the
	// enable_plugin tool call completes.
	spCtx := context.Background()

	// Launch subprocess with config/secret resolver.
	sp, err := StartSubprocess(spCtx, lp.Manifest, pm.configResolver, pm.logger)
	if err != nil {
		return fmt.Errorf("plugin.PluginManager.startMCPRuntime: launch subprocess: %w", err)
	}

	// Set crash handler.
	sp.onCrash = func(name string, crashErr error) {
		pm.publishEvent(types.EventPluginError, name, map[string]any{
			"name":  name,
			"error": fmt.Sprintf("subprocess crashed: %v", crashErr),
		})
	}

	// Create MCP client over stdio.
	client := NewMCPClient(sp.Stdout(), sp.Stdin(), pm.logger)

	// Wire event bridge for notifications.
	if pm.eventBridge != nil {
		client.NotificationHandler = pm.eventBridge.NotificationHandler(pluginName)
	}

	// Start reader goroutine with a long-lived context.
	clientCtx, clientCancel := context.WithCancel(spCtx)
	go client.Run(clientCtx)

	// Initialize MCP connection with timeout.
	initCtx, initCancel := context.WithTimeout(ctx, mcpInitTimeout)
	defer initCancel()

	_, err = client.Initialize(initCtx)
	if err != nil {
		clientCancel()
		_ = sp.Stop()
		return fmt.Errorf("plugin.PluginManager.startMCPRuntime: MCP initialize: %w", err)
	}

	// Discover tools from the plugin.
	tools, err := client.ListTools(initCtx)
	if err != nil {
		clientCancel()
		_ = sp.Stop()
		return fmt.Errorf("plugin.PluginManager.startMCPRuntime: MCP tools/list: %w", err)
	}

	pm.logger.Info("MCP plugin connected",
		"plugin", pluginName,
		"tools_discovered", len(tools),
	)

	// Deregister existing placeholder tools.
	for _, toolName := range lp.registeredTools {
		pm.registry.Unregister(toolName)
	}

	// Federate real proxy tools with integration-aware clearance.
	clearance := clearanceForIntegration(lp.Manifest.Integration)
	lp.registeredTools = FederateTools(
		pm.registry,
		pluginName,
		tools,
		client,
		clearance,
		pm.logger,
	)

	// Update tool count from actual discovery (may differ from manifest).
	lp.State.ToolCount = len(tools)

	// Store references.
	lp.subprocess = sp
	lp.mcpClient = client
	lp.mcpCancel = clientCancel

	return nil
}

// startServiceRuntime launches a service-type plugin. Unlike MCP plugins, service
// plugins don't require the MCP handshake (initialize, tools/list). The subprocess
// is launched and tools are registered from the manifest with proxy handlers that
// forward calls via JSON-RPC. Events come back as JSON-RPC notifications (same as MCP).
//
// This is the preferred model for channel integrations (Discord, Slack, etc.) where
// the plugin maintains persistent external connections and doesn't need tool discovery.
func (pm *PluginManager) startServiceRuntime(_ context.Context, lp *LoadedPlugin) error {
	pluginName := lp.Manifest.Name

	// Resolve the entrypoint to a findable path.
	resolved := pm.resolveEntrypoint(&lp.Manifest)
	if resolved != lp.Manifest.Entrypoint {
		lp.Manifest.Entrypoint = resolved
	}

	// Use a background context for the subprocess lifecycle — the request
	// context (ctx) is short-lived and would kill the subprocess when the
	// enable_plugin tool call completes.
	spCtx := context.Background()

	// Launch subprocess with config/secret resolver.
	sp, err := StartSubprocess(spCtx, lp.Manifest, pm.configResolver, pm.logger)
	if err != nil {
		return fmt.Errorf("plugin.PluginManager.startServiceRuntime: launch subprocess: %w", err)
	}

	// Set crash handler.
	sp.onCrash = func(name string, crashErr error) {
		pm.publishEvent(types.EventPluginError, name, map[string]any{
			"name":  name,
			"error": fmt.Sprintf("service subprocess crashed: %v", crashErr),
		})
	}

	// Create MCP client for JSON-RPC communication (tool calls + notifications)
	// but skip the MCP initialize handshake — service plugins don't need it.
	client := NewMCPClient(sp.Stdout(), sp.Stdin(), pm.logger)

	// Wire event bridge for notifications.
	if pm.eventBridge != nil {
		client.NotificationHandler = pm.eventBridge.NotificationHandler(pluginName)
	}

	// Start reader goroutine with a long-lived context.
	clientCtx, clientCancel := context.WithCancel(spCtx)
	go client.Run(clientCtx)

	pm.logger.Info("service plugin connected (no MCP handshake)",
		"plugin", pluginName,
		"manifest_tools", len(lp.Manifest.Tools),
	)

	// Deregister existing placeholder tools.
	for _, toolName := range lp.registeredTools {
		pm.registry.Unregister(toolName)
	}

	// Federate proxy tools from manifest definitions (no discovery needed).
	// Convert manifest ToolDefs to the format FederateTools expects.
	clearance := clearanceForIntegration(lp.Manifest.Integration)
	lp.registeredTools = FederateToolsFromManifest(
		pm.registry,
		pluginName,
		lp.Manifest.Tools,
		client,
		clearance,
		pm.logger,
	)

	// Store references.
	lp.subprocess = sp
	lp.mcpClient = client
	lp.mcpCancel = clientCancel

	return nil
}

// stopMCPRuntime stops the subprocess and cleans up MCP client resources.
// Must be called with pm.mu held.
func (pm *PluginManager) stopMCPRuntime(lp *LoadedPlugin) {
	if lp.mcpCancel != nil {
		lp.mcpCancel()
		lp.mcpCancel = nil
	}
	if lp.mcpClient != nil {
		lp.mcpClient.Close()
		lp.mcpClient = nil
	}
	if lp.subprocess != nil {
		if err := lp.subprocess.Stop(); err != nil {
			pm.logger.Warn("error stopping plugin subprocess",
				"plugin", lp.Manifest.Name,
				"error", err.Error(),
			)
		}
		lp.subprocess = nil
	}
}

// reregisterPlaceholders replaces federated tools with placeholders.
// Must be called with pm.mu held.
func (pm *PluginManager) reregisterPlaceholders(lp *LoadedPlugin) {
	// Remove current tools.
	for _, toolName := range lp.registeredTools {
		pm.registry.Unregister(toolName)
	}

	// Re-register from manifest with placeholder handlers.
	lp.registeredTools = nil
	for _, toolDef := range lp.Manifest.Tools {
		toolName := pm.qualifiedToolName(lp.Manifest.Name, toolDef.Name)
		if pm.registry.HasTool(toolName) {
			continue
		}

		inputSchema := buildInputSchema(toolDef)
		handler := pm.makePlaceholderHandler(lp.Manifest.Name, toolDef.Name)
		pm.registry.Register(toolName, toolDef.Description, inputSchema, handler)
		lp.registeredTools = append(lp.registeredTools, toolName)
	}
}

// qualifiedToolName returns the MCP tool name for a plugin tool.
// Format: "plugin_{pluginName}_{toolName}"
func (pm *PluginManager) qualifiedToolName(pluginName, toolName string) string {
	return fmt.Sprintf("plugin_%s_%s", pluginName, toolName)
}

// makePlaceholderHandler creates an MCP tool handler that returns a
// "not yet connected" message. This allows tools to be discoverable via
// tools/list before the actual runtime loader is wired up.
func (pm *PluginManager) makePlaceholderHandler(pluginName, toolName string) mcp.ToolHandler {
	return func(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
		msg := fmt.Sprintf("Plugin tool %q from plugin %q is registered but not yet connected. "+
			"The plugin runtime loader for this plugin type has not been initialized.",
			toolName, pluginName)
		return types.NewErrorResult(msg), nil
	}
}

// buildInputSchema generates a JSON Schema object from a ToolDef's parameters.
func buildInputSchema(tool types.ToolDef) json.RawMessage {
	properties := make(map[string]any)
	var required []string

	for _, p := range tool.Parameters {
		prop := map[string]any{
			"type":        p.Type,
			"description": p.Description,
		}
		if p.Default != nil {
			prop["default"] = p.Default
		}
		properties[p.Name] = prop

		if p.Required {
			required = append(required, p.Name)
		}
	}

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	} else {
		schema["required"] = []string{}
	}

	data, _ := json.Marshal(schema)
	return data
}

// SetConfigResolver sets the config/secret resolver used for subprocess env injection.
func (pm *PluginManager) SetConfigResolver(resolver *PluginConfigResolver) {
	pm.configResolver = resolver
}

// SetCronRepo sets the cron repo used for auto-creating plugin resources.
func (pm *PluginManager) SetCronRepo(repo CronRepoInterface) {
	pm.cronRepo = repo
}

// EventBridge returns the event bridge instance, or nil if no EventBus was configured.
func (pm *PluginManager) EventBridge() *EventBridge {
	return pm.eventBridge
}

// GetManifest returns the manifest for a loaded plugin.
func (pm *PluginManager) GetManifest(name string) (*types.PluginManifest, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	lp, ok := pm.plugins[name]
	if !ok {
		return nil, fmt.Errorf("plugin.PluginManager.GetManifest: plugin %q not found", name)
	}
	m := lp.Manifest
	return &m, nil
}

// NotifyConfigChanged sends a notifications/configChanged notification to
// a running plugin's MCP client. This is called when a dynamic variable is
// updated via configure_plugin, allowing the plugin to react without restart.
// Returns nil if the plugin is not running or has no MCP client (non-error).
func (pm *PluginManager) NotifyConfigChanged(pluginName, variable, value string) error {
	pm.mu.RLock()
	lp, ok := pm.plugins[pluginName]
	if !ok {
		pm.mu.RUnlock()
		return fmt.Errorf("plugin.PluginManager.NotifyConfigChanged: plugin %q not found", pluginName)
	}
	client := lp.mcpClient
	enabled := lp.State.Enabled
	pm.mu.RUnlock()

	if !enabled || client == nil {
		// Plugin not running — config will take effect on next enable.
		pm.logger.Debug("skipping config notification (plugin not running)",
			"plugin", pluginName, "variable", variable)
		return nil
	}

	payload := map[string]string{
		"variable": variable,
		"value":    value,
	}

	if err := client.SendNotification("notifications/configChanged", payload); err != nil {
		return fmt.Errorf("plugin.PluginManager.NotifyConfigChanged: send to %q: %w", pluginName, err)
	}

	pm.logger.Info("config change notification sent to plugin",
		"plugin", pluginName, "variable", variable)
	return nil
}

// ConfigKeySeeder seeds config key metadata for plugin variables.
// This is a subset of repo.ConfigRepo.
type ConfigKeySeeder interface {
	UpsertKeyMeta(ctx context.Context, meta *types.ConfigKeyMeta) error
}

// SetConfigKeySeeder sets the config key seeder used for registering plugin
// variable metadata so they appear in list_config_keys.
func (pm *PluginManager) SetConfigKeySeeder(seeder ConfigKeySeeder) {
	pm.configKeySeeder = seeder
}

// SetSecretBridge sets the bridge used for registering/unregistering plugin
// secret provider adapters with the central secrets registry.
func (pm *PluginManager) SetSecretBridge(bridge *SecretRegistryBridge) {
	pm.secretBridge = bridge
}

// SetGuardBridge sets the bridge used for registering/unregistering plugin
// guard evaluators with the guard middleware.
func (pm *PluginManager) SetGuardBridge(bridge *GuardBridge) {
	pm.guardBridge = bridge
}

// SetAuditBridge sets the bridge used for registering/unregistering plugin
// audit writers with the audit PluginAuditSink.
func (pm *PluginManager) SetAuditBridge(bridge *AuditBridge) {
	pm.auditBridge = bridge
}

// SetPluginRepo sets the persistence repo for saving plugin state across restarts.
func (pm *PluginManager) SetPluginRepo(r repo.PluginRepo) {
	pm.pluginRepo = r
}

// RestoreEnabledPlugins reads persisted plugin states from the PluginRepo and
// auto-enables any plugins that were enabled before the last shutdown. This must
// be called after Discover() + LoadFromRegistry() so that the plugins are loaded
// into memory first.
func (pm *PluginManager) RestoreEnabledPlugins(ctx context.Context) {
	if pm.pluginRepo == nil {
		return
	}

	persisted, err := pm.pluginRepo.ListPlugins(ctx)
	if err != nil {
		pm.logger.Warn("failed to list persisted plugin states", "error", err)
		return
	}

	for _, saved := range persisted {
		if !saved.Enabled {
			continue
		}

		// Check if the plugin is loaded but not yet enabled.
		pm.mu.RLock()
		lp, exists := pm.plugins[saved.Name]
		pm.mu.RUnlock()

		if !exists {
			pm.logger.Warn("persisted plugin not loaded, skipping auto-enable",
				"plugin", saved.Name)
			continue
		}

		if lp.State.Enabled {
			continue // already enabled
		}

		pm.logger.Info("restoring enabled plugin", "plugin", saved.Name)
		if err := pm.EnablePlugin(ctx, saved.Name); err != nil {
			pm.logger.Warn("failed to restore enabled plugin",
				"plugin", saved.Name, "error", err)
		}
	}
}

// seedVariableConfigKeys registers ConfigKeyMeta entries for each plugin
// variable so they're discoverable via list_config_keys.
func (pm *PluginManager) seedVariableConfigKeys(ctx context.Context, manifest types.PluginManifest) {
	if pm.configKeySeeder == nil {
		return
	}
	for _, v := range manifest.Variables {
		key := fmt.Sprintf("plugin.%s.var.%s", manifest.Name, v.Name)
		desc := v.Description
		if desc == "" {
			desc = fmt.Sprintf("Plugin %s variable: %s", manifest.Name, v.Name)
		}
		valType := "string"
		switch v.Type {
		case types.PluginVarInt:
			valType = "int"
		case types.PluginVarFloat:
			valType = "float"
		case types.PluginVarBool:
			valType = "bool"
		}
		defaultVal := ""
		if v.Default != nil {
			defaultVal = fmt.Sprintf("%v", v.Default)
		}
		meta := &types.ConfigKeyMeta{
			Key:         key,
			ScopeType:   "global",
			ValueType:   valType,
			DefaultVal:  defaultVal,
			Critical:    v.Secret,
			Description: desc,
		}
		if err := pm.configKeySeeder.UpsertKeyMeta(ctx, meta); err != nil {
			pm.logger.Warn("failed to seed config key meta for plugin variable",
				"plugin", manifest.Name,
				"variable", v.Name,
				"error", err,
			)
		}
	}
}

// createPluginResources auto-creates resources declared in the manifest
// (cron jobs, event handlers, etc.) and records them in the install registry.
func (pm *PluginManager) createPluginResources(ctx context.Context, lp *LoadedPlugin) {
	if len(lp.Manifest.Resources) == 0 {
		return
	}
	var created []types.CreatedResource
	for _, res := range lp.Manifest.Resources {
		switch res.Type {
		case "cron_job":
			if pm.cronRepo == nil {
				pm.logger.Warn("cron repo not available, skipping cron_job resource",
					"plugin", lp.Manifest.Name, "resource", res.Name)
				continue
			}
			schedule, _ := res.Config["schedule"].(string)
			jobType, _ := res.Config["job_type"].(string)
			if jobType == "" {
				jobType = "tool"
			}
			payloadBytes, _ := json.Marshal(res.Config["payload"])
			cronName := fmt.Sprintf("plugin:%s:%s", lp.Manifest.Name, res.Name)
			job := &repo.CronJob{
				Name:     cronName,
				Schedule: schedule,
				JobType:  jobType,
				Payload:  payloadBytes,
				Enabled:  true,
			}
			id, err := pm.cronRepo.CreateJob(ctx, job)
			if err != nil {
				pm.logger.Warn("failed to create cron job resource",
					"plugin", lp.Manifest.Name, "resource", res.Name, "error", err)
				continue
			}
			created = append(created, types.CreatedResource{
				Type: "cron_job",
				ID:   id,
				Name: cronName,
			})
			pm.logger.Info("created plugin cron job",
				"plugin", lp.Manifest.Name, "cron_id", id, "name", cronName)
		default:
			pm.logger.Warn("unsupported resource type in plugin manifest",
				"plugin", lp.Manifest.Name, "type", res.Type)
		}
	}
	// Persist created resource IDs in the install registry.
	if pm.installRegistry != nil && len(created) > 0 {
		pm.installRegistry.SetCreatedResources(lp.Manifest.Name, created)
	}
}

// cleanupPluginResources removes any auto-created resources for a plugin.
func (pm *PluginManager) cleanupPluginResources(ctx context.Context, lp *LoadedPlugin) {
	if pm.installRegistry == nil {
		return
	}
	resources := pm.installRegistry.GetCreatedResources(lp.Manifest.Name)
	for _, res := range resources {
		switch res.Type {
		case "cron_job":
			if pm.cronRepo == nil {
				continue
			}
			if err := pm.cronRepo.DeleteJob(ctx, res.ID); err != nil {
				pm.logger.Warn("failed to delete plugin cron job",
					"plugin", lp.Manifest.Name, "cron_id", res.ID, "error", err)
			} else {
				pm.logger.Info("deleted plugin cron job",
					"plugin", lp.Manifest.Name, "cron_id", res.ID)
			}
		default:
			pm.logger.Warn("unknown resource type during cleanup",
				"plugin", lp.Manifest.Name, "type", res.Type)
		}
	}
	pm.installRegistry.ClearCreatedResources(lp.Manifest.Name)
}

// clearanceForIntegration returns the ABAC clearance level for a plugin
// based on its integration category.
func clearanceForIntegration(integration types.PluginIntegration) int {
	switch integration {
	case types.PluginIntegrationTooling:
		return 1 // Operator — agents can call these tools
	case types.PluginIntegrationChannel:
		return 2 // Admin — internal channel routing, not agent-visible
	case types.PluginIntegrationSensor:
		return 2 // Admin — event publishing only
	case types.PluginIntegrationSecretProvider:
		return 3 // ChiefOfStaff — secrets are highest security
	case types.PluginIntegrationGuard:
		return 2 // Admin — guard evaluation tools
	case types.PluginIntegrationAudit:
		return 2 // Admin — audit stream tools
	default:
		return 1 // Operator fallback
	}
}

// publishEvent emits a plugin event on the EventBus if one is configured.
func (pm *PluginManager) publishEvent(eventType types.EventType, pluginName string, payload map[string]any) {
	if pm.bus == nil {
		return
	}
	event := nervous.NewEvent(eventType, "plugin-manager", pluginName, payload)
	pm.bus.Publish(event)
}
