package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/hyperax/hyperax/internal/repo"
)

// PluginSecretAdapter bridges the secrets.Provider interface to a plugin's MCP
// tools. When a secret_provider plugin is enabled, this adapter is registered
// with the secrets.Registry so the rest of Hyperax can use it transparently.
//
// The adapter delegates each Provider method to the corresponding MCP tool on
// the plugin subprocess. Tool names follow the convention:
//
//	get_secret, set_secret, delete_secret, list_secrets,
//	list_secret_entries, get_access_scope, update_access_scope,
//	rotate_secret, health_check
//
// The plugin receives standard JSON arguments and returns JSON results.
type PluginSecretAdapter struct {
	pluginName string
	client     *MCPClient
	logger     *slog.Logger
}

// NewPluginSecretAdapter creates an adapter that routes secrets.Provider calls
// to the named plugin's MCP tools via the given client.
func NewPluginSecretAdapter(pluginName string, client *MCPClient, logger *slog.Logger) *PluginSecretAdapter {
	return &PluginSecretAdapter{
		pluginName: pluginName,
		client:     client,
		logger:     logger.With("component", "plugin-secret-adapter", "plugin", pluginName),
	}
}

// Name returns the plugin name, used as the provider identifier in the registry.
func (a *PluginSecretAdapter) Name() string {
	return a.pluginName
}

// Get retrieves a secret value by key and scope.
func (a *PluginSecretAdapter) Get(ctx context.Context, key, scope string) (string, error) {
	args := map[string]string{"key": key, "scope": scope}
	result, err := a.callTool(ctx, "get_secret", args)
	if err != nil {
		return "", err
	}
	var resp struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", fmt.Errorf("plugin.PluginSecretAdapter.Get: unmarshal response: %w", err)
	}
	return resp.Value, nil
}

// Set creates or updates a secret value for the given key and scope.
func (a *PluginSecretAdapter) Set(ctx context.Context, key, value, scope string) error {
	args := map[string]string{"key": key, "value": value, "scope": scope}
	_, err := a.callTool(ctx, "set_secret", args)
	return err
}

// Delete removes a secret by key and scope.
func (a *PluginSecretAdapter) Delete(ctx context.Context, key, scope string) error {
	args := map[string]string{"key": key, "scope": scope}
	_, err := a.callTool(ctx, "delete_secret", args)
	return err
}

// List returns all secret keys for the given scope.
func (a *PluginSecretAdapter) List(ctx context.Context, scope string) ([]string, error) {
	args := map[string]string{"scope": scope}
	result, err := a.callTool(ctx, "list_secrets", args)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Keys []string `json:"keys"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("plugin.PluginSecretAdapter.List: unmarshal response: %w", err)
	}
	return resp.Keys, nil
}

// SetWithAccess stores a secret with an access scope restriction.
func (a *PluginSecretAdapter) SetWithAccess(ctx context.Context, key, value, scope, accessScope string) error {
	args := map[string]string{"key": key, "value": value, "scope": scope, "access_scope": accessScope}
	_, err := a.callTool(ctx, "set_secret", args)
	return err
}

// ListEntries returns secret metadata (not values) for a scope.
func (a *PluginSecretAdapter) ListEntries(ctx context.Context, scope string) ([]repo.SecretEntry, error) {
	args := map[string]string{"scope": scope}
	result, err := a.callTool(ctx, "list_secret_entries", args)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Entries []repo.SecretEntry `json:"entries"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("plugin.PluginSecretAdapter.ListEntries: unmarshal response: %w", err)
	}
	return resp.Entries, nil
}

// GetAccessScope returns the access_scope for a secret.
func (a *PluginSecretAdapter) GetAccessScope(ctx context.Context, key, scope string) (string, error) {
	args := map[string]string{"key": key, "scope": scope}
	result, err := a.callTool(ctx, "get_access_scope", args)
	if err != nil {
		return "", err
	}
	var resp struct {
		AccessScope string `json:"access_scope"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", fmt.Errorf("plugin.PluginSecretAdapter.GetAccessScope: unmarshal response: %w", err)
	}
	return resp.AccessScope, nil
}

// UpdateAccessScope changes the access restriction on an existing secret.
func (a *PluginSecretAdapter) UpdateAccessScope(ctx context.Context, key, scope, accessScope string) error {
	args := map[string]string{"key": key, "scope": scope, "access_scope": accessScope}
	_, err := a.callTool(ctx, "update_access_scope", args)
	return err
}

// Rotate replaces a secret value atomically, returning the old value.
func (a *PluginSecretAdapter) Rotate(ctx context.Context, key, newValue, scope string) (string, error) {
	args := map[string]string{"key": key, "new_value": newValue, "scope": scope}
	result, err := a.callTool(ctx, "rotate_secret", args)
	if err != nil {
		return "", err
	}
	var resp struct {
		OldValue string `json:"old_value"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", fmt.Errorf("plugin.PluginSecretAdapter.Rotate: unmarshal response: %w", err)
	}
	return resp.OldValue, nil
}

// Health checks whether the plugin secret backend is reachable and operational.
func (a *PluginSecretAdapter) Health(ctx context.Context) error {
	_, err := a.callTool(ctx, "health_check", map[string]string{})
	return err
}

// callTool sends a tools/call request to the plugin and extracts the text
// content from the MCP ToolResult response.
func (a *PluginSecretAdapter) callTool(ctx context.Context, toolName string, args any) (json.RawMessage, error) {
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("plugin.PluginSecretAdapter.callTool: marshal args for %s: %w", toolName, err)
	}

	result, err := a.client.CallTool(ctx, toolName, argsJSON)
	if err != nil {
		return nil, fmt.Errorf("plugin.PluginSecretAdapter.callTool: %s: %w", toolName, err)
	}

	// The plugin returns an MCP ToolResult. Extract the text content.
	var toolResult struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &toolResult); err != nil {
		// If not a standard ToolResult, return raw result.
		return result, nil
	}

	if toolResult.IsError {
		for _, c := range toolResult.Content {
			if c.Type == "text" {
				return nil, fmt.Errorf("plugin.PluginSecretAdapter.callTool: %s error: %s", toolName, c.Text)
			}
		}
		return nil, fmt.Errorf("plugin.PluginSecretAdapter.callTool: %s returned error", toolName)
	}

	// Return the first text content block as raw JSON for callers to unmarshal.
	for _, c := range toolResult.Content {
		if c.Type == "text" {
			return json.RawMessage(c.Text), nil
		}
	}

	return nil, fmt.Errorf("plugin.PluginSecretAdapter.callTool: %s returned no text content", toolName)
}
