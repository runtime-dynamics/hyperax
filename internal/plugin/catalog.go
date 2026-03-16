package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/hyperax/hyperax/pkg/types"
)

// CatalogManager manages the plugin catalog -- an embedded baseline
// plus GitHub-based refresh that checks for latest releases.
type CatalogManager struct {
	mu       sync.RWMutex
	catalog  *types.PluginCatalog
	pm       *PluginManager // cross-reference for install status
	registry *Registry      // install registry for detecting failed-to-load plugins
	logger   *slog.Logger
}

// NewCatalogManager creates a CatalogManager from embedded YAML data.
func NewCatalogManager(embeddedData []byte, pm *PluginManager, logger *slog.Logger) (*CatalogManager, error) {
	var cat types.PluginCatalog
	if err := yaml.Unmarshal(embeddedData, &cat); err != nil {
		return nil, fmt.Errorf("plugin.NewCatalogManager: %w", err)
	}
	return &CatalogManager{
		catalog: &cat,
		pm:      pm,
		logger:  logger,
	}, nil
}

// SetRegistry sets the install registry for detecting installed-but-failed-to-load plugins.
func (cm *CatalogManager) SetRegistry(r *Registry) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.registry = r
}

// List returns catalog entries filtered by optional category.
// If category is empty, all entries are returned.
// If verifiedOnly is true, only verified entries are returned.
func (cm *CatalogManager) List(category string, verifiedOnly bool) []types.CatalogEntryWithStatus {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var results []types.CatalogEntryWithStatus
	for _, entry := range cm.catalog.Plugins {
		if category != "" && entry.Category != category {
			continue
		}
		if verifiedOnly && !entry.Verified {
			continue
		}
		results = append(results, cm.annotate(entry))
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})
	return results
}

// Search returns catalog entries matching a keyword query against name,
// display_name, description, and tags.
func (cm *CatalogManager) Search(query string, category string) []types.CatalogEntryWithStatus {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return cm.List(category, false)
	}

	var results []types.CatalogEntryWithStatus
	for _, entry := range cm.catalog.Plugins {
		if category != "" && entry.Category != category {
			continue
		}
		if cm.matches(entry, q) {
			results = append(results, cm.annotate(entry))
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})
	return results
}

// Get returns a single catalog entry by name, or nil if not found.
func (cm *CatalogManager) Get(name string) *types.CatalogEntryWithStatus {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	for _, entry := range cm.catalog.Plugins {
		if entry.Name == name {
			e := cm.annotate(entry)
			return &e
		}
	}
	return nil
}

// ListVersions returns all available release versions for a catalog entry,
// sorted in ascending semantic order. It queries the GitHub releases API.
// ghToken is optional (for rate limits / private repos).
func (cm *CatalogManager) ListVersions(ctx context.Context, name, ghToken string) ([]string, error) {
	cm.mu.RLock()
	var source string
	for _, entry := range cm.catalog.Plugins {
		if entry.Name == name {
			source = entry.Source
			break
		}
	}
	cm.mu.RUnlock()

	if source == "" {
		return nil, fmt.Errorf("plugin %q not found in catalog", name)
	}
	if !strings.HasPrefix(source, "github.com/") {
		return nil, fmt.Errorf("plugin %q has no GitHub source", name)
	}

	src, err := ParseSource(source)
	if err != nil {
		return nil, fmt.Errorf("plugin.CatalogManager.ListVersions: %w", err)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases?per_page=100", src.Owner, src.Repo)
	data, err := ghGet(ctx, client, apiURL, ghToken)
	if err != nil {
		return nil, fmt.Errorf("plugin.CatalogManager.ListVersions: %w", err)
	}

	var releases []struct {
		TagName    string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.Unmarshal(data, &releases); err != nil {
		return nil, fmt.Errorf("plugin.CatalogManager.ListVersions: %w", err)
	}

	var versions []string
	for _, r := range releases {
		if r.Draft || r.Prerelease {
			continue
		}
		versions = append(versions, strings.TrimPrefix(r.TagName, "v"))
	}

	// Sort ascending by semantic version.
	sort.Slice(versions, func(i, j int) bool {
		return compareSemver(versions[i], versions[j]) < 0
	})

	return versions, nil
}

// compareSemver compares two semver strings. Returns -1, 0, or 1.
func compareSemver(a, b string) int {
	pa := strings.SplitN(a, ".", 3)
	pb := strings.SplitN(b, ".", 3)
	for i := 0; i < 3; i++ {
		var va, vb int
		if i < len(pa) {
			_, _ = fmt.Sscanf(pa[i], "%d", &va)
		}
		if i < len(pb) {
			_, _ = fmt.Sscanf(pb[i], "%d", &vb)
		}
		if va < vb {
			return -1
		}
		if va > vb {
			return 1
		}
	}
	return 0
}


// Refresh checks each catalog entry's GitHub repo for the latest release
// and updates the latest_version field. It also discovers new repos from
// the configured GitHub org that match the plugin prefix pattern.
// ghToken is optional (for private repos or higher rate limits).
func (cm *CatalogManager) Refresh(ctx context.Context, ghOrg, repoPrefix, ghToken string) (int, int, error) {
	if ghOrg == "" {
		ghOrg = "runtime-dynamics"
	}
	if repoPrefix == "" {
		repoPrefix = "hax-plugin-"
	}

	client := &http.Client{Timeout: 15 * time.Second}
	var added, updated int

	// Phase 1: Discover new repos from the GitHub org.
	discovered, err := cm.discoverOrgRepos(ctx, client, ghOrg, repoPrefix, ghToken)
	if err != nil {
		cm.logger.Warn("catalog refresh: org discovery failed, continuing with existing entries",
			"org", ghOrg, "error", err)
	} else {
		cm.mu.Lock()
		existing := make(map[string]bool)
		for _, e := range cm.catalog.Plugins {
			existing[e.Name] = true
		}
		for _, entry := range discovered {
			if !existing[entry.Name] {
				cm.catalog.Plugins = append(cm.catalog.Plugins, entry)
				added++
				cm.logger.Info("catalog: discovered new plugin", "name", entry.Name, "source", entry.Source)
			}
		}
		cm.mu.Unlock()
	}

	// Phase 2: Check latest release version for each entry with a GitHub source.
	cm.mu.RLock()
	entries := make([]types.CatalogEntry, len(cm.catalog.Plugins))
	copy(entries, cm.catalog.Plugins)
	cm.mu.RUnlock()

	for i, entry := range entries {
		if !strings.HasPrefix(entry.Source, "github.com/") {
			continue
		}

		src, parseErr := ParseSource(entry.Source)
		if parseErr != nil {
			cm.logger.Debug("catalog refresh: skip invalid source", "name", entry.Name, "error", parseErr)
			continue
		}

		latestVersion, fetchErr := cm.fetchLatestVersion(ctx, client, src.Owner, src.Repo, ghToken)
		if fetchErr != nil {
			cm.logger.Debug("catalog refresh: failed to fetch latest version",
				"name", entry.Name, "error", fetchErr)
			continue
		}

		if latestVersion != "" && latestVersion != entry.LatestVersion {
			cm.mu.Lock()
			if i < len(cm.catalog.Plugins) && cm.catalog.Plugins[i].Name == entry.Name {
				cm.catalog.Plugins[i].LatestVersion = latestVersion
			}
			cm.mu.Unlock()
			updated++
			cm.logger.Info("catalog: updated version",
				"plugin", entry.Name, "old", entry.LatestVersion, "new", latestVersion)
		}
	}

	cm.mu.Lock()
	cm.catalog.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	cm.mu.Unlock()

	cm.logger.Info("catalog refreshed", "added", added, "updated", updated,
		"total", len(cm.catalog.Plugins))
	return added, updated, nil
}

// discoverOrgRepos lists public repos in a GitHub org matching the prefix
// and builds catalog entries for any that have a hyperax-plugin.yaml manifest.
func (cm *CatalogManager) discoverOrgRepos(ctx context.Context, client *http.Client,
	org, prefix, ghToken string) ([]types.CatalogEntry, error) {

	// Fetch repos from the org (paginated, up to 100).
	apiURL := fmt.Sprintf("https://api.github.com/orgs/%s/repos?per_page=100&type=public", org)
	data, err := ghGet(ctx, client, apiURL, ghToken)
	if err != nil {
		return nil, fmt.Errorf("plugin.CatalogManager.discoverOrgRepos: %w", err)
	}

	var repos []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		HTMLURL     string `json:"html_url"`
		Topics      []string `json:"topics"`
	}
	if err := json.Unmarshal(data, &repos); err != nil {
		return nil, fmt.Errorf("plugin.CatalogManager.discoverOrgRepos: %w", err)
	}

	var entries []types.CatalogEntry
	for _, r := range repos {
		if !strings.HasPrefix(r.Name, prefix) {
			continue
		}

		// Derive plugin name from repo name (strip prefix).
		pluginName := strings.TrimPrefix(r.Name, prefix)
		if pluginName == "" {
			continue
		}

		// Get latest release version.
		latestVersion, _ := cm.fetchLatestVersion(ctx, client, org, r.Name, ghToken)
		if latestVersion == "" {
			latestVersion = "0.0.1"
		}

		// Infer category from topics or name.
		category := inferCategory(r.Topics, pluginName)

		entries = append(entries, types.CatalogEntry{
			Name:          pluginName,
			DisplayName:   titleCase(strings.ReplaceAll(pluginName, "-", " ")),
			Description:   r.Description,
			Category:      category,
			Author:        "Runtime Dynamics",
			License:       "Apache-2.0",
			Source:        fmt.Sprintf("github.com/%s/%s", org, r.Name),
			Homepage:      r.HTMLURL,
			LatestVersion: latestVersion,
			Verified:      true,
			Tags:          r.Topics,
		})
	}

	return entries, nil
}

// fetchLatestVersion queries the GitHub releases API for the latest release tag.
func (cm *CatalogManager) fetchLatestVersion(ctx context.Context, client *http.Client,
	owner, repo, ghToken string) (string, error) {

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
	data, err := ghGet(ctx, client, apiURL, ghToken)
	if err != nil {
		return "", fmt.Errorf("plugin.CatalogManager.fetchLatestVersion: %w", err)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(data, &release); err != nil {
		return "", fmt.Errorf("plugin.CatalogManager.fetchLatestVersion: %w", err)
	}

	return strings.TrimPrefix(release.TagName, "v"), nil
}

// inferCategory guesses a plugin's integration category from its topics or name.
func inferCategory(topics []string, name string) string {
	for _, t := range topics {
		switch t {
		case "channel", "messaging", "discord", "slack", "email":
			return "channel"
		case "secret", "secrets", "vault", "1password":
			return "secret_provider"
		case "sensor", "monitoring", "prometheus":
			return "sensor"
		case "guard", "approval":
			return "guard"
		case "audit":
			return "audit"
		case "tooling", "tools":
			return "tooling"
		}
	}
	// Fallback: check name patterns.
	switch {
	case strings.Contains(name, "discord") || strings.Contains(name, "slack") || strings.Contains(name, "email"):
		return "channel"
	case strings.Contains(name, "vault") || strings.Contains(name, "1password") || strings.Contains(name, "secret"):
		return "secret_provider"
	case strings.Contains(name, "prometheus") || strings.Contains(name, "sensor"):
		return "sensor"
	case strings.Contains(name, "approval") || strings.Contains(name, "gate") || strings.Contains(name, "guard"):
		return "guard"
	case strings.Contains(name, "audit"):
		return "audit"
	default:
		return "tooling"
	}
}

// titleCase capitalises the first letter of each word in s.
func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}


// matches checks if a catalog entry matches a search query.
func (cm *CatalogManager) matches(entry types.CatalogEntry, query string) bool {
	if strings.Contains(strings.ToLower(entry.Name), query) {
		return true
	}
	if strings.Contains(strings.ToLower(entry.DisplayName), query) {
		return true
	}
	if strings.Contains(strings.ToLower(entry.Description), query) {
		return true
	}
	for _, tag := range entry.Tags {
		if strings.Contains(strings.ToLower(tag), query) {
			return true
		}
	}
	return false
}

// annotate enriches a CatalogEntry with installation status from the PluginManager.
// If a plugin is in the install registry but failed to load (e.g. broken after
// a Hyperax upgrade), it is still marked as installed so the upgrade button appears.
func (cm *CatalogManager) annotate(entry types.CatalogEntry) types.CatalogEntryWithStatus {
	result := types.CatalogEntryWithStatus{
		CatalogEntry: entry,
	}
	if cm.pm == nil {
		return result
	}

	// Check loaded plugins first (normal path).
	plugins := cm.pm.ListPlugins()
	for _, p := range plugins {
		if p.Name == entry.Name {
			result.Installed = true
			result.InstalledVersion = p.Version
			result.Enabled = p.Enabled
			return result
		}
	}

	// Fallback: check install registry for plugins that are installed on disk
	// but failed to load (broken manifest, version incompatibility, etc.).
	// These still need the upgrade button so the user can fix them without
	// having to uninstall and lose configuration.
	if cm.registry != nil {
		if rec := cm.registry.Get(entry.Name); rec != nil {
			result.Installed = true
			result.Enabled = false
			// No version info available since the plugin didn't load —
			// use "0.0.0" as a sentinel so the UI always offers an upgrade.
			result.InstalledVersion = "0.0.0"
		}
	}

	return result
}

