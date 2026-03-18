package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/hyperax/hyperax/internal/app"
	"github.com/hyperax/hyperax/internal/config"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/storage"
	mysqlstore "github.com/hyperax/hyperax/internal/storage/mysql"
	"github.com/hyperax/hyperax/internal/storage/postgres"
	"github.com/hyperax/hyperax/internal/storage/sqlite"
	"github.com/hyperax/hyperax/internal/web"
	"github.com/hyperax/hyperax/ui"
	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	serve := serveCmd()

	rootCmd := &cobra.Command{
		Use:   "hyperax",
		Short: "Hyper-Agentic eXchange — AI agent infrastructure platform",
	}

	rootCmd.AddCommand(serve)
	rootCmd.AddCommand(initCmd())
	rootCmd.AddCommand(versionCmd())
	rootCmd.AddCommand(doctorCmd())
	rootCmd.AddCommand(approveCmd())

	// Default to serve when no subcommand is given
	rootCmd.RunE = serve.RunE

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func serveCmd() *cobra.Command {
	var addr string
	var logFile string
	var tlsCert string
	var tlsKey string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the Hyperax server",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load bootstrap config
			bootstrap, err := config.LoadBootstrap()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if err := bootstrap.Validate(); err != nil {
				return fmt.Errorf("validate config: %w", err)
			}

			// Override listen address if flag provided
			if addr != "" {
				bootstrap.ListenAddr = addr
			}

			// Override TLS cert/key if CLI flags provided
			if tlsCert != "" {
				bootstrap.TLS.CertFile = tlsCert
			}
			if tlsKey != "" {
				bootstrap.TLS.KeyFile = tlsKey
			}

			// Re-validate after CLI overrides (catches mismatched cert/key).
			if err := bootstrap.Validate(); err != nil {
				return fmt.Errorf("validate config (post-override): %w", err)
			}

			// Ensure data directory exists
			if err := bootstrap.EnsureDataDir(); err != nil {
				return fmt.Errorf("create data dir: %w", err)
			}

			// Create logger (optionally writing to a file)
			logger, logCloser := newLogger(bootstrap.LogLevel, logFile)
			if logCloser != nil {
				defer logCloser()
			}

			// Open database based on configured backend.
			var store *storage.Store
			var sqlDB *sql.DB
			ctx := context.Background()

			switch bootstrap.Storage.Backend {
			case "postgres":
				pgDB, err := postgres.Open(bootstrap.Storage.DSN, logger)
				if err != nil {
					return fmt.Errorf("open postgres: %w", err)
				}
				if err := pgDB.Migrate(ctx); err != nil {
					if closeErr := pgDB.Close(); closeErr != nil {
						fmt.Fprintf(os.Stderr, "warn: failed to close postgres after migrate error: %v\n", closeErr)
					}
					return fmt.Errorf("postgres migrate: %w", err)
				}
				store = pgDB.NewStore()
				sqlDB = pgDB.SqlDB()
			case "mysql":
				myDB, err := mysqlstore.Open(bootstrap.Storage.DSN, logger)
				if err != nil {
					return fmt.Errorf("open mysql: %w", err)
				}
				if err := myDB.Migrate(ctx); err != nil {
					if closeErr := myDB.Close(); closeErr != nil {
						fmt.Fprintf(os.Stderr, "warn: failed to close mysql after migrate error: %v\n", closeErr)
					}
					return fmt.Errorf("mysql migrate: %w", err)
				}
				store = myDB.NewStore()
				sqlDB = myDB.SqlDB()
			default: // "sqlite"
				db, err := sqlite.Open(bootstrap.Storage.DSN)
				if err != nil {
					return fmt.Errorf("open storage: %w", err)
				}
				if err := db.Migrate(ctx); err != nil {
					if closeErr := db.Close(); closeErr != nil {
						fmt.Fprintf(os.Stderr, "warn: failed to close sqlite after migrate error: %v\n", closeErr)
					}
					return fmt.Errorf("migrate: %w", err)
				}
				store = db.NewStore()
				sqlDB = db.SqlDB()
			}

			// Create EventBus
			bus := nervous.NewEventBus(256)

			// Create application
			application := app.New(bootstrap, store, bus, logger)
			application.Version = version

			// Prepare embedded UI filesystem
			uiFS := ui.DistFS()
			if uiFS == nil {
				logger.Warn("embedded UI not available")
			}

			// Build and set router
			router := web.BuildRouter(application, uiFS, sqlDB)
			application.SetRouter(router)

			// Signal-driven context
			ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			// Start application
			if err := application.Start(ctx); err != nil {
				return fmt.Errorf("start: %w", err)
			}

			// Block until signal
			<-ctx.Done()

			// Graceful shutdown
			application.Stop()
			return nil
		},
	}

	cmd.Flags().StringVarP(&addr, "addr", "a", "", "Listen address (overrides config)")
	cmd.Flags().StringVar(&logFile, "log", "", "Log file path (default: stderr)")
	cmd.Flags().StringVar(&tlsCert, "tls-cert", "", "TLS certificate file (overrides config)")
	cmd.Flags().StringVar(&tlsKey, "tls-key", "", "TLS key file (overrides config)")
	return cmd
}

func initCmd() *cobra.Command {
	var interactive bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize Hyperax configuration",
		Long:  "Creates the data directory and generates a bootstrap configuration file. Use --interactive for a guided setup wizard.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if interactive {
				outputPath, err := runInitWizard()
				if err != nil {
					return fmt.Errorf("init wizard: %w", err)
				}
				fmt.Println()
				fmt.Printf("Configuration written to %s\n", outputPath)
				fmt.Println("Run 'hyperax serve' to start the server.")
				return nil
			}

			// Non-interactive: use defaults.
			bootstrap, err := config.LoadBootstrap()
			if err != nil {
				return err
			}

			if err := bootstrap.EnsureDataDir(); err != nil {
				return fmt.Errorf("create data dir: %w", err)
			}

			fmt.Printf("Hyperax initialized\n")
			fmt.Printf("  Data dir:       %s\n", bootstrap.DataDir)
			fmt.Printf("  Org workspace:  %s\n", bootstrap.OrgWorkspaceDir)
			fmt.Printf("  Database:       %s\n", bootstrap.Storage.DSN)
			fmt.Printf("  Listen:         %s\n", bootstrap.ListenAddr)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "Run interactive setup wizard")
	return cmd
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("hyperax %s\n", version)
			if commit != "none" {
				fmt.Printf("  commit: %s\n", commit)
			}
			if date != "unknown" {
				fmt.Printf("  built:  %s\n", date)
			}
		},
	}
}

func newLogger(level string, logFile string) (*slog.Logger, func()) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level:     lvl,
		AddSource: true,
	}

	// Default to stderr
	if logFile == "" {
		handler := slog.NewJSONHandler(os.Stderr, opts)
		return slog.New(handler), nil
	}

	// Rotate: move current log to .bkp (overwrite if .bkp exists)
	if _, err := os.Stat(logFile); err == nil {
		if renameErr := os.Rename(logFile, logFile+".bkp"); renameErr != nil {
			fmt.Fprintf(os.Stderr, "WARNING: could not rotate log file: %v\n", renameErr)
		}
	}

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		// Fall back to stderr if we can't open the log file
		fmt.Fprintf(os.Stderr, "WARNING: could not open log file %s: %v, falling back to stderr\n", logFile, err)
		handler := slog.NewJSONHandler(os.Stderr, opts)
		return slog.New(handler), nil
	}

	handler := slog.NewJSONHandler(f, opts)
	closer := func() {
		if closeErr := f.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "WARNING: failed to close log file: %v\n", closeErr)
		}
	}
	return slog.New(handler), closer
}
