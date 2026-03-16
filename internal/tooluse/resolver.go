// Package tooluse provides the Tool Surface Resolver — the component that
// determines which MCP tools are visible and callable for a given persona
// based on ABAC clearance levels and delegation-granted scopes.
//
// The Resolver is designed for use from the CommHub routing layer and LLM
// provider adapters without creating circular imports (it depends only on
// pkg/types and the ToolSchema slice contract from the registry).
package tooluse

import (
	"strings"

	"github.com/hyperax/hyperax/pkg/types"
)

// ToolSchema mirrors mcp.ToolSchema to avoid importing the mcp package.
// The caller is responsible for converting mcp.ToolSchema slices into this type.
type ToolSchema struct {
	Name              string
	Description       string
	InputSchema       []byte // json.RawMessage
	MinClearanceLevel int
	RequiredAction    string
	ExposedToLLM      bool // Only tools with ExposedToLLM=true are sent to the LLM.
}

// Resolver filters the full tool registry down to the subset a persona is
// permitted to invoke, considering ABAC clearance and delegation scopes.
type Resolver struct {
	schemas []ToolSchema
}

// NewResolver creates a Resolver over the given tool schemas.
// The schemas slice is typically obtained from ToolRegistry.Schemas() and
// converted to the local ToolSchema type to avoid an import cycle.
func NewResolver(schemas []ToolSchema) *Resolver {
	return &Resolver{schemas: schemas}
}

// ResolveTools returns the tool definitions accessible at the given clearance
// level, optionally expanded by delegation scopes.
//
// Delegation scopes follow the format "tools:<action>:<target>" where:
//   - action is "view", "read", "write", "execute", "delete", "coordinate", or "admin"
//   - target is a tool name or "*" for wildcard
//
// A tool is included if:
//  1. The persona's clearance level >= the tool's MinClearanceLevel, OR
//  2. A delegation scope explicitly grants access to that tool's action.
// ResolveTools returns the tool definitions accessible at the given clearance
// level, optionally expanded by delegation scopes and restricted by allowed
// actions.
//
// allowedActions restricts which tool action types are included. If empty or
// nil, all action types are allowed (no restriction beyond ABAC). When
// populated, only tools whose RequiredAction is in the set are returned.
// This enables role-scoped tool profiles (e.g., QA sees only "view"+"read"+"execute",
// Chief of Staff sees only "view"+"read"+"coordinate").
func (r *Resolver) ResolveTools(clearanceLevel int, delegationScopes []string, allowedActions ...string) []types.ToolDefinition {
	grants := parseDelegationScopes(delegationScopes)

	// Build allowed action set for O(1) lookup.
	var actionFilter map[string]bool
	if len(allowedActions) > 0 {
		actionFilter = make(map[string]bool, len(allowedActions))
		for _, a := range allowedActions {
			actionFilter[a] = true
		}
	}

	out := make([]types.ToolDefinition, 0, len(r.schemas))
	for _, s := range r.schemas {
		// Only include tools explicitly marked for LLM exposure.
		if !s.ExposedToLLM {
			continue
		}
		// Apply role-scoped action filter.
		if actionFilter != nil {
			action := s.RequiredAction
			if action == "" {
				action = "view"
			}
			if !actionFilter[action] {
				continue
			}
		}
		if !isAccessible(s, clearanceLevel, grants) {
			continue
		}
		out = append(out, types.ToolDefinition{
			Name:        s.Name,
			Description: s.Description,
			InputSchema: s.InputSchema,
		})
	}
	return out
}

// delegationGrants is the parsed representation of delegation scopes for
// efficient lookup during resolution.
type delegationGrants struct {
	// wildcardActions contains actions granted for all tools (target="*").
	wildcardActions map[string]bool

	// toolActions maps tool name → set of granted actions.
	toolActions map[string]map[string]bool
}

// parseDelegationScopes parses "tools:<action>:<target>" scope strings into
// a lookup-friendly structure.
func parseDelegationScopes(scopes []string) delegationGrants {
	g := delegationGrants{
		wildcardActions: make(map[string]bool),
		toolActions:     make(map[string]map[string]bool),
	}

	for _, scope := range scopes {
		parts := strings.SplitN(scope, ":", 3)
		if len(parts) != 3 || parts[0] != "tools" {
			continue
		}

		action := parts[1]
		target := parts[2]

		if target == "*" {
			g.wildcardActions[action] = true
		} else {
			if g.toolActions[target] == nil {
				g.toolActions[target] = make(map[string]bool)
			}
			g.toolActions[target][action] = true
		}
	}

	return g
}

// isAccessible reports whether the tool is accessible at the given clearance
// level or via delegation grants.
func isAccessible(schema ToolSchema, clearanceLevel int, grants delegationGrants) bool {
	// Direct clearance check.
	if clearanceLevel >= schema.MinClearanceLevel {
		return true
	}

	// Check delegation grants — the tool's RequiredAction must be granted.
	action := schema.RequiredAction
	if action == "" {
		action = "view"
	}

	// Wildcard grant for this action.
	if grants.wildcardActions[action] {
		return true
	}

	// Specific tool grant for this action.
	if actions, ok := grants.toolActions[schema.Name]; ok && actions[action] {
		return true
	}

	return false
}
