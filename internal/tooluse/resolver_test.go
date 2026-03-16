package tooluse

import (
	"encoding/json"
	"testing"
)

// testSchemas returns a representative set of tool schemas for testing.
func testSchemas() []ToolSchema {
	return []ToolSchema{
		{Name: "list_tasks", Description: "List all tasks", InputSchema: json.RawMessage(`{}`), MinClearanceLevel: 0, RequiredAction: "view", ExposedToLLM: true},
		{Name: "create_pipeline", Description: "Create a pipeline", InputSchema: json.RawMessage(`{"type":"object"}`), MinClearanceLevel: 1, RequiredAction: "write", ExposedToLLM: true},
		{Name: "run_pipeline", Description: "Run a pipeline", InputSchema: json.RawMessage(`{"type":"object"}`), MinClearanceLevel: 1, RequiredAction: "execute", ExposedToLLM: true},
		{Name: "set_config", Description: "Set config value", InputSchema: json.RawMessage(`{"type":"object"}`), MinClearanceLevel: 2, RequiredAction: "admin", ExposedToLLM: true},
		{Name: "set_secret", Description: "Set a secret", InputSchema: json.RawMessage(`{"type":"object"}`), MinClearanceLevel: 2, RequiredAction: "admin", ExposedToLLM: true},
		{Name: "delete_doc", Description: "Delete a document", InputSchema: json.RawMessage(`{"type":"object"}`), MinClearanceLevel: 1, RequiredAction: "delete", ExposedToLLM: true},
	}
}

func TestResolveTools_ClearanceZero(t *testing.T) {
	r := NewResolver(testSchemas())
	tools := r.ResolveTools(0, nil)

	if len(tools) != 1 {
		t.Fatalf("clearance 0: expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "list_tasks" {
		t.Errorf("clearance 0: expected list_tasks, got %s", tools[0].Name)
	}
}

func TestResolveTools_ClearanceOne(t *testing.T) {
	r := NewResolver(testSchemas())
	tools := r.ResolveTools(1, nil)

	expected := map[string]bool{
		"list_tasks":      true,
		"create_pipeline": true,
		"run_pipeline":    true,
		"delete_doc":      true,
	}
	if len(tools) != len(expected) {
		t.Fatalf("clearance 1: expected %d tools, got %d", len(expected), len(tools))
	}
	for _, td := range tools {
		if !expected[td.Name] {
			t.Errorf("clearance 1: unexpected tool %s", td.Name)
		}
	}
}

func TestResolveTools_ClearanceTwo(t *testing.T) {
	r := NewResolver(testSchemas())
	tools := r.ResolveTools(2, nil)

	if len(tools) != 6 {
		t.Fatalf("clearance 2: expected 6 tools, got %d", len(tools))
	}
}

func TestResolveTools_WildcardDelegation(t *testing.T) {
	r := NewResolver(testSchemas())
	// Clearance 0 + wildcard admin delegation should unlock admin tools.
	tools := r.ResolveTools(0, []string{"tools:admin:*"})

	names := make(map[string]bool)
	for _, td := range tools {
		names[td.Name] = true
	}

	// Should include list_tasks (clearance 0) + set_config + set_secret (admin wildcard).
	if !names["list_tasks"] {
		t.Error("wildcard admin: expected list_tasks")
	}
	if !names["set_config"] {
		t.Error("wildcard admin: expected set_config")
	}
	if !names["set_secret"] {
		t.Error("wildcard admin: expected set_secret")
	}
	// Should NOT include create_pipeline (requires write, not admin).
	if names["create_pipeline"] {
		t.Error("wildcard admin: should not include create_pipeline (requires write)")
	}
}

func TestResolveTools_SpecificToolDelegation(t *testing.T) {
	r := NewResolver(testSchemas())
	// Clearance 0 + specific execute grant for run_pipeline.
	tools := r.ResolveTools(0, []string{"tools:execute:run_pipeline"})

	names := make(map[string]bool)
	for _, td := range tools {
		names[td.Name] = true
	}

	if !names["list_tasks"] {
		t.Error("specific delegation: expected list_tasks (clearance 0)")
	}
	if !names["run_pipeline"] {
		t.Error("specific delegation: expected run_pipeline (delegated)")
	}
	if names["create_pipeline"] {
		t.Error("specific delegation: should not include create_pipeline")
	}
	if len(tools) != 2 {
		t.Errorf("specific delegation: expected 2 tools, got %d", len(tools))
	}
}

func TestResolveTools_MultipleDelegationScopes(t *testing.T) {
	r := NewResolver(testSchemas())
	scopes := []string{
		"tools:write:create_pipeline",
		"tools:delete:delete_doc",
		"tools:admin:set_config",
	}
	tools := r.ResolveTools(0, scopes)

	names := make(map[string]bool)
	for _, td := range tools {
		names[td.Name] = true
	}

	expected := []string{"list_tasks", "create_pipeline", "delete_doc", "set_config"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("multi-scope: expected %s", name)
		}
	}
	if len(tools) != len(expected) {
		t.Errorf("multi-scope: expected %d tools, got %d", len(expected), len(tools))
	}
}

func TestResolveTools_InvalidScopesIgnored(t *testing.T) {
	r := NewResolver(testSchemas())
	// Non-tools scopes and malformed scopes should be silently ignored.
	tools := r.ResolveTools(0, []string{
		"not:a:tools:scope",
		"tools:read", // missing target
		"invalid",
		"",
	})

	if len(tools) != 1 {
		t.Fatalf("invalid scopes: expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "list_tasks" {
		t.Errorf("invalid scopes: expected list_tasks, got %s", tools[0].Name)
	}
}

func TestResolveTools_EmptyRegistry(t *testing.T) {
	r := NewResolver(nil)
	tools := r.ResolveTools(2, []string{"tools:admin:*"})

	if len(tools) != 0 {
		t.Fatalf("empty registry: expected 0 tools, got %d", len(tools))
	}
}

func TestResolveTools_PreservesOrder(t *testing.T) {
	r := NewResolver(testSchemas())
	tools := r.ResolveTools(2, nil)

	expectedOrder := []string{"list_tasks", "create_pipeline", "run_pipeline", "set_config", "set_secret", "delete_doc"}
	for i, td := range tools {
		if td.Name != expectedOrder[i] {
			t.Errorf("order: position %d expected %s, got %s", i, expectedOrder[i], td.Name)
		}
	}
}

func TestResolveTools_ToolDefinitionFields(t *testing.T) {
	schema := []ToolSchema{
		{
			Name:              "test_tool",
			Description:       "A test tool",
			InputSchema:       json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}}}`),
			MinClearanceLevel: 0,
			RequiredAction:    "view",
			ExposedToLLM:      true,
		},
	}
	r := NewResolver(schema)
	tools := r.ResolveTools(0, nil)

	if len(tools) != 1 {
		t.Fatalf("fields: expected 1 tool, got %d", len(tools))
	}
	td := tools[0]
	if td.Name != "test_tool" {
		t.Errorf("fields: name = %s", td.Name)
	}
	if td.Description != "A test tool" {
		t.Errorf("fields: description = %s", td.Description)
	}
	var schema2 map[string]any
	if err := json.Unmarshal(td.InputSchema, &schema2); err != nil {
		t.Fatalf("fields: InputSchema not valid JSON: %v", err)
	}
	if schema2["type"] != "object" {
		t.Errorf("fields: InputSchema type = %v", schema2["type"])
	}
}

func TestResolveTools_DefaultAction(t *testing.T) {
	// Tool with empty RequiredAction should default to "view".
	schema := []ToolSchema{
		{Name: "no_action", Description: "No action set", InputSchema: json.RawMessage(`{}`), MinClearanceLevel: 1, ExposedToLLM: true},
	}
	r := NewResolver(schema)

	// Clearance 0 should not see it.
	tools := r.ResolveTools(0, nil)
	if len(tools) != 0 {
		t.Fatalf("default action: expected 0 tools at clearance 0, got %d", len(tools))
	}

	// But a view wildcard delegation should unlock it.
	tools = r.ResolveTools(0, []string{"tools:view:*"})
	if len(tools) != 1 {
		t.Fatalf("default action: expected 1 tool with view wildcard, got %d", len(tools))
	}
}

func TestResolveTools_ExposedToLLMFilter(t *testing.T) {
	schemas := []ToolSchema{
		{Name: "exposed_tool", Description: "Exposed", InputSchema: json.RawMessage(`{}`), MinClearanceLevel: 0, RequiredAction: "view", ExposedToLLM: true},
		{Name: "hidden_tool", Description: "Hidden", InputSchema: json.RawMessage(`{}`), MinClearanceLevel: 0, RequiredAction: "view", ExposedToLLM: false},
	}
	r := NewResolver(schemas)
	tools := r.ResolveTools(2, nil) // High clearance, but hidden_tool should still be excluded.

	if len(tools) != 1 {
		t.Fatalf("exposed filter: expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "exposed_tool" {
		t.Errorf("exposed filter: expected exposed_tool, got %s", tools[0].Name)
	}
}

func TestParseDelegationScopes(t *testing.T) {
	scopes := []string{
		"tools:view:*",
		"tools:write:create_pipeline",
		"tools:admin:set_config",
		"not-a-tools-scope",
		"other:scope:format",
	}

	g := parseDelegationScopes(scopes)

	if !g.wildcardActions["view"] {
		t.Error("parse: expected wildcard view")
	}
	if g.wildcardActions["write"] {
		t.Error("parse: unexpected wildcard write")
	}
	if !g.toolActions["create_pipeline"]["write"] {
		t.Error("parse: expected write on create_pipeline")
	}
	if !g.toolActions["set_config"]["admin"] {
		t.Error("parse: expected admin on set_config")
	}
}
