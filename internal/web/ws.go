package web

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
	"github.com/hyperax/hyperax/internal/auth"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/web/render"
	"github.com/hyperax/hyperax/pkg/types"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// wsFilterMessage is the JSON structure clients send to set event filters.
// Clients send {"type":"subscribe","patterns":["pipeline.*","lifecycle.*"]}
// to filter which events they receive. An empty patterns list or "*" receives all.
type wsFilterMessage struct {
	Type     string   `json:"type"`
	Patterns []string `json:"patterns"`
}

// wsClientState tracks per-connection filter patterns.
type wsClientState struct {
	conn     *websocket.Conn
	patterns []string // glob patterns; empty = receive all
	mu       sync.RWMutex
}

// matchesEvent returns true if the event matches any of the client's filter
// patterns. If no patterns are set, all events match.
func (c *wsClientState) matchesEvent(eventType string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.patterns) == 0 {
		return true
	}
	for _, p := range c.patterns {
		if nervous.MatchEventType(p, types.EventType(eventType)) {
			return true
		}
	}
	return false
}

// setPatterns updates the client's filter patterns.
func (c *wsClientState) setPatterns(patterns []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.patterns = patterns
}

// WSHub bridges the EventBus to WebSocket clients.
// It supports late-join replay via RingBuffer: clients connecting with
// ?since=N receive all buffered events with SequenceID > N before
// switching to the live stream.
//
// Clients can send filter messages to control which events they receive:
//
//	{"type":"subscribe","patterns":["pipeline.*","lifecycle.*"]}
//
// When a TokenIssuer is set, JWT authentication is required for WebSocket
// upgrade. The JWT is extracted from the Authorization header (Bearer scheme)
// or the ?token query parameter. Validated claims are logged with the
// connection for audit purposes.
type WSHub struct {
	bus                   *nervous.EventBus
	ringBuffer            *nervous.RingBuffer
	logger                *slog.Logger
	clients               sync.Map // clientID -> *wsClientState
	nextID                atomic.Int64
	jwtIssuer             *auth.TokenIssuer
	disableLoopbackExempt bool // for testing: force JWT checks even on loopback
}

// NewWSHub creates a WebSocket hub connected to the EventBus and RingBuffer.
// The ringBuffer provides late-join replay for newly connected clients.
func NewWSHub(bus *nervous.EventBus, ringBuffer *nervous.RingBuffer, logger *slog.Logger) *WSHub {
	return &WSHub{
		bus:        bus,
		ringBuffer: ringBuffer,
		logger:     logger,
	}
}

// SetJWTIssuer enables JWT authentication on WebSocket upgrade.
// When set, clients must present a valid JWT via Authorization header
// or ?token query parameter. When nil, no authentication is enforced.
func (h *WSHub) SetJWTIssuer(issuer *auth.TokenIssuer) {
	h.jwtIssuer = issuer
}

// HandleWebSocket upgrades an HTTP connection to WebSocket and streams events.
// Clients may pass ?since=N to replay all buffered events with SequenceID > N
// before switching to the live stream, enabling late-join catch-up.
//
// When JWT authentication is enabled (via SetJWTIssuer), the handler extracts
// and validates a JWT from the Authorization header or ?token query parameter
// before performing the WebSocket upgrade. Invalid or missing JWTs result in
// an HTTP 401 response and no upgrade.
func (h *WSHub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// JWT validation gate: when a TokenIssuer is configured, require a valid JWT.
	// Localhost connections (the embedded dashboard) are exempt from JWT auth
	// since they originate from the same machine and have no way to acquire a
	// token without an external MCP client.
	var claims *auth.Claims
	if h.jwtIssuer != nil && (h.disableLoopbackExempt || !isLoopback(r)) {
		tokenStr, err := auth.ExtractJWT(r)
		if err != nil {
			h.logger.Warn("websocket auth: missing jwt", "error", err, "remote", r.RemoteAddr)
			render.Error(w, r, "authentication required", http.StatusUnauthorized)
			return
		}

		claims, err = h.jwtIssuer.Validate(tokenStr)
		if err != nil {
			h.logger.Warn("websocket auth: invalid jwt", "error", err, "remote", r.RemoteAddr)
			render.Error(w, r, "invalid or expired token", http.StatusUnauthorized)
			return
		}
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("websocket upgrade failed", "error", err)
		return
	}

	clientID := h.nextID.Add(1)
	subID := fmt.Sprintf("ws-client-%d", clientID)

	// Parse optional ?since=N query parameter for late-join replay.
	var sinceSeq uint64
	if s := r.URL.Query().Get("since"); s != "" {
		if v, err := strconv.ParseUint(s, 10, 64); err == nil {
			sinceSeq = v
		}
	}

	// Log connection with auth context when available.
	if claims != nil {
		h.logger.Info("websocket client connected",
			"client_id", clientID,
			"since", sinceSeq,
			"persona_id", claims.PersonaID,
			"clearance_level", claims.ClearanceLevel,
		)
	} else {
		h.logger.Info("websocket client connected", "client_id", clientID, "since", sinceSeq)
	}

	// Parse optional ?filter query parameter for initial event filtering.
	// Example: ?filter=pipeline.*,lifecycle.*
	var initialPatterns []string
	if f := r.URL.Query().Get("filter"); f != "" {
		for _, p := range strings.Split(f, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				initialPatterns = append(initialPatterns, p)
			}
		}
	}

	client := &wsClientState{
		conn:     conn,
		patterns: initialPatterns,
	}
	h.clients.Store(clientID, client)

	// Late-join replay: send buffered events before subscribing to live stream.
	if h.ringBuffer != nil {
		missed := h.ringBuffer.Replay(sinceSeq)
		for _, event := range missed {
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				_ = conn.Close()
				h.clients.Delete(clientID)
				h.logger.Warn("websocket replay write failed", "client_id", clientID, "error", err)
				return
			}
		}
		h.logger.Info("websocket late-join replay complete", "client_id", clientID, "replayed", len(missed))
	}

	// Subscribe to live events after replay is complete.
	sub := h.bus.Subscribe(subID, nil)

	// Write events to WebSocket, applying per-client filters.
	go func() {
		for event := range sub.Ch {
			// Apply subscription filter.
			if !client.matchesEvent(string(event.Type)) {
				continue
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		}
	}()

	// Read pump: handles disconnect detection and filter update messages.
	// Clients can send {"type":"subscribe","patterns":["pipeline.*"]} to
	// dynamically update their event filter.
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				h.bus.Unsubscribe(subID)
				_ = conn.Close()
				h.clients.Delete(clientID)
				h.logger.Info("websocket client disconnected", "client_id", clientID)
				return
			}

			// Try to parse as a filter message.
			var filterMsg wsFilterMessage
			if err := json.Unmarshal(msg, &filterMsg); err != nil {
				continue
			}
			if filterMsg.Type == "subscribe" {
				client.setPatterns(filterMsg.Patterns)
				h.logger.Info("websocket client updated filters",
					"client_id", clientID,
					"patterns", filterMsg.Patterns,
				)
			}
		}
	}()
}

// isLoopback returns true when the request originates from a loopback address
// (127.0.0.0/8 or ::1). Used to exempt the embedded dashboard from JWT auth.
func isLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
