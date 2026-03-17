package adapters

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/internal/secrets"
	"github.com/hyperax/hyperax/pkg/types"
)

func testSecretRegistry(t *testing.T, key, value string) *secrets.Registry {
	t.Helper()
	reg := secrets.NewRegistry()
	reg.Register(&testProvider{secrets: map[string]string{key: value}})
	return reg
}

// testProvider is a minimal in-memory secrets.Provider for testing.
type testProvider struct {
	secrets map[string]string
}

func (p *testProvider) Name() string { return "local" }
func (p *testProvider) Get(_ context.Context, key, _ string) (string, error) {
	v, ok := p.secrets[key]
	if !ok {
		return "", secrets.ErrSecretNotFound
	}
	return v, nil
}
func (p *testProvider) Set(_ context.Context, key, value, _ string) error {
	p.secrets[key] = value
	return nil
}
func (p *testProvider) Delete(_ context.Context, key, _ string) error {
	delete(p.secrets, key)
	return nil
}
func (p *testProvider) List(_ context.Context, _ string) ([]string, error) {
	keys := make([]string, 0, len(p.secrets))
	for k := range p.secrets {
		keys = append(keys, k)
	}
	return keys, nil
}
func (p *testProvider) Rotate(_ context.Context, key, newValue, scope string) (string, error) {
	old, err := p.Get(context.Background(), key, scope)
	if err != nil {
		return "", err
	}
	p.secrets[key] = newValue
	return old, nil
}
func (p *testProvider) Health(_ context.Context) error { return nil }

func (p *testProvider) SetWithAccess(_ context.Context, key, value, _ string, _ string) error {
	p.secrets[key] = value
	return nil
}

func (p *testProvider) ListEntries(_ context.Context, _ string) ([]repo.SecretEntry, error) {
	return nil, nil
}

func (p *testProvider) GetAccessScope(_ context.Context, _, _ string) (string, error) {
	return "global", nil
}

func (p *testProvider) UpdateAccessScope(_ context.Context, _, _, _ string) error {
	return nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestWebhookAdapter_Name(t *testing.T) {
	a := NewWebhookAdapter(WebhookConfig{}, nil, testLogger())
	if a.Name() != "webhook" {
		t.Fatalf("expected name 'webhook', got %q", a.Name())
	}
}

func TestWebhookAdapter_StartStop(t *testing.T) {
	a := NewWebhookAdapter(WebhookConfig{}, nil, testLogger())

	if a.Healthy() {
		t.Fatal("should not be healthy before start")
	}

	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if !a.Healthy() {
		t.Fatal("should be healthy after start")
	}

	// Double start should fail.
	if err := a.Start(context.Background()); err == nil {
		t.Fatal("expected error on double start")
	}

	if err := a.Stop(); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
	if a.Healthy() {
		t.Fatal("should not be healthy after stop")
	}
}

func TestWebhookAdapter_Send_HMACSigning(t *testing.T) {
	const secretKey = "test-hmac-secret"

	var receivedBody []byte
	var receivedSig string
	var receivedMailID string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSig = r.Header.Get("X-Signature-256")
		receivedMailID = r.Header.Get("X-AgentMail-ID")
		var err error
		receivedBody, err = io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	reg := testSecretRegistry(t, "webhook_key", secretKey)

	a := NewWebhookAdapter(WebhookConfig{
		TargetURL: srv.URL,
		SecretRef: "secret:webhook_key",
	}, reg, testLogger())

	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = a.Stop() }()

	mail := &types.AgentMail{
		ID:       "test-mail-001",
		From:     "instance-a",
		To:       "instance-b",
		Priority: types.MailPriorityStandard,
		Payload:  json.RawMessage(`{"action":"hello"}`),
		SentAt:   time.Now(),
	}

	if err := a.Send(context.Background(), mail); err != nil {
		t.Fatalf("send: %v", err)
	}

	// Verify HMAC signature.
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write(receivedBody)
	expectedSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if receivedSig != expectedSig {
		t.Fatalf("signature mismatch:\n  got:      %s\n  expected: %s", receivedSig, expectedSig)
	}

	if receivedMailID != "test-mail-001" {
		t.Fatalf("expected X-AgentMail-ID 'test-mail-001', got %q", receivedMailID)
	}
}

func TestWebhookAdapter_Send_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	reg := testSecretRegistry(t, "wh_key", "secret")
	a := NewWebhookAdapter(WebhookConfig{
		TargetURL: srv.URL,
		SecretRef: "secret:wh_key",
	}, reg, testLogger())
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = a.Stop() }() //nolint:errcheck // cleanup

	err := a.Send(context.Background(), &types.AgentMail{
		ID:       "err-test",
		From:     "a",
		To:       "b",
		Priority: types.MailPriorityStandard,
		SentAt:   time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestWebhookAdapter_Send_NilMail(t *testing.T) {
	a := NewWebhookAdapter(WebhookConfig{}, nil, testLogger())
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	err := a.Send(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil mail")
	}
}

func TestWebhookAdapter_HandleInbound_ValidSignature(t *testing.T) {
	const secretKey = "inbound-secret"
	reg := testSecretRegistry(t, "wh_inbound", secretKey)

	a := NewWebhookAdapter(WebhookConfig{
		SecretRef:   "secret:wh_inbound",
		InboundPath: "/webhooks/agentmail",
	}, reg, testLogger())
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = a.Stop() }() //nolint:errcheck // cleanup

	mail := &types.AgentMail{
		ID:       "inbound-001",
		From:     "external",
		To:       "local",
		Priority: types.MailPriorityUrgent,
		Payload:  json.RawMessage(`{"event":"test"}`),
		SentAt:   time.Now(),
	}
	body, err := json.Marshal(mail)
	if err != nil {
		t.Fatalf("marshal mail: %v", err)
	}

	// Compute HMAC.
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	handler := a.HandleInbound()
	srv := httptest.NewServer(handler)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-Signature-256", sig)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}

	// Verify the message was queued.
	msgs, err := a.Receive(context.Background())
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].ID != "inbound-001" {
		t.Fatalf("expected mail ID 'inbound-001', got %q", msgs[0].ID)
	}

	// Second receive should be empty.
	msgs2, err := a.Receive(context.Background())
	if err != nil {
		t.Fatalf("second receive: %v", err)
	}
	if len(msgs2) != 0 {
		t.Fatalf("expected 0 messages after drain, got %d", len(msgs2))
	}
}

func TestWebhookAdapter_HandleInbound_InvalidSignature(t *testing.T) {
	reg := testSecretRegistry(t, "wh_inbound", "real-secret")
	a := NewWebhookAdapter(WebhookConfig{
		SecretRef: "secret:wh_inbound",
	}, reg, testLogger())
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = a.Stop() }() //nolint:errcheck // cleanup

	handler := a.HandleInbound()
	srv := httptest.NewServer(handler)
	defer srv.Close()

	body := []byte(`{"id":"bad-sig"}`)
	req, err := http.NewRequest(http.MethodPost, srv.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-Signature-256", "sha256=0000000000000000000000000000000000000000000000000000000000000000")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestWebhookAdapter_HandleInbound_MissingSignature(t *testing.T) {
	reg := testSecretRegistry(t, "wh_inbound", "secret")
	a := NewWebhookAdapter(WebhookConfig{
		SecretRef: "secret:wh_inbound",
	}, reg, testLogger())
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = a.Stop() }() //nolint:errcheck // cleanup

	handler := a.HandleInbound()
	srv := httptest.NewServer(handler)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // cleanup

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestWebhookAdapter_HandleInbound_MethodNotAllowed(t *testing.T) {
	a := NewWebhookAdapter(WebhookConfig{}, nil, testLogger())
	handler := a.HandleInbound()
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL) //nolint:noctx // test code, no context needed
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // cleanup

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}
