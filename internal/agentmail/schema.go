package agentmail

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// SchemaVersion represents a versioned payload schema for AgentMail messages.
// Schema IDs follow the format "namespace.name.vN" (e.g. "slack.message.v1").
type SchemaVersion struct {
	// SchemaID is the full versioned identifier (e.g. "webhook.delivery.v2").
	SchemaID string `json:"schema_id"`

	// Namespace groups related schemas (e.g. "slack", "discord", "email", "agentmail").
	Namespace string `json:"namespace"`

	// Name is the schema name within the namespace (e.g. "message", "delivery").
	Name string `json:"name"`

	// Version is the numeric version (1, 2, 3...).
	Version int `json:"version"`

	// Fields defines the expected payload field names and their JSON types.
	// Used for structural validation: {"text": "string", "channel": "string"}.
	Fields map[string]string `json:"fields"`

	// RequiredFields lists field names that must be present in the payload.
	RequiredFields []string `json:"required_fields,omitempty"`

	// Deprecated marks a schema version as deprecated.
	// Deprecated schemas are still accepted but a warning is logged.
	Deprecated bool `json:"deprecated,omitempty"`
}

// ParseSchemaID splits a schema ID into namespace, name, and version.
// Returns ("", "", 0, error) if the format is invalid.
// Expected format: "namespace.name.vN" (e.g. "slack.message.v1").
func ParseSchemaID(schemaID string) (namespace, name string, version int, err error) {
	if schemaID == "" {
		return "", "", 0, fmt.Errorf("empty schema ID")
	}

	parts := strings.Split(schemaID, ".")
	if len(parts) < 3 {
		return "", "", 0, fmt.Errorf("invalid schema ID %q: expected namespace.name.vN", schemaID)
	}

	// The last part is the version (e.g. "v1", "v2").
	versionStr := parts[len(parts)-1]
	if len(versionStr) < 2 || versionStr[0] != 'v' {
		return "", "", 0, fmt.Errorf("invalid version in schema ID %q: expected vN", schemaID)
	}

	v := 0
	for _, c := range versionStr[1:] {
		if c < '0' || c > '9' {
			return "", "", 0, fmt.Errorf("invalid version number in schema ID %q", schemaID)
		}
		v = v*10 + int(c-'0')
	}
	if v == 0 {
		return "", "", 0, fmt.Errorf("version must be >= 1 in schema ID %q", schemaID)
	}

	namespace = parts[0]
	name = strings.Join(parts[1:len(parts)-1], ".")
	return namespace, name, v, nil
}

// SchemaKey returns the unversioned schema key (namespace.name).
func SchemaKey(namespace, name string) string {
	return namespace + "." + name
}

// SchemaRegistry manages versioned payload schemas for AgentMail messages.
// It supports schema registration, lookup, version negotiation, and basic
// structural validation of payloads against registered schemas.
//
// Thread-safe for concurrent use.
type SchemaRegistry struct {
	mu      sync.RWMutex
	schemas map[string]map[int]*SchemaVersion // key -> version -> schema
}

// NewSchemaRegistry creates an empty schema registry.
func NewSchemaRegistry() *SchemaRegistry {
	return &SchemaRegistry{
		schemas: make(map[string]map[int]*SchemaVersion),
	}
}

// Register adds a schema version to the registry.
// Returns an error if a schema with the same ID is already registered.
func (r *SchemaRegistry) Register(schema *SchemaVersion) error {
	if schema == nil {
		return fmt.Errorf("schema must not be nil")
	}

	ns, name, version, err := ParseSchemaID(schema.SchemaID)
	if err != nil {
		return fmt.Errorf("agentmail.SchemaRegistry.Register: %w", err)
	}

	schema.Namespace = ns
	schema.Name = name
	schema.Version = version

	key := SchemaKey(ns, name)

	r.mu.Lock()
	defer r.mu.Unlock()

	versions, ok := r.schemas[key]
	if !ok {
		versions = make(map[int]*SchemaVersion)
		r.schemas[key] = versions
	}

	if _, exists := versions[version]; exists {
		return fmt.Errorf("schema %q already registered", schema.SchemaID)
	}

	versions[version] = schema
	return nil
}

// Get returns a specific schema version.
// Returns nil if not found.
func (r *SchemaRegistry) Get(schemaID string) *SchemaVersion {
	ns, name, version, err := ParseSchemaID(schemaID)
	if err != nil {
		return nil
	}

	key := SchemaKey(ns, name)

	r.mu.RLock()
	defer r.mu.RUnlock()

	versions, ok := r.schemas[key]
	if !ok {
		return nil
	}
	return versions[version]
}

// Latest returns the highest version of a schema by namespace and name.
// Returns nil if no versions are registered.
func (r *SchemaRegistry) Latest(namespace, name string) *SchemaVersion {
	key := SchemaKey(namespace, name)

	r.mu.RLock()
	defer r.mu.RUnlock()

	versions, ok := r.schemas[key]
	if !ok {
		return nil
	}

	var maxV int
	var latest *SchemaVersion
	for v, s := range versions {
		if v > maxV {
			maxV = v
			latest = s
		}
	}
	return latest
}

// Versions returns all registered versions for a schema, sorted ascending.
func (r *SchemaRegistry) Versions(namespace, name string) []*SchemaVersion {
	key := SchemaKey(namespace, name)

	r.mu.RLock()
	defer r.mu.RUnlock()

	versions, ok := r.schemas[key]
	if !ok {
		return nil
	}

	result := make([]*SchemaVersion, 0, len(versions))
	for _, s := range versions {
		result = append(result, s)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Version < result[j].Version
	})
	return result
}

// Negotiate selects the best schema version that both sender and receiver support.
// Given a requested schema ID and a set of supported schema IDs, it returns the
// highest mutually supported version. Returns the requested version if it is
// directly supported, otherwise falls back to the highest common version.
//
// Returns ("", error) if no compatible version is found.
func (r *SchemaRegistry) Negotiate(requested string, supported []string) (string, error) {
	reqNS, reqName, reqVersion, err := ParseSchemaID(requested)
	if err != nil {
		return "", fmt.Errorf("invalid requested schema: %w", err)
	}

	reqKey := SchemaKey(reqNS, reqName)

	// Build set of supported versions for the same schema.
	supportedVersions := make(map[int]string) // version -> schemaID
	for _, sid := range supported {
		ns, name, v, parseErr := ParseSchemaID(sid)
		if parseErr != nil {
			continue
		}
		if SchemaKey(ns, name) == reqKey {
			supportedVersions[v] = sid
		}
	}

	// Direct match?
	if sid, ok := supportedVersions[reqVersion]; ok {
		return sid, nil
	}

	// No direct match — find highest common version.
	if len(supportedVersions) == 0 {
		return "", fmt.Errorf("no compatible version for schema %q", reqKey)
	}

	var bestVersion int
	var bestID string
	for v, sid := range supportedVersions {
		if v > bestVersion {
			bestVersion = v
			bestID = sid
		}
	}

	return bestID, nil
}

// Validate checks a JSON payload against a registered schema's field definitions.
// Returns nil if the schema is not registered (permissive for unknown schemas).
// Returns an error listing missing required fields or type mismatches.
func (r *SchemaRegistry) Validate(schemaID string, payload json.RawMessage) error {
	schema := r.Get(schemaID)
	if schema == nil {
		return nil // permissive: unknown schemas pass validation
	}

	if len(payload) == 0 || string(payload) == "null" {
		if len(schema.RequiredFields) > 0 {
			return fmt.Errorf("payload is empty but schema %q requires fields: %v",
				schemaID, schema.RequiredFields)
		}
		return nil
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(payload, &obj); err != nil {
		return fmt.Errorf("payload is not a JSON object: %w", err)
	}

	// Check required fields.
	var missing []string
	for _, field := range schema.RequiredFields {
		if _, ok := obj[field]; !ok {
			missing = append(missing, field)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required fields for schema %q: %v", schemaID, missing)
	}

	// Check field types for declared fields that are present.
	var typeErrors []string
	for fieldName, expectedType := range schema.Fields {
		raw, ok := obj[fieldName]
		if !ok {
			continue // optional field not present
		}

		if !checkJSONType(raw, expectedType) {
			typeErrors = append(typeErrors, fmt.Sprintf("field %q: expected %s", fieldName, expectedType))
		}
	}
	if len(typeErrors) > 0 {
		sort.Strings(typeErrors)
		return fmt.Errorf("type mismatches in schema %q: %s", schemaID, strings.Join(typeErrors, "; "))
	}

	return nil
}

// List returns all registered schema IDs.
func (r *SchemaRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var ids []string
	for _, versions := range r.schemas {
		for _, s := range versions {
			ids = append(ids, s.SchemaID)
		}
	}
	sort.Strings(ids)
	return ids
}

// RegisterBuiltinSchemas registers the built-in adapter schemas.
// Called during system initialization to ensure all adapter payloads are typed.
func (r *SchemaRegistry) RegisterBuiltinSchemas() {
	builtins := []*SchemaVersion{
		{
			SchemaID: "webhook.delivery.v1",
			Fields: map[string]string{
				"url":         "string",
				"method":      "string",
				"status_code": "number",
			},
			RequiredFields: []string{"url"},
		},
		{
			SchemaID: "slack.message.v1",
			Fields: map[string]string{
				"text":    "string",
				"user":    "string",
				"ts":      "string",
				"channel": "string",
			},
			RequiredFields: []string{"text"},
		},
		{
			SchemaID: "discord.message.v1",
			Fields: map[string]string{
				"content":    "string",
				"author_id":  "string",
				"author":     "string",
				"message_id": "string",
				"channel_id": "string",
			},
			RequiredFields: []string{"content"},
		},
		{
			SchemaID: "email.message.v1",
			Fields: map[string]string{
				"from":         "string",
				"to":           "string",
				"subject":      "string",
				"body":         "string",
				"content_type": "string",
			},
			RequiredFields: []string{"from", "subject"},
		},
		{
			SchemaID: "agentmail.envelope.v1",
			Fields: map[string]string{
				"task":    "string",
				"command": "string",
				"data":    "object",
			},
		},
	}

	for _, schema := range builtins {
		_ = r.Register(schema) // ignore errors for idempotent re-registration
	}
}

// checkJSONType checks if a raw JSON value matches the expected type name.
// Supported types: "string", "number", "boolean", "object", "array", "null".
func checkJSONType(raw json.RawMessage, expected string) bool {
	if len(raw) == 0 {
		return expected == "null"
	}

	first := raw[0]
	switch expected {
	case "string":
		return first == '"'
	case "number":
		return first == '-' || (first >= '0' && first <= '9')
	case "boolean":
		return first == 't' || first == 'f'
	case "object":
		return first == '{'
	case "array":
		return first == '['
	case "null":
		return string(raw) == "null"
	default:
		return true // unknown type: permissive
	}
}
