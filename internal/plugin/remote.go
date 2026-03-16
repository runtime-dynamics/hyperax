package plugin

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/hyperax/hyperax/pkg/types"
	"gopkg.in/yaml.v3"
)

// GitHubSource represents a parsed plugin source like "github.com/org/repo@v1.2.0".
type GitHubSource struct {
	Owner   string
	Repo    string
	Version string // empty = latest
}

// ParseSource parses a GitHub plugin source string.
// Supported formats:
//   - "github.com/org/repo"
//   - "github.com/org/repo@v1.2.0"
func ParseSource(source string) (*GitHubSource, error) {
	s := strings.TrimPrefix(source, "https://")
	s = strings.TrimPrefix(s, "http://")

	if !strings.HasPrefix(s, "github.com/") {
		return nil, fmt.Errorf("plugin source must start with github.com/, got %q", source)
	}

	rest := strings.TrimPrefix(s, "github.com/")

	// Split version if present.
	var version string
	if idx := strings.LastIndex(rest, "@"); idx > 0 {
		version = rest[idx+1:]
		rest = rest[:idx]
	}

	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid GitHub source %q, expected github.com/owner/repo[@version]", source)
	}

	return &GitHubSource{
		Owner:   parts[0],
		Repo:    parts[1],
		Version: version,
	}, nil
}

// ghRelease is a minimal GitHub release API response.
type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

// ghAsset is a minimal GitHub release asset.
type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// FetchRelease downloads a platform archive from a GitHub release and extracts it.
// The archive is located by pattern-matching release assets against the current OS
// and architecture (runtime.GOOS/GOARCH). After extraction, the manifest is read from the extracted
// hyperax-plugin.yaml file inside the archive (goreleaser bundles it there).
// If ghToken is non-empty, it's used as a Bearer token for private repo access.
// The archive is extracted to targetDir/{manifest.Name}/.
func FetchRelease(ctx context.Context, src GitHubSource, ghToken string,
	targetDir string, logger *slog.Logger) (*types.PluginManifest, error) {

	client := &http.Client{Timeout: 60 * time.Second}

	// Build release API URL.
	var apiURL string
	if src.Version != "" {
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s",
			src.Owner, src.Repo, src.Version)
	} else {
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest",
			src.Owner, src.Repo)
	}

	logger.Info("fetching GitHub release", "url", apiURL)

	// Fetch release metadata.
	releaseData, err := ghGet(ctx, client, apiURL, ghToken)
	if err != nil {
		return nil, fmt.Errorf("plugin.FetchRelease: fetch release metadata: %w", err)
	}

	var release ghRelease
	if err := json.Unmarshal(releaseData, &release); err != nil {
		return nil, fmt.Errorf("plugin.FetchRelease: parse release JSON: %w", err)
	}

	// Find platform archive by pattern matching against release assets.
	version := strings.TrimPrefix(release.TagName, "v")

	assetName := findPlatformAsset(release.Assets, src.Repo, version, runtime.GOOS, runtime.GOARCH)
	if assetName == "" {
		// Build list of available assets for a helpful error message.
		var names []string
		for _, a := range release.Assets {
			names = append(names, a.Name)
		}
		return nil, fmt.Errorf("plugin.FetchRelease: release %s has no archive for %s/%s; available assets: %v",
			release.TagName, runtime.GOOS, runtime.GOARCH, names)
	}

	assetURL := findAssetURL(release.Assets, assetName)
	if assetURL == "" {
		return nil, fmt.Errorf("plugin.FetchRelease: release %s: could not resolve download URL for %q", release.TagName, assetName)
	}

	// Download the platform archive.
	archiveData, err := ghGet(ctx, client, assetURL, ghToken)
	if err != nil {
		return nil, fmt.Errorf("plugin.FetchRelease: download artifact %q: %w", assetName, err)
	}

	// We need a temporary extraction directory first because we don't know the
	// plugin name until we read the manifest from inside the archive.
	tmpDir, err := os.MkdirTemp("", "hyperax-plugin-extract-*")
	if err != nil {
		return nil, fmt.Errorf("plugin.FetchRelease: create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Extract based on file extension.
	switch {
	case strings.HasSuffix(assetName, ".tar.gz") || strings.HasSuffix(assetName, ".tgz"):
		if err := extractTarGz(archiveData, tmpDir); err != nil {
			return nil, fmt.Errorf("plugin.FetchRelease: extract tar.gz: %w", err)
		}
	case strings.HasSuffix(assetName, ".zip"):
		if err := extractZip(archiveData, tmpDir); err != nil {
			return nil, fmt.Errorf("plugin.FetchRelease: extract zip: %w", err)
		}
	default:
		return nil, fmt.Errorf("plugin.FetchRelease: unsupported archive format: %s", assetName)
	}

	// Read manifest from the extracted archive contents.
	// If the archive doesn't include it, fall back to downloading the manifest
	// as a separate release asset (some goreleaser configs don't bundle it).
	manifestPath := filepath.Join(tmpDir, "hyperax-plugin.yaml")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		// Try downloading manifest as a separate release asset.
		manifestAssetURL := findAssetURL(release.Assets, "hyperax-plugin.yaml")
		if manifestAssetURL == "" {
			return nil, fmt.Errorf("plugin.FetchRelease: manifest not found in archive and not available as release asset")
		}
		logger.Info("manifest not in archive, downloading as separate asset", "plugin", src.Repo)
		manifestData, err = ghGet(ctx, client, manifestAssetURL, ghToken)
		if err != nil {
			return nil, fmt.Errorf("plugin.FetchRelease: download manifest asset: %w", err)
		}
		// Write it into the temp dir so it gets copied to the final destination.
		if writeErr := os.WriteFile(manifestPath, manifestData, 0o644); writeErr != nil {
			return nil, fmt.Errorf("plugin.FetchRelease: write manifest to temp dir: %w", writeErr)
		}
	}

	manifest, err := ParseManifestFromBytes(manifestData)
	if err != nil {
		return nil, fmt.Errorf("plugin.FetchRelease: parse extracted manifest: %w", err)
	}

	// The GitHub release tag is the authoritative version for distributed plugins.
	// The manifest YAML inside the archive may have a stale version field if the
	// plugin author forgot to bump it before tagging. Override with the release tag.
	releaseVersion := strings.TrimPrefix(release.TagName, "v")
	if manifest.Version != releaseVersion {
		logger.Warn("manifest version does not match release tag, using release tag version",
			"plugin", manifest.Name,
			"manifest_version", manifest.Version,
			"release_tag", release.TagName,
		)
		manifest.Version = releaseVersion

		// Rewrite the manifest on disk so the corrected version persists across
		// server restarts (Discover reads the manifest from the plugin directory).
		corrected, marshalErr := yaml.Marshal(manifest)
		if marshalErr == nil {
			_ = os.WriteFile(manifestPath, corrected, 0o644)
		}
	}

	// Move extracted contents to the final destination directory.
	destDir := filepath.Join(targetDir, manifest.Name)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, fmt.Errorf("plugin.FetchRelease: create plugin dir: %w", err)
	}

	// Copy all files from tmpDir to destDir.
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return nil, fmt.Errorf("plugin.FetchRelease: read temp dir: %w", err)
	}
	for _, entry := range entries {
		srcPath := filepath.Join(tmpDir, entry.Name())
		dstPath := filepath.Join(destDir, entry.Name())
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return nil, fmt.Errorf("plugin.FetchRelease: read extracted file %s: %w", entry.Name(), err)
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("plugin.FetchRelease: stat extracted file %s: %w", entry.Name(), err)
		}
		if err := os.WriteFile(dstPath, data, info.Mode()); err != nil {
			return nil, fmt.Errorf("plugin.FetchRelease: write file %s: %w", entry.Name(), err)
		}
	}

	logger.Info("plugin fetched from GitHub",
		"plugin", manifest.Name,
		"version", release.TagName,
		"platform", runtime.GOOS+"/"+runtime.GOARCH,
		"archive", assetName,
		"dest", destDir,
	)

	return manifest, nil
}

// findPlatformAsset locates the correct archive asset for the current platform.
// It tries exact goreleaser naming patterns first, then falls back to substring matching.
// Handles both underscore-separated (default goreleaser) and dash-separated naming.
// Returns the asset name or "" if no match is found.
func findPlatformAsset(assets []ghAsset, repo, version, goos, goarch string) string {
	// Exact patterns that goreleaser produces with various name_template configs.
	patterns := []string{
		// Default goreleaser: {project}_{version}_{os}_{arch}
		fmt.Sprintf("%s_%s_%s_%s.tar.gz", repo, version, goos, goarch),
		fmt.Sprintf("%s_%s_%s_%s.zip", repo, version, goos, goarch),
		// Dash-separated without version: {project}-{os}-{arch}
		fmt.Sprintf("%s-%s-%s.tar.gz", repo, goos, goarch),
		fmt.Sprintf("%s-%s-%s.zip", repo, goos, goarch),
		// Dash-separated with version: {project}-{version}-{os}-{arch}
		fmt.Sprintf("%s-%s-%s-%s.tar.gz", repo, version, goos, goarch),
		fmt.Sprintf("%s-%s-%s-%s.zip", repo, version, goos, goarch),
	}

	for _, p := range patterns {
		for _, a := range assets {
			if a.Name == p {
				return a.Name
			}
		}
	}

	// Fallback: substring match on os+arch with recognized archive extensions.
	// Check both underscore and dash separators.
	needles := []string{goos + "_" + goarch, goos + "-" + goarch}
	for _, needle := range needles {
		for _, a := range assets {
			if strings.Contains(a.Name, needle) &&
				(strings.HasSuffix(a.Name, ".tar.gz") || strings.HasSuffix(a.Name, ".tgz") || strings.HasSuffix(a.Name, ".zip")) {
				return a.Name
			}
		}
	}

	return ""
}

// extractTarGz extracts a gzip-compressed tar archive from bytes to the destination directory.
// It applies path traversal protection and preserves file permissions.
func extractTarGz(data []byte, destDir string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("plugin.extractTarGz: open gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("plugin.extractTarGz: read tar entry: %w", err)
		}

		// Prevent path traversal.
		name := filepath.Clean(hdr.Name)
		if strings.HasPrefix(name, "..") {
			continue
		}

		target := filepath.Join(destDir, name)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			// Ensure parent directory exists.
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}

			// Preserve file mode from the archive; default to 0o644 if unset.
			mode := hdr.FileInfo().Mode()
			if mode == 0 {
				mode = 0o644
			}

			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}

			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return err
			}
			_ = out.Close()
		}
	}

	return nil
}

// ghGet performs a GET request with optional GitHub token authentication.
// For GitHub API URLs (api.github.com), it sends the appropriate JSON accept header.
// For download URLs (github.com/...download...), it sends a plain request that
// follows redirects to the CDN.
func ghGet(ctx context.Context, client *http.Client, url, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	// Use appropriate Accept header based on URL type.
	if strings.Contains(url, "api.github.com") {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	// For download URLs (browser_download_url), no special Accept header needed —
	// Go's http.Client follows 302 redirects to the CDN automatically.

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	// Limit to 100 MB for artifacts.
	return io.ReadAll(io.LimitReader(resp.Body, 100<<20))
}

// findAssetURL returns the download URL for a named asset, or "".
func findAssetURL(assets []ghAsset, name string) string {
	for _, a := range assets {
		if a.Name == name {
			return a.BrowserDownloadURL
		}
	}
	return ""
}

// extractZip extracts a zip archive from bytes to the destination directory.
func extractZip(data []byte, destDir string) error {
	// Write to temp file for zip.OpenReader to work with.
	tmpFile, err := os.CreateTemp("", "hyperax-plugin-*.zip")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return err
	}
	_ = tmpFile.Close()

	r, err := zip.OpenReader(tmpPath)
	if err != nil {
		return fmt.Errorf("plugin.extractZip: open zip: %w", err)
	}
	defer func() { _ = r.Close() }()

	for _, f := range r.File {
		// Prevent path traversal.
		name := filepath.Clean(f.Name)
		if strings.HasPrefix(name, "..") {
			continue
		}

		target := filepath.Join(destDir, name)

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}

		// Ensure parent dir exists.
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			_ = rc.Close()
			return err
		}

		if _, err := io.Copy(out, rc); err != nil {
			_ = out.Close()
			_ = rc.Close()
			return err
		}

		_ = out.Close()
		_ = rc.Close()
	}

	return nil
}
