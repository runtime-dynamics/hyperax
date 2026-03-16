package types

import (
	"encoding/json"
	"time"
)

// MailPriority defines urgency tiers for AgentMail with different ack deadlines.
type MailPriority string

const (
	MailPriorityUrgent     MailPriority = "urgent"     // 30s ack deadline
	MailPriorityStandard   MailPriority = "standard"   // 5min ack deadline
	MailPriorityBackground MailPriority = "background" // 30min ack deadline
)

// AckDeadlineFor returns the acknowledgment deadline for a mail priority.
func AckDeadlineFor(p MailPriority) time.Duration {
	switch p {
	case MailPriorityUrgent:
		return 30 * time.Second
	case MailPriorityStandard:
		return 5 * time.Minute
	case MailPriorityBackground:
		return 30 * time.Minute
	default:
		return 5 * time.Minute
	}
}

// AgentMail is a cross-instance message envelope.
type AgentMail struct {
	ID           string          `json:"id"`
	From         string          `json:"from"`           // source instance ID
	To           string          `json:"to"`             // destination instance ID
	WorkspaceID  string          `json:"workspace_id"`   // coordination scope
	Priority     MailPriority    `json:"priority"`
	Payload      json.RawMessage `json:"payload"`
	PGPSignature string          `json:"pgp_signature"`
	Encrypted    bool            `json:"encrypted"`
	AckDeadline  time.Duration   `json:"ack_deadline"`
	SchemaID     string          `json:"schema_id"` // payload wire format
	SentAt       time.Time       `json:"sent_at"`
}

// MailAck represents an acknowledgment for an AgentMail message.
type MailAck struct {
	MailID     string    `json:"mail_id"`
	InstanceID string    `json:"instance_id"`
	AckedAt    time.Time `json:"acked_at"`
	Status     string    `json:"status"` // received, processing, completed, failed
}

// PartitionLock is a workspace-scoped lock triggered by partition detection.
type PartitionLock struct {
	WorkspaceID string    `json:"workspace_id"`
	InstanceID  string    `json:"instance_id"`
	Reason      string    `json:"reason"`
	LockedAt    time.Time `json:"locked_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// DeadLetterEntry is a quarantined message that failed processing.
type DeadLetterEntry struct {
	ID          string    `json:"id"`
	MailID      string    `json:"mail_id"`
	Reason      string    `json:"reason"`
	Attempts    int       `json:"attempts"`
	QuarantinedAt time.Time `json:"quarantined_at"`
	OriginalMail  *AgentMail `json:"original_mail"`
}
