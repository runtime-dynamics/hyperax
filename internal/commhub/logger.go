package commhub

import (
	"context"
	"log/slog"

	"fmt"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// CommLogger is middleware that persists all CommHub messages to the comm_log
// table. It wraps a CommHubRepo and provides a simple Log method that handlers
// and the hierarchy manager call after each message send, receive, or bounce.
type CommLogger struct {
	repo   repo.CommHubRepo
	logger *slog.Logger
}

// NewCommLogger creates a CommLogger backed by the given repository.
// If repo is nil, Log calls are silently skipped (graceful degradation).
func NewCommLogger(repo repo.CommHubRepo, logger *slog.Logger) *CommLogger {
	return &CommLogger{
		repo:   repo,
		logger: logger,
	}
}

// Log persists a communication event to the comm_log table.
// direction should be one of: "sent", "received", "bounced".
// Returns an error if the persistence fails; callers may choose to log
// the error rather than propagate it to avoid blocking message delivery.
func (cl *CommLogger) Log(ctx context.Context, env *types.MessageEnvelope, direction string) error {
	if cl.repo == nil {
		return nil
	}

	entry := &types.CommLogEntry{
		FromAgent:   env.From,
		ToAgent:     env.To,
		ContentType: env.ContentType,
		Content:     env.Content,
		Trust:       env.Trust.String(),
		Direction:   direction,
	}

	if err := cl.repo.LogMessage(ctx, entry); err != nil {
		cl.logger.Warn("failed to log communication",
			"from", env.From,
			"to", env.To,
			"direction", direction,
			"error", err,
		)
		return fmt.Errorf("commhub.CommLogger.Log: %w", err)
	}

	cl.logger.Debug("communication logged",
		"from", env.From,
		"to", env.To,
		"direction", direction,
	)

	return nil
}

// GetLog retrieves communication log entries for an agent.
// Returns entries where the agent is either sender or receiver.
func (cl *CommLogger) GetLog(ctx context.Context, agentID string, limit int) ([]*types.CommLogEntry, error) {
	if cl.repo == nil {
		return nil, nil
	}
	entries, err := cl.repo.GetCommLog(ctx, agentID, limit)
	if err != nil {
		return nil, fmt.Errorf("commhub.CommLogger.GetLog: %w", err)
	}
	return entries, nil
}

// GetLogBetween retrieves communication log entries between two agents.
func (cl *CommLogger) GetLogBetween(ctx context.Context, from, to string, limit int) ([]*types.CommLogEntry, error) {
	if cl.repo == nil {
		return nil, nil
	}
	entries, err := cl.repo.GetCommLogBetween(ctx, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("commhub.CommLogger.GetLogBetween: %w", err)
	}
	return entries, nil
}

// LogWithSession persists a communication event to the comm_log table with a session_id.
// direction should be one of: "sent", "received", "bounced".
func (cl *CommLogger) LogWithSession(ctx context.Context, env *types.MessageEnvelope, direction, sessionID string) error {
	if cl.repo == nil {
		return nil
	}

	entry := &types.CommLogEntry{
		FromAgent:   env.From,
		ToAgent:     env.To,
		ContentType: env.ContentType,
		Content:     env.Content,
		Trust:       env.Trust.String(),
		Direction:   direction,
		SessionID:   sessionID,
	}

	if err := cl.repo.LogMessageWithSession(ctx, entry); err != nil {
		cl.logger.Warn("failed to log communication with session",
			"from", env.From,
			"to", env.To,
			"direction", direction,
			"session_id", sessionID,
			"error", err,
		)
		return fmt.Errorf("commhub.CommLogger.LogWithSession: %w", err)
	}

	cl.logger.Debug("communication logged with session",
		"from", env.From,
		"to", env.To,
		"direction", direction,
		"session_id", sessionID,
	)

	return nil
}

func (cl *CommLogger) GetLogBySession(ctx context.Context, sessionID string, limit int) ([]*types.CommLogEntry, error) {
	if cl.repo == nil {
		return nil, nil
	}
	entries, err := cl.repo.GetCommLogBySession(ctx, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("commhub.CommLogger.GetLogBySession: %w", err)
	}
	return entries, nil
}
