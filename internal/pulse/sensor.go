package pulse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hyperax/hyperax/internal/cron"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/pkg/types"
)

const (
	// defaultSensorTimeout is the default execution timeout for sensor actions.
	defaultSensorTimeout = 10 * time.Second

	// maxResponseBytes is the maximum response body size read from HTTP sensors.
	maxResponseBytes = 1 << 20 // 1 MiB
)

// SecretResolver resolves secret references (e.g., "secret:api_key") to their
// actual values using the SecretRepo.
type SecretResolver func(ctx context.Context, ref string) (string, error)

// SensorManager manages sensor cadences — mechanical polling tasks that execute
// HTTP requests or shell commands, evaluate responses against match criteria,
// and inject events into the Nervous System when conditions are met.
//
// Unlike regular cadences which fire events for agents to process, sensors
// are fully autonomous: they execute, evaluate, and publish without any
// agent involvement.
type SensorManager struct {
	sensors   map[string]*types.SensorCadence
	mu        sync.RWMutex
	bus       *nervous.EventBus
	logger    *slog.Logger
	client    *http.Client
	resolver  SecretResolver
	evaluator *JSONPathEvaluator
	nowFunc   func() time.Time
}

// NewSensorManager creates a SensorManager wired to the EventBus.
func NewSensorManager(bus *nervous.EventBus, logger *slog.Logger) *SensorManager {
	return &SensorManager{
		sensors: make(map[string]*types.SensorCadence),
		bus:     bus,
		logger:  logger.With("component", "sensor"),
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		evaluator: NewJSONPathEvaluator(),
		nowFunc:   time.Now,
	}
}

// SetSecretResolver configures secret resolution for HTTP headers that
// reference secrets via "secret:key_name" syntax.
func (sm *SensorManager) SetSecretResolver(fn SecretResolver) {
	sm.resolver = fn
}

// BuildSecretResolver creates a SecretResolver from a SecretRepo.
// Returns nil if the repo is nil.
func BuildSecretResolver(secrets repo.SecretRepo) SecretResolver {
	if secrets == nil {
		return nil
	}
	return func(ctx context.Context, ref string) (string, error) {
		// ref format: "secret:key_name" or "secret:key_name:scope"
		// For now, use global scope.
		return secrets.Get(ctx, ref, "global")
	}
}

// Tick evaluates all enabled sensors and executes those whose schedule is due.
// Called by the Pulse Engine on each tick cycle.
func (sm *SensorManager) Tick(ctx context.Context) {
	now := sm.nowFunc()

	sm.mu.RLock()
	ids := make([]string, 0, len(sm.sensors))
	for id := range sm.sensors {
		ids = append(ids, id)
	}
	sm.mu.RUnlock()

	for _, id := range ids {
		sm.evaluateSensor(ctx, id, now)
	}
}

// evaluateSensor checks a single sensor and executes it if due.
func (sm *SensorManager) evaluateSensor(ctx context.Context, id string, now time.Time) {
	sm.mu.Lock()
	s, ok := sm.sensors[id]
	if !ok || !s.Enabled || s.NextFire == nil || now.Before(*s.NextFire) {
		sm.mu.Unlock()
		return
	}

	// Snapshot sensor config for execution outside the lock.
	sensor := *s
	fired := now
	s.LastFired = &fired

	// Compute next fire time.
	sched, err := cron.Parse(s.Schedule)
	if err != nil {
		sm.mu.Unlock()
		sm.publishSensorError(s.Name, id, fmt.Sprintf("schedule parse error: %v", err))
		return
	}
	next := sched.NextAfter(now)
	s.NextFire = &next
	sm.mu.Unlock()

	// Execute the sensor action.
	sm.executeSensor(ctx, id, &sensor)
}

// executeSensor runs the sensor's action and evaluates the response.
func (sm *SensorManager) executeSensor(ctx context.Context, id string, sensor *types.SensorCadence) {
	timeout := time.Duration(sensor.Action.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = defaultSensorTimeout
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var responseBody string
	var err error

	switch sensor.Action.Type {
	case "http":
		responseBody, err = sm.executeHTTP(execCtx, &sensor.Action)
	case "shell":
		responseBody, err = sm.executeShell(execCtx, &sensor.Action)
	default:
		sm.publishSensorError(sensor.Name, id, fmt.Sprintf("unknown action type: %s", sensor.Action.Type))
		return
	}

	// Publish fire event.
	sm.bus.Publish(nervous.NewEvent(
		types.EventSensorFire,
		"sensor",
		"system",
		map[string]any{
			"sensor_id":   id,
			"sensor_name": sensor.Name,
			"action_type": sensor.Action.Type,
		},
	))

	if err != nil {
		sm.mu.Lock()
		if s, ok := sm.sensors[id]; ok {
			s.LastResult = fmt.Sprintf("error: %v", err)
			s.LastMatched = false
		}
		sm.mu.Unlock()

		if execCtx.Err() == context.DeadlineExceeded {
			sm.publishSensorTimeout(sensor.Name, id)
		} else {
			sm.publishSensorError(sensor.Name, id, err.Error())
		}
		return
	}

	// Store last result.
	sm.mu.Lock()
	if s, ok := sm.sensors[id]; ok {
		s.LastResult = responseBody
	}
	sm.mu.Unlock()

	// Evaluate match criteria.
	matched := sm.evaluator.EvaluateAll(responseBody, sensor.Criteria)

	sm.mu.Lock()
	if s, ok := sm.sensors[id]; ok {
		s.LastMatched = matched
	}
	sm.mu.Unlock()

	if matched && sensor.Event.EventType != "" {
		sm.injectEvent(sensor, responseBody)
	}

	sm.logger.Debug("sensor executed",
		"id", id,
		"name", sensor.Name,
		"matched", matched,
	)
}

// executeHTTP performs an HTTP request and returns the response body.
func (sm *SensorManager) executeHTTP(ctx context.Context, action *types.SensorAction) (string, error) {
	method := action.Method
	if method == "" {
		method = "GET"
	}

	var body io.Reader
	if action.Body != "" {
		body = bytes.NewBufferString(action.Body)
	}

	req, err := http.NewRequestWithContext(ctx, method, action.URL, body)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	// Set headers, resolving secret references.
	for key, val := range action.Headers {
		resolved, resolveErr := sm.resolveValue(ctx, val)
		if resolveErr != nil {
			sm.logger.Warn("failed to resolve header secret",
				"header", key,
				"error", resolveErr,
			)
			continue
		}
		req.Header.Set(key, resolved)
	}

	resp, err := sm.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			sm.logger.Warn("executeHTTP: failed to close response body", "error", cerr)
		}
	}()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	return string(data), nil
}

// executeShell runs a shell command and returns stdout.
func (sm *SensorManager) executeShell(ctx context.Context, action *types.SensorAction) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", action.Command)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("shell command: %w", err)
	}
	return string(out), nil
}

// resolveValue resolves a value that may contain a "secret:" prefix.
func (sm *SensorManager) resolveValue(ctx context.Context, val string) (string, error) {
	if len(val) > 7 && val[:7] == "secret:" {
		if sm.resolver == nil {
			return "", fmt.Errorf("secret resolver not configured")
		}
		return sm.resolver(ctx, val[7:])
	}
	return val, nil
}

// injectEvent publishes a matched sensor event on the EventBus.
func (sm *SensorManager) injectEvent(sensor *types.SensorCadence, responseBody string) {
	var payload any

	if sensor.Event.PayloadFrom != "" {
		// Extract a subset of the response using JSONPath.
		extracted := sm.evaluator.Extract(responseBody, sensor.Event.PayloadFrom)
		if extracted != nil {
			payload = extracted
		} else {
			payload = responseBody
		}
	} else {
		// Use the full response as payload.
		var parsed any
		if err := json.Unmarshal([]byte(responseBody), &parsed); err == nil {
			payload = parsed
		} else {
			payload = responseBody
		}
	}

	sm.bus.Publish(nervous.NewEvent(
		sensor.Event.EventType,
		"sensor:"+sensor.Name,
		"system",
		map[string]any{
			"sensor_id":   sensor.ID,
			"sensor_name": sensor.Name,
			"payload":     payload,
		},
	))

	sm.bus.Publish(nervous.NewEvent(
		types.EventSensorMatch,
		"sensor",
		"system",
		map[string]any{
			"sensor_id":     sensor.ID,
			"sensor_name":   sensor.Name,
			"injected_type": string(sensor.Event.EventType),
		},
	))

	sm.logger.Info("sensor match — event injected",
		"sensor", sensor.Name,
		"event_type", sensor.Event.EventType,
	)
}

func (sm *SensorManager) publishSensorError(name, id, msg string) {
	sm.bus.Publish(nervous.NewEvent(
		types.EventSensorError,
		"sensor",
		"system",
		map[string]string{
			"sensor_id":   id,
			"sensor_name": name,
			"error":       msg,
		},
	))
	sm.logger.Warn("sensor error", "id", id, "name", name, "error", msg)
}

func (sm *SensorManager) publishSensorTimeout(name, id string) {
	sm.bus.Publish(nervous.NewEvent(
		types.EventSensorTimeout,
		"sensor",
		"system",
		map[string]string{
			"sensor_id":   id,
			"sensor_name": name,
		},
	))
	sm.logger.Warn("sensor timeout", "id", id, "name", name)
}

// --- CRUD operations ---

// CreateSensor adds a new sensor cadence. Returns the created sensor or an error.
func (sm *SensorManager) CreateSensor(name, schedule string, action types.SensorAction, criteria []MatchCriteria, event types.SensorEventConfig) (*types.SensorCadence, error) {
	if name == "" {
		return nil, fmt.Errorf("sensor name is required")
	}
	if schedule == "" {
		return nil, fmt.Errorf("sensor schedule is required")
	}
	if action.Type != "http" && action.Type != "shell" {
		return nil, fmt.Errorf("sensor action type must be 'http' or 'shell', got %q", action.Type)
	}
	if action.Type == "http" && action.URL == "" {
		return nil, fmt.Errorf("sensor URL is required for http actions")
	}
	if action.Type == "shell" && action.Command == "" {
		return nil, fmt.Errorf("sensor command is required for shell actions")
	}

	sched, err := cron.Parse(schedule)
	if err != nil {
		return nil, fmt.Errorf("invalid schedule %q: %w", schedule, err)
	}

	now := sm.nowFunc()
	next := sched.NextAfter(now)

	// Convert local MatchCriteria to types.MatchCriteria.
	typeCriteria := make([]types.MatchCriteria, len(criteria))
	for i, c := range criteria {
		typeCriteria[i] = types.MatchCriteria(c)
	}

	sensor := &types.SensorCadence{
		ID:        uuid.New().String(),
		Name:      name,
		Schedule:  schedule,
		Enabled:   true,
		Action:    action,
		Criteria:  typeCriteria,
		Event:     event,
		NextFire:  &next,
		CreatedAt: now,
	}

	sm.mu.Lock()
	sm.sensors[sensor.ID] = sensor
	sm.mu.Unlock()

	sm.logger.Info("sensor created", "id", sensor.ID, "name", name, "schedule", schedule, "action", action.Type)
	return sensor, nil
}

// UpdateSensor updates an existing sensor's configuration.
func (sm *SensorManager) UpdateSensor(id string, name, schedule string, action *types.SensorAction, criteria []MatchCriteria, event *types.SensorEventConfig) error {
	if id == "" {
		return fmt.Errorf("sensor id is required")
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	s, ok := sm.sensors[id]
	if !ok {
		return fmt.Errorf("sensor %q not found", id)
	}

	if name != "" {
		s.Name = name
	}
	if schedule != "" && schedule != s.Schedule {
		sched, err := cron.Parse(schedule)
		if err != nil {
			return fmt.Errorf("invalid schedule %q: %w", schedule, err)
		}
		s.Schedule = schedule
		next := sched.NextAfter(sm.nowFunc())
		s.NextFire = &next
	}
	if action != nil {
		s.Action = *action
	}
	if criteria != nil {
		typeCriteria := make([]types.MatchCriteria, len(criteria))
		for i, c := range criteria {
			typeCriteria[i] = types.MatchCriteria(c)
		}
		s.Criteria = typeCriteria
	}
	if event != nil {
		s.Event = *event
	}

	sm.logger.Info("sensor updated", "id", id, "name", s.Name)
	return nil
}

// DeleteSensor removes a sensor by ID.
func (sm *SensorManager) DeleteSensor(id string) error {
	if id == "" {
		return fmt.Errorf("sensor id is required")
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, ok := sm.sensors[id]; !ok {
		return fmt.Errorf("sensor %q not found", id)
	}
	delete(sm.sensors, id)

	sm.logger.Info("sensor deleted", "id", id)
	return nil
}

// ListSensors returns a snapshot of all sensor cadences.
func (sm *SensorManager) ListSensors() []types.SensorCadence {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make([]types.SensorCadence, 0, len(sm.sensors))
	for _, s := range sm.sensors {
		result = append(result, *s)
	}
	return result
}

// GetSensor returns a copy of a sensor by ID.
func (sm *SensorManager) GetSensor(id string) (*types.SensorCadence, error) {
	if id == "" {
		return nil, fmt.Errorf("sensor id is required")
	}

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	s, ok := sm.sensors[id]
	if !ok {
		return nil, fmt.Errorf("sensor %q not found", id)
	}

	copy := *s
	return &copy, nil
}
