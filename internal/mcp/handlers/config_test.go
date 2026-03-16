package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/config"
	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

// ---------------------------------------------------------------------------
// Mock ConfigRepo for handler tests
// ---------------------------------------------------------------------------

type configTestRepo struct {
	mu      sync.Mutex
	values  map[string]string // "scope_type:scope_id:key" -> value
	keys    map[string]*types.ConfigKeyMeta
	history map[string][]types.ConfigChange
}

func newMockConfigRepo() *configTestRepo {
	return &configTestRepo{
		values:  make(map[string]string),
		keys:    make(map[string]*types.ConfigKeyMeta),
		history: make(map[string][]types.ConfigChange),
	}
}

func (m *configTestRepo) scopeKey(key string, scope types.ConfigScope) string {
	return fmt.Sprintf("%s:%s:%s", scope.Type, scope.ID, key)
}

func (m *configTestRepo) GetValue(_ context.Context, key string, scope types.ConfigScope) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sk := m.scopeKey(key, scope)
	val, ok := m.values[sk]
	if !ok {
		return "", fmt.Errorf("not found: %s", sk)
	}
	return val, nil
}

func (m *configTestRepo) SetValue(_ context.Context, key, value string, scope types.ConfigScope, actor string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	sk := m.scopeKey(key, scope)
	old := m.values[sk]
	m.values[sk] = value
	m.history[key] = append(m.history[key], types.ConfigChange{
		Key:       key,
		OldValue:  old,
		NewValue:  value,
		ScopeType: scope.Type,
		ScopeID:   scope.ID,
		Actor:     actor,
		ChangedAt: time.Now(),
	})
	return nil
}

func (m *configTestRepo) GetKeyMeta(_ context.Context, key string) (*types.ConfigKeyMeta, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	meta, ok := m.keys[key]
	if !ok {
		return nil, fmt.Errorf("key %q not found", key)
	}
	return meta, nil
}

func (m *configTestRepo) ListKeys(_ context.Context) ([]types.ConfigKeyMeta, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []types.ConfigKeyMeta
	for _, meta := range m.keys {
		out = append(out, *meta)
	}
	return out, nil
}

func (m *configTestRepo) ListValues(_ context.Context, scope types.ConfigScope) ([]types.ConfigValue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []types.ConfigValue
	prefix := fmt.Sprintf("%s:%s:", scope.Type, scope.ID)
	for sk, val := range m.values {
		if len(sk) > len(prefix) && sk[:len(prefix)] == prefix {
			key := sk[len(prefix):]
			out = append(out, types.ConfigValue{
				Key:       key,
				Value:     val,
				ScopeType: scope.Type,
				ScopeID:   scope.ID,
			})
		}
	}
	return out, nil
}

func (m *configTestRepo) GetHistory(_ context.Context, key string, limit int) ([]types.ConfigChange, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	changes := m.history[key]
	if limit > 0 && len(changes) > limit {
		changes = changes[len(changes)-limit:]
	}
	return changes, nil
}

func (m *configTestRepo) UpsertKeyMeta(_ context.Context, meta *types.ConfigKeyMeta) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keys[meta.Key] = meta
	return nil
}

// ---------------------------------------------------------------------------
// Test setup
// ---------------------------------------------------------------------------

func setupConfigHandler(t *testing.T) (*ConfigHandler, *configTestRepo, context.Context) {
	t.Helper()
	repo := newMockConfigRepo()
	bus := nervous.NewEventBus(64)
	store := config.NewConfigStore(repo, bus)
	handler := NewConfigHandler(store, repo)

	// Seed a test key so Resolve and Set can find it.
	_ = repo.UpsertKeyMeta(context.Background(), &types.ConfigKeyMeta{
		Key:         "test.key",
		ScopeType:   "global",
		ValueType:   "string",
		DefaultVal:  "default-val",
		Description: "A test configuration key",
	})
	_ = repo.UpsertKeyMeta(context.Background(), &types.ConfigKeyMeta{
		Key:         "log.level",
		ScopeType:   "global",
		ValueType:   "string",
		DefaultVal:  "info",
		Description: "Logging level",
	})

	return handler, repo, context.Background()
}

func callConfigTool(t *testing.T, fn func(context.Context, json.RawMessage) (*types.ToolResult, error), ctx context.Context, args any) *types.ToolResult {
	t.Helper()
	data, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	result, err := fn(ctx, data)
	if err != nil {
		t.Fatalf("tool error: %v", err)
	}
	return result
}

func configResultText(r *types.ToolResult) string {
	if len(r.Content) == 0 {
		return ""
	}
	return r.Content[0].Text
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestConfigHandler_RegisterTools(t *testing.T) {
	h, _, _ := setupConfigHandler(t)
	registry := mcp.NewToolRegistry()
	h.RegisterTools(registry)

	// Wave XIII consolidation: ConfigHandler registers a single "config" tool.
	if registry.ToolCount() != 1 {
		t.Errorf("expected 1 consolidated tool, got %d", registry.ToolCount())
	}

	schemas := registry.Schemas()
	if len(schemas) == 0 || schemas[0].Name != "config" {
		t.Errorf("expected tool named 'config', got %v", schemas)
	}
}

func TestConfigHandler_GetConfig_Default(t *testing.T) {
	h, _, ctx := setupConfigHandler(t)

	type args struct {
		Key string `json:"key"`
	}
	result := callConfigTool(t, h.getConfig, ctx, args{Key: "test.key"})

	if result.IsError {
		t.Fatalf("unexpected error: %s", configResultText(result))
	}

	text := configResultText(result)
	if text == "" {
		t.Fatal("empty result")
	}

	var out map[string]string
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if out["value"] != "default-val" {
		t.Errorf("expected default-val, got %q", out["value"])
	}
}

func TestConfigHandler_GetConfig_MissingKey(t *testing.T) {
	h, _, ctx := setupConfigHandler(t)

	type args struct {
		Key string `json:"key"`
	}
	result := callConfigTool(t, h.getConfig, ctx, args{Key: ""})

	if !result.IsError {
		t.Error("expected error for empty key")
	}
}

func TestConfigHandler_GetConfig_NotFound(t *testing.T) {
	h, _, ctx := setupConfigHandler(t)

	type args struct {
		Key string `json:"key"`
	}
	result := callConfigTool(t, h.getConfig, ctx, args{Key: "nonexistent.key"})

	if !result.IsError {
		t.Error("expected error for nonexistent key")
	}
}

func TestConfigHandler_SetConfig(t *testing.T) {
	h, _, ctx := setupConfigHandler(t)

	type setArgs struct {
		Key       string `json:"key"`
		Value     string `json:"value"`
		ScopeType string `json:"scope_type"`
		ScopeID   string `json:"scope_id"`
		Actor     string `json:"actor"`
	}
	result := callConfigTool(t, h.setConfig, ctx, setArgs{
		Key:       "test.key",
		Value:     "new-value",
		ScopeType: "global",
		ScopeID:   "",
		Actor:     "test-user",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", configResultText(result))
	}

	// Verify it resolves to the new value.
	type getArgs struct {
		Key string `json:"key"`
	}
	getResult := callConfigTool(t, h.getConfig, ctx, getArgs{Key: "test.key"})
	if getResult.IsError {
		t.Fatalf("unexpected error on get: %s", configResultText(getResult))
	}

	var out map[string]string
	if err := json.Unmarshal([]byte(configResultText(getResult)), &out); err != nil {
		t.Fatalf("unmarshal get result: %v", err)
	}
	if out["value"] != "new-value" {
		t.Errorf("expected new-value after set, got %q", out["value"])
	}
}

func TestConfigHandler_SetConfig_MissingParams(t *testing.T) {
	h, _, ctx := setupConfigHandler(t)

	type args struct {
		Key       string `json:"key"`
		Value     string `json:"value"`
		ScopeType string `json:"scope_type"`
		Actor     string `json:"actor"`
	}

	// Missing key
	result := callConfigTool(t, h.setConfig, ctx, args{Value: "v", ScopeType: "global", Actor: "a"})
	if !result.IsError {
		t.Error("expected error for missing key")
	}

	// Missing value
	result = callConfigTool(t, h.setConfig, ctx, args{Key: "test.key", ScopeType: "global", Actor: "a"})
	if !result.IsError {
		t.Error("expected error for missing value")
	}

	// Missing scope_type
	result = callConfigTool(t, h.setConfig, ctx, args{Key: "test.key", Value: "v", Actor: "a"})
	if !result.IsError {
		t.Error("expected error for missing scope_type")
	}

	// Missing actor
	result = callConfigTool(t, h.setConfig, ctx, args{Key: "test.key", Value: "v", ScopeType: "global"})
	if !result.IsError {
		t.Error("expected error for missing actor")
	}
}

func TestConfigHandler_ListConfigKeys(t *testing.T) {
	h, _, ctx := setupConfigHandler(t)

	result := callConfigTool(t, h.listConfigKeys, ctx, struct{}{})

	if result.IsError {
		t.Fatalf("unexpected error: %s", configResultText(result))
	}

	text := configResultText(result)
	if text == "" {
		t.Fatal("empty result")
	}

	var keys []types.ConfigKeyMeta
	if err := json.Unmarshal([]byte(text), &keys); err != nil {
		t.Fatalf("unmarshal keys: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(keys))
	}
}

func TestConfigHandler_ListConfigKeys_Empty(t *testing.T) {
	repo := newMockConfigRepo()
	bus := nervous.NewEventBus(64)
	store := config.NewConfigStore(repo, bus)
	handler := NewConfigHandler(store, repo)

	result := callConfigTool(t, handler.listConfigKeys, context.Background(), struct{}{})

	if result.IsError {
		t.Fatalf("unexpected error: %s", configResultText(result))
	}

	text := configResultText(result)
	var keys []interface{}
	if err := json.Unmarshal([]byte(text), &keys); err != nil {
		t.Fatalf("unmarshal keys: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected empty array, got %d items", len(keys))
	}
}

func TestConfigHandler_GetConfigHistory(t *testing.T) {
	h, _, ctx := setupConfigHandler(t)

	// Set a value to create history.
	type setArgs struct {
		Key       string `json:"key"`
		Value     string `json:"value"`
		ScopeType string `json:"scope_type"`
		Actor     string `json:"actor"`
	}
	_ = callConfigTool(t, h.setConfig, ctx, setArgs{Key: "test.key", Value: "v1", ScopeType: "global", Actor: "user1"})
	_ = callConfigTool(t, h.setConfig, ctx, setArgs{Key: "test.key", Value: "v2", ScopeType: "global", Actor: "user2"})

	type histArgs struct {
		Key   string `json:"key"`
		Limit int    `json:"limit"`
	}
	result := callConfigTool(t, h.getConfigHistory, ctx, histArgs{Key: "test.key", Limit: 10})

	if result.IsError {
		t.Fatalf("unexpected error: %s", configResultText(result))
	}

	text := configResultText(result)
	var changes []types.ConfigChange
	if err := json.Unmarshal([]byte(text), &changes); err != nil {
		t.Fatalf("unmarshal history: %v", err)
	}
	if len(changes) != 2 {
		t.Errorf("expected 2 history entries, got %d", len(changes))
	}
}

func TestConfigHandler_GetConfigHistory_MissingKey(t *testing.T) {
	h, _, ctx := setupConfigHandler(t)

	type args struct {
		Key string `json:"key"`
	}
	result := callConfigTool(t, h.getConfigHistory, ctx, args{Key: ""})

	if !result.IsError {
		t.Error("expected error for empty key")
	}
}

func TestConfigHandler_GetConfigHistory_Empty(t *testing.T) {
	h, _, ctx := setupConfigHandler(t)

	type args struct {
		Key string `json:"key"`
	}
	result := callConfigTool(t, h.getConfigHistory, ctx, args{Key: "log.level"})

	if result.IsError {
		t.Fatalf("unexpected error: %s", configResultText(result))
	}

	text := configResultText(result)
	var changes []interface{}
	if err := json.Unmarshal([]byte(text), &changes); err != nil {
		t.Fatalf("unmarshal history: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("expected empty array, got %d items", len(changes))
	}
}

func TestConfigHandler_GetConfig_ScopeResolution(t *testing.T) {
	h, repo, ctx := setupConfigHandler(t)

	// Set global value.
	_ = repo.SetValue(ctx, "test.key", "global-val", types.ConfigScope{Type: "global", ID: ""}, "setup")

	// Set workspace value.
	_ = repo.SetValue(ctx, "test.key", "ws-val", types.ConfigScope{Type: "workspace", ID: "my-ws"}, "setup")

	// Resolve without scope should get global.
	type getArgs struct {
		Key         string `json:"key"`
		WorkspaceID string `json:"workspace_id"`
		AgentID     string `json:"agent_id"`
	}

	result := callConfigTool(t, h.getConfig, ctx, getArgs{Key: "test.key"})
	var out map[string]string
	_ = json.Unmarshal([]byte(configResultText(result)), &out)
	if out["value"] != "global-val" {
		t.Errorf("expected global-val, got %q", out["value"])
	}

	// Resolve with workspace should get workspace value.
	result = callConfigTool(t, h.getConfig, ctx, getArgs{Key: "test.key", WorkspaceID: "my-ws"})
	_ = json.Unmarshal([]byte(configResultText(result)), &out)
	if out["value"] != "ws-val" {
		t.Errorf("expected ws-val, got %q", out["value"])
	}
}
