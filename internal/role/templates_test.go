package role

import (
	"context"
	"testing"
)

func TestBuiltinTemplatesLoaded(t *testing.T) {
	reg := NewRoleTemplateRegistry(nil)
	templates := reg.List()

	expectedIDs := []string{
		"chief_of_staff",
		"security_analyst",
		"code_reviewer",
		"project_manager",
		"technical_writer",
		"qa_engineer",
		"communications_specialist",
		"brand_manager",
		"legal_communications_board",
		"communication_security_manager",
		"sre_team_lead",
		"personal_coach",
		"team_lead",
		"backend_developer",
		"frontend_developer",
		"spec_writer",
		"office_manager",
		"concept_artist",
		"designer",
		"research_assistant",
		"community_manager",
	}

	if len(templates) != len(expectedIDs) {
		t.Errorf("expected %d built-in templates, got %d", len(expectedIDs), len(templates))
	}

	for _, id := range expectedIDs {
		tmpl := reg.Get(id)
		if tmpl == nil {
			t.Errorf("built-in template %q not found", id)
			continue
		}
		if !tmpl.BuiltIn {
			t.Errorf("template %q should be marked as built-in", id)
		}
		if tmpl.SystemPrompt == "" {
			t.Errorf("template %q has empty system prompt", id)
		}
		if tmpl.Name == "" {
			t.Errorf("template %q has empty name", id)
		}
	}
}

func TestGetReturnsNilForUnknown(t *testing.T) {
	reg := NewRoleTemplateRegistry(nil)
	if tmpl := reg.Get("nonexistent"); tmpl != nil {
		t.Error("expected nil for unknown template ID")
	}
}

func TestRegisterCustomTemplate(t *testing.T) {
	reg := NewRoleTemplateRegistry(nil)

	custom := &RoleTemplate{
		ID:             "devops_engineer",
		Name:           "DevOps Engineer",
		Description:    "Manages CI/CD and infrastructure.",
		SystemPrompt:   "You are a DevOps Engineer agent.",
		SuggestedModel: "claude-haiku-4-5",
		ClearanceLevel: 2,
	}

	if err := reg.Register(context.Background(), custom); err != nil {
		t.Fatalf("register custom template: %v", err)
	}

	got := reg.Get("devops_engineer")
	if got == nil {
		t.Fatal("custom template not found after registration")
	}
	if got.BuiltIn {
		t.Error("custom template should not be marked as built-in")
	}
	if got.SystemPrompt != "You are a DevOps Engineer agent." {
		t.Error("system prompt mismatch")
	}
}

func TestCannotOverrideBuiltin(t *testing.T) {
	reg := NewRoleTemplateRegistry(nil)

	override := &RoleTemplate{
		ID:           "security_analyst",
		SystemPrompt: "overridden",
	}

	err := reg.Register(context.Background(), override)
	if err == nil {
		t.Error("expected error when overriding built-in template")
	}
}

func TestCustomTemplatePriority(t *testing.T) {
	reg := NewRoleTemplateRegistry(nil)

	// Custom template with unique ID should be retrievable.
	custom := &RoleTemplate{
		ID:           "custom_role",
		Name:         "Custom Role",
		SystemPrompt: "Custom prompt.",
	}
	if err := reg.Register(context.Background(), custom); err != nil {
		t.Fatalf("register: %v", err)
	}

	// List should include both built-ins and custom.
	all := reg.List()
	if len(all) != 22 { // 21 built-in + 1 custom
		t.Errorf("expected 22 templates, got %d", len(all))
	}
}
