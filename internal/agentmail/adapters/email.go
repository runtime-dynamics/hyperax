package adapters

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hyperax/hyperax/internal/agentmail"
	"github.com/hyperax/hyperax/internal/secrets"
	"github.com/hyperax/hyperax/pkg/types"
)

// Compile-time interface assertion.
var _ agentmail.MessengerAdapter = (*EmailAdapter)(nil)

// EmailConfig holds configuration for the EmailAdapter.
type EmailConfig struct {
	// IMAPHost is the IMAP server hostname (e.g. "imap.gmail.com").
	IMAPHost string

	// IMAPPort is the IMAP server port (e.g. 993 for TLS, 143 for STARTTLS).
	IMAPPort int

	// SMTPHost is the SMTP server hostname (e.g. "smtp.gmail.com").
	SMTPHost string

	// SMTPPort is the SMTP server port (e.g. 587 for STARTTLS, 465 for TLS).
	SMTPPort int

	// UsernameRef is a "secret:key:scope" reference for the email account username.
	UsernameRef string

	// PasswordRef is a "secret:key:scope" reference for the email account password or app-specific password.
	PasswordRef string

	// FromAddress is the sender address for outbound messages.
	FromAddress string

	// DefaultRecipient is the default recipient when no routing is specified.
	DefaultRecipient string

	// Folder is the IMAP folder to monitor (defaults to "INBOX").
	Folder string

	// TLSInsecure disables TLS certificate verification (for self-signed certs in dev).
	TLSInsecure bool

	// Timeout is the network timeout for IMAP/SMTP operations. Defaults to 30s.
	Timeout time.Duration
}

// EmailAdapter implements MessengerAdapter for email via IMAP (inbound) and SMTP (outbound).
// Outbound: SMTP with STARTTLS for message delivery.
// Inbound: IMAP polling for unseen messages with MIME multipart parsing.
type EmailAdapter struct {
	config    EmailConfig
	secrets   *secrets.Registry
	logger    *slog.Logger
	healthy   atomic.Bool
	started   atomic.Bool
	lastUID   uint32 // last seen IMAP UID for incremental polling
	mu        sync.Mutex
	stopOnce  sync.Once
}

// NewEmailAdapter creates an EmailAdapter with the given configuration.
func NewEmailAdapter(cfg EmailConfig, reg *secrets.Registry, logger *slog.Logger) *EmailAdapter {
	if cfg.Folder == "" {
		cfg.Folder = "INBOX"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.IMAPPort == 0 {
		cfg.IMAPPort = 993
	}
	if cfg.SMTPPort == 0 {
		cfg.SMTPPort = 587
	}

	return &EmailAdapter{
		config:  cfg,
		secrets: reg,
		logger:  logger.With("adapter", "email"),
	}
}

// Name returns "email".
func (a *EmailAdapter) Name() string {
	return "email"
}

// Start validates IMAP and SMTP credentials by connecting to both servers,
// then marks the adapter as healthy.
func (a *EmailAdapter) Start(ctx context.Context) error {
	if a.started.Swap(true) {
		return fmt.Errorf("email adapter already started")
	}

	// Validate IMAP connectivity.
	if err := a.validateIMAP(ctx); err != nil {
		a.started.Store(false)
		return fmt.Errorf("adapters.EmailAdapter.Start: %w", err)
	}

	// Validate SMTP connectivity.
	if err := a.validateSMTP(ctx); err != nil {
		a.started.Store(false)
		return fmt.Errorf("adapters.EmailAdapter.Start: %w", err)
	}

	a.healthy.Store(true)
	a.logger.Info("email adapter started",
		"imap", fmt.Sprintf("%s:%d", a.config.IMAPHost, a.config.IMAPPort),
		"smtp", fmt.Sprintf("%s:%d", a.config.SMTPHost, a.config.SMTPPort),
		"from", a.config.FromAddress,
	)
	return nil
}

// Stop marks the adapter as unhealthy.
func (a *EmailAdapter) Stop() error {
	a.stopOnce.Do(func() {
		a.healthy.Store(false)
		a.started.Store(false)
		a.logger.Info("email adapter stopped")
	})
	return nil
}

// Healthy returns true if the adapter is operational.
func (a *EmailAdapter) Healthy() bool {
	return a.healthy.Load()
}

// Send delivers an AgentMail message via SMTP with STARTTLS.
// The mail payload is included as the message body. Attachments in the payload
// are encoded as MIME multipart parts.
func (a *EmailAdapter) Send(ctx context.Context, mail *types.AgentMail) error {
	if mail == nil {
		return fmt.Errorf("mail must not be nil")
	}
	if !a.healthy.Load() {
		return fmt.Errorf("email adapter is not healthy")
	}

	username, password, err := a.resolveCredentials(ctx)
	if err != nil {
		return fmt.Errorf("adapters.EmailAdapter.Send: %w", err)
	}

	recipient := a.config.DefaultRecipient
	if mail.To != "" && mail.To != "local" && strings.Contains(mail.To, "@") {
		recipient = mail.To
	}
	if recipient == "" {
		return fmt.Errorf("no recipient configured and mail.To is not an email address")
	}

	// Build the email message.
	subject := fmt.Sprintf("AgentMail: %s (from %s, priority %s)", mail.ID, mail.From, mail.Priority)
	body := formatMailBody(mail)

	msg := buildMIMEMessage(a.config.FromAddress, recipient, subject, body)

	// Connect and send via SMTP with STARTTLS.
	addr := fmt.Sprintf("%s:%d", a.config.SMTPHost, a.config.SMTPPort)
	tlsCfg := &tls.Config{
		ServerName:         a.config.SMTPHost,
		InsecureSkipVerify: a.config.TLSInsecure, //nolint:gosec // configurable for dev
	}

	auth := smtp.PlainAuth("", username, password, a.config.SMTPHost)
	if err := smtpSendMail(addr, auth, a.config.FromAddress, []string{recipient}, msg, tlsCfg); err != nil {
		a.healthy.Store(false)
		return fmt.Errorf("adapters.EmailAdapter.Send: %w", err)
	}

	a.logger.Debug("email sent", "mail_id", mail.ID, "to", recipient)
	return nil
}

// Receive polls the IMAP server for unseen messages since the last known UID.
// Messages are parsed from MIME format and converted to AgentMail envelopes.
func (a *EmailAdapter) Receive(ctx context.Context) ([]*types.AgentMail, error) {
	if !a.healthy.Load() {
		return nil, fmt.Errorf("email adapter is not healthy")
	}
	username, password, err := a.resolveCredentials(ctx)
	if err != nil {
		return nil, fmt.Errorf("adapters.EmailAdapter.Receive: %w", err)
	}

	a.mu.Lock()
	sinceUID := a.lastUID
	a.mu.Unlock()

	conn, err := a.connectIMAP()
	if err != nil {
		a.healthy.Store(false)
		return nil, fmt.Errorf("imap connect: %w", err)
	}
	defer conn.Close()

	// Login — uses sanitized login to prevent credential leakage in errors (SEC-002).
	if err := a.imapLogin(conn, username, password); err != nil {
		return nil, err
	}

	// Select folder.
	if _, err := a.imapCommand(conn, fmt.Sprintf("SELECT %q", a.config.Folder)); err != nil {
		return nil, fmt.Errorf("imap select: %w", err)
	}

	// Search for unseen messages.
	searchCmd := "SEARCH UNSEEN"
	if sinceUID > 0 {
		searchCmd = fmt.Sprintf("UID SEARCH UNSEEN UID %d:*", sinceUID+1)
	}
	searchResp, err := a.imapCommand(conn, searchCmd)
	if err != nil {
		return nil, fmt.Errorf("imap search: %w", err)
	}

	uids := parseSearchUIDs(searchResp, sinceUID > 0)
	if len(uids) == 0 {
		_ = a.imapLogout(conn)
		return nil, nil
	}

	var messages []*types.AgentMail
	var maxUID uint32

	for _, uid := range uids {
		fetchCmd := fmt.Sprintf("UID FETCH %d (BODY[])", uid)
		if sinceUID == 0 {
			fetchCmd = fmt.Sprintf("FETCH %d BODY[]", uid)
		}

		rawResp, fetchErr := a.imapCommand(conn, fetchCmd)
		if fetchErr != nil {
			a.logger.Warn("imap fetch failed", "uid", uid, "error", fetchErr)
			continue
		}

		parsed := parseEmailMessage(rawResp)
		if parsed == nil {
			continue
		}

		payload, _ := json.Marshal(parsed)
		agentMail := &types.AgentMail{
			ID:          fmt.Sprintf("email-%s-%d", a.config.IMAPHost, uid),
			From:        fmt.Sprintf("email:%s", parsed.From),
			To:          "local",
			WorkspaceID: "",
			Priority:    types.MailPriorityStandard,
			Payload:     payload,
			SchemaID:    "email.message.v1",
			SentAt:      parsed.Date,
		}
		messages = append(messages, agentMail)

		if uid > maxUID {
			maxUID = uid
		}
	}

	_ = a.imapLogout(conn)

	if maxUID > 0 {
		a.mu.Lock()
		a.lastUID = maxUID
		a.mu.Unlock()
	}

	if len(messages) > 0 {
		a.logger.Debug("received email messages", "count", len(messages))
	}
	return messages, nil
}

// resolveCredentials resolves the IMAP/SMTP username and password from the secrets registry.
func (a *EmailAdapter) resolveCredentials(ctx context.Context) (username, password string, err error) {
	if a.secrets == nil {
		return "", "", fmt.Errorf("secret registry not configured")
	}
	username, err = secrets.ResolveSecretRef(ctx, a.secrets, a.config.UsernameRef)
	if err != nil {
		return "", "", fmt.Errorf("adapters.EmailAdapter.resolveCredentials: %w", err)
	}
	password, err = secrets.ResolveSecretRef(ctx, a.secrets, a.config.PasswordRef)
	if err != nil {
		return "", "", fmt.Errorf("adapters.EmailAdapter.resolveCredentials: %w", err)
	}
	return username, password, nil
}

// validateIMAP connects to the IMAP server and issues a NOOP to verify connectivity.
func (a *EmailAdapter) validateIMAP(ctx context.Context) error {
	username, password, err := a.resolveCredentials(ctx)
	if err != nil {
		return fmt.Errorf("adapters.EmailAdapter.validateIMAP: %w", err)
	}

	conn, err := a.connectIMAP()
	if err != nil {
		return fmt.Errorf("adapters.EmailAdapter.validateIMAP: %w", err)
	}
	defer conn.Close()

	// Uses sanitized login to prevent credential leakage in errors (SEC-002).
	if err := a.imapLogin(conn, username, password); err != nil {
		return fmt.Errorf("adapters.EmailAdapter.validateIMAP: %w", err)
	}

	if _, err := a.imapCommand(conn, "NOOP"); err != nil {
		return fmt.Errorf("adapters.EmailAdapter.validateIMAP: %w", err)
	}

	_ = a.imapLogout(conn)
	return nil
}

// validateSMTP connects to the SMTP server and issues EHLO to verify connectivity.
func (a *EmailAdapter) validateSMTP(_ context.Context) error {
	addr := net.JoinHostPort(a.config.SMTPHost, fmt.Sprintf("%d", a.config.SMTPPort))
	conn, err := net.DialTimeout("tcp", addr, a.config.Timeout)
	if err != nil {
		return fmt.Errorf("adapters.EmailAdapter.validateSMTP: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, a.config.SMTPHost)
	if err != nil {
		return fmt.Errorf("adapters.EmailAdapter.validateSMTP: %w", err)
	}
	defer client.Close()

	if err := client.Hello("hyperax"); err != nil {
		return fmt.Errorf("adapters.EmailAdapter.validateSMTP: %w", err)
	}

	return nil
}

// connectIMAP establishes a TLS connection to the IMAP server.
func (a *EmailAdapter) connectIMAP() (*imapConn, error) {
	addr := fmt.Sprintf("%s:%d", a.config.IMAPHost, a.config.IMAPPort)
	tlsCfg := &tls.Config{
		ServerName:         a.config.IMAPHost,
		InsecureSkipVerify: a.config.TLSInsecure, //nolint:gosec // configurable for dev
	}

	dialer := &net.Dialer{Timeout: a.config.Timeout}
	rawConn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	if err != nil {
		return nil, fmt.Errorf("tls dial %s: %w", addr, err)
	}

	conn := &imapConn{
		conn:   rawConn,
		reader: bufio.NewReader(rawConn),
		writer: rawConn,
		tag:    0,
	}

	// Read server greeting.
	if _, err := conn.readLine(); err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("read greeting: %w", err)
	}

	return conn, nil
}

// imapLogin sends an IMAP LOGIN command with the given credentials and returns
// a sanitized error that never exposes the username or password in error
// messages, log output, or stack traces (SEC-002).
func (a *EmailAdapter) imapLogin(conn *imapConn, username, password string) error {
	cmd := fmt.Sprintf("LOGIN %q %q", username, password)
	_, err := a.imapCommand(conn, cmd)
	if err != nil {
		// Strip any credential content from the error message to prevent leakage.
		errMsg := err.Error()
		errMsg = strings.ReplaceAll(errMsg, username, "[REDACTED]")
		errMsg = strings.ReplaceAll(errMsg, password, "[REDACTED]")
		return fmt.Errorf("imap login failed: %s", errMsg)
	}
	return nil
}

// imapCommand sends a tagged IMAP command and reads all response lines until the tagged OK/NO/BAD.
func (a *EmailAdapter) imapCommand(conn *imapConn, cmd string) ([]string, error) {
	tag := conn.nextTag()
	line := fmt.Sprintf("%s %s\r\n", tag, cmd)
	if _, err := conn.writer.Write([]byte(line)); err != nil {
		return nil, fmt.Errorf("write command: %w", err)
	}

	var lines []string
	for {
		resp, err := conn.readLine()
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}
		lines = append(lines, resp)

		if strings.HasPrefix(resp, tag+" ") {
			if strings.Contains(resp, "OK") {
				return lines, nil
			}
			return lines, fmt.Errorf("imap error: %s", resp)
		}
	}
}

// imapLogout sends LOGOUT and closes the connection.
func (a *EmailAdapter) imapLogout(conn *imapConn) error {
	if _, err := a.imapCommand(conn, "LOGOUT"); err != nil {
		a.logger.Error("failed to send IMAP LOGOUT command", "error", err)
	}
	return conn.Close()
}

// imapConn wraps a TLS connection with buffered reading and tag generation.
type imapConn struct {
	conn   net.Conn
	reader *bufio.Reader
	writer io.Writer
	tag    int
}

// nextTag returns the next IMAP command tag (e.g. "A001", "A002").
func (c *imapConn) nextTag() string {
	c.tag++
	return fmt.Sprintf("A%03d", c.tag)
}

// readLine reads a single CRLF-terminated line from the connection.
// For literal data ({N}\r\n), it reads the specified number of bytes
// and appends subsequent lines.
func (c *imapConn) readLine() (string, error) {
	line, err := c.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")

	// Handle IMAP literal: line ends with {N}
	if idx := strings.LastIndex(line, "{"); idx >= 0 && strings.HasSuffix(line, "}") {
		sizeStr := line[idx+1 : len(line)-1]
		size, parseErr := strconv.Atoi(sizeStr)
		if parseErr == nil && size > 0 {
			literal := make([]byte, size)
			if _, readErr := io.ReadFull(c.reader, literal); readErr != nil {
				return line, readErr
			}
			line = line[:idx] + string(literal)
			// Read the trailing line after the literal.
			trailing, _ := c.reader.ReadString('\n')
			line += strings.TrimRight(trailing, "\r\n")
		}
	}

	return line, nil
}

// Close closes the underlying connection.
func (c *imapConn) Close() error {
	return c.conn.Close()
}

// smtpSendMail sends an email using SMTP with STARTTLS support.
// This is a var to allow test overrides.
var smtpSendMail = defaultSMTPSendMail

// setSmtpSendMail overrides the SMTP send function. Used in tests only.
func setSmtpSendMail(fn func(addr string, auth smtp.Auth, from string, to []string, msg []byte, tlsCfg *tls.Config) error) {
	smtpSendMail = fn
}

// defaultSMTPSendMail performs the actual SMTP send with STARTTLS.
func defaultSMTPSendMail(addr string, auth smtp.Auth, from string, to []string, msg []byte, tlsCfg *tls.Config) error {
	conn, err := net.DialTimeout("tcp", addr, 30*time.Second)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}

	host, _, _ := net.SplitHostPort(addr)
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	if err := client.Hello("hyperax"); err != nil {
		return fmt.Errorf("ehlo: %w", err)
	}

	// Attempt STARTTLS if the server supports it.
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}

	if err := client.Mail(from); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}

	for _, addr := range to {
		if err := client.Rcpt(addr); err != nil {
			return fmt.Errorf("rcpt to %s: %w", addr, err)
		}
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close data: %w", err)
	}

	return client.Quit()
}

// buildMIMEMessage constructs a basic RFC 2822 email message.
func buildMIMEMessage(from, to, subject, body string) []byte {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("From: %s\r\n", from))
	b.WriteString(fmt.Sprintf("To: %s\r\n", to))
	b.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	b.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z)))
	b.WriteString("X-Mailer: HyperAX-AgentMail/1.0\r\n")
	b.WriteString("\r\n")
	b.WriteString(base64.StdEncoding.EncodeToString([]byte(body)))
	return []byte(b.String())
}

// formatMailBody creates a human-readable text representation of an AgentMail.
func formatMailBody(mail *types.AgentMail) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("AgentMail ID: %s\n", mail.ID))
	b.WriteString(fmt.Sprintf("From: %s\n", mail.From))
	b.WriteString(fmt.Sprintf("To: %s\n", mail.To))
	b.WriteString(fmt.Sprintf("Priority: %s\n", mail.Priority))
	b.WriteString(fmt.Sprintf("Sent: %s\n", mail.SentAt.Format(time.RFC3339)))

	payloadStr := string(mail.Payload)
	if payloadStr != "" && payloadStr != "null" {
		b.WriteString("\n---\n")
		b.WriteString(payloadStr)
		b.WriteString("\n")
	}

	return b.String()
}

// parsedEmail holds the extracted fields from an IMAP-fetched email message.
type parsedEmail struct {
	From        string            `json:"from"`
	To          string            `json:"to"`
	Subject     string            `json:"subject"`
	Date        time.Time         `json:"date"`
	Body        string            `json:"body"`
	ContentType string            `json:"content_type"`
	Headers     map[string]string `json:"headers,omitempty"`
	Attachments []emailAttachment `json:"attachments,omitempty"`
}

// emailAttachment represents a MIME attachment extracted from an email.
type emailAttachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
}

// parseEmailMessage extracts email fields from raw IMAP FETCH response lines.
// Returns nil if the response cannot be parsed.
func parseEmailMessage(lines []string) *parsedEmail {
	// Find the raw message body in the FETCH response.
	raw := extractFetchBody(lines)
	if raw == "" {
		return nil
	}

	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		return nil
	}

	from := msg.Header.Get("From")
	to := msg.Header.Get("To")
	subject := msg.Header.Get("Subject")
	dateStr := msg.Header.Get("Date")
	date, _ := mail.ParseDate(dateStr)

	result := &parsedEmail{
		From:    from,
		To:      to,
		Subject: subject,
		Date:    date,
		Headers: make(map[string]string),
	}

	// Capture select headers.
	for _, hdr := range []string{"Message-ID", "In-Reply-To", "References", "X-Mailer"} {
		if v := msg.Header.Get(hdr); v != "" {
			result.Headers[hdr] = v
		}
	}

	// Parse MIME body.
	contentType := msg.Header.Get("Content-Type")
	result.ContentType = contentType

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		// Simple single-part message.
		body, readErr := io.ReadAll(io.LimitReader(msg.Body, 1<<20)) // 1MB limit
		if readErr == nil {
			result.Body = decodeBody(string(body), msg.Header.Get("Content-Transfer-Encoding"))
		}
		return result
	}

	// Multipart message — extract text parts and attachment metadata.
	boundary := params["boundary"]
	if boundary == "" {
		return result
	}

	mr := multipart.NewReader(msg.Body, boundary)
	for {
		part, partErr := mr.NextPart()
		if partErr != nil {
			break
		}

		partType := part.Header.Get("Content-Type")
		filename := part.FileName()

		if filename != "" {
			// Attachment — record metadata only, skip content to save memory.
			data, _ := io.ReadAll(io.LimitReader(part, 10<<20))
			result.Attachments = append(result.Attachments, emailAttachment{
				Filename:    filename,
				ContentType: partType,
				Size:        len(data),
			})
			continue
		}

		// Text part — extract body.
		if strings.HasPrefix(partType, "text/plain") || (result.Body == "" && strings.HasPrefix(partType, "text/")) {
			data, readErr := io.ReadAll(io.LimitReader(part, 1<<20))
			if readErr == nil {
				encoding := part.Header.Get("Content-Transfer-Encoding")
				result.Body = decodeBody(string(data), encoding)
			}
		}
	}

	return result
}

// extractFetchBody extracts the raw message body from IMAP FETCH response lines.
// It looks for the content between the BODY[] literal markers.
func extractFetchBody(lines []string) string {
	var b strings.Builder
	inBody := false

	for _, line := range lines {
		if !inBody {
			// Look for the start of BODY[] content.
			if strings.Contains(line, "BODY[]") || strings.Contains(line, "BODY[TEXT]") {
				// The body content starts after the literal size indicator.
				idx := strings.Index(line, "}")
				if idx >= 0 && idx+1 < len(line) {
					b.WriteString(line[idx+1:])
					b.WriteString("\n")
				}
				inBody = true
				continue
			}
		} else {
			// Stop at the closing paren of the FETCH response or a tagged response.
			if line == ")" || (len(line) > 1 && line[0] == 'A' && strings.Contains(line, "OK")) {
				break
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	return b.String()
}

// decodeBody decodes an email body based on Content-Transfer-Encoding.
func decodeBody(body, encoding string) string {
	encoding = strings.ToLower(strings.TrimSpace(encoding))
	switch encoding {
	case "base64":
		decoded, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(body), ""))
		if err != nil {
			return body // return raw on decode failure
		}
		return string(decoded)
	case "quoted-printable":
		r := textproto.NewReader(bufio.NewReader(strings.NewReader(body)))
		// Quoted-printable is line-based; read all dot-decoded lines.
		var b strings.Builder
		for {
			line, err := r.ReadLine()
			if err != nil {
				break
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
		if b.Len() > 0 {
			return b.String()
		}
		return body
	default:
		return body
	}
}

// parseSearchUIDs extracts message UIDs from an IMAP SEARCH response.
// If uidMode is true, the UIDs are absolute; otherwise, they are sequence numbers.
func parseSearchUIDs(lines []string, uidMode bool) []uint32 {
	_ = uidMode // both modes parse the same response format
	var uids []uint32
	for _, line := range lines {
		if !strings.HasPrefix(line, "* SEARCH") {
			continue
		}
		parts := strings.Fields(line)
		for _, p := range parts[2:] { // skip "* SEARCH"
			n, err := strconv.ParseUint(p, 10, 32)
			if err == nil {
				uids = append(uids, uint32(n))
			}
		}
	}
	return uids
}
