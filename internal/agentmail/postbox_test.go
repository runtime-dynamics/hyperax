package agentmail

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
	_ "modernc.org/sqlite"
)

// testDB creates an in-memory SQLite database with the agentmail schema.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}

	schema := `
	CREATE TABLE agentmail_messages (
		id          TEXT PRIMARY KEY,
		from_id     TEXT NOT NULL,
		to_id       TEXT NOT NULL,
		workspace_id TEXT NOT NULL DEFAULT '',
		priority    TEXT NOT NULL DEFAULT 'standard',
		payload     TEXT NOT NULL DEFAULT '{}',
		pgp_signature TEXT NOT NULL DEFAULT '',
		encrypted   INTEGER NOT NULL DEFAULT 0,
		ack_deadline_ms INTEGER NOT NULL DEFAULT 300000,
		schema_id   TEXT NOT NULL DEFAULT '',
		direction   TEXT NOT NULL DEFAULT 'outbound',
		sent_at     TEXT NOT NULL DEFAULT (datetime('now')),
		created_at  TEXT NOT NULL DEFAULT (datetime('now'))
	);
	CREATE INDEX idx_agentmail_direction_priority
		ON agentmail_messages(direction, priority, sent_at);
	CREATE TABLE agentmail_acks (
		mail_id     TEXT PRIMARY KEY REFERENCES agentmail_messages(id) ON DELETE CASCADE,
		instance_id TEXT NOT NULL,
		acked_at    TEXT NOT NULL DEFAULT (datetime('now')),
		status      TEXT NOT NULL DEFAULT 'received'
	);
	CREATE TABLE agentmail_dead_letters (
		id              TEXT PRIMARY KEY,
		mail_id         TEXT NOT NULL,
		reason          TEXT NOT NULL DEFAULT '',
		attempts        INTEGER NOT NULL DEFAULT 0,
		quarantined_at  TEXT NOT NULL DEFAULT (datetime('now')),
		original_mail   TEXT NOT NULL DEFAULT '{}'
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })
	return db
}

// testRepoFromDB creates an AgentMailRepo backed by the given test DB.
// We use the concrete sqlite type directly since it's in the same test binary.
type testAgentMailRepo struct {
	db *sql.DB
}

const sqliteTimeFormat = "2006-01-02 15:04:05"

func (r *testAgentMailRepo) Enqueue(ctx context.Context, mail *types.AgentMail, direction string) error {
	if mail.ID == "" {
		mail.ID = fmt.Sprintf("mail-%d", time.Now().UnixNano())
	}
	if mail.Priority == "" {
		mail.Priority = types.MailPriorityStandard
	}
	if mail.SentAt.IsZero() {
		mail.SentAt = time.Now()
	}
	payload := "{}"
	if mail.Payload != nil {
		payload = string(mail.Payload)
	}
	ackMs := types.AckDeadlineFor(mail.Priority).Milliseconds()
	if mail.AckDeadline > 0 {
		ackMs = mail.AckDeadline.Milliseconds()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO agentmail_messages
			(id, from_id, to_id, workspace_id, priority, payload, pgp_signature,
			 encrypted, ack_deadline_ms, schema_id, direction, sent_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		mail.ID, mail.From, mail.To, mail.WorkspaceID,
		string(mail.Priority), payload, mail.PGPSignature,
		testBoolToInt(mail.Encrypted), ackMs, mail.SchemaID,
		direction, mail.SentAt.UTC().Format(sqliteTimeFormat),
	)
	return err
}

func testBoolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func testMakePlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	s := make([]byte, 0, 2*n-1)
	for i := 0; i < n; i++ {
		if i > 0 {
			s = append(s, ',')
		}
		s = append(s, '?')
	}
	return string(s)
}

func (r *testAgentMailRepo) Dequeue(ctx context.Context, direction string, limit int) ([]*types.AgentMail, error) {
	if limit <= 0 {
		limit = 100
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	rows, err := tx.QueryContext(ctx,
		`SELECT id, from_id, to_id, workspace_id, priority, payload, pgp_signature,
		        encrypted, ack_deadline_ms, schema_id, sent_at
		 FROM agentmail_messages WHERE direction = ?
		 ORDER BY CASE priority WHEN 'urgent' THEN 0 WHEN 'standard' THEN 1 WHEN 'background' THEN 2 ELSE 1 END, sent_at ASC
		 LIMIT ?`, direction, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var msgs []*types.AgentMail
	var ids []any
	for rows.Next() {
		m := &types.AgentMail{}
		var sentAt, payload string
		var encrypted int
		var ackMs int64
		if err := rows.Scan(&m.ID, &m.From, &m.To, &m.WorkspaceID, &m.Priority, &payload,
			&m.PGPSignature, &encrypted, &ackMs, &m.SchemaID, &sentAt); err != nil {
			return nil, err
		}
		m.Payload = json.RawMessage(payload)
		m.Encrypted = encrypted != 0
		m.AckDeadline = time.Duration(ackMs) * time.Millisecond
		m.SentAt, _ = time.Parse(sqliteTimeFormat, sentAt) //nolint:errcheck // test repo, format is controlled
		msgs = append(msgs, m)
		ids = append(ids, m.ID)
	}

	if len(ids) > 0 {
		ph := testMakePlaceholders(len(ids))
		_, err = tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM agentmail_messages WHERE id IN (%s)", ph), ids...)
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return msgs, nil
}

func (r *testAgentMailRepo) Peek(ctx context.Context, direction string, limit int) ([]*types.AgentMail, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, from_id, to_id, workspace_id, priority, payload, pgp_signature,
		        encrypted, ack_deadline_ms, schema_id, sent_at
		 FROM agentmail_messages WHERE direction = ?
		 ORDER BY CASE priority WHEN 'urgent' THEN 0 WHEN 'standard' THEN 1 WHEN 'background' THEN 2 ELSE 1 END, sent_at ASC
		 LIMIT ?`, direction, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var msgs []*types.AgentMail
	for rows.Next() {
		m := &types.AgentMail{}
		var sentAt, payload string
		var encrypted int
		var ackMs int64
		if err := rows.Scan(&m.ID, &m.From, &m.To, &m.WorkspaceID, &m.Priority, &payload,
			&m.PGPSignature, &encrypted, &ackMs, &m.SchemaID, &sentAt); err != nil {
			return nil, err
		}
		m.Payload = json.RawMessage(payload)
		m.Encrypted = encrypted != 0
		m.AckDeadline = time.Duration(ackMs) * time.Millisecond
		m.SentAt, _ = time.Parse(sqliteTimeFormat, sentAt) //nolint:errcheck // test repo, format is controlled
		msgs = append(msgs, m)
	}
	return msgs, nil
}

func (r *testAgentMailRepo) GetByID(ctx context.Context, id string) (*types.AgentMail, error) {
	m := &types.AgentMail{}
	var sentAt, payload string
	var encrypted int
	var ackMs int64
	err := r.db.QueryRowContext(ctx,
		`SELECT id, from_id, to_id, workspace_id, priority, payload, pgp_signature,
		        encrypted, ack_deadline_ms, schema_id, sent_at
		 FROM agentmail_messages WHERE id = ?`, id).Scan(
		&m.ID, &m.From, &m.To, &m.WorkspaceID, &m.Priority, &payload,
		&m.PGPSignature, &encrypted, &ackMs, &m.SchemaID, &sentAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("mail %q not found", id)
	}
	if err != nil {
		return nil, err
	}
	m.Payload = json.RawMessage(payload)
	m.Encrypted = encrypted != 0
	m.AckDeadline = time.Duration(ackMs) * time.Millisecond
	m.SentAt, _ = time.Parse(sqliteTimeFormat, sentAt) //nolint:errcheck // test repo, format is controlled
	return m, nil
}

func (r *testAgentMailRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM agentmail_messages WHERE id = ?", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected() //nolint:errcheck // SQLite always supports RowsAffected
	if n == 0 {
		return fmt.Errorf("mail %q not found", id)
	}
	return nil
}

func (r *testAgentMailRepo) CountByDirection(ctx context.Context, direction string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM agentmail_messages WHERE direction = ?", direction).Scan(&count)
	return count, err
}

func (r *testAgentMailRepo) Acknowledge(ctx context.Context, ack *types.MailAck) error {
	if ack.AckedAt.IsZero() {
		ack.AckedAt = time.Now()
	}
	if ack.Status == "" {
		ack.Status = "received"
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO agentmail_acks (mail_id, instance_id, acked_at, status) VALUES (?, ?, ?, ?)`,
		ack.MailID, ack.InstanceID, ack.AckedAt.UTC().Format(sqliteTimeFormat), ack.Status)
	return err
}

func (r *testAgentMailRepo) GetAck(ctx context.Context, mailID string) (*types.MailAck, error) {
	ack := &types.MailAck{}
	var ackedAt string
	err := r.db.QueryRowContext(ctx,
		"SELECT mail_id, instance_id, acked_at, status FROM agentmail_acks WHERE mail_id = ?", mailID).Scan(
		&ack.MailID, &ack.InstanceID, &ackedAt, &ack.Status)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("ack for mail %q not found", mailID)
	}
	if err != nil {
		return nil, err
	}
	ack.AckedAt, _ = time.Parse(sqliteTimeFormat, ackedAt) //nolint:errcheck // test repo, format is controlled
	return ack, nil
}

func (r *testAgentMailRepo) ListUnacknowledged(ctx context.Context, limit int) ([]*types.AgentMail, error) {
	return nil, nil // not tested in postbox tests
}

func (r *testAgentMailRepo) QuarantineToDLO(ctx context.Context, entry *types.DeadLetterEntry) error {
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("dle-%d", time.Now().UnixNano())
	}
	if entry.QuarantinedAt.IsZero() {
		entry.QuarantinedAt = time.Now()
	}
	originalJSON := "{}"
	if entry.OriginalMail != nil {
		data, marshalErr := json.Marshal(entry.OriginalMail)
		if marshalErr != nil {
			return fmt.Errorf("marshal original mail: %w", marshalErr)
		}
		originalJSON = string(data)
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO agentmail_dead_letters (id, mail_id, reason, attempts, quarantined_at, original_mail) VALUES (?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.MailID, entry.Reason, entry.Attempts,
		entry.QuarantinedAt.UTC().Format(sqliteTimeFormat), originalJSON)
	return err
}

func (r *testAgentMailRepo) ListDLO(ctx context.Context, limit int) ([]*types.DeadLetterEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, mail_id, reason, attempts, quarantined_at, original_mail FROM agentmail_dead_letters ORDER BY quarantined_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var entries []*types.DeadLetterEntry
	for rows.Next() {
		e := &types.DeadLetterEntry{}
		var qa, orig string
		if err := rows.Scan(&e.ID, &e.MailID, &e.Reason, &e.Attempts, &qa, &orig); err != nil {
			return nil, err
		}
		e.QuarantinedAt, _ = time.Parse(sqliteTimeFormat, qa) //nolint:errcheck // test repo, format is controlled
		if orig != "" && orig != "{}" {
			var m types.AgentMail
			if err := json.Unmarshal([]byte(orig), &m); err == nil {
				e.OriginalMail = &m
			}
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func (r *testAgentMailRepo) RemoveFromDLO(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM agentmail_dead_letters WHERE id = ?", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected() //nolint:errcheck // SQLite always supports RowsAffected
	if n == 0 {
		return fmt.Errorf("DLO entry %q not found", id)
	}
	return nil
}

func newTestPostbox(t *testing.T) (*Postbox, *nervous.EventBus) {
	t.Helper()
	db := testDB(t)
	r := &testAgentMailRepo{db: db}
	bus := nervous.NewEventBus(64)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewPostbox(r, bus, logger), bus
}

func TestPostbox_SendAndDequeueOutbound(t *testing.T) {
	pb, _ := newTestPostbox(t) //nolint:dogsled // bus not needed
	ctx := context.Background()

	mail := &types.AgentMail{
		ID:       "m1",
		From:     "instance-a",
		To:       "instance-b",
		Priority: types.MailPriorityStandard,
		Payload:  json.RawMessage(`{"hello":"world"}`),
	}

	if err := pb.SendOutbound(ctx, mail); err != nil {
		t.Fatalf("send outbound: %v", err)
	}

	count, err := pb.OutboundCount(ctx)
	if err != nil {
		t.Fatalf("outbound count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 outbound, got %d", count)
	}

	msgs, err := pb.DequeueOutbound(ctx, 10)
	if err != nil {
		t.Fatalf("dequeue outbound: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].ID != "m1" {
		t.Fatalf("expected mail ID 'm1', got %q", msgs[0].ID)
	}

	// Queue should be empty now.
	count, err = pb.OutboundCount(ctx)
	if err != nil {
		t.Fatalf("outbound count after dequeue: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 outbound after dequeue, got %d", count)
	}
}

func TestPostbox_ReceiveInbound(t *testing.T) {
	pb, _ := newTestPostbox(t) //nolint:dogsled // bus not needed
	ctx := context.Background()

	mail := &types.AgentMail{
		ID:       "m2",
		From:     "external",
		To:       "agent-1",
		Priority: types.MailPriorityUrgent,
	}

	if err := pb.ReceiveInbound(ctx, mail); err != nil {
		t.Fatalf("receive inbound: %v", err)
	}

	count, err := pb.InboundCount(ctx)
	if err != nil {
		t.Fatalf("inbound count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 inbound, got %d", count)
	}

	msgs, err := pb.DequeueInbound(ctx, 10)
	if err != nil {
		t.Fatalf("dequeue inbound: %v", err)
	}
	if len(msgs) != 1 || msgs[0].ID != "m2" {
		t.Fatal("unexpected inbound message")
	}
}

func TestPostbox_PriorityOrdering(t *testing.T) {
	pb, _ := newTestPostbox(t) //nolint:dogsled // bus not needed
	ctx := context.Background()

	// Send in reverse priority order.
	for _, p := range []types.MailPriority{types.MailPriorityBackground, types.MailPriorityStandard, types.MailPriorityUrgent} {
		mail := &types.AgentMail{
			ID:       fmt.Sprintf("m-%s", p),
			From:     "a",
			To:       "b",
			Priority: p,
			SentAt:   time.Now(),
		}
		if err := pb.SendOutbound(ctx, mail); err != nil {
			t.Fatalf("send %s: %v", p, err)
		}
	}

	msgs, err := pb.DequeueOutbound(ctx, 10)
	if err != nil {
		t.Fatalf("dequeue outbound: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Priority != types.MailPriorityUrgent {
		t.Fatalf("expected urgent first, got %s", msgs[0].Priority)
	}
	if msgs[1].Priority != types.MailPriorityStandard {
		t.Fatalf("expected standard second, got %s", msgs[1].Priority)
	}
	if msgs[2].Priority != types.MailPriorityBackground {
		t.Fatalf("expected background third, got %s", msgs[2].Priority)
	}
}

func TestPostbox_Acknowledge(t *testing.T) {
	pb, _ := newTestPostbox(t) //nolint:dogsled // bus not needed
	ctx := context.Background()

	mail := &types.AgentMail{ID: "m3", From: "a", To: "b"}
	if err := pb.SendOutbound(ctx, mail); err != nil {
		t.Fatalf("send outbound: %v", err)
	}

	ack := &types.MailAck{
		MailID:     "m3",
		InstanceID: "instance-b",
		Status:     "completed",
	}
	if err := pb.Acknowledge(ctx, ack); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
}

func TestPostbox_Quarantine(t *testing.T) {
	pb, _ := newTestPostbox(t) //nolint:dogsled // bus not needed
	ctx := context.Background()

	mail := &types.AgentMail{ID: "m4", From: "a", To: "b"}
	entry := &types.DeadLetterEntry{
		ID:           "dle-1",
		MailID:       "m4",
		Reason:       "adapter unavailable",
		Attempts:     3,
		OriginalMail: mail,
	}

	if err := pb.Quarantine(ctx, entry); err != nil {
		t.Fatalf("quarantine: %v", err)
	}

	dlo, err := pb.ListDLO(ctx, 10)
	if err != nil {
		t.Fatalf("list DLO: %v", err)
	}
	if len(dlo) != 1 {
		t.Fatalf("expected 1 DLO entry, got %d", len(dlo))
	}
	if dlo[0].MailID != "m4" {
		t.Fatalf("expected mail_id 'm4', got %q", dlo[0].MailID)
	}

	if err := pb.RemoveFromDLO(ctx, "dle-1"); err != nil {
		t.Fatalf("remove from DLO: %v", err)
	}

	dlo, err = pb.ListDLO(ctx, 10)
	if err != nil {
		t.Fatalf("list DLO after removal: %v", err)
	}
	if len(dlo) != 0 {
		t.Fatal("expected empty DLO after removal")
	}
}

func TestPostbox_PeekDoesNotRemove(t *testing.T) {
	pb, _ := newTestPostbox(t) //nolint:dogsled // bus not needed
	ctx := context.Background()

	if err := pb.SendOutbound(ctx, &types.AgentMail{ID: "m5", From: "a", To: "b"}); err != nil {
		t.Fatalf("send outbound: %v", err)
	}

	msgs, err := pb.PeekOutbound(ctx, 10)
	if err != nil {
		t.Fatalf("peek outbound: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 peeked message, got %d", len(msgs))
	}

	// Should still be there.
	count, err := pb.OutboundCount(ctx)
	if err != nil {
		t.Fatalf("outbound count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 outbound after peek, got %d", count)
	}
}
