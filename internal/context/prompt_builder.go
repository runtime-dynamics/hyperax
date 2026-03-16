package context

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hyperax/hyperax/internal/role"
)

// PromptBuilderConfig holds all inputs for structured prompt construction.
type PromptBuilderConfig struct {
	// AgentName is the display name of the agent.
	AgentName string
	// Personality is the agent's personality description.
	Personality string
	// SystemPrompt is the agent's explicit system prompt override.
	// When set, it takes precedence over the template's SystemPrompt.
	SystemPrompt string
	// RoleTemplateID is the ID of the linked role template (informational).
	RoleTemplateID string
	// Template is the resolved role template. May be nil if no template is linked.
	Template *role.RoleTemplate
	// EngagementRules is the agent's JSON engagement rules override.
	// Empty string means "use template rules as-is". An explicit "[]"
	// means "suppress all engagement rules".
	EngagementRules string
	// SessionSummary is the compacted summary from a previous session.
	// When present, it is included as a <Previous Context> block.
	SessionSummary string
}

// BuildStructuredSystemPrompt renders an XML-tagged structured system prompt
// from the given configuration. The prompt is assembled in a deterministic
// order: identity, role, instructions, engagement model, global hints, and
// optional previous context.
func BuildStructuredSystemPrompt(cfg PromptBuilderConfig) string {
	var sb strings.Builder

	// <identity> block — always present when an agent name exists.
	sb.WriteString("<identity>\n")
	sb.WriteString(fmt.Sprintf("    You are, %s.", cfg.AgentName))
	if cfg.Personality != "" {
		sb.WriteString(fmt.Sprintf(" Your personality is: %s", cfg.Personality))
	}
	sb.WriteString("\n</identity>\n")

	// <role> block — use template Description if available, fallback to Personality.
	roleDesc := cfg.Personality
	if cfg.Template != nil && cfg.Template.Description != "" {
		roleDesc = cfg.Template.Description
	}
	if roleDesc != "" {
		sb.WriteString("<role>\n")
		sb.WriteString(fmt.Sprintf("    %s\n", roleDesc))
		sb.WriteString("</role>\n")
	}

	// <Instructions> block — agent SystemPrompt takes precedence over template.
	instructions := cfg.SystemPrompt
	if instructions == "" && cfg.Template != nil {
		instructions = cfg.Template.SystemPrompt
	}
	if instructions != "" {
		sb.WriteString("<Instructions>\n")
		sb.WriteString(fmt.Sprintf("    %s\n", instructions))
		sb.WriteString("</Instructions>\n")
	}

	// <Engagement Model> block — merge template and agent rules, render active ones.
	rules := resolveEngagementRules(cfg)
	if len(rules) > 0 {
		sb.WriteString("<Engagement Model>\n")
		for _, r := range rules {
			sb.WriteString(fmt.Sprintf("    - %s", r.Trigger))
			if len(r.Chain) > 0 {
				steps := make([]string, len(r.Chain))
				for i, step := range r.Chain {
					steps[i] = fmt.Sprintf("%s (%s)", step.Role, step.Action)
				}
				sb.WriteString(fmt.Sprintf(" → %s", strings.Join(steps, " → ")))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("</Engagement Model>\n")
	}

	// <Global Hints> block — always present to provide baseline behavioural guidance.
	sb.WriteString("<Global Hints>\n")
	sb.WriteString("    - Store and search/retrieve memories whenever possible.\n")
	sb.WriteString("    - Use your tooling to understand the environment, and alter it as discussed with the user or other agents.\n")
	sb.WriteString("    - Don't construct arbitrary commands if you already have a tool to do the job.\n")
	sb.WriteString("    - Asking is better than rewriting.\n")
	sb.WriteString("    - ALWAYS create a task (add_task) for any actionable item you encounter or are asked to do, including follow-ups. If a task already exists, update its status. Tasks are the single source of truth for tracking work.\n")
	sb.WriteString("</Global Hints>\n")

	// <Previous Context> block — only when a session summary exists.
	if cfg.SessionSummary != "" {
		sb.WriteString("<Previous Context>\n")
		sb.WriteString(cfg.SessionSummary)
		sb.WriteString("\n</Previous Context>\n")
	}

	return sb.String()
}

// resolveEngagementRules parses the agent's engagement rules JSON and merges
// them with template rules using role.MergeEngagementRules.
//
// Behaviour:
//   - Empty string → use template rules as-is.
//   - Invalid JSON → fall back to template rules (defensive).
//   - Explicit "[]" (empty array) → suppress all engagement rules.
//   - Non-empty array → merge agent rules on top of template rules.
func resolveEngagementRules(cfg PromptBuilderConfig) []role.EngagementRule {
	var templateRules []role.EngagementRule
	if cfg.Template != nil {
		templateRules = cfg.Template.EngagementRules
	}

	// Empty string → use template rules as-is.
	if cfg.EngagementRules == "" {
		return templateRules
	}

	// Parse agent rules from JSON.
	var agentRules []role.EngagementRule
	if err := json.Unmarshal([]byte(cfg.EngagementRules), &agentRules); err != nil {
		// Invalid JSON → fall back to template rules.
		return templateRules
	}

	// Explicit empty array "[]" → no engagement rules at all.
	if len(agentRules) == 0 {
		return nil
	}

	// Merge agent rules on top of template rules.
	return role.MergeEngagementRules(templateRules, agentRules)
}
