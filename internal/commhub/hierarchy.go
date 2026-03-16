package commhub

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// maxDelegationDepth limits recursive delegation lookups to prevent infinite loops.
const maxDelegationDepth = 10

// HierarchyManager enforces agent hierarchy rules for message routing.
// It validates that senders have permission to message recipients based on
// the agent relationship tree and explicit permissions, and supports
// delegation routing where a message can be forwarded to the correct delegate.
type HierarchyManager struct {
	repo   repo.CommHubRepo
	bus    *nervous.EventBus
	logger *slog.Logger
}

// NewHierarchyManager creates a HierarchyManager wired to the given repo and event bus.
// If repo is nil, all validation calls return nil (permissive fallback).
func NewHierarchyManager(repo repo.CommHubRepo, bus *nervous.EventBus, logger *slog.Logger) *HierarchyManager {
	return &HierarchyManager{
		repo:   repo,
		bus:    bus,
		logger: logger,
	}
}

// ValidateRoute checks whether the sender has permission to message the recipient.
// Permission is granted if any of the following are true:
//   - An explicit CommPermission exists (send or both) for from -> to
//   - An explicit CommPermission with wildcard target ("*") exists for from
//   - A hierarchy relationship exists where from is parent of to, or to is parent of from
//   - from and to are peers under the same parent
//
// Returns nil if routing is permitted, or an error describing why the route is invalid.
// On rejection, publishes a comm.bounced event to the Nervous System.
func (hm *HierarchyManager) ValidateRoute(ctx context.Context, from, to string) error {
	if hm.repo == nil {
		return nil
	}

	// Check explicit permissions first (most specific).
	permitted, err := hm.repo.CheckPermission(ctx, from, to)
	if err != nil {
		hm.logger.Warn("permission check failed, allowing route",
			"from", from, "to", to, "error", err,
		)
		return nil
	}
	if permitted {
		return nil
	}

	// Check hierarchy: parent -> child or child -> parent is always allowed.
	if hm.hasHierarchyLink(ctx, from, to) {
		return nil
	}

	// Check peer relationship: both share the same parent.
	if hm.arePeers(ctx, from, to) {
		return nil
	}

	// Route denied — publish bounce event.
	bounceErr := fmt.Errorf("route denied: %s is not permitted to message %s", from, to)

	hm.bus.Publish(nervous.NewEvent(
		types.EventCommBounced,
		"hierarchy",
		from,
		map[string]string{
			"from":   from,
			"to":     to,
			"reason": bounceErr.Error(),
		},
	))

	hm.logger.Warn("message route denied",
		"from", from,
		"to", to,
	)

	return bounceErr
}

// hasHierarchyLink returns true if from is a parent of to, or to is a parent of from.
func (hm *HierarchyManager) hasHierarchyLink(ctx context.Context, from, to string) bool {
	// Check from is parent of to.
	if _, err := hm.repo.GetRelationship(ctx, from, to); err == nil {
		return true
	}

	// Check to is parent of from.
	if _, err := hm.repo.GetRelationship(ctx, to, from); err == nil {
		return true
	}

	return false
}

// arePeers returns true if both agents share the same parent agent.
func (hm *HierarchyManager) arePeers(ctx context.Context, agentA, agentB string) bool {
	parentA, errA := hm.repo.GetParent(ctx, agentA)
	parentB, errB := hm.repo.GetParent(ctx, agentB)

	if errA != nil || errB != nil {
		return false
	}

	return parentA.ParentAgent == parentB.ParentAgent
}

// GetDelegateFor walks the hierarchy to find a delegate agent responsible for
// the given category. It starts at agentID and checks children for a delegate
// relationship whose Relationship field matches the category. If no direct
// delegate is found, it recursively checks the parent chain.
//
// Returns the delegate agent ID, or an error if no delegate is found after
// exhausting the hierarchy (up to maxDelegationDepth levels).
func (hm *HierarchyManager) GetDelegateFor(ctx context.Context, agentID, category string) (string, error) {
	if hm.repo == nil {
		return "", fmt.Errorf("no repo available for delegation lookup")
	}

	return hm.findDelegate(ctx, agentID, category, 0)
}

// findDelegate recursively searches for a delegate matching the category.
// It first checks children of the current agent, then walks up to the parent.
func (hm *HierarchyManager) findDelegate(ctx context.Context, agentID, category string, depth int) (string, error) {
	if depth >= maxDelegationDepth {
		return "", fmt.Errorf("delegation depth exceeded for category %q starting from %s", category, agentID)
	}

	// Check children for a delegate relationship matching the category.
	children, err := hm.repo.GetChildren(ctx, agentID)
	if err == nil {
		for _, child := range children {
			if child.Relationship == "delegate" && child.ChildAgent == category {
				return child.ChildAgent, nil
			}
			// Also check if the child's relationship type matches the category.
			if child.Relationship == category {
				return child.ChildAgent, nil
			}
		}
	}

	// Walk up to parent and check their children.
	parent, err := hm.repo.GetParent(ctx, agentID)
	if err != nil {
		return "", fmt.Errorf("commhub.HierarchyManager.findDelegate: %w", err)
	}

	return hm.findDelegate(ctx, parent.ParentAgent, category, depth+1)
}

// SetRelationship creates or updates an agent relationship and publishes
// a hierarchy changed event.
func (hm *HierarchyManager) SetRelationship(ctx context.Context, rel *types.AgentRelationship) error {
	if hm.repo == nil {
		return fmt.Errorf("no repo available for hierarchy management")
	}

	if err := hm.repo.SetRelationship(ctx, rel); err != nil {
		return fmt.Errorf("commhub.HierarchyManager.SetRelationship: %w", err)
	}

	hm.bus.Publish(nervous.NewEvent(
		types.EventCommHierarchyChanged,
		"hierarchy",
		rel.ParentAgent,
		map[string]string{
			"parent":       rel.ParentAgent,
			"child":        rel.ChildAgent,
			"relationship": rel.Relationship,
			"action":       "set",
		},
	))

	hm.logger.Info("agent relationship set",
		"parent", rel.ParentAgent,
		"child", rel.ChildAgent,
		"relationship", rel.Relationship,
	)

	return nil
}

// GetFullHierarchy returns all agent relationships in the system.
func (hm *HierarchyManager) GetFullHierarchy(ctx context.Context) ([]*types.AgentRelationship, error) {
	if hm.repo == nil {
		return nil, nil
	}
	rels, err := hm.repo.GetFullHierarchy(ctx)
	if err != nil {
		return nil, fmt.Errorf("commhub.HierarchyManager.GetFullHierarchy: %w", err)
	}
	return rels, nil
}
