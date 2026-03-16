package types

import "time"

// WorkQueueItem represents a durable work item in an agent's message queue.
// Items are enqueued when an agent receives a message (via send_message MCP tool
// or REST chat/send endpoint) and dequeued by the Agent Scheduler for processing.
type WorkQueueItem struct {
	ID          string     `json:"id"`
	AgentName   string     `json:"agent_name"`
	FromAgent   string     `json:"from_agent"`
	Content     string     `json:"content"`
	ContentType string     `json:"content_type"`
	Trust       string     `json:"trust"`
	SessionID   string     `json:"session_id,omitempty"`
	Priority    int        `json:"priority"` // 0=normal, 1=high, 2=urgent
	CreatedAt   time.Time  `json:"created_at"`
	ConsumedAt  *time.Time `json:"consumed_at,omitempty"`
}
