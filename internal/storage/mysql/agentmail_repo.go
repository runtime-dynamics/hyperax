package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hyperax/hyperax/pkg/types"
)

// AgentMailRepo implements repo.AgentMailRepo for MySQL.
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

	// MySQL uses TINYINT(1) for booleans; encode explicitly as 0/1 to avoid
	// driver-dependent bool encoding behaviour (SEC-001).
	encryptedInt := 0
	if mail.Encrypted {
		encryptedInt = 1
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO agentmail_messages
			(id, from_id, to_id, workspace_id, priority, payload, pgp_signature,
			 encrypted, ack_deadline_ms, schema_id, direction, sent_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		mail.ID, mail.From, mail.To, mail.WorkspaceID,
		string(mail.Priority), payload, mail.PGPSignature,
		encryptedInt, ackMs, mail.SchemaID,
		direction, mail.SentAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("mysql.AgentMailRepo.Enqueue: %w", err)
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
		return nil, fmt.Errorf("mysql.AgentMailRepo.Dequeue: %w", err)
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
		return nil, fmt.Errorf("mysql.AgentMailRepo.Dequeue: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var messages []*types.AgentMail
	var ids []any
	for rows.Next() {
		m, err := scanMyMail(rows)
		if err != nil {
			return nil, fmt.Errorf("mysql.AgentMailRepo.Dequeue: %w", err)
		}
		messages = append(messages, m)
		ids = append(ids, m.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql.AgentMailRepo.Dequeue: %w", err)
	}

	if len(ids) > 0 {
		placeholders := myMailPlaceholders(len(ids))
		_, err = tx.ExecContext(ctx,
			fmt.Sprintf("DELETE FROM agentmail_messages WHERE id IN (%s)", placeholders),
			ids...,
		)
		if err != nil {
			return nil, fmt.Errorf("mysql.AgentMailRepo.Dequeue: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("mysql.AgentMailRepo.Dequeue: %w", err)
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
		return nil, fmt.Errorf("mysql.AgentMailRepo.Peek: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var messages []*types.AgentMail
	for rows.Next() {
		m, err := scanMyMail(rows)
		if err != nil {
			return nil, fmt.Errorf("mysql.AgentMailRepo.Peek: %w", err)
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql.AgentMailRepo.Peek: %w", err)
	}
	return messages, nil
}

// GetByID retrieves a single mail message by its ID.
func (r *AgentMailRepo) GetByID(ctx context.Context, id string) (*types.AgentMail, error) {
	m := &types.AgentMail{}
	var payload string
	var ackMs int64

	// MySQL stores booleans as TINYINT(1); scan into int then convert (SEC-001).
	var encryptedInt int

	err := r.db.QueryRowContext(ctx,
		`SELECT id, from_id, to_id, workspace_id, priority, payload, pgp_signature,
		        encrypted, ack_deadline_ms, schema_id, sent_at
		 FROM agentmail_messages WHERE id = ?`, id,
	).Scan(
		&m.ID, &m.From, &m.To, &m.WorkspaceID,
		&m.Priority, &payload, &m.PGPSignature,
		&encryptedInt, &ackMs, &m.SchemaID, &m.SentAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("mail %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("mysql.AgentMailRepo.GetByID: %w", err)
	}

	m.Encrypted = encryptedInt != 0
	m.Payload = json.RawMessage(payload)
	m.AckDeadline = time.Duration(ackMs) * time.Millisecond
	return m, nil
}

// Delete removes a mail message by its ID.
func (r *AgentMailRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM agentmail_messages WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("mysql.AgentMailRepo.Delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mysql.AgentMailRepo.Delete: %w", err)
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
		return 0, fmt.Errorf("mysql.AgentMailRepo.CountByDirection: %w", err)
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
		`INSERT INTO agentmail_acks (mail_id, instance_id, acked_at, status)
		 VALUES (?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE
		   instance_id = VALUES(instance_id),
		   acked_at = VALUES(acked_at),
		   status = VALUES(status)`,
		ack.MailID, ack.InstanceID, ack.AckedAt.UTC(), ack.Status,
	)
	if err != nil {
		return fmt.Errorf("mysql.AgentMailRepo.Acknowledge: %w", err)
	}
	return nil
}

// GetAck retrieves the acknowledgment for a mail message.
func (r *AgentMailRepo) GetAck(ctx context.Context, mailID string) (*types.MailAck, error) {
	ack := &types.MailAck{}

	err := r.db.QueryRowContext(ctx,
		"SELECT mail_id, instance_id, acked_at, status FROM agentmail_acks WHERE mail_id = ?",
		mailID,
	).Scan(&ack.MailID, &ack.InstanceID, &ack.AckedAt, &ack.Status)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("ack for mail %q not found", mailID)
	}
	if err != nil {
		return nil, fmt.Errorf("mysql.AgentMailRepo.GetAck: %w", err)
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
		   AND DATE_ADD(m.sent_at, INTERVAL m.ack_deadline_ms / 1000 SECOND) < NOW()
		 ORDER BY m.sent_at ASC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("mysql.AgentMailRepo.ListUnacknowledged: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var messages []*types.AgentMail
	for rows.Next() {
		m, err := scanMyMail(rows)
		if err != nil {
			return nil, fmt.Errorf("mysql.AgentMailRepo.ListUnacknowledged: %w", err)
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql.AgentMailRepo.ListUnacknowledged: %w", err)
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
		entry.QuarantinedAt.UTC(), originalJSON,
	)
	if err != nil {
		return fmt.Errorf("mysql.AgentMailRepo.QuarantineToDLO: %w", err)
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
		return nil, fmt.Errorf("mysql.AgentMailRepo.ListDLO: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []*types.DeadLetterEntry
	for rows.Next() {
		e := &types.DeadLetterEntry{}
		var originalJSON string

		if err := rows.Scan(&e.ID, &e.MailID, &e.Reason, &e.Attempts,
			&e.QuarantinedAt, &originalJSON); err != nil {
			return nil, fmt.Errorf("mysql.AgentMailRepo.ListDLO: %w", err)
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
		return nil, fmt.Errorf("mysql.AgentMailRepo.ListDLO: %w", err)
	}
	return entries, nil
}

// RemoveFromDLO removes a dead letter entry by its ID.
func (r *AgentMailRepo) RemoveFromDLO(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM agentmail_dead_letters WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("mysql.AgentMailRepo.RemoveFromDLO: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mysql.AgentMailRepo.RemoveFromDLO: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("DLO entry %q not found", id)
	}
	return nil
}

// scanMyMail scans an AgentMail row from the standard column set.
// MySQL stores booleans as TINYINT(1); scan into int then convert (SEC-001).
func scanMyMail(rows *sql.Rows) (*types.AgentMail, error) {
	m := &types.AgentMail{}
	var payload string
	var ackMs int64
	var encryptedInt int

	if err := rows.Scan(
		&m.ID, &m.From, &m.To, &m.WorkspaceID,
		&m.Priority, &payload, &m.PGPSignature,
		&encryptedInt, &ackMs, &m.SchemaID, &m.SentAt,
	); err != nil {
		return nil, fmt.Errorf("mysql.scanMyMail: %w", err)
	}

	m.Encrypted = encryptedInt != 0
	m.Payload = json.RawMessage(payload)
	m.AckDeadline = time.Duration(ackMs) * time.Millisecond
	return m, nil
}

// myMailPlaceholders generates "?, ?, ..., ?" for the given count.
func myMailPlaceholders(count int) string {
	parts := make([]string, count)
	for i := 0; i < count; i++ {
		parts[i] = "?"
	}
	return strings.Join(parts, ", ")
}
