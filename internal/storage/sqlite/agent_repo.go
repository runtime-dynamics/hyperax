package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/repo"
)

// AgentRepo implements repo.AgentRepo for SQLite.
type AgentRepo struct {
	db *sql.DB
}

// agentColumns is the SELECT column list used by all read queries.
const agentColumns = `id, name, persona_id, parent_agent_id, workspace_id, status,
	COALESCE(status_reason, ''),
	COALESCE(personality, ''), COALESCE(role_template_id, ''), COALESCE(clearance_level, 0),
	COALESCE(provider_id, ''), COALESCE(default_model, ''), COALESCE(chat_model, ''),
	COALESCE(is_internal, 0),
	COALESCE(system_prompt, ''), COALESCE(guard_bypass, 0), COALESCE(engagement_rules, ''),
	created_at, updated_at`

// scanAgent scans a single agent row into the Agent struct.
func scanAgent(a *repo.Agent, scanner interface{ Scan(...any) error }) error {
	var parentAgentID, workspaceID sql.NullString
	var createdAt, updatedAt string
	var isInternal, guardBypass int
	err := scanner.Scan(
		&a.ID, &a.Name, &a.PersonaID, &parentAgentID, &workspaceID,
		&a.Status, &a.StatusReason,
		&a.Personality, &a.RoleTemplateID, &a.ClearanceLevel,
		&a.ProviderID, &a.DefaultModel, &a.ChatModel, &isInternal,
		&a.SystemPrompt, &guardBypass, &a.EngagementRules,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return fmt.Errorf("sqlite.scanAgent: %w", err)
	}

	if parentAgentID.Valid {
		a.ParentAgentID = parentAgentID.String
	}
	if workspaceID.Valid {
		a.WorkspaceID = workspaceID.String
	}
	a.IsInternal = isInternal == 1
	a.GuardBypass = guardBypass == 1
	if a.CreatedAt, err = parseSQLiteTime(createdAt, "sqlite.scanAgent"); err != nil {
		return err
	}
	if a.UpdatedAt, err = parseSQLiteTime(updatedAt, "sqlite.scanAgent"); err != nil {
		return err
	}

	return nil
}

// Create inserts a new agent and returns its generated ID.
// Internal agents (is_internal=true) must have unique names; an error is
// returned if a duplicate internal agent name is detected.
func (r *AgentRepo) Create(ctx context.Context, agent *repo.Agent) (string, error) {
	if agent.ID == "" {
		agent.ID = uuid.New().String()
	}
	if agent.Status == "" {
		agent.Status = "idle"
	}

	// Guard: prevent duplicate internal agents by name.
	if agent.IsInternal {
		var count int
		err := r.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM agents WHERE name = ? COLLATE NOCASE AND is_internal = 1`,
			agent.Name,
		).Scan(&count)
		if err == nil && count > 0 {
			return "", fmt.Errorf("internal agent %q already exists", agent.Name)
		}
	}

	var parentAgentID, workspaceID *string
	if agent.ParentAgentID != "" {
		parentAgentID = &agent.ParentAgentID
	}
	if agent.WorkspaceID != "" {
		workspaceID = &agent.WorkspaceID
	}

	var providerID, defaultModel, roleTemplateID *string
	if agent.ProviderID != "" {
		providerID = &agent.ProviderID
	}
	if agent.DefaultModel != "" {
		defaultModel = &agent.DefaultModel
	}
	if agent.RoleTemplateID != "" {
		roleTemplateID = &agent.RoleTemplateID
	}

	isInternal := 0
	if agent.IsInternal {
		isInternal = 1
	}
	guardBypass := 0
	if agent.GuardBypass {
		guardBypass = 1
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO agents (id, name, persona_id, parent_agent_id, workspace_id, status,
		    status_reason, personality, role_template_id, clearance_level, provider_id, default_model,
		    chat_model, is_internal, system_prompt, guard_bypass, engagement_rules)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		agent.ID, agent.Name, agent.PersonaID, parentAgentID, workspaceID, agent.Status,
		agent.StatusReason, agent.Personality, roleTemplateID, agent.ClearanceLevel, providerID, defaultModel,
		agent.ChatModel, isInternal, agent.SystemPrompt, guardBypass, agent.EngagementRules,
	)
	if err != nil {
		return "", fmt.Errorf("sqlite.AgentRepo.Create: %w", err)
	}

	return agent.ID, nil
}

// Get retrieves an agent by its ID.
func (r *AgentRepo) Get(ctx context.Context, id string) (*repo.Agent, error) {
	a := &repo.Agent{}
	row := r.db.QueryRowContext(ctx,
		`SELECT `+agentColumns+` FROM agents WHERE id = ?`, id,
	)
	if err := scanAgent(a, row); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("repo.AgentRepo.Get: agent %q not found", id)
		}
		return nil, fmt.Errorf("sqlite.AgentRepo.Get: %w", err)
	}
	repo.AgentReasonCache.Apply(a)
	return a, nil
}

// GetByName retrieves an agent by its unique name.
func (r *AgentRepo) GetByName(ctx context.Context, name string) (*repo.Agent, error) {
	a := &repo.Agent{}
	row := r.db.QueryRowContext(ctx,
		`SELECT `+agentColumns+` FROM agents WHERE name = ? COLLATE NOCASE`, name,
	)
	if err := scanAgent(a, row); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("repo.AgentRepo.GetByName: agent %q not found", name)
		}
		return nil, fmt.Errorf("sqlite.AgentRepo.GetByName: %w", err)
	}
	repo.AgentReasonCache.Apply(a)
	return a, nil
}

// List returns all agents ordered by name.
func (r *AgentRepo) List(ctx context.Context) ([]*repo.Agent, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+agentColumns+` FROM agents ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.AgentRepo.List: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return r.scanAgents(rows)
}

// ListByPersona returns all agents assigned to a specific persona template.
func (r *AgentRepo) ListByPersona(ctx context.Context, personaID string) ([]*repo.Agent, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+agentColumns+` FROM agents WHERE persona_id = ? ORDER BY name`, personaID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.AgentRepo.ListByPersona: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return r.scanAgents(rows)
}

// Update modifies an existing agent.
func (r *AgentRepo) Update(ctx context.Context, id string, agent *repo.Agent) error {
	// Clear in-memory error reason when transitioning away from error status.
	if agent.Status != repo.AgentStatusError {
		repo.AgentReasonCache.Clear(id)
	}

	var parentAgentID, workspaceID *string
	if agent.ParentAgentID != "" {
		parentAgentID = &agent.ParentAgentID
	}
	if agent.WorkspaceID != "" {
		workspaceID = &agent.WorkspaceID
	}

	var providerID, defaultModel, roleTemplateID *string
	if agent.ProviderID != "" {
		providerID = &agent.ProviderID
	}
	if agent.DefaultModel != "" {
		defaultModel = &agent.DefaultModel
	}
	if agent.RoleTemplateID != "" {
		roleTemplateID = &agent.RoleTemplateID
	}

	isInternal := 0
	if agent.IsInternal {
		isInternal = 1
	}
	guardBypass := 0
	if agent.GuardBypass {
		guardBypass = 1
	}

	res, err := r.db.ExecContext(ctx,
		`UPDATE agents SET
		    name = ?, persona_id = ?, parent_agent_id = ?,
		    workspace_id = ?, status = ?, status_reason = ?,
		    personality = ?, role_template_id = ?, clearance_level = ?,
		    provider_id = ?, default_model = ?, chat_model = ?,
		    is_internal = ?, system_prompt = ?, guard_bypass = ?,
		    engagement_rules = ?,
		    updated_at = datetime('now')
		 WHERE id = ?`,
		agent.Name, agent.PersonaID, parentAgentID,
		workspaceID, agent.Status, agent.StatusReason,
		agent.Personality, roleTemplateID, agent.ClearanceLevel,
		providerID, defaultModel, agent.ChatModel,
		isInternal, agent.SystemPrompt, guardBypass,
		agent.EngagementRules, id,
	)
	if err != nil {
		return fmt.Errorf("sqlite.AgentRepo.Update: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.AgentRepo.Update: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("agent %q not found", id)
	}

	return nil
}

// Delete removes an agent by its ID.
func (r *AgentRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM agents WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("sqlite.AgentRepo.Delete: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.AgentRepo.Delete: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("agent %q not found", id)
	}

	return nil
}

// SetAgentError atomically sets an agent's status to "error" with a reason.
// The reason is stored in memory only (AgentReasonCache), NOT in the database.
// This prevents stale error messages from previous binary versions persisting
// across restarts and misleading debugging.
func (r *AgentRepo) SetAgentError(ctx context.Context, agentID, reason string) error {
	repo.AgentReasonCache.Set(agentID, reason)
	res, err := r.db.ExecContext(ctx,
		`UPDATE agents SET status = ?, status_reason = '', updated_at = datetime('now') WHERE id = ?`,
		repo.AgentStatusError, agentID,
	)
	if err != nil {
		return fmt.Errorf("sqlite.AgentRepo.SetAgentError: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.AgentRepo.SetAgentError: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("agent %q not found", agentID)
	}
	return nil
}

// scanAgents scans multiple agent rows into a slice.
func (r *AgentRepo) scanAgents(rows *sql.Rows) ([]*repo.Agent, error) {
	var agents []*repo.Agent
	for rows.Next() {
		a := &repo.Agent{}
		if err := scanAgent(a, rows); err != nil {
			return nil, fmt.Errorf("sqlite.AgentRepo.scanAgents: %w", err)
		}
		repo.AgentReasonCache.Apply(a)
		agents = append(agents, a)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.AgentRepo.scanAgents: %w", err)
	}
	return agents, nil
}
