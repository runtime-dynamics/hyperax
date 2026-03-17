package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hyperax/hyperax/internal/delegation"
	"github.com/hyperax/hyperax/internal/hints"
	"github.com/hyperax/hyperax/internal/lifecycle"
	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/memory"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/internal/role"
	"github.com/hyperax/hyperax/internal/storage"
	"github.com/hyperax/hyperax/pkg/types"
)

// actionClearanceAgent maps each agent action to its minimum ABAC clearance.
var actionClearanceAgent = map[string]int{
	// Agent CRUD
	"list_agents":  0,
	"get_agent":    0,
	"create_agent": 1,
	"update_agent": 1,
	"delete_agent": 1,
	// Role templates
	"list_role_templates":           0,
	"override_role_template":        2,
	"remove_role_template_override": 2,
	"get_effective_engagement_rules": 0,
	// Lifecycle
	"log_agent_transition": 1,
	"get_agent_state":      0,
	"heartbeat":            1,
	"get_stale_agents":     0,
	// Hints
	"get_hints":               0,
	"list_hint_providers":     0,
	"configure_hint_provider": 1,
	// Delegation
	"grant_delegation":          1,
	"revoke_delegation":         1,
	"list_delegations":          0,
	"get_delegation_credential": 2,
	// Onboarding
	"onboard_agent": 1,
}

// AgentHandler implements the consolidated "agent" MCP tool covering agent CRUD,
// lifecycle, hints, delegation, and onboarding.
type AgentHandler struct {
	store            *storage.Store
	templateRegistry *role.RoleTemplateRegistry
	bus              *nervous.EventBus
	hintsEngine      *hints.Engine
	delegationSvc    *delegation.Service
	memoryStore      *memory.MemoryStore
}

// NewAgentHandler creates an AgentHandler with its own RoleTemplateRegistry.
func NewAgentHandler(store *storage.Store) *AgentHandler {
	return &AgentHandler{
		store:            store,
		templateRegistry: role.NewRoleTemplateRegistry(store.Config),
	}
}

// SetTemplateRegistry overrides the role template registry (for testing or custom wiring).
func (h *AgentHandler) SetTemplateRegistry(registry *role.RoleTemplateRegistry) {
	h.templateRegistry = registry
}

// TemplateRegistry returns the handler's role template registry so it can be
// shared with other components (e.g. ChatAPI for runtime system prompt resolution).
func (h *AgentHandler) TemplateRegistry() *role.RoleTemplateRegistry {
	return h.templateRegistry
}

// SetLifecycleDeps wires the EventBus dependency for lifecycle tools.
func (h *AgentHandler) SetLifecycleDeps(bus *nervous.EventBus) {
	h.bus = bus
}

// SetHintsDeps wires the Hints Engine dependency.
func (h *AgentHandler) SetHintsDeps(engine *hints.Engine) {
	h.hintsEngine = engine
}

// SetDelegationDeps wires the Delegation Service dependency.
func (h *AgentHandler) SetDelegationDeps(svc *delegation.Service) {
	h.delegationSvc = svc
}

// SetMemoryDeps wires the Memory Store dependency for onboard_agent.
func (h *AgentHandler) SetMemoryDeps(ms *memory.MemoryStore) {
	h.memoryStore = ms
}

// RegisterTools registers the consolidated "agent" MCP tool.
func (h *AgentHandler) RegisterTools(registry *mcp.ToolRegistry) {
	registry.Register(
		"agent",
		"Agent management: CRUD, lifecycle, hints, delegation, templates, and onboarding. "+
			"Actions: list_agents | get_agent | create_agent | update_agent | delete_agent | "+
			"list_role_templates | override_role_template | remove_role_template_override | get_effective_engagement_rules | "+
			"log_agent_transition | get_agent_state | heartbeat | get_stale_agents | "+
			"get_hints | list_hint_providers | configure_hint_provider | "+
			"grant_delegation | revoke_delegation | list_delegations | get_delegation_credential | "+
			"onboard_agent",
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": [
						"list_agents", "get_agent", "create_agent", "update_agent", "delete_agent",
						"list_role_templates", "override_role_template", "remove_role_template_override",
						"get_effective_engagement_rules",
						"log_agent_transition", "get_agent_state", "heartbeat", "get_stale_agents",
						"get_hints", "list_hint_providers", "configure_hint_provider",
						"grant_delegation", "revoke_delegation", "list_delegations", "get_delegation_credential",
						"onboard_agent"
					],
					"description": "The agent action to perform"
				},
				"agent_id":           {"type": "string", "description": "Agent ID"},
				"name":               {"type": "string", "description": "Agent or provider name"},
				"personality":        {"type": "string", "description": "Agent personality/role description"},
				"system_prompt":      {"type": "string", "description": "System prompt for LLM interactions or role template override"},
				"role_template_id":   {"type": "string", "description": "ID of a role template"},
				"role_template":      {"type": "string", "description": "Name of a built-in role template to apply"},
				"template_id":        {"type": "string", "description": "Template ID (for override/remove actions)"},
				"clearance_level":    {"type": "integer", "description": "Clearance level (0-3)"},
				"provider_id":        {"type": "string", "description": "LLM provider ID"},
				"default_model":      {"type": "string", "description": "Default model name (work model for tool-use)"},
				"chat_model":         {"type": "string", "description": "Chat model name (cheap model for conversational responses)"},
				"parent_agent_id":    {"type": "string", "description": "Parent agent ID for hierarchy"},
				"workspace_id":       {"type": "string", "description": "Workspace ID"},
				"status":             {"type": "string", "description": "Agent status"},
				"is_internal":        {"type": "boolean", "description": "Mark as internal system agent"},
				"guard_bypass":       {"type": "boolean", "description": "Allow guard bypass"},
				"engagement_rules":   {"type": "string", "description": "JSON array of engagement rules"},
				"from_state":         {"type": "string", "description": "State transitioning from (lifecycle)"},
				"to_state":           {"type": "string", "description": "State transitioning to (lifecycle)"},
				"reason":             {"type": "string", "description": "Reason for transition or delegation"},
				"ttl_seconds":        {"type": "integer", "description": "Heartbeat TTL in seconds"},
				"query":              {"type": "string", "description": "Search query (hints/recall)"},
				"file_path":          {"type": "string", "description": "File path for hints"},
				"language":           {"type": "string", "description": "Programming language for hints"},
				"max_results":        {"type": "integer", "description": "Max results"},
				"providers":          {"type": "string", "description": "Comma-separated hint provider names"},
				"enabled":            {"type": "boolean", "description": "Enable/disable hint provider"},
				"granter_id":         {"type": "string", "description": "Delegation granter persona ID"},
				"grantee_id":         {"type": "string", "description": "Delegation grantee persona ID"},
				"grant_type":         {"type": "string", "description": "Delegation type: clearance_elevation, credential_passthrough, scope_access"},
				"credential":         {"type": "string", "description": "Credential value for delegation"},
				"elevated_level":     {"type": "integer", "description": "Target clearance level for delegation"},
				"scopes":             {"type": "array", "items": {"type": "string"}, "description": "Scopes for delegation"},
				"expires_at":         {"type": "string", "description": "Expiry timestamp (RFC3339)"},
				"delegation_id":      {"type": "string", "description": "Delegation ID"},
				"requester_id":       {"type": "string", "description": "Requester persona ID for credential access"},
				"persona_id":         {"type": "string", "description": "Persona ID (delegation/onboarding)"},
				"role":               {"type": "string", "description": "Query role: grantee or granter"}
			},
			"required": ["action"]
		}`),
		h.dispatch,
	)
}

// dispatch routes the consolidated "agent" tool to the appropriate handler method.
func (h *AgentHandler) dispatch(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var envelope struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return nil, fmt.Errorf("handlers.AgentHandler.dispatch: %w", err)
	}

	if err := checkActionClearance(ctx, envelope.Action, actionClearanceAgent); err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	switch envelope.Action {
	// Agent CRUD
	case "list_agents":
		return h.listAgents(ctx, params)
	case "get_agent":
		return h.getAgent(ctx, params)
	case "create_agent":
		return h.createAgent(ctx, params)
	case "update_agent":
		return h.updateAgent(ctx, params)
	case "delete_agent":
		return h.deleteAgent(ctx, params)
	// Role templates
	case "list_role_templates":
		return h.listRoleTemplates(ctx, params)
	case "override_role_template":
		return h.overrideRoleTemplate(ctx, params)
	case "remove_role_template_override":
		return h.removeRoleTemplateOverride(ctx, params)
	case "get_effective_engagement_rules":
		return h.getEffectiveEngagementRules(ctx, params)
	// Lifecycle
	case "log_agent_transition":
		return h.logAgentTransition(ctx, params)
	case "get_agent_state":
		return h.getAgentState(ctx, params)
	case "heartbeat":
		return h.heartbeat(ctx, params)
	case "get_stale_agents":
		return h.getStaleAgents(ctx, params)
	// Hints
	case "get_hints":
		return h.getHints(ctx, params)
	case "list_hint_providers":
		return h.listHintProviders(ctx, params)
	case "configure_hint_provider":
		return h.configureHintProvider(ctx, params)
	// Delegation
	case "grant_delegation":
		return h.grantDelegation(ctx, params)
	case "revoke_delegation":
		return h.revokeDelegation(ctx, params)
	case "list_delegations":
		return h.listDelegations(ctx, params)
	case "get_delegation_credential":
		return h.getDelegationCredential(ctx, params)
	// Onboarding
	case "onboard_agent":
		return h.onboardAgent(ctx, params)
	default:
		return types.NewErrorResult(fmt.Sprintf("unknown agent action %q", envelope.Action)), nil
	}
}

func (h *AgentHandler) createAgent(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Name            string `json:"name"`
		Personality     string `json:"personality"`
		SystemPrompt    string `json:"system_prompt"`
		RoleTemplateID  string `json:"role_template_id"`
		RoleTemplate    string `json:"role_template"`
		ClearanceLevel  int    `json:"clearance_level"`
		ProviderID      string `json:"provider_id"`
		DefaultModel    string `json:"default_model"`
		ChatModel       string `json:"chat_model"`
		ParentAgentID   string `json:"parent_agent_id"`
		WorkspaceID     string `json:"workspace_id"`
		Status          string `json:"status"`
		IsInternal      bool   `json:"is_internal"`
		GuardBypass     bool   `json:"guard_bypass"`
		EngagementRules string `json:"engagement_rules"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.AgentHandler.createAgent: %w", err)
	}
	if args.Name == "" {
		return types.NewErrorResult("name is required"), nil
	}

	if h.store.Agents == nil {
		return types.NewErrorResult("agent repo not available"), nil
	}

	// Apply role template defaults if specified.
	templateApplied := ""
	if args.RoleTemplate != "" && h.templateRegistry != nil {
		tmpl := h.templateRegistry.Get(args.RoleTemplate)
		if tmpl == nil {
			return types.NewErrorResult(fmt.Sprintf("role template %q not found", args.RoleTemplate)), nil
		}
		args.RoleTemplateID = tmpl.ID
		if args.ClearanceLevel == 0 && tmpl.ClearanceLevel > 0 {
			args.ClearanceLevel = tmpl.ClearanceLevel
		}
		if args.DefaultModel == "" {
			args.DefaultModel = tmpl.SuggestedModel
		}
		if args.Personality == "" {
			args.Personality = tmpl.Description
		}
		templateApplied = tmpl.ID
	}

	// Prevent duplicate internal agents.
	if args.IsInternal {
		existing, err := h.store.Agents.GetByName(ctx, args.Name)
		if err == nil && existing.IsInternal {
			return types.NewErrorResult(fmt.Sprintf("internal agent %q already exists (id: %s)", args.Name, existing.ID)), nil
		}
	}

	// Validate parent agent exists if specified.
	if args.ParentAgentID != "" {
		if _, err := h.store.Agents.Get(ctx, args.ParentAgentID); err != nil {
			return types.NewErrorResult(fmt.Sprintf("parent agent not found: %v", err)), nil
		}
	}
	a := &repo.Agent{
		Name:            args.Name,
		Personality:     args.Personality,
		SystemPrompt:    args.SystemPrompt,
		RoleTemplateID:  args.RoleTemplateID,
		ClearanceLevel:  args.ClearanceLevel,
		ProviderID:      args.ProviderID,
		DefaultModel:    args.DefaultModel,
		ChatModel:       args.ChatModel,
		ParentAgentID:   args.ParentAgentID,
		WorkspaceID:     args.WorkspaceID,
		Status:          args.Status,
		IsInternal:      args.IsInternal,
		GuardBypass:     args.GuardBypass,
		EngagementRules: args.EngagementRules,
	}

	id, err := h.store.Agents.Create(ctx, a)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("create agent: %v", err)), nil
	}

	result := map[string]any{
		"id":      id,
		"message": fmt.Sprintf("Agent %q created.", args.Name),
	}
	if templateApplied != "" {
		result["template_applied"] = templateApplied
	}

	return types.NewToolResult(result), nil
}

func (h *AgentHandler) listAgents(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	if h.store.Agents == nil {
		return types.NewErrorResult("agent repo not available"), nil
	}

	agents, err := h.store.Agents.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("handlers.AgentHandler.listAgents: %w", err)
	}

	if len(agents) == 0 {
		return types.NewToolResult([]interface{}{}), nil
	}

	return types.NewToolResult(agents), nil
}

func (h *AgentHandler) getAgent(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.AgentHandler.getAgent: %w", err)
	}
	if args.AgentID == "" {
		return types.NewErrorResult("agent_id is required"), nil
	}

	if h.store.Agents == nil {
		return types.NewErrorResult("agent repo not available"), nil
	}

	agent, err := h.store.Agents.Get(ctx, args.AgentID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("agent not found: %v", err)), nil
	}

	return types.NewToolResult(agent), nil
}

func (h *AgentHandler) updateAgent(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		AgentID         string  `json:"agent_id"`
		Name            string  `json:"name"`
		Personality     string  `json:"personality"`
		SystemPrompt    string  `json:"system_prompt"`
		RoleTemplateID  string  `json:"role_template_id"`
		ClearanceLevel  *int    `json:"clearance_level"`
		ProviderID      string  `json:"provider_id"`
		DefaultModel    string  `json:"default_model"`
		ChatModel       *string `json:"chat_model"`
		ParentAgentID   *string `json:"parent_agent_id"`
		WorkspaceID     string  `json:"workspace_id"`
		Status          string  `json:"status"`
		GuardBypass     *bool   `json:"guard_bypass"`
		EngagementRules *string `json:"engagement_rules"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.AgentHandler.updateAgent: %w", err)
	}
	if args.AgentID == "" {
		return types.NewErrorResult("agent_id is required"), nil
	}

	if h.store.Agents == nil {
		return types.NewErrorResult("agent repo not available"), nil
	}

	existing, err := h.store.Agents.Get(ctx, args.AgentID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("agent not found: %v", err)), nil
	}

	// Capture the original name before patching so we can detect renames.
	oldName := existing.Name

	// Track whether provider/model changed so we can auto-recover error agents.
	providerOrModelChanged := false

	// Internal agents only allow provider_id, default_model, and chat_model changes.
	if existing.IsInternal {
		if args.ProviderID != "" {
			existing.ProviderID = args.ProviderID
			providerOrModelChanged = true
		}
		if args.DefaultModel != "" {
			existing.DefaultModel = args.DefaultModel
			providerOrModelChanged = true
		}
		if args.ChatModel != nil {
			existing.ChatModel = *args.ChatModel
		}
	} else {
		if args.Name != "" {
			existing.Name = args.Name
		}
		if args.Personality != "" {
			existing.Personality = args.Personality
		}
		if args.SystemPrompt != "" {
			existing.SystemPrompt = args.SystemPrompt
		}
		if args.RoleTemplateID != "" {
			existing.RoleTemplateID = args.RoleTemplateID
			if args.SystemPrompt == "" {
				existing.SystemPrompt = ""
			}
		}
		if args.ClearanceLevel != nil {
			existing.ClearanceLevel = *args.ClearanceLevel
		}
		if args.ProviderID != "" {
			existing.ProviderID = args.ProviderID
			providerOrModelChanged = true
		}
		if args.DefaultModel != "" {
			existing.DefaultModel = args.DefaultModel
			providerOrModelChanged = true
		}
		if args.ChatModel != nil {
			existing.ChatModel = *args.ChatModel
		}
		if args.ParentAgentID != nil {
			existing.ParentAgentID = *args.ParentAgentID
		}
		if args.WorkspaceID != "" {
			existing.WorkspaceID = args.WorkspaceID
		}
		if args.Status != "" {
			existing.Status = args.Status
			// When explicitly resetting to idle, clear any stale error reason
			// so the UI no longer displays the old error text.
			if args.Status == repo.AgentStatusIdle {
				existing.StatusReason = ""
			}
		}
		if args.GuardBypass != nil {
			existing.GuardBypass = *args.GuardBypass
		}
		if args.EngagementRules != nil {
			existing.EngagementRules = *args.EngagementRules
		}
	}

	// Auto-recover: if the agent is in error state and the user changed the
	// provider or model, reset to idle so the scheduler picks it up again.
	if providerOrModelChanged && existing.Status == repo.AgentStatusError {
		existing.Status = repo.AgentStatusIdle
		existing.StatusReason = ""
	}

	if err := h.store.Agents.Update(ctx, args.AgentID, existing); err != nil {
		return types.NewErrorResult(fmt.Sprintf("update agent: %v", err)), nil
	}

	// Cascade name change to comm_log, chat_sessions, and agent_relationships.
	// These tables key on agent name, so references must be updated to prevent
	// orphaning conversation history and hierarchy entries.
	if existing.Name != oldName {
		if h.store.CommHub != nil {
			if err := h.store.CommHub.RenameAgentRefs(ctx, oldName, existing.Name); err != nil {
				slog.Warn("failed to rename agent refs in comm_log", "old", oldName, "new", existing.Name, "error", err)
			}
		}
		if h.store.Sessions != nil {
			if err := h.store.Sessions.RenameAgent(ctx, oldName, existing.Name); err != nil {
				slog.Warn("failed to rename agent in sessions", "old", oldName, "new", existing.Name, "error", err)
			}
		}
		if h.store.WorkQueue != nil {
			if err := h.store.WorkQueue.RenameAgent(ctx, oldName, existing.Name); err != nil {
				slog.Warn("failed to rename agent in work queue", "old", oldName, "new", existing.Name, "error", err)
			}
		}
	}

	return types.NewToolResult(fmt.Sprintf("Agent %q updated.", existing.Name)), nil
}

func (h *AgentHandler) deleteAgent(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.AgentHandler.deleteAgent: %w", err)
	}
	if args.AgentID == "" {
		return types.NewErrorResult("agent_id is required"), nil
	}

	if h.store.Agents == nil {
		return types.NewErrorResult("agent repo not available"), nil
	}

	// Block deletion of internal agents.
	existing, err := h.store.Agents.Get(ctx, args.AgentID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("agent not found: %v", err)), nil
	}
	if existing.IsInternal {
		return types.NewErrorResult("cannot delete internal agent"), nil
	}

	if err := h.store.Agents.Delete(ctx, args.AgentID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("delete agent: %v", err)), nil
	}

	return types.NewToolResult(fmt.Sprintf("Agent %q deleted.", args.AgentID)), nil
}

// listRoleTemplates returns all available role templates (built-in and custom).
func (h *AgentHandler) listRoleTemplates(_ context.Context, _ json.RawMessage) (*types.ToolResult, error) {
	if h.templateRegistry == nil {
		return types.NewErrorResult("template registry not available"), nil
	}

	templates := h.templateRegistry.List()
	items := make([]map[string]any, 0, len(templates))
	for _, t := range templates {
		item := map[string]any{
			"id":              t.ID,
			"name":            t.Name,
			"description":     t.Description,
			"system_prompt":   t.SystemPrompt,
			"suggested_model": t.SuggestedModel,
			"clearance_level": t.ClearanceLevel,
			"built_in":        t.BuiltIn,
			"has_override":    t.HasOverride,
		}
		if len(t.EngagementRules) > 0 {
			item["engagement_rules"] = t.EngagementRules
		}
		items = append(items, item)
	}

	return types.NewToolResult(map[string]any{
		"templates": items,
		"count":     len(items),
	}), nil
}

func (h *AgentHandler) overrideRoleTemplate(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		TemplateID   string `json:"template_id"`
		SystemPrompt string `json:"system_prompt"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.AgentHandler.overrideRoleTemplate: %w", err)
	}
	if args.TemplateID == "" {
		return types.NewErrorResult("template_id is required"), nil
	}
	if args.SystemPrompt == "" {
		return types.NewErrorResult("system_prompt is required"), nil
	}
	if h.templateRegistry == nil {
		return types.NewErrorResult("template registry not available"), nil
	}
	if err := h.templateRegistry.SetOverride(ctx, args.TemplateID, args.SystemPrompt); err != nil {
		return types.NewErrorResult(fmt.Sprintf("override failed: %v", err)), nil
	}
	return types.NewToolResult(fmt.Sprintf("Override set for template %q.", args.TemplateID)), nil
}

func (h *AgentHandler) removeRoleTemplateOverride(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		TemplateID string `json:"template_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.AgentHandler.removeRoleTemplateOverride: %w", err)
	}
	if args.TemplateID == "" {
		return types.NewErrorResult("template_id is required"), nil
	}
	if h.templateRegistry == nil {
		return types.NewErrorResult("template registry not available"), nil
	}
	if err := h.templateRegistry.RemoveOverride(ctx, args.TemplateID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("remove override failed: %v", err)), nil
	}
	return types.NewToolResult(fmt.Sprintf("Override removed for template %q.", args.TemplateID)), nil
}

// getEffectiveEngagementRules merges template defaults with agent-level overrides
// and resolves each chain step's role to an actual agent (if one exists).
func (h *AgentHandler) getEffectiveEngagementRules(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.AgentHandler.getEffectiveEngagementRules: %w", err)
	}
	if args.AgentID == "" {
		return types.NewErrorResult("agent_id is required"), nil
	}
	if h.store.Agents == nil {
		return types.NewErrorResult("agent repo not available"), nil
	}

	agent, err := h.store.Agents.Get(ctx, args.AgentID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("agent not found: %v", err)), nil
	}

	// Get template rules (if agent has a role template).
	var templateRules []role.EngagementRule
	if agent.RoleTemplateID != "" && h.templateRegistry != nil {
		tmpl := h.templateRegistry.Get(agent.RoleTemplateID)
		if tmpl != nil {
			templateRules = tmpl.EngagementRules
		}
	}

	// Parse agent-level overrides from JSON string.
	var agentRules []role.EngagementRule
	if agent.EngagementRules != "" {
		if err := json.Unmarshal([]byte(agent.EngagementRules), &agentRules); err != nil {
			// Ignore malformed JSON, just use template defaults.
			agentRules = nil
		}
	}

	// Merge template + agent rules.
	merged := role.MergeEngagementRules(templateRules, agentRules)

	// Build a role->agent lookup for resolution.
	allAgents, err := h.store.Agents.List(ctx)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("list agents: %v", err)), nil
	}
	roleAgentMap := make(map[string]*repo.Agent, len(allAgents))
	for _, a := range allAgents {
		if a.RoleTemplateID != "" {
			if _, exists := roleAgentMap[a.RoleTemplateID]; !exists {
				roleAgentMap[a.RoleTemplateID] = a
			}
		}
	}

	// Determine which rule IDs came from agent overrides.
	agentRuleIDs := make(map[string]bool, len(agentRules))
	for _, ar := range agentRules {
		agentRuleIDs[ar.ID] = true
	}

	type resolvedStep struct {
		Role       string `json:"role"`
		Action     string `json:"action"`
		AgentName  string `json:"agent_name,omitempty"`
		AgentID    string `json:"agent_id,omitempty"`
		Unassigned bool   `json:"unassigned,omitempty"`
	}
	type resolvedRule struct {
		ID       string         `json:"id"`
		Trigger  string         `json:"trigger"`
		Color    string         `json:"color"`
		Chain    []resolvedStep `json:"chain"`
		Source   string         `json:"source"`
		Disabled bool           `json:"disabled,omitempty"`
	}

	result := make([]resolvedRule, 0, len(merged))
	for _, rule := range merged {
		source := "template"
		if agentRuleIDs[rule.ID] {
			source = "custom"
		}

		chain := make([]resolvedStep, 0, len(rule.Chain))
		for _, step := range rule.Chain {
			rs := resolvedStep{
				Role:   step.Role,
				Action: step.Action,
			}
			if a, ok := roleAgentMap[step.Role]; ok {
				rs.AgentName = a.Name
				rs.AgentID = a.ID
			} else {
				rs.Unassigned = true
			}
			chain = append(chain, rs)
		}

		result = append(result, resolvedRule{
			ID:       rule.ID,
			Trigger:  rule.Trigger,
			Color:    rule.Color,
			Chain:    chain,
			Source:   source,
			Disabled: rule.Disabled,
		})
	}

	return types.NewToolResult(result), nil
}

// ── Lifecycle actions ──────────────────────────────────────────────────────────

func (h *AgentHandler) logAgentTransition(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		AgentID   string `json:"agent_id"`
		FromState string `json:"from_state"`
		ToState   string `json:"to_state"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.AgentHandler.logAgentTransition: %w", err)
	}
	if args.AgentID == "" {
		return types.NewErrorResult("agent_id is required"), nil
	}
	if args.FromState == "" {
		return types.NewErrorResult("from_state is required"), nil
	}
	if args.ToState == "" {
		return types.NewErrorResult("to_state is required"), nil
	}

	if h.store.Lifecycle == nil {
		return types.NewErrorResult("lifecycle repo not available"), nil
	}

	if err := lifecycle.ValidateTransition(
		lifecycle.State(args.FromState),
		lifecycle.State(args.ToState),
	); err != nil {
		return types.NewErrorResult(fmt.Sprintf("invalid transition: %v", err)), nil
	}

	entry := &repo.LifecycleTransition{
		AgentID:   args.AgentID,
		FromState: args.FromState,
		ToState:   args.ToState,
		Reason:    args.Reason,
	}

	if err := h.store.Lifecycle.LogTransition(ctx, entry); err != nil {
		return types.NewErrorResult(fmt.Sprintf("log transition: %v", err)), nil
	}

	if h.bus != nil {
		h.bus.Publish(nervous.NewEvent(
			types.EventLifecycleTransition,
			"lifecycle_handler",
			"global",
			map[string]string{
				"agent_id":   args.AgentID,
				"from_state": args.FromState,
				"to_state":   args.ToState,
				"reason":     args.Reason,
			},
		))
	}

	return types.NewToolResult(map[string]string{
		"agent_id":   args.AgentID,
		"from_state": args.FromState,
		"to_state":   args.ToState,
		"status":     "recorded",
	}), nil
}

func (h *AgentHandler) getAgentState(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.AgentHandler.getAgentState: %w", err)
	}
	if args.AgentID == "" {
		return types.NewErrorResult("agent_id is required"), nil
	}

	if h.store.Lifecycle == nil {
		return types.NewErrorResult("lifecycle repo not available"), nil
	}

	state, err := h.store.Lifecycle.GetState(ctx, args.AgentID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get agent state: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"agent_id": args.AgentID,
		"state":    state,
	}), nil
}

func (h *AgentHandler) heartbeat(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.AgentHandler.heartbeat: %w", err)
	}
	if args.AgentID == "" {
		return types.NewErrorResult("agent_id is required"), nil
	}

	if h.store.Lifecycle == nil {
		return types.NewErrorResult("lifecycle repo not available"), nil
	}

	if err := h.store.Lifecycle.WriteHeartbeat(ctx, args.AgentID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("write heartbeat: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"agent_id": args.AgentID,
		"status":   "recorded",
	}), nil
}

func (h *AgentHandler) getStaleAgents(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		TTLSeconds int `json:"ttl_seconds"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.AgentHandler.getStaleAgents: %w", err)
	}

	if args.TTLSeconds <= 0 {
		args.TTLSeconds = 300
	}

	if h.store.Lifecycle == nil {
		return types.NewErrorResult("lifecycle repo not available"), nil
	}

	ttl := time.Duration(args.TTLSeconds) * time.Second
	stale, err := h.store.Lifecycle.GetStaleAgents(ctx, ttl)
	if err != nil {
		return nil, fmt.Errorf("handlers.AgentHandler.getStaleAgents: %w", err)
	}

	if len(stale) == 0 {
		return types.NewToolResult([]interface{}{}), nil
	}

	return types.NewToolResult(stale), nil
}

// ── Hints actions ──────────────────────────────────────────────────────────────

func (h *AgentHandler) getHints(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceID string `json:"workspace_id"`
		Query       string `json:"query"`
		FilePath    string `json:"file_path"`
		Language    string `json:"language"`
		MaxResults  int    `json:"max_results"`
		Providers   string `json:"providers"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.AgentHandler.getHints: %w", err)
	}
	if args.Query == "" {
		return types.NewErrorResult("query is required"), nil
	}
	if h.hintsEngine == nil {
		return types.NewErrorResult("hints engine not available"), nil
	}

	req := &hints.HintRequest{
		WorkspaceID: args.WorkspaceID,
		Query:       args.Query,
		FilePath:    args.FilePath,
		Language:    args.Language,
		MaxResults:  args.MaxResults,
	}

	if args.Providers != "" {
		parts := strings.Split(args.Providers, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		req.Providers = parts
	}

	results, err := h.hintsEngine.GetHints(ctx, req)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get hints: %v", err)), nil
	}

	if len(results) == 0 {
		return types.NewToolResult("No hints found for the given query."), nil
	}

	return types.NewToolResult(results), nil
}

func (h *AgentHandler) listHintProviders(_ context.Context, _ json.RawMessage) (*types.ToolResult, error) {
	if h.hintsEngine == nil {
		return types.NewErrorResult("hints engine not available"), nil
	}

	hintProviders := h.hintsEngine.ListProviders()

	if len(hintProviders) == 0 {
		return types.NewToolResult("No hint providers registered."), nil
	}

	var sb strings.Builder
	sb.WriteString("Available hint providers:\n")
	for _, name := range hintProviders {
		fmt.Fprintf(&sb, "  - %s\n", name)
	}
	return types.NewToolResult(sb.String()), nil
}

func (h *AgentHandler) configureHintProvider(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.AgentHandler.configureHintProvider: %w", err)
	}
	if args.Name == "" {
		return types.NewErrorResult("name is required"), nil
	}
	if h.hintsEngine == nil {
		return types.NewErrorResult("hints engine not available"), nil
	}
	if h.store.Config == nil {
		return types.NewErrorResult("config repo not available"), nil
	}

	hintProviders := h.hintsEngine.ListProviders()
	found := false
	for _, p := range hintProviders {
		if p == args.Name {
			found = true
			break
		}
	}
	if !found {
		return types.NewErrorResult(fmt.Sprintf("unknown provider: %q", args.Name)), nil
	}

	key := fmt.Sprintf("hints.provider.%s.enabled", args.Name)
	value := "false"
	if args.Enabled {
		value = "true"
	}

	scope := types.ConfigScope{Type: "global"}
	if err := h.store.Config.SetValue(ctx, key, value, scope, "agent_handler"); err != nil {
		return types.NewErrorResult(fmt.Sprintf("save config: %v", err)), nil
	}

	actionStr := "disabled"
	if args.Enabled {
		actionStr = "enabled"
	}
	return types.NewToolResult(fmt.Sprintf("Hint provider %q %s.", args.Name, actionStr)), nil
}

// ── Delegation actions ─────────────────────────────────────────────────────────

func (h *AgentHandler) grantDelegation(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		GranterID     string   `json:"granter_id"`
		GranteeID     string   `json:"grantee_id"`
		GrantType     string   `json:"grant_type"`
		Credential    string   `json:"credential"`
		ElevatedLevel int      `json:"elevated_level"`
		Scopes        []string `json:"scopes"`
		ExpiresAt     string   `json:"expires_at"`
		Reason        string   `json:"reason"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.AgentHandler.grantDelegation: %w", err)
	}
	if args.GranterID == "" {
		return types.NewErrorResult("granter_id is required"), nil
	}
	if args.GranteeID == "" {
		return types.NewErrorResult("grantee_id is required"), nil
	}
	if args.Reason == "" {
		return types.NewErrorResult("reason is required"), nil
	}
	if h.delegationSvc == nil {
		return types.NewErrorResult("delegation service not available"), nil
	}

	grantType := types.DelegationGrantType(args.GrantType)
	switch grantType {
	case types.GrantClearanceElevation, types.GrantCredentialPassthrough, types.GrantScopeAccess:
		// valid
	default:
		return types.NewErrorResult(fmt.Sprintf("invalid grant_type: %s", args.GrantType)), nil
	}

	d, err := h.delegationSvc.Grant(ctx, delegation.GrantRequest{
		GranterID:     args.GranterID,
		GranteeID:     args.GranteeID,
		GrantType:     grantType,
		Credential:    args.Credential,
		ElevatedLevel: args.ElevatedLevel,
		Scopes:        args.Scopes,
		ExpiresAt:     args.ExpiresAt,
		Reason:        args.Reason,
	})
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("grant delegation: %v", err)), nil
	}

	return types.NewToolResult(map[string]any{
		"delegation_id": d.ID,
		"granter_id":    d.GranterID,
		"grantee_id":    d.GranteeID,
		"grant_type":    string(d.GrantType),
		"created_at":    d.CreatedAt,
		"expires_at":    d.ExpiresAt,
		"status":        "granted",
	}), nil
}

func (h *AgentHandler) revokeDelegation(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		DelegationID string `json:"delegation_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.AgentHandler.revokeDelegation: %w", err)
	}
	if args.DelegationID == "" {
		return types.NewErrorResult("delegation_id is required"), nil
	}
	if h.delegationSvc == nil {
		return types.NewErrorResult("delegation service not available"), nil
	}

	if err := h.delegationSvc.Revoke(ctx, args.DelegationID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("revoke delegation: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"delegation_id": args.DelegationID,
		"status":        "revoked",
	}), nil
}

func (h *AgentHandler) listDelegations(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		PersonaID string `json:"persona_id"`
		Role      string `json:"role"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.AgentHandler.listDelegations: %w", err)
	}
	if args.Role == "" {
		args.Role = "grantee"
	}
	if h.delegationSvc == nil {
		return types.NewErrorResult("delegation service not available"), nil
	}

	var delegations []*types.Delegation
	var err error

	if args.PersonaID == "" {
		delegations, err = h.delegationSvc.ListAll(ctx)
	} else {
		switch args.Role {
		case "grantee":
			delegations, err = h.delegationSvc.ListByGrantee(ctx, args.PersonaID)
		case "granter":
			delegations, err = h.delegationSvc.ListByGranter(ctx, args.PersonaID)
		default:
			return types.NewErrorResult(fmt.Sprintf("invalid role: %s (use grantee or granter)", args.Role)), nil
		}
	}

	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("list delegations: %v", err)), nil
	}

	results := make([]map[string]any, 0, len(delegations))
	for _, d := range delegations {
		entry := map[string]any{
			"id":         d.ID,
			"granter_id": d.GranterID,
			"grantee_id": d.GranteeID,
			"grant_type": string(d.GrantType),
			"reason":     d.Reason,
			"created_at": d.CreatedAt,
			"active":     d.IsActive(),
		}
		if d.ExpiresAt != "" {
			entry["expires_at"] = d.ExpiresAt
		}
		if d.ElevatedLevel > 0 {
			entry["elevated_level"] = d.ElevatedLevel
		}
		if len(d.Scopes) > 0 {
			entry["scopes"] = d.Scopes
		}
		if d.RevokedAt != "" {
			entry["revoked_at"] = d.RevokedAt
		}
		results = append(results, entry)
	}

	return types.NewToolResult(map[string]any{
		"persona_id":  args.PersonaID,
		"role":        args.Role,
		"count":       len(results),
		"delegations": results,
	}), nil
}

func (h *AgentHandler) getDelegationCredential(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		DelegationID string `json:"delegation_id"`
		RequesterID  string `json:"requester_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.AgentHandler.getDelegationCredential: %w", err)
	}
	if args.DelegationID == "" {
		return types.NewErrorResult("delegation_id is required"), nil
	}
	if args.RequesterID == "" {
		return types.NewErrorResult("requester_id is required"), nil
	}
	if h.delegationSvc == nil {
		return types.NewErrorResult("delegation service not available"), nil
	}

	val, err := h.delegationSvc.GetCredential(ctx, args.DelegationID, args.RequesterID)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get credential: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"delegation_id": args.DelegationID,
		"credential":    val,
	}), nil
}

// ── Onboarding action ──────────────────────────────────────────────────────────

func (h *AgentHandler) onboardAgent(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		PersonaID   string `json:"persona_id"`
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.AgentHandler.onboardAgent: %w", err)
	}
	if args.PersonaID == "" {
		return types.NewErrorResult("persona_id is required"), nil
	}
	if args.WorkspaceID == "" {
		return types.NewErrorResult("workspace_id is required"), nil
	}

	if h.memoryStore == nil {
		return types.NewErrorResult("memory system not available"), nil
	}

	query := types.MemoryQuery{
		Query:       "*",
		PersonaID:   args.PersonaID,
		WorkspaceID: args.WorkspaceID,
		MaxResults:  20,
	}

	results, err := h.memoryStore.Recall(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("handlers.AgentHandler.onboardAgent: %w", err)
	}

	if len(results) == 0 {
		return types.NewToolResult(map[string]any{
			"persona_id":   args.PersonaID,
			"workspace_id": args.WorkspaceID,
			"memories":     0,
			"message":      "No memories available for onboarding.",
		}), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Onboarding context for persona %s (workspace %s):\n", args.PersonaID, args.WorkspaceID)
	fmt.Fprintf(&sb, "Loaded %d memories:\n", len(results))
	for _, mc := range results {
		m := mc.Memory
		fmt.Fprintf(&sb, "  [%s/%s] %s\n", m.Scope, m.Type, m.Content)
	}

	return types.NewToolResult(map[string]any{
		"persona_id":   args.PersonaID,
		"workspace_id": args.WorkspaceID,
		"memories":     len(results),
		"context":      sb.String(),
	}), nil
}
