package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/repo"
)

// AuditRepo implements repo.AuditRepo for SQLite.
type AuditRepo struct {
	db *sql.DB
}

// CreateAudit inserts a new audit definition and returns its generated ID.
func (r *AuditRepo) CreateAudit(ctx context.Context, audit *repo.Audit) (string, error) {
	if audit.ID == "" {
		audit.ID = uuid.New().String()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO audits (id, name, workspace_name, project_name, status, audit_type, scope_description)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		audit.ID, audit.Name, audit.WorkspaceName, audit.ProjectName,
		audit.Status, audit.AuditType, audit.ScopeDescription,
	)
	if err != nil {
		return "", fmt.Errorf("sqlite.AuditRepo.CreateAudit: %w", err)
	}
	return audit.ID, nil
}

// ListAudits returns all audits for a given workspace, ordered by creation time descending.
func (r *AuditRepo) ListAudits(ctx context.Context, workspaceName string) ([]*repo.Audit, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, workspace_name, COALESCE(project_name, ''), status, audit_type,
		        COALESCE(scope_description, ''), created_at, updated_at
		 FROM audits WHERE workspace_name = ? ORDER BY created_at DESC`, workspaceName,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.AuditRepo.ListAudits: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var audits []*repo.Audit
	for rows.Next() {
		a := &repo.Audit{}
		var createdAt, updatedAt string
		var projectName, scopeDesc sql.NullString

		if err := rows.Scan(
			&a.ID, &a.Name, &a.WorkspaceName, &projectName, &a.Status, &a.AuditType,
			&scopeDesc, &createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("sqlite.AuditRepo.ListAudits: %w", err)
		}

		a.ProjectName = projectName.String
		a.ScopeDescription = scopeDesc.String
		var parseErr error
		if a.CreatedAt, parseErr = parseSQLiteTime(createdAt, "sqlite.AuditRepo.ListAudits"); parseErr != nil {
			return nil, parseErr
		}
		if a.UpdatedAt, parseErr = parseSQLiteTime(updatedAt, "sqlite.AuditRepo.ListAudits"); parseErr != nil {
			return nil, parseErr
		}
		audits = append(audits, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.AuditRepo.ListAudits: %w", err)
	}
	return audits, nil
}

// GetAuditItem retrieves a single audit item by its ID, including all metadata.
func (r *AuditRepo) GetAuditItem(ctx context.Context, itemID string) (*repo.AuditItem, error) {
	item := &repo.AuditItem{}
	var filePath, symbolName sql.NullString
	var reviewedAt sql.NullString

	err := r.db.QueryRowContext(ctx,
		`SELECT id, audit_id, item_type, COALESCE(file_path, ''), COALESCE(symbol_name, ''),
		        status, context_data, findings, reviewed_at
		 FROM audit_items WHERE id = ?`, itemID,
	).Scan(
		&item.ID, &item.AuditID, &item.ItemType, &filePath, &symbolName,
		&item.Status, &item.ContextData, &item.Findings, &reviewedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("audit item %q not found", itemID)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite.AuditRepo.GetAuditItem: %w", err)
	}

	item.FilePath = filePath.String
	item.SymbolName = symbolName.String
	if reviewedAt.Valid {
		t, parseErr := parseSQLiteTime(reviewedAt.String, "sqlite.AuditRepo.GetAuditItem.reviewedAt")
		if parseErr != nil {
			return nil, parseErr
		}
		item.ReviewedAt = &t
	}

	return item, nil
}

// GetAuditItems returns all items belonging to a given audit.
func (r *AuditRepo) GetAuditItems(ctx context.Context, auditID string) ([]*repo.AuditItem, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, audit_id, item_type, COALESCE(file_path, ''), COALESCE(symbol_name, ''),
		        status, context_data, findings, reviewed_at
		 FROM audit_items WHERE audit_id = ? ORDER BY item_type, file_path`, auditID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.AuditRepo.GetAuditItems: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []*repo.AuditItem
	for rows.Next() {
		item := &repo.AuditItem{}
		var filePath, symbolName sql.NullString
		var reviewedAt sql.NullString

		if err := rows.Scan(
			&item.ID, &item.AuditID, &item.ItemType, &filePath, &symbolName,
			&item.Status, &item.ContextData, &item.Findings, &reviewedAt,
		); err != nil {
			return nil, fmt.Errorf("sqlite.AuditRepo.GetAuditItems: %w", err)
		}

		item.FilePath = filePath.String
		item.SymbolName = symbolName.String
		if reviewedAt.Valid {
			t, parseErr := parseSQLiteTime(reviewedAt.String, "sqlite.AuditRepo.GetAuditItems.reviewedAt")
			if parseErr != nil {
				return nil, parseErr
			}
			item.ReviewedAt = &t
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.AuditRepo.GetAuditItems: %w", err)
	}
	return items, nil
}

// UpdateAuditItem sets the status, findings, and reviewed_at timestamp for an audit item.
func (r *AuditRepo) UpdateAuditItem(ctx context.Context, id string, status string, findings string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE audit_items SET status = ?, findings = ?, reviewed_at = datetime('now') WHERE id = ?`,
		status, findings, id,
	)
	if err != nil {
		return fmt.Errorf("sqlite.AuditRepo.UpdateAuditItem: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite.AuditRepo.UpdateAuditItem: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("audit item %q not found", id)
	}
	return nil
}

// GetAuditProgress computes the completion summary for an audit by counting items per status.
func (r *AuditRepo) GetAuditProgress(ctx context.Context, auditID string) (*repo.AuditProgress, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT status, COUNT(*) FROM audit_items WHERE audit_id = ? GROUP BY status`, auditID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite.AuditRepo.GetAuditProgress: %w", err)
	}
	defer func() { _ = rows.Close() }()

	progress := &repo.AuditProgress{}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("sqlite.AuditRepo.GetAuditProgress: %w", err)
		}

		progress.Total += count
		switch status {
		case "pending":
			progress.Pending += count
		case "pass":
			progress.Pass += count
		case "fail":
			progress.Fail += count
		case "skip":
			progress.Skip += count
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite.AuditRepo.GetAuditProgress: %w", err)
	}
	return progress, nil
}
