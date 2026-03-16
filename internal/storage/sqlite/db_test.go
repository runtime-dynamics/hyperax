package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hyperax/hyperax/pkg/types"
)

func setupTestDB(t *testing.T) (*DB, context.Context) {
	t.Helper()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "test.db")

	db, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return db, ctx
}

func TestOpen(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "test.db")

	db, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	// DB file should exist
	if _, err := os.Stat(dsn); os.IsNotExist(err) {
		t.Error("database file not created")
	}
}

func TestOpen_InvalidPath(t *testing.T) {
	_, err := Open("/nonexistent/path/test.db")
	// modernc sqlite creates parent dirs, but ping should work or fail depending on FS
	// Just verify we get a DB or an error (not a panic)
	if err != nil {
		return // expected for invalid paths
	}
}

func TestMigrate(t *testing.T) {
	db, ctx := setupTestDB(t)

	// Verify tables exist by querying them
	tables := []string{"workspaces", "config_keys", "config_values", "file_hashes", "symbols", "pipelines", "project_plans", "personas", "audits"}
	for _, table := range tables {
		var count int
		err := db.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count)
		if err != nil {
			t.Errorf("table %s not created: %v", table, err)
		}
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	db, ctx := setupTestDB(t)

	// Run migrate again — should not error (IF NOT EXISTS)
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("second migrate should be idempotent: %v", err)
	}
	_ = ctx
}

func TestNewStore(t *testing.T) {
	db, _ := setupTestDB(t)
	store := db.NewStore()

	if store.Workspaces == nil {
		t.Error("Workspaces repo not wired")
	}
	if store.Config == nil {
		t.Error("Config repo not wired")
	}
}

// --- WorkspaceRepo Tests ---

func TestWorkspaceRepo_CreateAndGet(t *testing.T) {
	db, ctx := setupTestDB(t)
	repo := &WorkspaceRepo{db: db.db}

	ws := &types.WorkspaceInfo{
		ID:       "ws-1",
		Name:     "test-workspace",
		RootPath: "/tmp/test",
	}

	if err := repo.CreateWorkspace(ctx, ws); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.GetWorkspace(ctx, "test-workspace")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.ID != "ws-1" {
		t.Errorf("id = %q, want %q", got.ID, "ws-1")
	}
	if got.Name != "test-workspace" {
		t.Errorf("name = %q, want %q", got.Name, "test-workspace")
	}
	if got.RootPath != "/tmp/test" {
		t.Errorf("root_path = %q, want %q", got.RootPath, "/tmp/test")
	}
}

func TestWorkspaceRepo_ListWorkspaces(t *testing.T) {
	db, ctx := setupTestDB(t)
	repo := &WorkspaceRepo{db: db.db}

	// Empty initially
	list, err := repo.ListWorkspaces(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0, got %d", len(list))
	}

	// Add two workspaces
	if err := repo.CreateWorkspace(ctx, &types.WorkspaceInfo{ID: "a", Name: "alpha", RootPath: "/a"}); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if err := repo.CreateWorkspace(ctx, &types.WorkspaceInfo{ID: "b", Name: "beta", RootPath: "/b"}); err != nil {
		t.Fatalf("create b: %v", err)
	}

	list, err = repo.ListWorkspaces(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2, got %d", len(list))
	}
}

func TestWorkspaceRepo_Exists(t *testing.T) {
	db, ctx := setupTestDB(t)
	repo := &WorkspaceRepo{db: db.db}

	exists, err := repo.WorkspaceExists(ctx, "nope")
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if exists {
		t.Error("should not exist")
	}

	if err := repo.CreateWorkspace(ctx, &types.WorkspaceInfo{ID: "ws-1", Name: "exists-test", RootPath: "/x"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	exists, err = repo.WorkspaceExists(ctx, "exists-test")
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if !exists {
		t.Error("should exist")
	}
}

func TestWorkspaceRepo_GetNotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	repo := &WorkspaceRepo{db: db.db}

	_, err := repo.GetWorkspace(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent workspace")
	}
}

func TestWorkspaceRepo_DuplicateName(t *testing.T) {
	db, ctx := setupTestDB(t)
	repo := &WorkspaceRepo{db: db.db}

	ws := &types.WorkspaceInfo{ID: "ws-1", Name: "dup", RootPath: "/a"}
	if err := repo.CreateWorkspace(ctx, ws); err != nil {
		t.Fatalf("create first: %v", err)
	}

	// Re-registering the same name should upsert (update ID and root_path).
	ws2 := &types.WorkspaceInfo{ID: "ws-2", Name: "dup", RootPath: "/b"}
	if err := repo.CreateWorkspace(ctx, ws2); err != nil {
		t.Fatalf("upsert should not error: %v", err)
	}

	got, err := repo.GetWorkspace(ctx, "dup")
	if err != nil {
		t.Fatalf("get after upsert: %v", err)
	}
	if got.ID != "ws-2" {
		t.Errorf("expected ID ws-2 after upsert, got %s", got.ID)
	}
	if got.RootPath != "/b" {
		t.Errorf("expected root_path /b after upsert, got %s", got.RootPath)
	}
}

// --- ConfigRepo Tests ---

func TestConfigRepo_UpsertAndGetKeyMeta(t *testing.T) {
	db, ctx := setupTestDB(t)
	repo := &ConfigRepo{db: db.db}

	meta := &types.ConfigKeyMeta{
		Key:         "test.key",
		ScopeType:   "global",
		ValueType:   "string",
		DefaultVal:  "default",
		Critical:    false,
		Description: "A test key",
	}

	if err := repo.UpsertKeyMeta(ctx, meta); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := repo.GetKeyMeta(ctx, "test.key")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.Key != "test.key" {
		t.Errorf("key = %q", got.Key)
	}
	if got.DefaultVal != "default" {
		t.Errorf("default_val = %q", got.DefaultVal)
	}
	if got.Description != "A test key" {
		t.Errorf("description = %q", got.Description)
	}
}

func TestConfigRepo_SetAndGetValue(t *testing.T) {
	db, ctx := setupTestDB(t)
	repo := &ConfigRepo{db: db.db}

	// Must create key first
	if err := repo.UpsertKeyMeta(ctx, &types.ConfigKeyMeta{Key: "app.name", ScopeType: "global", ValueType: "string"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	scope := types.ConfigScope{Type: "global", ID: ""}
	if err := repo.SetValue(ctx, "app.name", "hyperax", scope, "test"); err != nil {
		t.Fatalf("set: %v", err)
	}

	val, err := repo.GetValue(ctx, "app.name", scope)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if val != "hyperax" {
		t.Errorf("value = %q, want %q", val, "hyperax")
	}
}

func TestConfigRepo_SetValue_Upsert(t *testing.T) {
	db, ctx := setupTestDB(t)
	repo := &ConfigRepo{db: db.db}

	if err := repo.UpsertKeyMeta(ctx, &types.ConfigKeyMeta{Key: "x", ScopeType: "global", ValueType: "string"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	scope := types.ConfigScope{Type: "global", ID: ""}
	if err := repo.SetValue(ctx, "x", "first", scope, "test"); err != nil {
		t.Fatalf("set first: %v", err)
	}
	if err := repo.SetValue(ctx, "x", "second", scope, "test"); err != nil {
		t.Fatalf("set second: %v", err)
	}

	val, _ := repo.GetValue(ctx, "x", scope)
	if val != "second" {
		t.Errorf("value = %q, want %q", val, "second")
	}
}

func TestConfigRepo_ListKeys(t *testing.T) {
	db, ctx := setupTestDB(t)
	repo := &ConfigRepo{db: db.db}

	if err := repo.UpsertKeyMeta(ctx, &types.ConfigKeyMeta{Key: "a.key", ScopeType: "global", ValueType: "string"}); err != nil {
		t.Fatalf("upsert a: %v", err)
	}
	if err := repo.UpsertKeyMeta(ctx, &types.ConfigKeyMeta{Key: "b.key", ScopeType: "workspace", ValueType: "int"}); err != nil {
		t.Fatalf("upsert b: %v", err)
	}

	keys, err := repo.ListKeys(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(keys))
	}
}

func TestConfigRepo_ListValues(t *testing.T) {
	db, ctx := setupTestDB(t)
	repo := &ConfigRepo{db: db.db}

	if err := repo.UpsertKeyMeta(ctx, &types.ConfigKeyMeta{Key: "k1", ScopeType: "global", ValueType: "string"}); err != nil {
		t.Fatalf("upsert k1: %v", err)
	}
	if err := repo.UpsertKeyMeta(ctx, &types.ConfigKeyMeta{Key: "k2", ScopeType: "global", ValueType: "string"}); err != nil {
		t.Fatalf("upsert k2: %v", err)
	}

	scope := types.ConfigScope{Type: "global", ID: ""}
	if err := repo.SetValue(ctx, "k1", "v1", scope, "test"); err != nil {
		t.Fatalf("set k1: %v", err)
	}
	if err := repo.SetValue(ctx, "k2", "v2", scope, "test"); err != nil {
		t.Fatalf("set k2: %v", err)
	}

	values, err := repo.ListValues(ctx, scope)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(values) != 2 {
		t.Errorf("expected 2, got %d", len(values))
	}
}

func TestConfigRepo_ScopedValues(t *testing.T) {
	db, ctx := setupTestDB(t)
	repo := &ConfigRepo{db: db.db}

	if err := repo.UpsertKeyMeta(ctx, &types.ConfigKeyMeta{Key: "theme", ScopeType: "agent", ValueType: "string"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	global := types.ConfigScope{Type: "global", ID: ""}
	agent := types.ConfigScope{Type: "agent", ID: "agent-1"}

	if err := repo.SetValue(ctx, "theme", "dark", global, "test"); err != nil {
		t.Fatalf("set global: %v", err)
	}
	if err := repo.SetValue(ctx, "theme", "light", agent, "test"); err != nil {
		t.Fatalf("set agent: %v", err)
	}

	// Global should return "dark"
	val, _ := repo.GetValue(ctx, "theme", global)
	if val != "dark" {
		t.Errorf("global = %q, want dark", val)
	}

	// Agent should return "light"
	val, _ = repo.GetValue(ctx, "theme", agent)
	if val != "light" {
		t.Errorf("agent = %q, want light", val)
	}
}

func TestConfigRepo_CriticalKey(t *testing.T) {
	db, ctx := setupTestDB(t)
	repo := &ConfigRepo{db: db.db}

	if err := repo.UpsertKeyMeta(ctx, &types.ConfigKeyMeta{
		Key:      "dangerous",
		Critical: true,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	meta, err := repo.GetKeyMeta(ctx, "dangerous")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !meta.Critical {
		t.Error("expected critical=true")
	}
}
