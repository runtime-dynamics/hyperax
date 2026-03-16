package types

// SecretScope defines the access level for a secret.
type SecretScope string

const (
	SecretScopeGlobal    SecretScope = "global"    // Available to all agents
	SecretScopeWorkspace SecretScope = "workspace" // Available within a workspace
	SecretScopeAgent     SecretScope = "agent"     // Available to a specific agent only
)

// SecretMeta describes a secret without revealing its value.
type SecretMeta struct {
	Key       string      `json:"key"`
	Scope     SecretScope `json:"scope"`
	CreatedAt string      `json:"created_at"`
	UpdatedAt string      `json:"updated_at"`
}
