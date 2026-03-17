// Package plugin — registry.go persists plugin installation records to a JSON
// file so that manually installed plugins (via local path or remote URL) survive
// server restarts. The registry is loaded on startup after directory-based
// Discover() completes, and any plugins present in the registry but not already
// loaded are re-loaded from their recorded manifest path.
package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/hyperax/hyperax/pkg/types"
)

// registryFileName is the name of the JSON file that stores plugin installation
// records inside the plugin directory.
const registryFileName = "registry.json"

// PluginRecord captures how a plugin was installed so it can be reloaded after
// a server restart. The Source field uses a scheme prefix ("local:" or "remote:")
// to distinguish installation origins.
type PluginRecord struct {
	// Name is the plugin's unique identifier (from its manifest).
	Name string `json:"name"`

	// Source describes the installation origin, e.g. "local:/path/to/plugin"
	// or "remote:https://example.com/manifest.yaml".
	Source string `json:"source"`

	// ManifestPath is the absolute directory path containing the manifest file.
	// Populated for local installs; empty for remote installs.
	ManifestPath string `json:"manifest_path,omitempty"`

	// ManifestURL is the remote URL the manifest was fetched from.
	// Populated for remote installs; empty for local installs.
	ManifestURL string `json:"manifest_url,omitempty"`

	// CreatedResources tracks resources auto-created when the plugin was enabled
	// (e.g. cron jobs, event handlers). Used for cleanup on uninstall.
	CreatedResources []types.CreatedResource `json:"created_resources,omitempty"`
}

// Registry persists plugin installation records to a JSON file on disk.
// It is safe for concurrent use.
type Registry struct {
	path    string
	records map[string]PluginRecord
	mu      sync.Mutex
}

// NewRegistry creates or loads a registry from the given directory. If the
// registry file does not exist, an empty registry is returned. The directory
// is created if it does not already exist.
func NewRegistry(dir string) (*Registry, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("plugin.NewRegistry: create dir: %w", err)
	}

	r := &Registry{
		path:    filepath.Join(dir, registryFileName),
		records: make(map[string]PluginRecord),
	}

	data, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return r, nil
		}
		return nil, fmt.Errorf("plugin.NewRegistry: read file: %w", err)
	}

	if len(data) > 0 {
		if err := json.Unmarshal(data, &r.records); err != nil {
			return nil, fmt.Errorf("plugin.NewRegistry: parse JSON: %w", err)
		}
	}

	return r, nil
}

// Add records a plugin installation and flushes to disk.
func (r *Registry) Add(rec PluginRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.records[rec.Name] = rec
	return r.flush()
}

// Remove deletes a plugin record and flushes to disk.
func (r *Registry) Remove(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.records, name)
	return r.flush()
}

// List returns a snapshot of all recorded plugins.
func (r *Registry) List() []PluginRecord {
	r.mu.Lock()
	defer r.mu.Unlock()

	recs := make([]PluginRecord, 0, len(r.records))
	for _, rec := range r.records {
		recs = append(recs, rec)
	}
	return recs
}

// Get returns a single plugin record by name, or nil if not found.
func (r *Registry) Get(name string) *PluginRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.records[name]
	if !ok {
		return nil
	}
	return &rec
}


// SetCreatedResources records auto-created resource IDs for a plugin and flushes to disk.
func (r *Registry) SetCreatedResources(name string, resources []types.CreatedResource) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.records[name]
	if !ok {
		return nil
	}
	rec.CreatedResources = resources
	r.records[name] = rec
	return r.flush()
}

// GetCreatedResources returns the auto-created resources for a plugin.
func (r *Registry) GetCreatedResources(name string) []types.CreatedResource {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.records[name]
	if !ok {
		return nil
	}
	return rec.CreatedResources
}

// ClearCreatedResources removes the created resources list for a plugin and flushes to disk.
func (r *Registry) ClearCreatedResources(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.records[name]
	if !ok {
		return nil
	}
	rec.CreatedResources = nil
	r.records[name] = rec
	return r.flush()
}

// flush writes the current records map to disk as indented JSON.
// Must be called with r.mu held.
func (r *Registry) flush() error {
	data, err := json.MarshalIndent(r.records, "", "  ")
	if err != nil {
		return fmt.Errorf("plugin.Registry.flush: marshal: %w", err)
	}
	return os.WriteFile(r.path, data, 0o644)
}
