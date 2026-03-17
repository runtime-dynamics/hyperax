package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hyperax/hyperax/pkg/types"
)

// AgentMailRepo implements repo.AgentMailRepo for SQLite.
type AgentMailRepo struct {
	db *sql.DB
}

// Enqueue persists a new outbound or inbound mail message.
func (r *AgentMailRepo) Enqueue(ctx context.Context, mail *types.AgentMail, direction string) error {
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
		boolToInt(mail.Encrypted), ackMs, mail.SchemaID,
		direction, mail.SentAt.UTC().Format(sqliteTimeFormat),
	)
	if err != nil {
		return fmt.Errorf("sqlite.AgentMailRepo.Enqueue: %w", err)
	}
	return nil
}

// Dequeue retrieves and removes up to limit messages from the specified direction queue.
func (r *AgentMailRepo) Dequeue(ctx context.Context, direction string, limit int) ([]*types.AgentMail, error) {
	if limit <= 0 {
		limit = 100
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("sqlite.AgentMailRepo.Dequeue: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	rows, err := tx.QueryContext(ctx,
		`SELECT id, from_id, to_id, workspace_id, priority, payload, pgp_signature,
		        encrypted, ack_deadline_ms, schema_id, sent_at
		 FROM agentmail_messages
		 WHERE direction = ?
		 ORDER BY
		   CASE priority
		     WHEN 'urgent' THEN 0
		     WHEN 'standard' THEN 1
		     WHEN 'background' THEN 2
		     ELSE 1
		   END,
		   sent_at ASC
		 LIMIT ?`,
		direction, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.AgentMailRepo.Dequeue: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var messages []*types.AgentMail
	var ids []any
	for rows.Next() {
		m, err := scanMail(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite.AgentMailRepo.Dequeue: %w", err)
		}
		messages = append(messages, m)
		ids = append(ids, m.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.AgentMailRepo.Dequeue: %w", err)
	}

	// Delete dequeued messages.
	if len(ids) > 0 {
		placeholders := makePlaceholders(len(ids))
		_, err = tx.ExecContext(ctx,
			fmt.Sprintf("DELETE FROM agentmail_messages WHERE id IN (%s)", placeholders),
			ids...,
		)
		if err != nil {
			return nil, fmt.Errorf("sqlite.AgentMailRepo.Dequeue: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("sqlite.AgentMailRepo.Dequeue: %w", err)
	}

	return messages, nil
}

// Peek returns up to limit messages from the specified direction queue without removing them.
func (r *AgentMailRepo) Peek(ctx context.Context, direction string, limit int) ([]*types.AgentMail, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, from_id, to_id, workspace_id, priority, payload, pgp_signature,
		        encrypted, ack_deadline_ms, schema_id, sent_at
		 FROM agentmail_messages
		 WHERE direction = ?
		 ORDER BY
		   CASE priority
		     WHEN 'urgent' THEN 0
		     WHEN 'standard' THEN 1
		     WHEN 'background' THEN 2
		     ELSE 1
		   END,
		   sent_at ASC
		 LIMIT ?`,
		direction, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.AgentMailRepo.Peek: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var messages []*types.AgentMail
	for rows.Next() {
		m, err := scanMail(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite.AgentMailRepo.Peek: %w", err)
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.AgentMailRepo.Peek: %w", err)
	}
	return messages, nil
}

// GetByID retrieves a single mail message by its ID.
func (r *AgentMailRepo) GetByID(ctx context.Context, id string) (*types.AgentMail, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, from_id, to_id, workspace_id, priority, payload, pgp_signature,
		        encrypted, ack_deadline_ms, schema_id, sent_at
		 FROM agentmail_messages WHERE id = ?`, id,
	)
	m := &types.AgentMail{}
	var sentAt string
	var payload string
	var encrypted int
	var ackMs int64

	err := row.Scan(
		&m.ID, &m.From, &m.To, &m.WorkspaceID,
		&m.Priority, &payload, &m.PGPSignature,
		&encrypted, &ackMs, &m.SchemaID, &sentAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("mail %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite.AgentMailRepo.GetByID: %w", err)
	}

	m.Payload = json.RawMessage(payload)
	m.Encrypted = encrypted != 0
	m.AckDeadline = time.Duration(ackMs) * time.Millisecond
	if m.SentAt, err = parseSQLiteTime(sentAt, "sqlite.AgentMailRepo.GetByID"); err != nil {
		return nil, err
	}

	return m, nil
}

// Delete removes a mail message by its ID.
func (r *AgentMailRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM agentmail_messages WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("sqlite.AgentMailRepo.Delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.AgentMailRepo.Delete: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("mail %q not found", id)
	}
	return nil
}

// CountByDirection returns the number of messages in the given direction queue.
func (r *AgentMailRepo) CountByDirection(ctx context.Context, direction string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM agentmail_messages WHERE direction = ?", direction,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("sqlite.AgentMailRepo.CountByDirection: %w", err)
	}
	return count, nil
}

// Acknowledge records an acknowledgment for a mail message.
func (r *AgentMailRepo) Acknowledge(ctx context.Context, ack *types.MailAck) error {
	if ack.AckedAt.IsZero() {
		ack.AckedAt = time.Now()
	}
	if ack.Status == "" {
		ack.Status = "received"
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO agentmail_acks (mail_id, instance_id, acked_at, status)
		 VALUES (?, ?, ?, ?)`,
		ack.MailID, ack.InstanceID,
		ack.AckedAt.UTC().Format(sqliteTimeFormat), ack.Status,
	)
	if err != nil {
		return fmt.Errorf("sqlite.AgentMailRepo.Acknowledge: %w", err)
	}
	return nil
}

// GetAck retrieves the acknowledgment for a mail message.
func (r *AgentMailRepo) GetAck(ctx context.Context, mailID string) (*types.MailAck, error) {
	ack := &types.MailAck{}
	var ackedAt string

	err := r.db.QueryRowContext(ctx,
		"SELECT mail_id, instance_id, acked_at, status FROM agentmail_acks WHERE mail_id = ?",
		mailID,
	).Scan(&ack.MailID, &ack.InstanceID, &ackedAt, &ack.Status)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("ack for mail %q not found", mailID)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite.AgentMailRepo.GetAck: %w", err)
	}

	if ack.AckedAt, err = parseSQLiteTime(ackedAt, "sqlite.AgentMailRepo.GetAck"); err != nil {
		return nil, err
	}
	return ack, nil
}

// ListUnacknowledged returns mail messages that have not been acknowledged
// and whose ack deadline has passed.
func (r *AgentMailRepo) ListUnacknowledged(ctx context.Context, limit int) ([]*types.AgentMail, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT m.id, m.from_id, m.to_id, m.workspace_id, m.priority, m.payload,
		        m.pgp_signature, m.encrypted, m.ack_deadline_ms, m.schema_id, m.sent_at
		 FROM agentmail_messages m
		 LEFT JOIN agentmail_acks a ON m.id = a.mail_id
		 WHERE a.mail_id IS NULL
		   AND datetime(m.sent_at, '+' || (m.ack_deadline_ms / 1000) || ' seconds') < datetime('now')
		 ORDER BY m.sent_at ASC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.AgentMailRepo.ListUnacknowledged: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var messages []*types.AgentMail
	for rows.Next() {
		m, err := scanMail(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite.AgentMailRepo.ListUnacknowledged: %w", err)
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.AgentMailRepo.ListUnacknowledged: %w", err)
	}
	return messages, nil
}

// QuarantineToDLO moves a failed message to the dead letter office.
func (r *AgentMailRepo) QuarantineToDLO(ctx context.Context, entry *types.DeadLetterEntry) error {
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("dle-%d", time.Now().UnixNano())
	}
	if entry.QuarantinedAt.IsZero() {
		entry.QuarantinedAt = time.Now()
	}

	originalJSON := "{}"
	if entry.OriginalMail != nil {
		data, err := json.Marshal(entry.OriginalMail)
		if err == nil {
			originalJSON = string(data)
		}
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO agentmail_dead_letters (id, mail_id, reason, attempts, quarantined_at, original_mail)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.MailID, entry.Reason, entry.Attempts,
		entry.QuarantinedAt.UTC().Format(sqliteTimeFormat), originalJSON,
	)
	if err != nil {
		return fmt.Errorf("sqlite.AgentMailRepo.QuarantineToDLO: %w", err)
	}
	return nil
}

// ListDLO returns dead letter entries, ordered by quarantined_at DESC.
func (r *AgentMailRepo) ListDLO(ctx context.Context, limit int) ([]*types.DeadLetterEntry, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, mail_id, reason, attempts, quarantined_at, original_mail
		 FROM agentmail_dead_letters
		 ORDER BY quarantined_at DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.AgentMailRepo.ListDLO: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []*types.DeadLetterEntry
	for rows.Next() {
		e := &types.DeadLetterEntry{}
		var quarantinedAt, originalJSON string

		if err := rows.Scan(&e.ID, &e.MailID, &e.Reason, &e.Attempts,
			&quarantinedAt, &originalJSON); err != nil {
			return nil, fmt.Errorf("sqlite.AgentMailRepo.ListDLO: %w", err)
		}

		var parseErr error
		if e.QuarantinedAt, parseErr = parseSQLiteTime(quarantinedAt, "sqlite.AgentMailRepo.ListDLO"); parseErr != nil {
			return nil, parseErr
		}
		if originalJSON != "" && originalJSON != "{}" {
			var m types.AgentMail
			if err := json.Unmarshal([]byte(originalJSON), &m); err == nil {
				e.OriginalMail = &m
			}
		}

		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.AgentMailRepo.ListDLO: %w", err)
	}
	return entries, nil
}

// RemoveFromDLO removes a dead letter entry by its ID.
func (r *AgentMailRepo) RemoveFromDLO(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM agentmail_dead_letters WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("sqlite.AgentMailRepo.RemoveFromDLO: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.AgentMailRepo.RemoveFromDLO: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("DLO entry %q not found", id)
	}
	return nil
}

// scanMail scans an AgentMail row from the standard column set.
func scanMail(rows *sql.Rows) (*types.AgentMail, error) {
	m := &types.AgentMail{}
	var sentAt, payload string
	var encrypted int
	var ackMs int64

	if err := rows.Scan(
		&m.ID, &m.From, &m.To, &m.WorkspaceID,
		&m.Priority, &payload, &m.PGPSignature,
		&encrypted, &ackMs, &m.SchemaID, &sentAt,
	); err != nil {
		return nil, fmt.Errorf("sqlite.scanMail: %w", err)
	}

	m.Payload = json.RawMessage(payload)
	m.Encrypted = encrypted != 0
	m.AckDeadline = time.Duration(ackMs) * time.Millisecond
	var parseErr error
	if m.SentAt, parseErr = parseSQLiteTime(sentAt, "sqlite.scanMail"); parseErr != nil {
		return nil, parseErr
	}

	return m, nil
}

// boolToInt converts a bool to SQLite integer (0/1).
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// makePlaceholders returns "?,?,?" with n placeholders.
func makePlaceholders(n int) string {
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
