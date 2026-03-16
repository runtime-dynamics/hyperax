package adapters

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/secrets"
	"github.com/hyperax/hyperax/pkg/types"
)

func TestEmailAdapter_Name(t *testing.T) {
	a := NewEmailAdapter(EmailConfig{}, nil, testLogger())
	if a.Name() != "email" {
		t.Fatalf("expected name 'email', got %q", a.Name())
	}
}

func TestEmailAdapter_Defaults(t *testing.T) {
	a := NewEmailAdapter(EmailConfig{}, nil, testLogger())

	if a.config.Folder != "INBOX" {
		t.Fatalf("expected default folder 'INBOX', got %q", a.config.Folder)
	}
	if a.config.IMAPPort != 993 {
		t.Fatalf("expected default IMAP port 993, got %d", a.config.IMAPPort)
	}
	if a.config.SMTPPort != 587 {
		t.Fatalf("expected default SMTP port 587, got %d", a.config.SMTPPort)
	}
	if a.config.Timeout != 30*time.Second {
		t.Fatalf("expected default timeout 30s, got %v", a.config.Timeout)
	}
}

func TestEmailAdapter_CustomConfig(t *testing.T) {
	a := NewEmailAdapter(EmailConfig{
		Folder:   "SENT",
		IMAPPort: 143,
		SMTPPort: 465,
		Timeout:  10 * time.Second,
	}, nil, testLogger())

	if a.config.Folder != "SENT" {
		t.Fatalf("expected folder 'SENT', got %q", a.config.Folder)
	}
	if a.config.IMAPPort != 143 {
		t.Fatalf("expected IMAP port 143, got %d", a.config.IMAPPort)
	}
	if a.config.SMTPPort != 465 {
		t.Fatalf("expected SMTP port 465, got %d", a.config.SMTPPort)
	}
}

func TestEmailAdapter_HealthyLifecycle(t *testing.T) {
	a := NewEmailAdapter(EmailConfig{}, nil, testLogger())
	if a.Healthy() {
		t.Fatal("should not be healthy before start")
	}
	if a.started.Load() {
		t.Fatal("should not be started before start")
	}
}

func TestEmailAdapter_Send_NotHealthy(t *testing.T) {
	a := NewEmailAdapter(EmailConfig{}, nil, testLogger())
	err := a.Send(context.Background(), &types.AgentMail{
		ID: "test", From: "a", To: "b", Priority: types.MailPriorityStandard, SentAt: time.Now(),
	})
	if err == nil {
		t.Fatal("expected error when not healthy")
	}
	if !strings.Contains(err.Error(), "not healthy") {
		t.Fatalf("expected 'not healthy' error, got %q", err.Error())
	}
}

func TestEmailAdapter_Send_NilMail(t *testing.T) {
	a := NewEmailAdapter(EmailConfig{}, nil, testLogger())
	a.healthy.Store(true)
	err := a.Send(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil mail")
	}
	if !strings.Contains(err.Error(), "must not be nil") {
		t.Fatalf("expected 'must not be nil' error, got %q", err.Error())
	}
}

func TestEmailAdapter_Send_NoRecipient(t *testing.T) {
	reg := testSecretRegistry(t, "email_user", "user@test.com")

	a := NewEmailAdapter(EmailConfig{
		UsernameRef: "secret:email_user",
		PasswordRef: "secret:email_user", // reuse same key for simplicity
		// No DefaultRecipient, and mail.To is not an email
	}, reg, testLogger())
	a.healthy.Store(true)

	err := a.Send(context.Background(), &types.AgentMail{
		ID: "test", From: "agent-a", To: "local", Priority: types.MailPriorityStandard, SentAt: time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for no recipient")
	}
	if !strings.Contains(err.Error(), "no recipient") {
		t.Fatalf("expected 'no recipient' error, got %q", err.Error())
	}
}

func testEmailSecretRegistry(t *testing.T) *secrets.Registry {
	t.Helper()
	reg := secrets.NewRegistry()
	reg.Register(&testProvider{secrets: map[string]string{
		"email_user": "user@test.com",
		"email_pass": "password123",
	}})
	return reg
}

func TestEmailAdapter_Send_Success(t *testing.T) {
	var capturedFrom string
	var capturedTo []string
	var capturedMsg []byte

	origSend := smtpSendMail
	defer func() { smtpSendMail = origSend }()
	smtpSendMail = func(addr string, auth smtp.Auth, from string, to []string, msg []byte, _ *tls.Config) error {
		capturedFrom = from
		capturedTo = to
		capturedMsg = msg
		return nil
	}

	reg := testEmailSecretRegistry(t)

	a := NewEmailAdapter(EmailConfig{
		SMTPHost:         "smtp.test.com",
		SMTPPort:         587,
		FromAddress:      "hyperax@test.com",
		DefaultRecipient: "admin@test.com",
		UsernameRef:      "secret:email_user",
		PasswordRef:      "secret:email_pass",
	}, reg, testLogger())
	a.healthy.Store(true)

	mail := &types.AgentMail{
		ID:       "email-send-001",
		From:     "agent-a",
		To:       "agent-b",
		Priority: types.MailPriorityUrgent,
		Payload:  json.RawMessage(`{"task":"deploy"}`),
		SentAt:   time.Now(),
	}

	if err := a.Send(context.Background(), mail); err != nil {
		t.Fatalf("send: %v", err)
	}

	if capturedFrom != "hyperax@test.com" {
		t.Fatalf("expected from 'hyperax@test.com', got %q", capturedFrom)
	}
	if len(capturedTo) != 1 || capturedTo[0] != "admin@test.com" {
		t.Fatalf("expected to ['admin@test.com'], got %v", capturedTo)
	}
	if !strings.Contains(string(capturedMsg), "Subject: AgentMail: email-send-001") {
		t.Fatalf("expected subject containing mail ID in message:\n%s", string(capturedMsg))
	}
	if !strings.Contains(string(capturedMsg), "X-Mailer: HyperAX-AgentMail/1.0") {
		t.Fatalf("expected X-Mailer header in message:\n%s", string(capturedMsg))
	}
}

func TestEmailAdapter_Send_EmailRecipient(t *testing.T) {
	var capturedTo []string

	origSend := smtpSendMail
	defer func() { smtpSendMail = origSend }()
	smtpSendMail = func(_ string, _ smtp.Auth, _ string, to []string, _ []byte, _ *tls.Config) error {
		capturedTo = to
		return nil
	}

	reg := testEmailSecretRegistry(t)

	a := NewEmailAdapter(EmailConfig{
		SMTPHost:         "smtp.test.com",
		FromAddress:      "hyperax@test.com",
		DefaultRecipient: "fallback@test.com",
		UsernameRef:      "secret:email_user",
		PasswordRef:      "secret:email_pass",
	}, reg, testLogger())
	a.healthy.Store(true)

	// When mail.To contains an email address, it should be used instead of DefaultRecipient.
	mail := &types.AgentMail{
		ID: "test", From: "a", To: "recipient@example.com", Priority: types.MailPriorityStandard, SentAt: time.Now(),
	}
	if err := a.Send(context.Background(), mail); err != nil {
		t.Fatalf("send: %v", err)
	}

	if len(capturedTo) != 1 || capturedTo[0] != "recipient@example.com" {
		t.Fatalf("expected to ['recipient@example.com'], got %v", capturedTo)
	}
}

func TestEmailAdapter_Receive_NotHealthy(t *testing.T) {
	a := NewEmailAdapter(EmailConfig{}, nil, testLogger())
	_, err := a.Receive(context.Background())
	if err == nil {
		t.Fatal("expected error when not healthy")
	}
}

func TestEmailAdapter_DoubleStart(t *testing.T) {
	a := NewEmailAdapter(EmailConfig{}, nil, testLogger())
	a.started.Store(true)
	err := a.Start(context.Background())
	if err == nil {
		t.Fatal("expected error on double start")
	}
	if !strings.Contains(err.Error(), "already started") {
		t.Fatalf("expected 'already started' error, got %q", err.Error())
	}
}

func TestEmailAdapter_StopIdempotent(t *testing.T) {
	a := NewEmailAdapter(EmailConfig{}, nil, testLogger())
	a.healthy.Store(true)
	a.started.Store(true)

	if err := a.Stop(); err != nil {
		t.Fatalf("first stop: %v", err)
	}
	if a.Healthy() {
		t.Fatal("should not be healthy after stop")
	}

	// Second stop should not error (sync.Once).
	if err := a.Stop(); err != nil {
		t.Fatalf("second stop: %v", err)
	}
}

// --- MIME parsing tests ---

func TestBuildMIMEMessage(t *testing.T) {
	msg := buildMIMEMessage("from@test.com", "to@test.com", "Test Subject", "Hello World")

	s := string(msg)
	if !strings.Contains(s, "From: from@test.com") {
		t.Fatal("missing From header")
	}
	if !strings.Contains(s, "To: to@test.com") {
		t.Fatal("missing To header")
	}
	if !strings.Contains(s, "Subject: Test Subject") {
		t.Fatal("missing Subject header")
	}
	if !strings.Contains(s, "MIME-Version: 1.0") {
		t.Fatal("missing MIME-Version header")
	}
	if !strings.Contains(s, "Content-Transfer-Encoding: base64") {
		t.Fatal("missing Content-Transfer-Encoding header")
	}
	if !strings.Contains(s, "X-Mailer: HyperAX-AgentMail/1.0") {
		t.Fatal("missing X-Mailer header")
	}
}

func TestFormatMailBody(t *testing.T) {
	mail := &types.AgentMail{
		ID:       "test-001",
		From:     "agent-a",
		To:       "agent-b",
		Priority: types.MailPriorityUrgent,
		Payload:  json.RawMessage(`{"key":"value"}`),
		SentAt:   time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
	}

	body := formatMailBody(mail)
	if !strings.Contains(body, "test-001") {
		t.Fatal("missing mail ID in body")
	}
	if !strings.Contains(body, "agent-a") {
		t.Fatal("missing from in body")
	}
	if !strings.Contains(body, `{"key":"value"}`) {
		t.Fatal("missing payload in body")
	}
}

func TestFormatMailBody_NullPayload(t *testing.T) {
	mail := &types.AgentMail{
		ID:       "test-002",
		From:     "a",
		To:       "b",
		Priority: types.MailPriorityStandard,
		Payload:  json.RawMessage("null"),
		SentAt:   time.Now(),
	}

	body := formatMailBody(mail)
	if strings.Contains(body, "---") {
		t.Fatal("should not contain separator for null payload")
	}
}

func TestDecodeBody_Base64(t *testing.T) {
	// "Hello World" in base64
	encoded := "SGVsbG8gV29ybGQ="
	decoded := decodeBody(encoded, "base64")
	if decoded != "Hello World" {
		t.Fatalf("expected 'Hello World', got %q", decoded)
	}
}

func TestDecodeBody_Plain(t *testing.T) {
	raw := "Just plain text"
	decoded := decodeBody(raw, "7bit")
	if decoded != raw {
		t.Fatalf("expected %q, got %q", raw, decoded)
	}
}

func TestDecodeBody_Empty(t *testing.T) {
	decoded := decodeBody("", "")
	if decoded != "" {
		t.Fatalf("expected empty string, got %q", decoded)
	}
}

func TestParseSearchUIDs(t *testing.T) {
	lines := []string{
		"* SEARCH 1 3 5 7",
		"A001 OK SEARCH completed",
	}

	uids := parseSearchUIDs(lines, false)
	if len(uids) != 4 {
		t.Fatalf("expected 4 UIDs, got %d", len(uids))
	}
	if uids[0] != 1 || uids[1] != 3 || uids[2] != 5 || uids[3] != 7 {
		t.Fatalf("unexpected UIDs: %v", uids)
	}
}

func TestParseSearchUIDs_Empty(t *testing.T) {
	lines := []string{
		"* SEARCH",
		"A001 OK SEARCH completed",
	}

	uids := parseSearchUIDs(lines, false)
	if len(uids) != 0 {
		t.Fatalf("expected 0 UIDs, got %d", len(uids))
	}
}

func TestParseSearchUIDs_NoSearchLine(t *testing.T) {
	lines := []string{
		"A001 OK SEARCH completed",
	}

	uids := parseSearchUIDs(lines, false)
	if len(uids) != 0 {
		t.Fatalf("expected 0 UIDs, got %d", len(uids))
	}
}

func TestParseEmailMessage_Simple(t *testing.T) {
	raw := "From: sender@test.com\r\n" +
		"To: recv@test.com\r\n" +
		"Subject: Test Email\r\n" +
		"Date: Mon, 09 Mar 2026 12:00:00 +0000\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Message-ID: <abc123@test.com>\r\n" +
		"\r\n" +
		"Hello from the test email body.\r\n"

	lines := []string{
		"* 1 FETCH (BODY[] {" + "200" + "}" + raw,
		")",
		"A001 OK FETCH completed",
	}

	parsed := parseEmailMessage(lines)
	if parsed == nil {
		t.Fatal("expected parsed email, got nil")
	}
	if parsed.From != "sender@test.com" {
		t.Fatalf("expected from 'sender@test.com', got %q", parsed.From)
	}
	if parsed.To != "recv@test.com" {
		t.Fatalf("expected to 'recv@test.com', got %q", parsed.To)
	}
	if parsed.Subject != "Test Email" {
		t.Fatalf("expected subject 'Test Email', got %q", parsed.Subject)
	}
}

func TestParseEmailMessage_Empty(t *testing.T) {
	parsed := parseEmailMessage(nil)
	if parsed != nil {
		t.Fatal("expected nil for empty lines")
	}
}

func TestParseEmailMessage_NoBody(t *testing.T) {
	lines := []string{
		"A001 OK FETCH completed",
	}
	parsed := parseEmailMessage(lines)
	if parsed != nil {
		t.Fatal("expected nil for no body content")
	}
}

func TestExtractFetchBody(t *testing.T) {
	lines := []string{
		"* 1 FETCH (BODY[] {100}From: test@test.com",
		"Subject: Hello",
		"",
		"Body text here",
		")",
		"A001 OK FETCH completed",
	}

	body := extractFetchBody(lines)
	if !strings.Contains(body, "From: test@test.com") {
		t.Fatalf("expected body to contain 'From:', got %q", body)
	}
	if !strings.Contains(body, "Body text here") {
		t.Fatalf("expected body to contain text, got %q", body)
	}
}

func TestExtractFetchBody_Empty(t *testing.T) {
	body := extractFetchBody(nil)
	if body != "" {
		t.Fatalf("expected empty body, got %q", body)
	}
}

func TestResolveCredentials_NoRegistry(t *testing.T) {
	a := NewEmailAdapter(EmailConfig{
		UsernameRef: "secret:user",
		PasswordRef: "secret:pass",
	}, nil, testLogger())

	_, _, err := a.resolveCredentials(context.Background())
	if err == nil {
		t.Fatal("expected error with nil registry")
	}
	if !strings.Contains(err.Error(), "secret registry not configured") {
		t.Fatalf("unexpected error: %v", err)
	}
}
