package plugin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// ConfigGetter retrieves config values (subset of config.ConfigStore).
type ConfigGetter interface {
	Resolve(ctx context.Context, key, agentID, workspaceID string) (string, error)
}

// ConfigSetter stores config values (subset of config.ConfigStore).
type ConfigSetter interface {
	Set(ctx context.Context, key, value string, scope any, actor string) error
}

// InterjectionCreator creates interjections (subset of interject.Manager).
type InterjectionCreator interface {
	Create(ctx context.Context, params any) (string, error)
}

// challengeTTL is how long a challenge code remains valid.
const challengeTTL = 10 * time.Minute

// PendingChallenge tracks an in-flight approval challenge for a plugin.
type PendingChallenge struct {
	Code       string
	PluginName string
	ChannelID  string
	ExpiresAt  time.Time
}

// ApprovalGate manages plugin approval state using the config system.
// Plugins with ApprovalRequired=true must be explicitly approved before
// their events are processed by the EventBridge.
//
// The challenge-response flow works as follows:
//  1. User requests approval → gate generates an 8-char code
//  2. The code is sent to the user via the plugin's own communication channel
//     (e.g., Discord DM), proving the plugin connection works
//  3. User enters the code in Hyperax UI → gate validates and marks approved
type ApprovalGate struct {
	configGet ConfigGetter
	logger    *slog.Logger

	mu         sync.Mutex
	challenges map[string]*PendingChallenge // keyed by plugin name
}

// NewApprovalGate creates an ApprovalGate backed by the config store.
func NewApprovalGate(configGet ConfigGetter, logger *slog.Logger) *ApprovalGate {
	return &ApprovalGate{
		configGet:  configGet,
		logger:     logger.With("component", "plugin-approval"),
		challenges: make(map[string]*PendingChallenge),
	}
}

// IsApproved checks whether a plugin has been approved in config.
func (ag *ApprovalGate) IsApproved(ctx context.Context, pluginName string) bool {
	if ag.configGet == nil {
		return false
	}
	key := fmt.Sprintf("plugin.%s.approved", pluginName)
	val, err := ag.configGet.Resolve(ctx, key, "", "")
	if err != nil {
		return false
	}
	return val == "true"
}

// GenerateChallenge creates a new 8-character challenge code for a plugin.
// The code must be validated via ValidateChallenge within the TTL.
// Returns the generated code.
func (ag *ApprovalGate) GenerateChallenge(pluginName, channelID string) (string, error) {
	code, err := generateCode(4) // 4 bytes = 8 hex chars
	if err != nil {
		return "", fmt.Errorf("plugin.ApprovalGate.GenerateChallenge: %w", err)
	}

	ag.mu.Lock()
	defer ag.mu.Unlock()

	ag.challenges[pluginName] = &PendingChallenge{
		Code:       code,
		PluginName: pluginName,
		ChannelID:  channelID,
		ExpiresAt:  time.Now().Add(challengeTTL),
	}

	ag.logger.Info("challenge generated for plugin",
		"plugin", pluginName,
		"expires_in", challengeTTL.String(),
	)

	return code, nil
}

// ValidateChallenge checks whether the provided code matches the pending
// challenge for the given plugin. On success, the challenge is consumed.
// Returns true if the code is valid and not expired.
func (ag *ApprovalGate) ValidateChallenge(pluginName, code string) bool {
	ag.mu.Lock()
	defer ag.mu.Unlock()

	challenge, ok := ag.challenges[pluginName]
	if !ok {
		ag.logger.Warn("no pending challenge for plugin", "plugin", pluginName)
		return false
	}

	if time.Now().After(challenge.ExpiresAt) {
		delete(ag.challenges, pluginName)
		ag.logger.Warn("challenge expired for plugin", "plugin", pluginName)
		return false
	}

	if challenge.Code != code {
		ag.logger.Warn("invalid challenge code for plugin",
			"plugin", pluginName)
		return false
	}

	// Consume the challenge.
	delete(ag.challenges, pluginName)
	ag.logger.Info("challenge validated for plugin", "plugin", pluginName)
	return true
}

// HasPendingChallenge returns whether a plugin has an active challenge.
func (ag *ApprovalGate) HasPendingChallenge(pluginName string) bool {
	ag.mu.Lock()
	defer ag.mu.Unlock()
	ch, ok := ag.challenges[pluginName]
	if !ok {
		return false
	}
	return time.Now().Before(ch.ExpiresAt)
}

// generateCode produces a hex-encoded random string of the given byte length.
func generateCode(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("plugin.generateCode: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// ApprovalConfigKey returns the config key used for plugin approval state.
func ApprovalConfigKey(pluginName string) string {
	return fmt.Sprintf("plugin.%s.approved", pluginName)
}
