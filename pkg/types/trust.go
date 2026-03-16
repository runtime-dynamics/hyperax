package types

// TrustLevel defines the trust boundary for message routing.
// Three levels enforce isolation between internal agent traffic,
// authenticated external sources, and fully untrusted input.
type TrustLevel int

const (
	// TrustInternal indicates same-instance, verified agent traffic.
	// Messages are typed Go structs with no string parsing from untrusted sources.
	TrustInternal TrustLevel = iota

	// TrustAuthorized indicates a known external instance or whitelisted user.
	// Messages are signed and verified but still originate outside the process.
	TrustAuthorized

	// TrustExternal indicates an unknown, fully untrusted source.
	// Messages pass through the full Context Sieve before delivery.
	TrustExternal
)

// String returns the human-readable trust level name.
func (t TrustLevel) String() string {
	switch t {
	case TrustInternal:
		return "internal"
	case TrustAuthorized:
		return "authorized"
	case TrustExternal:
		return "external"
	default:
		return "unknown"
	}
}

// ParseTrustLevel converts a string to a TrustLevel.
// Returns TrustExternal for unrecognized values (safe default).
func ParseTrustLevel(s string) TrustLevel {
	switch s {
	case "internal":
		return TrustInternal
	case "authorized":
		return TrustAuthorized
	case "external":
		return TrustExternal
	default:
		return TrustExternal
	}
}

// MessageEnvelope wraps all messages flowing through the CommHub.
// Every communication in Hyperax is wrapped in this envelope, carrying
// trust level, trace ID, and routing information.
type MessageEnvelope struct {
	// ID is the unique identifier for this message.
	ID string `json:"id"`

	// From is the sender agent or source identifier.
	From string `json:"from"`

	// To is the recipient agent identifier.
	To string `json:"to"`

	// Trust is the trust level assigned to this message.
	Trust TrustLevel `json:"trust"`

	// ContentType describes the payload format: "text", "json", "tool_call", "tool_result".
	ContentType string `json:"content_type"`

	// Content is the raw message payload.
	Content string `json:"content"`

	// Metadata holds optional key-value pairs attached by sieve layers or senders.
	Metadata map[string]string `json:"metadata,omitempty"`

	// Timestamp is the Unix millisecond timestamp when the message was created.
	Timestamp int64 `json:"timestamp"`

	// TraceID is the distributed tracing correlation identifier.
	TraceID string `json:"trace_id,omitempty"`
}
