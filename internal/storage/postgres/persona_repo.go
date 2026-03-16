package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/repo"
)

// PersonaRepo implements repo.PersonaRepo for PostgreSQL.
type PersonaRepo struct {
	db *sql.DB
}

// Create inserts a new persona and returns its generated ID.
func (r *PersonaRepo) Create(ctx context.Context, persona *repo.Persona) (string, error) {
	if persona.ID == "" {
		persona.ID = uuid.New().String()
	}

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

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO personas (id, name, description, system_prompt, team, role, home_machine_uuid, clearance_level, is_active, provider_id, default_model, guard_bypass, role_template_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		persona.ID, persona.Name, persona.Description, persona.SystemPrompt,
		persona.Team, persona.Role, persona.HomeMachineUUID,
		persona.ClearanceLevel, persona.IsActive, providerID, defaultModel, persona.GuardBypass, roleTemplateID,
	)
	if err != nil {
		return "", fmt.Errorf("postgres.PersonaRepo.Create: %w", err)
	}
	return persona.ID, nil
}

// Get retrieves a persona by its ID.
func (r *PersonaRepo) Get(ctx context.Context, id string) (*repo.Persona, error) {
	return r.scanOne(ctx,
		`SELECT id, name, COALESCE(description, ''), COALESCE(system_prompt, ''),
		        COALESCE(team, ''), COALESCE(role, ''), COALESCE(home_machine_uuid, ''),
		        clearance_level, is_active, COALESCE(provider_id, ''), COALESCE(default_model, ''),
		        COALESCE(guard_bypass, false), COALESCE(role_template_id, ''), created_at, updated_at
		 FROM personas WHERE id = $1`, id,
	)
}

// GetByName retrieves a persona by its unique name.
func (r *PersonaRepo) GetByName(ctx context.Context, name string) (*repo.Persona, error) {
	return r.scanOne(ctx,
		`SELECT id, name, COALESCE(description, ''), COALESCE(system_prompt, ''),
		        COALESCE(team, ''), COALESCE(role, ''), COALESCE(home_machine_uuid, ''),
		        clearance_level, is_active, COALESCE(provider_id, ''), COALESCE(default_model, ''),
		        COALESCE(guard_bypass, false), COALESCE(role_template_id, ''), created_at, updated_at
		 FROM personas WHERE LOWER(name) = LOWER($1)`, name,
	)
}

// List returns all personas ordered by name.
func (r *PersonaRepo) List(ctx context.Context) ([]*repo.Persona, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, COALESCE(description, ''), COALESCE(system_prompt, ''),
		        COALESCE(team, ''), COALESCE(role, ''), COALESCE(home_machine_uuid, ''),
		        clearance_level, is_active, COALESCE(provider_id, ''), COALESCE(default_model, ''),
		        COALESCE(guard_bypass, false), COALESCE(role_template_id, ''), created_at, updated_at
		 FROM personas ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.PersonaRepo.List: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var personas []*repo.Persona
	for rows.Next() {
		p := &repo.Persona{}
		if err := rows.Scan(
			&p.ID, &p.Name, &p.Description, &p.SystemPrompt,
			&p.Team, &p.Role, &p.HomeMachineUUID,
			&p.ClearanceLevel, &p.IsActive, &p.ProviderID, &p.DefaultModel,
			&p.GuardBypass, &p.RoleTemplateID, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("postgres.PersonaRepo.List: %w", err)
		}
		personas = append(personas, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres.PersonaRepo.List: %w", err)
	}
	return personas, nil
}

// Update modifies an existing persona by ID.
func (r *PersonaRepo) Update(ctx context.Context, id string, persona *repo.Persona) error {
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

	res, err := r.db.ExecContext(ctx,
		`UPDATE personas SET
		    name = $1, description = $2, system_prompt = $3,
		    team = $4, role = $5, home_machine_uuid = $6,
		    clearance_level = $7, is_active = $8,
		    provider_id = $9, default_model = $10,
		    guard_bypass = $11, role_template_id = $12,
		    updated_at = NOW()
		 WHERE id = $13`,
		persona.Name, persona.Description, persona.SystemPrompt,
		persona.Team, persona.Role, persona.HomeMachineUUID,
		persona.ClearanceLevel, persona.IsActive, providerID, defaultModel, persona.GuardBypass, roleTemplateID, id,
	)
	if err != nil {
		return fmt.Errorf("postgres.PersonaRepo.Update: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.PersonaRepo.Update: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("persona %q not found", id)
	}
	return nil
}

// Delete removes a persona by its ID.
func (r *PersonaRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM personas WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("postgres.PersonaRepo.Delete: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres.PersonaRepo.Delete: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("persona %q not found", id)
	}
	return nil
}

// scanOne executes a query expected to return a single persona row.
func (r *PersonaRepo) scanOne(ctx context.Context, query string, args ...any) (*repo.Persona, error) {
	p := &repo.Persona{}
	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&p.ID, &p.Name, &p.Description, &p.SystemPrompt,
		&p.Team, &p.Role, &p.HomeMachineUUID,
		&p.ClearanceLevel, &p.IsActive, &p.ProviderID, &p.DefaultModel,
		&p.GuardBypass, &p.RoleTemplateID, &p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("persona not found")
	}
	if err != nil {
		return nil, fmt.Errorf("postgres.PersonaRepo.scanOne: %w", err)
	}
	return p, nil
}
