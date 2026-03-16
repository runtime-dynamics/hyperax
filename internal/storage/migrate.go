package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"
)

// MigrationRecord tracks which migrations have been applied.
type MigrationRecord struct {
	Version   int
	Name      string
	AppliedAt time.Time
}

// Migrator provides a backend-agnostic migration runner.
// It reads numbered migration files from an embed.FS and applies
// them sequentially, tracking applied versions in a migration_history table.
//
// Migration files must follow the naming convention:
//
//	NNN_name.up.sql   (forward migration)
//	NNN_name.down.sql (optional rollback)
//
// The migrator supports SQLite, PostgreSQL, and MySQL through dialect-specific
// DDL for the tracking table.
type Migrator struct {
	db             *sql.DB
	migrations     embed.FS
	migrationsDir  string // subdirectory within the embed.FS (e.g. "migrations")
	dialect        string // "sqlite", "postgres", "mysql"
	logger         *slog.Logger
}

// NewMigrator creates a Migrator for the given database and migration source.
// dialect must be "sqlite", "postgres", or "mysql".
func NewMigrator(db *sql.DB, migrations embed.FS, migrationsDir, dialect string, logger *slog.Logger) *Migrator {
	if logger == nil {
		logger = slog.Default()
	}
	return &Migrator{
		db:            db,
		migrations:    migrations,
		migrationsDir: migrationsDir,
		dialect:       dialect,
		logger:        logger,
	}
}

// Up applies all pending migrations in version order.
// Returns the number of migrations applied and any error encountered.
func (m *Migrator) Up(ctx context.Context) (int, error) {
	if err := m.ensureTrackingTable(ctx); err != nil {
		return 0, fmt.Errorf("storage.Migrator.Up: %w", err)
	}

	applied, err := m.appliedVersions(ctx)
	if err != nil {
		return 0, fmt.Errorf("storage.Migrator.Up: %w", err)
	}

	pending, err := m.pendingMigrations(applied)
	if err != nil {
		return 0, fmt.Errorf("storage.Migrator.Up: %w", err)
	}

	if len(pending) == 0 {
		m.logger.Info("migrations up to date")
		return 0, nil
	}

	count := 0
	for _, mig := range pending {
		m.logger.Info("applying migration", "version", mig.version, "name", mig.name)

		if err := m.applyMigration(ctx, mig); err != nil {
			return count, fmt.Errorf("storage.Migrator.Up: migration %03d_%s: %w", mig.version, mig.name, err)
		}
		count++
	}

	m.logger.Info("migrations complete", "applied", count)
	return count, nil
}

// Version returns the current migration version (highest applied), or 0 if none.
func (m *Migrator) Version(ctx context.Context) (int, error) {
	if err := m.ensureTrackingTable(ctx); err != nil {
		return 0, fmt.Errorf("storage.Migrator.Version: %w", err)
	}

	var version int
	err := m.db.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(version), 0) FROM migration_history",
	).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("storage.Migrator.Version: %w", err)
	}
	return version, nil
}

// History returns all applied migration records in version order.
func (m *Migrator) History(ctx context.Context) ([]MigrationRecord, error) {
	if err := m.ensureTrackingTable(ctx); err != nil {
		return nil, fmt.Errorf("storage.Migrator.History: %w", err)
	}

	rows, err := m.db.QueryContext(ctx,
		"SELECT version, name, applied_at FROM migration_history ORDER BY version ASC",
	)
	if err != nil {
		return nil, fmt.Errorf("storage.Migrator.History: %w", err)
	}
	defer rows.Close()

	var records []MigrationRecord
	for rows.Next() {
		var r MigrationRecord
		var appliedAt string
		if err := rows.Scan(&r.Version, &r.Name, &appliedAt); err != nil {
			return nil, fmt.Errorf("storage.Migrator.History: %w", err)
		}
		r.AppliedAt, _ = time.Parse(time.RFC3339, appliedAt)
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage.Migrator.History: %w", err)
	}
	return records, nil
}

// Pending returns migration files that have not yet been applied.
func (m *Migrator) Pending(ctx context.Context) ([]string, error) {
	if err := m.ensureTrackingTable(ctx); err != nil {
		return nil, fmt.Errorf("storage.Migrator.Pending: %w", err)
	}

	applied, err := m.appliedVersions(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage.Migrator.Pending: %w", err)
	}

	pending, err := m.pendingMigrations(applied)
	if err != nil {
		return nil, fmt.Errorf("storage.Migrator.Pending: %w", err)
	}

	names := make([]string, len(pending))
	for i, mig := range pending {
		names[i] = fmt.Sprintf("%03d_%s", mig.version, mig.name)
	}
	return names, nil
}

// BootstrapExisting detects an existing database that was created before
// migration tracking was introduced. If the database has core tables (agents)
// but no migration_history records, all current migration files are recorded
// as already applied so future runs only execute genuinely new migrations.
//
// This is a one-time operation: once migration_history has records, the method
// is a no-op. Safe to call on fresh databases (no core tables → nothing seeded).
func (m *Migrator) BootstrapExisting(ctx context.Context) error {
	if err := m.ensureTrackingTable(ctx); err != nil {
		return fmt.Errorf("storage.Migrator.BootstrapExisting: %w", err)
	}

	// If migration_history already has records, nothing to do.
	var count int
	if err := m.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM migration_history",
	).Scan(&count); err != nil {
		return fmt.Errorf("storage.Migrator.BootstrapExisting: %w", err)
	}
	if count > 0 {
		return nil
	}

	// Detect an existing database by checking for a core table.
	hasCore, err := m.hasCoreTable(ctx)
	if err != nil {
		return fmt.Errorf("storage.Migrator.BootstrapExisting: %w", err)
	}
	if !hasCore {
		return nil // Fresh install — let Up() run all migrations.
	}

	// Existing database: seed all migration files as already applied.
	entries, err := fs.ReadDir(m.migrations, m.migrationsDir)
	if err != nil {
		return fmt.Errorf("storage.Migrator.BootstrapExisting: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	seeded := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		version, name, ok := parseMigrationFilename(entry.Name())
		if !ok {
			continue
		}

		// Use dialect-appropriate upsert to avoid duplicates.
		var insertSQL string
		switch m.dialect {
		case "sqlite":
			insertSQL = "INSERT OR IGNORE INTO migration_history (version, name, applied_at) VALUES (?, ?, ?)"
		case "postgres":
			insertSQL = "INSERT INTO migration_history (version, name, applied_at) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING"
		case "mysql":
			insertSQL = "INSERT IGNORE INTO migration_history (version, name, applied_at) VALUES (?, ?, ?)"
		default:
			insertSQL = "INSERT INTO migration_history (version, name, applied_at) VALUES (?, ?, ?)"
		}

		if _, err := m.db.ExecContext(ctx, insertSQL, version, name, now); err != nil {
			m.logger.Warn("failed to seed migration record", "version", version, "error", err)
			continue
		}
		seeded++
	}

	m.logger.Info("bootstrapped migration history for existing database", "seeded", seeded)
	return nil
}

// hasCoreTable checks if the database already has core application tables.
func (m *Migrator) hasCoreTable(ctx context.Context) (bool, error) {
	var query string
	switch m.dialect {
	case "sqlite":
		query = "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='agents'"
	case "postgres":
		query = "SELECT COUNT(*) FROM information_schema.tables WHERE table_name='agents' AND table_schema='public'"
	case "mysql":
		query = "SELECT COUNT(*) FROM information_schema.tables WHERE table_name='agents' AND table_schema=DATABASE()"
	default:
		return false, fmt.Errorf("unsupported dialect: %s", m.dialect)
	}

	var count int
	if err := m.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return false, fmt.Errorf("storage.Migrator.hasCoreTable: %w", err)
	}
	return count > 0, nil
}

// migration represents a single migration file.
type migration struct {
	version  int
	name     string
	filename string
}

// ensureTrackingTable creates the migration_history table if it doesn't exist.
func (m *Migrator) ensureTrackingTable(ctx context.Context) error {
	var ddl string
	switch m.dialect {
	case "sqlite":
		ddl = `CREATE TABLE IF NOT EXISTS migration_history (
			version    INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			applied_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`
	case "postgres":
		ddl = `CREATE TABLE IF NOT EXISTS migration_history (
			version    INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`
	case "mysql":
		ddl = `CREATE TABLE IF NOT EXISTS migration_history (
			version    INT PRIMARY KEY,
			name       VARCHAR(255) NOT NULL,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`
	default:
		return fmt.Errorf("unsupported dialect: %s", m.dialect)
	}

	_, err := m.db.ExecContext(ctx, ddl)
	if err != nil {
		return fmt.Errorf("storage.Migrator.ensureTrackingTable: %w", err)
	}
	return nil
}

// appliedVersions returns a set of already-applied migration versions.
func (m *Migrator) appliedVersions(ctx context.Context) (map[int]bool, error) {
	rows, err := m.db.QueryContext(ctx, "SELECT version FROM migration_history")
	if err != nil {
		return nil, fmt.Errorf("storage.Migrator.appliedVersions: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("storage.Migrator.appliedVersions: %w", err)
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage.Migrator.appliedVersions: %w", err)
	}
	return applied, nil
}

// pendingMigrations reads the migrations directory and returns files not yet applied.
func (m *Migrator) pendingMigrations(applied map[int]bool) ([]migration, error) {
	entries, err := fs.ReadDir(m.migrations, m.migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("storage.Migrator.pendingMigrations: %w", err)
	}

	var pending []migration
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fname := entry.Name()

		// Only process .up.sql files.
		if !strings.HasSuffix(fname, ".up.sql") {
			continue
		}

		version, name, ok := parseMigrationFilename(fname)
		if !ok {
			continue
		}

		if applied[version] {
			continue
		}

		pending = append(pending, migration{
			version:  version,
			name:     name,
			filename: fname,
		})
	}

	// Sort by version ascending.
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].version < pending[j].version
	})

	return pending, nil
}

// applyMigration reads and executes a migration file, then records it.
func (m *Migrator) applyMigration(ctx context.Context, mig migration) error {
	path := m.migrationsDir + "/" + mig.filename
	data, err := m.migrations.ReadFile(path)
	if err != nil {
		return fmt.Errorf("storage.Migrator.applyMigration: %w", err)
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage.Migrator.applyMigration: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Execute migration statements.
	// Split on semicolons to handle multi-statement migrations.
	for _, stmt := range splitStatements(string(data)) {
		stmt = stripLeadingSQLComments(stmt)
		if stmt == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			// For SQLite, ignore idempotent errors.
			if m.dialect == "sqlite" && isIdempotentError(err) {
				continue
			}
			return fmt.Errorf("storage.Migrator.applyMigration: %w\n  SQL: %s", err, truncateSQL(stmt, 200))
		}
	}

	// Record the migration.
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = tx.ExecContext(ctx,
		"INSERT INTO migration_history (version, name, applied_at) VALUES (?, ?, ?)",
		mig.version, mig.name, now,
	)
	if err != nil {
		return fmt.Errorf("storage.Migrator.applyMigration: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage.Migrator.applyMigration: %w", err)
	}
	return nil
}

// parseMigrationFilename extracts version and name from "NNN_name.up.sql".
func parseMigrationFilename(fname string) (version int, name string, ok bool) {
	// Strip ".up.sql" suffix.
	base := strings.TrimSuffix(fname, ".up.sql")
	if base == fname {
		return 0, "", false
	}

	// Split on first underscore.
	idx := strings.Index(base, "_")
	if idx < 1 {
		return 0, "", false
	}

	versionStr := base[:idx]
	name = base[idx+1:]

	// Parse version number.
	v := 0
	for _, c := range versionStr {
		if c < '0' || c > '9' {
			return 0, "", false
		}
		v = v*10 + int(c-'0')
	}

	if v == 0 {
		return 0, "", false
	}

	return v, name, true
}

// splitStatements splits SQL text on semicolons, respecting quoted strings
// and SQLite BEGIN...END trigger bodies (which contain embedded semicolons).
func splitStatements(sql string) []string {
	var stmts []string
	var current strings.Builder
	inSingleQuote := false
	inDoubleQuote := false
	depth := 0 // BEGIN...END nesting depth (trigger bodies)

	for i := 0; i < len(sql); i++ {
		c := sql[i]

		if c == '\'' && !inDoubleQuote {
			inSingleQuote = !inSingleQuote
		} else if c == '"' && !inSingleQuote {
			inDoubleQuote = !inDoubleQuote
		}

		if !inSingleQuote && !inDoubleQuote {
			// Track BEGIN...END blocks (trigger bodies).
			if matchSQLKeyword(sql, i, "BEGIN") {
				depth++
			} else if depth > 0 && matchSQLKeyword(sql, i, "END") {
				depth--
			}
		}

		if c == ';' && !inSingleQuote && !inDoubleQuote && depth == 0 {
			stmt := strings.TrimSpace(current.String())
			if stmt != "" {
				stmts = append(stmts, stmt)
			}
			current.Reset()
			continue
		}

		current.WriteByte(c)
	}

	// Don't forget trailing content without a semicolon.
	if trailing := strings.TrimSpace(current.String()); trailing != "" {
		stmts = append(stmts, trailing)
	}

	return stmts
}

// matchSQLKeyword checks if keyword appears at position i as a whole word
// (bounded by non-alphanumeric characters or string boundaries).
func matchSQLKeyword(sql string, i int, keyword string) bool {
	if i+len(keyword) > len(sql) {
		return false
	}
	if !strings.EqualFold(sql[i:i+len(keyword)], keyword) {
		return false
	}
	// Must be preceded by non-alphanumeric or start-of-string.
	if i > 0 && isAlphaNumOrUnderscore(sql[i-1]) {
		return false
	}
	// Must be followed by non-alphanumeric or end-of-string.
	end := i + len(keyword)
	if end < len(sql) && isAlphaNumOrUnderscore(sql[end]) {
		return false
	}
	return true
}

func isAlphaNumOrUnderscore(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

// stripLeadingSQLComments removes leading SQL line comments (-- ...) and blank
// lines from a statement, then trims whitespace. This prevents multi-line
// statements that start with a comment (e.g., "-- description\nCREATE TABLE...")
// from being incorrectly skipped by a naive HasPrefix("--") check.
func stripLeadingSQLComments(stmt string) string {
	lines := strings.Split(stmt, "\n")
	start := 0
	for start < len(lines) {
		trimmed := strings.TrimSpace(lines[start])
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			start++
			continue
		}
		break
	}
	if start >= len(lines) {
		return ""
	}
	return strings.TrimSpace(strings.Join(lines[start:], "\n"))
}

// isIdempotentError returns true for SQLite errors that indicate a migration
// was partially applied (e.g., "table already exists", "duplicate column").
func isIdempotentError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "duplicate column")
}

// truncateSQL shortens a SQL string for error messages.
func truncateSQL(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
