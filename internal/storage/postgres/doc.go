// Package postgres implements the PostgreSQL storage backend for Hyperax.
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
//  1. Create the migration file in migrations/ using PostgreSQL-native types:
//     - TIMESTAMPTZ instead of TEXT for timestamps
//     - BOOLEAN instead of INTEGER for booleans
//     - SERIAL instead of INTEGER PRIMARY KEY AUTOINCREMENT for auto-increment
//     - Use $1, $2, ... for parameter placeholders (not ?)
//
//  2. Create the repo file (e.g., pipeline_repo.go) following these conventions:
//     - Struct with single `db *sql.DB` field
//     - Use $N placeholders for all parameterized queries
//     - Scan BOOLEAN columns directly into Go bool (no int conversion needed)
//     - Scan TIMESTAMPTZ columns directly into time.Time (pgx handles this)
//     - Use NOW() instead of datetime('now') for current timestamps
//     - Use EXCLUDED.column in ON CONFLICT ... DO UPDATE (uppercase EXCLUDED)
//     - Use TRUE/FALSE instead of 1/0 for boolean literals in WHERE clauses
//
//  3. Wire the new repo in db.go's NewStore() method.
//
//  4. Add the corresponding table DDL to a new migration file.
//
// # Key Differences from SQLite
//
//   - Timestamps: PostgreSQL TIMESTAMPTZ maps directly to time.Time via pgx.
//     No need to parse strings with time.Parse("2006-01-02 15:04:05", ...).
//   - Booleans: PostgreSQL BOOLEAN maps directly to Go bool. No need for int
//     conversion (isActive == 1).
//   - Placeholders: Use $1, $2, ... instead of ?.
//   - UPSERT: Use ON CONFLICT ... DO UPDATE SET col = EXCLUDED.col.
//   - Auto-increment: Use SERIAL or BIGSERIAL instead of AUTOINCREMENT.
//   - NULL handling: COALESCE works identically in both databases.
//
// # Connection
//
// Uses the pgx driver (github.com/jackc/pgx/v5/stdlib) registered as "pgx"
// with database/sql. DSN format: "postgres://user:pass@host:5432/dbname?sslmode=disable"
package postgres
