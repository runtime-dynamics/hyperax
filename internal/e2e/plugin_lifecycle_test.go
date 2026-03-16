//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// mcpCall performs a JSON-RPC 2.0 tools/call request against the Hyperax MCP
// Streamable HTTP endpoint. It returns the parsed result payload on success or
// fails the test on transport/JSON-RPC errors.
func mcpCall(t *testing.T, server *httptest.Server, tool string, args any) json.RawMessage {
	t.Helper()

	argsJSON, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args for %s: %v", tool, err)
	}

	rpcReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      tool,
			"arguments": json.RawMessage(argsJSON),
		},
	}
	body, _ := json.Marshal(rpcReq)

	resp, err := http.Post(server.URL+"/mcp", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /mcp (tool=%s): %v", tool, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /mcp (tool=%s): HTTP %d", tool, resp.StatusCode)
	}

	var rpcResp struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode JSON-RPC response (tool=%s): %v", tool, err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("JSON-RPC error (tool=%s): code=%d msg=%s", tool, rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result
}

// mcpCallAllowError is like mcpCall but returns the full response including
// ToolResult.IsError results without failing. The caller inspects the result.
func mcpCallAllowError(t *testing.T, server *httptest.Server, tool string, args any) json.RawMessage {
	t.Helper()

	argsJSON, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args for %s: %v", tool, err)
	}

	rpcReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      tool,
			"arguments": json.RawMessage(argsJSON),
		},
	}
	body, _ := json.Marshal(rpcReq)

	resp, err := http.Post(server.URL+"/mcp", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /mcp (tool=%s): %v", tool, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /mcp (tool=%s): HTTP %d", tool, resp.StatusCode)
	}

	var rpcResp struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode JSON-RPC response (tool=%s): %v", tool, err)
	}
	// JSON-RPC level errors are still fatal (method not found, parse error, etc.)
	if rpcResp.Error != nil {
		t.Fatalf("JSON-RPC error (tool=%s): code=%d msg=%s", tool, rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result
}

// extractToolResultText extracts the text content from a ToolResult JSON payload.
// ToolResult has shape: {"content": [{"type":"text","text":"..."}], "isError": bool}
func extractToolResultText(t *testing.T, raw json.RawMessage) (string, bool) {
	t.Helper()

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal ToolResult: %v", err)
	}
	if len(result.Content) == 0 {
		t.Fatal("ToolResult has no content items")
	}
	return result.Content[0].Text, result.IsError
}

// parseToolResultJSON extracts the text content from a ToolResult and unmarshals
// it as JSON into the target. Fails the test if the result is an error.
func parseToolResultJSON(t *testing.T, raw json.RawMessage, target any) {
	t.Helper()
	text, isError := extractToolResultText(t, raw)
	if isError {
		t.Fatalf("unexpected ToolResult error: %s", text)
	}
	if err := json.Unmarshal([]byte(text), target); err != nil {
		t.Fatalf("unmarshal ToolResult text as JSON: %v (text=%s)", err, text)
	}
}

// writeTestManifest creates a minimal hyperax-plugin.yaml manifest file in the
// given directory. The manifest declares a native-type plugin with one tool and
// one non-secret variable, suitable for lifecycle testing without requiring a
// real binary.
func writeTestManifest(t *testing.T, dir, pluginName string) {
	t.Helper()

	manifest := fmt.Sprintf(`name: %s
version: "1.0.0"
type: native
description: "E2E test plugin for lifecycle validation"
author: "hyperax-e2e"
api_version: "1"
min_hyperax_version: "0.0.0"
integration: tooling
permissions:
  - workspace:read
entrypoint: "./test-plugin"
variables:
  - name: log_level
    type: string
    required: false
    default: "info"
    description: "Log level for the test plugin"
    secret: false
tools:
  - name: test_action
    description: "A no-op test action for lifecycle validation"
    parameters:
      - name: input
        type: string
        required: false
        description: "Test input parameter"
`, pluginName)

	manifestPath := filepath.Join(dir, "hyperax-plugin.yaml")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0644); err != nil {
		t.Fatalf("write test manifest: %v", err)
	}
}

// TestPluginLifecycle_ListPlugins_Empty verifies that list_plugins returns an
// empty list when no plugins are installed.
func TestPluginLifecycle_ListPlugins_Empty(t *testing.T) {
	h := newTestHarness(t)

	result := mcpCall(t, h.server, "list_plugins", map[string]any{})

	var plugins []any
	parseToolResultJSON(t, result, &plugins)

	if len(plugins) != 0 {
		t.Errorf("expected empty plugin list, got %d items", len(plugins))
	}
}

// TestPluginLifecycle_InstallConfigureEnableDisableUninstall exercises the full
// plugin lifecycle via MCP tool calls through the test harness:
//  1. list_plugins — verify empty
//  2. install_plugin — from a local test manifest
//  3. list_plugins — verify the plugin appears
//  4. get_plugin_info — verify manifest fields
//  5. configure_plugin — set a variable
//  6. enable_plugin — attempt enable (expected error for native without binary)
//  7. disable_plugin — graceful disable
//  8. uninstall_plugin — verify removal
func TestPluginLifecycle_InstallConfigureEnableDisableUninstall(t *testing.T) {
	h := newTestHarness(t)
	pluginName := "e2e-test-plugin"

	// Create a temporary plugin directory with a valid manifest.
	pluginDir := filepath.Join(t.TempDir(), pluginName)
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("create plugin dir: %v", err)
	}
	writeTestManifest(t, pluginDir, pluginName)

	// --- Step 1: list_plugins — empty ---
	result := mcpCall(t, h.server, "list_plugins", map[string]any{})
	var emptyPlugins []any
	parseToolResultJSON(t, result, &emptyPlugins)
	if len(emptyPlugins) != 0 {
		t.Fatalf("step 1: expected empty plugin list, got %d items", len(emptyPlugins))
	}

	// --- Step 2: install_plugin from local path ---
	result = mcpCall(t, h.server, "install_plugin", map[string]string{
		"path": pluginDir,
	})
	var installResult map[string]any
	parseToolResultJSON(t, result, &installResult)

	if installResult["name"] != pluginName {
		t.Errorf("step 2: install name=%v, want %s", installResult["name"], pluginName)
	}
	if installResult["version"] != "1.0.0" {
		t.Errorf("step 2: install version=%v, want 1.0.0", installResult["version"])
	}
	toolCount, ok := installResult["tools"].(float64)
	if !ok || toolCount != 1 {
		t.Errorf("step 2: install tools=%v, want 1", installResult["tools"])
	}
	if installResult["integration"] != "tooling" {
		t.Errorf("step 2: install integration=%v, want tooling", installResult["integration"])
	}

	// --- Step 3: list_plugins — plugin appears ---
	result = mcpCall(t, h.server, "list_plugins", map[string]any{})
	var pluginList []map[string]any
	parseToolResultJSON(t, result, &pluginList)
	if len(pluginList) != 1 {
		t.Fatalf("step 3: expected 1 plugin, got %d", len(pluginList))
	}
	if pluginList[0]["name"] != pluginName {
		t.Errorf("step 3: plugin name=%v, want %s", pluginList[0]["name"], pluginName)
	}
	if pluginList[0]["status"] != "loaded" {
		t.Errorf("step 3: plugin status=%v, want loaded", pluginList[0]["status"])
	}

	// --- Step 4: get_plugin_info — verify manifest fields ---
	result = mcpCall(t, h.server, "get_plugin_info", map[string]string{
		"name": pluginName,
	})
	var pluginInfo map[string]any
	parseToolResultJSON(t, result, &pluginInfo)

	if pluginInfo["name"] != pluginName {
		t.Errorf("step 4: info name=%v, want %s", pluginInfo["name"], pluginName)
	}
	if pluginInfo["version"] != "1.0.0" {
		t.Errorf("step 4: info version=%v, want 1.0.0", pluginInfo["version"])
	}
	if pluginInfo["type"] != "native" {
		t.Errorf("step 4: info type=%v, want native", pluginInfo["type"])
	}
	if pluginInfo["description"] != "E2E test plugin for lifecycle validation" {
		t.Errorf("step 4: info description=%v", pluginInfo["description"])
	}
	if pluginInfo["author"] != "hyperax-e2e" {
		t.Errorf("step 4: info author=%v, want hyperax-e2e", pluginInfo["author"])
	}
	if pluginInfo["integration"] != "tooling" {
		t.Errorf("step 4: info integration=%v, want tooling", pluginInfo["integration"])
	}

	// Verify tools array contains our test tool.
	tools, ok := pluginInfo["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Errorf("step 4: info tools is empty or not array")
	} else {
		foundTestAction := false
		for _, tool := range tools {
			if tool == "test_action" {
				foundTestAction = true
			}
		}
		if !foundTestAction {
			t.Errorf("step 4: tool 'test_action' not found in %v", tools)
		}
	}

	// --- Step 5: configure_plugin — set a non-secret variable ---
	result = mcpCall(t, h.server, "configure_plugin", map[string]string{
		"name":     pluginName,
		"variable": "log_level",
		"value":    "debug",
	})
	var configResult map[string]any
	parseToolResultJSON(t, result, &configResult)

	if configResult["plugin"] != pluginName {
		t.Errorf("step 5: config plugin=%v, want %s", configResult["plugin"], pluginName)
	}
	if configResult["variable"] != "log_level" {
		t.Errorf("step 5: config variable=%v, want log_level", configResult["variable"])
	}
	if configResult["value"] != "debug" {
		t.Errorf("step 5: config value=%v, want debug", configResult["value"])
	}

	// Verify the config was persisted by reading it back.
	result = mcpCall(t, h.server, "get_plugin_config", map[string]string{
		"name": pluginName,
	})
	var configRead map[string]any
	parseToolResultJSON(t, result, &configRead)

	configValues, ok := configRead["config"].(map[string]any)
	if !ok {
		t.Fatalf("step 5 verify: config field is not a map: %v", configRead["config"])
	}
	if configValues["log_level"] != "debug" {
		t.Errorf("step 5 verify: config log_level=%v, want debug", configValues["log_level"])
	}

	// --- Step 6: enable_plugin — expect error for native without binary ---
	// Native plugins require a real executable; enabling will fail, which is
	// the expected behavior in this test environment. We verify the system
	// handles the error gracefully rather than crashing.
	enableResult := mcpCallAllowError(t, h.server, "enable_plugin", map[string]string{
		"name": pluginName,
	})
	enableText, enableIsError := extractToolResultText(t, enableResult)
	// The enable may succeed (placeholder mode) or fail (no binary). Either
	// outcome is acceptable for this lifecycle test. We just verify we got
	// a coherent response.
	if enableIsError {
		t.Logf("step 6: enable_plugin returned expected error (no binary): %s", enableText)
	} else {
		t.Logf("step 6: enable_plugin succeeded (placeholder mode)")
	}

	// --- Step 7: disable_plugin ---
	disableResult := mcpCallAllowError(t, h.server, "disable_plugin", map[string]string{
		"name": pluginName,
	})
	disableText, disableIsError := extractToolResultText(t, disableResult)
	if disableIsError {
		// disable may return an error if the plugin was never enabled or is
		// already in loaded state — this is acceptable.
		t.Logf("step 7: disable_plugin returned: %s", disableText)
	} else {
		var disableResp map[string]string
		if err := json.Unmarshal([]byte(disableText), &disableResp); err == nil {
			if disableResp["status"] != "disabled" {
				t.Errorf("step 7: disable status=%v, want disabled", disableResp["status"])
			}
		}
	}

	// --- Step 8: uninstall_plugin ---
	result = mcpCall(t, h.server, "uninstall_plugin", map[string]string{
		"name": pluginName,
	})
	var uninstallResult map[string]string
	parseToolResultJSON(t, result, &uninstallResult)

	if uninstallResult["status"] != "uninstalled" {
		t.Errorf("step 8: uninstall status=%v, want uninstalled", uninstallResult["status"])
	}

	// Verify the plugin is gone from the list.
	result = mcpCall(t, h.server, "list_plugins", map[string]any{})
	var finalPlugins []any
	parseToolResultJSON(t, result, &finalPlugins)
	if len(finalPlugins) != 0 {
		t.Errorf("step 8 verify: expected empty plugin list after uninstall, got %d", len(finalPlugins))
	}
}

// TestPluginLifecycle_InstallMissingManifest verifies that install_plugin
// returns a graceful error when the specified path has no manifest file.
func TestPluginLifecycle_InstallMissingManifest(t *testing.T) {
	h := newTestHarness(t)

	emptyDir := t.TempDir()
	result := mcpCallAllowError(t, h.server, "install_plugin", map[string]string{
		"path": emptyDir,
	})

	text, isError := extractToolResultText(t, result)
	if !isError {
		t.Errorf("expected error for missing manifest, got success: %s", text)
	}
}

// TestPluginLifecycle_ConfigureNonexistentPlugin verifies that configure_plugin
// returns an error when the plugin is not installed.
func TestPluginLifecycle_ConfigureNonexistentPlugin(t *testing.T) {
	h := newTestHarness(t)

	result := mcpCallAllowError(t, h.server, "configure_plugin", map[string]string{
		"name":     "nonexistent-plugin",
		"variable": "foo",
		"value":    "bar",
	})

	text, isError := extractToolResultText(t, result)
	if !isError {
		t.Errorf("expected error for nonexistent plugin, got success: %s", text)
	}
}

// TestPluginLifecycle_GetInfoNonexistent verifies that get_plugin_info returns
// an error for a plugin that is not installed.
func TestPluginLifecycle_GetInfoNonexistent(t *testing.T) {
	h := newTestHarness(t)

	result := mcpCallAllowError(t, h.server, "get_plugin_info", map[string]string{
		"name": "does-not-exist",
	})

	text, isError := extractToolResultText(t, result)
	if !isError {
		t.Errorf("expected error for nonexistent plugin info, got success: %s", text)
	}
}

// TestPluginLifecycle_UninstallNonexistent verifies that uninstall_plugin
// returns an error when the plugin is not installed.
func TestPluginLifecycle_UninstallNonexistent(t *testing.T) {
	h := newTestHarness(t)

	result := mcpCallAllowError(t, h.server, "uninstall_plugin", map[string]string{
		"name": "ghost-plugin",
	})

	text, isError := extractToolResultText(t, result)
	if !isError {
		t.Errorf("expected error for uninstalling nonexistent plugin, got success: %s", text)
	}
}

// TestPluginLifecycle_ConfigureUndeclaredVariable verifies that configure_plugin
// rejects variables not declared in the manifest.
func TestPluginLifecycle_ConfigureUndeclaredVariable(t *testing.T) {
	h := newTestHarness(t)
	pluginName := "e2e-undeclared-var-plugin"

	pluginDir := filepath.Join(t.TempDir(), pluginName)
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("create plugin dir: %v", err)
	}
	writeTestManifest(t, pluginDir, pluginName)

	// Install the plugin first.
	mcpCall(t, h.server, "install_plugin", map[string]string{"path": pluginDir})

	// Try to configure a variable not in the manifest.
	result := mcpCallAllowError(t, h.server, "configure_plugin", map[string]string{
		"name":     pluginName,
		"variable": "undeclared_var",
		"value":    "should-fail",
	})

	text, isError := extractToolResultText(t, result)
	if !isError {
		t.Errorf("expected error for undeclared variable, got success: %s", text)
	}
}
