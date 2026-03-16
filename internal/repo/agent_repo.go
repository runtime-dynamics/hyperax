package repo

import (
	"context"
	"sync"
	"time"
)

// AgentReasonCache stores agent error reasons in memory only.
// Error messages are ephemeral — persisting them to the database causes stale
// error messages from previous binary versions to resurface after restarts,
// making debugging misleading and preventing clean recovery.
var AgentReasonCache agentReasonCache

type agentReasonCache struct {
	reasons sync.Map // agentID → reason string
}

// Set stores an error reason for an agent.
func (c *agentReasonCache) Set(agentID, reason string) {
	c.reasons.Store(agentID, reason)
}

// Get returns the in-memory error reason for an agent, or empty if none.
func (c *agentReasonCache) Get(agentID string) string {
	v, ok := c.reasons.Load(agentID)
	if !ok {
		return ""
	}
	return v.(string)
}

// Clear removes the cached reason for an agent.
func (c *agentReasonCache) Clear(agentID string) {
	c.reasons.Delete(agentID)
}

// Apply overlays the in-memory error reason onto an agent loaded from the
// database. Only applies to agents in error status.
func (c *agentReasonCache) Apply(agent *Agent) {
	if agent.Status == AgentStatusError {
		if reason := c.Get(agent.ID); reason != "" {
			agent.StatusReason = reason
		}
	}
}

// Agent status constants.
const (
	AgentStatusIdle   = "idle"
	AgentStatusActive = "active"
	AgentStatusError  = "error"
)

// Agent represents an agent instance with all configuration fields.
// After persona elimination, agents absorb persona fields directly:
// personality, role_template_id, clearance_level, provider_id, default_model,
// chat_model, is_internal, system_prompt, and guard_bypass.
type Agent struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	PersonaID       string    `json:"persona_id,omitempty"` // Kept for migration compatibility
	ParentAgentID   string    `json:"parent_agent_id,omitempty"`
	WorkspaceID     string    `json:"workspace_id,omitempty"`
	Status          string    `json:"status"`
	StatusReason    string    `json:"status_reason"`
	Personality     string    `json:"personality,omitempty"`
	RoleTemplateID  string    `json:"role_template_id,omitempty"`
	ClearanceLevel  int       `json:"clearance_level"`
	ProviderID      string    `json:"provider_id,omitempty"`
	DefaultModel    string    `json:"default_model,omitempty"`
	ChatModel       string    `json:"chat_model,omitempty"`
	IsInternal      bool      `json:"is_internal"`
	SystemPrompt    string    `json:"system_prompt,omitempty"`
	GuardBypass     bool      `json:"guard_bypass"`
	EngagementRules string    `json:"engagement_rules,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// AgentRepo handles agent CRUD operations.
type AgentRepo interface {
	Create(ctx context.Context, agent *Agent) (string, error)
	Get(ctx context.Context, id string) (*Agent, error)
	GetByName(ctx context.Context, name string) (*Agent, error)
	List(ctx context.Context) ([]*Agent, error)
	ListByPersona(ctx context.Context, personaID string) ([]*Agent, error)
	Update(ctx context.Context, id string, agent *Agent) error
	Delete(ctx context.Context, id string) error
	// SetAgentError atomically sets an agent's status to "error" with a reason.
	SetAgentError(ctx context.Context, agentID, reason string) error
}

