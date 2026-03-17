package adapters

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hyperax/hyperax/pkg/types"
)

func TestDiscordAdapter_Name(t *testing.T) {
	a := NewDiscordAdapter(DiscordConfig{}, nil, testLogger())
	if a.Name() != "discord" {
		t.Fatalf("expected name 'discord', got %q", a.Name())
	}
}

func TestDiscordAdapter_Start_ValidToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/@me" {
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "12345", "username": "hyperax-bot"})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	origBase := discordAPIBase
	defer func() { setDiscordAPIBase(origBase) }()
	setDiscordAPIBase(srv.URL)

	reg := testSecretRegistry(t, "discord_token", "Bot-Token-123")
	a := NewDiscordAdapter(DiscordConfig{
		BotTokenRef:      "secret:discord_token",
		DefaultChannelID: "999888777",
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

func TestDiscordAdapter_Start_InvalidToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	origBase := discordAPIBase
	defer func() { setDiscordAPIBase(origBase) }()
	setDiscordAPIBase(srv.URL)

	reg := testSecretRegistry(t, "discord_token", "bad-token")
	a := NewDiscordAdapter(DiscordConfig{
		BotTokenRef: "secret:discord_token",
	}, reg, testLogger())

	if err := a.Start(context.Background()); err == nil {
		t.Fatal("expected error for invalid token")
	}
	if a.Healthy() {
		t.Fatal("should not be healthy after failed start")
	}
}

func TestDiscordAdapter_Send(t *testing.T) {
	var receivedAuth string
	var receivedBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/@me" {
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "12345"})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/messages") && r.Method == http.MethodPost {
			receivedAuth = r.Header.Get("Authorization")
			receivedBody, _ = io.ReadAll(r.Body) //nolint:errcheck // test mock server
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "msg-001"})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	origBase := discordAPIBase
	defer func() { setDiscordAPIBase(origBase) }()
	setDiscordAPIBase(srv.URL)

	reg := testSecretRegistry(t, "discord_token", "my-bot-token")
	a := NewDiscordAdapter(DiscordConfig{
		BotTokenRef:      "secret:discord_token",
		DefaultChannelID: "CH001",
	}, reg, testLogger())
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = a.Stop() }() //nolint:errcheck // cleanup

	mail := &types.AgentMail{
		ID:       "discord-send-001",
		From:     "agent-x",
		To:       "agent-y",
		Priority: types.MailPriorityUrgent,
		Payload:  json.RawMessage(`{"cmd":"deploy"}`),
		SentAt:   time.Now(),
	}

	if err := a.Send(context.Background(), mail); err != nil {
		t.Fatalf("send: %v", err)
	}

	if receivedAuth != "Bot my-bot-token" {
		t.Fatalf("expected 'Bot my-bot-token', got %q", receivedAuth)
	}

	// Verify the embed was sent.
	var msg discordCreateMessage
	if err := json.Unmarshal(receivedBody, &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(msg.Embeds) != 1 {
		t.Fatalf("expected 1 embed, got %d", len(msg.Embeds))
	}
	if !strings.Contains(msg.Embeds[0].Title, "discord-send-001") {
		t.Fatalf("expected embed title to contain mail ID, got %q", msg.Embeds[0].Title)
	}
	// Urgent priority should be red.
	if msg.Embeds[0].Color != 0xFF0000 {
		t.Fatalf("expected red color (0xFF0000) for urgent, got 0x%06X", msg.Embeds[0].Color)
	}
}

func TestDiscordAdapter_Send_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/@me" {
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "12345"})
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Missing Permissions"}`)) //nolint:errcheck // test mock server
	}))
	defer srv.Close()

	origBase := discordAPIBase
	defer func() { setDiscordAPIBase(origBase) }()
	setDiscordAPIBase(srv.URL)

	reg := testSecretRegistry(t, "discord_token", "token")
	a := NewDiscordAdapter(DiscordConfig{
		BotTokenRef:      "secret:discord_token",
		DefaultChannelID: "CH001",
	}, reg, testLogger())
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = a.Stop() }() //nolint:errcheck // cleanup

	err := a.Send(context.Background(), &types.AgentMail{
		ID: "err", From: "a", To: "b", Priority: types.MailPriorityStandard, SentAt: time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected error to contain '403', got %q", err.Error())
	}
}

func TestDiscordAdapter_Receive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/@me" {
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "12345"})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/messages") && r.Method == http.MethodGet {
			messages := []discordMessage{
				{ID: "111", Content: "hello", Author: discordAuthor{ID: "U1", Username: "user1", Bot: false}},
				{ID: "222", Content: "world", Author: discordAuthor{ID: "U2", Username: "user2", Bot: false}},
				{ID: "333", Content: "bot msg", Author: discordAuthor{ID: "B1", Username: "botuser", Bot: true}}, // should be skipped
			}
			_ = json.NewEncoder(w).Encode(messages)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	origBase := discordAPIBase
	defer func() { setDiscordAPIBase(origBase) }()
	setDiscordAPIBase(srv.URL)

	reg := testSecretRegistry(t, "discord_token", "token")
	a := NewDiscordAdapter(DiscordConfig{
		BotTokenRef:      "secret:discord_token",
		DefaultChannelID: "CH001",
	}, reg, testLogger())
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = a.Stop() }() //nolint:errcheck // cleanup

	msgs, err := a.Receive(context.Background())
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	// Bot message should be filtered.
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (bot filtered), got %d", len(msgs))
	}
	if msgs[0].From != "discord:U1" {
		t.Fatalf("expected from 'discord:U1', got %q", msgs[0].From)
	}
	if msgs[0].SchemaID != "discord.message.v1" {
		t.Fatalf("expected schema 'discord.message.v1', got %q", msgs[0].SchemaID)
	}
}

func TestDiscordAdapter_Receive_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/@me" {
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "12345"})
			return
		}
		_ = json.NewEncoder(w).Encode([]discordMessage{})
	}))
	defer srv.Close()

	origBase := discordAPIBase
	defer func() { setDiscordAPIBase(origBase) }()
	setDiscordAPIBase(srv.URL)

	reg := testSecretRegistry(t, "discord_token", "token")
	a := NewDiscordAdapter(DiscordConfig{
		BotTokenRef:      "secret:discord_token",
		DefaultChannelID: "CH001",
	}, reg, testLogger())
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = a.Stop() }() //nolint:errcheck // cleanup

	msgs, err := a.Receive(context.Background())
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(msgs))
	}
}

func TestDiscordAdapter_PriorityColor(t *testing.T) {
	tests := []struct {
		priority types.MailPriority
		color    int
	}{
		{types.MailPriorityUrgent, 0xFF0000},
		{types.MailPriorityStandard, 0x0099FF},
		{types.MailPriorityBackground, 0x808080},
		{"unknown", 0x0099FF},
	}

	for _, tt := range tests {
		got := priorityColor(tt.priority)
		if got != tt.color {
			t.Errorf("priorityColor(%q) = 0x%06X, want 0x%06X", tt.priority, got, tt.color)
		}
	}
}
