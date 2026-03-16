package repo

import (
	"context"

	"github.com/hyperax/hyperax/pkg/types"
)

// CommHubRepo provides persistence for agent hierarchy, communication logging,
// and permission management. All methods are context-aware for cancellation
// and timeout support.
type CommHubRepo interface {
	// Hierarchy

	// SetRelationship creates or updates an agent relationship.
	// If a relationship with the same ID exists, it is replaced.
	SetRelationship(ctx context.Context, rel *types.AgentRelationship) error

	// GetRelationship retrieves a specific relationship by parent and child agent IDs.
	// Returns an error if no matching relationship is found.
	GetRelationship(ctx context.Context, parentAgent, childAgent string) (*types.AgentRelationship, error)

	// GetChildren returns all relationships where parentAgent is the supervisor.
	GetChildren(ctx context.Context, parentAgent string) ([]*types.AgentRelationship, error)

	// GetParent returns the relationship where childAgent is the subordinate.
	// Returns an error if the child has no parent relationship.
	GetParent(ctx context.Context, childAgent string) (*types.AgentRelationship, error)

	// GetFullHierarchy returns every agent relationship in the system.
	GetFullHierarchy(ctx context.Context) ([]*types.AgentRelationship, error)

	// DeleteRelationship removes a relationship by its ID.
	// Returns an error if the relationship does not exist.
	DeleteRelationship(ctx context.Context, id string) error

	// Comm log

	// LogMessage persists a communication log entry.
	LogMessage(ctx context.Context, entry *types.CommLogEntry) error

	// GetCommLog retrieves log entries for a specific agent (as sender or receiver),
	// ordered by created_at DESC, limited to the given count.
	GetCommLog(ctx context.Context, agentID string, limit int) ([]*types.CommLogEntry, error)

	// GetCommLogBetween retrieves log entries between two specific agents,
	// ordered by created_at DESC, limited to the given count.
	GetCommLogBetween(ctx context.Context, from, to string, limit int) ([]*types.CommLogEntry, error)

	// LogMessageWithSession persists a communication log entry including its session_id.
	LogMessageWithSession(ctx context.Context, entry *types.CommLogEntry) error

	// GetCommLogBySession retrieves log entries for a specific session,
	// ordered by created_at DESC, limited to the given count.
	GetCommLogBySession(ctx context.Context, sessionID string, limit int) ([]*types.CommLogEntry, error)

	// Permissions

	// GrantPermission creates or replaces a communication permission.
	// Uses UPSERT on the (agent_id, target_id) unique constraint.
	GrantPermission(ctx context.Context, perm *types.CommPermission) error

	// RevokePermission removes a communication permission for the given agent-target pair.
	// Returns an error if no matching permission is found.
	RevokePermission(ctx context.Context, agentID, targetID string) error

	// CheckPermission returns true if the agent has permission to communicate
	// with the target. Checks both specific target and wildcard ("*") permissions.
	CheckPermission(ctx context.Context, agentID, targetID string) (bool, error)

	// ListPermissions returns all permissions granted to the given agent.
	ListPermissions(ctx context.Context, agentID string) ([]*types.CommPermission, error)

	// Overflow persistence

	// PersistOverflow stores a message that was dropped due to inbox backpressure.
	PersistOverflow(ctx context.Context, entry *types.OverflowEntry) error

	// DrainOverflow retrieves and marks as retrieved up to limit overflow messages
	// for the given agent, ordered oldest first. Returns nil if none available.
	DrainOverflow(ctx context.Context, agentID string, limit int) ([]*types.OverflowEntry, error)

	// CountOverflow returns the number of unretrieved overflow entries for an agent.
	CountOverflow(ctx context.Context, agentID string) (int, error)

	// PurgeOverflow deletes retrieved overflow entries older than the given cutoff.
	PurgeOverflow(ctx context.Context, olderThan string) (int64, error)

	// RenameAgentRefs cascades an agent name change to all comm_log entries
	// (from_agent, to_agent) and agent_relationships (parent_agent, child_agent).
	RenameAgentRefs(ctx context.Context, oldName, newName string) error
}
