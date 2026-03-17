package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hyperax/hyperax/internal/agentmail"
	"github.com/hyperax/hyperax/internal/secrets"
	"github.com/hyperax/hyperax/pkg/types"
)

// Compile-time interface assertion.
var _ agentmail.MessengerAdapter = (*SlackAdapter)(nil)

// SlackConfig holds configuration for the SlackAdapter.
type SlackConfig struct {
	// BotTokenRef is a "secret:key:scope" reference for the Slack Bot User OAuth Token (xoxb-...).
	BotTokenRef string

	// DefaultChannelID is the Slack channel ID for outbound messages when no routing is specified.
	DefaultChannelID string

	// Timeout is the HTTP client timeout. Defaults to 10s.
	Timeout time.Duration
}

// SlackAdapter implements MessengerAdapter for Slack Web API.
// Outbound: Uses chat.postMessage to send messages.
// Inbound: Uses conversations.history to poll for new messages.
type SlackAdapter struct {
	config    SlackConfig
	secrets   *secrets.Registry
	logger    *slog.Logger
	client    *http.Client
	healthy   atomic.Bool
	started   atomic.Bool
	lastTS    string // timestamp of last received message for polling
	mu        sync.Mutex
	stopOnce  sync.Once
}

// slackAPIBase is the Slack Web API base URL. It is a var (not const)
// to allow test overrides via setSlackAPIBase.
var slackAPIBase = "https://slack.com/api"

// setSlackAPIBase overrides the Slack API base URL. Used in tests only.
func setSlackAPIBase(base string) { slackAPIBase = base }

// NewSlackAdapter creates a SlackAdapter with the given configuration.
func NewSlackAdapter(cfg SlackConfig, reg *secrets.Registry, logger *slog.Logger) *SlackAdapter {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	return &SlackAdapter{
		config:  cfg,
		secrets: reg,
		logger:  logger.With("adapter", "slack"),
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// Name returns "slack".
func (a *SlackAdapter) Name() string {
	return "slack"
}

// Start verifies the Slack bot token is valid by calling auth.test, then marks healthy.
func (a *SlackAdapter) Start(ctx context.Context) error {
	if a.started.Swap(true) {
		return fmt.Errorf("slack adapter already started")
	}

	// Validate the token with auth.test.
	if err := a.authTest(ctx); err != nil {
		a.started.Store(false)
		return fmt.Errorf("adapters.SlackAdapter.Start: %w", err)
	}

	a.healthy.Store(true)
	a.logger.Info("slack adapter started", "channel", a.config.DefaultChannelID)
	return nil
}

// Stop marks the adapter as unhealthy.
func (a *SlackAdapter) Stop() error {
	a.stopOnce.Do(func() {
		a.healthy.Store(false)
		a.started.Store(false)
		a.logger.Info("slack adapter stopped")
	})
	return nil
}

// Healthy returns true if the adapter is operational.
func (a *SlackAdapter) Healthy() bool {
	return a.healthy.Load()
}

// Send posts an AgentMail message to the configured Slack channel using chat.postMessage.
// The mail payload is serialised as a JSON code block for structured delivery.
func (a *SlackAdapter) Send(ctx context.Context, mail *types.AgentMail) error {
	if mail == nil {
		return fmt.Errorf("mail must not be nil")
	}
	if !a.healthy.Load() {
		return fmt.Errorf("slack adapter is not healthy")
	}

	token, err := a.resolveToken(ctx)
	if err != nil {
		return fmt.Errorf("adapters.SlackAdapter.Send: %w", err)
	}


	// Build a Slack-friendly message with metadata.
	text := fmt.Sprintf("*AgentMail* `%s`\nFrom: `%s` → To: `%s`\nPriority: %s",
		mail.ID, mail.From, mail.To, mail.Priority)

	payloadStr := string(mail.Payload)
	if payloadStr != "" && payloadStr != "null" {
		text += fmt.Sprintf("\n```\n%s\n```", payloadStr)
	}

	channelID := a.config.DefaultChannelID

	reqBody, err := json.Marshal(map[string]string{
		"channel": channelID,
		"text":    text,
	})
	if err != nil {
		return fmt.Errorf("adapters.SlackAdapter.Send: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, slackAPIBase+"/chat.postMessage", bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("adapters.SlackAdapter.Send: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.client.Do(req)
	if err != nil {
		a.healthy.Store(false)
		return fmt.Errorf("adapters.SlackAdapter.Send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var slackResp slackResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&slackResp); err != nil {
		return fmt.Errorf("adapters.SlackAdapter.Send: %w", err)
	}

	if !slackResp.OK {
		return fmt.Errorf("slack API error: %s", slackResp.Error)
	}

	a.logger.Debug("slack message sent", "mail_id", mail.ID, "channel", channelID, "ts", slackResp.TS)
	return nil
}

// Receive polls conversations.history for new messages since the last known timestamp.
// Messages are converted to AgentMail envelopes with the Slack message as payload.
func (a *SlackAdapter) Receive(ctx context.Context) ([]*types.AgentMail, error) {
	if !a.healthy.Load() {
		return nil, fmt.Errorf("slack adapter is not healthy")
	}

	token, err := a.resolveToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("adapters.SlackAdapter.Receive: %w", err)
	}


	a.mu.Lock()
	oldest := a.lastTS
	a.mu.Unlock()

	url := fmt.Sprintf("%s/conversations.history?channel=%s&limit=50", slackAPIBase, a.config.DefaultChannelID)
	if oldest != "" {
		url += "&oldest=" + oldest
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("adapters.SlackAdapter.Receive: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.client.Do(req)
	if err != nil {
		a.healthy.Store(false)
		return nil, fmt.Errorf("adapters.SlackAdapter.Receive: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var histResp slackHistoryResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(&histResp); err != nil {
		return nil, fmt.Errorf("adapters.SlackAdapter.Receive: %w", err)
	}

	if !histResp.OK {
		return nil, fmt.Errorf("slack conversations.history error: %s", histResp.Error)
	}

	if len(histResp.Messages) == 0 {
		return nil, nil
	}

	var messages []*types.AgentMail
	var latestTS string

	for _, msg := range histResp.Messages {
		// Skip bot messages to avoid echo loops.
		if msg.BotID != "" {
			continue
		}

		payload, marshalErr := json.Marshal(map[string]string{
			"text":    msg.Text,
			"user":    msg.User,
			"ts":      msg.TS,
			"channel": a.config.DefaultChannelID,
		})
		if marshalErr != nil {
			a.logger.Warn("failed to marshal slack message payload", "ts", msg.TS, "error", marshalErr)
			continue
		}

		mail := &types.AgentMail{
			ID:          fmt.Sprintf("slack-%s-%s", a.config.DefaultChannelID, msg.TS),
			From:        fmt.Sprintf("slack:%s", msg.User),
			To:          "local",
			WorkspaceID: "",
			Priority:    types.MailPriorityStandard,
			Payload:     payload,
			SchemaID:    "slack.message.v1",
			SentAt:      time.Now(),
		}
		messages = append(messages, mail)

		if msg.TS > latestTS {
			latestTS = msg.TS
		}
	}

	if latestTS != "" {
		a.mu.Lock()
		a.lastTS = latestTS
		a.mu.Unlock()
	}

	if len(messages) > 0 {
		a.logger.Debug("received slack messages", "count", len(messages))
	}
	return messages, nil
}

// authTest calls Slack auth.test to validate the bot token.
func (a *SlackAdapter) authTest(ctx context.Context) error {
	token, err := a.resolveToken(ctx)
	if err != nil {
		return fmt.Errorf("adapters.SlackAdapter.authTest: %w", err)
	}


	req, err := http.NewRequestWithContext(ctx, http.MethodPost, slackAPIBase+"/auth.test", nil)
	if err != nil {
		return fmt.Errorf("adapters.SlackAdapter.authTest: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("adapters.SlackAdapter.authTest: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var authResp slackResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&authResp); err != nil {
		return fmt.Errorf("adapters.SlackAdapter.authTest: %w", err)
	}

	if !authResp.OK {
		return fmt.Errorf("auth.test: %s", authResp.Error)
	}

	return nil
}

// resolveToken resolves the Slack bot token from the secrets registry.
func (a *SlackAdapter) resolveToken(ctx context.Context) (string, error) {
	if a.secrets == nil {
		return "", fmt.Errorf("secret registry not configured")
	}
	token, err := secrets.ResolveSecretRef(ctx, a.secrets, a.config.BotTokenRef)
	if err != nil {
		return "", fmt.Errorf("adapters.SlackAdapter.resolveToken: %w", err)
	}
	return token, nil
}

// slackResponse is a minimal Slack API response envelope.
type slackResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	TS    string `json:"ts,omitempty"`
}

// slackMessage represents a single message from conversations.history.
type slackMessage struct {
	Type  string `json:"type"`
	User  string `json:"user"`
	Text  string `json:"text"`
	TS    string `json:"ts"`
	BotID string `json:"bot_id,omitempty"`
}

// slackHistoryResponse is the response from conversations.history.
type slackHistoryResponse struct {
	OK       bool           `json:"ok"`
	Error    string         `json:"error,omitempty"`
	Messages []slackMessage `json:"messages"`
}
