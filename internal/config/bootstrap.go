package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// BootstrapConfig contains only the fields needed before the database is available.
type BootstrapConfig struct {
	ListenAddr      string              `yaml:"listen_addr"`
	DataDir         string              `yaml:"data_dir"`
	OrgWorkspaceDir string              `yaml:"org_workspace_dir"`
	Storage         BootstrapStorage    `yaml:"storage"`
	LogLevel        string              `yaml:"log_level"`
	Observability   ObservabilityConfig `yaml:"observability"`
	Cache           CacheConfig         `yaml:"cache"`
	Search          SearchConfig        `yaml:"search"`
	AuditSink       AuditSinkConfig     `yaml:"audit_sink"`
	TLS             TLSConfig           `yaml:"tls"`
}

// TLSConfig holds TLS/HTTPS server parameters. When CertFile and KeyFile are
// both non-empty the server starts in HTTPS mode. An optional HTTP redirect
// listener can be enabled to forward plain-text requests to HTTPS.
type TLSConfig struct {
	// CertFile is the filesystem path to the PEM-encoded TLS certificate.
	CertFile string `yaml:"cert_file"`
	// KeyFile is the filesystem path to the PEM-encoded TLS private key.
	KeyFile string `yaml:"key_file"`
	// RedirectHTTP, when true, starts an additional HTTP listener that
	// returns 301 redirects to the equivalent HTTPS URL.
	RedirectHTTP bool `yaml:"redirect_http"`
	// HTTPAddr is the listen address for the HTTP redirect server.
	// Defaults to ":80" when RedirectHTTP is true and this field is empty.
	HTTPAddr string `yaml:"http_addr"`
}

// AuditSinkConfig controls the external JSONL audit file exporter.
// When enabled, the sink subscribes to all EventBus events (or a filtered
// subset) and appends them as one JSON object per line to an append-only file.
type AuditSinkConfig struct {
	// Enabled controls whether the JSONL audit sink is active. Default: false.
	Enabled bool `yaml:"enabled"`
	// FilePath is the path to the JSONL output file. Default: {data_dir}/audit.jsonl.
	FilePath string `yaml:"file_path"`
	// MaxSizeMB is the maximum file size in megabytes before rotation. 0 = no rotation.
	MaxSizeMB int64 `yaml:"max_size_mb"`
	// EventFilters is a list of glob patterns for event types to export.
	// An empty list means all events are exported. Patterns follow the same
	// semantics as nervous.MatchEventType (e.g. "pipeline.*", "*.completed").
	EventFilters []string `yaml:"event_filters"`
}

// CacheConfig holds in-memory cache parameters. Duration fields are strings
// because gopkg.in/yaml.v3 does not natively unmarshal time.Duration.
// Parsing into time.Duration is handled by the wiring layer (app.go).
type CacheConfig struct {
	// Enabled controls whether the in-memory cache is active.
	Enabled bool `yaml:"enabled"`

	// TTL is the time-to-live for cache entries (e.g. "10m", "1h").
	TTL string `yaml:"ttl"`

	// MaxSizeMB is the hard maximum cache size in megabytes.
	MaxSizeMB int `yaml:"max_size_mb"`

	// Shards is the number of internal cache shards (must be power of two).
	Shards int `yaml:"shards"`

	// CleanInterval is the frequency of the background eviction sweep (e.g. "5m").
	CleanInterval string `yaml:"clean_interval"`
}

// ObservabilityConfig holds tracer and metrics configuration.
type ObservabilityConfig struct {
	// OTelEndpoint is the gRPC endpoint for the OpenTelemetry collector,
	// e.g. "localhost:4317". Empty means tracing is disabled (no-op tracer).
	OTelEndpoint string `yaml:"otel_endpoint"`
	// ServiceName is the OTEL service name. Default: "hyperax".
	ServiceName string `yaml:"service_name"`
	// SamplingRate is the trace sampling rate from 0.0 to 1.0. Default: 1.0.
	SamplingRate float64 `yaml:"sampling_rate"`
}

// SearchConfig holds hybrid search engine parameters.
type SearchConfig struct {
	// EnableVector enables the vector search branch and RRF fusion.
	// When false, only BM25 (FTS5/LIKE) results are returned. Default: false.
	EnableVector bool `yaml:"enable_vector"`
	// EmbeddingModel is the filesystem path to the ONNX model file.
	EmbeddingModel string `yaml:"embedding_model"`
	// EmbeddingDim is the embedding vector dimension. Default: 384.
	EmbeddingDim int `yaml:"embedding_dim"`
	// FusionK is the RRF k parameter controlling rank-position impact.
	// Higher values produce more uniform fusion. Default: 60.
	FusionK int `yaml:"fusion_k"`
}

// BootstrapStorage defines database connection parameters.
type BootstrapStorage struct {
	Backend string `yaml:"backend"`
	DSN     string `yaml:"dsn"`
	MaxOpen int    `yaml:"max_open"`
	MaxIdle int    `yaml:"max_idle"`
}

// LoadBootstrap reads the minimal startup configuration from hyperax.yaml.
// If no config file is found, sensible defaults are used.
func LoadBootstrap() (*BootstrapConfig, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("config.LoadBootstrap: resolve home dir: %w", err)
	}

	cfg := &BootstrapConfig{
		ListenAddr:      ":9090",
		DataDir:         filepath.Join(homeDir, ".hyperax"),
		OrgWorkspaceDir: filepath.Join(homeDir, "Documents", "HyperAX"),
		LogLevel:        "info",
		Storage: BootstrapStorage{
			Backend: "sqlite",
			MaxOpen: 25,
			MaxIdle: 5,
		},
		Observability: ObservabilityConfig{
			ServiceName:  "hyperax",
			SamplingRate: 1.0,
		},
		Cache: CacheConfig{
			Enabled:       true,
			TTL:           "10m",
			MaxSizeMB:     256,
			Shards:        1024,
			CleanInterval: "5m",
		},
		Search: SearchConfig{
			EnableVector: false,
			EmbeddingDim: 384,
			FusionK:      60,
		},
	}

	paths := []string{
		"hyperax.yaml",
		filepath.Join(homeDir, ".hyperax", "hyperax.yaml"),
		"/etc/hyperax/hyperax.yaml",
	}

	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("config.LoadBootstrap: parse %s: %w", p, err)
		}
		break
	}

	// Expand ~ in paths
	if strings.HasPrefix(cfg.DataDir, "~/") {
		cfg.DataDir = filepath.Join(homeDir, cfg.DataDir[2:])
	}
	if strings.HasPrefix(cfg.OrgWorkspaceDir, "~/") {
		cfg.OrgWorkspaceDir = filepath.Join(homeDir, cfg.OrgWorkspaceDir[2:])
	}

	// Default SQLite DSN if not set
	if cfg.Storage.Backend == "sqlite" && cfg.Storage.DSN == "" {
		cfg.Storage.DSN = filepath.Join(cfg.DataDir, "hyperax.db")
	}

	return cfg, nil
}

// Validate checks the bootstrap config for required fields.
func (c *BootstrapConfig) Validate() error {
	if c.ListenAddr == "" {
		return fmt.Errorf("config.BootstrapConfig.Validate: listen_addr is required")
	}
	if c.DataDir == "" {
		return fmt.Errorf("config.BootstrapConfig.Validate: data_dir is required")
	}
	switch c.Storage.Backend {
	case "sqlite", "postgres", "mysql":
		// ok
	default:
		return fmt.Errorf("config.BootstrapConfig.Validate: unsupported storage backend: %s", c.Storage.Backend)
	}
	// TLS: both cert and key must be provided together.
	if (c.TLS.CertFile != "") != (c.TLS.KeyFile != "") {
		return fmt.Errorf("config.BootstrapConfig.Validate: tls.cert_file and tls.key_file must both be set")
	}
	return nil
}

// TLSEnabled reports whether the server should start in HTTPS mode.
func (c *BootstrapConfig) TLSEnabled() bool {
	return c.TLS.CertFile != "" && c.TLS.KeyFile != ""
}

// EnsureDataDir creates the data directory if it doesn't exist.
func (c *BootstrapConfig) EnsureDataDir() error {
	return os.MkdirAll(c.DataDir, 0o755)
}

// EnsureOrgWorkspaceDir creates the org workspace directory if it doesn't exist.
func (c *BootstrapConfig) EnsureOrgWorkspaceDir() error {
	return os.MkdirAll(c.OrgWorkspaceDir, 0o755)
}
