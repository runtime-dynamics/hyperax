package plugin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hyperax/hyperax/pkg/types"
)

func TestParseManifest_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "hyperax-plugin.yaml")

	content := `
name: "test-plugin"
version: "1.0.0"
type: "wasm"
description: "A test plugin"
author: "test@example.com"
min_hyperax_version: "1.0.0"
api_version: "1.0.0"
permissions:
  - workspace:read
  - tools:register
entrypoint: "./test.wasm"
tools:
  - name: "test_tool"
    description: "A test tool"
    parameters:
      - name: "input"
        type: "string"
        required: true
        description: "Input string"
      - name: "format"
        type: "string"
        required: false
        default: "json"
        description: "Output format"
health_check:
  interval: "30s"
  timeout: "5s"
`
	if err := os.WriteFile(manifestPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := ParseManifest(manifestPath)
	if err != nil {
		t.Fatalf("ParseManifest() error: %v", err)
	}

	if m.Name != "test-plugin" {
		t.Errorf("Name = %q, want %q", m.Name, "test-plugin")
	}
	if m.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", m.Version, "1.0.0")
	}
	if m.Type != types.PluginTypeWasm {
		t.Errorf("Type = %q, want %q", m.Type, types.PluginTypeWasm)
	}
	if m.Description != "A test plugin" {
		t.Errorf("Description = %q, want %q", m.Description, "A test plugin")
	}
	if len(m.Permissions) != 2 {
		t.Errorf("Permissions count = %d, want 2", len(m.Permissions))
	}
	if len(m.Tools) != 1 {
		t.Fatalf("Tools count = %d, want 1", len(m.Tools))
	}
	if m.Tools[0].Name != "test_tool" {
		t.Errorf("Tool[0].Name = %q, want %q", m.Tools[0].Name, "test_tool")
	}
	if len(m.Tools[0].Parameters) != 2 {
		t.Errorf("Tool[0].Parameters count = %d, want 2", len(m.Tools[0].Parameters))
	}
	if m.Tools[0].Parameters[0].Required != true {
		t.Error("Tool[0].Parameters[0].Required = false, want true")
	}
	if m.Tools[0].Parameters[1].Default != "json" {
		t.Errorf("Tool[0].Parameters[1].Default = %v, want %q", m.Tools[0].Parameters[1].Default, "json")
	}
}

func TestParseManifest_FileNotFound(t *testing.T) {
	_, err := ParseManifest("/nonexistent/path/hyperax-plugin.yaml")
	if err == nil {
		t.Fatal("ParseManifest() should return error for missing file")
	}
}

func TestParseManifest_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "hyperax-plugin.yaml")

	if err := os.WriteFile(manifestPath, []byte("not: [valid: yaml: {{"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseManifest(manifestPath)
	if err == nil {
		t.Fatal("ParseManifest() should return error for invalid YAML")
	}
}

func TestValidateManifest_Valid(t *testing.T) {
	m := &types.PluginManifest{
		Name:    "valid-plugin",
		Version: "1.0.0",
		Type:    types.PluginTypeWasm,
		Tools: []types.ToolDef{
			{Name: "my_tool", Description: "Does something"},
		},
	}
	if err := ValidateManifest(m, "dev"); err != nil {
		t.Errorf("ValidateManifest() unexpected error: %v", err)
	}
}

func TestValidateManifest_EmptyName(t *testing.T) {
	m := &types.PluginManifest{
		Version: "1.0.0",
		Type:    types.PluginTypeWasm,
		Tools:   []types.ToolDef{{Name: "t", Description: "d"}},
	}
	err := ValidateManifest(m, "dev")
	if err == nil {
		t.Fatal("ValidateManifest() should reject empty name")
	}
	if got := err.Error(); got != "manifest validation: name is required" {
		t.Errorf("error = %q, want name required message", got)
	}
}

func TestValidateManifest_EmptyVersion(t *testing.T) {
	m := &types.PluginManifest{
		Name: "my-plugin",
		Type: types.PluginTypeWasm,
		Tools: []types.ToolDef{
			{Name: "t", Description: "d"},
		},
	}
	err := ValidateManifest(m, "dev")
	if err == nil {
		t.Fatal("ValidateManifest() should reject empty version")
	}
}

func TestValidateManifest_InvalidType(t *testing.T) {
	m := &types.PluginManifest{
		Name:    "my-plugin",
		Version: "1.0.0",
		Type:    "invalid",
		Tools:   []types.ToolDef{{Name: "t", Description: "d"}},
	}
	err := ValidateManifest(m, "dev")
	if err == nil {
		t.Fatal("ValidateManifest() should reject invalid type")
	}
}

func TestValidateManifest_NoTools(t *testing.T) {
	m := &types.PluginManifest{
		Name:    "my-plugin",
		Version: "1.0.0",
		Type:    types.PluginTypeWasm,
		Tools:   []types.ToolDef{},
	}
	err := ValidateManifest(m, "dev")
	if err == nil {
		t.Fatal("ValidateManifest() should reject empty tools list")
	}
}

func TestValidateManifest_ToolEmptyName(t *testing.T) {
	m := &types.PluginManifest{
		Name:    "my-plugin",
		Version: "1.0.0",
		Type:    types.PluginTypeWasm,
		Tools:   []types.ToolDef{{Name: "", Description: "d"}},
	}
	err := ValidateManifest(m, "dev")
	if err == nil {
		t.Fatal("ValidateManifest() should reject tool with empty name")
	}
}

func TestValidateManifest_ToolEmptyDescription(t *testing.T) {
	m := &types.PluginManifest{
		Name:    "my-plugin",
		Version: "1.0.0",
		Type:    types.PluginTypeWasm,
		Tools:   []types.ToolDef{{Name: "t", Description: ""}},
	}
	err := ValidateManifest(m, "dev")
	if err == nil {
		t.Fatal("ValidateManifest() should reject tool with empty description")
	}
}

func TestValidateManifest_AllPluginTypes(t *testing.T) {
	validTypes := []types.PluginType{
		types.PluginTypeWasm,
		types.PluginTypeMCP,
		types.PluginTypeHTTP,
		types.PluginTypeNative,
	}

	for _, pt := range validTypes {
		t.Run(string(pt), func(t *testing.T) {
			m := &types.PluginManifest{
				Name:    "my-plugin",
				Version: "1.0.0",
				Type:    pt,
				Tools:   []types.ToolDef{{Name: "t", Description: "d"}},
			}
			if err := ValidateManifest(m, "dev"); err != nil {
				t.Errorf("ValidateManifest() unexpected error for type %q: %v", pt, err)
			}
		})
	}
}
