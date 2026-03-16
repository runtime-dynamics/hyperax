// Package mysql implements the MySQL/MariaDB storage backend for Hyperax.
//
// # Implemented Repositories
//
// This package provides a representative subset of the 25 repository interfaces
// defined in internal/repo/:
//
//   - WorkspaceRepo  (workspace_repo.go)
//   - ConfigRepo     (config_repo.go)
//   - PersonaRepo    (persona_repo.go)
//   - SecretRepo     (secret_repo.go)
//   - ProviderRepo   (provider_repo.go)
//
// # Pattern for Adding Remaining Repos
//
// To implement a new repository (e.g., PipelineRepo):
//
//  1. Create the migration file in migrations/ using MySQL-native types:
//     - VARCHAR(255) for primary keys and indexed TEXT columns (MySQL cannot
//       index unbounded TEXT)
//     - DATETIME with ON UPDATE CURRENT_TIMESTAMP for auto-updated timestamps
//     - TINYINT(1) for booleans (the go-sql-driver scans these to bool)
//     - INT AUTO_INCREMENT for auto-incrementing integer keys
//     - Use backtick-quoted identifiers for reserved words (`key`, `value`)
//     - ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
//
//  2. Create the repo file (e.g., pipeline_repo.go) following these conventions:
//     - Struct with single `db *sql.DB` field
//     - Use ? placeholders for parameterized queries (same as SQLite)
//     - Scan TINYINT(1) directly into Go bool (go-sql-driver handles this)
//     - Scan DATETIME directly into time.Time (requires parseTime=true in DSN)
//     - Use NOW() instead of datetime('now') for current timestamps
//     - Use ON DUPLICATE KEY UPDATE instead of ON CONFLICT ... DO UPDATE
//     - Use VALUES(col) to reference the proposed insert value in ON DUPLICATE KEY
//     - Use backtick-quoted column names for reserved words: `key`, `value`
//     - MySQL UPDATE with ON UPDATE CURRENT_TIMESTAMP auto-sets updated_at,
//       so explicit updated_at = NOW() is optional in UPDATE statements
//
//  3. Wire the new repo in db.go's NewStore() method.
//
//  4. Add the corresponding table DDL to a new migration file.
//
// # Key Differences from SQLite
//
//   - Timestamps: MySQL DATETIME with parseTime=true DSN param maps directly
//     to time.Time via go-sql-driver. No need to parse strings.
//   - Booleans: MySQL TINYINT(1) maps to Go bool via go-sql-driver.
//   - Placeholders: Uses ? (same as SQLite), unlike PostgreSQL's $N.
//   - UPSERT: Uses ON DUPLICATE KEY UPDATE col = VALUES(col) instead of
//     ON CONFLICT ... DO UPDATE SET col = EXCLUDED.col.
//   - Primary keys: Must use VARCHAR(N) instead of TEXT for indexed columns.
//   - Reserved words: `key` and `value` must be backtick-quoted.
//   - Auto-increment: Uses AUTO_INCREMENT instead of AUTOINCREMENT.
//
// # Connection
//
// Uses the go-sql-driver/mysql driver registered as "mysql" with database/sql.
// DSN format: "user:pass@tcp(host:3306)/dbname?parseTime=true&charset=utf8mb4"
//
// IMPORTANT: The parseTime=true parameter is required for time.Time scanning.
package mysql
