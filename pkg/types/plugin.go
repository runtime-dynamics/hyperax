package types

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// PluginVariableType defines the type of a plugin configuration variable.
type PluginVariableType string

const (
	PluginVarString      PluginVariableType = "string"
	PluginVarInt         PluginVariableType = "int"
	PluginVarFloat       PluginVariableType = "float"
	PluginVarBool        PluginVariableType = "bool"
	PluginVarArrayString PluginVariableType = "array_string"
	PluginVarArrayInt    PluginVariableType = "array_int"
	PluginVarArrayFloat  PluginVariableType = "array_float"
)

// ValidPluginVariableTypes is the set of recognised variable types.
var ValidPluginVariableTypes = map[PluginVariableType]bool{
	PluginVarString:      true,
	PluginVarInt:         true,
	PluginVarFloat:       true,
	PluginVarBool:        true,
	PluginVarArrayString: true,
	PluginVarArrayInt:    true,
	PluginVarArrayFloat:  true,
}

// PluginIntegration describes where a plugin slots into Hyperax.
type PluginIntegration string

const (
	PluginIntegrationChannel        PluginIntegration = "channel"
	PluginIntegrationTooling        PluginIntegration = "tooling"
	PluginIntegrationSecretProvider PluginIntegration = "secret_provider"
	PluginIntegrationSensor         PluginIntegration = "sensor"
	PluginIntegrationGuard          PluginIntegration = "guard"
	PluginIntegrationAudit          PluginIntegration = "audit"
)

// ValidPluginIntegrations is the set of recognised integration categories.
var ValidPluginIntegrations = map[PluginIntegration]bool{
	PluginIntegrationChannel:        true,
	PluginIntegrationTooling:        true,
	PluginIntegrationSecretProvider: true,
	PluginIntegrationSensor:         true,
	PluginIntegrationGuard:          true,
	PluginIntegrationAudit:          true,
}

// PluginVariable describes a typed configuration variable expected by a plugin.
type PluginVariable struct {
	Name        string             `json:"name" yaml:"name"`
	Type        PluginVariableType `json:"type" yaml:"type"`
	Required    bool               `json:"required" yaml:"required"`
	Default     any                `json:"default,omitempty" yaml:"default"`
	Description string             `json:"description,omitempty" yaml:"description"`
	Secret      bool               `json:"secret" yaml:"secret"`
	Dynamic     bool               `json:"dynamic" yaml:"dynamic"`
	EnvName     string             `json:"env_name,omitempty" yaml:"env_name"`
}

// PluginResource describes a resource that a plugin expects to be auto-created.
type PluginResource struct {
	Type   string         `json:"type" yaml:"type"`
	Name   string         `json:"name" yaml:"name"`
	Config map[string]any `json:"config,omitempty" yaml:"config"`
}

// CreatedResource tracks a resource that was auto-created for a plugin.
type CreatedResource struct {
	Type string `json:"type" yaml:"type"`
	ID   string `json:"id" yaml:"id"`
	Name string `json:"name" yaml:"name"`
}

// PluginType enumerates the supported plugin execution mechanisms.
type PluginType string

const (
	PluginTypeWasm    PluginType = "wasm"
	PluginTypeMCP     PluginType = "mcp"
	PluginTypeHTTP    PluginType = "http"
	PluginTypeNative  PluginType = "native"
	PluginTypeService PluginType = "service"
)

// PluginManifest describes a plugin's metadata and capabilities.
type PluginManifest struct {
	Name             string            `json:"name" yaml:"name"`
	Version          string            `json:"version" yaml:"version"`
	Type             PluginType        `json:"type" yaml:"type"`
	Description      string            `json:"description" yaml:"description"`
	Author           string            `json:"author" yaml:"author"`
	License          string            `json:"license,omitempty" yaml:"license"`
	SourceRepo       string            `json:"source_repo,omitempty" yaml:"source_repo"`
	MinHyperaxVer    string            `json:"min_hyperax_version" yaml:"min_hyperax_version"`
	APIVersion       string            `json:"api_version" yaml:"api_version"`
	Permissions      []string          `json:"permissions" yaml:"permissions"`
	Entrypoint       string            `json:"entrypoint" yaml:"entrypoint"`
	Args             []string          `json:"args,omitempty" yaml:"args"`
	Sandbox          WasmSandbox       `json:"sandbox,omitempty" yaml:"sandbox"`
	Env              []EnvVar          `json:"env,omitempty" yaml:"env"`
	Integration      PluginIntegration `json:"integration,omitempty" yaml:"integration"`
	Variables        []PluginVariable  `json:"variables,omitempty" yaml:"variables"`
	ApprovalRequired bool              `json:"approval_required,omitempty" yaml:"approval_required"`
	Resources        []PluginResource  `json:"resources,omitempty" yaml:"resources"`
	Artifacts        map[string]string `json:"artifacts,omitempty" yaml:"artifacts"`
	Tools            []ToolDef         `json:"tools" yaml:"tools"`
	Events           []EventDef        `json:"events,omitempty" yaml:"events"`
	HealthCheck      HealthCheckConfig `json:"health_check,omitempty" yaml:"health_check"`
}

// EnvVar describes an environment variable expected by a plugin.
type EnvVar struct {
	Name        string `json:"name" yaml:"name"`
	Required    bool   `json:"required" yaml:"required"`
	Default     string `json:"default,omitempty" yaml:"default"`
	Description string `json:"description,omitempty" yaml:"description"`
}

// EventDef describes an event emitted by a plugin.
type EventDef struct {
	Type        string `json:"type" yaml:"type"`
	Description string `json:"description,omitempty" yaml:"description"`
}

// WasmSandbox configures the Wasm runtime sandbox for a plugin.
type WasmSandbox struct {
	MaxMemoryMB    int      `json:"max_memory_mb" yaml:"max_memory_mb"`
	AllowedPaths   []string `json:"allowed_paths" yaml:"allowed_paths"`
	AllowNetwork   bool     `json:"allow_network" yaml:"allow_network"`
	TimeoutPerCall string   `json:"timeout" yaml:"timeout"` // duration string
}

// HealthCheckConfig defines a plugin's health check settings.
type HealthCheckConfig struct {
	Interval string `json:"interval" yaml:"interval"`
	Timeout  string `json:"timeout" yaml:"timeout"`
	Endpoint string `json:"endpoint,omitempty" yaml:"endpoint"`
}

// ToolDef describes a tool provided by a plugin.
type ToolDef struct {
	Name        string         `json:"name" yaml:"name"`
	Description string         `json:"description" yaml:"description"`
	Parameters  []ParameterDef `json:"parameters" yaml:"parameters"`
}

// ParameterDef describes a tool parameter.
type ParameterDef struct {
	Name        string `json:"name" yaml:"name"`
	Type        string `json:"type" yaml:"type"`
	Required    bool   `json:"required" yaml:"required"`
	Default     any    `json:"default,omitempty" yaml:"default"`
	Description string `json:"description" yaml:"description"`
}

// PluginState tracks the runtime state of a loaded plugin.
type PluginState struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Version       string     `json:"version"`
	Type          PluginType `json:"type"`
	Status        string     `json:"status"` // loaded, enabled, disabled, error
	Enabled       bool       `json:"enabled"`
	ToolCount     int        `json:"tool_count"`
	LastHealthAt  *time.Time `json:"last_health_at,omitempty"`
	HealthStatus  string     `json:"health_status"` // healthy, unhealthy, unknown
	FailureCount  int        `json:"failure_count"`
	LoadedAt      time.Time  `json:"loaded_at"`
	Error         string     `json:"error,omitempty"`
	SourceHash    string     `json:"source_hash,omitempty"`
}

// PluginSourceHash computes a deterministic short hash from a plugin source repo URL.
// Used to scope secrets to specific plugins. Returns first 12 hex chars of SHA-256.
func PluginSourceHash(sourceRepo string) string {
	if sourceRepo == "" {
		return ""
	}
	h := sha256.Sum256([]byte(sourceRepo))
	return hex.EncodeToString(h[:6]) // 12 hex chars
}
