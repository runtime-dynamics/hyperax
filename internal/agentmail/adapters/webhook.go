package adapters

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/hyperax/hyperax/internal/web/render"
	"github.com/hyperax/hyperax/pkg/types"
)

// Compile-time interface assertion.
var _ agentmail.MessengerAdapter = (*WebhookAdapter)(nil)

// WebhookConfig holds configuration for the WebhookAdapter.
type WebhookConfig struct {
	// TargetURL is the outbound webhook endpoint.
	TargetURL string

	// SecretRef is a "secret:key:scope" reference for the HMAC signing key.
	// Resolved via the secrets registry at runtime.
	SecretRef string

	// InboundPath is the HTTP path to listen on for inbound webhooks (e.g. "/webhooks/agentmail").
	InboundPath string

	// Timeout is the HTTP client timeout for outbound requests.
	Timeout time.Duration
}

// WebhookAdapter implements MessengerAdapter for HTTP webhook delivery with HMAC-SHA256 signing.
// Outbound: POST to TargetURL with X-Signature-256 header.
// Inbound: HTTP handler validates HMAC signature and queues received messages.
type WebhookAdapter struct {
	config   WebhookConfig
	secrets  *secrets.Registry
	logger   *slog.Logger
	client   *http.Client
	healthy  atomic.Bool
	inbound  []*types.AgentMail
	mu       sync.Mutex
	started  atomic.Bool
	stopOnce sync.Once
}

// NewWebhookAdapter creates a WebhookAdapter with the given configuration and secret registry.
// The secret registry is used to resolve the HMAC signing key from the SecretRef.
func NewWebhookAdapter(cfg WebhookConfig, reg *secrets.Registry, logger *slog.Logger) *WebhookAdapter {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	a := &WebhookAdapter{
		config:  cfg,
		secrets: reg,
		logger:  logger.With("adapter", "webhook"),
		client: &http.Client{
			Timeout: timeout,
		},
	}
	return a
}

// Name returns "webhook".
func (a *WebhookAdapter) Name() string {
	return "webhook"
}

// Start marks the adapter as healthy and ready to process messages.
func (a *WebhookAdapter) Start(_ context.Context) error {
	if a.started.Swap(true) {
		return fmt.Errorf("webhook adapter already started")
	}
	a.healthy.Store(true)
	a.logger.Info("webhook adapter started",
		"target_url", a.config.TargetURL,
		"inbound_path", a.config.InboundPath,
	)
	return nil
}

// Stop marks the adapter as unhealthy and prevents further processing.
func (a *WebhookAdapter) Stop() error {
	a.stopOnce.Do(func() {
		a.healthy.Store(false)
		a.started.Store(false)
		a.logger.Info("webhook adapter stopped")
	})
	return nil
}

// Healthy returns true if the adapter has been started and not stopped.
func (a *WebhookAdapter) Healthy() bool {
	return a.healthy.Load()
}

// Send delivers an AgentMail as a JSON POST to the configured target URL.
// The request body is signed with HMAC-SHA256 using the secret resolved from SecretRef.
// The signature is sent in the X-Signature-256 header as "sha256=<hex>".
func (a *WebhookAdapter) Send(ctx context.Context, mail *types.AgentMail) error {
	if mail == nil {
		return fmt.Errorf("mail must not be nil")
	}
	if !a.healthy.Load() {
		return fmt.Errorf("webhook adapter is not healthy")
	}

	body, err := json.Marshal(mail)
	if err != nil {
		return fmt.Errorf("adapters.WebhookAdapter.Send: %w", err)
	}

	signature, err := a.sign(ctx, body)
	if err != nil {
		return fmt.Errorf("adapters.WebhookAdapter.Send: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.config.TargetURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("adapters.WebhookAdapter.Send: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature-256", "sha256="+signature)
	req.Header.Set("X-AgentMail-ID", mail.ID)

	resp, err := a.client.Do(req)
	if err != nil {
		a.healthy.Store(false)
		return fmt.Errorf("adapters.WebhookAdapter.Send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	a.logger.Debug("webhook sent", "mail_id", mail.ID, "status", resp.StatusCode)
	return nil
}

// Receive returns and drains all queued inbound messages.
func (a *WebhookAdapter) Receive(_ context.Context) ([]*types.AgentMail, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.inbound) == 0 {
		return nil, nil
	}

	msgs := a.inbound
	a.inbound = nil
	return msgs, nil
}

// HandleInbound returns an http.HandlerFunc that receives inbound webhook POSTs.
// It validates the HMAC-SHA256 signature and queues the parsed AgentMail.
// Mount this handler at InboundPath on your HTTP router.
func (a *WebhookAdapter) HandleInbound() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			render.Error(w, r, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
		if err != nil {
			render.Error(w, r, "read body failed", http.StatusBadRequest)
			return
		}

		sigHeader := r.Header.Get("X-Signature-256")
		if err := a.verify(r.Context(), body, sigHeader); err != nil {
			a.logger.Warn("inbound webhook signature verification failed", "error", err)
			render.Error(w, r, "invalid signature", http.StatusUnauthorized)
			return
		}

		var mail types.AgentMail
		if err := json.Unmarshal(body, &mail); err != nil {
			render.Error(w, r, "invalid JSON", http.StatusBadRequest)
			return
		}

		a.mu.Lock()
		a.inbound = append(a.inbound, &mail)
		a.mu.Unlock()

		a.logger.Debug("inbound webhook received", "mail_id", mail.ID)
		render.JSON(w, r, map[string]string{"status": "accepted"}, http.StatusAccepted)
	}
}

// sign computes HMAC-SHA256 of the payload using the resolved secret.
func (a *WebhookAdapter) sign(ctx context.Context, payload []byte) (string, error) {
	key, err := a.resolveSecret(ctx)
	if err != nil {
		return "", fmt.Errorf("adapters.WebhookAdapter.sign: %w", err)
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// verify checks that the X-Signature-256 header matches the expected HMAC.
func (a *WebhookAdapter) verify(ctx context.Context, payload []byte, sigHeader string) error {
	if sigHeader == "" {
		return fmt.Errorf("missing X-Signature-256 header")
	}

	// Strip "sha256=" prefix if present.
	const prefix = "sha256="
	sig := sigHeader
	if len(sigHeader) > len(prefix) && sigHeader[:len(prefix)] == prefix {
		sig = sigHeader[len(prefix):]
	}

	expected, err := a.sign(ctx, payload)
	if err != nil {
		return fmt.Errorf("adapters.WebhookAdapter.verify: %w", err)
	}

	sigBytes, err := hex.DecodeString(sig)
	if err != nil {
		return fmt.Errorf("adapters.WebhookAdapter.verify: %w", err)
	}

	expectedBytes, err := hex.DecodeString(expected)
	if err != nil {
		return fmt.Errorf("adapters.WebhookAdapter.verify: %w", err)
	}

	if !hmac.Equal(sigBytes, expectedBytes) {
		return fmt.Errorf("signature mismatch")
	}

	return nil
}

// resolveSecret resolves the HMAC signing key from the secrets registry.
func (a *WebhookAdapter) resolveSecret(ctx context.Context) (string, error) {
	if a.secrets == nil {
		return "", fmt.Errorf("secret registry not configured")
	}
	value, err := secrets.ResolveSecretRef(ctx, a.secrets, a.config.SecretRef)
	if err != nil {
		return "", fmt.Errorf("adapters.WebhookAdapter.resolveSecret: %w", err)
	}
	return value, nil
}
