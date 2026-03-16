package types

import "time"

// ConfigScope defines the resolution hierarchy: agent -> workspace -> global -> default.
type ConfigScope struct {
	Type string `json:"type"` // "global", "workspace", "agent"
	ID   string `json:"id"`   // "" for global, workspace_id, or agent_id
}

// ConfigKeyMeta describes a configuration key's schema.
type ConfigKeyMeta struct {
	Key         string `json:"key"`
	ScopeType   string `json:"scope_type"`
	ValueType   string `json:"value_type"` // string, int, float, bool, json, duration
	DefaultVal  string `json:"default_val"`
	Critical    bool   `json:"critical"`
	Description string `json:"description"`
}

// ConfigValue is a resolved configuration value.
type ConfigValue struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	ScopeType string `json:"scope_type"`
	ScopeID   string `json:"scope_id"`
}

// ConfigChange records a configuration mutation for audit trail.
type ConfigChange struct {
	Key       string    `json:"key"`
	OldValue  string    `json:"old_value"`
	NewValue  string    `json:"new_value"`
	ScopeType string    `json:"scope_type"`
	ScopeID   string    `json:"scope_id"`
	Actor     string    `json:"actor"`
	ChangedAt time.Time `json:"changed_at"`
}
