package repo

import (
	"context"
	"time"
)

// Audit is a top-level audit definition.
type Audit struct {
	ID               string
	Name             string
	WorkspaceName    string
	ProjectName      string
	Status           string
	AuditType        string
	ScopeDescription string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// AuditItem is an individual item to be reviewed.
type AuditItem struct {
	ID          string
	AuditID     string
	ItemType    string
	FilePath    string
	SymbolName  string
	Status      string
	ContextData string
	Findings    string
	ReviewedAt  *time.Time
}

// AuditProgress summarizes audit completion status.
type AuditProgress struct {
	Total   int
	Pending int
	Pass    int
	Fail    int
	Skip    int
}

// AuditRepo handles audit definitions and items.
type AuditRepo interface {
	CreateAudit(ctx context.Context, audit *Audit) (string, error)
	ListAudits(ctx context.Context, workspaceName string) ([]*Audit, error)
	GetAuditItem(ctx context.Context, itemID string) (*AuditItem, error)
	GetAuditItems(ctx context.Context, auditID string) ([]*AuditItem, error)
	UpdateAuditItem(ctx context.Context, id string, status string, findings string) error
	GetAuditProgress(ctx context.Context, auditID string) (*AuditProgress, error)
}
