package repo

import (
	"context"
	"time"
)

// Persona represents an agent persona.
type Persona struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	SystemPrompt    string    `json:"system_prompt"`
	Team            string    `json:"team"`
	Role            string    `json:"role"`
	HomeMachineUUID string    `json:"home_machine_uuid"`
	ClearanceLevel  int       `json:"clearance_level"`
	IsActive        bool      `json:"is_active"`
	ProviderID      string    `json:"provider_id"`
	DefaultModel    string    `json:"default_model"`
	GuardBypass     bool      `json:"guard_bypass"`
	RoleTemplateID  string    `json:"role_template_id"`
	IsInternal      bool      `json:"is_internal"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// PersonaRepo handles persona CRUD.
type PersonaRepo interface {
	Create(ctx context.Context, persona *Persona) (string, error)
	Get(ctx context.Context, id string) (*Persona, error)
	GetByName(ctx context.Context, name string) (*Persona, error)
	List(ctx context.Context) ([]*Persona, error)
	Update(ctx context.Context, id string, persona *Persona) error
	Delete(ctx context.Context, id string) error
}
