package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/hyperax/hyperax/internal/config"
	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/provider"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/internal/storage"
	"github.com/hyperax/hyperax/pkg/types"
	"golang.org/x/crypto/bcrypt"
)

// actionClearanceConfig maps each config action to its minimum ABAC clearance.
var actionClearanceConfig = map[string]int{
	// Config actions
	"get_config":         0,
	"set_config":         2,
	"list_config_keys":   0,
	"get_config_history": 0,

	// Auth/token actions
	"create_mcp_token": 2,
	"revoke_mcp_token": 3,
	"list_mcp_tokens":  0,
	"rotate_mcp_token": 3,

	// Provider actions
	"list_providers":           0,
	"get_provider":             0,
	"create_provider":          2,
	"update_provider":          2,
	"delete_provider":          2,
	"set_default_provider":     2,
	"get_default_provider":     0,
	"list_provider_presets":    0,
	"refresh_provider_models":  1,
	"validate_provider_models": 0,
}

// ConfigHandler implements the consolidated "config" MCP tool.
// It provides runtime configuration management, MCP token authentication,
// and LLM provider configuration through a single action-dispatched tool.
type ConfigHandler struct {
	// Config deps
	configStore *config.ConfigStore
	configRepo  repo.ConfigRepo

	// Auth deps (set via SetAuthDeps)
	tokenRepo repo.MCPTokenRepo
	agentRepo repo.AgentRepo
	logger    *slog.Logger

	// Provider deps (set via SetProviderDeps)
	providerStore *storage.Store

	// Shared deps (set via SetEventBus)
	bus *nervous.EventBus
}

// NewConfigHandler creates a ConfigHandler backed by the given ConfigStore
// (for scoped resolution and sets) and ConfigRepo (for history queries).
func NewConfigHandler(configStore *config.ConfigStore, configRepo repo.ConfigRepo) *ConfigHandler {
	return &ConfigHandler{
		configStore: configStore,
		configRepo:  configRepo,
	}
}

// SetAuthDeps injects MCP token authentication dependencies.
func (h *ConfigHandler) SetAuthDeps(tokenRepo repo.MCPTokenRepo, agentRepo repo.AgentRepo, logger *slog.Logger) {
	h.tokenRepo = tokenRepo
	h.agentRepo = agentRepo
	h.logger = logger
}

// SetProviderDeps injects LLM provider management dependencies.
func (h *ConfigHandler) SetProviderDeps(store *storage.Store) {
	h.providerStore = store
}

// SetEventBus configures the EventBus for publishing audit trail events.
func (h *ConfigHandler) SetEventBus(bus *nervous.EventBus) {
	h.bus = bus
}

// RegisterTools registers the consolidated config tool.
func (h *ConfigHandler) RegisterTools(registry *mcp.ToolRegistry) {
	registry.Register(
		"config",
		"Configuration, authentication, and provider management. "+
			"Actions: get_config | set_config | list_config_keys | get_config_history | "+
			"create_mcp_token | revoke_mcp_token | list_mcp_tokens | rotate_mcp_token | "+
			"list_providers | get_provider | create_provider | update_provider | delete_provider | "+
			"set_default_provider | get_default_provider | list_provider_presets | "+
			"refresh_provider_models | validate_provider_models",
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": [
					"get_config", "set_config", "list_config_keys", "get_config_history",
					"create_mcp_token", "revoke_mcp_token", "list_mcp_tokens", "rotate_mcp_token",
					"list_providers", "get_provider", "create_provider", "update_provider", "delete_provider",
					"set_default_provider", "get_default_provider", "list_provider_presets",
					"refresh_provider_models", "validate_provider_models"
				], "description": "Action to perform"},

				"key":          {"type": "string", "description": "Configuration key (get_config, set_config, get_config_history)"},
				"value":        {"type": "string", "description": "Value to set (set_config)"},
				"scope_type":   {"type": "string", "description": "Scope type: global, workspace, or agent (set_config)"},
				"scope_id":     {"type": "string", "description": "Scope identifier (set_config)"},
				"workspace_id": {"type": "string", "description": "Workspace ID for scoped resolution (get_config)"},
				"agent_id":     {"type": "string", "description": "Agent ID (get_config, create_mcp_token, list_mcp_tokens)"},
				"actor":        {"type": "string", "description": "Identity of the actor (set_config)"},
				"limit":        {"type": "integer", "description": "Maximum results (get_config_history, default 20)"},

				"label":           {"type": "string", "description": "Human-readable label (create_mcp_token)"},
				"clearance_level": {"type": "integer", "description": "Clearance level for token (create_mcp_token, 0-2)"},
				"scopes":          {"type": "array", "items": {"type": "string"}, "description": "Scope restrictions (create_mcp_token)"},
				"expires_in":      {"type": "string", "description": "Expiry duration e.g. '24h', '7d' (create_mcp_token)"},
				"token_id":        {"type": "string", "description": "Token ID (revoke_mcp_token, rotate_mcp_token)"},

				"provider_id":    {"type": "string", "description": "Provider ID (get/update/delete/set_default/refresh)"},
				"name":           {"type": "string", "description": "Provider display name (create/update)"},
				"kind":           {"type": "string", "description": "Provider kind: openai, anthropic, google, etc. (create/update)"},
				"base_url":       {"type": "string", "description": "Provider API base URL (create/update)"},
				"secret_key_ref": {"type": "string", "description": "Secret key reference (create/update)"},
				"models":         {"type": "string", "description": "JSON array of model names (create/update)"},
				"metadata":       {"type": "string", "description": "JSON metadata object (create/update)"},
				"is_default":     {"type": "boolean", "description": "Set as default provider (create_provider)"},
				"is_enabled":     {"type": "boolean", "description": "Enable or disable provider (update_provider)"}
			},
			"required": ["action"]
		}`),
		h.dispatch,
	)
}

// dispatch routes the consolidated "config" tool to the correct handler method.
func (h *ConfigHandler) dispatch(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var envelope struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return nil, fmt.Errorf("handlers.ConfigHandler.dispatch: %w", err)
	}

	if err := checkActionClearance(ctx, envelope.Action, actionClearanceConfig); err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	switch envelope.Action {
	// Config actions
	case "get_config":
		return h.getConfig(ctx, params)
	case "set_config":
		return h.setConfig(ctx, params)
	case "list_config_keys":
		return h.listConfigKeys(ctx, params)
	case "get_config_history":
		return h.getConfigHistory(ctx, params)

	// Auth/token actions
	case "create_mcp_token":
		return h.createMCPToken(ctx, params)
	case "revoke_mcp_token":
		return h.revokeMCPToken(ctx, params)
	case "list_mcp_tokens":
		return h.listMCPTokens(ctx, params)
	case "rotate_mcp_token":
		return h.rotateMCPToken(ctx, params)

	// Provider actions
	case "list_providers":
		return h.listProviders(ctx, params)
	case "get_provider":
		return h.getProvider(ctx, params)
	case "create_provider":
		return h.createProvider(ctx, params)
	case "update_provider":
		return h.updateProvider(ctx, params)
	case "delete_provider":
		return h.deleteProvider(ctx, params)
	case "set_default_provider":
		return h.setDefaultProvider(ctx, params)
	case "get_default_provider":
		return h.getDefaultProvider(ctx, params)
	case "list_provider_presets":
		return h.listProviderPresets(ctx, params)
	case "refresh_provider_models":
		return h.refreshProviderModels(ctx, params)
	case "validate_provider_models":
		return h.validateProviderModels(ctx, params)

	default:
		return types.NewErrorResult(fmt.Sprintf("unknown config action %q", envelope.Action)), nil
	}
}

// ── Config actions ──────────────────────────────────────────────────────────

// getConfig resolves a configuration value using the scope chain.
func (h *ConfigHandler) getConfig(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Key         string `json:"key"`
		WorkspaceID string `json:"workspace_id"`
		AgentID     string `json:"agent_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ConfigHandler.getConfig: %w", err)
	}
	if args.Key == "" {
		return types.NewErrorResult("key is required"), nil
	}

	value, err := h.configStore.Resolve(ctx, args.Key, args.AgentID, args.WorkspaceID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("resolve config: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"key":   args.Key,
		"value": value,
	}), nil
}

// setConfig stores a configuration value at the specified scope.
func (h *ConfigHandler) setConfig(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Key       string `json:"key"`
		Value     string `json:"value"`
		ScopeType string `json:"scope_type"`
		ScopeID   string `json:"scope_id"`
		Actor     string `json:"actor"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ConfigHandler.setConfig: %w", err)
	}
	if args.Key == "" {
		return types.NewErrorResult("key is required"), nil
	}
	if args.Value == "" {
		return types.NewErrorResult("value is required"), nil
	}
	if args.ScopeType == "" {
		return types.NewErrorResult("scope_type is required"), nil
	}
	if args.Actor == "" {
		return types.NewErrorResult("actor is required"), nil
	}

	scope := types.ConfigScope{
		Type: args.ScopeType,
		ID:   args.ScopeID,
	}

	if err := h.configStore.Set(ctx, args.Key, args.Value, scope, args.Actor); err != nil {
		return types.NewErrorResult(fmt.Sprintf("set config: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"message":    fmt.Sprintf("Config key %q set to %q at scope %s.", args.Key, args.Value, args.ScopeType),
		"key":        args.Key,
		"value":      args.Value,
		"scope_type": args.ScopeType,
		"scope_id":   args.ScopeID,
	}), nil
}

// listConfigKeys returns all registered configuration key definitions.
func (h *ConfigHandler) listConfigKeys(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	keys, err := h.configStore.ListKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("handlers.ConfigHandler.listConfigKeys: %w", err)
	}

	if len(keys) == 0 {
		return types.NewToolResult([]interface{}{}), nil
	}

	return types.NewToolResult(keys), nil
}

// getConfigHistory returns the change history for a configuration key.
func (h *ConfigHandler) getConfigHistory(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Key   string `json:"key"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ConfigHandler.getConfigHistory: %w", err)
	}
	if args.Key == "" {
		return types.NewErrorResult("key is required"), nil
	}
	if args.Limit <= 0 {
		args.Limit = 20
	}

	history, err := h.configRepo.GetHistory(ctx, args.Key, args.Limit)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get config history: %v", err)), nil
	}

	if len(history) == 0 {
		return types.NewToolResult([]interface{}{}), nil
	}

	return types.NewToolResult(history), nil
}

// ── Auth/token actions ──────────────────────────────────────────────────────

// createMCPToken generates a cryptographically random 64-char token, bcrypt-hashes
// it, stores the hash, and returns the plaintext exactly once.
func (h *ConfigHandler) createMCPToken(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	if h.tokenRepo == nil {
		return types.NewErrorResult("token management not available"), nil
	}

	var args struct {
		AgentID        string   `json:"agent_id"`
		Label          string   `json:"label"`
		ClearanceLevel int      `json:"clearance_level"`
		Scopes         []string `json:"scopes"`
		ExpiresIn      string   `json:"expires_in"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return types.NewErrorResult("invalid parameters: " + err.Error()), nil
	}

	if args.AgentID == "" {
		return types.NewErrorResult("agent_id is required"), nil
	}

	// Verify the calling agent has permission to create tokens for the target.
	auth := mcp.AuthFromContext(ctx)
	if auth.Authenticated && auth.PersonaID != args.AgentID && auth.ClearanceLevel < 2 {
		return types.NewErrorResult("clearance level >= 2 required to create tokens for other agents"), nil
	}

	// Validate the target agent exists.
	agent, err := h.agentRepo.Get(ctx, args.AgentID)
	if err != nil {
		return types.NewErrorResult("agent not found: " + err.Error()), nil
	}

	// Token clearance cannot exceed the agent's clearance.
	if args.ClearanceLevel > agent.ClearanceLevel {
		return types.NewErrorResult(fmt.Sprintf(
			"token clearance %d exceeds agent clearance %d",
			args.ClearanceLevel, agent.ClearanceLevel,
		)), nil
	}

	// Parse optional expiry duration.
	var expiresAt *time.Time
	if args.ExpiresIn != "" {
		dur, err := parseDuration(args.ExpiresIn)
		if err != nil {
			return types.NewErrorResult("invalid expires_in: " + err.Error()), nil
		}
		t := time.Now().Add(dur)
		expiresAt = &t
	}

	// Generate a cryptographically random 64-character hex token.
	plaintext, err := generateToken(32) // 32 bytes = 64 hex chars
	if err != nil {
		return nil, fmt.Errorf("handlers.ConfigHandler.createMCPToken: generate token: %w", err)
	}

	// Bcrypt hash the token for storage.
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("handlers.ConfigHandler.createMCPToken: bcrypt hash: %w", err)
	}

	if args.Scopes == nil {
		args.Scopes = []string{}
	}

	token := &types.MCPToken{
		ID:             generateID(),
		AgentID:        args.AgentID,
		TokenHash:      string(hash),
		Label:          args.Label,
		ClearanceLevel: args.ClearanceLevel,
		Scopes:         args.Scopes,
		ExpiresAt:      expiresAt,
	}

	if err := h.tokenRepo.Create(ctx, token); err != nil {
		return types.NewErrorResult("failed to create token: " + err.Error()), nil
	}

	h.logger.Info("mcp token created",
		"token_id", token.ID,
		"agent_id", args.AgentID,
		"label", args.Label,
		"clearance", args.ClearanceLevel,
	)

	// Publish token.created audit event.
	if h.bus != nil {
		h.bus.Publish(nervous.NewEvent(
			types.EventTokenCreated,
			"auth_handler",
			"global",
			map[string]any{
				"token_id":        token.ID,
				"agent_id":        args.AgentID,
				"label":           args.Label,
				"clearance_level": args.ClearanceLevel,
			},
		))
	}

	result := map[string]any{
		"token_id":        token.ID,
		"plaintext_token": plaintext, // Returned exactly once.
		"agent_id":        args.AgentID,
		"label":           args.Label,
		"clearance_level": args.ClearanceLevel,
		"scopes":          args.Scopes,
		"expires_at":      token.ExpiresAt,
		"warning":         "Store this token securely. It cannot be retrieved again.",
	}

	return types.NewToolResult(result), nil
}

// revokeMCPToken immediately invalidates a token.
func (h *ConfigHandler) revokeMCPToken(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	if h.tokenRepo == nil {
		return types.NewErrorResult("token management not available"), nil
	}

	var args struct {
		TokenID string `json:"token_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return types.NewErrorResult("invalid parameters: " + err.Error()), nil
	}

	if args.TokenID == "" {
		return types.NewErrorResult("token_id is required"), nil
	}

	if err := h.tokenRepo.Revoke(ctx, args.TokenID); err != nil {
		return types.NewErrorResult("failed to revoke token: " + err.Error()), nil
	}

	h.logger.Info("mcp token revoked", "token_id", args.TokenID)

	// Publish token.revoked audit event.
	if h.bus != nil {
		h.bus.Publish(nervous.NewEvent(
			types.EventTokenRevoked,
			"auth_handler",
			"global",
			map[string]any{
				"token_id": args.TokenID,
			},
		))
	}

	return types.NewToolResult(map[string]any{
		"token_id": args.TokenID,
		"status":   "revoked",
	}), nil
}

// listMCPTokens returns all tokens for an agent (never exposes hashes).
func (h *ConfigHandler) listMCPTokens(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	if h.tokenRepo == nil {
		return types.NewErrorResult("token management not available"), nil
	}

	var args struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return types.NewErrorResult("invalid parameters: " + err.Error()), nil
	}

	if args.AgentID == "" {
		return types.NewErrorResult("agent_id is required"), nil
	}

	tokens, err := h.tokenRepo.ListByAgent(ctx, args.AgentID)
	if err != nil {
		return types.NewErrorResult("failed to list tokens: " + err.Error()), nil
	}

	// Build response without exposing hashes.
	items := make([]map[string]any, 0, len(tokens))
	for _, t := range tokens {
		item := map[string]any{
			"id":              t.ID,
			"label":           t.Label,
			"clearance_level": t.ClearanceLevel,
			"scopes":          t.Scopes,
			"created_at":      t.CreatedAt,
			"is_valid":        t.IsValid(),
		}
		if t.ExpiresAt != nil {
			item["expires_at"] = t.ExpiresAt
		}
		if t.RevokedAt != nil {
			item["revoked_at"] = t.RevokedAt
		}
		items = append(items, item)
	}

	return types.NewToolResult(map[string]any{
		"agent_id": args.AgentID,
		"tokens":   items,
		"count":    len(items),
	}), nil
}

// rotateMCPToken revokes an existing token and creates a new one with the same
// configuration (persona, clearance, scopes, label).
func (h *ConfigHandler) rotateMCPToken(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	if h.tokenRepo == nil {
		return types.NewErrorResult("token management not available"), nil
	}

	var args struct {
		TokenID string `json:"token_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return types.NewErrorResult("invalid parameters: " + err.Error()), nil
	}

	if args.TokenID == "" {
		return types.NewErrorResult("token_id is required"), nil
	}

	// Look up the existing token to copy its configuration.
	old, err := h.tokenRepo.GetByID(ctx, args.TokenID)
	if err != nil {
		return types.NewErrorResult("token not found: " + err.Error()), nil
	}

	// Revoke the old token.
	if err := h.tokenRepo.Revoke(ctx, args.TokenID); err != nil {
		return types.NewErrorResult("failed to revoke old token: " + err.Error()), nil
	}

	// Generate a new token with the same configuration.
	plaintext, err := generateToken(32)
	if err != nil {
		return nil, fmt.Errorf("handlers.ConfigHandler.rotateMCPToken: generate token: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("handlers.ConfigHandler.rotateMCPToken: bcrypt hash: %w", err)
	}

	newToken := &types.MCPToken{
		ID:             generateID(),
		AgentID:        old.AgentID,
		TokenHash:      string(hash),
		Label:          old.Label,
		ClearanceLevel: old.ClearanceLevel,
		Scopes:         old.Scopes,
		ExpiresAt:      old.ExpiresAt,
	}

	if err := h.tokenRepo.Create(ctx, newToken); err != nil {
		return types.NewErrorResult("failed to create new token: " + err.Error()), nil
	}

	h.logger.Info("mcp token rotated",
		"old_token_id", args.TokenID,
		"new_token_id", newToken.ID,
		"agent_id", newToken.AgentID,
	)

	// Publish token.rotated audit event.
	if h.bus != nil {
		h.bus.Publish(nervous.NewEvent(
			types.EventTokenRotated,
			"auth_handler",
			"global",
			map[string]any{
				"old_token_id": args.TokenID,
				"new_token_id": newToken.ID,
				"agent_id":     newToken.AgentID,
			},
		))
	}

	return types.NewToolResult(map[string]any{
		"old_token_id":    args.TokenID,
		"new_token_id":    newToken.ID,
		"plaintext_token": plaintext,
		"agent_id":        newToken.AgentID,
		"label":           newToken.Label,
		"clearance_level": newToken.ClearanceLevel,
		"scopes":          newToken.Scopes,
		"warning":         "Store this token securely. It cannot be retrieved again.",
	}), nil
}

// ── Provider actions ────────────────────────────────────────────────────────

func (h *ConfigHandler) listProviders(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	if h.providerStore == nil || h.providerStore.Providers == nil {
		return types.NewErrorResult("provider repo not available"), nil
	}

	providers, err := h.providerStore.Providers.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("handlers.ConfigHandler.listProviders: %w", err)
	}

	if len(providers) == 0 {
		return types.NewToolResult([]interface{}{}), nil
	}

	return types.NewToolResult(providers), nil
}

func (h *ConfigHandler) getProvider(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ProviderID string `json:"provider_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ConfigHandler.getProvider: %w", err)
	}
	if args.ProviderID == "" {
		return types.NewErrorResult("provider_id is required"), nil
	}

	if h.providerStore == nil || h.providerStore.Providers == nil {
		return types.NewErrorResult("provider repo not available"), nil
	}

	p, err := h.providerStore.Providers.Get(ctx, args.ProviderID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get provider: %v", err)), nil
	}

	return types.NewToolResult(p), nil
}

func (h *ConfigHandler) createProvider(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Name         string `json:"name"`
		Kind         string `json:"kind"`
		BaseURL      string `json:"base_url"`
		SecretKeyRef string `json:"secret_key_ref"`
		Models       string `json:"models"`
		Metadata     string `json:"metadata"`
		IsDefault    bool   `json:"is_default"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ConfigHandler.createProvider: %w", err)
	}
	if args.Name == "" {
		return types.NewErrorResult("name is required"), nil
	}
	if args.Kind == "" {
		return types.NewErrorResult("kind is required"), nil
	}
	if args.BaseURL == "" {
		return types.NewErrorResult("base_url is required"), nil
	}

	if h.providerStore == nil || h.providerStore.Providers == nil {
		return types.NewErrorResult("provider repo not available"), nil
	}

	// Default JSON values for empty fields.
	models := args.Models
	if models == "" {
		models = "[]"
	}
	metadata := args.Metadata
	if metadata == "" {
		metadata = "{}"
	}

	p := &repo.Provider{
		Name:         args.Name,
		Kind:         args.Kind,
		BaseURL:      args.BaseURL,
		SecretKeyRef: args.SecretKeyRef,
		IsDefault:    args.IsDefault,
		IsEnabled:    true,
		Models:       models,
		Metadata:     metadata,
	}

	id, err := h.providerStore.Providers.Create(ctx, p)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("create provider: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"id":      id,
		"message": fmt.Sprintf("Provider %q created.", args.Name),
	}), nil
}

func (h *ConfigHandler) updateProvider(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ProviderID   string `json:"provider_id"`
		Name         string `json:"name"`
		Kind         string `json:"kind"`
		BaseURL      string `json:"base_url"`
		SecretKeyRef string `json:"secret_key_ref"`
		Models       string `json:"models"`
		Metadata     string `json:"metadata"`
		IsEnabled    *bool  `json:"is_enabled"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ConfigHandler.updateProvider: %w", err)
	}
	if args.ProviderID == "" {
		return types.NewErrorResult("provider_id is required"), nil
	}

	if h.providerStore == nil || h.providerStore.Providers == nil {
		return types.NewErrorResult("provider repo not available"), nil
	}

	// Fetch existing provider to merge fields.
	existing, err := h.providerStore.Providers.Get(ctx, args.ProviderID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("provider not found: %v", err)), nil
	}

	// Track whether this update re-enables a previously disabled provider.
	wasDisabled := !existing.IsEnabled

	// Apply updates only for non-empty fields.
	if args.Name != "" {
		existing.Name = args.Name
	}
	if args.Kind != "" {
		existing.Kind = args.Kind
	}
	if args.BaseURL != "" {
		existing.BaseURL = args.BaseURL
	}
	if args.SecretKeyRef != "" {
		existing.SecretKeyRef = args.SecretKeyRef
	}
	if args.Models != "" {
		existing.Models = args.Models
	}
	if args.Metadata != "" {
		existing.Metadata = args.Metadata
	}
	if args.IsEnabled != nil {
		existing.IsEnabled = *args.IsEnabled
	}

	if err := h.providerStore.Providers.Update(ctx, args.ProviderID, existing); err != nil {
		return types.NewErrorResult(fmt.Sprintf("update provider: %v", err)), nil
	}

	// If the provider was re-enabled, reactivate agents that were suspended
	// due to the provider being disabled.
	if wasDisabled && existing.IsEnabled {
		h.reactivateSuspendedAgents(ctx, args.ProviderID, existing.Name)
	}

	return types.NewToolResult(fmt.Sprintf("Provider %q updated.", existing.Name)), nil
}

// reactivateSuspendedAgents finds agents using the given provider that were
// suspended with reason "provider disabled" and transitions them back to idle.
func (h *ConfigHandler) reactivateSuspendedAgents(ctx context.Context, providerID, providerName string) {
	if h.providerStore == nil || h.providerStore.Agents == nil {
		return
	}

	agents, err := h.providerStore.Agents.List(ctx)
	if err != nil {
		slog.Warn("provider re-enable: failed to list agents", "provider_id", providerID, "error", err)
		return
	}

	reactivated := 0
	for _, agent := range agents {
		if agent.ProviderID != providerID {
			continue
		}
		if agent.Status != string(types.AgentStateSuspended) || agent.StatusReason != "provider disabled" {
			continue
		}

		agent.Status = string(types.AgentStateIdle)
		agent.StatusReason = ""
		if updateErr := h.providerStore.Agents.Update(ctx, agent.ID, agent); updateErr != nil {
			slog.Warn("provider re-enable: failed to reactivate agent",
				"agent_id", agent.ID, "agent_name", agent.Name, "error", updateErr)
			continue
		}
		reactivated++

		slog.Info("provider re-enable: reactivated agent",
			"agent_id", agent.ID, "agent_name", agent.Name, "provider", providerName)
	}

	if reactivated > 0 && h.bus != nil {
		h.bus.Publish(nervous.NewEvent(
			types.EventProviderReEnabled,
			"provider",
			providerID,
			map[string]any{
				"provider_id":   providerID,
				"provider_name": providerName,
				"reactivated":   reactivated,
			},
		))
	}
}

func (h *ConfigHandler) deleteProvider(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ProviderID string `json:"provider_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ConfigHandler.deleteProvider: %w", err)
	}
	if args.ProviderID == "" {
		return types.NewErrorResult("provider_id is required"), nil
	}

	if h.providerStore == nil || h.providerStore.Providers == nil {
		return types.NewErrorResult("provider repo not available"), nil
	}

	if err := h.providerStore.Providers.Delete(ctx, args.ProviderID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("delete provider: %v", err)), nil
	}

	return types.NewToolResult(fmt.Sprintf("Provider %q deleted.", args.ProviderID)), nil
}

func (h *ConfigHandler) setDefaultProvider(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ProviderID string `json:"provider_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ConfigHandler.setDefaultProvider: %w", err)
	}
	if args.ProviderID == "" {
		return types.NewErrorResult("provider_id is required"), nil
	}

	if h.providerStore == nil || h.providerStore.Providers == nil {
		return types.NewErrorResult("provider repo not available"), nil
	}

	if err := h.providerStore.Providers.SetDefault(ctx, args.ProviderID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("set default provider: %v", err)), nil
	}

	return types.NewToolResult(fmt.Sprintf("Provider %q set as default.", args.ProviderID)), nil
}

func (h *ConfigHandler) getDefaultProvider(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	if h.providerStore == nil || h.providerStore.Providers == nil {
		return types.NewErrorResult("provider repo not available"), nil
	}

	p, err := h.providerStore.Providers.GetDefault(ctx)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get default provider: %v", err)), nil
	}

	return types.NewToolResult(p), nil
}

func (h *ConfigHandler) listProviderPresets(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	presets := provider.Presets()
	return types.NewToolResult(presets), nil
}

// refreshProviderModels discovers available models from a provider's API and
// persists the updated list back to the store.
func (h *ConfigHandler) refreshProviderModels(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ProviderID string `json:"provider_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.ConfigHandler.refreshProviderModels: %w", err)
	}
	if args.ProviderID == "" {
		return types.NewErrorResult("provider_id is required"), nil
	}

	if h.providerStore == nil || h.providerStore.Providers == nil {
		return types.NewErrorResult("provider repo not available"), nil
	}

	p, err := h.providerStore.Providers.Get(ctx, args.ProviderID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("provider not found: %v", err)), nil
	}

	// Resolve the API key from the secrets store if the provider references one.
	apiKey, err := h.resolveAPIKey(ctx, p.SecretKeyRef)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("resolve API key: %v", err)), nil
	}

	discovered, err := provider.DiscoverModels(ctx, p.Kind, p.BaseURL, apiKey)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("model discovery failed for %q (%s): %v", p.Name, p.Kind, err)), nil
	}

	// Marshal the discovered model list and update the provider record.
	modelsJSON, err := json.Marshal(discovered)
	if err != nil {
		return nil, fmt.Errorf("handlers.ConfigHandler.refreshProviderModels: marshal models: %w", err)
	}
	p.Models = string(modelsJSON)

	if err := h.providerStore.Providers.Update(ctx, args.ProviderID, p); err != nil {
		return nil, fmt.Errorf("handlers.ConfigHandler.refreshProviderModels: update provider models: %w", err)
	}

	slog.Info("refreshed provider models",
		"provider_id", args.ProviderID,
		"provider_name", p.Name,
		"model_count", len(discovered),
	)

	return types.NewToolResult(map[string]interface{}{
		"provider_id":   args.ProviderID,
		"provider_name": p.Name,
		"models":        discovered,
		"model_count":   len(discovered),
	}), nil
}

// modelValidationResult describes the outcome for a single agent.
type modelValidationResult struct {
	AgentID      string `json:"agent_id"`
	AgentName    string `json:"agent_name"`
	ProviderID   string `json:"provider_id"`
	ProviderName string `json:"provider_name"`
	Model        string `json:"model"`
	Valid        bool   `json:"valid"`
	Reason       string `json:"reason,omitempty"`
}

// validateProviderModels checks every persona's assigned model against the
// live model list from its provider.
func (h *ConfigHandler) validateProviderModels(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	if h.providerStore == nil || h.providerStore.Providers == nil {
		return types.NewErrorResult("provider repo not available"), nil
	}
	if h.providerStore.Agents == nil {
		return types.NewErrorResult("agent repo not available"), nil
	}

	// 1. List all providers and build a lookup by ID.
	providers, err := h.providerStore.Providers.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("handlers.ConfigHandler.validateProviderModels: list providers: %w", err)
	}

	type providerInfo struct {
		provider *repo.Provider
		models   []string // live-discovered models
	}
	providerMap := make(map[string]*providerInfo, len(providers))

	for _, p := range providers {
		if !p.IsEnabled {
			continue
		}

		apiKey, keyErr := h.resolveAPIKey(ctx, p.SecretKeyRef)
		if keyErr != nil {
			slog.Warn("skipping provider: cannot resolve API key",
				"provider_id", p.ID,
				"provider_name", p.Name,
				"error", keyErr,
			)
			continue
		}

		discovered, discErr := provider.DiscoverModels(ctx, p.Kind, p.BaseURL, apiKey)
		if discErr != nil {
			slog.Warn("skipping provider: model discovery failed",
				"provider_id", p.ID,
				"provider_name", p.Name,
				"error", discErr,
			)
			continue
		}

		providerMap[p.ID] = &providerInfo{
			provider: p,
			models:   discovered,
		}
	}

	// 2. List all agents and validate each one with a provider+model assignment.
	agents, err := h.providerStore.Agents.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("handlers.ConfigHandler.validateProviderModels: list agents: %w", err)
	}

	var results []modelValidationResult

	for _, agent := range agents {
		// Skip agents without a provider or model assignment.
		if agent.ProviderID == "" || agent.DefaultModel == "" {
			continue
		}

		info, ok := providerMap[agent.ProviderID]
		if !ok {
			// Provider not reachable or disabled — record as unknown.
			results = append(results, modelValidationResult{
				AgentID:      agent.ID,
				AgentName:    agent.Name,
				ProviderID:   agent.ProviderID,
				ProviderName: "(unavailable)",
				Model:        agent.DefaultModel,
				Valid:        false,
				Reason:       "provider not reachable or disabled",
			})
			continue
		}

		valid := provider.ValidateModel(info.models, agent.DefaultModel)

		result := modelValidationResult{
			AgentID:      agent.ID,
			AgentName:    agent.Name,
			ProviderID:   agent.ProviderID,
			ProviderName: info.provider.Name,
			Model:        agent.DefaultModel,
			Valid:        valid,
		}

		if !valid {
			result.Reason = fmt.Sprintf("model %q no longer available on provider %q", agent.DefaultModel, info.provider.Name)

			// Log a lifecycle transition to error state if the lifecycle repo
			// is wired.
			if h.providerStore.Lifecycle != nil {
				transErr := h.providerStore.Lifecycle.LogTransition(ctx, &repo.LifecycleTransition{
					AgentID:   agent.ID,
					FromState: "active",
					ToState:   string(types.AgentStateError),
					Reason:    result.Reason,
				})
				if transErr != nil {
					slog.Error("failed to log lifecycle transition",
						"agent_id", agent.ID,
						"error", transErr,
					)
				}
			}
		}

		results = append(results, result)
	}

	// Build summary counts.
	validCount := 0
	invalidCount := 0
	for _, r := range results {
		if r.Valid {
			validCount++
		} else {
			invalidCount++
		}
	}

	return types.NewToolResult(map[string]interface{}{
		"total_checked": len(results),
		"valid":         validCount,
		"invalid":       invalidCount,
		"results":       results,
	}), nil
}

// resolveAPIKey retrieves the API key from the secrets store for the given
// secret reference. If the reference is empty the provider does not require
// a key (e.g. Ollama), so an empty string is returned with no error.
func (h *ConfigHandler) resolveAPIKey(ctx context.Context, secretKeyRef string) (string, error) {
	if secretKeyRef == "" {
		return "", nil
	}
	if h.providerStore == nil || h.providerStore.Secrets == nil {
		return "", fmt.Errorf("secrets repo not available")
	}
	apiKey, err := h.providerStore.Secrets.Get(ctx, secretKeyRef, "global")
	if err != nil {
		return "", fmt.Errorf("get secret %q: %w", secretKeyRef, err)
	}
	return apiKey, nil
}

// ── Shared helpers ──────────────────────────────────────────────────────────

// generateToken creates a cryptographically random hex-encoded token.
func generateToken(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("handlers.generateToken: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// generateID creates a short random ID for tokens.
func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// parseDuration parses durations like "24h", "7d", "30d" into time.Duration.
func parseDuration(s string) (time.Duration, error) {
	// Handle day suffix (not supported by time.ParseDuration).
	if len(s) > 1 && s[len(s)-1] == 'd' {
		var days int
		if _, err := fmt.Sscanf(s, "%dd", &days); err == nil && days > 0 {
			return time.Duration(days) * 24 * time.Hour, nil
		}
	}
	return time.ParseDuration(s)
}
