package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/hyperax/hyperax/internal/repo"
)

// SymbolRepo implements repo.SymbolRepo for MySQL.
type SymbolRepo struct {
	db *sql.DB
}

// UpsertFileHash inserts or updates a file hash record and returns the file_id.
func (r *SymbolRepo) UpsertFileHash(ctx context.Context, workspaceID, filePath, hash string) (int64, error) {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO file_hashes (workspace_id, file_path, hash_value)
		 VALUES (?, ?, ?)
		 ON DUPLICATE KEY UPDATE hash_value = VALUES(hash_value), updated_at = NOW()`,
		workspaceID, filePath, hash,
	)
	if err != nil {
		return 0, fmt.Errorf("mysql.SymbolRepo.UpsertFileHash: %w", err)
	}

	var fileID int64
	err = r.db.QueryRowContext(ctx,
		"SELECT file_id FROM file_hashes WHERE workspace_id = ? AND file_path = ?",
		workspaceID, filePath,
	).Scan(&fileID)
	if err != nil {
		return 0, fmt.Errorf("mysql.SymbolRepo.UpsertFileHash: %w", err)
	}

	return fileID, nil
}

// GetFileHash returns the stored hash for a workspace file.
func (r *SymbolRepo) GetFileHash(ctx context.Context, workspaceID, filePath string) (string, error) {
	var hash string
	err := r.db.QueryRowContext(ctx,
		"SELECT hash_value FROM file_hashes WHERE workspace_id = ? AND file_path = ?",
		workspaceID, filePath,
	).Scan(&hash)
	if err != nil {
		return "", fmt.Errorf("mysql.SymbolRepo.GetFileHash: %w", err)
	}
	return hash, nil
}

// Upsert inserts or replaces a symbol record.
func (r *SymbolRepo) Upsert(ctx context.Context, sym *repo.Symbol) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT IGNORE INTO symbols (file_id, name, kind, start_line, end_line, signature, workspace_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sym.FileID, sym.Name, sym.Kind, sym.StartLine, sym.EndLine, sym.Signature, sym.WorkspaceID,
	)
	if err != nil {
		return fmt.Errorf("mysql.SymbolRepo.Upsert: %w", err)
	}
	return nil
}

// GetFileSymbols returns all symbols for a file identified by workspace and path.
func (r *SymbolRepo) GetFileSymbols(ctx context.Context, workspaceID, filePath string) ([]*repo.Symbol, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT s.id, s.file_id, s.name, s.kind, s.start_line, s.end_line, COALESCE(s.signature, ''), s.workspace_id
		 FROM symbols s
		 JOIN file_hashes fh ON s.file_id = fh.file_id
		 WHERE fh.workspace_id = ? AND fh.file_path = ?
		 ORDER BY s.start_line`,
		workspaceID, filePath,
	)
	if err != nil {
		return nil, fmt.Errorf("mysql.SymbolRepo.GetFileSymbols: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var symbols []*repo.Symbol
	for rows.Next() {
		sym := &repo.Symbol{}
		var id int64
		if err := rows.Scan(&id, &sym.FileID, &sym.Name, &sym.Kind, &sym.StartLine, &sym.EndLine, &sym.Signature, &sym.WorkspaceID); err != nil {
			return nil, fmt.Errorf("mysql.SymbolRepo.GetFileSymbols: %w", err)
		}
		sym.ID = strconv.FormatInt(id, 10)
		symbols = append(symbols, sym)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql.SymbolRepo.GetFileSymbols: %w", err)
	}
	return symbols, nil
}

// DeleteByWorkspacePath removes all symbols and the file hash record for a
// file identified by workspace ID and path. This is used when a file is
// deleted from disk and its index entries must be purged.
func (r *SymbolRepo) DeleteByWorkspacePath(ctx context.Context, workspaceID, filePath string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mysql.SymbolRepo.DeleteByWorkspacePath: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx,
		`DELETE FROM symbols WHERE file_id IN (SELECT file_id FROM file_hashes WHERE workspace_id = ? AND file_path = ?)`,
		workspaceID, filePath)
	if err != nil {
		return fmt.Errorf("mysql.SymbolRepo.DeleteByWorkspacePath: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		`DELETE FROM file_hashes WHERE workspace_id = ? AND file_path = ?`,
		workspaceID, filePath)
	if err != nil {
		return fmt.Errorf("mysql.SymbolRepo.DeleteByWorkspacePath: %w", err)
	}

	return tx.Commit()
}

// DeleteByFile removes all symbols associated with a given file_id.
func (r *SymbolRepo) DeleteByFile(ctx context.Context, fileID int64) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM symbols WHERE file_id = ?", fileID)
	if err != nil {
		return fmt.Errorf("mysql.SymbolRepo.DeleteByFile: %w", err)
	}
	return nil
}
