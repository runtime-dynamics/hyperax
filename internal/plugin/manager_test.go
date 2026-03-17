package plugin

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

// newTestManager creates a PluginManager with a real ToolRegistry and EventBus
// pointed at the given plugin directory. Suitable for unit tests.
func newTestManager(t *testing.T, pluginDir string) *PluginManager {
	t.Helper()
	registry := mcp.NewToolRegistry()
	bus := nervous.NewEventBus(16)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	return NewPluginManager(registry, bus, logger, pluginDir, "dev")
}

// sampleManifest returns a minimal valid PluginManifest for testing.
func sampleManifest() types.PluginManifest {
	return types.PluginManifest{
		Name:          "test-plugin",
		Version:       "1.0.0",
		Type:          types.PluginTypeWasm,
		Description:   "A test plugin",
		Author:        "test@example.com",
		MinHyperaxVer: "1.0.0",
		APIVersion:    "1.0.0",
		Permissions:   []string{"workspace:read", "tools:register"},
		Entrypoint:    "./test.wasm",
		Tools: []types.ToolDef{
			{
				Name:        "greet",
				Description: "Greets the user",
				Parameters: []types.ParameterDef{
					{
						Name:        "name",
						Type:        "string",
						Required:    true,
						Description: "Name to greet",
					},
				},
			},
			{
				Name:        "farewell",
				Description: "Says goodbye",
				Parameters:  []types.ParameterDef{},
			},
		},
	}
}

// writeSampleManifest writes a hyperax-plugin.yaml to the given directory.
func writeSampleManifest(t *testing.T, dir string) {
	t.Helper()
	content := `
name: "discovered-plugin"
version: "2.0.0"
type: "http"
description: "Discovered via directory scan"
author: "scan@example.com"
min_hyperax_version: "1.0.0"
api_version: "1.0.0"
permissions:
  - tools:register
entrypoint: "http://localhost:8080"
tools:
  - name: "scan_tool"
    description: "Discovered tool"
    parameters:
      - name: "query"
        type: "string"
        required: true
        description: "Search query"
`
	manifestPath := filepath.Join(dir, "hyperax-plugin.yaml")
	if err := os.WriteFile(manifestPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadPlugin_Success(t *testing.T) {
	pm := newTestManager(t, "")
	ctx := context.Background()

	if err := pm.LoadPlugin(ctx, sampleManifest()); err != nil {
		t.Fatalf("LoadPlugin() error: %v", err)
	}

	// Verify plugin state.
	states := pm.ListPlugins()
	if len(states) != 1 {
		t.Fatalf("ListPlugins() count = %d, want 1", len(states))
	}
	if states[0].Name != "test-plugin" {
		t.Errorf("Name = %q, want %q", states[0].Name, "test-plugin")
	}
	if states[0].Status != "loaded" {
		t.Errorf("Status = %q, want %q", states[0].Status, "loaded")
	}
	if states[0].Enabled {
		t.Error("Enabled = true, want false after initial load")
	}
	if states[0].ToolCount != 2 {
		t.Errorf("ToolCount = %d, want 2", states[0].ToolCount)
	}
	if states[0].HealthStatus != "unknown" {
		t.Errorf("HealthStatus = %q, want %q", states[0].HealthStatus, "unknown")
	}

	// Verify tools are registered in the MCP registry.
	if !pm.registry.HasTool("plugin_test-plugin_greet") {
		t.Error("expected tool 'plugin_test-plugin_greet' to be registered")
	}
	if !pm.registry.HasTool("plugin_test-plugin_farewell") {
		t.Error("expected tool 'plugin_test-plugin_farewell' to be registered")
	}
}

func TestLoadPlugin_DuplicateRejected(t *testing.T) {
	pm := newTestManager(t, "")
	ctx := context.Background()

	if err := pm.LoadPlugin(ctx, sampleManifest()); err != nil {
		t.Fatalf("first LoadPlugin() error: %v", err)
	}

	err := pm.LoadPlugin(ctx, sampleManifest())
	if err == nil {
		t.Fatal("LoadPlugin() should reject duplicate plugin")
	}
}

func TestLoadPlugin_InvalidManifest(t *testing.T) {
	pm := newTestManager(t, "")
	ctx := context.Background()

	m := types.PluginManifest{Name: "", Version: "1.0.0", Type: types.PluginTypeWasm}
	err := pm.LoadPlugin(ctx, m)
	if err == nil {
		t.Fatal("LoadPlugin() should reject invalid manifest")
	}
}

func TestLoadPlugin_InvalidPermissions(t *testing.T) {
	pm := newTestManager(t, "")
	ctx := context.Background()

	m := sampleManifest()
	m.Permissions = []string{"workspace:read", "dangerous:perm"}

	err := pm.LoadPlugin(ctx, m)
	if err == nil {
		t.Fatal("LoadPlugin() should reject unknown permissions")
	}
}

func TestUnloadPlugin_Success(t *testing.T) {
	pm := newTestManager(t, "")
	ctx := context.Background()

	if err := pm.LoadPlugin(ctx, sampleManifest()); err != nil {
		t.Fatal(err)
	}

	if err := pm.UnloadPlugin(ctx, "test-plugin"); err != nil {
		t.Fatalf("UnloadPlugin() error: %v", err)
	}

	// Verify plugin is removed.
	states := pm.ListPlugins()
	if len(states) != 0 {
		t.Errorf("ListPlugins() count = %d, want 0 after unload", len(states))
	}

	// Verify tools are deregistered.
	if pm.registry.HasTool("plugin_test-plugin_greet") {
		t.Error("tool 'plugin_test-plugin_greet' should be deregistered after unload")
	}
	if pm.registry.HasTool("plugin_test-plugin_farewell") {
		t.Error("tool 'plugin_test-plugin_farewell' should be deregistered after unload")
	}
}

func TestUnloadPlugin_NotFound(t *testing.T) {
	pm := newTestManager(t, "")
	ctx := context.Background()

	err := pm.UnloadPlugin(ctx, "nonexistent")
	if err == nil {
		t.Fatal("UnloadPlugin() should return error for unknown plugin")
	}
}

func TestEnablePlugin_Success(t *testing.T) {
	pm := newTestManager(t, "")
	ctx := context.Background()

	if err := pm.LoadPlugin(ctx, sampleManifest()); err != nil {
		t.Fatal(err)
	}

	if err := pm.EnablePlugin(ctx, "test-plugin"); err != nil {
		t.Fatalf("EnablePlugin() error: %v", err)
	}

	state, err := pm.GetPlugin("test-plugin")
	if err != nil {
		t.Fatal(err)
	}
	if !state.Enabled {
		t.Error("Enabled = false, want true after enable")
	}
	if state.Status != "enabled" {
		t.Errorf("Status = %q, want %q", state.Status, "enabled")
	}
}

func TestEnablePlugin_AlreadyEnabled(t *testing.T) {
	pm := newTestManager(t, "")
	ctx := context.Background()

	if err := pm.LoadPlugin(ctx, sampleManifest()); err != nil {
		t.Fatal(err)
	}
	if err := pm.EnablePlugin(ctx, "test-plugin"); err != nil {
		t.Fatal(err)
	}

	err := pm.EnablePlugin(ctx, "test-plugin")
	if err == nil {
		t.Fatal("EnablePlugin() should return error when already enabled")
	}
}

func TestEnablePlugin_NotFound(t *testing.T) {
	pm := newTestManager(t, "")
	ctx := context.Background()

	err := pm.EnablePlugin(ctx, "nonexistent")
	if err == nil {
		t.Fatal("EnablePlugin() should return error for unknown plugin")
	}
}

func TestDisablePlugin_Success(t *testing.T) {
	pm := newTestManager(t, "")
	ctx := context.Background()

	if err := pm.LoadPlugin(ctx, sampleManifest()); err != nil {
		t.Fatal(err)
	}
	if err := pm.EnablePlugin(ctx, "test-plugin"); err != nil {
		t.Fatal(err)
	}

	if err := pm.DisablePlugin(ctx, "test-plugin"); err != nil {
		t.Fatalf("DisablePlugin() error: %v", err)
	}

	state, err := pm.GetPlugin("test-plugin")
	if err != nil {
		t.Fatal(err)
	}
	if state.Enabled {
		t.Error("Enabled = true, want false after disable")
	}
	if state.Status != "disabled" {
		t.Errorf("Status = %q, want %q", state.Status, "disabled")
	}
}

func TestDisablePlugin_AlreadyDisabled(t *testing.T) {
	pm := newTestManager(t, "")
	ctx := context.Background()

	if err := pm.LoadPlugin(ctx, sampleManifest()); err != nil {
		t.Fatal(err)
	}

	err := pm.DisablePlugin(ctx, "test-plugin")
	if err == nil {
		t.Fatal("DisablePlugin() should return error when already disabled")
	}
}

func TestGetPlugin_NotFound(t *testing.T) {
	pm := newTestManager(t, "")

	_, err := pm.GetPlugin("nonexistent")
	if err == nil {
		t.Fatal("GetPlugin() should return error for unknown plugin")
	}
}

func TestGetPlugin_ReturnsCopy(t *testing.T) {
	pm := newTestManager(t, "")
	ctx := context.Background()

	if err := pm.LoadPlugin(ctx, sampleManifest()); err != nil {
		t.Fatal(err)
	}

	state1, err := pm.GetPlugin("test-plugin")
	if err != nil {
		t.Fatalf("GetPlugin() error: %v", err)
	}
	state1.Status = "mutated"

	state2, err := pm.GetPlugin("test-plugin")
	if err != nil {
		t.Fatalf("GetPlugin() error: %v", err)
	}
	if state2.Status == "mutated" {
		t.Error("GetPlugin() should return a copy, not a reference to internal state")
	}
}

func TestDiscover_WithManifest(t *testing.T) {
	// Create a temp plugin directory with one plugin subdirectory.
	pluginDir := t.TempDir()
	pluginSubDir := filepath.Join(pluginDir, "discovered-plugin")
	if err := os.Mkdir(pluginSubDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeSampleManifest(t, pluginSubDir)

	pm := newTestManager(t, pluginDir)
	ctx := context.Background()

	if err := pm.Discover(ctx); err != nil {
		t.Fatalf("Discover() error: %v", err)
	}

	states := pm.ListPlugins()
	if len(states) != 1 {
		t.Fatalf("ListPlugins() count = %d, want 1 after discovery", len(states))
	}
	if states[0].Name != "discovered-plugin" {
		t.Errorf("Name = %q, want %q", states[0].Name, "discovered-plugin")
	}
	if states[0].Version != "2.0.0" {
		t.Errorf("Version = %q, want %q", states[0].Version, "2.0.0")
	}
}

func TestDiscover_EmptyDir(t *testing.T) {
	pluginDir := t.TempDir()
	pm := newTestManager(t, pluginDir)
	ctx := context.Background()

	if err := pm.Discover(ctx); err != nil {
		t.Fatalf("Discover() error on empty dir: %v", err)
	}

	states := pm.ListPlugins()
	if len(states) != 0 {
		t.Errorf("ListPlugins() count = %d, want 0", len(states))
	}
}

func TestDiscover_NonexistentDir(t *testing.T) {
	pm := newTestManager(t, "/tmp/nonexistent-plugin-dir-abc123")
	ctx := context.Background()

	// Should not error, just skip.
	if err := pm.Discover(ctx); err != nil {
		t.Fatalf("Discover() should not error for nonexistent dir: %v", err)
	}
}

func TestDiscover_EmptyPluginDir(t *testing.T) {
	pm := newTestManager(t, "")
	ctx := context.Background()

	if err := pm.Discover(ctx); err != nil {
		t.Fatalf("Discover() should not error with empty pluginDir: %v", err)
	}
}

func TestDiscover_SkipsNonDirectories(t *testing.T) {
	pluginDir := t.TempDir()

	// Create a regular file (not a directory) in the plugin dir.
	filePath := filepath.Join(pluginDir, "not-a-dir.yaml")
	if err := os.WriteFile(filePath, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	pm := newTestManager(t, pluginDir)
	ctx := context.Background()

	if err := pm.Discover(ctx); err != nil {
		t.Fatalf("Discover() error: %v", err)
	}

	states := pm.ListPlugins()
	if len(states) != 0 {
		t.Errorf("ListPlugins() count = %d, want 0", len(states))
	}
}

func TestDiscover_SkipsSubdirWithoutManifest(t *testing.T) {
	pluginDir := t.TempDir()

	// Create a subdirectory without a manifest.
	subDir := filepath.Join(pluginDir, "no-manifest")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	pm := newTestManager(t, pluginDir)
	ctx := context.Background()

	if err := pm.Discover(ctx); err != nil {
		t.Fatalf("Discover() error: %v", err)
	}

	states := pm.ListPlugins()
	if len(states) != 0 {
		t.Errorf("ListPlugins() count = %d, want 0", len(states))
	}
}

func TestPlaceholderToolReturnsError(t *testing.T) {
	pm := newTestManager(t, "")
	ctx := context.Background()

	if err := pm.LoadPlugin(ctx, sampleManifest()); err != nil {
		t.Fatal(err)
	}

	// Dispatch to the placeholder tool.
	result, err := pm.registry.Dispatch(ctx, "plugin_test-plugin_greet", []byte(`{"name":"world"}`))
	if err != nil {
		t.Fatalf("Dispatch() error: %v", err)
	}
	if result == nil {
		t.Fatal("Dispatch() returned nil result")
	}
	if !result.IsError {
		t.Error("placeholder tool should return IsError=true")
	}
}

func TestLoadPlugin_ToolRegistrationCountMatchesRegistry(t *testing.T) {
	pm := newTestManager(t, "")
	ctx := context.Background()

	initialCount := pm.registry.ToolCount()

	if err := pm.LoadPlugin(ctx, sampleManifest()); err != nil {
		t.Fatal(err)
	}

	newCount := pm.registry.ToolCount()
	if newCount != initialCount+2 {
		t.Errorf("ToolCount = %d, want %d (initial %d + 2 tools)", newCount, initialCount+2, initialCount)
	}

	// Unload and verify count goes back.
	if err := pm.UnloadPlugin(ctx, "test-plugin"); err != nil {
		t.Fatal(err)
	}

	finalCount := pm.registry.ToolCount()
	if finalCount != initialCount {
		t.Errorf("ToolCount after unload = %d, want %d", finalCount, initialCount)
	}
}

func TestUnloadPlugin_SecretProviderWithActiveSecrets_Blocked(t *testing.T) {
	pm := newTestManager(t, "")
	ctx := context.Background()

	m := sampleManifest()
	m.Name = "vault-plugin"
	m.Integration = types.PluginIntegrationSecretProvider

	if err := pm.LoadPlugin(ctx, m); err != nil {
		t.Fatal(err)
	}

	// Wire a secret bridge that reports the plugin is active and has secrets.
	pm.SetSecretBridge(&SecretRegistryBridge{
		RegisterAdapter:   func(_ *PluginSecretAdapter) {},
		UnregisterAdapter: func(_ string) {},
		IsActive: func(name string) bool {
			return name == "vault-plugin"
		},
		HasSecrets: func(_ context.Context, name string) (bool, error) {
			return true, nil
		},
	})

	err := pm.UnloadPlugin(ctx, "vault-plugin")
	if err == nil {
		t.Fatal("UnloadPlugin() should block removal of active secret provider with secrets")
	}
	if !strings.Contains(err.Error(), "active secret provider") {
		t.Errorf("unexpected error message: %v", err)
	}

	// Plugin should still be loaded.
	states := pm.ListPlugins()
	if len(states) != 1 {
		t.Fatalf("expected plugin to remain loaded, got %d plugins", len(states))
	}
}

func TestUnloadPlugin_SecretProviderNotActive_Allowed(t *testing.T) {
	pm := newTestManager(t, "")
	ctx := context.Background()

	m := sampleManifest()
	m.Name = "vault-plugin"
	m.Integration = types.PluginIntegrationSecretProvider

	if err := pm.LoadPlugin(ctx, m); err != nil {
		t.Fatal(err)
	}

	unregisterCalled := false
	pm.SetSecretBridge(&SecretRegistryBridge{
		RegisterAdapter: func(_ *PluginSecretAdapter) {},
		UnregisterAdapter: func(_ string) {
			unregisterCalled = true
		},
		IsActive: func(_ string) bool {
			return false // not active
		},
		HasSecrets: func(_ context.Context, _ string) (bool, error) {
			return true, nil // has secrets, but not active — should still be allowed
		},
	})

	if err := pm.UnloadPlugin(ctx, "vault-plugin"); err != nil {
		t.Fatalf("UnloadPlugin() should allow removal of non-active secret provider: %v", err)
	}
	if !unregisterCalled {
		t.Error("expected UnregisterAdapter to be called")
	}
}

func TestDisablePlugin_SecretProviderWithActiveSecrets_Blocked(t *testing.T) {
	pm := newTestManager(t, "")
	ctx := context.Background()

	m := sampleManifest()
	m.Name = "vault-plugin"
	m.Integration = types.PluginIntegrationSecretProvider

	if err := pm.LoadPlugin(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := pm.EnablePlugin(ctx, "vault-plugin"); err != nil {
		t.Fatal(err)
	}

	pm.SetSecretBridge(&SecretRegistryBridge{
		RegisterAdapter:   func(_ *PluginSecretAdapter) {},
		UnregisterAdapter: func(_ string) {},
		IsActive: func(name string) bool {
			return name == "vault-plugin"
		},
		HasSecrets: func(_ context.Context, name string) (bool, error) {
			return true, nil
		},
	})

	err := pm.DisablePlugin(ctx, "vault-plugin")
	if err == nil {
		t.Fatal("DisablePlugin() should block disabling active secret provider with secrets")
	}
	if !strings.Contains(err.Error(), "active secret provider") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestListPlugins_MultiplePlugins(t *testing.T) {
	pm := newTestManager(t, "")
	ctx := context.Background()

	m1 := sampleManifest()
	m2 := sampleManifest()
	m2.Name = "second-plugin"
	m2.Tools = []types.ToolDef{
		{Name: "tool_b", Description: "Tool B"},
	}

	if err := pm.LoadPlugin(ctx, m1); err != nil {
		t.Fatal(err)
	}
	if err := pm.LoadPlugin(ctx, m2); err != nil {
		t.Fatal(err)
	}

	states := pm.ListPlugins()
	if len(states) != 2 {
		t.Fatalf("ListPlugins() count = %d, want 2", len(states))
	}

	names := map[string]bool{}
	for _, s := range states {
		names[s.Name] = true
	}
	if !names["test-plugin"] || !names["second-plugin"] {
		t.Errorf("expected both plugins in list, got: %v", names)
	}
}
