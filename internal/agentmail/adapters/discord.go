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
var _ agentmail.MessengerAdapter = (*DiscordAdapter)(nil)

// DiscordConfig holds configuration for the DiscordAdapter.
type DiscordConfig struct {
	// BotTokenRef is a "secret:key:scope" reference for the Discord Bot Token.
	BotTokenRef string

	// DefaultChannelID is the Discord channel ID for outbound messages.
	DefaultChannelID string

	// Timeout is the HTTP client timeout. Defaults to 10s.
	Timeout time.Duration
}

// discordAPIBase is the Discord REST API base URL. It is a var (not const)
// to allow test overrides via setDiscordAPIBase.
var discordAPIBase = "https://discord.com/api/v10"

// setDiscordAPIBase overrides the Discord API base URL. Used in tests only.
func setDiscordAPIBase(base string) { discordAPIBase = base }

// DiscordAdapter implements MessengerAdapter for the Discord REST API.
// Outbound: Uses Create Message endpoint to post to a channel.
// Inbound: Polls channel messages for new content.
type DiscordAdapter struct {
	config   DiscordConfig
	secrets  *secrets.Registry
	logger   *slog.Logger
	client   *http.Client
	healthy  atomic.Bool
	started  atomic.Bool
	lastID   string // snowflake ID of last received message for polling
	mu       sync.Mutex
	stopOnce sync.Once
}

// NewDiscordAdapter creates a DiscordAdapter with the given configuration.
func NewDiscordAdapter(cfg DiscordConfig, reg *secrets.Registry, logger *slog.Logger) *DiscordAdapter {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	return &DiscordAdapter{
		config:  cfg,
		secrets: reg,
		logger:  logger.With("adapter", "discord"),
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// Name returns "discord".
func (a *DiscordAdapter) Name() string {
	return "discord"
}

// Start validates the bot token by fetching the bot user and marks the adapter healthy.
func (a *DiscordAdapter) Start(ctx context.Context) error {
	if a.started.Swap(true) {
		return fmt.Errorf("discord adapter already started")
	}

	if err := a.validateToken(ctx); err != nil {
		a.started.Store(false)
		return fmt.Errorf("adapters.DiscordAdapter.Start: %w", err)
	}

	a.healthy.Store(true)
	a.logger.Info("discord adapter started", "channel", a.config.DefaultChannelID)
	return nil
}

// Stop marks the adapter as unhealthy.
func (a *DiscordAdapter) Stop() error {
	a.stopOnce.Do(func() {
		a.healthy.Store(false)
		a.started.Store(false)
		a.logger.Info("discord adapter stopped")
	})
	return nil
}

// Healthy returns true if the adapter is operational.
func (a *DiscordAdapter) Healthy() bool {
	return a.healthy.Load()
}

// Send posts an AgentMail message to the configured Discord channel.
// The mail is formatted as an embed with metadata fields.
func (a *DiscordAdapter) Send(ctx context.Context, mail *types.AgentMail) error {
	if mail == nil {
		return fmt.Errorf("mail must not be nil")
	}
	if !a.healthy.Load() {
		return fmt.Errorf("discord adapter is not healthy")
	}

	token, err := a.resolveToken(ctx)
	if err != nil {
		return fmt.Errorf("adapters.DiscordAdapter.Send: %w", err)
	}

	// Build a Discord message with embedded fields for structured display.
	embed := discordEmbed{
		Title:       fmt.Sprintf("AgentMail: %s", mail.ID),
		Description: string(mail.Payload),
		Color:       priorityColor(mail.Priority),
		Fields: []discordField{
			{Name: "From", Value: mail.From, Inline: true},
			{Name: "To", Value: mail.To, Inline: true},
			{Name: "Priority", Value: string(mail.Priority), Inline: true},
		},
		Timestamp: mail.SentAt.Format(time.RFC3339),
	}

	reqBody, err := json.Marshal(discordCreateMessage{
		Embeds: []discordEmbed{embed},
	})
	if err != nil {
		return fmt.Errorf("adapters.DiscordAdapter.Send: %w", err)
	}

	url := fmt.Sprintf("%s/channels/%s/messages", discordAPIBase, a.config.DefaultChannelID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("adapters.DiscordAdapter.Send: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+token)
	req.Header.Set("User-Agent", "HyperAX-AgentMail (hyperax, 1.0)")

	resp, err := a.client.Do(req)
	if err != nil {
		a.healthy.Store(false)
		return fmt.Errorf("adapters.DiscordAdapter.Send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("discord API error (status %d): %s", resp.StatusCode, string(body))
	}
	io.Copy(io.Discard, resp.Body) //nolint:errcheck

	a.logger.Debug("discord message sent", "mail_id", mail.ID, "channel", a.config.DefaultChannelID)
	return nil
}

// Receive polls the Discord channel for new messages since the last known message ID.
// Messages are converted to AgentMail envelopes with the Discord message as payload.
func (a *DiscordAdapter) Receive(ctx context.Context) ([]*types.AgentMail, error) {
	if !a.healthy.Load() {
		return nil, fmt.Errorf("discord adapter is not healthy")
	}

	token, err := a.resolveToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("adapters.DiscordAdapter.Receive: %w", err)
	}

	a.mu.Lock()
	after := a.lastID
	a.mu.Unlock()

	url := fmt.Sprintf("%s/channels/%s/messages?limit=50", discordAPIBase, a.config.DefaultChannelID)
	if after != "" {
		url += "&after=" + after
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("adapters.DiscordAdapter.Receive: %w", err)
	}
	req.Header.Set("Authorization", "Bot "+token)
	req.Header.Set("User-Agent", "HyperAX-AgentMail (hyperax, 1.0)")

	resp, err := a.client.Do(req)
	if err != nil {
		a.healthy.Store(false)
		return nil, fmt.Errorf("adapters.DiscordAdapter.Receive: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck
		return nil, fmt.Errorf("discord API error: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("adapters.DiscordAdapter.Receive: %w", err)
	}

	var discordMsgs []discordMessage
	if err := json.Unmarshal(body, &discordMsgs); err != nil {
		return nil, fmt.Errorf("adapters.DiscordAdapter.Receive: %w", err)
	}

	if len(discordMsgs) == 0 {
		return nil, nil
	}

	var messages []*types.AgentMail
	var latestID string

	for _, msg := range discordMsgs {
		// Skip bot messages to avoid echo loops.
		if msg.Author.Bot {
			continue
		}

		payload, _ := json.Marshal(map[string]string{
			"content":    msg.Content,
			"author_id":  msg.Author.ID,
			"author":     msg.Author.Username,
			"message_id": msg.ID,
			"channel_id": a.config.DefaultChannelID,
		})

		mail := &types.AgentMail{
			ID:          fmt.Sprintf("discord-%s-%s", a.config.DefaultChannelID, msg.ID),
			From:        fmt.Sprintf("discord:%s", msg.Author.ID),
			To:          "local",
			WorkspaceID: "",
			Priority:    types.MailPriorityStandard,
			Payload:     payload,
			SchemaID:    "discord.message.v1",
			SentAt:      time.Now(),
		}
		messages = append(messages, mail)

		if msg.ID > latestID {
			latestID = msg.ID
		}
	}

	if latestID != "" {
		a.mu.Lock()
		a.lastID = latestID
		a.mu.Unlock()
	}

	if len(messages) > 0 {
		a.logger.Debug("received discord messages", "count", len(messages))
	}
	return messages, nil
}

// validateToken calls GET /users/@me to verify the bot token is valid.
func (a *DiscordAdapter) validateToken(ctx context.Context) error {
	token, err := a.resolveToken(ctx)
	if err != nil {
		return fmt.Errorf("adapters.DiscordAdapter.validateToken: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discordAPIBase+"/users/@me", nil)
	if err != nil {
		return fmt.Errorf("adapters.DiscordAdapter.validateToken: %w", err)
	}
	req.Header.Set("Authorization", "Bot "+token)
	req.Header.Set("User-Agent", "HyperAX-AgentMail (hyperax, 1.0)")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("adapters.DiscordAdapter.validateToken: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("discord returned status %d for token validation", resp.StatusCode)
	}

	return nil
}

// resolveToken resolves the Discord bot token from the secrets registry.
func (a *DiscordAdapter) resolveToken(ctx context.Context) (string, error) {
	if a.secrets == nil {
		return "", fmt.Errorf("secret registry not configured")
	}
	token, err := secrets.ResolveSecretRef(ctx, a.secrets, a.config.BotTokenRef)
	if err != nil {
		return "", fmt.Errorf("adapters.DiscordAdapter.resolveToken: %w", err)
	}
	return token, nil
}

// priorityColor maps AgentMail priority to a Discord embed colour.
func priorityColor(p types.MailPriority) int {
	switch p {
	case types.MailPriorityUrgent:
		return 0xFF0000 // red
	case types.MailPriorityStandard:
		return 0x0099FF // blue
	case types.MailPriorityBackground:
		return 0x808080 // grey
	default:
		return 0x0099FF
	}
}

// Discord API types — minimal structs for the endpoints we use.

type discordCreateMessage struct {
	Content string         `json:"content,omitempty"`
	Embeds  []discordEmbed `json:"embeds,omitempty"`
}

type discordEmbed struct {
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Color       int            `json:"color,omitempty"`
	Fields      []discordField `json:"fields,omitempty"`
	Timestamp   string         `json:"timestamp,omitempty"`
}

type discordField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

type discordMessage struct {
	ID      string        `json:"id"`
	Content string        `json:"content"`
	Author  discordAuthor `json:"author"`
}

type discordAuthor struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Bot      bool   `json:"bot"`
}
