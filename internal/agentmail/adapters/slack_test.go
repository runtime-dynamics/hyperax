package adapters

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hyperax/hyperax/pkg/types"
)

func TestSlackAdapter_Name(t *testing.T) {
	a := NewSlackAdapter(SlackConfig{}, nil, testLogger())
	if a.Name() != "slack" {
		t.Fatalf("expected name 'slack', got %q", a.Name())
	}
}

func TestSlackAdapter_Start_AuthTest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth.test" {
			_ = json.NewEncoder(w).Encode(slackResponse{OK: true})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	// Override the Slack API base for testing.
	origBase := slackAPIBase
	defer func() { setSlackAPIBase(origBase) }()
	setSlackAPIBase(srv.URL)

	reg := testSecretRegistry(t, "slack_token", "xoxb-test-token")
	a := NewSlackAdapter(SlackConfig{
		BotTokenRef:      "secret:slack_token",
		DefaultChannelID: "C123",
	}, reg, testLogger())

	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !a.Healthy() {
		t.Fatal("should be healthy after start")
	}

	if err := a.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if a.Healthy() {
		t.Fatal("should not be healthy after stop")
	}
}

func TestSlackAdapter_Start_AuthFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(slackResponse{OK: false, Error: "invalid_auth"})
	}))
	defer srv.Close()

	origBase := slackAPIBase
	defer func() { setSlackAPIBase(origBase) }()
	setSlackAPIBase(srv.URL)

	reg := testSecretRegistry(t, "slack_token", "xoxb-bad")
	a := NewSlackAdapter(SlackConfig{
		BotTokenRef: "secret:slack_token",
	}, reg, testLogger())

	if err := a.Start(context.Background()); err == nil {
		t.Fatal("expected error for failed auth.test")
	}
	if a.Healthy() {
		t.Fatal("should not be healthy after failed start")
	}
}

func TestSlackAdapter_Send(t *testing.T) {
	var receivedChannel string
	var receivedText string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth.test" {
			_ = json.NewEncoder(w).Encode(slackResponse{OK: true})
			return
		}
		if r.URL.Path == "/chat.postMessage" {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			receivedChannel = body["channel"]
			receivedText = body["text"]
			_ = json.NewEncoder(w).Encode(slackResponse{OK: true, TS: "1234567890.123456"})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	origBase := slackAPIBase
	defer func() { setSlackAPIBase(origBase) }()
	setSlackAPIBase(srv.URL)

	reg := testSecretRegistry(t, "slack_token", "xoxb-test")
	a := NewSlackAdapter(SlackConfig{
		BotTokenRef:      "secret:slack_token",
		DefaultChannelID: "C999",
	}, reg, testLogger())
	_ = a.Start(context.Background())
	defer func() { _ = a.Stop() }()

	mail := &types.AgentMail{
		ID:       "slack-send-001",
		From:     "agent-a",
		To:       "agent-b",
		Priority: types.MailPriorityUrgent,
		Payload:  json.RawMessage(`{"task":"deploy"}`),
		SentAt:   time.Now(),
	}

	if err := a.Send(context.Background(), mail); err != nil {
		t.Fatalf("send: %v", err)
	}

	if receivedChannel != "C999" {
		t.Fatalf("expected channel 'C999', got %q", receivedChannel)
	}
	if !strings.Contains(receivedText, "slack-send-001") {
		t.Fatalf("expected text to contain mail ID, got %q", receivedText)
	}
}

func TestSlackAdapter_Send_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth.test" {
			_ = json.NewEncoder(w).Encode(slackResponse{OK: true})
			return
		}
		_ = json.NewEncoder(w).Encode(slackResponse{OK: false, Error: "channel_not_found"})
	}))
	defer srv.Close()

	origBase := slackAPIBase
	defer func() { setSlackAPIBase(origBase) }()
	setSlackAPIBase(srv.URL)

	reg := testSecretRegistry(t, "slack_token", "xoxb-test")
	a := NewSlackAdapter(SlackConfig{
		BotTokenRef:      "secret:slack_token",
		DefaultChannelID: "C_BAD",
	}, reg, testLogger())
	_ = a.Start(context.Background())
	defer func() { _ = a.Stop() }()

	err := a.Send(context.Background(), &types.AgentMail{
		ID: "err", From: "a", To: "b", Priority: types.MailPriorityStandard, SentAt: time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for API error response")
	}
	if !strings.Contains(err.Error(), "channel_not_found") {
		t.Fatalf("expected error to contain 'channel_not_found', got %q", err.Error())
	}
}

func TestSlackAdapter_Receive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth.test" {
			_ = json.NewEncoder(w).Encode(slackResponse{OK: true})
			return
		}
		if r.URL.Path == "/conversations.history" {
			_ = json.NewEncoder(w).Encode(slackHistoryResponse{
				OK: true,
				Messages: []slackMessage{
					{Type: "message", User: "U123", Text: "hello agent", TS: "1234567890.000001"},
					{Type: "message", User: "U456", Text: "deploy please", TS: "1234567890.000002"},
					{Type: "message", BotID: "B001", Text: "bot echo", TS: "1234567890.000003"}, // should be skipped
				},
			})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	origBase := slackAPIBase
	defer func() { setSlackAPIBase(origBase) }()
	setSlackAPIBase(srv.URL)

	reg := testSecretRegistry(t, "slack_token", "xoxb-test")
	a := NewSlackAdapter(SlackConfig{
		BotTokenRef:      "secret:slack_token",
		DefaultChannelID: "C123",
	}, reg, testLogger())
	_ = a.Start(context.Background())
	defer func() { _ = a.Stop() }()

	msgs, err := a.Receive(context.Background())
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	// Bot message should be filtered out.
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (bot filtered), got %d", len(msgs))
	}
	if msgs[0].From != "slack:U123" {
		t.Fatalf("expected from 'slack:U123', got %q", msgs[0].From)
	}
	if msgs[0].SchemaID != "slack.message.v1" {
		t.Fatalf("expected schema 'slack.message.v1', got %q", msgs[0].SchemaID)
	}
}

func TestSlackAdapter_Receive_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth.test" {
			_ = json.NewEncoder(w).Encode(slackResponse{OK: true})
			return
		}
		_ = json.NewEncoder(w).Encode(slackHistoryResponse{OK: true, Messages: nil})
	}))
	defer srv.Close()

	origBase := slackAPIBase
	defer func() { setSlackAPIBase(origBase) }()
	setSlackAPIBase(srv.URL)

	reg := testSecretRegistry(t, "slack_token", "xoxb-test")
	a := NewSlackAdapter(SlackConfig{
		BotTokenRef:      "secret:slack_token",
		DefaultChannelID: "C123",
	}, reg, testLogger())
	_ = a.Start(context.Background())
	defer func() { _ = a.Stop() }()

	msgs, err := a.Receive(context.Background())
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(msgs))
	}
}

