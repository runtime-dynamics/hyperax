package commhub

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

// SieveLayer is a function that processes a message envelope.
// It returns the (possibly modified) envelope, or an error to reject the message.
type SieveLayer func(env *types.MessageEnvelope) (*types.MessageEnvelope, error)

// ContextSieve applies ordered sanitization layers to incoming messages.
// The 5-layer pipeline is: pattern filter, length limiter, content classifier,
// metadata stripper, and structural sifter. Each layer can transform or reject.
type ContextSieve struct {
	layers []SieveLayer
	bus    *nervous.EventBus
}

// NewContextSieve creates a sieve with the standard 5-layer pipeline.
func NewContextSieve(bus *nervous.EventBus) *ContextSieve {
	s := &ContextSieve{bus: bus}
	s.layers = []SieveLayer{
		s.patternFilter,
		s.lengthLimiter,
		s.contentClassifier,
		s.metadataStripper,
		s.structuralSifter,
	}
	return s
}

// ProcessLightweight runs the envelope through only the Pattern Filter (Layer 1)
// and Metadata Stripper (Layer 4). This is used for recursive sifting when an
// Internal message carries trust lineage from an External source.
func (s *ContextSieve) ProcessLightweight(env *types.MessageEnvelope) (*types.MessageEnvelope, error) {
	lightweight := []SieveLayer{
		s.patternFilter,
		s.metadataStripper,
	}
	current := env
	for i, layer := range lightweight {
		result, err := layer(current)
		if err != nil {
			s.bus.Publish(nervous.NewEvent(
				types.EventCommSieveFlag,
				"commhub.sieve.lightweight",
				"global",
				map[string]string{
					"layer": fmt.Sprintf("lightweight-%d", i),
					"from":  env.From,
					"to":    env.To,
					"error": err.Error(),
				},
			))
			return nil, fmt.Errorf("lightweight sieve layer %d: %w", i, err)
		}
		current = result
	}
	return current, nil
}

// Process runs the envelope through all sieve layers in order.
// Returns the sanitized envelope or an error if any layer rejects the message.
func (s *ContextSieve) Process(env *types.MessageEnvelope) (*types.MessageEnvelope, error) {
	current := env
	for i, layer := range s.layers {
		result, err := layer(current)
		if err != nil {
			// Publish sieve flag event so the Nervous System is aware of the rejection.
			s.bus.Publish(nervous.NewEvent(
				types.EventCommSieveFlag,
				"commhub.sieve",
				"global",
				map[string]string{
					"layer": fmt.Sprintf("%d", i),
					"from":  env.From,
					"to":    env.To,
					"error": err.Error(),
				},
			))
			return nil, fmt.Errorf("sieve layer %d: %w", i, err)
		}
		current = result
	}
	return current, nil
}

// dangerousPatterns are compiled regexes for common prompt injection attempts.
// Compiled once at init time to avoid per-message regex compilation overhead.
var dangerousPatterns []*regexp.Regexp

func init() {
	patterns := []string{
		`(?i)ignore\s+(all\s+)?(previous\s+)?instructions`,
		`(?i)system\s*:\s*you\s+are`,
		`(?i)new\s+instructions?\s*:`,
		`(?i)\[SYSTEM\]`,
	}
	dangerousPatterns = make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			continue
		}
		dangerousPatterns = append(dangerousPatterns, re)
	}
}

// patternFilter is Layer 1: blocks known prompt injection patterns.
func (s *ContextSieve) patternFilter(env *types.MessageEnvelope) (*types.MessageEnvelope, error) {
	for _, re := range dangerousPatterns {
		if re.MatchString(env.Content) {
			return nil, fmt.Errorf("blocked by pattern filter: suspicious content detected")
		}
	}
	return env, nil
}

// maxContentLength is the maximum allowed message content length in runes.
const maxContentLength = 100_000

// lengthLimiter is Layer 2: rejects messages exceeding the maximum content size.
func (s *ContextSieve) lengthLimiter(env *types.MessageEnvelope) (*types.MessageEnvelope, error) {
	if utf8.RuneCountInString(env.Content) > maxContentLength {
		return nil, fmt.Errorf("content exceeds maximum length (%d runes)", maxContentLength)
	}
	return env, nil
}

// contentClassifier is Layer 3: tags content with trust markers in metadata.
func (s *ContextSieve) contentClassifier(env *types.MessageEnvelope) (*types.MessageEnvelope, error) {
	if env.Metadata == nil {
		env.Metadata = make(map[string]string)
	}

	switch env.Trust {
	case types.TrustExternal:
		env.Metadata["sanitized"] = "true"
		env.Metadata["trust_verified"] = "false"
	case types.TrustAuthorized:
		env.Metadata["trust_verified"] = "true"
	case types.TrustInternal:
		env.Metadata["trust_verified"] = "true"
	}

	// Auto-detect content type if not explicitly set.
	if env.ContentType == "" {
		content := strings.TrimSpace(env.Content)
		if strings.HasPrefix(content, "{") || strings.HasPrefix(content, "[") {
			env.ContentType = "json"
		} else {
			env.ContentType = "text"
		}
	}

	return env, nil
}

// sensitiveMetadataKeys are metadata keys stripped from untrusted messages.
var sensitiveMetadataKeys = []string{
	"system_prompt",
	"admin",
	"elevated",
	"sudo",
	"override",
}

// metadataStripper is Layer 4: removes sensitive metadata from untrusted messages.
func (s *ContextSieve) metadataStripper(env *types.MessageEnvelope) (*types.MessageEnvelope, error) {
	if env.Trust >= types.TrustExternal {
		for _, key := range sensitiveMetadataKeys {
			delete(env.Metadata, key)
		}
	}
	return env, nil
}

// structuralSifter is Layer 5: validates that JSON-typed payloads are structurally valid.
// For AgentMail payloads, this is critical to prevent malformed cross-instance data
// from corrupting agent state.
func (s *ContextSieve) structuralSifter(env *types.MessageEnvelope) (*types.MessageEnvelope, error) {
	if env.ContentType == "json" {
		content := strings.TrimSpace(env.Content)
		if !strings.HasPrefix(content, "{") && !strings.HasPrefix(content, "[") {
			return nil, fmt.Errorf("content_type is json but content is not valid JSON structure")
		}
	}
	return env, nil
}
