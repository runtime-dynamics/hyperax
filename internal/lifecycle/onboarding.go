package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/hyperax/hyperax/internal/commhub"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// OnboardingResult summarises the outcome of the 4-step onboarding flow.
type OnboardingResult struct {
	AgentID      string   `json:"agent_id"`
	PersonaID    string   `json:"persona_id"`
	InboxCreated bool     `json:"inbox_created"`
	Relationships int     `json:"relationships"`
	Permissions   int     `json:"permissions"`
	MemoriesLoaded int   `json:"memories_loaded"`
	TasksAssigned  int   `json:"tasks_assigned"`
}

// OnboardingDeps bundles the repositories and services needed for
// the 4-step agent onboarding flow.
type OnboardingDeps struct {
	LifecycleRepo repo.LifecycleRepo
	AgentRepo     repo.AgentRepo
	CommHubRepo   repo.CommHubRepo
	MemoryRepo    repo.MemoryRepo
	ProjectRepo   repo.ProjectRepo
	Hub           *commhub.CommHub
	Bus           *nervous.EventBus
	Logger        *slog.Logger
}

// Onboarder implements the 4-step agent onboarding lifecycle:
//
//  1. Identity Definition — load persona, create inbox channel
//  2. Relationship Mapping — write hierarchy to agent_relationships, seed permissions
//  3. Context Hydration — recall global/project/persona memories, package as TrustInternal envelope
//  4. Task Assignment — query assigned tasks, include in hydration envelope
//
// Each step publishes progress events on the Nervous System. If any step fails,
// the agent is transitioned to error state.
type Onboarder struct {
	deps OnboardingDeps
}

// NewOnboarder creates an Onboarder with the given dependencies.
func NewOnboarder(deps OnboardingDeps) *Onboarder {
	return &Onboarder{deps: deps}
}

// Onboard executes the full 4-step onboarding flow for the given agent.
// The agent must be in the pending state. It is transitioned to onboarding
// at the start, and to active on success (or error on failure).
//
// Parameters:
//   - agentID: the unique identifier for the agent being onboarded
//   - personaID: the persona to load for identity definition
//   - parentAgentID: the supervisor agent (empty if top-level)
//   - workspaceID: the workspace context for memory recall and task queries
func (o *Onboarder) Onboard(ctx context.Context, agentID, personaID, parentAgentID, workspaceID string) (*OnboardingResult, error) {
	result := &OnboardingResult{
		AgentID:   agentID,
		PersonaID: personaID,
	}

	// Transition pending → onboarding.
	if err := o.transition(ctx, agentID, StatePending, StateOnboarding, "onboarding started"); err != nil {
		return nil, fmt.Errorf("lifecycle.Onboarder.Onboard: %w", err)
	}

	o.publishProgress(agentID, types.EventOnboardingStarted, map[string]any{
		"agent_id":   agentID,
		"persona_id": personaID,
	})

	// Step 1: Identity Definition
	agent, err := o.stepIdentity(ctx, agentID, personaID)
	if err != nil {
		if transErr := o.transition(ctx, agentID, StateOnboarding, StateError, "identity definition failed: "+err.Error()); transErr != nil {
			o.deps.Logger.Error("CRITICAL: failed to transition agent to error state",
				"agent_id", agentID, "step", "identity", "transition_error", transErr, "original_error", err)
		}
		return result, fmt.Errorf("lifecycle.Onboarder.Onboard: %w", err)
	}
	result.InboxCreated = true
	o.publishProgress(agentID, types.EventOnboardingIdentityDone, map[string]any{
		"agent_id": agentID,
		"agent":    agent.Name,
	})
	o.routeStepMessage(ctx, agentID, "identity_done", map[string]any{
		"step":   "identity",
		"agent":  agent.Name,
		"status": "complete",
	})

	// Step 2: Relationship Mapping
	rels, perms, err := o.stepRelationships(ctx, agentID, parentAgentID)
	if err != nil {
		if transErr := o.transition(ctx, agentID, StateOnboarding, StateError, "relationship mapping failed: "+err.Error()); transErr != nil {
			o.deps.Logger.Error("CRITICAL: failed to transition agent to error state",
				"agent_id", agentID, "step", "relationships", "transition_error", transErr, "original_error", err)
		}
		return result, fmt.Errorf("lifecycle.Onboarder.Onboard: %w", err)
	}
	result.Relationships = rels
	result.Permissions = perms
	o.publishProgress(agentID, types.EventOnboardingRelationshipDone, map[string]any{
		"agent_id":      agentID,
		"relationships": rels,
		"permissions":   perms,
	})
	o.routeStepMessage(ctx, agentID, "relationships_done", map[string]any{
		"step":          "relationships",
		"relationships": rels,
		"permissions":   perms,
		"status":        "complete",
	})

	// Step 3: Context Hydration
	memoriesLoaded, err := o.stepContextHydration(ctx, agentID, personaID, workspaceID)
	if err != nil {
		// Non-fatal: log and continue. Agent can operate without memories.
		o.deps.Logger.Warn("context hydration partially failed",
			"agent_id", agentID,
			"error", err,
		)
	}
	result.MemoriesLoaded = memoriesLoaded
	o.publishProgress(agentID, types.EventOnboardingContextDone, map[string]any{
		"agent_id":        agentID,
		"memories_loaded": memoriesLoaded,
	})
	o.routeStepMessage(ctx, agentID, "context_done", map[string]any{
		"step":            "context_hydration",
		"memories_loaded": memoriesLoaded,
		"status":          "complete",
	})

	// Step 4: Task Assignment
	tasksAssigned, err := o.stepTaskAssignment(ctx, agentID, personaID)
	if err != nil {
		// Non-fatal: log and continue. Agent can request tasks later.
		o.deps.Logger.Warn("task assignment partially failed",
			"agent_id", agentID,
			"error", err,
		)
	}
	result.TasksAssigned = tasksAssigned
	o.publishProgress(agentID, types.EventOnboardingTasksDone, map[string]any{
		"agent_id":       agentID,
		"tasks_assigned": tasksAssigned,
	})
	o.routeStepMessage(ctx, agentID, "tasks_done", map[string]any{
		"step":           "task_assignment",
		"tasks_assigned": tasksAssigned,
		"status":         "complete",
	})

	// Transition onboarding → active.
	if err := o.transition(ctx, agentID, StateOnboarding, StateActive, "onboarding complete"); err != nil {
		return result, fmt.Errorf("lifecycle.Onboarder.Onboard: %w", err)
	}

	o.publishProgress(agentID, types.EventOnboardingCompleted, map[string]any{
		"agent_id":        agentID,
		"agent":           agent.Name,
		"memories_loaded": memoriesLoaded,
		"tasks_assigned":  tasksAssigned,
	})
	o.routeStepMessage(ctx, agentID, "completed", map[string]any{
		"step":            "completed",
		"agent":           agent.Name,
		"memories_loaded": memoriesLoaded,
		"tasks_assigned":  tasksAssigned,
		"status":          "complete",
	})

	o.deps.Logger.Info("agent onboarding complete",
		"agent_id", agentID,
		"agent", agent.Name,
		"relationships", rels,
		"memories", memoriesLoaded,
		"tasks", tasksAssigned,
	)

	return result, nil
}

// stepIdentity loads the agent and ensures the CommHub inbox exists.
func (o *Onboarder) stepIdentity(ctx context.Context, agentID, personaID string) (*repo.Agent, error) {
	agent, err := o.deps.AgentRepo.Get(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("lifecycle.Onboarder.stepIdentity: %w", err)
	}

	// Trigger inbox creation by sending an internal bootstrap message.
	// The CommHub auto-creates inboxes on first delivery.
	if o.deps.Hub != nil {
		bootstrap := &types.MessageEnvelope{
			From:        "system",
			To:          agentID,
			Content:     fmt.Sprintf(`{"type":"identity","agent_id":"%s","agent_name":"%s"}`, agent.ID, agent.Name),
			ContentType: "application/json",
			Trust:       types.TrustInternal,
		}
		if err := o.deps.Hub.Send(ctx, bootstrap); err != nil {
			return nil, fmt.Errorf("lifecycle.Onboarder.stepIdentity: %w", err)
		}
	}

	return agent, nil
}

// stepRelationships writes the hierarchy relationship and seeds communication
// permissions. Returns the count of relationships and permissions created.
func (o *Onboarder) stepRelationships(ctx context.Context, agentID, parentAgentID string) (int, int, error) {
	var rels, perms int

	if o.deps.CommHubRepo == nil {
		return 0, 0, nil
	}

	// Create supervisor relationship if a parent is specified.
	if parentAgentID != "" {
		rel := &types.AgentRelationship{
			ParentAgent:  parentAgentID,
			ChildAgent:   agentID,
			Relationship: "supervisor",
		}
		if err := o.deps.CommHubRepo.SetRelationship(ctx, rel); err != nil {
			return rels, perms, fmt.Errorf("lifecycle.Onboarder.stepRelationships: %w", err)
		}
		rels++

		// Grant bidirectional communication permission with parent.
		perm := &types.CommPermission{
			AgentID:    agentID,
			TargetID:   parentAgentID,
			Permission: "both",
		}
		if err := o.deps.CommHubRepo.GrantPermission(ctx, perm); err != nil {
			return rels, perms, fmt.Errorf("lifecycle.Onboarder.stepRelationships: %w", err)
		}
		perms++

		parentPerm := &types.CommPermission{
			AgentID:    parentAgentID,
			TargetID:   agentID,
			Permission: "both",
		}
		if err := o.deps.CommHubRepo.GrantPermission(ctx, parentPerm); err != nil {
			return rels, perms, fmt.Errorf("lifecycle.Onboarder.stepRelationships: %w", err)
		}
		perms++
	}

	// Grant permission to communicate with the system/postmaster.
	sysPerm := &types.CommPermission{
		AgentID:    agentID,
		TargetID:   "system",
		Permission: "both",
	}
	if err := o.deps.CommHubRepo.GrantPermission(ctx, sysPerm); err != nil {
		o.deps.Logger.Warn("grant system permission failed", "error", err)
	} else {
		perms++
	}

	return rels, perms, nil
}

// stepContextHydration recalls global, workspace, and persona memories and
// delivers them to the agent's inbox as a TrustInternal envelope.
// Returns the total number of memories loaded.
func (o *Onboarder) stepContextHydration(ctx context.Context, agentID, personaID, workspaceID string) (int, error) {
	if o.deps.MemoryRepo == nil {
		return 0, nil
	}

	var allMemories []any
	const recallLimit = 20

	// Recall global memories.
	globalMems, err := o.deps.MemoryRepo.Recall(ctx, "", types.MemoryScopeGlobal, "", "", recallLimit)
	if err == nil {
		for _, m := range globalMems {
			allMemories = append(allMemories, m)
		}
	}

	// Recall workspace/project-scoped memories.
	if workspaceID != "" {
		wsMems, err := o.deps.MemoryRepo.Recall(ctx, "", types.MemoryScopeProject, workspaceID, "", recallLimit)
		if err == nil {
			for _, m := range wsMems {
				allMemories = append(allMemories, m)
			}
		}
	}

	// Recall persona-scoped memories.
	if personaID != "" {
		pMems, err := o.deps.MemoryRepo.Recall(ctx, "", types.MemoryScopePersona, "", personaID, recallLimit)
		if err == nil {
			for _, m := range pMems {
				allMemories = append(allMemories, m)
			}
		}
	}

	if len(allMemories) == 0 {
		return 0, nil
	}

	// Package memories as a TrustInternal envelope and deliver.
	if o.deps.Hub != nil {
		payload, err := json.Marshal(map[string]any{
			"type":     "context_hydration",
			"memories": allMemories,
		})
		if err != nil {
			return len(allMemories), fmt.Errorf("lifecycle.Onboarder.stepContextHydration: marshal payload: %w", err)
		}
		env := &types.MessageEnvelope{
			From:        "system",
			To:          agentID,
			Content:     string(payload),
			ContentType: "application/json",
			Trust:       types.TrustInternal,
		}
		if err := o.deps.Hub.Send(ctx, env); err != nil {
			return len(allMemories), fmt.Errorf("lifecycle.Onboarder.stepContextHydration: %w", err)
		}
	}

	return len(allMemories), nil
}

// stepTaskAssignment queries tasks assigned to the agent's persona and delivers
// them via CommHub. Returns the count of tasks assigned.
func (o *Onboarder) stepTaskAssignment(ctx context.Context, agentID, personaID string) (int, error) {
	if o.deps.ProjectRepo == nil || personaID == "" {
		return 0, nil
	}

	// ListTasksByAgent retrieves all tasks assigned to this agent across
	// projects. Empty status and projectID means "all pending/in_progress tasks".
	tasks, err := o.deps.ProjectRepo.ListTasksByAgent(ctx, personaID, "", "")
	if err != nil {
		return 0, fmt.Errorf("lifecycle.Onboarder.stepTaskAssignment: %w", err)
	}

	if len(tasks) == 0 {
		return 0, nil
	}

	// Deliver task list to agent inbox.
	if o.deps.Hub != nil {
		payload, err := json.Marshal(map[string]any{
			"type":  "task_assignment",
			"tasks": tasks,
		})
		if err != nil {
			return len(tasks), fmt.Errorf("lifecycle.Onboarder.stepTaskAssignment: marshal payload: %w", err)
		}
		env := &types.MessageEnvelope{
			From:        "system",
			To:          agentID,
			Content:     string(payload),
			ContentType: "application/json",
			Trust:       types.TrustInternal,
		}
		if err := o.deps.Hub.Send(ctx, env); err != nil {
			return len(tasks), fmt.Errorf("lifecycle.Onboarder.stepTaskAssignment: %w", err)
		}
	}

	return len(tasks), nil
}

// transition validates and logs a lifecycle transition.
func (o *Onboarder) transition(ctx context.Context, agentID string, from, to State, reason string) error {
	if err := ValidateTransition(from, to); err != nil {
		return fmt.Errorf("lifecycle.Onboarder.transition: %w", err)
	}

	entry := &repo.LifecycleTransition{
		AgentID:   agentID,
		FromState: string(from),
		ToState:   string(to),
		Reason:    reason,
	}
	if err := o.deps.LifecycleRepo.LogTransition(ctx, entry); err != nil {
		return fmt.Errorf("lifecycle.Onboarder.transition: %w", err)
	}
	return nil
}

// publishProgress emits a typed onboarding progress event on the Nervous System.
func (o *Onboarder) publishProgress(agentID string, eventType types.EventType, payload map[string]any) {
	o.deps.Bus.Publish(nervous.NewEvent(
		eventType,
		"onboarder",
		agentID,
		payload,
	))
}

// routeStepMessage delivers an onboarding step progression message through
// CommHub so it passes through the Context Sieve and lands in the agent's
// inbox. This is a best-effort delivery — failures are logged but do not
// abort the onboarding flow.
func (o *Onboarder) routeStepMessage(ctx context.Context, agentID, step string, payload map[string]any) {
	if o.deps.Hub == nil {
		return
	}

	data, err := json.Marshal(map[string]any{
		"type":    "onboarding_progress",
		"step":    step,
		"payload": payload,
	})
	if err != nil {
		return
	}

	env := &types.MessageEnvelope{
		From:        "system:onboarding",
		To:          agentID,
		Content:     string(data),
		ContentType: "application/json",
		Trust:       types.TrustInternal,
	}
	if err := o.deps.Hub.Send(ctx, env); err != nil {
		o.deps.Logger.Warn("onboarding step message delivery failed",
			"agent_id", agentID,
			"step", step,
			"error", err,
		)
	}
}
