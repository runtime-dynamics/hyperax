package storage

import (
	"context"
	"database/sql"
	"embed"
	"testing"

	_ "modernc.org/sqlite"
)

//go:embed testdata/migrations/*.sql
var testMigrationsFS embed.FS

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMigrator_Up(t *testing.T) {
	db := openTestDB(t)
	m := NewMigrator(db, testMigrationsFS, "testdata/migrations", "sqlite", nil)

	ctx := context.Background()
	n, err := m.Up(ctx)
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 migrations applied, got %d", n)
	}

	// Verify tables were created.
	var name string
	err = db.QueryRowContext(ctx,
		"SELECT name FROM sqlite_master WHERE type='table' AND name='test_users'",
	).Scan(&name)
	if err != nil {
		t.Fatal("test_users table should exist after migration")
	}

	err = db.QueryRowContext(ctx,
		"SELECT name FROM sqlite_master WHERE type='table' AND name='test_posts'",
	).Scan(&name)
	if err != nil {
		t.Fatal("test_posts table should exist after migration")
	}
}

func TestMigrator_Up_Idempotent(t *testing.T) {
	db := openTestDB(t)
	m := NewMigrator(db, testMigrationsFS, "testdata/migrations", "sqlite", nil)

	ctx := context.Background()
	n1, _ := m.Up(ctx)
	n2, err := m.Up(ctx)
	if err != nil {
		t.Fatalf("second up: %v", err)
	}
	if n1 != 2 {
		t.Fatalf("first run: expected 2, got %d", n1)
	}
	if n2 != 0 {
		t.Fatalf("second run: expected 0 (already applied), got %d", n2)
	}
}

func TestMigrator_Version(t *testing.T) {
	db := openTestDB(t)
	m := NewMigrator(db, testMigrationsFS, "testdata/migrations", "sqlite", nil)

	ctx := context.Background()

	// Before any migrations.
	v0, _ := m.Version(ctx)
	if v0 != 0 {
		t.Fatalf("expected version 0 before migrations, got %d", v0)
	}

	m.Up(ctx)

	v, err := m.Version(ctx)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if v != 2 {
		t.Fatalf("expected version 2 after migrations, got %d", v)
	}
}

func TestMigrator_History(t *testing.T) {
	db := openTestDB(t)
	m := NewMigrator(db, testMigrationsFS, "testdata/migrations", "sqlite", nil)

	ctx := context.Background()
	m.Up(ctx)

	history, err := m.History(ctx)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 history records, got %d", len(history))
	}
	if history[0].Version != 1 || history[0].Name != "users" {
		t.Fatalf("unexpected first record: %+v", history[0])
	}
	if history[1].Version != 2 || history[1].Name != "posts" {
		t.Fatalf("unexpected second record: %+v", history[1])
	}
}

func TestMigrator_Pending(t *testing.T) {
	db := openTestDB(t)
	m := NewMigrator(db, testMigrationsFS, "testdata/migrations", "sqlite", nil)

	ctx := context.Background()

	pending, err := m.Pending(ctx)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(pending))
	}

	m.Up(ctx)

	pending2, _ := m.Pending(ctx)
	if len(pending2) != 0 {
		t.Fatalf("expected 0 pending after up, got %d", len(pending2))
	}
}

func TestParseMigrationFilename(t *testing.T) {
	cases := []struct {
		fname   string
		version int
		name    string
		ok      bool
	}{
		{"001_core.up.sql", 1, "core", true},
		{"018_agentmail.up.sql", 18, "agentmail", true},
		{"001_core.down.sql", 0, "", false},
		{"core.up.sql", 0, "", false},
		{"abc_core.up.sql", 0, "", false},
		{"", 0, "", false},
	}

	for _, tc := range cases {
		v, n, ok := parseMigrationFilename(tc.fname)
		if ok != tc.ok || v != tc.version || n != tc.name {
			t.Errorf("parseMigrationFilename(%q) = (%d, %q, %v), want (%d, %q, %v)",
				tc.fname, v, n, ok, tc.version, tc.name, tc.ok)
		}
	}
}

func TestSplitStatements(t *testing.T) {
	input := `CREATE TABLE a (id INT);
CREATE TABLE b (name TEXT);
INSERT INTO a VALUES (1);`

	stmts := splitStatements(input)
	if len(stmts) != 3 {
		t.Fatalf("expected 3 statements, got %d: %v", len(stmts), stmts)
	}
}

func TestSplitStatements_QuotedSemicolon(t *testing.T) {
	input := `INSERT INTO a (v) VALUES ('hello; world');
CREATE TABLE b (id INT);`

	stmts := splitStatements(input)
	if len(stmts) != 2 {
		t.Fatalf("expected 2 statements, got %d: %v", len(stmts), stmts)
	}
	if stmts[0] != "INSERT INTO a (v) VALUES ('hello; world')" {
		t.Fatalf("first statement incorrect: %q", stmts[0])
	}
}

func TestSplitStatements_Empty(t *testing.T) {
	stmts := splitStatements("")
	if len(stmts) != 0 {
		t.Fatalf("expected 0 statements, got %d", len(stmts))
	}
}

func TestTruncateSQL(t *testing.T) {
	short := "SELECT 1"
	if r := truncateSQL(short, 200); r != short {
		t.Fatalf("short should not truncate: %q", r)
	}

	long := "SELECT " + string(make([]byte, 300))
	if r := truncateSQL(long, 10); len(r) > 14 { // 10 + "..."
		t.Fatalf("long should truncate: len=%d", len(r))
	}
}
