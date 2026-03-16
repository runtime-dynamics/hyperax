package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"

	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/internal/storage"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB wraps a SQLite database connection.
type DB struct {
	db *sql.DB
}

// Open creates a new SQLite database connection with WAL mode and foreign keys enabled.
func Open(dsn string) (*DB, error) {
	connStr := fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)", dsn)

	db, err := sql.Open("sqlite", connStr)
	if err != nil {
		return nil, fmt.Errorf("sqlite.Open: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	// Verify connection
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite.Open: %w", err)
	}

	return &DB{db: db}, nil
}

// Migrate runs all pending database migrations using the generic Migrator.
// On first run after upgrading from the hardcoded migration approach, it
// bootstraps the migration_history table by recording all existing migrations
// as already applied, then only runs genuinely new migrations going forward.
func (d *DB) Migrate(ctx context.Context) error {
	logger := slog.Default()
	migrator := storage.NewMigrator(d.db, migrationsFS, "migrations", "sqlite", logger)

	// Bootstrap: for existing databases that predate migration tracking,
	// seed all current migration versions as already applied.
	if err := migrator.BootstrapExisting(ctx); err != nil {
		return fmt.Errorf("sqlite.DB.Migrate: %w", err)
	}

	n, err := migrator.Up(ctx)
	if err != nil {
		return fmt.Errorf("sqlite.DB.Migrate: %w", err)
	}
	if n > 0 {
		logger.Info("sqlite migrations applied", "count", n)
	}
	return nil
}

// Close closes the database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// NewStore creates a fully-wired Store from the SQLite database.
func (d *DB) NewStore() *storage.Store {
	wsRepo := &WorkspaceRepo{db: d.db}
	return &storage.Store{
		Workspaces:    wsRepo,
		Config:        &ConfigRepo{db: d.db},
		Symbols:       &SymbolRepo{db: d.db},
		Search:        &SearchRepo{db: d.db},
		Projects:      &ProjectRepo{db: d.db},
		Pipelines:     &PipelineRepo{db: d.db},
		Agents:        &AgentRepo{db: d.db},
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
		Plugins:       &PluginRepo{db: d.db},
		ExternalDocs:  NewExternalDocRepoSQLite(d.db),
		Sessions:      &SessionRepo{db: d.db},
		WorkQueue:     &WorkQueueRepo{db: d.db},
		Specs:  &SpecRepo{db: d.db},
		Closer: d,
	}
}

// DB returns the underlying sql.DB for direct queries when needed.
func (d *DB) SqlDB() *sql.DB {
	return d.db
}

// WorkspaceRepo returns the SQLite workspace repository.
func (d *DB) WorkspaceRepo() repo.WorkspaceRepo {
	return &WorkspaceRepo{db: d.db}
}

// ConfigRepo returns the SQLite config repository.
func (d *DB) ConfigRepo() repo.ConfigRepo {
	return &ConfigRepo{db: d.db}
}
