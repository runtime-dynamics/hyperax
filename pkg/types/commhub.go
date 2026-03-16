package types

import "time"

// CommHub extension event types for hierarchy, bounce, broadcast, and permission changes.
const (
	EventCommHierarchyChanged EventType = "comm.hierarchy.changed"
	EventCommBounced          EventType = "comm.bounced"
	EventCommBroadcast        EventType = "comm.broadcast"
	EventCommPermChanged      EventType = "comm.permission.changed"
)

// AgentRelationship defines a hierarchical relationship between two agents.
// The parent-child model supports supervisor, peer, and delegate relationships,
// which the HierarchyManager uses for route validation and delegation routing.
type AgentRelationship struct {
	ID           string    `json:"id"`
	ParentAgent  string    `json:"parent_agent"`  // supervisor agent ID
	ChildAgent   string    `json:"child_agent"`   // subordinate agent ID
	Relationship string    `json:"relationship"`  // "supervisor", "peer", "delegate"
	CreatedAt    time.Time `json:"created_at"`
}

// CommLogEntry records a single communication event in the persistent comm log.
// Direction indicates whether the message was sent, received, or bounced.
type CommLogEntry struct {
	ID          string    `json:"id"`
	FromAgent   string    `json:"from_agent"`
	ToAgent     string    `json:"to_agent"`
	ContentType string    `json:"content_type"`
	Content     string    `json:"content"`
	Trust       string    `json:"trust"`
	Direction   string    `json:"direction"` // "sent", "received", "bounced"
	SessionID   string    `json:"session_id"`
	CreatedAt   time.Time `json:"created_at"`
}

// ChatSession represents a bounded conversation between an agent and peer.
type ChatSession struct {
	ID         string     `json:"id"`
	AgentName  string     `json:"agent_name"`
	PeerID     string     `json:"peer_id"`
	StartedAt  time.Time  `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
	Summary    string     `json:"summary"`
}

// CommPermission controls which agents are allowed to communicate with each other.
// TargetID of "*" indicates the agent can communicate with all agents.
type CommPermission struct {
	ID         string    `json:"id"`
	AgentID    string    `json:"agent_id"`
	TargetID   string    `json:"target_id"`   // who they can communicate with ("*" = all)
	Permission string    `json:"permission"`  // "send", "receive", "both"
	CreatedAt  time.Time `json:"created_at"`
}

// OverflowEntry represents a message that was dropped from an agent's inbox
// due to backpressure and persisted to the database for later retrieval.
type OverflowEntry struct {
	ID          string            `json:"id"`
	AgentID     string            `json:"agent_id"`
	From        string            `json:"from"`
	To          string            `json:"to"`
	ContentType string            `json:"content_type"`
	Content     string            `json:"content"`
	Trust       int               `json:"trust"`
	Metadata    map[string]string `json:"metadata"`
	OriginalTS  int64             `json:"original_ts"`
	CreatedAt   time.Time         `json:"created_at"`
	Retrieved   bool              `json:"retrieved"`
}
