package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/storage"
	"github.com/hyperax/hyperax/pkg/types"
)

// ---------------------------------------------------------------------------
// Test helpers (log-specific)
// ---------------------------------------------------------------------------

// mockConfigRepo implements repo.ConfigRepo for testing runtime state tools.
type mockConfigRepo struct {
	values map[string]string // key -> value (global scope only)
}

func (m *mockConfigRepo) GetValue(_ context.Context, key string, _ types.ConfigScope) (string, error) {
	v, ok := m.values[key]
	if !ok {
		return "", fmt.Errorf("key %q not found", key)
	}
	return v, nil
}

func (m *mockConfigRepo) SetValue(_ context.Context, _, _ string, _ types.ConfigScope, _ string) error {
	return nil
}

func (m *mockConfigRepo) GetKeyMeta(_ context.Context, _ string) (*types.ConfigKeyMeta, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockConfigRepo) ListKeys(_ context.Context) ([]types.ConfigKeyMeta, error) {
	return nil, nil
}

func (m *mockConfigRepo) ListValues(_ context.Context, _ types.ConfigScope) ([]types.ConfigValue, error) {
	var out []types.ConfigValue
	for k, v := range m.values {
		out = append(out, types.ConfigValue{
			Key:       k,
			Value:     v,
			ScopeType: "global",
		})
	}
	return out, nil
}

func (m *mockConfigRepo) GetHistory(_ context.Context, _ string, _ int) ([]types.ConfigChange, error) {
	return nil, nil
}

func (m *mockConfigRepo) UpsertKeyMeta(_ context.Context, _ *types.ConfigKeyMeta) error {
	return nil
}

// setupLogHandler creates a LogHandler backed by a temp workspace and optional
// mock config repo. Returns the handler, workspace root, and context.
func setupLogHandler(t *testing.T, configValues map[string]string) (*ObservabilityHandler, string, context.Context) {
	t.Helper()
	root := t.TempDir()

	store := &storage.Store{
		Workspaces: &mockWorkspaceRepo{
			workspaces: map[string]*types.WorkspaceInfo{
				"test": {ID: "ws-test", Name: "test", RootPath: root},
			},
		},
	}

	if configValues != nil {
		store.Config = &mockConfigRepo{values: configValues}
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := NewObservabilityHandler(store, logger)
	return handler, root, context.Background()
}

// ---------------------------------------------------------------------------
// list_logs tests
// ---------------------------------------------------------------------------

func TestListLogs_WorkspaceLogs(t *testing.T) {
	h, root, ctx := setupLogHandler(t, nil)

	// Create .hyperax/logs/ with some log files.
	logsDir := filepath.Join(root, ".hyperax", "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, root, ".hyperax/logs/app.log", "line1\nline2\n")
	writeFile(t, root, ".hyperax/logs/error.log", "err\n")

	type args struct {
		WorkspaceID string `json:"workspace_id"`
	}
	result := callTool(t, h.listLogs, ctx, args{WorkspaceID: "test"})

	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	text := resultText(result)
	if !strings.Contains(text, "app.log") {
		t.Errorf("expected app.log in result, got: %s", text)
	}
	if !strings.Contains(text, "error.log") {
		t.Errorf("expected error.log in result, got: %s", text)
	}
}

func TestListLogs_NoFiles(t *testing.T) {
	h, _, ctx := setupLogHandler(t, nil)

	// Use a workspace that has no .hyperax/logs/ directory.
	type args struct {
		WorkspaceID string `json:"workspace_id"`
	}
	result := callTool(t, h.listLogs, ctx, args{WorkspaceID: "test"})

	text := resultText(result)
	// The workspace has no logs and /var/log/ may or may not exist on the
	// test machine. At minimum, no error should be raised.
	if result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
}

func TestListLogs_InvalidWorkspace(t *testing.T) {
	h, _, ctx := setupLogHandler(t, nil)

	type args struct {
		WorkspaceID string `json:"workspace_id"`
	}
	result := callTool(t, h.listLogs, ctx, args{WorkspaceID: "nonexistent"})

	if !result.IsError {
		t.Errorf("expected error for invalid workspace, got: %s", resultText(result))
	}
	if !strings.Contains(resultText(result), "not found") {
		t.Errorf("expected 'not found' in error, got: %s", resultText(result))
	}
}

func TestListLogs_AllWorkspaces(t *testing.T) {
	h, root, ctx := setupLogHandler(t, nil)

	// Create logs in the test workspace.
	writeFile(t, root, ".hyperax/logs/scan.log", "data\n")

	// Call without workspace_id to scan all workspaces.
	result := callTool(t, h.listLogs, ctx, struct{}{})

	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	text := resultText(result)
	if !strings.Contains(text, "scan.log") {
		t.Errorf("expected scan.log from all-workspace scan, got: %s", text)
	}
}

// ---------------------------------------------------------------------------
// get_log_lines tests
// ---------------------------------------------------------------------------

func TestGetLogLines_DefaultCount(t *testing.T) {
	h, root, ctx := setupLogHandler(t, nil)

	// Create a file with 100 lines.
	var sb strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	logPath := filepath.Join(root, "test.log")
	if err := os.WriteFile(logPath, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	type args struct {
		Path string `json:"path"`
	}
	result := callTool(t, h.getLogLines, ctx, args{Path: logPath})

	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	text := resultText(result)
	// Default is 50 lines, so line 51 should be present but line 50 should not.
	var decoded string
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !strings.Contains(decoded, "line 100") {
		t.Errorf("expected 'line 100', got: %s", decoded)
	}
	if !strings.Contains(decoded, "line 51") {
		t.Errorf("expected 'line 51', got: %s", decoded)
	}
	if strings.Contains(decoded, "line 50\n") {
		t.Errorf("line 50 should not be present in last 50, got: %s", decoded)
	}
}

func TestGetLogLines_CustomCount(t *testing.T) {
	h, root, ctx := setupLogHandler(t, nil)

	var sb strings.Builder
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	logPath := filepath.Join(root, "custom.log")
	if err := os.WriteFile(logPath, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	type args struct {
		Path  string `json:"path"`
		Lines int    `json:"lines"`
	}
	result := callTool(t, h.getLogLines, ctx, args{Path: logPath, Lines: 5})

	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	var decoded string
	if err := json.Unmarshal([]byte(resultText(result)), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !strings.Contains(decoded, "line 20") {
		t.Errorf("expected 'line 20', got: %s", decoded)
	}
	if !strings.Contains(decoded, "line 16") {
		t.Errorf("expected 'line 16', got: %s", decoded)
	}
	if strings.Contains(decoded, "line 15\n") {
		t.Errorf("line 15 should not be present in last 5, got: %s", decoded)
	}
}

func TestGetLogLines_FileNotFound(t *testing.T) {
	h, root, ctx := setupLogHandler(t, nil)

	type args struct {
		Path string `json:"path"`
	}
	result := callTool(t, h.getLogLines, ctx, args{Path: filepath.Join(root, "missing.log")})

	if !result.IsError {
		t.Errorf("expected error for missing file, got: %s", resultText(result))
	}
	if !strings.Contains(resultText(result), "not found") {
		t.Errorf("expected 'not found' message, got: %s", resultText(result))
	}
}

func TestGetLogLines_EmptyFile(t *testing.T) {
	h, root, ctx := setupLogHandler(t, nil)

	logPath := filepath.Join(root, "empty.log")
	if err := os.WriteFile(logPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	type args struct {
		Path string `json:"path"`
	}
	result := callTool(t, h.getLogLines, ctx, args{Path: logPath})

	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	var decoded string
	if err := json.Unmarshal([]byte(resultText(result)), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded != "(empty file)" {
		t.Errorf("expected '(empty file)', got: %s", decoded)
	}
}

func TestGetLogLines_PathRequired(t *testing.T) {
	h, _, ctx := setupLogHandler(t, nil)

	type args struct {
		Path string `json:"path"`
	}
	result := callTool(t, h.getLogLines, ctx, args{Path: ""})

	if !result.IsError {
		t.Errorf("expected error for empty path, got: %s", resultText(result))
	}
}

func TestGetLogLines_WorkspaceSandbox(t *testing.T) {
	h, root, ctx := setupLogHandler(t, nil)

	// Create a log file inside the workspace.
	logPath := filepath.Join(root, "inside.log")
	if err := os.WriteFile(logPath, []byte("safe\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	type args struct {
		Path        string `json:"path"`
		WorkspaceID string `json:"workspace_id"`
	}

	// Should succeed for path inside workspace.
	result := callTool(t, h.getLogLines, ctx, args{Path: logPath, WorkspaceID: "test"})
	if result.IsError {
		t.Fatalf("expected success for in-workspace path, got: %s", resultText(result))
	}

	// Should fail for path outside workspace.
	result = callTool(t, h.getLogLines, ctx, args{Path: "/etc/passwd", WorkspaceID: "test"})
	if !result.IsError {
		t.Errorf("expected error for path outside workspace, got: %s", resultText(result))
	}
	if !strings.Contains(resultText(result), "escapes workspace") {
		t.Errorf("expected 'escapes workspace' message, got: %s", resultText(result))
	}
}

// ---------------------------------------------------------------------------
// get_log_errors tests
// ---------------------------------------------------------------------------

func TestGetLogErrors_DefaultPattern(t *testing.T) {
	h, root, ctx := setupLogHandler(t, nil)

	content := `2024-01-01 INFO Starting app
2024-01-01 ERROR Connection refused
2024-01-01 DEBUG heartbeat
2024-01-01 WARN Disk usage high
2024-01-01 INFO Request served
2024-01-01 FATAL Out of memory
2024-01-01 PANIC nil pointer
`
	logPath := filepath.Join(root, "app.log")
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	type args struct {
		Path string `json:"path"`
	}
	result := callTool(t, h.getLogErrors, ctx, args{Path: logPath})

	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	var decoded string
	if err := json.Unmarshal([]byte(resultText(result)), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Should contain ERROR, WARN, FATAL, PANIC lines.
	for _, keyword := range []string{"ERROR", "WARN", "FATAL", "PANIC"} {
		if !strings.Contains(decoded, keyword) {
			t.Errorf("expected %s in output, got: %s", keyword, decoded)
		}
	}

	// Should NOT contain INFO or DEBUG lines.
	if strings.Contains(decoded, "Starting app") {
		t.Errorf("INFO line should not appear: %s", decoded)
	}
	if strings.Contains(decoded, "heartbeat") {
		t.Errorf("DEBUG line should not appear: %s", decoded)
	}
}

func TestGetLogErrors_CustomPattern(t *testing.T) {
	h, root, ctx := setupLogHandler(t, nil)

	content := `alpha one
beta two
gamma three
alpha four
delta five
`
	logPath := filepath.Join(root, "custom.log")
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	type args struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
	}
	result := callTool(t, h.getLogErrors, ctx, args{Path: logPath, Pattern: "alpha"})

	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	var decoded string
	if err := json.Unmarshal([]byte(resultText(result)), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !strings.Contains(decoded, "alpha one") {
		t.Errorf("expected 'alpha one', got: %s", decoded)
	}
	if !strings.Contains(decoded, "alpha four") {
		t.Errorf("expected 'alpha four', got: %s", decoded)
	}
	if strings.Contains(decoded, "beta") {
		t.Errorf("beta should not match, got: %s", decoded)
	}
}

func TestGetLogErrors_InvalidPattern(t *testing.T) {
	h, root, ctx := setupLogHandler(t, nil)

	logPath := filepath.Join(root, "dummy.log")
	if err := os.WriteFile(logPath, []byte("line\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	type args struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
	}
	result := callTool(t, h.getLogErrors, ctx, args{Path: logPath, Pattern: "[invalid"})

	if !result.IsError {
		t.Errorf("expected error for invalid regex, got: %s", resultText(result))
	}
	if !strings.Contains(resultText(result), "invalid pattern") {
		t.Errorf("expected 'invalid pattern' message, got: %s", resultText(result))
	}
}

func TestGetLogErrors_NoMatches(t *testing.T) {
	h, root, ctx := setupLogHandler(t, nil)

	content := "INFO all good\nDEBUG tick\n"
	logPath := filepath.Join(root, "clean.log")
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	type args struct {
		Path string `json:"path"`
	}
	result := callTool(t, h.getLogErrors, ctx, args{Path: logPath})

	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	var decoded string
	if err := json.Unmarshal([]byte(resultText(result)), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded != "No matching lines found." {
		t.Errorf("expected 'No matching lines found.', got: %s", decoded)
	}
}

func TestGetLogErrors_FileNotFound(t *testing.T) {
	h, root, ctx := setupLogHandler(t, nil)

	type args struct {
		Path string `json:"path"`
	}
	result := callTool(t, h.getLogErrors, ctx, args{Path: filepath.Join(root, "ghost.log")})

	if !result.IsError {
		t.Errorf("expected error for missing file, got: %s", resultText(result))
	}
}

// ---------------------------------------------------------------------------
// list_runtime_states tests
// ---------------------------------------------------------------------------

func TestListRuntimeStates_WithEntries(t *testing.T) {
	configValues := map[string]string{
		"runtime_state.disk":   "df -h /",
		"runtime_state.uptime": "uptime",
		"other.key":            "ignored",
	}
	h, _, ctx := setupLogHandler(t, configValues)

	result := callTool(t, h.listRuntimeStates, ctx, struct{}{})

	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	text := resultText(result)
	if !strings.Contains(text, "disk") {
		t.Errorf("expected 'disk' state, got: %s", text)
	}
	if !strings.Contains(text, "uptime") {
		t.Errorf("expected 'uptime' state, got: %s", text)
	}
	// Non-runtime_state keys should be excluded.
	if strings.Contains(text, "other.key") {
		t.Errorf("non-runtime_state key should not appear, got: %s", text)
	}
}

func TestListRuntimeStates_NoEntries(t *testing.T) {
	configValues := map[string]string{
		"some.other.key": "value",
	}
	h, _, ctx := setupLogHandler(t, configValues)

	result := callTool(t, h.listRuntimeStates, ctx, struct{}{})

	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	var decoded string
	if err := json.Unmarshal([]byte(resultText(result)), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded != "No runtime state getters configured." {
		t.Errorf("expected empty message, got: %s", decoded)
	}
}

func TestListRuntimeStates_NoConfigRepo(t *testing.T) {
	h, _, ctx := setupLogHandler(t, nil) // nil config means store.Config == nil

	result := callTool(t, h.listRuntimeStates, ctx, struct{}{})

	if !result.IsError {
		t.Errorf("expected error when config repo is nil, got: %s", resultText(result))
	}
	if !strings.Contains(resultText(result), "config repo not available") {
		t.Errorf("expected 'config repo not available', got: %s", resultText(result))
	}
}

// ---------------------------------------------------------------------------
// get_runtime_state tests
// ---------------------------------------------------------------------------

func TestGetRuntimeState_Success(t *testing.T) {
	configValues := map[string]string{
		"runtime_state.echo_test": "echo hello_runtime",
	}
	h, _, ctx := setupLogHandler(t, configValues)

	type args struct {
		Name string `json:"name"`
	}
	result := callTool(t, h.getRuntimeState, ctx, args{Name: "echo_test"})

	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	var decoded string
	if err := json.Unmarshal([]byte(resultText(result)), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded != "hello_runtime" {
		t.Errorf("expected 'hello_runtime', got: %s", decoded)
	}
}

func TestGetRuntimeState_NotFound(t *testing.T) {
	configValues := map[string]string{}
	h, _, ctx := setupLogHandler(t, configValues)

	type args struct {
		Name string `json:"name"`
	}
	result := callTool(t, h.getRuntimeState, ctx, args{Name: "nonexistent"})

	if !result.IsError {
		t.Errorf("expected error for missing state, got: %s", resultText(result))
	}
	if !strings.Contains(resultText(result), "not found") {
		t.Errorf("expected 'not found' in error, got: %s", resultText(result))
	}
}

func TestGetRuntimeState_NameRequired(t *testing.T) {
	configValues := map[string]string{}
	h, _, ctx := setupLogHandler(t, configValues)

	type args struct {
		Name string `json:"name"`
	}
	result := callTool(t, h.getRuntimeState, ctx, args{Name: ""})

	if !result.IsError {
		t.Errorf("expected error for empty name, got: %s", resultText(result))
	}
	if !strings.Contains(resultText(result), "name is required") {
		t.Errorf("expected 'name is required', got: %s", resultText(result))
	}
}

func TestGetRuntimeState_NoConfigRepo(t *testing.T) {
	h, _, ctx := setupLogHandler(t, nil)

	type args struct {
		Name string `json:"name"`
	}
	result := callTool(t, h.getRuntimeState, ctx, args{Name: "anything"})

	if !result.IsError {
		t.Errorf("expected error when config repo is nil, got: %s", resultText(result))
	}
}

func TestGetRuntimeState_FailingCommand(t *testing.T) {
	configValues := map[string]string{
		"runtime_state.fail": "exit 1",
	}
	h, _, ctx := setupLogHandler(t, configValues)

	type args struct {
		Name string `json:"name"`
	}
	result := callTool(t, h.getRuntimeState, ctx, args{Name: "fail"})

	if !result.IsError {
		t.Errorf("expected error for failing command, got: %s", resultText(result))
	}
	if !strings.Contains(resultText(result), "command failed") {
		t.Errorf("expected 'command failed' message, got: %s", resultText(result))
	}
}

// ---------------------------------------------------------------------------
// Registration test
// ---------------------------------------------------------------------------

func TestObservabilityHandler_RegisterTools(t *testing.T) {
	store := &storage.Store{
		Workspaces: &mockWorkspaceRepo{workspaces: map[string]*types.WorkspaceInfo{}},
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	h := NewObservabilityHandler(store, logger)

	registry := mcp.NewToolRegistry()
	h.RegisterTools(registry)

	// Consolidated handler registers a single "observability" tool.
	if registry.ToolCount() != 1 {
		t.Errorf("expected 1 tool (observability), got %d", registry.ToolCount())
	}

	schemas := registry.Schemas()
	if len(schemas) == 0 || schemas[0].Name != "observability" {
		t.Errorf("expected tool named 'observability', got %v", schemas)
	}
}

// ---------------------------------------------------------------------------
// tailFile unit tests
// ---------------------------------------------------------------------------

func TestTailFile_FewerLinesThanRequested(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "short.log")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	lines, err := tailLogFile(path, 100)
	if err != nil {
		t.Fatalf("tailFile: %v", err)
	}
	// File has 3 content lines + 1 trailing empty from split.
	// bufio.Scanner strips the trailing newline, so we get exactly 3.
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d: %v", len(lines), lines)
	}
}

func TestTailFile_ExactCount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exact.log")
	if err := os.WriteFile(path, []byte("1\n2\n3\n4\n5\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	lines, err := tailLogFile(path, 3)
	if err != nil {
		t.Fatalf("tailFile: %v", err)
	}
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "3" || lines[1] != "4" || lines[2] != "5" {
		t.Errorf("expected [3,4,5], got %v", lines)
	}
}

// ---------------------------------------------------------------------------
// scanDir unit tests
// ---------------------------------------------------------------------------

func TestScanDir_NonexistentDir(t *testing.T) {
	entries := scanLogDir("/nonexistent/path/that/does/not/exist")
	if len(entries) != 0 {
		t.Errorf("expected empty slice for nonexistent dir, got %d entries", len(entries))
	}
}

func TestScanDir_SkipsSubdirectories(t *testing.T) {
	dir := t.TempDir()

	// Create a file and a subdirectory.
	if err := os.WriteFile(filepath.Join(dir, "file.log"), []byte("data"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	entries := scanLogDir(dir)
	if len(entries) != 1 {
		t.Errorf("expected 1 entry (skipping subdirs), got %d", len(entries))
	}
	if entries[0].Path != filepath.Join(dir, "file.log") {
		t.Errorf("unexpected path: %s", entries[0].Path)
	}
}
