package plugin

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/hyperax/hyperax/pkg/types"
	"golang.org/x/mod/semver"
	"gopkg.in/yaml.v3"
)

// supportedAPIMajor is the major API version that this Hyperax build supports.
const supportedAPIMajor = "1"

// validPluginTypes is the set of recognised plugin type strings.
var validPluginTypes = map[types.PluginType]bool{
	types.PluginTypeWasm:    true,
	types.PluginTypeMCP:     true,
	types.PluginTypeHTTP:    true,
	types.PluginTypeNative:  true,
	types.PluginTypeService: true,
}

// validEnvNameRe matches environment variable names (uppercase letters, digits, underscores).
var validEnvNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ParseManifest reads a hyperax-plugin.yaml file and unmarshals it into a
// PluginManifest. Returns a descriptive error if the file cannot be read
// or contains invalid YAML.
func ParseManifest(path string) (*types.PluginManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("plugin.ParseManifest: read %s: %w", path, err)
	}

	var m types.PluginManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("plugin.ParseManifest: parse %s: %w", path, err)
	}

	return &m, nil
}

// ParseManifestFromBytes parses a hyperax-plugin.yaml from raw bytes.
// Used when the manifest is fetched from a remote URL.
func ParseManifestFromBytes(data []byte) (*types.PluginManifest, error) {
	var m types.PluginManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("plugin.ParseManifestFromBytes: %w", err)
	}
	return &m, nil
}

// ValidateManifest checks that a parsed PluginManifest contains all required
// fields and that their values are within acceptable ranges.
//
// hyperaxVersion is the running binary version (e.g. "1.2.0" or "dev").
// When "dev", version compatibility checks are skipped.
//
// Validation rules:
//   - Name must be non-empty.
//   - Version must be non-empty.
//   - Type must be one of: wasm, mcp, http, native.
//   - At least one tool must be declared.
//   - Each tool must have a non-empty name and description.
//   - min_hyperax_version is enforced via semver (unless "dev").
//   - api_version major must match supported API major.
//   - Integration must be a recognised category (defaults to "tooling").
//   - Variables must have valid types and env-compatible names.
//   - Legacy Env entries are auto-converted to Variables if Variables is empty.
func ValidateManifest(m *types.PluginManifest, hyperaxVersion string) error {
	if m.Name == "" {
		return fmt.Errorf("manifest validation: name is required")
	}
	if m.Version == "" {
		return fmt.Errorf("manifest validation: version is required for plugin %q", m.Name)
	}
	if !validPluginTypes[m.Type] {
		return fmt.Errorf("manifest validation: invalid plugin type %q for plugin %q (must be wasm, mcp, http, or native)", m.Type, m.Name)
	}
	if len(m.Tools) == 0 {
		return fmt.Errorf("manifest validation: plugin %q must declare at least one tool", m.Name)
	}
	for i, t := range m.Tools {
		if t.Name == "" {
			return fmt.Errorf("manifest validation: tool[%d] in plugin %q has empty name", i, m.Name)
		}
		if t.Description == "" {
			return fmt.Errorf("manifest validation: tool %q in plugin %q has empty description", t.Name, m.Name)
		}
	}

	// Version compatibility checks (skipped for dev builds).
	if hyperaxVersion != "" && hyperaxVersion != "dev" {
		if err := checkVersionCompat(m, hyperaxVersion); err != nil {
			return err
		}
	}

	// Default integration to "tooling" for backward compat.
	if m.Integration == "" {
		m.Integration = types.PluginIntegrationTooling
	}
	if !types.ValidPluginIntegrations[m.Integration] {
		return fmt.Errorf("manifest validation: invalid integration %q for plugin %q (must be channel, tooling, secret_provider, or sensor)", m.Integration, m.Name)
	}

	// Auto-convert legacy Env to Variables if Variables is empty.
	if len(m.Variables) == 0 && len(m.Env) > 0 {
		m.Variables = make([]types.PluginVariable, len(m.Env))
		for i, e := range m.Env {
			m.Variables[i] = types.PluginVariable{
				Name:        e.Name,
				Type:        types.PluginVarString,
				Required:    e.Required,
				Default:     e.Default,
				Description: e.Description,
			}
		}
	}

	// Validate variables.
	for i, v := range m.Variables {
		if v.Name == "" {
			return fmt.Errorf("manifest validation: variable[%d] in plugin %q has empty name", i, m.Name)
		}
		envName := v.EnvName
		if envName == "" {
			envName = v.Name
		}
		if !validEnvNameRe.MatchString(envName) {
			return fmt.Errorf("manifest validation: variable %q in plugin %q has invalid env name %q", v.Name, m.Name, envName)
		}
		if v.Type != "" && !types.ValidPluginVariableTypes[v.Type] {
			return fmt.Errorf("manifest validation: variable %q in plugin %q has invalid type %q", v.Name, m.Name, v.Type)
		}
		// Default type to string if empty.
		if v.Type == "" {
			m.Variables[i].Type = types.PluginVarString
		}
	}

	return nil
}

// checkVersionCompat validates min_hyperax_version and api_version requirements.
func checkVersionCompat(m *types.PluginManifest, hyperaxVersion string) error {
	// Ensure both versions have the "v" prefix for semver comparison.
	hv := ensureVPrefix(hyperaxVersion)

	if m.MinHyperaxVer != "" {
		minV := ensureVPrefix(m.MinHyperaxVer)
		if semver.IsValid(hv) && semver.IsValid(minV) {
			if semver.Compare(hv, minV) < 0 {
				return fmt.Errorf("plugin %q requires Hyperax >= %s, running %s",
					m.Name, m.MinHyperaxVer, hyperaxVersion)
			}
		}
	}

	if m.APIVersion != "" {
		pluginMajor := strings.SplitN(m.APIVersion, ".", 2)[0]
		if pluginMajor != supportedAPIMajor {
			return fmt.Errorf("plugin %q requires API v%s, Hyperax supports v%s",
				m.Name, m.APIVersion, supportedAPIMajor)
		}
	}

	return nil
}

// ensureVPrefix adds a "v" prefix for semver compatibility if missing.
func ensureVPrefix(ver string) string {
	if ver == "" {
		return ""
	}
	if !strings.HasPrefix(ver, "v") {
		return "v" + ver
	}
	return ver
}
