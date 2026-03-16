package lifecycle

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/hyperax/hyperax/internal/commhub"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/refactor"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// RehydrationResult summarises what happened during agent rehydration.
type RehydrationResult struct {
	AgentID            string `json:"agent_id"`
	CheckpointRestored bool   `json:"checkpoint_restored"`
	RefactorRolledBack bool   `json:"refactor_rolled_back"`
	MessagesReplayed   int    `json:"messages_replayed"`
	FellBackToOnboard  bool   `json:"fell_back_to_onboard"`
}

// Rehydrator orchestrates the full agent rehydration flow:
//
//  1. Load the latest checkpoint for the agent.
//  2. If an active refactor transaction exists, roll it back.
//  3. Replay unprocessed CommHub messages since the checkpoint.
//  4. Transition the agent to active.
//
// If no checkpoint is found, the rehydrator falls back to a full onboarding
// flow by transitioning the agent to the onboarding state.
type Rehydrator struct {
	lifecycleRepo repo.LifecycleRepo
	checkpointRepo repo.CheckpointRepo
	txManager     *refactor.TransactionManager
	hub           *commhub.CommHub
	bus           *nervous.EventBus
	logger        *slog.Logger
}

// NewRehydrator creates a Rehydrator with all required dependencies.
func NewRehydrator(
	lifecycleRepo repo.LifecycleRepo,
	checkpointRepo repo.CheckpointRepo,
	txManager *refactor.TransactionManager,
	hub *commhub.CommHub,
	bus *nervous.EventBus,
	logger *slog.Logger,
) *Rehydrator {
	return &Rehydrator{
		lifecycleRepo:  lifecycleRepo,
		checkpointRepo: checkpointRepo,
		txManager:      txManager,
		hub:            hub,
		bus:            bus,
		logger:         logger,
	}
}

// Rehydrate executes the full rehydration sequence for the given agent.
// The agent must be in the rehydrating state before calling this method.
// On success, the agent is transitioned to active. On failure, it is
// transitioned to error.
func (r *Rehydrator) Rehydrate(ctx context.Context, agentID string) (*RehydrationResult, error) {
	result := &RehydrationResult{AgentID: agentID}

	// Verify the agent is in the rehydrating state.
	currentState, err := r.lifecycleRepo.GetState(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("lifecycle.Rehydrator.Rehydrate: %w", err)
	}
	if State(currentState) != StateRehydrating {
		return nil, fmt.Errorf("agent %q is in state %q, expected %q", agentID, currentState, StateRehydrating)
	}

	// Step 1: Load latest checkpoint.
	cp, err := r.checkpointRepo.GetLatest(ctx, agentID)
	if err != nil {
		// No checkpoint found — fall back to onboarding.
		r.logger.Info("no checkpoint found, falling back to onboarding",
			"agent_id", agentID,
		)
		result.FellBackToOnboard = true
		if transErr := r.transitionAgent(ctx, agentID, StateRehydrating, StateOnboarding, "no checkpoint, falling back to onboarding"); transErr != nil {
			return result, fmt.Errorf("lifecycle.Rehydrator.Rehydrate: %w", transErr)
		}
		return result, nil
	}

	result.CheckpointRestored = true
	r.logger.Info("checkpoint loaded",
		"agent_id", agentID,
		"checkpoint_id", cp.ID,
		"task_id", cp.TaskID,
		"checkpointed_at", cp.CheckpointedAt,
	)

	// Step 2: Rollback any active refactor transaction.
	if cp.RefactorTxID != "" && r.txManager != nil {
		if _, getErr := r.txManager.Get(cp.RefactorTxID); getErr == nil {
			if rbErr := r.txManager.Rollback(cp.RefactorTxID); rbErr != nil {
				r.logger.Warn("refactor tx rollback failed during rehydration",
					"agent_id", agentID,
					"tx_id", cp.RefactorTxID,
					"error", rbErr,
				)
			} else {
				result.RefactorRolledBack = true
				r.logger.Info("refactor tx rolled back during rehydration",
					"agent_id", agentID,
					"tx_id", cp.RefactorTxID,
				)
			}
		}
	}

	// Step 3: Replay unprocessed CommHub messages.
	if r.hub != nil {
		msgs := r.hub.Receive(agentID, 0) // consume all pending
		result.MessagesReplayed = len(msgs)
		if len(msgs) > 0 {
			r.logger.Info("replayed pending CommHub messages",
				"agent_id", agentID,
				"count", len(msgs),
			)
		}
	}

	// Step 4: Transition to active.
	// The rehydrating→active transition must be valid per the FSM.
	if err := r.transitionAgent(ctx, agentID, StateRehydrating, StateActive, "rehydration complete"); err != nil {
		// If we cannot go active, fall to error.
		if transErr := r.transitionAgent(ctx, agentID, StateRehydrating, StateError, "rehydration failed: "+err.Error()); transErr != nil {
			r.logger.Error("CRITICAL: failed to transition agent to error state",
				"agent_id", agentID, "transition_error", transErr, "original_error", err)
		}
		return result, fmt.Errorf("lifecycle.Rehydrator.Rehydrate: %w", err)
	}

	// Publish rehydration completion event.
	r.bus.Publish(nervous.NewEvent(
		types.EventLifecycleTransition,
		"rehydrator",
		agentID,
		map[string]any{
			"agent_id":            agentID,
			"checkpoint_restored": result.CheckpointRestored,
			"refactor_rolled_back": result.RefactorRolledBack,
			"messages_replayed":   result.MessagesReplayed,
		},
	))

	r.logger.Info("agent rehydration complete",
		"agent_id", agentID,
		"checkpoint_restored", result.CheckpointRestored,
		"messages_replayed", result.MessagesReplayed,
	)

	return result, nil
}

func (r *Rehydrator) transitionAgent(ctx context.Context, agentID string, from, to State, reason string) error {
	if err := ValidateTransition(from, to); err != nil {
		return fmt.Errorf("lifecycle.Rehydrator.transitionAgent: %w", err)
	}

	transition := &repo.LifecycleTransition{
		AgentID:   agentID,
		FromState: string(from),
		ToState:   string(to),
		Reason:    reason,
	}
	if err := r.lifecycleRepo.LogTransition(ctx, transition); err != nil {
		return fmt.Errorf("lifecycle.Rehydrator.transitionAgent: %w", err)
	}
	return nil
}
