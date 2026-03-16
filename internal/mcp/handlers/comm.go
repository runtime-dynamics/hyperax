package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hyperax/hyperax/internal/agentmail"
	"github.com/hyperax/hyperax/internal/commhub"
	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/internal/nervous"
	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/internal/storage"
	"github.com/hyperax/hyperax/pkg/types"
)

// CompletionTriggerFunc triggers async LLM completion for an agent upon
// receiving a message. The function spawns its own goroutine internally.
type CompletionTriggerFunc func(agentName, senderID, content, sessionID string)

// actionClearanceComm maps each comm action to its minimum ABAC clearance.
var actionClearanceComm = map[string]int{
	// CommHub core
	"send":         0, // was send_message: clearance 0
	"receive":      0, // was receive_messages: clearance 0
	"inbox_size":   0, // was inbox_size: clearance 0
	"list_inboxes": 0, // was list_agent_inboxes: clearance 0

	// CommHub extended
	"get_log":          0, // was get_comm_log: clearance 0
	"set_hierarchy":    1, // was set_agent_hierarchy: clearance 1
	"grant_permission": 1, // was grant_comm_permission: clearance 1
	"broadcast":        1, // was broadcast_internal_memo: clearance 1
	"get_hierarchy":    0, // was get_hierarchy: clearance 0

	// AgentMail
	"send_mail":          1, // was send_mail: clearance 1
	"list_inbox":         0, // was list_inbox: clearance 0
	"get_mail":           0, // was get_mail: clearance 0
	"ack_mail":           1, // was ack_mail: clearance 1
	"list_adapters":      0, // was list_adapters: clearance 0
	"configure_adapter":  3, // was configure_adapter: clearance 3
	"postbox_status":     0, // was get_postbox_status: clearance 0
	"list_dead_letters":  0, // was list_dead_letters: clearance 0
	"retry_dead_letter":  1, // was retry_dead_letter: clearance 1
	"discard_dead_letter": 1, // new: same clearance as retry
	"partition_status":   0, // was get_partition_status: clearance 0

	// Sessions
	"new_session":     1, // was new_chat_session: clearance 1
	"get_session":     0, // was get_active_session: clearance 0
	"list_sessions":   0, // was list_chat_sessions: clearance 0
	"archive_session": 1, // was archive_chat_session: clearance 1
}

// CommHandler implements the consolidated "comm" MCP tool, absorbing CommHub,
// CommHubExt, AgentMail, and Session handler functionality.
type CommHandler struct {
	// CommHub core
	hub               *commhub.CommHub
	commLog           *commhub.CommLogger
	store             *storage.Store
	completionTrigger CompletionTriggerFunc
	logger            *slog.Logger

	// CommHub extended
	commRepo  repo.CommHubRepo
	hierarchy *commhub.HierarchyManager
	bus       *nervous.EventBus
	agentRepo repo.AgentRepo

	// AgentMail
	postbox    *agentmail.Postbox
	amRegistry *agentmail.AdapterRegistry
	ackTracker *agentmail.AckTracker
	dlo        *agentmail.DeadLetterOffice

	// Sessions
	sessions repo.SessionRepo
}

// CommHandlerDeps holds all dependencies for the consolidated CommHandler.
type CommHandlerDeps struct {
	Hub       *commhub.CommHub
	CommLog   *commhub.CommLogger
	Store     *storage.Store
	Logger    *slog.Logger
	CommRepo  repo.CommHubRepo
	Hierarchy *commhub.HierarchyManager
	Bus       *nervous.EventBus
	AgentRepo repo.AgentRepo
	Postbox   *agentmail.Postbox
	Registry  *agentmail.AdapterRegistry
	Tracker   *agentmail.AckTracker
	DLO       *agentmail.DeadLetterOffice
	Sessions  repo.SessionRepo
}

// NewCommHandler creates a CommHandler with all dependencies.
func NewCommHandler(deps CommHandlerDeps) *CommHandler {
	return &CommHandler{
		hub:        deps.Hub,
		commLog:    deps.CommLog,
		store:      deps.Store,
		logger:     deps.Logger,
		commRepo:   deps.CommRepo,
		hierarchy:  deps.Hierarchy,
		bus:        deps.Bus,
		agentRepo:  deps.AgentRepo,
		postbox:    deps.Postbox,
		amRegistry: deps.Registry,
		ackTracker: deps.Tracker,
		dlo:        deps.DLO,
		sessions:   deps.Sessions,
	}
}

// SetCompletionTrigger wires the async LLM completion trigger that fires when
// an agent receives a message via the send action.
func (h *CommHandler) SetCompletionTrigger(fn CompletionTriggerFunc) {
	h.completionTrigger = fn
}

// SetExtDeps wires CommHub extended dependencies (hierarchy, permissions, broadcast).
func (h *CommHandler) SetExtDeps(commRepo repo.CommHubRepo, hierarchy *commhub.HierarchyManager, bus *nervous.EventBus, agentRepo repo.AgentRepo) {
	h.commRepo = commRepo
	h.hierarchy = hierarchy
	h.bus = bus
	h.agentRepo = agentRepo
}

// SetSessionDeps wires chat session management dependencies.
func (h *CommHandler) SetSessionDeps(sessions repo.SessionRepo) {
	h.sessions = sessions
}

// SetMailDeps wires AgentMail dependencies (postbox, adapters, ack tracker, DLO).
func (h *CommHandler) SetMailDeps(postbox *agentmail.Postbox, registry *agentmail.AdapterRegistry, tracker *agentmail.AckTracker, dlo *agentmail.DeadLetterOffice) {
	h.postbox = postbox
	h.amRegistry = registry
	h.ackTracker = tracker
	h.dlo = dlo
}

// RegisterTools registers the consolidated "comm" tool.
func (h *CommHandler) RegisterTools(registry *mcp.ToolRegistry) {
	registry.Register(
		"comm",
		"Communication management: agent messaging (send/receive/inbox), hierarchy, permissions, "+
			"broadcast, agent mail (send/ack/dead-letter), and chat sessions. "+
			"Actions: send | receive | inbox_size | list_inboxes | get_log | set_hierarchy | "+
			"grant_permission | broadcast | get_hierarchy | send_mail | list_inbox | get_mail | "+
			"ack_mail | list_adapters | configure_adapter | postbox_status | list_dead_letters | "+
			"retry_dead_letter | discard_dead_letter | partition_status | new_session | get_session | "+
			"list_sessions | archive_session",
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": [
					"send", "receive", "inbox_size", "list_inboxes",
					"get_log", "set_hierarchy", "grant_permission", "broadcast", "get_hierarchy",
					"send_mail", "list_inbox", "get_mail", "ack_mail",
					"list_adapters", "configure_adapter", "postbox_status",
					"list_dead_letters", "retry_dead_letter", "discard_dead_letter", "partition_status",
					"new_session", "get_session", "list_sessions", "archive_session"
				], "description": "Action to perform"},
				"from":           {"type": "string", "description": "Sender agent ID (send, broadcast, send_mail)"},
				"to":             {"type": "string", "description": "Recipient agent ID or instance (send, send_mail)"},
				"content":        {"type": "string", "description": "Message content (send, broadcast)"},
				"content_type":   {"type": "string", "description": "Content type: text, json, tool_call, tool_result (send, broadcast)", "default": "text"},
				"trust":          {"type": "string", "description": "Trust level: internal, authorized, external (send, broadcast)", "default": "internal"},
				"agent_id":       {"type": "string", "description": "Agent ID (receive, inbox_size, get_log, grant_permission)"},
				"limit":          {"type": "integer", "description": "Maximum results to return"},
				"peer_id":        {"type": "string", "description": "Peer agent ID (get_log, sessions)"},
				"parent_agent":   {"type": "string", "description": "Parent/supervisor agent ID (set_hierarchy)"},
				"child_agent":    {"type": "string", "description": "Child/subordinate agent ID (set_hierarchy)"},
				"relationship":   {"type": "string", "description": "Relationship type: supervisor, peer, delegate (set_hierarchy)", "default": "supervisor"},
				"target_id":      {"type": "string", "description": "Target agent ID for permission grant"},
				"permission":     {"type": "string", "description": "Permission type: send, receive, both (grant_permission)", "default": "both"},
				"workspace_id":   {"type": "string", "description": "Workspace scope (send_mail, partition_status)"},
				"priority":       {"type": "string", "enum": ["urgent", "standard", "background"], "description": "Message priority (send_mail)"},
				"payload":        {"type": "object", "description": "Arbitrary JSON payload (send_mail)"},
				"schema_id":      {"type": "string", "description": "Wire format schema identifier (send_mail)"},
				"mail_id":        {"type": "string", "description": "Mail message ID (get_mail, ack_mail)"},
				"instance_id":    {"type": "string", "description": "Instance ID (ack_mail)"},
				"status":         {"type": "string", "description": "Ack status (ack_mail)"},
				"adapter_name":   {"type": "string", "description": "Adapter name (configure_adapter)"},
				"adapter_action": {"type": "string", "enum": ["start", "stop", "health"], "description": "Adapter action (configure_adapter)"},
				"entry_id":       {"type": "string", "description": "DLO entry ID (retry_dead_letter, discard_dead_letter)"},
				"agent_name":     {"type": "string", "description": "Agent name (sessions)"},
				"session_id":     {"type": "string", "description": "Session ID (archive_session)"}
			},
			"required": ["action"]
		}`),
		h.dispatch,
	)
}

// dispatch routes the consolidated "comm" tool to the correct handler method.
func (h *CommHandler) dispatch(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var envelope struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return nil, fmt.Errorf("handlers.CommHandler.dispatch: %w", err)
	}

	if err := checkActionClearance(ctx, envelope.Action, actionClearanceComm); err != nil {
		return types.NewErrorResult(err.Error()), nil
	}

	switch envelope.Action {
	// CommHub core
	case "send":
		return h.sendMessage(ctx, params)
	case "receive":
		return h.receiveMessages(ctx, params)
	case "inbox_size":
		return h.inboxSize(ctx, params)
	case "list_inboxes":
		return h.listAgentInboxes(ctx, params)

	// CommHub extended
	case "get_log":
		return h.getCommLog(ctx, params)
	case "set_hierarchy":
		return h.setAgentHierarchy(ctx, params)
	case "grant_permission":
		return h.grantCommPermission(ctx, params)
	case "broadcast":
		return h.broadcastInternalMemo(ctx, params)
	case "get_hierarchy":
		return h.getHierarchy(ctx, params)

	// AgentMail
	case "send_mail":
		return h.sendMail(ctx, params)
	case "list_inbox":
		return h.listMailInbox(ctx, params)
	case "get_mail":
		return h.getMail(ctx, params)
	case "ack_mail":
		return h.ackMail(ctx, params)
	case "list_adapters":
		return h.listAdapters(ctx, params)
	case "configure_adapter":
		return h.configureAdapter(ctx, params)
	case "postbox_status":
		return h.getPostboxStatus(ctx, params)
	case "list_dead_letters":
		return h.listDeadLetters(ctx, params)
	case "retry_dead_letter":
		return h.retryDeadLetter(ctx, params)
	case "discard_dead_letter":
		return h.discardDeadLetter(ctx, params)
	case "partition_status":
		return h.getPartitionStatus(ctx, params)

	// Sessions
	case "new_session":
		return h.newChatSession(ctx, params)
	case "get_session":
		return h.getActiveSession(ctx, params)
	case "list_sessions":
		return h.listChatSessions(ctx, params)
	case "archive_session":
		return h.archiveChatSession(ctx, params)

	default:
		return types.NewErrorResult(fmt.Sprintf("unknown comm action %q", envelope.Action)), nil
	}
}

// ─── CommHub core methods ──────────────────────────────────────────────────

func (h *CommHandler) sendMessage(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		From        string `json:"from"`
		To          string `json:"to"`
		Content     string `json:"content"`
		ContentType string `json:"content_type"`
		Trust       string `json:"trust"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.CommHandler.sendMessage: %w", err)
	}
	if args.From == "" {
		return types.NewErrorResult("from is required"), nil
	}
	if args.To == "" {
		return types.NewErrorResult("to is required"), nil
	}
	if args.Content == "" {
		return types.NewErrorResult("content is required"), nil
	}

	if args.ContentType == "" {
		args.ContentType = "text"
	}

	trust := types.TrustInternal
	if args.Trust != "" {
		trust = types.ParseTrustLevel(args.Trust)
	}

	env := &types.MessageEnvelope{
		ID:          fmt.Sprintf("mcp-%s-%s", args.From, args.To),
		From:        args.From,
		To:          args.To,
		Trust:       trust,
		ContentType: args.ContentType,
		Content:     args.Content,
	}

	if err := h.hub.Send(ctx, env); err != nil {
		return types.NewErrorResult(fmt.Sprintf("send failed: %v", err)), nil
	}

	// Ensure a session exists for this conversation pair.
	sessionID := ""
	if h.store != nil && h.store.Sessions != nil {
		session, sessErr := h.store.Sessions.GetActiveSession(ctx, args.To, args.From)
		if sessErr == nil && session != nil {
			sessionID = session.ID
		} else {
			newID, createErr := h.store.Sessions.CreateSession(ctx, args.To, args.From)
			if createErr == nil {
				sessionID = newID
			}
		}
	}

	// Persist to comm_log for chat history.
	if h.commLog != nil {
		if sessionID != "" {
			if err := h.commLog.LogWithSession(ctx, env, "sent", sessionID); err != nil {
				h.logger.Error("failed to log communication", "from", args.From, "to", args.To, "session", sessionID, "error", err)
			}
		} else {
			if err := h.commLog.Log(ctx, env, "sent"); err != nil {
				h.logger.Error("failed to log communication", "from", args.From, "to", args.To, "error", err)
			}
		}
	}

	// Resolve recipient to display name for the work queue.
	recipientName := args.To
	if h.store != nil && h.store.Agents != nil {
		if resolved, err := h.store.Agents.Get(ctx, args.To); err == nil {
			recipientName = resolved.Name
		} else if resolved, err := h.store.Agents.GetByName(ctx, args.To); err == nil {
			recipientName = resolved.Name
		}
	}

	// Enqueue to the durable work queue for the Agent Scheduler.
	if h.store != nil && h.store.WorkQueue != nil {
		item := &types.WorkQueueItem{
			AgentName:   recipientName,
			FromAgent:   args.From,
			Content:     args.Content,
			ContentType: args.ContentType,
			Trust:       trust.String(),
			SessionID:   sessionID,
		}
		if err := h.store.WorkQueue.Enqueue(ctx, item); err != nil {
			h.logger.Error("failed to enqueue to work queue, falling back to direct trigger", "agent", recipientName, "error", err)
			if h.completionTrigger != nil {
				h.completionTrigger(recipientName, args.From, args.Content, sessionID)
			}
		}
	} else if h.completionTrigger != nil {
		h.completionTrigger(recipientName, args.From, args.Content, sessionID)
	}

	return types.NewToolResult(map[string]string{
		"status":  "delivered",
		"from":    args.From,
		"to":      args.To,
		"message": fmt.Sprintf("Message delivered from %s to %s.", args.From, args.To),
	}), nil
}

func (h *CommHandler) receiveMessages(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		AgentID string `json:"agent_id"`
		Limit   int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.CommHandler.receiveMessages: %w", err)
	}
	if args.AgentID == "" {
		return types.NewErrorResult("agent_id is required"), nil
	}
	if args.Limit <= 0 {
		args.Limit = 10
	}

	msgs := h.hub.Receive(args.AgentID, args.Limit)
	if len(msgs) == 0 {
		return types.NewToolResult([]interface{}{}), nil
	}

	return types.NewToolResult(msgs), nil
}

func (h *CommHandler) inboxSize(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.CommHandler.inboxSize: %w", err)
	}
	if args.AgentID == "" {
		return types.NewErrorResult("agent_id is required"), nil
	}

	size := h.hub.InboxSize(args.AgentID)

	return types.NewToolResult(map[string]interface{}{
		"agent_id": args.AgentID,
		"size":     size,
	}), nil
}

func (h *CommHandler) listAgentInboxes(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	inboxes := h.hub.ListInboxes()

	if len(inboxes) == 0 {
		return types.NewToolResult([]interface{}{}), nil
	}

	return types.NewToolResult(inboxes), nil
}

// ─── CommHub extended methods ──────────────────────────────────────────────

func (h *CommHandler) getCommLog(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		AgentID string `json:"agent_id"`
		PeerID  string `json:"peer_id"`
		Limit   int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.CommHandler.getCommLog: %w", err)
	}
	if args.AgentID == "" {
		return types.NewErrorResult("agent_id is required"), nil
	}
	if args.Limit <= 0 {
		args.Limit = 20
	}

	var entries []*types.CommLogEntry
	var err error

	if args.PeerID != "" {
		entries, err = h.commLog.GetLogBetween(ctx, args.AgentID, args.PeerID, args.Limit)
	} else {
		entries, err = h.commLog.GetLog(ctx, args.AgentID, args.Limit)
	}
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get comm log: %v", err)), nil
	}

	if len(entries) == 0 {
		return types.NewToolResult([]interface{}{}), nil
	}

	return types.NewToolResult(entries), nil
}

func (h *CommHandler) setAgentHierarchy(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		ParentAgent  string `json:"parent_agent"`
		ChildAgent   string `json:"child_agent"`
		Relationship string `json:"relationship"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.CommHandler.setAgentHierarchy: %w", err)
	}
	if args.ParentAgent == "" {
		return types.NewErrorResult("parent_agent is required"), nil
	}
	if args.ChildAgent == "" {
		return types.NewErrorResult("child_agent is required"), nil
	}
	if args.Relationship == "" {
		args.Relationship = "supervisor"
	}

	switch args.Relationship {
	case "supervisor", "peer", "delegate":
		// Valid.
	default:
		return types.NewErrorResult(fmt.Sprintf("invalid relationship type %q: must be supervisor, peer, or delegate", args.Relationship)), nil
	}

	rel := &types.AgentRelationship{
		ParentAgent:  args.ParentAgent,
		ChildAgent:   args.ChildAgent,
		Relationship: args.Relationship,
	}

	if err := h.hierarchy.SetRelationship(ctx, rel); err != nil {
		return types.NewErrorResult(fmt.Sprintf("set hierarchy: %v", err)), nil
	}

	return types.NewToolResult(map[string]string{
		"status":       "created",
		"parent_agent": args.ParentAgent,
		"child_agent":  args.ChildAgent,
		"relationship": args.Relationship,
		"message":      fmt.Sprintf("Relationship %s -> %s (%s) established.", args.ParentAgent, args.ChildAgent, args.Relationship),
	}), nil
}

func (h *CommHandler) grantCommPermission(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		AgentID    string `json:"agent_id"`
		TargetID   string `json:"target_id"`
		Permission string `json:"permission"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.CommHandler.grantCommPermission: %w", err)
	}
	if args.AgentID == "" {
		return types.NewErrorResult("agent_id is required"), nil
	}
	if args.TargetID == "" {
		return types.NewErrorResult("target_id is required"), nil
	}
	if args.Permission == "" {
		args.Permission = "both"
	}

	switch args.Permission {
	case "send", "receive", "both":
		// Valid.
	default:
		return types.NewErrorResult(fmt.Sprintf("invalid permission %q: must be send, receive, or both", args.Permission)), nil
	}

	if h.commRepo == nil {
		return types.NewErrorResult("commhub repo not available"), nil
	}

	perm := &types.CommPermission{
		AgentID:    args.AgentID,
		TargetID:   args.TargetID,
		Permission: args.Permission,
	}

	if err := h.commRepo.GrantPermission(ctx, perm); err != nil {
		return types.NewErrorResult(fmt.Sprintf("grant permission: %v", err)), nil
	}

	h.bus.Publish(nervous.NewEvent(
		types.EventCommPermChanged,
		"commhub_ext",
		args.AgentID,
		map[string]string{
			"agent_id":   args.AgentID,
			"target_id":  args.TargetID,
			"permission": args.Permission,
			"action":     "grant",
		},
	))

	return types.NewToolResult(map[string]string{
		"status":  "granted",
		"message": fmt.Sprintf("Permission %q granted: %s -> %s.", args.Permission, args.AgentID, args.TargetID),
	}), nil
}

func (h *CommHandler) broadcastInternalMemo(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		From        string `json:"from"`
		Content     string `json:"content"`
		ContentType string `json:"content_type"`
		Trust       string `json:"trust"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.CommHandler.broadcastInternalMemo: %w", err)
	}
	if args.From == "" {
		return types.NewErrorResult("from is required"), nil
	}
	if args.Content == "" {
		return types.NewErrorResult("content is required"), nil
	}
	if args.ContentType == "" {
		args.ContentType = "text"
	}

	trust := types.TrustInternal
	if args.Trust != "" {
		trust = types.ParseTrustLevel(args.Trust)
	}

	inboxes := h.hub.ListInboxes()

	delivered := 0
	var failed []string

	for _, inbox := range inboxes {
		if inbox.AgentID == args.From {
			continue
		}

		env := &types.MessageEnvelope{
			ID:          fmt.Sprintf("broadcast-%s-%s", args.From, inbox.AgentID),
			From:        args.From,
			To:          inbox.AgentID,
			Trust:       trust,
			ContentType: args.ContentType,
			Content:     args.Content,
		}

		if err := h.hub.Send(ctx, env); err != nil {
			failed = append(failed, inbox.AgentID)
			h.logger.Warn("broadcast delivery failed",
				"from", args.From,
				"to", inbox.AgentID,
				"error", err,
			)
			continue
		}
		delivered++
	}

	h.bus.Publish(nervous.NewEvent(
		types.EventCommBroadcast,
		"commhub_ext",
		args.From,
		map[string]interface{}{
			"from":      args.From,
			"delivered": delivered,
			"failed":    len(failed),
			"trust":     trust.String(),
		},
	))

	result := map[string]interface{}{
		"status":    "broadcast_complete",
		"delivered": delivered,
		"failed":    len(failed),
		"message":   fmt.Sprintf("Broadcast from %s delivered to %d agent(s).", args.From, delivered),
	}
	if len(failed) > 0 {
		result["failed_agents"] = strings.Join(failed, ", ")
	}

	return types.NewToolResult(result), nil
}

func (h *CommHandler) getHierarchy(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	if h.agentRepo != nil {
		agents, err := h.agentRepo.List(ctx)
		if err != nil {
			return types.NewErrorResult(fmt.Sprintf("list agents for hierarchy: %v", err)), nil
		}
		if len(agents) > 0 {
			return types.NewToolResult(buildHierarchyTree(agents)), nil
		}
	}

	rels, err := h.hierarchy.GetFullHierarchy(ctx)
	if err != nil {
		return types.NewErrorResult(fmt.Sprintf("get hierarchy: %v", err)), nil
	}
	if len(rels) == 0 {
		return types.NewToolResult([]any{}), nil
	}
	return types.NewToolResult(rels), nil
}

// hierarchyNode is a JSON-serializable tree node for the org chart.
type hierarchyNode struct {
	AgentID  string          `json:"agent_id"`
	ParentID string          `json:"parent_id,omitempty"`
	Children []hierarchyNode `json:"children"`
}

// buildHierarchyTree constructs a forest of hierarchyNode trees from flat agent records.
func buildHierarchyTree(agents []*repo.Agent) []hierarchyNode {
	agentIDs := make(map[string]bool, len(agents))
	parentMap := make(map[string]string, len(agents))
	childrenOf := make(map[string][]string, len(agents))

	for _, a := range agents {
		agentIDs[a.ID] = true
		parentMap[a.ID] = a.ParentAgentID
	}

	var rootIDs []string
	for _, a := range agents {
		if a.ParentAgentID != "" && agentIDs[a.ParentAgentID] {
			childrenOf[a.ParentAgentID] = append(childrenOf[a.ParentAgentID], a.ID)
		} else {
			rootIDs = append(rootIDs, a.ID)
		}
	}

	var build func(id string) hierarchyNode
	build = func(id string) hierarchyNode {
		node := hierarchyNode{
			AgentID:  id,
			ParentID: parentMap[id],
			Children: []hierarchyNode{},
		}
		for _, childID := range childrenOf[id] {
			node.Children = append(node.Children, build(childID))
		}
		return node
	}

	roots := make([]hierarchyNode, 0, len(rootIDs))
	for _, id := range rootIDs {
		roots = append(roots, build(id))
	}
	return roots
}

// ─── AgentMail methods ─────────────────────────────────────────────────────

func (h *CommHandler) sendMail(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		To          string          `json:"to"`
		From        string          `json:"from"`
		WorkspaceID string          `json:"workspace_id"`
		Priority    string          `json:"priority"`
		Payload     json.RawMessage `json:"payload"`
		SchemaID    string          `json:"schema_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return types.NewErrorResult("invalid parameters: " + err.Error()), nil
	}

	if args.To == "" {
		return types.NewErrorResult("to is required"), nil
	}
	if len(args.Payload) == 0 {
		return types.NewErrorResult("payload is required"), nil
	}

	priority := types.MailPriority(args.Priority)
	if priority == "" {
		priority = types.MailPriorityStandard
	}

	mail := &types.AgentMail{
		From:        args.From,
		To:          args.To,
		WorkspaceID: args.WorkspaceID,
		Priority:    priority,
		Payload:     args.Payload,
		SchemaID:    args.SchemaID,
		SentAt:      time.Now(),
	}

	if err := h.postbox.SendOutbound(ctx, mail); err != nil {
		return types.NewErrorResult("failed to send mail: " + err.Error()), nil
	}

	if h.ackTracker != nil {
		h.ackTracker.Track(mail)
	}

	h.logger.Info("mail sent via MCP",
		"mail_id", mail.ID,
		"to", mail.To,
		"priority", mail.Priority,
	)

	return types.NewToolResult(map[string]any{
		"mail_id":      mail.ID,
		"to":           mail.To,
		"priority":     mail.Priority,
		"ack_deadline": types.AckDeadlineFor(priority).String(),
		"status":       "enqueued",
	}), nil
}

func (h *CommHandler) listMailInbox(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Limit int `json:"limit"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		h.logger.Error("failed to parse agentmail handler params", "error", err)
		return types.NewErrorResult("invalid parameters: " + err.Error()), nil
	}
	if args.Limit <= 0 {
		args.Limit = 20
	}

	messages, err := h.postbox.PeekInbound(ctx, args.Limit)
	if err != nil {
		return types.NewErrorResult("failed to list inbox: " + err.Error()), nil
	}

	items := make([]map[string]any, 0, len(messages))
	for _, m := range messages {
		items = append(items, map[string]any{
			"id":           m.ID,
			"from":         m.From,
			"to":           m.To,
			"workspace_id": m.WorkspaceID,
			"priority":     m.Priority,
			"encrypted":    m.Encrypted,
			"sent_at":      m.SentAt,
		})
	}

	return types.NewToolResult(map[string]any{
		"messages": items,
		"count":    len(items),
	}), nil
}

func (h *CommHandler) getMail(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		MailID string `json:"mail_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return types.NewErrorResult("invalid parameters: " + err.Error()), nil
	}
	if args.MailID == "" {
		return types.NewErrorResult("mail_id is required"), nil
	}

	mail, err := h.postbox.GetByID(ctx, args.MailID)
	if err != nil {
		return types.NewErrorResult("mail not found: " + err.Error()), nil
	}

	return types.NewToolResult(mail), nil
}

func (h *CommHandler) ackMail(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		MailID     string `json:"mail_id"`
		InstanceID string `json:"instance_id"`
		Status     string `json:"status"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return types.NewErrorResult("invalid parameters: " + err.Error()), nil
	}
	if args.MailID == "" || args.InstanceID == "" || args.Status == "" {
		return types.NewErrorResult("mail_id, instance_id, and status are required"), nil
	}

	ack := &types.MailAck{
		MailID:     args.MailID,
		InstanceID: args.InstanceID,
		AckedAt:    time.Now(),
		Status:     args.Status,
	}

	if err := h.postbox.Acknowledge(ctx, ack); err != nil {
		return types.NewErrorResult("failed to acknowledge: " + err.Error()), nil
	}

	if h.ackTracker != nil {
		_ = h.ackTracker.Acknowledge(ack)
	}

	h.logger.Info("mail acknowledged via MCP",
		"mail_id", args.MailID,
		"instance_id", args.InstanceID,
		"status", args.Status,
	)

	return types.NewToolResult(map[string]any{
		"mail_id":     args.MailID,
		"instance_id": args.InstanceID,
		"status":      args.Status,
		"acked_at":    ack.AckedAt,
	}), nil
}

func (h *CommHandler) listAdapters(_ context.Context, _ json.RawMessage) (*types.ToolResult, error) {
	if h.amRegistry == nil {
		return types.NewToolResult(map[string]any{
			"adapters": []any{},
			"count":    0,
		}), nil
	}

	health := h.amRegistry.Healthy()
	adapters := make([]map[string]any, 0, len(health))
	for name, healthy := range health {
		adapters = append(adapters, map[string]any{
			"name":    name,
			"healthy": healthy,
		})
	}

	return types.NewToolResult(map[string]any{
		"adapters": adapters,
		"count":    len(adapters),
	}), nil
}

func (h *CommHandler) configureAdapter(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		AdapterName   string `json:"adapter_name"`
		AdapterAction string `json:"adapter_action"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return types.NewErrorResult("invalid parameters: " + err.Error()), nil
	}
	if args.AdapterName == "" || args.AdapterAction == "" {
		return types.NewErrorResult("adapter_name and adapter_action are required"), nil
	}

	if h.amRegistry == nil {
		return types.NewErrorResult("adapter registry not available"), nil
	}

	adapter := h.amRegistry.Get(args.AdapterName)
	if adapter == nil {
		return types.NewErrorResult("adapter not found: " + args.AdapterName), nil
	}

	switch args.AdapterAction {
	case "start":
		if err := adapter.Start(ctx); err != nil {
			return types.NewErrorResult("failed to start adapter: " + err.Error()), nil
		}
		return types.NewToolResult(map[string]any{
			"adapter": args.AdapterName,
			"action":  "start",
			"status":  "started",
		}), nil

	case "stop":
		if err := adapter.Stop(); err != nil {
			return types.NewErrorResult("failed to stop adapter: " + err.Error()), nil
		}
		return types.NewToolResult(map[string]any{
			"adapter": args.AdapterName,
			"action":  "stop",
			"status":  "stopped",
		}), nil

	case "health":
		return types.NewToolResult(map[string]any{
			"adapter": args.AdapterName,
			"healthy": adapter.Healthy(),
		}), nil

	default:
		return types.NewErrorResult("unknown adapter_action: " + args.AdapterAction + " (valid: start, stop, health)"), nil
	}
}

func (h *CommHandler) getPostboxStatus(ctx context.Context, _ json.RawMessage) (*types.ToolResult, error) {
	outbound, err := h.postbox.OutboundCount(ctx)
	if err != nil {
		outbound = -1
	}
	inbound, err := h.postbox.InboundCount(ctx)
	if err != nil {
		inbound = -1
	}

	result := map[string]any{
		"outbound_queue": outbound,
		"inbound_queue":  inbound,
	}

	if h.ackTracker != nil {
		result["ack_tracker"] = json.RawMessage(h.ackTracker.Stats())
	}

	if h.dlo != nil {
		result["dead_letter_count"] = h.dlo.Count()
	}

	if h.amRegistry != nil {
		result["adapters"] = h.amRegistry.Healthy()
	}

	return types.NewToolResult(result), nil
}

func (h *CommHandler) listDeadLetters(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		Limit int `json:"limit"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		h.logger.Error("failed to parse agentmail handler params", "error", err)
		return types.NewErrorResult("invalid parameters: " + err.Error()), nil
	}
	if args.Limit <= 0 {
		args.Limit = 20
	}

	entries, err := h.postbox.ListDLO(ctx, args.Limit)
	if err != nil {
		return types.NewErrorResult("failed to list dead letters: " + err.Error()), nil
	}

	items := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		item := map[string]any{
			"id":             e.ID,
			"mail_id":        e.MailID,
			"reason":         e.Reason,
			"attempts":       e.Attempts,
			"quarantined_at": e.QuarantinedAt,
		}
		if e.OriginalMail != nil {
			item["from"] = e.OriginalMail.From
			item["to"] = e.OriginalMail.To
			item["priority"] = e.OriginalMail.Priority
		}
		items = append(items, item)
	}

	return types.NewToolResult(map[string]any{
		"entries": items,
		"count":   len(items),
	}), nil
}

func (h *CommHandler) retryDeadLetter(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		EntryID string `json:"entry_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return types.NewErrorResult("invalid parameters: " + err.Error()), nil
	}
	if args.EntryID == "" {
		return types.NewErrorResult("entry_id is required"), nil
	}

	if h.dlo != nil {
		if err := h.dlo.Retry(ctx, args.EntryID); err != nil {
			return types.NewErrorResult("retry failed: " + err.Error()), nil
		}
		return types.NewToolResult(map[string]any{
			"entry_id": args.EntryID,
			"status":   "retried",
		}), nil
	}

	if err := h.postbox.RemoveFromDLO(ctx, args.EntryID); err != nil {
		return types.NewErrorResult("failed to remove DLO entry: " + err.Error()), nil
	}

	return types.NewToolResult(map[string]any{
		"entry_id": args.EntryID,
		"status":   "removed_from_dlo",
		"note":     "Entry removed. Use send_mail to re-send the message.",
	}), nil
}

func (h *CommHandler) discardDeadLetter(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		EntryID string `json:"entry_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return types.NewErrorResult("invalid parameters: " + err.Error()), nil
	}
	if args.EntryID == "" {
		return types.NewErrorResult("entry_id is required"), nil
	}

	if err := h.postbox.RemoveFromDLO(ctx, args.EntryID); err != nil {
		return types.NewErrorResult("failed to discard DLO entry: " + err.Error()), nil
	}

	return types.NewToolResult(map[string]any{
		"entry_id": args.EntryID,
		"status":   "discarded",
	}), nil
}

func (h *CommHandler) getPartitionStatus(_ context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		h.logger.Error("failed to parse agentmail handler params", "error", err)
		return types.NewErrorResult("invalid parameters: " + err.Error()), nil
	}

	if h.ackTracker == nil {
		return types.NewToolResult(map[string]any{
			"partitions": map[string]any{},
			"stats":      map[string]any{},
		}), nil
	}

	result := map[string]any{
		"stats": json.RawMessage(h.ackTracker.Stats()),
	}

	if args.WorkspaceID != "" {
		lock := h.ackTracker.GetPartitionLock(args.WorkspaceID)
		if lock != nil {
			result["partitions"] = map[string]any{
				args.WorkspaceID: lock,
			}
		} else {
			result["partitions"] = map[string]any{}
		}
	} else {
		result["partitions"] = h.ackTracker.GetPartitionStatus()
	}

	return types.NewToolResult(result), nil
}

// ─── Session methods ───────────────────────────────────────────────────────

func (h *CommHandler) newChatSession(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		AgentName string `json:"agent_name"`
		PeerID    string `json:"peer_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.CommHandler.newChatSession: %w", err)
	}
	if args.AgentName == "" {
		return types.NewErrorResult("agent_name is required"), nil
	}
	if args.PeerID == "" {
		return types.NewErrorResult("peer_id is required"), nil
	}

	existing, err := h.sessions.GetActiveSession(ctx, args.AgentName, args.PeerID)
	if err != nil {
		return nil, fmt.Errorf("handlers.CommHandler.newChatSession: check active session: %w", err)
	}
	if existing != nil {
		if endErr := h.sessions.EndSession(ctx, existing.ID); endErr != nil {
			return nil, fmt.Errorf("handlers.CommHandler.newChatSession: end existing session: %w", endErr)
		}
	}

	sessionID, err := h.sessions.CreateSession(ctx, args.AgentName, args.PeerID)
	if err != nil {
		return nil, fmt.Errorf("handlers.CommHandler.newChatSession: create session: %w", err)
	}

	return types.NewToolResult(map[string]string{
		"session_id": sessionID,
		"status":     "created",
	}), nil
}

func (h *CommHandler) getActiveSession(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		AgentName string `json:"agent_name"`
		PeerID    string `json:"peer_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.CommHandler.getActiveSession: %w", err)
	}
	if args.AgentName == "" {
		return types.NewErrorResult("agent_name is required"), nil
	}
	if args.PeerID == "" {
		return types.NewErrorResult("peer_id is required"), nil
	}

	session, err := h.sessions.GetActiveSession(ctx, args.AgentName, args.PeerID)
	if err != nil {
		return nil, fmt.Errorf("handlers.CommHandler.getActiveSession: %w", err)
	}
	if session == nil {
		return types.NewToolResult(map[string]string{"session_id": "", "status": "none"}), nil
	}

	return types.NewToolResult(session), nil
}

func (h *CommHandler) listChatSessions(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		AgentName string `json:"agent_name"`
		PeerID    string `json:"peer_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.CommHandler.listChatSessions: %w", err)
	}
	if args.AgentName == "" {
		return types.NewErrorResult("agent_name is required"), nil
	}
	if args.PeerID == "" {
		return types.NewErrorResult("peer_id is required"), nil
	}

	sessions, err := h.sessions.ListSessions(ctx, args.AgentName, args.PeerID)
	if err != nil {
		return nil, fmt.Errorf("handlers.CommHandler.listChatSessions: %w", err)
	}
	if sessions == nil {
		sessions = []*types.ChatSession{}
	}

	return types.NewToolResult(sessions), nil
}

func (h *CommHandler) archiveChatSession(ctx context.Context, params json.RawMessage) (*types.ToolResult, error) {
	var args struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("handlers.CommHandler.archiveChatSession: %w", err)
	}
	if args.SessionID == "" {
		return types.NewErrorResult("session_id is required"), nil
	}

	if err := h.sessions.ArchiveSession(ctx, args.SessionID); err != nil {
		return nil, fmt.Errorf("handlers.CommHandler.archiveChatSession: %w", err)
	}

	return types.NewToolResult(map[string]string{
		"session_id": args.SessionID,
		"status":     "archived",
	}), nil
}
