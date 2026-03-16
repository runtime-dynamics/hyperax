package types

import "time"

// MemoryScope defines the visibility tier of a memory entry.
// Recall cascades through scopes in order: persona → project → global.
type MemoryScope string

const (
	MemoryScopeGlobal  MemoryScope = "global"  // Cross-project institutional knowledge
	MemoryScopeProject MemoryScope = "project"  // Workspace-specific knowledge
	MemoryScopePersona MemoryScope = "persona"  // Agent-specific learned experience
)

// MemoryType categorises the nature and retention of a memory entry.
type MemoryType string

const (
	MemoryTypeEpisodic   MemoryType = "episodic"   // Specific events/observations; subject to consolidation
	MemoryTypeSemantic   MemoryType = "semantic"    // Distilled knowledge from consolidated episodics
	MemoryTypeProcedural MemoryType = "procedural"  // How-to knowledge; long retention
)

// Memory is the core domain type for the knowledge system.
type Memory struct {
	ID               string         `json:"id"`
	Scope            MemoryScope    `json:"scope"`
	Type             MemoryType     `json:"type"`
	Content          string         `json:"content"`
	WorkspaceID      string         `json:"workspace_id,omitempty"`  // NULL for global scope
	PersonaID        string         `json:"persona_id,omitempty"`   // NULL for global/project scope
	Metadata         map[string]any `json:"metadata,omitempty"`     // source, confidence, tags, anchored
	Embedding        []float32      `json:"embedding,omitempty"`    // 384-dim vector (nil if not embedded)
	CreatedAt        time.Time      `json:"created_at"`
	AccessedAt       time.Time      `json:"accessed_at"`            // Updated on recall
	AccessCount      int            `json:"access_count"`
	ConsolidatedInto string         `json:"consolidated_into,omitempty"` // Points to merged memory ID
	ContestedBy      string         `json:"contested_by,omitempty"`      // ID of conflicting memory
	ContestedAt      time.Time      `json:"contested_at,omitempty"`      // When conflict was detected
}

// IsAnchored returns true if this memory is a protected institutional law.
func (m *Memory) IsAnchored() bool {
	if m.Metadata == nil {
		return false
	}
	anchored, _ := m.Metadata["anchored"].(bool)
	return anchored
}

// Tags returns the tags from metadata, or nil.
func (m *Memory) Tags() []string {
	if m.Metadata == nil {
		return nil
	}
	raw, ok := m.Metadata["tags"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		tags := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				tags = append(tags, s)
			}
		}
		return tags
	}
	return nil
}

// MemoryQuery specifies search parameters for the retrieval engine.
type MemoryQuery struct {
	Query       string        `json:"query"`
	PersonaID   string        `json:"persona_id,omitempty"`
	WorkspaceID string        `json:"workspace_id,omitempty"`
	MaxResults  int           `json:"max_results,omitempty"`
	Timeout     time.Duration `json:"timeout,omitempty"`
}

// MemoryContext wraps a recalled memory with its fused relevance score
// and position in the result set.
type MemoryContext struct {
	Memory Memory  `json:"memory"`
	Score  float64 `json:"score"`  // RRF-fused or BM25 score
	Rank   int     `json:"rank"`   // Position in recall result
	Source string  `json:"source"` // "proactive" or "tool_injection"
}

// MemoryAnnotation is a non-destructive "sticky note" attached to a memory.
type MemoryAnnotation struct {
	ID             string    `json:"id"`
	MemoryID       string    `json:"memory_id"`
	Annotation     string    `json:"annotation"`
	AnnotationType string    `json:"annotation_type"` // warning, correction, context, deprecation
	CreatedBy      string    `json:"created_by"`
	CreatedAt      time.Time `json:"created_at"`
}
