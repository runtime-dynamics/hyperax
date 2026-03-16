package nervous

import (
	"strings"

	"github.com/hyperax/hyperax/pkg/types"
)

// MatchEventType checks whether an event type matches a glob pattern.
// Supported patterns:
//   - "*"           matches everything
//   - "pipeline.*"  matches any event type starting with "pipeline."
//   - "*.completed" matches any event type ending with ".completed"
//   - "cron.fire"   exact match
//
// The matching is case-sensitive and uses '.' as the namespace separator.
func MatchEventType(pattern string, eventType types.EventType) bool {
	p := pattern
	et := string(eventType)

	// Wildcard matches everything.
	if p == "*" {
		return true
	}

	// No wildcard at all: exact match.
	if !strings.Contains(p, "*") {
		return p == et
	}

	// Single trailing wildcard: "prefix.*" matches "prefix.anything"
	if strings.HasSuffix(p, ".*") && strings.Count(p, "*") == 1 {
		prefix := strings.TrimSuffix(p, "*")
		return strings.HasPrefix(et, prefix)
	}

	// Single leading wildcard: "*.suffix" matches "anything.suffix"
	if strings.HasPrefix(p, "*.") && strings.Count(p, "*") == 1 {
		suffix := strings.TrimPrefix(p, "*")
		return strings.HasSuffix(et, suffix)
	}

	// General glob: split on '*' and match segments in order.
	return matchGlob(p, et)
}

// matchGlob performs a simple glob match where '*' matches any substring.
// It handles patterns like "a*b", "*mid*", etc.
func matchGlob(pattern, text string) bool {
	// Edge case: pattern is just "*" (handled above, but defensive).
	if pattern == "*" {
		return true
	}

	parts := strings.Split(pattern, "*")

	// First part must match the prefix of the text.
	if !strings.HasPrefix(text, parts[0]) {
		return false
	}
	text = text[len(parts[0]):]

	// Middle parts must appear in order.
	for i := 1; i < len(parts)-1; i++ {
		idx := strings.Index(text, parts[i])
		if idx < 0 {
			return false
		}
		text = text[idx+len(parts[i]):]
	}

	// Last part must match the suffix.
	return strings.HasSuffix(text, parts[len(parts)-1])
}

// MakeFilterFunc creates an EventBus filter function from a glob pattern.
// This is used by subscribe_events to create runtime subscriptions.
func MakeFilterFunc(pattern string) func(types.NervousEvent) bool {
	return func(e types.NervousEvent) bool {
		return MatchEventType(pattern, e.Type)
	}
}
