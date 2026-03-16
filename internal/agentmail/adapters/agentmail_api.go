package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/hyperax/hyperax/internal/agentmail"
	"github.com/hyperax/hyperax/internal/secrets"
	"github.com/hyperax/hyperax/pkg/types"
)

// Compile-time interface assertion.
var _ agentmail.MessengerAdapter = (*AgentMailAPIAdapter)(nil)

// AgentMailAPIConfig holds configuration for the AgentMailAPIAdapter.
type AgentMailAPIConfig struct {
	// BaseURL is the remote AgentMail API base (e.g. "https://peer.example.com/api/v1/agentmail").
	BaseURL string

	// InstanceID identifies this Hyperax instance to the remote peer.
	InstanceID string

	// APIKeyRef is a "secret:key:scope" reference for the API bearer token.
	APIKeyRef string

	// PollInterval is how often Receive polls the remote inbox. Defaults to 15s.
	PollInterval time.Duration

	// Timeout is the HTTP client timeout. Defaults to 10s.
	Timeout time.Duration
}

// AgentMailAPIAdapter implements MessengerAdapter for peer-to-peer AgentMail REST API communication.
// Outbound: POST /send with bearer token authentication.
// Inbound: GET /inbox/{instanceID} poll-based receive.
type AgentMailAPIAdapter struct {
	config  AgentMailAPIConfig
	secrets *secrets.Registry
	logger  *slog.Logger
	client  *http.Client
	healthy atomic.Bool
	started atomic.Bool
}

// NewAgentMailAPIAdapter creates an AgentMailAPIAdapter with the given configuration.
func NewAgentMailAPIAdapter(cfg AgentMailAPIConfig, reg *secrets.Registry, logger *slog.Logger) *AgentMailAPIAdapter {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 15 * time.Second
	}

	return &AgentMailAPIAdapter{
		config:  cfg,
		secrets: reg,
		logger:  logger.With("adapter", "agentmail_api"),
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// Name returns "agentmail_api".
func (a *AgentMailAPIAdapter) Name() string {
	return "agentmail_api"
}

// Start marks the adapter as healthy.
func (a *AgentMailAPIAdapter) Start(_ context.Context) error {
	if a.started.Swap(true) {
		return fmt.Errorf("agentmail_api adapter already started")
	}
	a.healthy.Store(true)
	a.logger.Info("agentmail_api adapter started",
		"base_url", a.config.BaseURL,
		"instance_id", a.config.InstanceID,
	)
	return nil
}

// Stop marks the adapter as unhealthy.
func (a *AgentMailAPIAdapter) Stop() error {
	a.healthy.Store(false)
	a.started.Store(false)
	a.logger.Info("agentmail_api adapter stopped")
	return nil
}

// Healthy returns true if the adapter has been started and not stopped.
func (a *AgentMailAPIAdapter) Healthy() bool {
	return a.healthy.Load()
}

// Send delivers an AgentMail message to the remote peer via POST /send.
// The request includes a Bearer token resolved from APIKeyRef.
func (a *AgentMailAPIAdapter) Send(ctx context.Context, mail *types.AgentMail) error {
	if mail == nil {
		return fmt.Errorf("mail must not be nil")
	}
	if !a.healthy.Load() {
		return fmt.Errorf("agentmail_api adapter is not healthy")
	}

	body, err := json.Marshal(mail)
	if err != nil {
		return fmt.Errorf("adapters.AgentMailAPIAdapter.Send: %w", err)
	}

	url := a.config.BaseURL + "/send"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("adapters.AgentMailAPIAdapter.Send: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if err := a.setAuth(ctx, req); err != nil {
		return fmt.Errorf("adapters.AgentMailAPIAdapter.Send: %w", err)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		a.healthy.Store(false)
		return fmt.Errorf("adapters.AgentMailAPIAdapter.Send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		a.logger.Error("failed to drain agentmail API response body", "error", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("peer returned status %d", resp.StatusCode)
	}

	a.logger.Debug("mail sent to peer", "mail_id", mail.ID, "status", resp.StatusCode)
	return nil
}

// Receive polls the remote peer's inbox for messages addressed to this instance.
// Returns an empty slice if no messages are available.
func (a *AgentMailAPIAdapter) Receive(ctx context.Context) ([]*types.AgentMail, error) {
	if !a.healthy.Load() {
		return nil, fmt.Errorf("agentmail_api adapter is not healthy")
	}

	url := fmt.Sprintf("%s/inbox/%s", a.config.BaseURL, a.config.InstanceID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("adapters.AgentMailAPIAdapter.Receive: %w", err)
	}

	if err := a.setAuth(ctx, req); err != nil {
		return nil, fmt.Errorf("adapters.AgentMailAPIAdapter.Receive: %w", err)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		a.healthy.Store(false)
		return nil, fmt.Errorf("adapters.AgentMailAPIAdapter.Receive: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck
		return nil, fmt.Errorf("peer returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10MB limit
	if err != nil {
		return nil, fmt.Errorf("adapters.AgentMailAPIAdapter.Receive: %w", err)
	}

	var messages []*types.AgentMail
	if err := json.Unmarshal(body, &messages); err != nil {
		return nil, fmt.Errorf("adapters.AgentMailAPIAdapter.Receive: %w", err)
	}

	if len(messages) > 0 {
		a.logger.Debug("received messages from peer", "count", len(messages))
	}
	return messages, nil
}

// setAuth resolves the API key and sets the Authorization header.
func (a *AgentMailAPIAdapter) setAuth(ctx context.Context, req *http.Request) error {
	if a.secrets == nil {
		return fmt.Errorf("secret registry not configured")
	}
	token, err := secrets.ResolveSecretRef(ctx, a.secrets, a.config.APIKeyRef)
	if err != nil {
		return fmt.Errorf("adapters.AgentMailAPIAdapter.setAuth: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}
