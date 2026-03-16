package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/repo"
)

// PersonaRepo implements repo.PersonaRepo for SQLite.
type PersonaRepo struct {
	db *sql.DB
}

// Create inserts a new persona and returns its generated ID.
func (r *PersonaRepo) Create(ctx context.Context, persona *repo.Persona) (string, error) {
	if persona.ID == "" {
		persona.ID = uuid.New().String()
	}

	isActive := 0
	if persona.IsActive {
		isActive = 1
	}

	// Use NULL for empty provider_id and default_model.
	var providerID, defaultModel *string
	if persona.ProviderID != "" {
		providerID = &persona.ProviderID
	}
	if persona.DefaultModel != "" {
		defaultModel = &persona.DefaultModel
	}

	guardBypass := 0
	if persona.GuardBypass {
		guardBypass = 1
	}

	var roleTemplateID *string
	if persona.RoleTemplateID != "" {
		roleTemplateID = &persona.RoleTemplateID
	}

	isInternal := 0
	if persona.IsInternal {
		isInternal = 1
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO personas (id, name, description, system_prompt, team, role, home_machine_uuid, clearance_level, is_active, provider_id, default_model, guard_bypass, role_template_id, is_internal)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		persona.ID, persona.Name, persona.Description, persona.SystemPrompt,
		persona.Team, persona.Role, persona.HomeMachineUUID,
		persona.ClearanceLevel, isActive, providerID, defaultModel, guardBypass, roleTemplateID, isInternal,
	)
	if err != nil {
		return "", fmt.Errorf("sqlite.PersonaRepo.Create: %w", err)
	}

	return persona.ID, nil
}

// Get retrieves a persona by its ID.
func (r *PersonaRepo) Get(ctx context.Context, id string) (*repo.Persona, error) {
	p := &repo.Persona{}
	var isActive, guardBypass, isInternal int
	var createdAt, updatedAt string

	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, COALESCE(description, ''), COALESCE(system_prompt, ''),
		        COALESCE(team, ''), COALESCE(role, ''), COALESCE(home_machine_uuid, ''),
		        clearance_level, is_active, COALESCE(provider_id, ''), COALESCE(default_model, ''),
		        COALESCE(guard_bypass, 0), COALESCE(role_template_id, ''), COALESCE(is_internal, 0),
		        created_at, updated_at
		 FROM personas WHERE id = ?`, id,
	).Scan(
		&p.ID, &p.Name, &p.Description, &p.SystemPrompt,
		&p.Team, &p.Role, &p.HomeMachineUUID,
		&p.ClearanceLevel, &isActive, &p.ProviderID, &p.DefaultModel,
		&guardBypass, &p.RoleTemplateID, &isInternal, &createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("repo.PersonaRepo.Get: persona %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite.PersonaRepo.Get: %w", err)
	}

	p.IsActive = isActive == 1
	p.GuardBypass = guardBypass == 1
	p.IsInternal = isInternal == 1
	p.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	p.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)

	return p, nil
}

// GetByName retrieves a persona by its unique name.
// Returns an error if the persona is not found.
func (r *PersonaRepo) GetByName(ctx context.Context, name string) (*repo.Persona, error) {
	p := &repo.Persona{}
	var isActive, guardBypass, isInternal int
	var createdAt, updatedAt string

	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, COALESCE(description, ''), COALESCE(system_prompt, ''),
		        COALESCE(team, ''), COALESCE(role, ''), COALESCE(home_machine_uuid, ''),
		        clearance_level, is_active, COALESCE(provider_id, ''), COALESCE(default_model, ''),
		        COALESCE(guard_bypass, 0), COALESCE(role_template_id, ''), COALESCE(is_internal, 0),
		        created_at, updated_at
		 FROM personas WHERE name = ? COLLATE NOCASE`, name,
	).Scan(
		&p.ID, &p.Name, &p.Description, &p.SystemPrompt,
		&p.Team, &p.Role, &p.HomeMachineUUID,
		&p.ClearanceLevel, &isActive, &p.ProviderID, &p.DefaultModel,
		&guardBypass, &p.RoleTemplateID, &isInternal, &createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("repo.PersonaRepo.GetByName: persona %q not found", name)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite.PersonaRepo.GetByName: %w", err)
	}

	p.IsActive = isActive == 1
	p.GuardBypass = guardBypass == 1
	p.IsInternal = isInternal == 1
	p.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	p.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)

	return p, nil
}

// List returns all personas ordered by name.
func (r *PersonaRepo) List(ctx context.Context) ([]*repo.Persona, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, COALESCE(description, ''), COALESCE(system_prompt, ''),
		        COALESCE(team, ''), COALESCE(role, ''), COALESCE(home_machine_uuid, ''),
		        clearance_level, is_active, COALESCE(provider_id, ''), COALESCE(default_model, ''),
		        COALESCE(guard_bypass, 0), COALESCE(role_template_id, ''), COALESCE(is_internal, 0),
		        created_at, updated_at
		 FROM personas ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.PersonaRepo.List: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var personas []*repo.Persona
	for rows.Next() {
		p := &repo.Persona{}
		var isActive, guardBypass, isInternal int
		var createdAt, updatedAt string

		if err := rows.Scan(
			&p.ID, &p.Name, &p.Description, &p.SystemPrompt,
			&p.Team, &p.Role, &p.HomeMachineUUID,
			&p.ClearanceLevel, &isActive, &p.ProviderID, &p.DefaultModel,
			&guardBypass, &p.RoleTemplateID, &isInternal, &createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("sqlite.PersonaRepo.List: %w", err)
		}

		p.IsActive = isActive == 1
		p.GuardBypass = guardBypass == 1
		p.IsInternal = isInternal == 1
		p.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		p.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
		personas = append(personas, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.PersonaRepo.List: %w", err)
	}
	return personas, nil
}

// Update modifies an existing persona. Only non-zero fields on the provided
// persona struct are written; is_active and clearance_level are always applied.
func (r *PersonaRepo) Update(ctx context.Context, id string, persona *repo.Persona) error {
	isActive := 0
	if persona.IsActive {
		isActive = 1
	}
	guardBypass := 0
	if persona.GuardBypass {
		guardBypass = 1
	}

	// Use NULL for empty provider_id, default_model, and role_template_id.
	var providerID, defaultModel *string
	if persona.ProviderID != "" {
		providerID = &persona.ProviderID
	}
	if persona.DefaultModel != "" {
		defaultModel = &persona.DefaultModel
	}
	var roleTemplateID *string
	if persona.RoleTemplateID != "" {
		roleTemplateID = &persona.RoleTemplateID
	}

	isInternal := 0
	if persona.IsInternal {
		isInternal = 1
	}

	res, err := r.db.ExecContext(ctx,
		`UPDATE personas SET
		    name = ?, description = ?, system_prompt = ?,
		    team = ?, role = ?, home_machine_uuid = ?,
		    clearance_level = ?, is_active = ?,
		    provider_id = ?, default_model = ?,
		    guard_bypass = ?, role_template_id = ?,
		    is_internal = ?,
		    updated_at = datetime('now')
		 WHERE id = ?`,
		persona.Name, persona.Description, persona.SystemPrompt,
		persona.Team, persona.Role, persona.HomeMachineUUID,
		persona.ClearanceLevel, isActive, providerID, defaultModel, guardBypass, roleTemplateID,
		isInternal, id,
	)
	if err != nil {
		return fmt.Errorf("sqlite.PersonaRepo.Update: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.PersonaRepo.Update: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("persona %q not found", id)
	}

	return nil
}

// Delete removes a persona by its ID.
func (r *PersonaRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM personas WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("sqlite.PersonaRepo.Delete: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.PersonaRepo.Delete: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("persona %q not found", id)
	}

	return nil
}

