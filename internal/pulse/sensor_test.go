package pulse

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/pkg/types"
)

func newTestSensorManager() (*SensorManager, *nervous.EventBus) {
	bus := nervous.NewEventBus(64)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	sm := NewSensorManager(bus, logger)
	return sm, bus
}

func TestSensorManager_CreateSensor(t *testing.T) {
	sm, _ := newTestSensorManager()

	sensor, err := sm.CreateSensor(
		"health-check",
		"*/5 * * * *",
		types.SensorAction{Type: "http", URL: "http://localhost/health"},
		[]MatchCriteria{{JSONPath: "$.status", Operator: "eq", Value: "ok"}},
		types.SensorEventConfig{EventType: "health.ok"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sensor.ID == "" {
		t.Error("expected non-empty sensor ID")
	}
	if sensor.Name != "health-check" {
		t.Errorf("expected name 'health-check', got %q", sensor.Name)
	}
	if !sensor.Enabled {
		t.Error("expected sensor to be enabled by default")
	}
	if sensor.NextFire == nil {
		t.Error("expected non-nil NextFire")
	}
}

func TestSensorManager_CreateSensor_Validation(t *testing.T) {
	sm, _ := newTestSensorManager()

	tests := []struct {
		name     string
		sName    string
		schedule string
		action   types.SensorAction
		wantErr  string
	}{
		{"empty name", "", "*/5 * * * *", types.SensorAction{Type: "http", URL: "http://x"}, "name is required"},
		{"empty schedule", "test", "", types.SensorAction{Type: "http", URL: "http://x"}, "schedule is required"},
		{"invalid action type", "test", "*/5 * * * *", types.SensorAction{Type: "grpc"}, "must be 'http' or 'shell'"},
		{"http without URL", "test", "*/5 * * * *", types.SensorAction{Type: "http"}, "URL is required"},
		{"shell without command", "test", "*/5 * * * *", types.SensorAction{Type: "shell"}, "command is required"},
		{"invalid schedule", "test", "bad cron", types.SensorAction{Type: "http", URL: "http://x"}, "invalid schedule"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := sm.CreateSensor(tt.sName, tt.schedule, tt.action, nil, types.SensorEventConfig{EventType: "test"})
			if err == nil {
				t.Fatal("expected error")
			}
			if !containsStr(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestSensorManager_ListSensors(t *testing.T) {
	sm, _ := newTestSensorManager()

	// Empty initially.
	if list := sm.ListSensors(); len(list) != 0 {
		t.Errorf("expected 0 sensors, got %d", len(list))
	}

	// Create two sensors.
	_, _ = sm.CreateSensor("s1", "*/5 * * * *", types.SensorAction{Type: "shell", Command: "echo ok"}, nil, types.SensorEventConfig{EventType: "e1"})
	_, _ = sm.CreateSensor("s2", "*/10 * * * *", types.SensorAction{Type: "shell", Command: "echo ok"}, nil, types.SensorEventConfig{EventType: "e2"})

	if list := sm.ListSensors(); len(list) != 2 {
		t.Errorf("expected 2 sensors, got %d", len(list))
	}
}

func TestSensorManager_DeleteSensor(t *testing.T) {
	sm, _ := newTestSensorManager()

	sensor, _ := sm.CreateSensor("to-delete", "*/5 * * * *", types.SensorAction{Type: "shell", Command: "echo ok"}, nil, types.SensorEventConfig{EventType: "e"})

	if err := sm.DeleteSensor(sensor.ID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if list := sm.ListSensors(); len(list) != 0 {
		t.Errorf("expected 0 sensors after delete, got %d", len(list))
	}

	// Delete non-existent.
	if err := sm.DeleteSensor("nonexistent"); err == nil {
		t.Error("expected error deleting nonexistent sensor")
	}
}

func TestSensorManager_UpdateSensor(t *testing.T) {
	sm, _ := newTestSensorManager()

	sensor, _ := sm.CreateSensor("original", "*/5 * * * *", types.SensorAction{Type: "shell", Command: "echo ok"}, nil, types.SensorEventConfig{EventType: "e"})

	err := sm.UpdateSensor(sensor.ID, "renamed", "*/10 * * * *", nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected update error: %v", err)
	}

	updated, _ := sm.GetSensor(sensor.ID)
	if updated.Name != "renamed" {
		t.Errorf("expected name 'renamed', got %q", updated.Name)
	}
	if updated.Schedule != "*/10 * * * *" {
		t.Errorf("expected schedule '*/10 * * * *', got %q", updated.Schedule)
	}
}

func TestSensorManager_HTTPExecution(t *testing.T) {
	// Set up a test HTTP server.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "healthy",
			"count":  42,
		})
	}))
	defer server.Close()

	sm, bus := newTestSensorManager()

	// Subscribe to sensor events.
	sub := bus.SubscribeTypes("sensor-watcher", types.EventSensorMatch)
	defer bus.Unsubscribe("sensor-watcher")

	sensor, _ := sm.CreateSensor(
		"http-test",
		"@every 1s",
		types.SensorAction{Type: "http", URL: server.URL, Method: "GET"},
		[]MatchCriteria{
			{JSONPath: "$.status", Operator: "eq", Value: "healthy"},
			{JSONPath: "$.count", Operator: "gt", Value: "10"},
		},
		types.SensorEventConfig{EventType: "test.health.ok"},
	)

	// Force immediate execution.
	sm.mu.Lock()
	now := time.Now()
	past := now.Add(-time.Minute)
	sm.sensors[sensor.ID].NextFire = &past
	sm.mu.Unlock()

	sm.Tick(context.Background())

	// Check that a match event was published.
	select {
	case event := <-sub.Ch:
		if event.Type != types.EventSensorMatch {
			t.Errorf("expected sensor.match event, got %s", event.Type)
		}
	case <-time.After(2 * time.Second):
		t.Error("expected sensor match event within 2 seconds")
	}

	// Verify last result was stored.
	updated, _ := sm.GetSensor(sensor.ID)
	if !updated.LastMatched {
		t.Error("expected LastMatched to be true")
	}
	if updated.LastResult == "" {
		t.Error("expected non-empty LastResult")
	}
}

func TestSensorManager_ShellExecution(t *testing.T) {
	sm, _ := newTestSensorManager()

	sensor, _ := sm.CreateSensor(
		"shell-test",
		"@every 1s",
		types.SensorAction{Type: "shell", Command: `echo '{"status":"ok"}'`},
		[]MatchCriteria{
			{JSONPath: "$.status", Operator: "eq", Value: "ok"},
		},
		types.SensorEventConfig{EventType: "shell.check.ok"},
	)

	// Force immediate execution.
	sm.mu.Lock()
	now := time.Now()
	past := now.Add(-time.Minute)
	sm.sensors[sensor.ID].NextFire = &past
	sm.mu.Unlock()

	sm.Tick(context.Background())

	updated, _ := sm.GetSensor(sensor.ID)
	if !updated.LastMatched {
		t.Error("expected LastMatched to be true for shell sensor")
	}
}

func TestSensorManager_NoMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "unhealthy"})
	}))
	defer server.Close()

	sm, _ := newTestSensorManager()

	sensor, _ := sm.CreateSensor(
		"no-match",
		"@every 1s",
		types.SensorAction{Type: "http", URL: server.URL},
		[]MatchCriteria{
			{JSONPath: "$.status", Operator: "eq", Value: "healthy"},
		},
		types.SensorEventConfig{EventType: "test.healthy"},
	)

	sm.mu.Lock()
	past := time.Now().Add(-time.Minute)
	sm.sensors[sensor.ID].NextFire = &past
	sm.mu.Unlock()

	sm.Tick(context.Background())

	updated, _ := sm.GetSensor(sensor.ID)
	if updated.LastMatched {
		t.Error("expected LastMatched to be false when criteria don't match")
	}
}

func TestSensorManager_SecretResolution(t *testing.T) {
	sm, _ := newTestSensorManager()

	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	sm.SetSecretResolver(func(ctx context.Context, ref string) (string, error) {
		if ref == "api_token" {
			return "Bearer secret-123", nil
		}
		return "", nil
	})

	sensor, _ := sm.CreateSensor(
		"auth-test",
		"@every 1s",
		types.SensorAction{
			Type:   "http",
			URL:    server.URL,
			Method: "GET",
			Headers: map[string]string{
				"Authorization": "secret:api_token",
			},
		},
		nil,
		types.SensorEventConfig{EventType: "test.auth"},
	)

	sm.mu.Lock()
	past := time.Now().Add(-time.Minute)
	sm.sensors[sensor.ID].NextFire = &past
	sm.mu.Unlock()

	sm.Tick(context.Background())

	if receivedAuth != "Bearer secret-123" {
		t.Errorf("expected Authorization header 'Bearer secret-123', got %q", receivedAuth)
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
