package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// SessionTracker manages the lifecycle of agent sessions and records tool call
// metrics. It maintains an in-memory cache of active sessions for fast lookup
// and publishes events to the Nervous System EventBus on session and tool call
// state changes.
type SessionTracker struct {
	repo   repo.TelemetryRepo
	bus    *nervous.EventBus
	logger *slog.Logger
	mu     sync.RWMutex
	active map[string]*types.Session // sessionID -> session
}

// NewSessionTracker creates a SessionTracker backed by the given repository,
// event bus, and logger. All arguments must be non-nil.
func NewSessionTracker(repo repo.TelemetryRepo, bus *nervous.EventBus, logger *slog.Logger) *SessionTracker {
	return &SessionTracker{
		repo:   repo,
		bus:    bus,
		logger: logger,
		active: make(map[string]*types.Session),
	}
}

// StartSession begins a new session for the given agent. The metadata string
// should be valid JSON (defaults to "{}" if empty). Returns the session ID.
// The metadata JSON may contain provider_id and model keys, which are extracted
// and stored as top-level columns for efficient querying.
func (t *SessionTracker) StartSession(ctx context.Context, agentID string, metadata string) (string, error) {
	if agentID == "" {
		return "", fmt.Errorf("agent_id is required")
	}
	if metadata == "" {
		metadata = "{}"
	}

	// Extract provider_id and model from metadata for top-level storage.
	var providerID, model string
	var metaMap map[string]interface{}
	if err := json.Unmarshal([]byte(metadata), &metaMap); err == nil {
		if v, ok := metaMap["provider_id"].(string); ok {
			providerID = v
		}
		if v, ok := metaMap["model"].(string); ok {
			model = v
		}
	}

	session := &types.Session{
		AgentID:    agentID,
		ProviderID: providerID,
		Model:      model,
		StartedAt:  time.Now(),
		Status:     "active",
		Metadata:   metadata,
	}

	id, err := t.repo.CreateSession(ctx, session)
	if err != nil {
		return "", fmt.Errorf("telemetry.SessionTracker.StartSession: %w", err)
	}

	session.ID = id

	t.mu.Lock()
	t.active[id] = session
	t.mu.Unlock()

	t.logger.Info("session started", "session_id", id, "agent_id", agentID)

	// Publish session start event.
	if t.bus != nil {
		payload, err := json.Marshal(map[string]string{
			"session_id": id,
			"agent_id":   agentID,
		})
		if err != nil {
			t.logger.Error("StartSession: failed to marshal event payload", "session_id", id, "error", err)
		} else {
			t.bus.Publish(types.NervousEvent{
				Type:      types.EventTelemetrySessionStart,
				Source:    "telemetry.session_tracker",
				Scope:     agentID,
				Payload:   payload,
				Timestamp: time.Now(),
			})
		}
	}

	return id, nil
}

// EndSession marks a session as completed and removes it from the active cache.
func (t *SessionTracker) EndSession(ctx context.Context, sessionID string) error {
	if err := t.repo.EndSession(ctx, sessionID); err != nil {
		return fmt.Errorf("telemetry.SessionTracker.EndSession: %w", err)
	}

	t.mu.Lock()
	session, existed := t.active[sessionID]
	delete(t.active, sessionID)
	t.mu.Unlock()

	agentID := ""
	if existed && session != nil {
		agentID = session.AgentID
	}

	t.logger.Info("session ended", "session_id", sessionID, "agent_id", agentID)

	// Publish session end event.
	if t.bus != nil {
		payload, err := json.Marshal(map[string]string{
			"session_id": sessionID,
			"agent_id":   agentID,
		})
		if err != nil {
			t.logger.Error("EndSession: failed to marshal event payload", "session_id", sessionID, "error", err)
		} else {
			t.bus.Publish(types.NervousEvent{
				Type:      types.EventTelemetrySessionEnd,
				Source:    "telemetry.session_tracker",
				Scope:     agentID,
				Payload:   payload,
				Timestamp: time.Now(),
			})
		}
	}

	return nil
}

// RecordToolCall persists a tool call metric and updates the session's running
// totals. If the metric has no Cost set, it will be estimated using EstimateCost.
func (t *SessionTracker) RecordToolCall(ctx context.Context, sessionID string, metric *types.ToolCallMetric) error {
	metric.SessionID = sessionID

	// Auto-estimate cost if not provided.
	if metric.Cost == 0 {
		metric.Cost = EstimateCost(metric.ToolName, metric.InputSize, metric.OutputSize)
	}

	if err := t.repo.RecordToolCall(ctx, metric); err != nil {
		return fmt.Errorf("telemetry.SessionTracker.RecordToolCall: %w", err)
	}

	// Update in-memory session stats.
	t.mu.Lock()
	session, exists := t.active[sessionID]
	if exists {
		session.ToolCalls++
		session.TotalCost += metric.Cost
		// Persist to DB periodically — we do it on every call for correctness.
		if err := t.repo.UpdateSessionStats(ctx, sessionID, session.ToolCalls, session.TotalCost,
			session.PromptTokens, session.CompletionTokens, session.TotalTokens); err != nil {
			t.logger.Error("failed to persist session telemetry", "session", sessionID, "error", err)
		}
	}
	t.mu.Unlock()

	// Publish tool call event.
	if t.bus != nil {
		payload, err := json.Marshal(map[string]interface{}{
			"session_id":  sessionID,
			"tool_name":   metric.ToolName,
			"duration_ms": metric.Duration.Milliseconds(),
			"success":     metric.Success,
			"cost":        metric.Cost,
		})
		if err != nil {
			t.logger.Error("RecordToolCall: failed to marshal event payload", "session_id", sessionID, "error", err)
		} else {
			t.bus.Publish(types.NervousEvent{
				Type:      types.EventTelemetryToolCall,
				Source:    "telemetry.session_tracker",
				Payload:   payload,
				Timestamp: time.Now(),
			})
		}
	}

	return nil
}

// RecordTokenUsage accumulates prompt, completion, and total token counts for
// an active session. The counts are added to the session's running totals and
// persisted to the database. This should be called after each LLM completion
// when usage information is available.
func (t *SessionTracker) RecordTokenUsage(ctx context.Context, sessionID string, promptTokens, completionTokens, totalTokens int) error {
	t.mu.Lock()
	session, exists := t.active[sessionID]
	if !exists {
		t.mu.Unlock()
		return fmt.Errorf("session %q not found in active sessions", sessionID)
	}

	session.PromptTokens += promptTokens
	session.CompletionTokens += completionTokens
	session.TotalTokens += totalTokens

	// Persist the updated stats to the database.
	if err := t.repo.UpdateSessionStats(ctx, sessionID, session.ToolCalls, session.TotalCost,
		session.PromptTokens, session.CompletionTokens, session.TotalTokens); err != nil {
		t.mu.Unlock()
		return fmt.Errorf("telemetry.SessionTracker.RecordTokenUsage: %w", err)
	}
	t.mu.Unlock()

	t.logger.Debug("token usage recorded",
		"session_id", sessionID,
		"prompt_tokens", promptTokens,
		"completion_tokens", completionTokens,
		"total_tokens", totalTokens,
	)

	return nil
}

// GetActiveSession returns the in-memory session for the given agent, or nil
// if no active session exists. This is a fast, lock-guarded lookup that does
// not hit the database.
func (t *SessionTracker) GetActiveSession(agentID string) *types.Session {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, s := range t.active {
		if s.AgentID == agentID {
			return s
		}
	}
	return nil
}

// ActiveSessionCount returns the number of currently active sessions.
func (t *SessionTracker) ActiveSessionCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.active)
}
