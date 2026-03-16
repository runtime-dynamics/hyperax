package context

import (
	"strings"
	"testing"

	"github.com/hyperax/hyperax/internal/role"
)

func TestBuildStructuredSystemPrompt_BasicIdentity(t *testing.T) {
	prompt := BuildStructuredSystemPrompt(PromptBuilderConfig{
		AgentName:   "Atlas",
		Personality: "Calm and analytical",
	})

	if !strings.Contains(prompt, "<identity>") {
		t.Error("expected <identity> opening tag")
	}
	if !strings.Contains(prompt, "</identity>") {
		t.Error("expected </identity> closing tag")
	}
	if !strings.Contains(prompt, "You are, Atlas.") {
		t.Error("expected agent name in identity block")
	}
	if !strings.Contains(prompt, "Your personality is: Calm and analytical") {
		t.Error("expected personality in identity block")
	}
}

func TestBuildStructuredSystemPrompt_IdentityWithoutPersonality(t *testing.T) {
	prompt := BuildStructuredSystemPrompt(PromptBuilderConfig{
		AgentName: "Scout",
	})

	if !strings.Contains(prompt, "You are, Scout.") {
		t.Error("expected agent name in identity block")
	}
	if strings.Contains(prompt, "Your personality is:") {
		t.Error("personality clause should be absent when personality is empty")
	}
}

func TestBuildStructuredSystemPrompt_TemplateRole(t *testing.T) {
	prompt := BuildStructuredSystemPrompt(PromptBuilderConfig{
		AgentName:   "Atlas",
		Personality: "Calm",
		Template: &role.RoleTemplate{
			Description: "Chief architect overseeing system design",
		},
	})

	if !strings.Contains(prompt, "<role>") {
		t.Error("expected <role> opening tag")
	}
	if !strings.Contains(prompt, "</role>") {
		t.Error("expected </role> closing tag")
	}
	// Template description should take precedence over personality in role block.
	if !strings.Contains(prompt, "Chief architect overseeing system design") {
		t.Error("expected template description in role block")
	}
}

func TestBuildStructuredSystemPrompt_RoleFallsBackToPersonality(t *testing.T) {
	prompt := BuildStructuredSystemPrompt(PromptBuilderConfig{
		AgentName:   "Atlas",
		Personality: "Methodical problem solver",
	})

	if !strings.Contains(prompt, "<role>") {
		t.Error("expected <role> tag when personality is set and no template")
	}
	if !strings.Contains(prompt, "Methodical problem solver") {
		t.Error("expected personality as fallback in role block")
	}
}

func TestBuildStructuredSystemPrompt_NoRoleBlockWhenEmpty(t *testing.T) {
	prompt := BuildStructuredSystemPrompt(PromptBuilderConfig{
		AgentName: "Atlas",
	})

	if strings.Contains(prompt, "<role>") {
		t.Error("role block should be absent when both personality and template description are empty")
	}
}

func TestBuildStructuredSystemPrompt_InstructionsPrecedence(t *testing.T) {
	// Agent SystemPrompt should override template SystemPrompt.
	prompt := BuildStructuredSystemPrompt(PromptBuilderConfig{
		AgentName:    "Atlas",
		SystemPrompt: "Agent-level custom instructions",
		Template: &role.RoleTemplate{
			SystemPrompt: "Template-level default instructions",
		},
	})

	if !strings.Contains(prompt, "<Instructions>") {
		t.Error("expected <Instructions> tag")
	}
	if !strings.Contains(prompt, "Agent-level custom instructions") {
		t.Error("expected agent system prompt to take precedence")
	}
	if strings.Contains(prompt, "Template-level default instructions") {
		t.Error("template system prompt should NOT appear when agent has its own")
	}
}

func TestBuildStructuredSystemPrompt_InstructionsFromTemplate(t *testing.T) {
	// When agent has no SystemPrompt, template's should be used.
	prompt := BuildStructuredSystemPrompt(PromptBuilderConfig{
		AgentName: "Atlas",
		Template: &role.RoleTemplate{
			SystemPrompt: "Template-level default instructions",
		},
	})

	if !strings.Contains(prompt, "Template-level default instructions") {
		t.Error("expected template system prompt when agent has none")
	}
}

func TestBuildStructuredSystemPrompt_NoInstructionsBlock(t *testing.T) {
	prompt := BuildStructuredSystemPrompt(PromptBuilderConfig{
		AgentName: "Atlas",
	})

	if strings.Contains(prompt, "<Instructions>") {
		t.Error("Instructions block should be absent when no system prompt exists")
	}
}

func TestBuildStructuredSystemPrompt_EngagementRulesFromTemplate(t *testing.T) {
	prompt := BuildStructuredSystemPrompt(PromptBuilderConfig{
		AgentName: "Atlas",
		Template: &role.RoleTemplate{
			EngagementRules: []role.EngagementRule{
				{
					ID:      "code-review",
					Trigger: "When code changes are submitted",
					Chain: []role.EngagementRuleStep{
						{Role: "reviewer", Action: "review code"},
						{Role: "architect", Action: "approve design"},
					},
				},
			},
		},
	})

	if !strings.Contains(prompt, "<Engagement Model>") {
		t.Error("expected <Engagement Model> tag")
	}
	if !strings.Contains(prompt, "</Engagement Model>") {
		t.Error("expected </Engagement Model> closing tag")
	}
	if !strings.Contains(prompt, "When code changes are submitted") {
		t.Error("expected trigger text in engagement model")
	}
	if !strings.Contains(prompt, "reviewer (review code)") {
		t.Error("expected chain step with role and action")
	}
	if !strings.Contains(prompt, "architect (approve design)") {
		t.Error("expected second chain step")
	}
}

func TestBuildStructuredSystemPrompt_EngagementRulesAgentOverride(t *testing.T) {
	// Agent JSON rules should merge on top of template rules.
	prompt := BuildStructuredSystemPrompt(PromptBuilderConfig{
		AgentName: "Atlas",
		Template: &role.RoleTemplate{
			EngagementRules: []role.EngagementRule{
				{
					ID:      "deploy",
					Trigger: "When deployment is requested",
					Chain: []role.EngagementRuleStep{
						{Role: "ops", Action: "deploy"},
					},
				},
			},
		},
		EngagementRules: `[{"id":"custom","trigger":"When alerts fire","chain":[{"role":"oncall","action":"investigate"}]}]`,
	})

	if !strings.Contains(prompt, "<Engagement Model>") {
		t.Error("expected engagement model block")
	}
	// Both template rule and agent rule should be present after merge.
	if !strings.Contains(prompt, "When deployment is requested") {
		t.Error("expected template rule to be preserved in merge")
	}
	if !strings.Contains(prompt, "When alerts fire") {
		t.Error("expected agent override rule in merge")
	}
	if !strings.Contains(prompt, "oncall (investigate)") {
		t.Error("expected agent chain step")
	}
}

func TestBuildStructuredSystemPrompt_EngagementRulesEmptyArray(t *testing.T) {
	// Explicit "[]" should suppress all engagement rules, even from template.
	prompt := BuildStructuredSystemPrompt(PromptBuilderConfig{
		AgentName: "Atlas",
		Template: &role.RoleTemplate{
			EngagementRules: []role.EngagementRule{
				{
					ID:      "deploy",
					Trigger: "When deployment is requested",
				},
			},
		},
		EngagementRules: "[]",
	})

	if strings.Contains(prompt, "<Engagement Model>") {
		t.Error("engagement model block should be absent when agent rules are empty array")
	}
}

func TestBuildStructuredSystemPrompt_EngagementRulesInvalidJSON(t *testing.T) {
	// Invalid JSON should fall back to template rules.
	prompt := BuildStructuredSystemPrompt(PromptBuilderConfig{
		AgentName: "Atlas",
		Template: &role.RoleTemplate{
			EngagementRules: []role.EngagementRule{
				{
					ID:      "deploy",
					Trigger: "When deployment is requested",
				},
			},
		},
		EngagementRules: "not valid json",
	})

	if !strings.Contains(prompt, "<Engagement Model>") {
		t.Error("expected engagement model from template fallback")
	}
	if !strings.Contains(prompt, "When deployment is requested") {
		t.Error("expected template rule preserved on invalid agent JSON")
	}
}

func TestBuildStructuredSystemPrompt_SessionSummary(t *testing.T) {
	prompt := BuildStructuredSystemPrompt(PromptBuilderConfig{
		AgentName:      "Atlas",
		SessionSummary: "User discussed project architecture. Decided on microservices approach.",
	})

	if !strings.Contains(prompt, "<Previous Context>") {
		t.Error("expected <Previous Context> tag")
	}
	if !strings.Contains(prompt, "</Previous Context>") {
		t.Error("expected </Previous Context> closing tag")
	}
	if !strings.Contains(prompt, "User discussed project architecture") {
		t.Error("expected session summary content")
	}
}

func TestBuildStructuredSystemPrompt_NoSessionSummary(t *testing.T) {
	prompt := BuildStructuredSystemPrompt(PromptBuilderConfig{
		AgentName: "Atlas",
	})

	if strings.Contains(prompt, "<Previous Context>") {
		t.Error("Previous Context block should be absent when no session summary exists")
	}
}

func TestBuildStructuredSystemPrompt_GlobalHintsAlwaysPresent(t *testing.T) {
	// Global hints should appear in every prompt, even minimal ones.
	prompt := BuildStructuredSystemPrompt(PromptBuilderConfig{
		AgentName: "Atlas",
	})

	if !strings.Contains(prompt, "<Global Hints>") {
		t.Error("expected <Global Hints> tag")
	}
	if !strings.Contains(prompt, "</Global Hints>") {
		t.Error("expected </Global Hints> closing tag")
	}
	if !strings.Contains(prompt, "Store and search/retrieve memories") {
		t.Error("expected memory hint")
	}
	if !strings.Contains(prompt, "Use your tooling") {
		t.Error("expected tooling hint")
	}
	if !strings.Contains(prompt, "Don't construct arbitrary commands") {
		t.Error("expected no-arbitrary-commands hint")
	}
	if !strings.Contains(prompt, "Asking is better than rewriting") {
		t.Error("expected asking hint")
	}
}

func TestBuildStructuredSystemPrompt_FullPromptOrdering(t *testing.T) {
	// Verify that blocks appear in the correct order.
	prompt := BuildStructuredSystemPrompt(PromptBuilderConfig{
		AgentName:    "Atlas",
		Personality:  "Calm and focused",
		SystemPrompt: "You are a senior engineer.",
		Template: &role.RoleTemplate{
			Description: "Lead engineer role",
			EngagementRules: []role.EngagementRule{
				{ID: "review", Trigger: "On PR creation"},
			},
		},
		SessionSummary: "Previous session covered deployment strategy.",
	})

	// Verify order: identity < role < instructions < engagement < hints < context
	idxIdentity := strings.Index(prompt, "<identity>")
	idxRole := strings.Index(prompt, "<role>")
	idxInstructions := strings.Index(prompt, "<Instructions>")
	idxEngagement := strings.Index(prompt, "<Engagement Model>")
	idxHints := strings.Index(prompt, "<Global Hints>")
	idxContext := strings.Index(prompt, "<Previous Context>")

	if idxIdentity >= idxRole {
		t.Error("identity block should appear before role block")
	}
	if idxRole >= idxInstructions {
		t.Error("role block should appear before instructions block")
	}
	if idxInstructions >= idxEngagement {
		t.Error("instructions block should appear before engagement model block")
	}
	if idxEngagement >= idxHints {
		t.Error("engagement model block should appear before global hints block")
	}
	if idxHints >= idxContext {
		t.Error("global hints block should appear before previous context block")
	}
}

func TestBuildStructuredSystemPrompt_EngagementRuleWithoutChain(t *testing.T) {
	// Rules with triggers but no chain steps should render cleanly.
	prompt := BuildStructuredSystemPrompt(PromptBuilderConfig{
		AgentName: "Atlas",
		Template: &role.RoleTemplate{
			EngagementRules: []role.EngagementRule{
				{
					ID:      "alert",
					Trigger: "On system alert",
				},
			},
		},
	})

	if !strings.Contains(prompt, "- On system alert") {
		t.Error("expected trigger without chain arrow")
	}
	// Should NOT contain the arrow separator when there are no chain steps.
	lines := strings.Split(prompt, "\n")
	for _, line := range lines {
		if strings.Contains(line, "On system alert") && strings.Contains(line, "→") {
			t.Error("trigger without chain steps should not have arrow separator")
		}
	}
}
