package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/secrets"
	"github.com/hyperax/hyperax/internal/storage"
	"github.com/hyperax/hyperax/pkg/types"
)

// actionClearanceSecret maps each secret action to its minimum ABAC clearance.
var actionClearanceSecret = map[string]int{
	"get":              2, // was get_secret
	"set":              3, // was set_secret
	"delete":           3, // was delete_secret
	"list":             2, // was list_secrets
	"list_entries":     2, // was list_secret_entries
	"update_scope":     3, // was update_secret_access_scope
	"rotate":           3, // was rotate_secret
	"configure_provider": 3, // was configure_secret_provider
	"resolve_ref":      2, // was resolve_secret_ref
}

// SecretHandler implements the consolidated "secret" MCP tool.
type SecretHandler struct {
	store    *storage.Store
	registry *secrets.Registry
}

// NewSecretHandler creates a SecretHandler. The registry parameter is optional;
// when nil, operations use the store.Secrets repo directly.
func NewSecretHandler(store *storage.Store, registry *secrets.Registry) *SecretHandler {
	return &SecretHandler{store: store, registry: registry}
}

// RegisterTools registers the consolidated secret tool with the MCP registry.
func (h *SecretHandler) RegisterTools(registry *mcp.ToolRegistry) {
	registry.Register(
		"secret",
		"Secret management: get, set, delete, list, rotate secrets and configure providers. "+
			"Actions: get | set | delete | list | list_entries | update_scope | rotate | configure_provider | resolve_ref",
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"action":       {"type": "string", "enum": ["get", "set", "delete", "list", "list_entries", "update_scope", "rotate", "configure_provider", "resolve_ref"], "description": "Action to perform"},
				"key":          {"type": "string", "description": "Secret key"},
				"value":        {"type": "string", "description": "Secret value (set action)"},
				"scope":        {"type": "string", "description": "Secret scope (default: global)"},
				"access_scope": {"type": "string", "description": "Access restriction (set, update_scope actions)"},
				"new_value":    {"type": "string", "description": "New secret value (rotate action)"},
				"provider_action": {"type": "string", "enum": ["switch", "health", "list"], "description": "Provider action (configure_provider action)"},
				"provider":     {"type": "string", "description": "Provider name (configure_provider action)"},
				"ref":          {"type": "string", "description": "Secret reference to resolve (resolve_ref action)"}
			},
			"required": ["action"]
		}`),
		h.dispatch,
	)
}

// dispatch routes the consolidated "secret" tool to the correct handler method.
func (h *SecretHandler) dispatch(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var envelope struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return nil, fmt.Errorf("handlers.SecretHandler.dispatch: %w", err)
	}

	if err := checkActionClearance(ctx, envelope.Action, actionClearanceSecret); err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	switch envelope.Action {
	case "get":
		return h.getSecret(ctx, params)
	case "set":
		return h.setSecret(ctx, params)
	case "delete":
		return h.deleteSecret(ctx, params)
	case "list":
		return h.listSecrets(ctx, params)
	case "list_entries":
		return h.listSecretEntries(ctx, params)
	case "update_scope":
		return h.updateSecretAccessScope(ctx, params)
	case "rotate":
		return h.rotateSecret(ctx, params)
	case "configure_provider":
		return h.configureSecretProvider(ctx, params)
	case "resolve_ref":
		return h.resolveSecretRef(ctx, params)
	default:
		return types.NewErrorResult(fmt.Sprintf("unknown secret action %q", envelope.Action)), nil
	}
}

// activeProvider returns the active provider, falling back to nil.
func (h *SecretHandler) activeProvider() secrets.Provider {
	if h.registry == nil {
		return nil
	}
	p, err := h.registry.Active()
	if err != nil {
		return nil
	}
	return p
}

func (h *SecretHandler) getSecret(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Key   string `json:"key"`
		Scope string `json:"scope"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.SecretHandler.getSecret: %w", err)
	}
	if args.Key == "" {
		return types.NewErrorResult("key is required"), nil
	}
	if args.Scope == "" {
		args.Scope = "global"
	}

	auth := mcp.AuthFromContext(ctx)
	slog.Warn("secret accessed via MCP tool",
		"key", args.Key,
		"scope", args.Scope,
		"persona_id", auth.PersonaID,
		"authenticated", auth.Authenticated,
	)

	var accessScope string
	if p := h.activeProvider(); p != nil {
		var scopeErr error
		accessScope, scopeErr = p.GetAccessScope(ctx, args.Key, args.Scope)
		if scopeErr != nil {
			return types.NewErrorResult(fmt.Sprintf("get access scope: %v", scopeErr)), nil
		}
	} else if h.store.Secrets != nil {
		var scopeErr error
		accessScope, scopeErr = h.store.Secrets.GetAccessScope(ctx, args.Key, args.Scope)
		if scopeErr != nil {
			return types.NewErrorResult(fmt.Sprintf("get access scope: %v", scopeErr)), nil
		}
	}
	if err := checkSecretAccess(ctx, accessScope); err != nil {
		return types.NewErrorResult(fmt.Sprintf("access denied: %v", err)), nil
	}

	if p := h.activeProvider(); p != nil {
		value, err := p.Get(ctx, args.Key, args.Scope)
		if err != nil {
			return types.NewErrorResult(fmt.Sprintf("get secret: %v", err)), nil
		}
		return types.NewToolResult(map[string]string{
			"key":      args.Key,
			"scope":    args.Scope,
			"value":    value,
			"provider": p.Name(),
		}), nil
	}

	if h.store.Secrets == nil {
		return types.NewErrorResult("secret repo not available"), nil
	}

	value, err := h.store.Secrets.Get(ctx, args.Key, args.Scope)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get secret: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"key":      args.Key,
		"scope":    args.Scope,
		"value":    value,
		"provider": "local",
	}), nil
}

func (h *SecretHandler) setSecret(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Key         string `json:"key"`
		Value       string `json:"value"`
		Scope       string `json:"scope"`
		AccessScope string `json:"access_scope"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.SecretHandler.setSecret: %w", err)
	}
	if args.Key == "" {
		return types.NewErrorResult("key is required"), nil
	}
	if args.Value == "" {
		return types.NewErrorResult("value is required"), nil
	}
	if args.Scope == "" {
		args.Scope = "global"
	}
	if args.AccessScope == "" {
		args.AccessScope = "global"
	}

	auth := mcp.AuthFromContext(ctx)
	slog.Warn("secret modified via MCP tool",
		"action", "set",
		"key", args.Key,
		"scope", args.Scope,
		"access_scope", args.AccessScope,
		"persona_id", auth.PersonaID,
		"authenticated", auth.Authenticated,
	)

	useAccessScope := args.AccessScope != "global"

	if p := h.activeProvider(); p != nil {
		if useAccessScope {
			if err := p.SetWithAccess(ctx, args.Key, args.Value, args.Scope, args.AccessScope); err != nil {
				return types.NewErrorResult(fmt.Sprintf("set secret: %v", err)), nil
			}
		} else {
			if err := p.Set(ctx, args.Key, args.Value, args.Scope); err != nil {
				return types.NewErrorResult(fmt.Sprintf("set secret: %v", err)), nil
			}
		}
		return types.NewToolResult(fmt.Sprintf("Secret %q set in scope %q (access: %s) via %s.", args.Key, args.Scope, args.AccessScope, p.Name())), nil
	}

	if h.store.Secrets == nil {
		return types.NewErrorResult("secret repo not available"), nil
	}

	if useAccessScope {
		if err := h.store.Secrets.SetWithAccess(ctx, args.Key, args.Value, args.Scope, args.AccessScope); err != nil {
			return types.NewErrorResult(fmt.Sprintf("set secret: %v", err)), nil
		}
	} else {
		if err := h.store.Secrets.Set(ctx, args.Key, args.Value, args.Scope); err != nil {
			return types.NewErrorResult(fmt.Sprintf("set secret: %v", err)), nil
		}
	}

	return types.NewToolResult(fmt.Sprintf("Secret %q set in scope %q (access: %s).", args.Key, args.Scope, args.AccessScope)), nil
}

func (h *SecretHandler) deleteSecret(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Key   string `json:"key"`
		Scope string `json:"scope"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.SecretHandler.deleteSecret: %w", err)
	}
	if args.Key == "" {
		return types.NewErrorResult("key is required"), nil
	}
	if args.Scope == "" {
		args.Scope = "global"
	}

	auth := mcp.AuthFromContext(ctx)
	slog.Warn("secret modified via MCP tool",
		"action", "delete",
		"key", args.Key,
		"scope", args.Scope,
		"persona_id", auth.PersonaID,
		"authenticated", auth.Authenticated,
	)

	if p := h.activeProvider(); p != nil {
		if err := p.Delete(ctx, args.Key, args.Scope); err != nil {
			return types.NewErrorResult(fmt.Sprintf("delete secret: %v", err)), nil
		}
		return types.NewToolResult(fmt.Sprintf("Secret %q deleted from scope %q via %s.", args.Key, args.Scope, p.Name())), nil
	}

	if h.store.Secrets == nil {
		return types.NewErrorResult("secret repo not available"), nil
	}

	if err := h.store.Secrets.Delete(ctx, args.Key, args.Scope); err != nil {
		return types.NewErrorResult(fmt.Sprintf("delete secret: %v", err)), nil
	}

	return types.NewToolResult(fmt.Sprintf("Secret %q deleted from scope %q.", args.Key, args.Scope)), nil
}

func (h *SecretHandler) listSecrets(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Scope string `json:"scope"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.SecretHandler.listSecrets: %w", err)
	}
	if args.Scope == "" {
		args.Scope = "global"
	}

	if p := h.activeProvider(); p != nil {
		keys, err := p.List(ctx, args.Scope)
		if err != nil {
			return nil, fmt.Errorf("handlers.SecretHandler.listSecrets: %w", err)
		}
		if len(keys) == 0 {
			return types.NewToolResult(fmt.Sprintf("No secrets in scope %q (provider: %s).", args.Scope, p.Name())), nil
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "Secret keys in scope %q (provider: %s):\n", args.Scope, p.Name())
		for _, key := range keys {
			fmt.Fprintf(&sb, "  - %s\n", key)
		}
		return types.NewToolResult(sb.String()), nil
	}

	if h.store.Secrets == nil {
		return types.NewErrorResult("secret repo not available"), nil
	}

	keys, err := h.store.Secrets.List(ctx, args.Scope)
	if err != nil {
		return nil, fmt.Errorf("handlers.SecretHandler.listSecrets: %w", err)
	}

	if len(keys) == 0 {
		return types.NewToolResult(fmt.Sprintf("No secrets in scope %q.", args.Scope)), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Secret keys in scope %q:\n", args.Scope)
	for _, key := range keys {
		fmt.Fprintf(&sb, "  - %s\n", key)
	}
	return types.NewToolResult(sb.String()), nil
}

func (h *SecretHandler) listSecretEntries(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Scope string `json:"scope"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.SecretHandler.listSecretEntries: %w", err)
	}
	if args.Scope == "" {
		args.Scope = "global"
	}

	if p := h.activeProvider(); p != nil {
		entries, err := p.ListEntries(ctx, args.Scope)
		if err != nil {
			return types.NewErrorResult(fmt.Sprintf("list entries: %v", err)), nil
		}
		return types.NewToolResult(map[string]any{
			"scope":    args.Scope,
			"provider": p.Name(),
			"count":    len(entries),
			"entries":  entries,
		}), nil
	}

	if h.store.Secrets == nil {
		return types.NewErrorResult("secret repo not available"), nil
	}

	entries, err := h.store.Secrets.ListEntries(ctx, args.Scope)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("list entries: %v", err)), nil
	}

	return types.NewToolResult(map[string]any{
		"scope":   args.Scope,
		"count":   len(entries),
		"entries": entries,
	}), nil
}

func (h *SecretHandler) updateSecretAccessScope(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Key         string `json:"key"`
		Scope       string `json:"scope"`
		AccessScope string `json:"access_scope"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.SecretHandler.updateSecretAccessScope: %w", err)
	}
	if args.Key == "" {
		return types.NewErrorResult("key is required"), nil
	}
	if args.AccessScope == "" {
		return types.NewErrorResult("access_scope is required"), nil
	}
	if args.Scope == "" {
		args.Scope = "global"
	}

	auth := mcp.AuthFromContext(ctx)
	slog.Warn("secret access scope modified via MCP tool",
		"action", "update_access_scope",
		"key", args.Key,
		"scope", args.Scope,
		"new_access_scope", args.AccessScope,
		"persona_id", auth.PersonaID,
		"authenticated", auth.Authenticated,
	)

	if p := h.activeProvider(); p != nil {
		if err := p.UpdateAccessScope(ctx, args.Key, args.Scope, args.AccessScope); err != nil {
			return types.NewErrorResult(fmt.Sprintf("update access scope: %v", err)), nil
		}
		return types.NewToolResult(fmt.Sprintf("Access scope for secret %q updated to %q via %s.", args.Key, args.AccessScope, p.Name())), nil
	}

	if h.store.Secrets == nil {
		return types.NewErrorResult("secret repo not available"), nil
	}

	if err := h.store.Secrets.UpdateAccessScope(ctx, args.Key, args.Scope, args.AccessScope); err != nil {
		return types.NewErrorResult(fmt.Sprintf("update access scope: %v", err)), nil
	}

	return types.NewToolResult(fmt.Sprintf("Access scope for secret %q updated to %q.", args.Key, args.AccessScope)), nil
}

func (h *SecretHandler) rotateSecret(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Key      string `json:"key"`
		NewValue string `json:"new_value"`
		Scope    string `json:"scope"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.SecretHandler.rotateSecret: %w", err)
	}
	if args.Key == "" {
		return types.NewErrorResult("key is required"), nil
	}
	if args.NewValue == "" {
		return types.NewErrorResult("new_value is required"), nil
	}
	if args.Scope == "" {
		args.Scope = "global"
	}

	p := h.activeProvider()
	if p == nil {
		return types.NewErrorResult("no secret provider available for rotation"), nil
	}

	auth := mcp.AuthFromContext(ctx)
	slog.Warn("secret rotated via MCP tool",
		"key", args.Key,
		"scope", args.Scope,
		"provider", p.Name(),
		"persona_id", auth.PersonaID,
		"authenticated", auth.Authenticated,
	)

	oldVal, err := p.Rotate(ctx, args.Key, args.NewValue, args.Scope)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("rotate secret: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"key":       args.Key,
		"scope":     args.Scope,
		"old_value": oldVal,
		"provider":  p.Name(),
		"status":    "rotated",
	}), nil
}

func (h *SecretHandler) configureSecretProvider(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ProviderAction string `json:"provider_action"`
		Provider       string `json:"provider"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.SecretHandler.configureSecretProvider: %w", err)
	}

	if h.registry == nil {
		return types.NewErrorResult("secret provider registry not available"), nil
	}

	switch args.ProviderAction {
	case "list":
		names := h.registry.List()
		sort.Strings(names)
		active := h.registry.ActiveName()
		return types.NewToolResult(map[string]any{
			"providers": names,
			"active":    active,
		}), nil

	case "switch":
		if args.Provider == "" {
			return types.NewErrorResult("provider name is required for switch"), nil
		}
		if err := h.registry.SetActive(args.Provider); err != nil {
			return types.NewErrorResult(fmt.Sprintf("switch provider: %v", err)), nil
		}
		return types.NewToolResult(fmt.Sprintf("Active secret provider switched to %q.", args.Provider)), nil

	case "health":
		if args.Provider == "" {
			return types.NewErrorResult("provider name is required for health check"), nil
		}
		p := h.registry.Get(args.Provider)
		if p == nil {
			return types.NewErrorResult(fmt.Sprintf("provider %q not registered", args.Provider)), nil
		}
		if err := p.Health(ctx); err != nil {
			return types.NewToolResult(map[string]string{
				"provider": args.Provider,
				"status":   "unhealthy",
				"error":    err.Error(),
			}), nil
		}
		return types.NewToolResult(map[string]string{
			"provider": args.Provider,
			"status":   "healthy",
		}), nil

	default:
		return types.NewErrorResult(fmt.Sprintf("unknown provider_action %q (valid: switch, health, list)", args.ProviderAction)), nil
	}
}

func (h *SecretHandler) resolveSecretRef(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.SecretHandler.resolveSecretRef: %w", err)
	}
	if args.Ref == "" {
		return types.NewErrorResult("ref is required"), nil
	}

	if h.registry == nil {
		return types.NewErrorResult("secret provider registry not available"), nil
	}

	value, err := secrets.ResolveSecretRef(ctx, h.registry, args.Ref)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("resolve: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"ref":      args.Ref,
		"resolved": value,
	}), nil
}

// checkSecretAccess validates that the caller is authorized to access a secret
// based on its access_scope. Returns nil if access is permitted.
func checkSecretAccess(ctx context.Context, accessScope string) error {
	if accessScope == "" || accessScope == "global" {
		return nil // no restriction
	}
	auth := mcp.AuthFromContext(ctx)

	if strings.HasPrefix(accessScope, "persona:") {
		requiredPersona := strings.TrimPrefix(accessScope, "persona:")
		if auth.PersonaID != requiredPersona {
			return fmt.Errorf("secret restricted to persona %q", requiredPersona)
		}
		return nil
	}

	if strings.HasPrefix(accessScope, "plugin:") {
		requiredHash := strings.TrimPrefix(accessScope, "plugin:")
		if auth.PluginHash == "" || auth.PluginHash != requiredHash {
			return fmt.Errorf("secret restricted to plugin %q", requiredHash)
		}
		return nil
	}

	if strings.HasPrefix(accessScope, "provider:") {
		if auth.ClearanceLevel < types.ClearanceAdmin {
			requiredProvider := strings.TrimPrefix(accessScope, "provider:")
			return fmt.Errorf("secret restricted to provider %q (admin access required)", requiredProvider)
		}
		return nil
	}

	if accessScope == "system" {
		return fmt.Errorf("secret restricted to system use only")
	}

	return nil
}
