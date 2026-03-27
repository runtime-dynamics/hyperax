package api

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/internal/storage"
	"github.com/hyperax/hyperax/pkg/types"
)

// ChannelsAPI handles REST endpoints for the hyperax-bridge binary.
// These endpoints are NOT for the dashboard (which uses MCP tools).
type ChannelsAPI struct {
	store  *storage.Store
	bus    *nervous.EventBus
	logger *slog.Logger
}

// NewChannelsAPI creates a ChannelsAPI with all required dependencies.
func NewChannelsAPI(store *storage.Store, bus *nervous.EventBus, logger *slog.Logger) *ChannelsAPI {
	return &ChannelsAPI{
		store:  store,
		bus:    bus,
		logger: logger,
	}
}

// Routes returns the chi router for channel bridge endpoints.
func (a *ChannelsAPI) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/register", a.Register)
	r.Route("/{sessionID}", func(r chi.Router) {
		r.Post("/heartbeat", a.Heartbeat)
		r.Get("/events", a.PollEvents)
		r.Post("/reply", a.Reply)
		r.Post("/permission", a.ReceivePermission)
		r.Delete("/", a.Deregister)
	})
	return r
}

// registerRequest is the JSON body for the Register endpoint.
type registerRequest struct {
	Name          string `json:"name"`
	WorkspaceID   string `json:"workspace_id"`
	BridgeVersion string `json:"bridge_version"`
	Metadata      string `json:"metadata"`
}

// Register creates a new channel session for a connecting bridge instance.
// Returns the generated session ID.
func (a *ChannelsAPI) Register(w http.ResponseWriter, r *http.Request) {
	var body registerRequest
	if err := decodeBody(r, &body); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	if a.store.Channels == nil {
		respondError(w, r, http.StatusServiceUnavailable, "channel system not available")
		return
	}

	sessionID := channelAPIGenerateID()
	now := time.Now()
	session := &repo.ChannelSession{
		ID:            sessionID,
		Name:          body.Name,
		WorkspaceID:   body.WorkspaceID,
		Status:        "connected",
		BridgeVersion: body.BridgeVersion,
		Metadata:      body.Metadata,
		ConnectedAt:   now,
		LastHeartbeat: now,
	}

	if err := a.store.Channels.CreateSession(r.Context(), session); err != nil {
		a.logger.Error("failed to create channel session",
			"error", err,
		)
		respondError(w, r, http.StatusInternalServerError, "failed to create session")
		return
	}

	// Publish event for real-time dashboard updates.
	if a.bus != nil {
		a.bus.Publish(nervous.NewEvent(
			types.EventChannelSessionConnected,
			"channels_api",
			"global",
			map[string]string{
				"session_id":     sessionID,
				"name":           body.Name,
				"workspace_id":   body.WorkspaceID,
				"bridge_version": body.BridgeVersion,
			},
		))
	}

	a.logger.Info("channel session registered",
		"session_id", sessionID,
		"name", body.Name,
		"workspace_id", body.WorkspaceID,
	)

	respondJSON(w, r, http.StatusCreated, map[string]string{
		"session_id": sessionID,
	})
}

// Heartbeat updates the last_heartbeat timestamp for a session.
func (a *ChannelsAPI) Heartbeat(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		respondError(w, r, http.StatusBadRequest, "session_id is required")
		return
	}

	if a.store.Channels == nil {
		respondError(w, r, http.StatusServiceUnavailable, "channel system not available")
		return
	}

	if err := a.store.Channels.UpdateHeartbeat(r.Context(), sessionID); err != nil {
		a.logger.Error("failed to update channel heartbeat",
			"session_id", sessionID,
			"error", err,
		)
		respondError(w, r, http.StatusInternalServerError, "failed to update heartbeat")
		return
	}

	// Publish heartbeat event for monitoring.
	if a.bus != nil {
		a.bus.Publish(nervous.NewEvent(
			types.EventChannelSessionHeartbeat,
			"channels_api",
			"global",
			map[string]string{
				"session_id": sessionID,
			},
		))
	}

	w.WriteHeader(http.StatusOK)
}

// PollEvents dequeues pending events for a session (limit 10) and marks them
// as delivered. Returns an empty array when no events are pending.
func (a *ChannelsAPI) PollEvents(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		respondError(w, r, http.StatusBadRequest, "session_id is required")
		return
	}

	if a.store.Channels == nil {
		respondError(w, r, http.StatusServiceUnavailable, "channel system not available")
		return
	}

	events, err := a.store.Channels.DequeueEvents(r.Context(), sessionID, 10)
	if err != nil {
		a.logger.Error("failed to dequeue channel events",
			"session_id", sessionID,
			"error", err,
		)
		respondError(w, r, http.StatusInternalServerError, "failed to poll events")
		return
	}

	// Mark each dequeued event as delivered.
	for _, event := range events {
		if markErr := a.store.Channels.MarkEventDelivered(r.Context(), event.ID); markErr != nil {
			a.logger.Error("failed to mark channel event delivered",
				"event_id", event.ID,
				"error", markErr,
			)
		}

		if a.bus != nil {
			a.bus.Publish(nervous.NewEvent(
				types.EventChannelEventDelivered,
				"channels_api",
				"global",
				map[string]string{
					"session_id": sessionID,
					"event_id":   event.ID,
					"event_type": event.EventType,
				},
			))
		}
	}

	respondJSON(w, r, http.StatusOK, map[string]any{
		"events": events,
		"count":  len(events),
	})
}

// replyRequest is the JSON body for the Reply endpoint.
type replyRequest struct {
	Content  string `json:"content"`
	Sender   string `json:"sender"`
	Metadata string `json:"metadata"`
	TaskID   string `json:"task_id,omitempty"`
	Status   string `json:"status,omitempty"`
}

// Reply creates an inbound message from the bridge (Claude Code response).
// If task_id and status are included, the corresponding task status is updated.
func (a *ChannelsAPI) Reply(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		respondError(w, r, http.StatusBadRequest, "session_id is required")
		return
	}

	var body replyRequest
	if err := decodeBody(r, &body); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Content == "" {
		respondError(w, r, http.StatusBadRequest, "content is required")
		return
	}

	if a.store.Channels == nil {
		respondError(w, r, http.StatusServiceUnavailable, "channel system not available")
		return
	}

	sender := body.Sender
	if sender == "" {
		sender = "claude-code"
	}

	msgID := channelAPIGenerateID()
	msg := &repo.ChannelMessage{
		ID:        msgID,
		SessionID: sessionID,
		Direction: "inbound",
		Content:   body.Content,
		Sender:    sender,
		Metadata:  body.Metadata,
		CreatedAt: time.Now(),
	}
	if err := a.store.Channels.CreateMessage(r.Context(), msg); err != nil {
		a.logger.Error("failed to create channel reply message",
			"session_id", sessionID,
			"error", err,
		)
		respondError(w, r, http.StatusInternalServerError, "failed to store reply")
		return
	}

	// Publish event for real-time dashboard updates.
	if a.bus != nil {
		a.bus.Publish(nervous.NewEvent(
			types.EventChannelReplyReceived,
			"channels_api",
			"global",
			map[string]string{
				"session_id": sessionID,
				"message_id": msgID,
				"sender":     sender,
			},
		))
	}

	// If the reply includes task status, update the task.
	if body.TaskID != "" && body.Status != "" {
		if a.store.Projects != nil {
			if err := a.store.Projects.UpdateTaskStatus(r.Context(), body.TaskID, body.Status); err != nil {
				a.logger.Error("failed to update task status from channel reply",
					"task_id", body.TaskID,
					"status", body.Status,
					"error", err,
				)
				// Non-fatal: the reply was stored, task update is best-effort.
			} else if a.bus != nil {
				a.bus.Publish(nervous.NewEvent(
					types.EventChannelTaskCompleted,
					"channels_api",
					"global",
					map[string]string{
						"session_id": sessionID,
						"task_id":    body.TaskID,
						"status":     body.Status,
					},
				))
			}
		}
	}

	a.logger.Info("channel reply received",
		"session_id", sessionID,
		"message_id", msgID,
	)

	respondJSON(w, r, http.StatusCreated, map[string]string{
		"message_id": msgID,
	})
}

// permissionRequest is the JSON body for the ReceivePermission endpoint.
type permissionRequest struct {
	RequestID    string `json:"request_id"`
	ToolName     string `json:"tool_name"`
	Description  string `json:"description"`
	InputPreview string `json:"input_preview"`
}

// ReceivePermission creates a permission request record from the bridge.
// Claude Code is asking the dashboard operator to approve a tool invocation.
func (a *ChannelsAPI) ReceivePermission(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		respondError(w, r, http.StatusBadRequest, "session_id is required")
		return
	}

	var body permissionRequest
	if err := decodeBody(r, &body); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.RequestID == "" {
		respondError(w, r, http.StatusBadRequest, "request_id is required")
		return
	}
	if body.ToolName == "" {
		respondError(w, r, http.StatusBadRequest, "tool_name is required")
		return
	}

	if a.store.Channels == nil {
		respondError(w, r, http.StatusServiceUnavailable, "channel system not available")
		return
	}

	permID := channelAPIGenerateID()
	perm := &repo.ChannelPermission{
		ID:           permID,
		SessionID:    sessionID,
		RequestID:    body.RequestID,
		ToolName:     body.ToolName,
		Description:  body.Description,
		InputPreview: body.InputPreview,
		Status:       "pending",
		CreatedAt:    time.Now(),
	}

	if err := a.store.Channels.CreatePermission(r.Context(), perm); err != nil {
		a.logger.Error("failed to create channel permission",
			"session_id", sessionID,
			"request_id", body.RequestID,
			"error", err,
		)
		respondError(w, r, http.StatusInternalServerError, "failed to create permission request")
		return
	}

	// Publish event for real-time dashboard updates.
	if a.bus != nil {
		a.bus.Publish(nervous.NewEvent(
			types.EventChannelPermissionRequested,
			"channels_api",
			"global",
			map[string]string{
				"session_id": sessionID,
				"request_id": body.RequestID,
				"tool_name":  body.ToolName,
			},
		))
	}

	a.logger.Info("channel permission request received",
		"session_id", sessionID,
		"request_id", body.RequestID,
		"tool_name", body.ToolName,
	)

	respondJSON(w, r, http.StatusCreated, map[string]any{
		"id":         permID,
		"request_id": body.RequestID,
		"status":     "pending",
	})
}

// Deregister disconnects a channel session.
func (a *ChannelsAPI) Deregister(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		respondError(w, r, http.StatusBadRequest, "session_id is required")
		return
	}

	if a.store.Channels == nil {
		respondError(w, r, http.StatusServiceUnavailable, "channel system not available")
		return
	}

	if err := a.store.Channels.UpdateSessionStatus(r.Context(), sessionID, "disconnected"); err != nil {
		a.logger.Error("failed to deregister channel session",
			"session_id", sessionID,
			"error", err,
		)
		respondError(w, r, http.StatusInternalServerError, "failed to deregister session")
		return
	}

	// Publish event for real-time dashboard updates.
	if a.bus != nil {
		a.bus.Publish(nervous.NewEvent(
			types.EventChannelSessionDisconnected,
			"channels_api",
			"global",
			map[string]string{
				"session_id": sessionID,
				"reason":     "bridge_deregister",
			},
		))
	}

	a.logger.Info("channel session deregistered",
		"session_id", sessionID,
	)

	w.WriteHeader(http.StatusNoContent)
}

// channelAPIGenerateID creates a random 16-byte hex ID for channel entities.
func channelAPIGenerateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

