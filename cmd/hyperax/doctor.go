package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hyperax/hyperax/internal/config"
	"github.com/hyperax/hyperax/internal/storage/sqlite"
	"github.com/spf13/cobra"
)

// checkResult represents the outcome of a single health check.
type checkResult struct {
	Name   string // short check name
	Status string // "pass", "fail", "warn"
	Detail string // human-readable detail
}

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run health checks on the Hyperax installation",
		Long:  "Validates configuration, database integrity, migration state, workspace existence, and system health.",
		RunE: func(cmd *cobra.Command, args []string) error {
			results := runDoctorChecks()
			printResults(results)

			// Exit 1 if any check failed.
			for _, r := range results {
				if r.Status == "fail" {
					return fmt.Errorf("one or more checks failed")
				}
			}
			return nil
		},
	}
}

// runDoctorChecks executes all health checks and returns results.
func runDoctorChecks() []checkResult {
	var results []checkResult

	// 1. Bootstrap config
	bootstrap, err := config.LoadBootstrap()
	if err != nil {
		results = append(results, checkResult{
			Name:   "Configuration",
			Status: "fail",
			Detail: fmt.Sprintf("Failed to load bootstrap config: %v", err),
		})
		return results // Can't continue without config.
	}

	if valErr := bootstrap.Validate(); valErr != nil {
		results = append(results, checkResult{
			Name:   "Configuration",
			Status: "fail",
			Detail: fmt.Sprintf("Config validation failed: %v", valErr),
		})
	} else {
		results = append(results, checkResult{
			Name:   "Configuration",
			Status: "pass",
			Detail: fmt.Sprintf("Backend: %s, Listen: %s", bootstrap.Storage.Backend, bootstrap.ListenAddr),
		})
	}

	// 2. Data directory
	results = append(results, checkDataDir(bootstrap))

	// 3. Org workspace directory
	results = append(results, checkOrgWorkspace(bootstrap))

	// 4. Disk space
	results = append(results, checkDiskSpace(bootstrap.DataDir))

	// Only proceed with DB checks for SQLite backend.
	if bootstrap.Storage.Backend != "sqlite" {
		results = append(results, checkResult{
			Name:   "Database",
			Status: "warn",
			Detail: fmt.Sprintf("Skipping DB checks for %s backend (doctor only supports SQLite)", bootstrap.Storage.Backend),
		})
		return results
	}

	// 5. Database connectivity
	db, dbErr := sqlite.Open(bootstrap.Storage.DSN)
	if dbErr != nil {
		results = append(results, checkResult{
			Name:   "Database",
			Status: "fail",
			Detail: fmt.Sprintf("Cannot open database: %v", dbErr),
		})
		return results
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "warn: failed to close database: %v\n", closeErr)
		}
	}()

	sqlDB := db.SqlDB()

	results = append(results, checkResult{
		Name:   "Database",
		Status: "pass",
		Detail: fmt.Sprintf("Connected to %s", bootstrap.Storage.DSN),
	})

	// 6. SQLite integrity check
	results = append(results, checkSQLiteIntegrity(sqlDB))

	// 7. Migration version
	results = append(results, checkMigrationVersion(sqlDB))

	// 8. Core tables exist
	results = append(results, checkCoreTables(sqlDB))

	// 9. Provider connectivity
	results = append(results, checkProviders(sqlDB))

	// 10. Stale agents
	results = append(results, checkStaleAgents(db))

	return results
}

// checkDataDir verifies the data directory exists.
func checkDataDir(cfg *config.BootstrapConfig) checkResult {
	info, err := os.Stat(cfg.DataDir)
	if err != nil {
		return checkResult{
			Name:   "Data Directory",
			Status: "fail",
			Detail: fmt.Sprintf("%s does not exist", cfg.DataDir),
		}
	}
	if !info.IsDir() {
		return checkResult{
			Name:   "Data Directory",
			Status: "fail",
			Detail: fmt.Sprintf("%s is not a directory", cfg.DataDir),
		}
	}
	return checkResult{
		Name:   "Data Directory",
		Status: "pass",
		Detail: cfg.DataDir,
	}
}

// checkOrgWorkspace verifies the org workspace directory exists.
func checkOrgWorkspace(cfg *config.BootstrapConfig) checkResult {
	info, err := os.Stat(cfg.OrgWorkspaceDir)
	if err != nil {
		return checkResult{
			Name:   "Org Workspace",
			Status: "warn",
			Detail: fmt.Sprintf("%s does not exist (will be created on first serve)", cfg.OrgWorkspaceDir),
		}
	}
	if !info.IsDir() {
		return checkResult{
			Name:   "Org Workspace",
			Status: "fail",
			Detail: fmt.Sprintf("%s is not a directory", cfg.OrgWorkspaceDir),
		}
	}
	return checkResult{
		Name:   "Org Workspace",
		Status: "pass",
		Detail: cfg.OrgWorkspaceDir,
	}
}

// checkDiskSpace checks available disk space in the data directory.
func checkDiskSpace(dataDir string) checkResult {
	info, err := os.Stat(dataDir)
	if err != nil || !info.IsDir() {
		return checkResult{
			Name:   "Disk Space",
			Status: "warn",
			Detail: "Cannot check disk space (data dir missing)",
		}
	}

	// Check database file size as a proxy for disk usage.
	dbFiles, err := filepath.Glob(filepath.Join(dataDir, "*.db*"))
	if err != nil {
		return checkResult{
			Name:   "Disk Space",
			Status: "warn",
			Detail: fmt.Sprintf("Cannot list db files: %v", err),
		}
	}
	var totalSize int64
	for _, f := range dbFiles {
		if fi, err := os.Stat(f); err == nil {
			totalSize += fi.Size()
		}
	}

	sizeMB := float64(totalSize) / (1024 * 1024)
	if sizeMB > 1024 {
		return checkResult{
			Name:   "Disk Space",
			Status: "warn",
			Detail: fmt.Sprintf("Database files total %.1f MB (>1GB)", sizeMB),
		}
	}

	return checkResult{
		Name:   "Disk Space",
		Status: "pass",
		Detail: fmt.Sprintf("Database files: %.1f MB", sizeMB),
	}
}

// checkSQLiteIntegrity runs PRAGMA integrity_check.
func checkSQLiteIntegrity(db *sql.DB) checkResult {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var result string
	err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result)
	if err != nil {
		return checkResult{
			Name:   "SQLite Integrity",
			Status: "fail",
			Detail: fmt.Sprintf("PRAGMA integrity_check failed: %v", err),
		}
	}

	if result != "ok" {
		return checkResult{
			Name:   "SQLite Integrity",
			Status: "fail",
			Detail: fmt.Sprintf("Integrity check: %s", result),
		}
	}

	return checkResult{
		Name:   "SQLite Integrity",
		Status: "pass",
		Detail: "PRAGMA integrity_check: ok",
	}
}

// checkMigrationVersion reports which migrations have been applied by counting core tables.
func checkMigrationVersion(db *sql.DB) checkResult {
	ctx := context.Background()

	// Count tables that exist from known migrations.
	migrationTables := []struct {
		migration string
		table     string
	}{
		{"001_core", "workspaces"},
		{"002_phase2", "projects"},
		{"003_cron", "cron_jobs"},
		{"004_fts5", "symbols_fts"},
		{"005_workflow", "workflows"},
		{"006_nervous", "event_handlers"},
		{"007_commhub", "comm_log"},
		{"008_telemetry", "sessions"},
		{"009_providers", "providers"},
		{"010_memory", "memories"},
		{"011_interject", "interjections"},
		{"012_pipeline_ext", "pipeline_assignments"},
		{"014_checkpoints", "agent_checkpoints"},
		{"015_vectors", "vector_embeddings"},
		{"016_tokens", "mcp_tokens"},
		{"017_delegations", "delegations"},
		{"018_agentmail", "agentmail_messages"},
	}

	applied := 0
	var missing []string
	for _, mt := range migrationTables {
		var name string
		err := db.QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?",
			mt.table,
		).Scan(&name)
		if err == nil {
			applied++
		} else {
			missing = append(missing, mt.migration)
		}
	}

	total := len(migrationTables)
	if applied == total {
		return checkResult{
			Name:   "Migrations",
			Status: "pass",
			Detail: fmt.Sprintf("All %d migration groups applied", total),
		}
	}

	if applied == 0 {
		return checkResult{
			Name:   "Migrations",
			Status: "fail",
			Detail: "No migrations applied (database is empty)",
		}
	}

	return checkResult{
		Name:   "Migrations",
		Status: "warn",
		Detail: fmt.Sprintf("%d/%d applied, missing: %s", applied, total, strings.Join(missing, ", ")),
	}
}

// checkCoreTables verifies essential tables exist and have data.
func checkCoreTables(db *sql.DB) checkResult {
	ctx := context.Background()

	tables := []string{"workspaces", "config", "personas"}
	var counts []string

	for _, table := range tables {
		var count int
		err := db.QueryRowContext(ctx,
			fmt.Sprintf("SELECT COUNT(*) FROM %s", table), //nolint:gosec // table names are hardcoded
		).Scan(&count)
		if err != nil {
			return checkResult{
				Name:   "Core Tables",
				Status: "fail",
				Detail: fmt.Sprintf("Cannot query %s: %v", table, err),
			}
		}
		counts = append(counts, fmt.Sprintf("%s=%d", table, count))
	}

	return checkResult{
		Name:   "Core Tables",
		Status: "pass",
		Detail: strings.Join(counts, ", "),
	}
}

// checkProviders reports on configured LLM providers.
func checkProviders(db *sql.DB) checkResult {
	ctx := context.Background()

	var total, enabled int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM providers").Scan(&total)
	if err != nil {
		return checkResult{
			Name:   "Providers",
			Status: "warn",
			Detail: fmt.Sprintf("Cannot query providers: %v", err),
		}
	}

	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM providers WHERE is_enabled = 1").Scan(&enabled)
	if err != nil {
		enabled = 0
	}

	if total == 0 {
		return checkResult{
			Name:   "Providers",
			Status: "warn",
			Detail: "No LLM providers configured",
		}
	}

	return checkResult{
		Name:   "Providers",
		Status: "pass",
		Detail: fmt.Sprintf("%d configured, %d enabled", total, enabled),
	}
}

// checkStaleAgents reports on agents with expired heartbeats.
func checkStaleAgents(db *sqlite.DB) checkResult {
	ctx := context.Background()
	store := db.NewStore()

	if store.Lifecycle == nil {
		return checkResult{
			Name:   "Stale Agents",
			Status: "warn",
			Detail: "Lifecycle repo not available",
		}
	}

	stale, err := store.Lifecycle.GetStaleAgents(ctx, 5*time.Minute)
	if err != nil {
		return checkResult{
			Name:   "Stale Agents",
			Status: "warn",
			Detail: fmt.Sprintf("Cannot check stale agents: %v", err),
		}
	}

	if len(stale) == 0 {
		return checkResult{
			Name:   "Stale Agents",
			Status: "pass",
			Detail: "No stale agents (TTL: 5m)",
		}
	}

	return checkResult{
		Name:   "Stale Agents",
		Status: "warn",
		Detail: fmt.Sprintf("%d stale agents: %s", len(stale), strings.Join(stale, ", ")),
	}
}

// printResults formats and prints all check results as a checklist.
func printResults(results []checkResult) {
	fmt.Println()
	fmt.Println("Hyperax Doctor")
	fmt.Println(strings.Repeat("=", 50))
	fmt.Println()

	passCount, warnCount, failCount := 0, 0, 0

	for _, r := range results {
		var icon string
		switch r.Status {
		case "pass":
			icon = "[PASS]"
			passCount++
		case "warn":
			icon = "[WARN]"
			warnCount++
		case "fail":
			icon = "[FAIL]"
			failCount++
		}

		fmt.Printf("  %-8s %-20s %s\n", icon, r.Name, r.Detail)
	}

	fmt.Println()
	fmt.Println(strings.Repeat("-", 50))
	fmt.Printf("  %d passed, %d warnings, %d failed\n", passCount, warnCount, failCount)
	fmt.Println()
}
