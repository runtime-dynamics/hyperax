package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/hyperax/hyperax/pkg/types"
	"golang.org/x/crypto/bcrypt"
)

// TokenRepo implements repo.MCPTokenRepo for SQLite.
type TokenRepo struct {
	db *sql.DB
}

// Create stores a new MCP token. The TokenHash field must already be bcrypt-hashed.
func (r *TokenRepo) Create(ctx context.Context, token *types.MCPToken) error {
	scopesJSON, err := json.Marshal(token.Scopes)
	if err != nil {
		return fmt.Errorf("sqlite.TokenRepo.Create: %w", err)
	}

	var expiresAt sql.NullString
	if token.ExpiresAt != nil {
		expiresAt = sql.NullString{String: token.ExpiresAt.UTC().Format(sqliteTimeFormat), Valid: true}
	}

	_, err = r.db.ExecContext(ctx,
		`INSERT INTO mcp_tokens (id, agent_id, token_hash, label, clearance_level, scopes, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		token.ID, token.AgentID, token.TokenHash, token.Label,
		token.ClearanceLevel, string(scopesJSON), expiresAt,
	)
	if err != nil {
		return fmt.Errorf("sqlite.TokenRepo.Create: %w", err)
	}

	return nil
}

// ValidateToken finds a valid (non-revoked, non-expired) token by comparing
// the plaintext against stored bcrypt hashes. This iterates all active tokens
// because bcrypt hashes are non-deterministic (each hash includes a unique salt).
// Returns the matching token or an error if not found / invalid.
func (r *TokenRepo) ValidateToken(ctx context.Context, plaintext string) (*types.MCPToken, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, agent_id, token_hash, label, clearance_level, scopes,
		        expires_at, created_at, revoked_at
		 FROM mcp_tokens
		 WHERE revoked_at IS NULL`,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.TokenRepo.ValidateToken: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		token, err := scanToken(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite.TokenRepo.ValidateToken: %w", err)
		}

		// Skip expired tokens.
		if token.IsExpired() {
			continue
		}

		// Compare plaintext against the stored bcrypt hash.
		if err := bcrypt.CompareHashAndPassword([]byte(token.TokenHash), []byte(plaintext)); err == nil {
			return token, nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.TokenRepo.ValidateToken: %w", err)
	}

	return nil, fmt.Errorf("invalid or expired token")
}

// Revoke marks a token as revoked by setting its revoked_at timestamp.
func (r *TokenRepo) Revoke(ctx context.Context, tokenID string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE mcp_tokens SET revoked_at = datetime('now') WHERE id = ? AND revoked_at IS NULL`,
		tokenID,
	)
	if err != nil {
		return fmt.Errorf("sqlite.TokenRepo.Revoke: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.TokenRepo.Revoke: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("token %q not found or already revoked", tokenID)
	}

	return nil
}

// ListByAgent returns all tokens for an agent, ordered by creation time descending.
func (r *TokenRepo) ListByAgent(ctx context.Context, personaID string) ([]*types.MCPToken, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, agent_id, token_hash, label, clearance_level, scopes,
		        expires_at, created_at, revoked_at
		 FROM mcp_tokens
		 WHERE agent_id = ?
		 ORDER BY created_at DESC`,
		personaID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.TokenRepo.ListByAgent: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tokens []*types.MCPToken
	for rows.Next() {
		token, err := scanToken(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite.TokenRepo.ListByAgent: %w", err)
		}
		tokens = append(tokens, token)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.TokenRepo.ListByAgent: %w", err)
	}
	return tokens, nil
}

// DeleteExpired removes all tokens past their expiry time.
// Returns the number of deleted rows.
func (r *TokenRepo) DeleteExpired(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM mcp_tokens WHERE expires_at IS NOT NULL AND expires_at < datetime('now')`,
	)
	if err != nil {
		return 0, fmt.Errorf("sqlite.TokenRepo.DeleteExpired: %w", err)
	}

	return res.RowsAffected()
}

// GetByID retrieves a single token by its ID.
func (r *TokenRepo) GetByID(ctx context.Context, tokenID string) (*types.MCPToken, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, agent_id, token_hash, label, clearance_level, scopes,
		        expires_at, created_at, revoked_at
		 FROM mcp_tokens
		 WHERE id = ?`,
		tokenID,
	)

	token, err := scanTokenRow(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("token %q not found", tokenID)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite.TokenRepo.GetByID: %w", err)
	}

	return token, nil
}

// scanToken extracts a token from a row scanner (sql.Rows).
func scanToken(rows *sql.Rows) (*types.MCPToken, error) {
	var (
		t         types.MCPToken
		scopesRaw string
		expiresAt sql.NullString
		createdAt string
		revokedAt sql.NullString
	)

	if err := rows.Scan(
		&t.ID, &t.AgentID, &t.TokenHash, &t.Label, &t.ClearanceLevel,
		&scopesRaw, &expiresAt, &createdAt, &revokedAt,
	); err != nil {
		return nil, fmt.Errorf("sqlite.scanToken: %w", err)
	}

	if err := json.Unmarshal([]byte(scopesRaw), &t.Scopes); err != nil {
		slog.Error("failed to unmarshal token scopes from database", "error", err)
	}
	t.CreatedAt, _ = time.Parse(sqliteTimeFormat, createdAt)
	if expiresAt.Valid {
		parsed, _ := time.Parse(sqliteTimeFormat, expiresAt.String)
		t.ExpiresAt = &parsed
	}
	if revokedAt.Valid {
		parsed, _ := time.Parse(sqliteTimeFormat, revokedAt.String)
		t.RevokedAt = &parsed
	}

	return &t, nil
}

// scanTokenRow extracts a token from a single-row scanner (sql.Row).
func scanTokenRow(row *sql.Row) (*types.MCPToken, error) {
	var (
		t         types.MCPToken
		scopesRaw string
		expiresAt sql.NullString
		createdAt string
		revokedAt sql.NullString
	)

	if err := row.Scan(
		&t.ID, &t.AgentID, &t.TokenHash, &t.Label, &t.ClearanceLevel,
		&scopesRaw, &expiresAt, &createdAt, &revokedAt,
	); err != nil {
		return nil, fmt.Errorf("sqlite.scanTokenRow: %w", err)
	}

	if err := json.Unmarshal([]byte(scopesRaw), &t.Scopes); err != nil {
		slog.Error("failed to unmarshal token scopes from database", "error", err)
	}
	t.CreatedAt, _ = time.Parse(sqliteTimeFormat, createdAt)
	if expiresAt.Valid {
		parsed, _ := time.Parse(sqliteTimeFormat, expiresAt.String)
		t.ExpiresAt = &parsed
	}
	if revokedAt.Valid {
		parsed, _ := time.Parse(sqliteTimeFormat, revokedAt.String)
		t.RevokedAt = &parsed
	}

	return &t, nil
}
