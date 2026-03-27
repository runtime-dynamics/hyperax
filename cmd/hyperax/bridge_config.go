package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

func bridgeConfigCmd() *cobra.Command {
	var (
		url       string
		name      string
		workspace string
		output    string
	)

	cmd := &cobra.Command{
		Use:   "bridge-config",
		Short: "Generate Claude Code MCP config for the hyperax-bridge channel",
		Long: `Generates the .mcp.json configuration snippet needed to connect Claude Code
to this Hyperax instance via the hyperax-bridge channel.

The bridge enables Hyperax to push events (task assignments, messages, permission
relay requests) directly into your Claude Code session.

Examples:
  # Print the config to stdout
  hyperax bridge-config --url http://localhost:9090

  # Write directly to .mcp.json in the current directory
  hyperax bridge-config --url http://localhost:9090 --output .mcp.json

  # Custom session name and workspace binding
  hyperax bridge-config --url http://my-server:9090 --name "backend-dev" --workspace hyperax`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if url == "" {
				url = "http://localhost:9090"
			}

			bridgePath, err := findBridgeBinary()
			if err != nil {
				return fmt.Errorf("locate hyperax-bridge binary: %w", err)
			}

			bridgeArgs := []string{"--url", url}
			if name != "" {
				bridgeArgs = append(bridgeArgs, "--name", name)
			}
			if workspace != "" {
				bridgeArgs = append(bridgeArgs, "--workspace", workspace)
			}

			config := map[string]any{
				"mcpServers": map[string]any{
					"hyperax": map[string]any{
						"command": bridgePath,
						"args":    bridgeArgs,
					},
				},
			}

			data, err := json.MarshalIndent(config, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal config: %w", err)
			}

			if output != "" {
				return writeOrMergeMCPConfig(output, bridgePath, bridgeArgs)
			}

			fmt.Println(string(data))
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "Add this to your .mcp.json, then start Claude Code with:")
			fmt.Fprintln(os.Stderr, "  claude --channels server:hyperax")
			return nil
		},
	}

	cmd.Flags().StringVar(&url, "url", "http://localhost:9090", "Hyperax server URL the bridge connects back to")
	cmd.Flags().StringVar(&name, "name", "", "Session name (shown in the dashboard)")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace ID to bind the session to")
	cmd.Flags().StringVar(&output, "output", "", "Write config to this file (merges if file exists)")

	return cmd
}

// findBridgeBinary looks for the hyperax-bridge binary in common locations.
func findBridgeBinary() (string, error) {
	// Check next to the current hyperax binary
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		candidate := filepath.Join(dir, "hyperax-bridge")
		if runtime.GOOS == "windows" {
			candidate += ".exe"
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	// Check PATH
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		candidate := filepath.Join(dir, "hyperax-bridge")
		if runtime.GOOS == "windows" {
			candidate += ".exe"
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	// Fallback: assume it's in PATH
	return "hyperax-bridge", nil
}

// writeOrMergeMCPConfig writes the hyperax entry into an MCP config file,
// merging with existing entries if the file already exists.
func writeOrMergeMCPConfig(path, bridgePath string, bridgeArgs []string) error {
	existing := make(map[string]any)

	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			return fmt.Errorf("parse existing %s: %w", path, err)
		}
	}

	servers, ok := existing["mcpServers"].(map[string]any)
	if !ok {
		servers = make(map[string]any)
	}

	servers["hyperax"] = map[string]any{
		"command": bridgePath,
		"args":    bridgeArgs,
	}
	existing["mcpServers"] = servers

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	fmt.Fprintf(os.Stderr, "Wrote hyperax bridge config to %s\n", path)
	fmt.Fprintln(os.Stderr, "Start Claude Code with:")
	fmt.Fprintln(os.Stderr, "  claude --channels server:hyperax")
	return nil
}
