package postgres

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"

	"github.com/hyperax/hyperax/internal/storage"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB wraps a PostgreSQL database connection.
type DB struct {
	db     *sql.DB
	logger *slog.Logger
}

// Open creates a new PostgreSQL database connection.
// DSN format: "postgres://user:pass@host:5432/dbname?sslmode=disable"
func Open(dsn string, logger *slog.Logger) (*DB, error) {
	if logger == nil {
		logger = slog.Default()
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres.Open: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("postgres.Open: %w", err)
	}

	return &DB{db: db, logger: logger}, nil
}

// Migrate runs all pending database migrations using the embedded migration files.
func (d *DB) Migrate(ctx context.Context) error {
	migrator := storage.NewMigrator(d.db, migrationsFS, "migrations", "postgres", d.logger)
	n, err := migrator.Up(ctx)
	if err != nil {
		return fmt.Errorf("postgres.DB.Migrate: %w", err)
	}
	if n > 0 {
		d.logger.Info("postgres migrations applied", "count", n)
	}
	return nil
}

// NewStore creates a fully-wired Store from the PostgreSQL database.
func (d *DB) NewStore() *storage.Store {
	wsRepo := &WorkspaceRepo{db: d.db}
	return &storage.Store{
		Workspaces:    wsRepo,
		Config:        &ConfigRepo{db: d.db},
		Symbols:       &SymbolRepo{db: d.db},
		Search:        &SearchRepo{db: d.db},
		Projects:      &ProjectRepo{db: d.db},
		Pipelines:     &PipelineRepo{db: d.db},
		Audits:        &AuditRepo{db: d.db},
		Git:           NewGitRepo(wsRepo),
		Metrics:       &MetricsRepo{db: d.db},
		Interjections: &InterjectionRepo{db: d.db},
		Memory:        &MemoryRepo{db: d.db},
		Lifecycle:     &LifecycleRepo{db: d.db},
		Secrets:       &SecretRepo{db: d.db},
		Budgets:       &BudgetRepo{db: d.db},
		Cron:          &CronRepo{db: d.db},
		Workflows:     &WorkflowRepo{db: d.db},
		Nervous:       &NervousRepo{db: d.db},
		CommHub:       &CommHubRepo{db: d.db},
		Telemetry:     &TelemetryRepo{db: d.db},
		Providers:     &ProviderRepo{db: d.db},
		Checkpoints:   &CheckpointRepo{db: d.db},
		Vectors:       &VectorRepo{db: d.db},
		MCPTokens:     &TokenRepo{db: d.db},
		Delegations:   &DelegationRepo{db: d.db},
		AgentMail:     &AgentMailRepo{db: d.db},
		Sessions:      &SessionRepo{db: d.db},
		WorkQueue:     &WorkQueueRepo{db: d.db},
		Closer:        d,
	}
}

// Close closes the database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// SqlDB returns the underlying sql.DB for direct queries.
func (d *DB) SqlDB() *sql.DB {
	return d.db
}
