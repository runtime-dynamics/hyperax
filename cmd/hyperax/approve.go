package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// approveCmd creates the `hyperax approve` command for interactively
// approving or rejecting pending guard actions from a running Hyperax instance.
func approveCmd() *cobra.Command {
	var addr string

	cmd := &cobra.Command{
		Use:   "approve",
		Short: "Interactively approve or reject pending guard actions",
		Long: `Connects to a running Hyperax instance and presents pending guard actions
for interactive approval or rejection. Polls every 3 seconds for new actions.

The server address can be set via --addr flag, HYPERAX_ADDR environment variable,
or defaults to http://localhost:9090.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if addr == "" {
				addr = os.Getenv("HYPERAX_ADDR")
			}
			if addr == "" {
				addr = "http://localhost:9090"
			}
			addr = strings.TrimRight(addr, "/")

			return runApproveLoop(addr)
		},
	}

	cmd.Flags().StringVarP(&addr, "addr", "a", "", "Hyperax server address (default: $HYPERAX_ADDR or http://localhost:9090)")
	return cmd
}

// runApproveLoop is the main polling loop for the approve command. It fetches
// pending guard actions, presents them interactively, and processes decisions
// until the user sends Ctrl+C (EOF on stdin).
func runApproveLoop(addr string) error {
	scanner := bufio.NewScanner(os.Stdin)
	requestID := 0

	fmt.Printf("Connecting to %s...\n\n", addr)

	for {
		requestID++
		actions, err := fetchPendingActions(addr, requestID)
		if err != nil {
			fmt.Printf("Error fetching actions: %v\n", err)
			fmt.Println("Retrying in 3 seconds...")
			time.Sleep(3 * time.Second)
			continue
		}

		if len(actions) == 0 {
			fmt.Println("No pending actions. Waiting for new actions... (Ctrl+C to exit)")
			time.Sleep(3 * time.Second)
			continue
		}

		fmt.Printf("Found %d pending action(s):\n\n", len(actions))

		for _, action := range actions {
			displayAction(action)

			fmt.Print("[A]pprove / [R]eject / [S]kip? ")
			if !scanner.Scan() {
				// EOF — user pressed Ctrl+D or stdin closed.
				fmt.Println()
				return nil
			}
			choice := strings.TrimSpace(strings.ToLower(scanner.Text()))

			switch choice {
			case "a", "approve":
				notes := promptNotes(scanner)
				requestID++
				if err := submitDecision(addr, requestID, action.ID, "approve_action", notes); err != nil {
					fmt.Printf("  Error: %v\n", err)
				} else {
					fmt.Println("  Approved.")
				}
			case "r", "reject":
				notes := promptNotes(scanner)
				requestID++
				if err := submitDecision(addr, requestID, action.ID, "reject_action", notes); err != nil {
					fmt.Printf("  Error: %v\n", err)
				} else {
					fmt.Println("  Rejected.")
				}
			case "s", "skip", "":
				fmt.Println("  Skipped.")
			default:
				fmt.Printf("  Unknown choice %q, skipping.\n", choice)
			}
			fmt.Println()
		}
	}
}

// cliGuardAction mirrors the JSON structure returned by the get_pending_actions
// MCP tool. Each field maps to the guard action metadata.
type cliGuardAction struct {
	ID            string `json:"id"`
	ToolName      string `json:"tool_name"`
	ToolAction    string `json:"tool_action"`
	ToolParams    string `json:"tool_params"`
	GuardName     string `json:"guard_name"`
	CallerPersona string `json:"caller_persona"`
	ExpiresAt     string `json:"expires_at"`
}

// mcpRPCResponse is the minimal JSON-RPC 2.0 response envelope used for
// parsing MCP tool call results.
type mcpRPCResponse struct {
	Result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	} `json:"result"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// fetchPendingActions calls the get_pending_actions MCP tool on the running
// Hyperax instance and returns the list of pending guard actions.
func fetchPendingActions(addr string, id int) ([]cliGuardAction, error) {
	payload := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"get_pending_actions","arguments":{}}}`,
		id,
	)

	resp, err := http.Post(addr+"/mcp", "application/json", bytes.NewBufferString(payload))
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", addr, err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "warn: failed to close response body: %v\n", closeErr)
		}
	}()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var rpcResp mcpRPCResponse
	if err := json.Unmarshal(data, &rpcResp); err != nil {
		return nil, fmt.Errorf("parse JSON-RPC response: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("server error [%d]: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	if len(rpcResp.Result.Content) == 0 {
		return nil, nil
	}

	var result struct {
		Actions []cliGuardAction `json:"actions"`
	}
	if err := json.Unmarshal([]byte(rpcResp.Result.Content[0].Text), &result); err != nil {
		return nil, fmt.Errorf("parse actions payload: %w", err)
	}

	return result.Actions, nil
}

// displayAction prints a human-readable summary of a pending guard action
// to stdout, including time remaining before expiration.
func displayAction(a cliGuardAction) {
	params := a.ToolParams
	if len(params) > 120 {
		params = params[:117] + "..."
	}

	remaining := "expired"
	if exp, err := time.Parse(time.RFC3339, a.ExpiresAt); err == nil {
		d := time.Until(exp)
		if d > 0 {
			remaining = fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
		}
	}

	fmt.Printf("  ID:       %s\n", a.ID)
	fmt.Printf("  Tool:     %s (%s)\n", a.ToolName, a.ToolAction)
	fmt.Printf("  Guard:    %s\n", a.GuardName)
	fmt.Printf("  Caller:   %s\n", a.CallerPersona)
	fmt.Printf("  Params:   %s\n", params)
	fmt.Printf("  Timeout:  %s remaining\n", remaining)
	fmt.Println()
}

// promptNotes asks the user for optional notes to attach to an approval or
// rejection decision. Returns an empty string if the user presses Enter.
func promptNotes(scanner *bufio.Scanner) string {
	fmt.Print("  Notes (optional, press Enter to skip): ")
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}

// submitDecision calls the approve_action or reject_action MCP tool on the
// running Hyperax instance with the given action ID and optional notes.
func submitDecision(addr string, id int, actionID, toolName, notes string) error {
	args := map[string]string{"id": actionID}
	if notes != "" {
		args["notes"] = notes
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("marshal arguments: %w", err)
	}

	payload := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"%s","arguments":%s}}`,
		id, toolName, argsJSON,
	)

	resp, err := http.Post(addr+"/mcp", "application/json", bytes.NewBufferString(payload))
	if err != nil {
		return fmt.Errorf("connect to %s: %w", addr, err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "warn: failed to close response body: %v\n", closeErr)
		}
	}()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	var rpcResp mcpRPCResponse
	if err := json.Unmarshal(data, &rpcResp); err != nil {
		return fmt.Errorf("parse JSON-RPC response: %w", err)
	}
	if rpcResp.Error != nil {
		return fmt.Errorf("server error [%d]: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return nil
}
