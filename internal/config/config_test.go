package config

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/storage/sqlite"
	"github.com/hyperax/hyperax/pkg/types"
)

func setupConfigTest(t *testing.T) (*ConfigStore, context.Context) {
	t.Helper()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "test.db")

	db, err := sqlite.Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	store := db.NewStore()
	bus := nervous.NewEventBus(16)
	cs := NewConfigStore(store.Config, bus)

	return cs, ctx
}

func TestLoadBootstrap_Defaults(t *testing.T) {
	// Save and restore PWD to avoid finding a hyperax.yaml in current dir
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	cfg, err := LoadBootstrap()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.ListenAddr != ":9090" {
		t.Errorf("listen_addr = %q, want :9090", cfg.ListenAddr)
	}
	if cfg.Storage.Backend != "sqlite" {
		t.Errorf("backend = %q, want sqlite", cfg.Storage.Backend)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("log_level = %q, want info", cfg.LogLevel)
	}
}

func TestLoadBootstrap_FromFile(t *testing.T) {
	dir := t.TempDir()
	yamlContent := `
listen_addr: ":3000"
data_dir: "/custom/data"
log_level: "debug"
storage:
  backend: "postgres"
  dsn: "postgres://localhost/hyperax"
`
	if err := os.WriteFile(filepath.Join(dir, "hyperax.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	cfg, err := LoadBootstrap()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.ListenAddr != ":3000" {
		t.Errorf("listen_addr = %q", cfg.ListenAddr)
	}
	if cfg.Storage.Backend != "postgres" {
		t.Errorf("backend = %q", cfg.Storage.Backend)
	}
	if cfg.Storage.DSN != "postgres://localhost/hyperax" {
		t.Errorf("dsn = %q", cfg.Storage.DSN)
	}
}

func TestBootstrapConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     BootstrapConfig
		wantErr bool
	}{
		{
			name:    "valid",
			cfg:     BootstrapConfig{ListenAddr: ":9090", DataDir: "/tmp", Storage: BootstrapStorage{Backend: "sqlite"}},
			wantErr: false,
		},
		{
			name:    "no listen addr",
			cfg:     BootstrapConfig{DataDir: "/tmp", Storage: BootstrapStorage{Backend: "sqlite"}},
			wantErr: true,
		},
		{
			name:    "bad backend",
			cfg:     BootstrapConfig{ListenAddr: ":9090", DataDir: "/tmp", Storage: BootstrapStorage{Backend: "redis"}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBootstrapConfig_EnsureDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub", "dir")
	cfg := &BootstrapConfig{DataDir: dir}

	if err := cfg.EnsureDataDir(); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.IsDir() {
		t.Error("not a directory")
	}
}

func TestConfigStore_Resolve_ScopeChain(t *testing.T) {
	cs, ctx := setupConfigTest(t)

	// Seed a key with default
	if err := cs.repo.UpsertKeyMeta(ctx, &types.ConfigKeyMeta{
		Key:        "log.level",
		ScopeType:  "global",
		ValueType:  "string",
		DefaultVal: "info",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// No values set — should return default
	val, err := cs.Resolve(ctx, "log.level", "", "")
	if err != nil {
		t.Fatalf("resolve default: %v", err)
	}
	if val != "info" {
		t.Errorf("default = %q, want info", val)
	}

	// Set global value
	if err := cs.repo.SetValue(ctx, "log.level", "warn", types.ConfigScope{Type: "global", ID: ""}, "test"); err != nil {
		t.Fatalf("set global: %v", err)
	}
	val, err = cs.Resolve(ctx, "log.level", "", "")
	if err != nil {
		t.Fatalf("resolve global: %v", err)
	}
	if val != "warn" {
		t.Errorf("global = %q, want warn", val)
	}

	// Set workspace value — should override global
	if err := cs.repo.SetValue(ctx, "log.level", "debug", types.ConfigScope{Type: "workspace", ID: "ws-1"}, "test"); err != nil {
		t.Fatalf("set workspace: %v", err)
	}
	val, err = cs.Resolve(ctx, "log.level", "", "ws-1")
	if err != nil {
		t.Fatalf("resolve workspace: %v", err)
	}
	if val != "debug" {
		t.Errorf("workspace = %q, want debug", val)
	}

	// Set agent value — should override workspace
	if err := cs.repo.SetValue(ctx, "log.level", "error", types.ConfigScope{Type: "agent", ID: "agent-1"}, "test"); err != nil {
		t.Fatalf("set agent: %v", err)
	}
	val, err = cs.Resolve(ctx, "log.level", "agent-1", "ws-1")
	if err != nil {
		t.Fatalf("resolve agent: %v", err)
	}
	if val != "error" {
		t.Errorf("agent = %q, want error", val)
	}
}

func TestConfigStore_Set_PublishesEvent(t *testing.T) {
	cs, ctx := setupConfigTest(t)

	if err := cs.repo.UpsertKeyMeta(ctx, &types.ConfigKeyMeta{
		Key:       "test.key",
		ScopeType: "global",
		ValueType: "string",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Subscribe to events
	sub := cs.bus.Subscribe("test", nil)
	defer cs.bus.Unsubscribe("test")

	err := cs.Set(ctx, "test.key", "value", types.ConfigScope{Type: "global", ID: ""}, "admin")
	if err != nil {
		t.Fatalf("set: %v", err)
	}

	// Should have received config.changed event
	select {
	case event := <-sub.Ch:
		if event.Type != "config.changed" {
			t.Errorf("event type = %q, want config.changed", event.Type)
		}
	default:
		t.Error("expected event, got none")
	}
}

func TestConfigStore_IsCritical(t *testing.T) {
	cs, ctx := setupConfigTest(t)

	if err := cs.repo.UpsertKeyMeta(ctx, &types.ConfigKeyMeta{Key: "safe", Critical: false}); err != nil {
		t.Fatalf("upsert safe: %v", err)
	}
	if err := cs.repo.UpsertKeyMeta(ctx, &types.ConfigKeyMeta{Key: "dangerous", Critical: true}); err != nil {
		t.Fatalf("upsert dangerous: %v", err)
	}

	crit, err := cs.IsCritical(ctx, "safe")
	if err != nil {
		t.Fatalf("IsCritical safe: %v", err)
	}
	if crit {
		t.Error("safe should not be critical")
	}

	crit, err = cs.IsCritical(ctx, "dangerous")
	if err != nil {
		t.Fatalf("IsCritical dangerous: %v", err)
	}
	if !crit {
		t.Error("dangerous should be critical")
	}
}

func TestSeedConfigKeys(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "test.db")

	db, err := sqlite.Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := db.NewStore()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := SeedConfigKeys(ctx, store.Config, logger); err != nil {
		t.Fatalf("seed: %v", err)
	}

	keys, err := store.Config.ListKeys(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != len(DefaultConfigKeys) {
		t.Errorf("seeded %d keys, want %d", len(keys), len(DefaultConfigKeys))
	}
}
