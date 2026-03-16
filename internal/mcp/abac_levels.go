package mcp

// abacEntry defines the ABAC policy for a single tool.
type abacEntry struct {
	MinClearance int
	Action       string
	ExposedToLLM bool // Whether this tool is visible to the LLM tool-use resolver.
}

// defaultABACLevels maps tool names to their ABAC enforcement policy.
// Clearance tiers (see pkg/types/auth.go):
//   - 0 Observer:      read/search/list/get operations (any caller)
//   - 1 Operator:      create/update, pipeline execution, refactoring, lifecycle
//   - 2 Admin:         config, providers, plugins, alerts, budgets, token management
//   - 3 ChiefOfStaff:  secrets, interjection resolution, sieve bypass, safety overrides
//
// Action types:
//   - "view":       organisational visibility — see what exists (workspaces, projects,
//                   tasks, agents, pipelines, status) but NOT access workspace content
//   - "read":       workspace content access — files, docs, code, logs, standards
//   - "write":      creates or modifies workspace state (code, docs, config)
//   - "execute":    triggers execution (pipelines, sensors, workflows)
//   - "delete":     removes data
//   - "admin":      administrative operations (providers, secrets, plugins)
//   - "coordinate": inter-agent messaging, delegation, task management — no workspace mutation
//
// ExposedToLLM controls whether the tool is included in the LLM tool-use
// resolver. Tools marked false are still callable via direct MCP but the
// autonomous agent loop will never see them.
var defaultABACLevels = map[string]abacEntry{

	// ── Workspace (consolidated: per-action clearance enforced internally) ──
	"workspace": {MinClearance: 0, Action: "view", ExposedToLLM: true},

	// ── Code (consolidated: per-action clearance enforced internally) ──
	"code": {MinClearance: 0, Action: "read", ExposedToLLM: true},

	// ── Doc (consolidated: per-action clearance enforced internally) ──
	// Absorbs docs, external_docs, standards, and specs.
	"doc": {MinClearance: 0, Action: "read", ExposedToLLM: true},

	// ── Project (consolidated: per-action clearance enforced internally) ──
	"project": {MinClearance: 0, Action: "view", ExposedToLLM: true},

	// ── Pipeline (consolidated: per-action clearance enforced internally) ──
	"pipeline": {MinClearance: 0, Action: "view", ExposedToLLM: true},

	// ── Refactor (consolidated: per-action clearance enforced internally) ──
	"refactor": {MinClearance: 1, Action: "write", ExposedToLLM: true},

	// ── Audit (consolidated: per-action clearance enforced internally) ──
	"audit": {MinClearance: 0, Action: "view", ExposedToLLM: false},

	// ── Config (consolidated: per-action clearance enforced internally) ──
	"config": {MinClearance: 0, Action: "view", ExposedToLLM: false},

	// ── Agent (consolidated: per-action clearance enforced internally) ──
	"agent": {MinClearance: 0, Action: "view", ExposedToLLM: true},


	// ── Secret (consolidated: per-action clearance enforced internally) ──
	"secret": {MinClearance: 2, Action: "admin", ExposedToLLM: false},

	// ── Governance (consolidated: per-action clearance enforced internally) ──
	"governance": {MinClearance: 0, Action: "view", ExposedToLLM: true},

	// ── Observability (consolidated: per-action clearance enforced internally) ──
	"observability": {MinClearance: 0, Action: "view", ExposedToLLM: false},

	// ── Memory (consolidated: per-action clearance enforced internally) ──
	"memory": {MinClearance: 0, Action: "read", ExposedToLLM: true},

	// ── Comm (consolidated: per-action clearance enforced internally) ──
	"comm": {MinClearance: 0, Action: "view", ExposedToLLM: true},


	// ── Event (consolidated: per-action clearance enforced internally) ──
	// Nervous system + Pulse engine + Sensor cadences
	"event": {MinClearance: 0, Action: "view", ExposedToLLM: false},


	// ── Plugin (consolidated: per-action clearance enforced internally) ──
	"plugin": {MinClearance: 0, Action: "view", ExposedToLLM: false},


	// Delegation actions are consolidated into the "agent" tool.
}

// ApplyDefaultABACLevels applies the default ABAC clearance levels, action
// types, and LLM exposure flags to all registered tools in the registry.
// Tools not in the map default to clearance 0, action "view", not exposed
// to LLM (set during Register).
//
// Call this after all handlers are registered but before serving requests.
func ApplyDefaultABACLevels(registry *ToolRegistry) {
	for name, entry := range defaultABACLevels {
		registry.SetToolABAC(name, entry.MinClearance, entry.Action)
		if entry.ExposedToLLM {
			registry.SetToolExposedToLLM(name, true)
		}
	}
}
