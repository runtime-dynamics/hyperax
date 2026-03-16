package adapters

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperax/hyperax/pkg/types"
)

func TestAgentMailAPIAdapter_Name(t *testing.T) {
	a := NewAgentMailAPIAdapter(AgentMailAPIConfig{}, nil, testLogger())
	if a.Name() != "agentmail_api" {
		t.Fatalf("expected name 'agentmail_api', got %q", a.Name())
	}
}

func TestAgentMailAPIAdapter_StartStop(t *testing.T) {
	a := NewAgentMailAPIAdapter(AgentMailAPIConfig{}, nil, testLogger())

	if a.Healthy() {
		t.Fatal("should not be healthy before start")
	}

	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !a.Healthy() {
		t.Fatal("should be healthy after start")
	}

	// Double start should fail.
	if err := a.Start(context.Background()); err == nil {
		t.Fatal("expected error on double start")
	}

	if err := a.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if a.Healthy() {
		t.Fatal("should not be healthy after stop")
	}
}

func TestAgentMailAPIAdapter_Send(t *testing.T) {
	var receivedBody []byte
	var receivedAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/send" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		receivedAuth = r.Header.Get("Authorization")
		var err error
		receivedBody, err = io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	reg := testSecretRegistry(t, "api_key", "test-token-123")

	a := NewAgentMailAPIAdapter(AgentMailAPIConfig{
		BaseURL:    srv.URL,
		InstanceID: "instance-local",
		APIKeyRef:  "secret:api_key",
	}, reg, testLogger())

	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = a.Stop() }()

	mail := &types.AgentMail{
		ID:       "api-send-001",
		From:     "instance-local",
		To:       "instance-remote",
		Priority: types.MailPriorityUrgent,
		Payload:  json.RawMessage(`{"data":"test"}`),
		SentAt:   time.Now(),
	}

	if err := a.Send(context.Background(), mail); err != nil {
		t.Fatalf("send: %v", err)
	}

	if receivedAuth != "Bearer test-token-123" {
		t.Fatalf("expected 'Bearer test-token-123', got %q", receivedAuth)
	}

	var parsed types.AgentMail
	if err := json.Unmarshal(receivedBody, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.ID != "api-send-001" {
		t.Fatalf("expected mail ID 'api-send-001', got %q", parsed.ID)
	}
}

func TestAgentMailAPIAdapter_Send_NilMail(t *testing.T) {
	a := NewAgentMailAPIAdapter(AgentMailAPIConfig{}, nil, testLogger())
	_ = a.Start(context.Background())
	err := a.Send(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil mail")
	}
}

func TestAgentMailAPIAdapter_Send_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	reg := testSecretRegistry(t, "api_key", "token")
	a := NewAgentMailAPIAdapter(AgentMailAPIConfig{
		BaseURL:   srv.URL,
		APIKeyRef: "secret:api_key",
	}, reg, testLogger())
	_ = a.Start(context.Background())
	defer func() { _ = a.Stop() }()

	err := a.Send(context.Background(), &types.AgentMail{
		ID: "err-test", From: "a", To: "b", Priority: types.MailPriorityStandard, SentAt: time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestAgentMailAPIAdapter_Receive(t *testing.T) {
	messages := []*types.AgentMail{
		{
			ID: "recv-001", From: "remote", To: "local",
			Priority: types.MailPriorityStandard,
			Payload:  json.RawMessage(`{"msg":"hello"}`),
			SentAt:   time.Now(),
		},
		{
			ID: "recv-002", From: "remote", To: "local",
			Priority: types.MailPriorityBackground,
			Payload:  json.RawMessage(`{"msg":"world"}`),
			SentAt:   time.Now(),
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(messages)
	}))
	defer srv.Close()

	reg := testSecretRegistry(t, "api_key", "token")
	a := NewAgentMailAPIAdapter(AgentMailAPIConfig{
		BaseURL:    srv.URL,
		InstanceID: "local",
		APIKeyRef:  "secret:api_key",
	}, reg, testLogger())
	_ = a.Start(context.Background())
	defer func() { _ = a.Stop() }()

	received, err := a.Receive(context.Background())
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if len(received) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(received))
	}
	if received[0].ID != "recv-001" {
		t.Fatalf("expected first message ID 'recv-001', got %q", received[0].ID)
	}
}

func TestAgentMailAPIAdapter_Receive_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	reg := testSecretRegistry(t, "api_key", "token")
	a := NewAgentMailAPIAdapter(AgentMailAPIConfig{
		BaseURL:    srv.URL,
		InstanceID: "local",
		APIKeyRef:  "secret:api_key",
	}, reg, testLogger())
	_ = a.Start(context.Background())
	defer func() { _ = a.Stop() }()

	received, err := a.Receive(context.Background())
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if len(received) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(received))
	}
}

func TestAgentMailAPIAdapter_Receive_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	reg := testSecretRegistry(t, "api_key", "token")
	a := NewAgentMailAPIAdapter(AgentMailAPIConfig{
		BaseURL:    srv.URL,
		InstanceID: "local",
		APIKeyRef:  "secret:api_key",
	}, reg, testLogger())
	_ = a.Start(context.Background())
	defer func() { _ = a.Stop() }()

	_, err := a.Receive(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}
