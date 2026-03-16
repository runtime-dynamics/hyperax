package types

// CatalogEntry describes an available plugin in the official catalog.
type CatalogEntry struct {
	Name          string   `json:"name" yaml:"name"`
	DisplayName   string   `json:"display_name" yaml:"display_name"`
	Description   string   `json:"description" yaml:"description"`
	Category      string   `json:"category" yaml:"category"` // channel, tooling, secret_provider, sensor
	Author        string   `json:"author" yaml:"author"`
	License       string   `json:"license,omitempty" yaml:"license"`
	Source        string   `json:"source" yaml:"source"` // github.com/org/repo
	Homepage      string   `json:"homepage,omitempty" yaml:"homepage"`
	MinHyperaxVer string   `json:"min_hyperax_version,omitempty" yaml:"min_hyperax_version"`
	LatestVersion string   `json:"latest_version" yaml:"latest_version"`
	Verified      bool     `json:"verified" yaml:"verified"`
	Tags          []string `json:"tags,omitempty" yaml:"tags"`
	Icon          string   `json:"icon,omitempty" yaml:"icon"`
}

// PluginCatalog is the top-level catalog structure.
type PluginCatalog struct {
	Version   string         `json:"version" yaml:"version"`
	UpdatedAt string         `json:"updated_at" yaml:"updated_at"`
	Plugins   []CatalogEntry `json:"plugins" yaml:"plugins"`
}

// CatalogEntryWithStatus extends CatalogEntry with installation state.
type CatalogEntryWithStatus struct {
	CatalogEntry
	Installed        bool   `json:"installed"`
	InstalledVersion string `json:"installed_version,omitempty"`
	Enabled          bool   `json:"enabled"`
}
