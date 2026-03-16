package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/pulse"
)

func newTestSensorHandler() (*EventHandler, *pulse.SensorManager) {
	bus := nervous.NewEventBus(64)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	sm := pulse.NewSensorManager(bus, logger)
	h := NewEventHandler(nil, bus, nil, nil, &testLogger{})
	h.SetSensorDeps(sm)
	return h, sm
}

func TestSensorHandler_RegisterTools(t *testing.T) {
	handler, _ := newTestSensorHandler()
	registry := mcp.NewToolRegistry()
	handler.RegisterTools(registry)

	// Consolidated handler registers a single "event" tool.
	if registry.ToolCount() != 1 {
		t.Errorf("expected 1 tool (event), got %d", registry.ToolCount())
	}

	schemas := registry.Schemas()
	if len(schemas) == 0 || schemas[0].Name != "event" {
		t.Errorf("expected tool named 'event', got %v", schemas)
	}
}

func TestSensorHandler_CreateSensor(t *testing.T) {
	handler, _ := newTestSensorHandler()
	registry := mcp.NewToolRegistry()
	handler.RegisterTools(registry)

	params := json.RawMessage(`{
		"action": "create_sensor_cadence",
		"name": "test-sensor",
		"schedule": "*/5 * * * *",
		"sensor_action": {
			"type": "http",
			"url": "http://localhost:8080/health"
		},
		"criteria": [
			{"jsonpath": "$.status", "operator": "eq", "value": "ok"}
		],
		"event": {
			"event_type": "health.check.ok"
		}
	}`)

	result, err := registry.Dispatch(context.Background(), "event", params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "test-sensor") {
		t.Errorf("expected response to contain sensor name, got %s", result.Content[0].Text)
	}
}

func TestSensorHandler_CreateSensor_MissingName(t *testing.T) {
	handler, _ := newTestSensorHandler()
	registry := mcp.NewToolRegistry()
	handler.RegisterTools(registry)

	params := json.RawMessage(`{
		"action": "create_sensor_cadence",
		"schedule": "*/5 * * * *",
		"sensor_action": {"type": "http", "url": "http://x"},
		"event": {"event_type": "e"}
	}`)

	result, err := registry.Dispatch(context.Background(), "event", params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected tool error for missing name")
	}
}

func TestSensorHandler_ListSensors_Empty(t *testing.T) {
	handler, _ := newTestSensorHandler()
	registry := mcp.NewToolRegistry()
	handler.RegisterTools(registry)

	result, err := registry.Dispatch(context.Background(), "event", json.RawMessage(`{"action":"list_sensor_cadences"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "No sensor") {
		t.Errorf("expected 'No sensor' message, got %s", result.Content[0].Text)
	}
}

func TestSensorHandler_CRUD_Lifecycle(t *testing.T) {
	handler, _ := newTestSensorHandler()
	registry := mcp.NewToolRegistry()
	handler.RegisterTools(registry)

	// Create
	createParams := json.RawMessage(`{
		"action": "create_sensor_cadence",
		"name": "lifecycle-test",
		"schedule": "*/5 * * * *",
		"sensor_action": {"type": "shell", "command": "echo ok"},
		"event": {"event_type": "test.event"}
	}`)
	createResult, _ := registry.Dispatch(context.Background(), "event", createParams)
	if createResult.IsError {
		t.Fatalf("create failed: %s", createResult.Content[0].Text)
	}

	// Extract ID from response.
	var createResp map[string]any
	_ = json.Unmarshal([]byte(createResult.Content[0].Text), &createResp)
	sensorID := createResp["id"].(string)

	// Update
	updateParams, _ := json.Marshal(map[string]string{
		"action": "update_sensor_cadence",
		"id":     sensorID,
		"name":   "updated-sensor",
	})
	updateResult, _ := registry.Dispatch(context.Background(), "event", updateParams)
	if updateResult.IsError {
		t.Fatalf("update failed: %s", updateResult.Content[0].Text)
	}

	// List (should have 1)
	listResult, _ := registry.Dispatch(context.Background(), "event", json.RawMessage(`{"action":"list_sensor_cadences"}`))
	if strings.Contains(listResult.Content[0].Text, "No sensor") {
		t.Error("expected sensor in list after create")
	}

	// Delete
	deleteParams, _ := json.Marshal(map[string]string{"action": "delete_sensor_cadence", "id": sensorID})
	deleteResult, _ := registry.Dispatch(context.Background(), "event", deleteParams)
	if deleteResult.IsError {
		t.Fatalf("delete failed: %s", deleteResult.Content[0].Text)
	}

	// List again (should be empty)
	listResult2, _ := registry.Dispatch(context.Background(), "event", json.RawMessage(`{"action":"list_sensor_cadences"}`))
	if !strings.Contains(listResult2.Content[0].Text, "No sensor") {
		t.Error("expected empty list after delete")
	}
}

func TestSensorHandler_DeleteNonexistent(t *testing.T) {
	handler, _ := newTestSensorHandler()
	registry := mcp.NewToolRegistry()
	handler.RegisterTools(registry)

	params := json.RawMessage(`{"action":"delete_sensor_cadence", "id": "nonexistent-id"}`)
	result, _ := registry.Dispatch(context.Background(), "event", params)
	if !result.IsError {
		t.Error("expected error deleting nonexistent sensor")
	}
}
