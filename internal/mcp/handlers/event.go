package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/pulse"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

// actionClearanceEvent maps each event action to its minimum ABAC clearance.
var actionClearanceEvent = map[string]int{
	// Nervous system
	"subscribe_events":     0,
	"get_event_stats":      0,
	"create_event_handler": 1,
	"list_event_handlers":  0,
	"delete_handler":       1,
	"query_domain_events":  0,
	"promote_event":        1,
	"test_sensor":          1,
	// Pulse engine
	"fire_event":       1,
	"list_cadences":    0,
	"create_cadence":   1,
	"update_cadence":   1,
	"delete_cadence":   1,
	"get_pulse_status": 0,
	// Sensor cadences
	"create_sensor_cadence": 1,
	"update_sensor_cadence": 1,
	"list_sensor_cadences":  0,
	"delete_sensor_cadence": 1,
}

// eventLogger is a minimal logging interface used by EventHandler.
type eventLogger interface {
	Info(msg string, args ...any)
	Error(msg string, args ...any)
}

// EventHandler implements the consolidated "event" MCP tool covering the
// nervous system, pulse engine, and sensor cadence management.
type EventHandler struct {
	nervousRepo repo.NervousRepo
	bus         *nervous.EventBus
	ringBuffer  *nervous.RingBuffer
	executor    *nervous.Executor
	logger      eventLogger
	pulseEngine *pulse.Engine
	sensorMgr   *pulse.SensorManager
}

// NewEventHandler creates an EventHandler with nervous system dependencies.
func NewEventHandler(nervousRepo repo.NervousRepo, bus *nervous.EventBus, ringBuffer *nervous.RingBuffer, executor *nervous.Executor, log eventLogger) *EventHandler {
	return &EventHandler{
		nervousRepo: nervousRepo,
		bus:         bus,
		ringBuffer:  ringBuffer,
		executor:    executor,
		logger:      log,
	}
}

// SetPulseDeps wires the Pulse Engine dependency.
func (h *EventHandler) SetPulseDeps(engine *pulse.Engine) {
	h.pulseEngine = engine
}

// SetSensorDeps wires the Sensor Manager dependency.
func (h *EventHandler) SetSensorDeps(mgr *pulse.SensorManager) {
	h.sensorMgr = mgr
}

// RegisterTools registers the consolidated "event" MCP tool.
func (h *EventHandler) RegisterTools(registry *mcp.ToolRegistry) {
	registry.Register(
		"event",
		"Event system: nervous system subscriptions, event handlers, domain queries, pulse cadences, and sensor management. "+
			"Actions: subscribe_events | get_event_stats | create_event_handler | list_event_handlers | delete_handler | "+
			"query_domain_events | promote_event | test_sensor | fire_event | list_cadences | create_cadence | update_cadence | "+
			"delete_cadence | get_pulse_status | create_sensor_cadence | update_sensor_cadence | list_sensor_cadences | delete_sensor_cadence",
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": [
						"subscribe_events", "get_event_stats", "create_event_handler", "list_event_handlers",
						"delete_handler", "query_domain_events", "promote_event", "test_sensor",
						"fire_event", "list_cadences", "create_cadence", "update_cadence", "delete_cadence",
						"get_pulse_status",
						"create_sensor_cadence", "update_sensor_cadence", "list_sensor_cadences", "delete_sensor_cadence"
					],
					"description": "The event action to perform"
				},
				"subscriber_id":  {"type": "string", "description": "Subscription ID (subscribe_events)"},
				"event_filter":   {"type": "string", "description": "Glob pattern for event filtering"},
				"event_type":     {"type": "string", "description": "Event type (query/promote/fire)"},
				"source":         {"type": "string", "description": "Event source identifier"},
				"scope":          {"type": "string", "description": "Event scope (promote_event)"},
				"payload":        {"type": "string", "description": "JSON payload string"},
				"since":          {"type": "string", "description": "RFC3339 datetime for query filtering"},
				"limit":          {"type": "integer", "description": "Max results for queries"},
				"trace_id":       {"type": "string", "description": "Trace ID (test_sensor)"},
				"name":           {"type": "string", "description": "Name for handler/cadence/sensor"},
				"handler_action": {"type": "string", "description": "Handler action type: tool_call, webhook, or log (create_event_handler)"},
				"action_payload": {"type": "string", "description": "JSON config for event handler action"},
				"enabled":        {"type": "boolean", "description": "Enable/disable state"},
				"id":             {"type": "string", "description": "Cadence or sensor ID (update/delete)"},
				"schedule":       {"type": "string", "description": "Cron expression for cadence/sensor"},
				"priority":       {"type": "string", "description": "Cadence priority: background, standard, urgent"},
				"mode":           {"type": "string", "description": "Cadence mode: event or agent_order"},
				"target_agent":   {"type": "string", "description": "Target agent for agent_order mode"},
				"sensor_action":  {"type": "object", "description": "Sensor action config: {type, url, method, headers, body, command, timeout_seconds}"},
				"criteria":       {"type": "array", "description": "Sensor match criteria array"},
				"event_config":   {"type": "object", "description": "Sensor event injection config: {event_type, payload_from}"}
			},
			"required": ["action"]
		}`),
		h.dispatch,
	)
}

// dispatch routes the consolidated "event" tool to the appropriate handler method.
func (h *EventHandler) dispatch(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var envelope struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return nil, fmt.Errorf("handlers.EventHandler.dispatch: %w", err)
	}

	if err := checkActionClearance(ctx, envelope.Action, actionClearanceEvent); err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	switch envelope.Action {
	// Nervous system
	case "subscribe_events":
		return h.subscribeEvents(ctx, params)
	case "get_event_stats":
		return h.getEventStats(ctx, params)
	case "create_event_handler":
		return h.createEventHandler(ctx, params)
	case "list_event_handlers":
		return h.listEventHandlers(ctx, params)
	case "delete_handler":
		return h.deleteHandler(ctx, params)
	case "query_domain_events":
		return h.queryDomainEvents(ctx, params)
	case "promote_event":
		return h.promoteEvent(ctx, params)
	case "test_sensor":
		return h.testSensor(ctx, params)
	// Pulse engine
	case "fire_event":
		return h.fireEvent(ctx, params)
	case "list_cadences":
		return h.listCadences(ctx, params)
	case "create_cadence":
		return h.createCadence(ctx, params)
	case "update_cadence":
		return h.updateCadence(ctx, params)
	case "delete_cadence":
		return h.deleteCadence(ctx, params)
	case "get_pulse_status":
		return h.getPulseStatus(ctx, params)
	// Sensor cadences
	case "create_sensor_cadence":
		return h.createSensorCadence(ctx, params)
	case "update_sensor_cadence":
		return h.updateSensorCadence(ctx, params)
	case "list_sensor_cadences":
		return h.listSensorCadences(ctx, params)
	case "delete_sensor_cadence":
		return h.deleteSensorCadence(ctx, params)
	default:
		return types.NewErrorResult(fmt.Sprintf("unknown event action %q", envelope.Action)), nil
	}
}

// ── Nervous system actions ────────────────────────────────────────────────────

func (h *EventHandler) subscribeEvents(_ context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		SubscriberID string `json:"subscriber_id"`
		EventFilter  string `json:"event_filter"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.EventHandler.subscribeEvents: %w", err)
	}
	if args.SubscriberID == "" {
		return types.NewErrorResult("subscriber_id is required"), nil
	}
	if args.EventFilter == "" {
		return types.NewErrorResult("event_filter is required"), nil
	}

	filterFn := nervous.MakeFilterFunc(args.EventFilter)
	h.bus.Subscribe(args.SubscriberID, filterFn)

	h.bus.Publish(nervous.NewEvent(
		types.EventNervousSubscriptionAdded,
		"nervous_handler",
		"global",
		map[string]string{
			"subscriber_id": args.SubscriberID,
			"event_filter":  args.EventFilter,
		},
	))

	return types.NewToolResult(map[string]string{
		"subscriber_id": args.SubscriberID,
		"event_filter":  args.EventFilter,
		"message":       fmt.Sprintf("Subscribed %q with filter %q.", args.SubscriberID, args.EventFilter),
	}), nil
}

func (h *EventHandler) getEventStats(ctx context.Context, _ json.RawMessage) (*types.ToolResult, error) {
	if h.nervousRepo == nil {
		return types.NewErrorResult("nervous repo not available"), nil
	}

	stats, err := h.nervousRepo.GetEventStats(ctx)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get event stats: %v", err)), nil
	}

	if len(stats) == 0 {
		return types.NewToolResult(map[string]int64{}), nil
	}

	return types.NewToolResult(stats), nil
}

func (h *EventHandler) createEventHandler(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Name          string `json:"name"`
		EventFilter   string `json:"event_filter"`
		HandlerAction string `json:"handler_action"`
		ActionPayload string `json:"action_payload"`
		Enabled       *bool  `json:"enabled"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.EventHandler.createEventHandler: %w", err)
	}
	if args.Name == "" {
		return types.NewErrorResult("name is required"), nil
	}
	if args.EventFilter == "" {
		return types.NewErrorResult("event_filter is required"), nil
	}
	if args.HandlerAction == "" {
		return types.NewErrorResult("handler_action is required"), nil
	}

	if h.nervousRepo == nil {
		return types.NewErrorResult("nervous repo not available"), nil
	}

	enabled := true
	if args.Enabled != nil {
		enabled = *args.Enabled
	}

	handler := &types.EventHandler{
		Name:          args.Name,
		EventFilter:   args.EventFilter,
		Action:        args.HandlerAction,
		ActionPayload: args.ActionPayload,
		Enabled:       enabled,
	}

	id, err := h.nervousRepo.CreateHandler(ctx, handler)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("create handler: %v", err)), nil
	}

	if h.executor != nil {
		h.executor.ReloadHandlers()
	}

	return types.NewToolResult(map[string]string{
		"id":      id,
		"message": fmt.Sprintf("Event handler %q created.", args.Name),
	}), nil
}

func (h *EventHandler) listEventHandlers(ctx context.Context, _ json.RawMessage) (*types.ToolResult, error) {
	if h.nervousRepo == nil {
		return types.NewErrorResult("nervous repo not available"), nil
	}

	eventHandlers, err := h.nervousRepo.ListHandlers(ctx)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("list handlers: %v", err)), nil
	}

	if len(eventHandlers) == 0 {
		return types.NewToolResult([]interface{}{}), nil
	}

	return types.NewToolResult(eventHandlers), nil
}

func (h *EventHandler) deleteHandler(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.EventHandler.deleteHandler: %w", err)
	}
	if args.ID == "" {
		return types.NewErrorResult("id is required"), nil
	}
	if h.nervousRepo == nil {
		return types.NewErrorResult("nervous repo not available"), nil
	}

	if err := h.nervousRepo.DeleteHandler(ctx, args.ID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("delete handler: %v", err)), nil
	}

	if h.executor != nil {
		h.executor.ReloadHandlers()
	}

	return types.NewToolResult(map[string]string{
		"id":      args.ID,
		"status":  "deleted",
		"message": fmt.Sprintf("Event handler %q deleted.", args.ID),
	}), nil
}

func (h *EventHandler) queryDomainEvents(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		EventType string `json:"event_type"`
		Since     string `json:"since"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.EventHandler.queryDomainEvents: %w", err)
	}

	if h.nervousRepo == nil {
		return types.NewErrorResult("nervous repo not available"), nil
	}

	limit := args.Limit
	if limit <= 0 {
		limit = 50
	}

	var since time.Time
	if args.Since != "" {
		var err error
		since, err = time.Parse(time.RFC3339, args.Since)
		if err != nil {
			return types.NewErrorResult(fmt.Sprintf("invalid 'since' format (use RFC3339): %v", err)), nil
		}
	}

	events, err := h.nervousRepo.QueryEvents(ctx, args.EventType, since, limit)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("query events: %v", err)), nil
	}

	if len(events) == 0 {
		return types.NewToolResult([]interface{}{}), nil
	}

	return types.NewToolResult(events), nil
}

func (h *EventHandler) promoteEvent(_ context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		EventType string `json:"event_type"`
		Source    string `json:"source"`
		Scope    string `json:"scope"`
		Payload  string `json:"payload"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.EventHandler.promoteEvent: %w", err)
	}
	if args.EventType == "" {
		return types.NewErrorResult("event_type is required"), nil
	}

	source := args.Source
	if source == "" {
		source = "manual"
	}
	scope := args.Scope
	if scope == "" {
		scope = "global"
	}

	var payload any
	if args.Payload != "" {
		var raw json.RawMessage
		if err := json.Unmarshal([]byte(args.Payload), &raw); err == nil {
			payload = raw
		} else {
			payload = map[string]string{"message": args.Payload}
		}
	}

	event := nervous.NewEvent(types.EventType(args.EventType), source, scope, payload)
	h.bus.Publish(event)

	return types.NewToolResult(map[string]any{
		"event_type":  args.EventType,
		"sequence_id": event.SequenceID,
		"message":     fmt.Sprintf("Event %q published with sequence_id=%d.", args.EventType, event.SequenceID),
	}), nil
}

func (h *EventHandler) testSensor(_ context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		TraceID string `json:"trace_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.EventHandler.testSensor: %w", err)
	}

	traceID := args.TraceID
	if traceID == "" {
		traceID = fmt.Sprintf("test-%d", time.Now().UnixNano())
	}

	sizeBefore := h.ringBuffer.Size()

	event := nervous.NewEventWithTrace(
		types.EventType("nervous.test_sensor"),
		"test_sensor",
		"diagnostic",
		traceID,
		map[string]string{
			"trace_id": traceID,
			"purpose":  "sensor_test",
		},
	)
	h.bus.Publish(event)

	sizeAfter := h.ringBuffer.Size()

	return types.NewToolResult(map[string]any{
		"trace_id":             traceID,
		"sequence_id":          event.SequenceID,
		"ring_size_before":     sizeBefore,
		"ring_size_after":      sizeAfter,
		"bus_subscriber_count": h.bus.SubscriberCount(),
		"message":              fmt.Sprintf("Test event published (trace=%s, seq=%d). Ring buffer: %d -> %d.", traceID, event.SequenceID, sizeBefore, sizeAfter),
	}), nil
}

// ── Pulse engine actions ──────────────────────────────────────────────────────

func (h *EventHandler) fireEvent(_ context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		EventType string `json:"event_type"`
		Source    string `json:"source"`
		Payload   string `json:"payload"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.EventHandler.fireEvent: %w", err)
	}
	if args.EventType == "" {
		return types.NewErrorResult("event_type is required"), nil
	}
	if h.pulseEngine == nil {
		return types.NewErrorResult("pulse engine not available"), nil
	}
	if args.Source == "" {
		args.Source = "manual"
	}

	var payload any
	if args.Payload != "" {
		if err := json.Unmarshal([]byte(args.Payload), &payload); err != nil {
			return types.NewErrorResult(fmt.Sprintf("invalid payload JSON: %v", err)), nil
		}
	}

	if err := h.pulseEngine.FireEvent(types.EventType(args.EventType), args.Source, payload); err != nil {
		return types.NewErrorResult(fmt.Sprintf("fire event: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"message":    fmt.Sprintf("Event %q fired.", args.EventType),
		"event_type": args.EventType,
		"source":     args.Source,
	}), nil
}

func (h *EventHandler) listCadences(_ context.Context, _ json.RawMessage) (*types.ToolResult, error) {
	if h.pulseEngine == nil {
		return types.NewErrorResult("pulse engine not available"), nil
	}
	cadences := h.pulseEngine.ListCadences()
	if len(cadences) == 0 {
		return types.NewToolResult("No cadences configured."), nil
	}
	return types.NewToolResult(cadences), nil
}

func (h *EventHandler) createCadence(_ context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Name        string `json:"name"`
		Schedule    string `json:"schedule"`
		Priority    string `json:"priority"`
		Mode        string `json:"mode"`
		TargetAgent string `json:"target_agent"`
		Payload     string `json:"payload"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.EventHandler.createCadence: %w", err)
	}
	if args.Name == "" {
		return types.NewErrorResult("name is required"), nil
	}
	if args.Schedule == "" {
		return types.NewErrorResult("schedule is required"), nil
	}
	if h.pulseEngine == nil {
		return types.NewErrorResult("pulse engine not available"), nil
	}

	priority := types.PulsePriority(args.Priority)
	if args.Priority == "" {
		priority = types.PriorityStandard
	}

	mode := types.CadenceMode(args.Mode)
	if args.Mode == "" {
		mode = types.ModeEvent
	}

	var payload any
	if args.Payload != "" {
		if err := json.Unmarshal([]byte(args.Payload), &payload); err != nil {
			return types.NewErrorResult(fmt.Sprintf("invalid payload JSON: %v", err)), nil
		}
	}

	c, err := h.pulseEngine.CreateCadenceWithMode(args.Name, args.Schedule, priority, mode, args.TargetAgent, payload)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("create cadence: %v", err)), nil
	}

	return types.NewToolResult(map[string]any{
		"id":           c.ID,
		"name":         c.Name,
		"schedule":     c.Schedule,
		"priority":     c.Priority,
		"mode":         c.Mode,
		"target_agent": c.TargetAgent,
		"next_fire":    c.NextFire,
		"message":      fmt.Sprintf("Cadence %q created (mode=%s).", c.Name, c.Mode),
	}), nil
}

func (h *EventHandler) updateCadence(_ context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Schedule string `json:"schedule"`
		Priority string `json:"priority"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.EventHandler.updateCadence: %w", err)
	}
	if args.ID == "" {
		return types.NewErrorResult("id is required"), nil
	}
	if h.pulseEngine == nil {
		return types.NewErrorResult("pulse engine not available"), nil
	}

	if err := h.pulseEngine.UpdateCadence(args.ID, args.Name, args.Schedule, types.PulsePriority(args.Priority)); err != nil {
		return types.NewErrorResult(fmt.Sprintf("update cadence: %v", err)), nil
	}

	return types.NewToolResult(fmt.Sprintf("Cadence %q updated.", args.ID)), nil
}

func (h *EventHandler) deleteCadence(_ context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.EventHandler.deleteCadence: %w", err)
	}
	if args.ID == "" {
		return types.NewErrorResult("id is required"), nil
	}
	if h.pulseEngine == nil {
		return types.NewErrorResult("pulse engine not available"), nil
	}

	if err := h.pulseEngine.DeleteCadence(args.ID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("delete cadence: %v", err)), nil
	}

	return types.NewToolResult(fmt.Sprintf("Cadence %q deleted.", args.ID)), nil
}

func (h *EventHandler) getPulseStatus(_ context.Context, _ json.RawMessage) (*types.ToolResult, error) {
	if h.pulseEngine == nil {
		return types.NewErrorResult("pulse engine not available"), nil
	}
	return types.NewToolResult(h.pulseEngine.GetStatus()), nil
}

// ── Sensor cadence actions ────────────────────────────────────────────────────

func (h *EventHandler) createSensorCadence(_ context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Name     string                    `json:"name"`
		Schedule string                    `json:"schedule"`
		Action   types.SensorAction        `json:"sensor_action"`
		Criteria []types.MatchCriteria     `json:"criteria"`
		Event    types.SensorEventConfig   `json:"event_config"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.EventHandler.createSensorCadence: %w", err)
	}
	if args.Name == "" {
		return types.NewErrorResult("name is required"), nil
	}
	if args.Schedule == "" {
		return types.NewErrorResult("schedule is required"), nil
	}
	if args.Event.EventType == "" {
		return types.NewErrorResult("event_config.event_type is required"), nil
	}
	if h.sensorMgr == nil {
		return types.NewErrorResult("sensor manager not available"), nil
	}

	sensor, err := h.sensorMgr.CreateSensor(args.Name, args.Schedule, args.Action, args.Criteria, args.Event)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("create sensor: %v", err)), nil
	}

	return types.NewToolResult(map[string]any{
		"id":        sensor.ID,
		"name":      sensor.Name,
		"schedule":  sensor.Schedule,
		"action":    sensor.Action.Type,
		"criteria":  len(sensor.Criteria),
		"event":     string(sensor.Event.EventType),
		"next_fire": sensor.NextFire,
		"message":   fmt.Sprintf("Sensor %q created.", sensor.Name),
	}), nil
}

func (h *EventHandler) updateSensorCadence(_ context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ID       string                     `json:"id"`
		Name     string                     `json:"name"`
		Schedule string                     `json:"schedule"`
		Action   *types.SensorAction        `json:"sensor_action"`
		Criteria []types.MatchCriteria      `json:"criteria"`
		Event    *types.SensorEventConfig   `json:"event_config"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.EventHandler.updateSensorCadence: %w", err)
	}
	if args.ID == "" {
		return types.NewErrorResult("id is required"), nil
	}
	if h.sensorMgr == nil {
		return types.NewErrorResult("sensor manager not available"), nil
	}

	if err := h.sensorMgr.UpdateSensor(args.ID, args.Name, args.Schedule, args.Action, args.Criteria, args.Event); err != nil {
		return types.NewErrorResult(fmt.Sprintf("update sensor: %v", err)), nil
	}

	return types.NewToolResult(fmt.Sprintf("Sensor %q updated.", args.ID)), nil
}

func (h *EventHandler) listSensorCadences(_ context.Context, _ json.RawMessage) (*types.ToolResult, error) {
	if h.sensorMgr == nil {
		return types.NewErrorResult("sensor manager not available"), nil
	}
	sensors := h.sensorMgr.ListSensors()
	if len(sensors) == 0 {
		return types.NewToolResult("No sensor cadences configured."), nil
	}
	return types.NewToolResult(sensors), nil
}

func (h *EventHandler) deleteSensorCadence(_ context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.EventHandler.deleteSensorCadence: %w", err)
	}
	if args.ID == "" {
		return types.NewErrorResult("id is required"), nil
	}
	if h.sensorMgr == nil {
		return types.NewErrorResult("sensor manager not available"), nil
	}

	if err := h.sensorMgr.DeleteSensor(args.ID); err != nil {
		return types.NewErrorResult(fmt.Sprintf("delete sensor: %v", err)), nil
	}

	return types.NewToolResult(fmt.Sprintf("Sensor %q deleted.", args.ID)), nil
}
