package sqlite

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/repo"
)

func newAuditRepo(t *testing.T) (*AuditRepo, context.Context) {
	t.Helper()
	db, ctx := setupTestDB(t)
	return &AuditRepo{db: db.db}, ctx
}

// insertAuditItem is a test helper that inserts an audit item directly.
func insertAuditItem(t *testing.T, r *AuditRepo, ctx context.Context, auditID, itemType, filePath, symbolName, status string) string {
	t.Helper()
	id := uuid.New().String()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO audit_items (id, audit_id, item_type, file_path, symbol_name, status) VALUES (?, ?, ?, ?, ?, ?)`,
		id, auditID, itemType, filePath, symbolName, status,
	)
	if err != nil {
		t.Fatalf("insert audit item: %v", err)
	}
	return id
}

func TestAuditRepo_CreateAndList(t *testing.T) {
	r, ctx := newAuditRepo(t)

	audit := &repo.Audit{
		Name:             "code-review",
		WorkspaceName:    "ws1",
		Status:           "pending",
		AuditType:        "code",
		ScopeDescription: "Review all handlers",
	}

	id, err := r.CreateAudit(ctx, audit)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty id")
	}

	audits, err := r.ListAudits(ctx, "ws1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(audits) != 1 {
		t.Fatalf("expected 1 audit, got %d", len(audits))
	}

	got := audits[0]
	if got.Name != "code-review" {
		t.Errorf("name = %q, want %q", got.Name, "code-review")
	}
	if got.AuditType != "code" {
		t.Errorf("audit_type = %q, want %q", got.AuditType, "code")
	}
	if got.ScopeDescription != "Review all handlers" {
		t.Errorf("scope_description = %q", got.ScopeDescription)
	}
}

func TestAuditRepo_ListAudits_FiltersByWorkspace(t *testing.T) {
	r, ctx := newAuditRepo(t)

	_, _ = r.CreateAudit(ctx, &repo.Audit{Name: "a1", WorkspaceName: "ws1", Status: "pending", AuditType: "code"})
	_, _ = r.CreateAudit(ctx, &repo.Audit{Name: "a2", WorkspaceName: "ws1", Status: "pending", AuditType: "code"})
	_, _ = r.CreateAudit(ctx, &repo.Audit{Name: "a3", WorkspaceName: "ws2", Status: "pending", AuditType: "code"})

	list, err := r.ListAudits(ctx, "ws1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 audits for ws1, got %d", len(list))
	}
}

func TestAuditRepo_GetAuditItems(t *testing.T) {
	r, ctx := newAuditRepo(t)

	auditID, _ := r.CreateAudit(ctx, &repo.Audit{
		Name: "review", WorkspaceName: "ws1", Status: "pending", AuditType: "code",
	})

	insertAuditItem(t, r, ctx, auditID, "function", "main.go", "main", "pending")
	insertAuditItem(t, r, ctx, auditID, "function", "handler.go", "ServeHTTP", "pass")

	items, err := r.GetAuditItems(ctx, auditID)
	if err != nil {
		t.Fatalf("get items: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
}

func TestAuditRepo_UpdateAuditItem(t *testing.T) {
	r, ctx := newAuditRepo(t)

	auditID, _ := r.CreateAudit(ctx, &repo.Audit{
		Name: "review", WorkspaceName: "ws1", Status: "pending", AuditType: "code",
	})

	itemID := insertAuditItem(t, r, ctx, auditID, "function", "main.go", "main", "pending")

	if err := r.UpdateAuditItem(ctx, itemID, "pass", `{"note":"looks good"}`); err != nil {
		t.Fatalf("update: %v", err)
	}

	items, _ := r.GetAuditItems(ctx, auditID)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}

	item := items[0]
	if item.Status != "pass" {
		t.Errorf("status = %q, want %q", item.Status, "pass")
	}
	if item.Findings != `{"note":"looks good"}` {
		t.Errorf("findings = %q", item.Findings)
	}
	if item.ReviewedAt == nil {
		t.Error("expected reviewed_at to be set")
	}
}

func TestAuditRepo_UpdateAuditItem_NotFound(t *testing.T) {
	r, ctx := newAuditRepo(t)

	err := r.UpdateAuditItem(ctx, "nonexistent", "pass", "{}")
	if err == nil {
		t.Error("expected error for nonexistent item")
	}
}

func TestAuditRepo_GetAuditProgress(t *testing.T) {
	r, ctx := newAuditRepo(t)

	auditID, _ := r.CreateAudit(ctx, &repo.Audit{
		Name: "review", WorkspaceName: "ws1", Status: "pending", AuditType: "code",
	})

	insertAuditItem(t, r, ctx, auditID, "function", "a.go", "FuncA", "pending")
	insertAuditItem(t, r, ctx, auditID, "function", "b.go", "FuncB", "pass")
	insertAuditItem(t, r, ctx, auditID, "function", "c.go", "FuncC", "pass")
	insertAuditItem(t, r, ctx, auditID, "function", "d.go", "FuncD", "fail")
	insertAuditItem(t, r, ctx, auditID, "function", "e.go", "FuncE", "skip")

	progress, err := r.GetAuditProgress(ctx, auditID)
	if err != nil {
		t.Fatalf("get progress: %v", err)
	}

	if progress.Total != 5 {
		t.Errorf("total = %d, want 5", progress.Total)
	}
	if progress.Pending != 1 {
		t.Errorf("pending = %d, want 1", progress.Pending)
	}
	if progress.Pass != 2 {
		t.Errorf("pass = %d, want 2", progress.Pass)
	}
	if progress.Fail != 1 {
		t.Errorf("fail = %d, want 1", progress.Fail)
	}
	if progress.Skip != 1 {
		t.Errorf("skip = %d, want 1", progress.Skip)
	}
}

func TestAuditRepo_GetAuditProgress_Empty(t *testing.T) {
	r, ctx := newAuditRepo(t)

	auditID, _ := r.CreateAudit(ctx, &repo.Audit{
		Name: "empty-audit", WorkspaceName: "ws1", Status: "pending", AuditType: "code",
	})

	progress, err := r.GetAuditProgress(ctx, auditID)
	if err != nil {
		t.Fatalf("get progress: %v", err)
	}

	if progress.Total != 0 {
		t.Errorf("total = %d, want 0", progress.Total)
	}
}
