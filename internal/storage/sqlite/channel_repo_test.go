package sqlite

import (
	"testing"
	"time"

	"github.com/hyperax/hyperax/internal/repo"
)

// ---------------------------------------------------------------------------
// Session tests
// ---------------------------------------------------------------------------

func TestChannelRepo_CreateAndGetSession(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ChannelRepo{db: db.db}

	session := &repo.ChannelSession{
		Name:          "test-session",
		WorkspaceID:   "ws-abc",
		Status:        "connected",
		BridgeVersion: "0.1.0",
		Metadata:      `{"os":"darwin"}`,
	}

	if err := r.CreateSession(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if session.ID == "" {
		t.Fatal("expected ID to be generated")
	}

	got, err := r.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}

	if got.Name != "test-session" {
		t.Errorf("name = %q, want %q", got.Name, "test-session")
	}
	if got.WorkspaceID != "ws-abc" {
		t.Errorf("workspace_id = %q, want %q", got.WorkspaceID, "ws-abc")
	}
	if got.Status != "connected" {
		t.Errorf("status = %q, want %q", got.Status, "connected")
	}
	if got.BridgeVersion != "0.1.0" {
		t.Errorf("bridge_version = %q, want %q", got.BridgeVersion, "0.1.0")
	}
	if got.ConnectedAt.IsZero() {
		t.Error("connected_at should not be zero")
	}
	if got.DisconnectedAt != nil {
		t.Error("disconnected_at should be nil for a connected session")
	}
}

func TestChannelRepo_GetSession_NotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ChannelRepo{db: db.db}

	_, err := r.GetSession(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestChannelRepo_ListSessions(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ChannelRepo{db: db.db}

	// Empty initially.
	sessions, err := r.ListSessions(ctx, "")
	if err != nil {
		t.Fatalf("list (empty): %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0, got %d", len(sessions))
	}

	// Create two sessions with different statuses.
	if err := r.CreateSession(ctx, &repo.ChannelSession{
		Name: "s1", WorkspaceID: "ws-1", Status: "connected", BridgeVersion: "0.1.0", Metadata: "{}",
	}); err != nil {
		t.Fatalf("create s1: %v", err)
	}
	if err := r.CreateSession(ctx, &repo.ChannelSession{
		Name: "s2", WorkspaceID: "ws-1", Status: "idle", BridgeVersion: "0.1.0", Metadata: "{}",
	}); err != nil {
		t.Fatalf("create s2: %v", err)
	}

	// List all.
	sessions, err = r.ListSessions(ctx, "")
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("expected 2, got %d", len(sessions))
	}

	// Filter by status.
	sessions, err = r.ListSessions(ctx, "idle")
	if err != nil {
		t.Fatalf("list idle: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 idle session, got %d", len(sessions))
	}
	if sessions[0].Name != "s2" {
		t.Errorf("name = %q, want %q", sessions[0].Name, "s2")
	}
}

func TestChannelRepo_UpdateHeartbeat(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ChannelRepo{db: db.db}

	session := &repo.ChannelSession{
		Name: "hb-test", WorkspaceID: "ws-1", Status: "connected",
		BridgeVersion: "0.1.0", Metadata: "{}",
	}
	if err := r.CreateSession(ctx, session); err != nil {
		t.Fatalf("create: %v", err)
	}

	before, err := r.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("get before: %v", err)
	}

	if err := r.UpdateHeartbeat(ctx, session.ID); err != nil {
		t.Fatalf("update heartbeat: %v", err)
	}

	after, err := r.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("get after: %v", err)
	}

	// Heartbeat should be >= the original (fast tests may see equal timestamps).
	if after.LastHeartbeat.Before(before.LastHeartbeat) {
		t.Errorf("heartbeat went backwards: %v < %v", after.LastHeartbeat, before.LastHeartbeat)
	}
}

func TestChannelRepo_UpdateHeartbeat_NotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ChannelRepo{db: db.db}

	err := r.UpdateHeartbeat(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestChannelRepo_UpdateSessionStatus(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ChannelRepo{db: db.db}

	session := &repo.ChannelSession{
		Name: "status-test", WorkspaceID: "ws-1", Status: "connected",
		BridgeVersion: "0.1.0", Metadata: "{}",
	}
	if err := r.CreateSession(ctx, session); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Transition to disconnected -- should set disconnected_at.
	if err := r.UpdateSessionStatus(ctx, session.ID, "disconnected"); err != nil {
		t.Fatalf("update status: %v", err)
	}

	got, err := r.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != "disconnected" {
		t.Errorf("status = %q, want %q", got.Status, "disconnected")
	}
	if got.DisconnectedAt == nil {
		t.Error("disconnected_at should be set after disconnecting")
	}
}

func TestChannelRepo_UpdateSessionStatus_NotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ChannelRepo{db: db.db}

	err := r.UpdateSessionStatus(ctx, "nonexistent", "idle")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestChannelRepo_DeleteSession(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ChannelRepo{db: db.db}

	session := &repo.ChannelSession{
		Name: "del-test", WorkspaceID: "ws-1", Status: "connected",
		BridgeVersion: "0.1.0", Metadata: "{}",
	}
	if err := r.CreateSession(ctx, session); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := r.DeleteSession(ctx, session.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err := r.GetSession(ctx, session.ID)
	if err == nil {
		t.Fatal("expected error after deletion")
	}
}

func TestChannelRepo_DeleteSession_NotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ChannelRepo{db: db.db}

	err := r.DeleteSession(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

// ---------------------------------------------------------------------------
// Message tests
// ---------------------------------------------------------------------------

func TestChannelRepo_CreateAndListMessages(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ChannelRepo{db: db.db}

	// Create a parent session first (FK constraint).
	session := &repo.ChannelSession{
		Name: "msg-test", WorkspaceID: "ws-1", Status: "connected",
		BridgeVersion: "0.1.0", Metadata: "{}",
	}
	if err := r.CreateSession(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Insert three messages.
	for i, dir := range []string{"inbound", "outbound", "system"} {
		msg := &repo.ChannelMessage{
			SessionID: session.ID,
			Direction: dir,
			Content:   "hello " + dir,
			Sender:    "test-user",
			Metadata:  "{}",
		}
		if err := r.CreateMessage(ctx, msg); err != nil {
			t.Fatalf("create message %d: %v", i, err)
		}
		if msg.ID == "" {
			t.Fatalf("message %d: expected ID to be generated", i)
		}
	}

	// List all messages.
	msgs, err := r.ListMessages(ctx, session.ID, 0)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("expected 3, got %d", len(msgs))
	}

	// List with limit.
	msgs, err = r.ListMessages(ctx, session.ID, 2)
	if err != nil {
		t.Fatalf("list messages (limit): %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2, got %d", len(msgs))
	}
}

func TestChannelRepo_ListMessages_Empty(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ChannelRepo{db: db.db}

	session := &repo.ChannelSession{
		Name: "empty-msgs", WorkspaceID: "ws-1", Status: "connected",
		BridgeVersion: "0.1.0", Metadata: "{}",
	}
	if err := r.CreateSession(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	msgs, err := r.ListMessages(ctx, session.ID, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0, got %d", len(msgs))
	}
}

// ---------------------------------------------------------------------------
// Event queue tests
// ---------------------------------------------------------------------------

func TestChannelRepo_EnqueueAndDequeueEvents(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ChannelRepo{db: db.db}

	session := &repo.ChannelSession{
		Name: "evt-test", WorkspaceID: "ws-1", Status: "connected",
		BridgeVersion: "0.1.0", Metadata: "{}",
	}
	if err := r.CreateSession(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Enqueue two events.
	for i := 0; i < 2; i++ {
		evt := &repo.ChannelEvent{
			SessionID: session.ID,
			EventType: "message",
			Content:   "event content",
			Meta:      "{}",
		}
		if err := r.EnqueueEvent(ctx, evt); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
		if evt.ID == "" {
			t.Fatalf("event %d: expected ID to be generated", i)
		}
	}

	// Dequeue all pending.
	events, err := r.DequeueEvents(ctx, session.ID, 10)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("expected 2, got %d", len(events))
	}
	for _, e := range events {
		if e.Status != "pending" {
			t.Errorf("status = %q, want pending", e.Status)
		}
	}
}

func TestChannelRepo_MarkEventDelivered(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ChannelRepo{db: db.db}

	session := &repo.ChannelSession{
		Name: "deliver-test", WorkspaceID: "ws-1", Status: "connected",
		BridgeVersion: "0.1.0", Metadata: "{}",
	}
	if err := r.CreateSession(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	evt := &repo.ChannelEvent{
		SessionID: session.ID,
		EventType: "message",
		Content:   "payload",
		Meta:      "{}",
	}
	if err := r.EnqueueEvent(ctx, evt); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	if err := r.MarkEventDelivered(ctx, evt.ID); err != nil {
		t.Fatalf("mark delivered: %v", err)
	}

	// After marking delivered, dequeue should return empty.
	events, err := r.DequeueEvents(ctx, session.ID, 10)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 pending after delivery, got %d", len(events))
	}
}

func TestChannelRepo_MarkEventDelivered_NotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ChannelRepo{db: db.db}

	err := r.MarkEventDelivered(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent event")
	}
}

func TestChannelRepo_ExpireOldEvents(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ChannelRepo{db: db.db}

	session := &repo.ChannelSession{
		Name: "expire-test", WorkspaceID: "ws-1", Status: "connected",
		BridgeVersion: "0.1.0", Metadata: "{}",
	}
	if err := r.CreateSession(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Enqueue an event.
	evt := &repo.ChannelEvent{
		SessionID: session.ID,
		EventType: "notification",
		Content:   "old event",
		Meta:      "{}",
	}
	if err := r.EnqueueEvent(ctx, evt); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Expire events older than 1 hour in the future (should catch everything).
	cutoff := time.Now().Add(1 * time.Hour)
	affected, err := r.ExpireOldEvents(ctx, cutoff)
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	if affected != 1 {
		t.Errorf("expected 1 expired, got %d", affected)
	}

	// Dequeue should now be empty.
	events, err := r.DequeueEvents(ctx, session.ID, 10)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 pending after expiry, got %d", len(events))
	}
}

func TestChannelRepo_ExpireOldEvents_NoneExpired(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ChannelRepo{db: db.db}

	session := &repo.ChannelSession{
		Name: "no-expire", WorkspaceID: "ws-1", Status: "connected",
		BridgeVersion: "0.1.0", Metadata: "{}",
	}
	if err := r.CreateSession(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	if err := r.EnqueueEvent(ctx, &repo.ChannelEvent{
		SessionID: session.ID, EventType: "message", Content: "recent", Meta: "{}",
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Expire events older than 1 hour in the past (should catch nothing).
	cutoff := time.Now().Add(-1 * time.Hour)
	affected, err := r.ExpireOldEvents(ctx, cutoff)
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	if affected != 0 {
		t.Errorf("expected 0 expired, got %d", affected)
	}
}

// ---------------------------------------------------------------------------
// Permission tests
// ---------------------------------------------------------------------------

func TestChannelRepo_CreateAndGetPendingPermissions(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ChannelRepo{db: db.db}

	session := &repo.ChannelSession{
		Name: "perm-test", WorkspaceID: "ws-1", Status: "connected",
		BridgeVersion: "0.1.0", Metadata: "{}",
	}
	if err := r.CreateSession(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	perm := &repo.ChannelPermission{
		SessionID:    session.ID,
		RequestID:    "abc12",
		ToolName:     "Bash",
		Description:  "Run ls command",
		InputPreview: `{"command":"ls"}`,
	}
	if err := r.CreatePermission(ctx, perm); err != nil {
		t.Fatalf("create permission: %v", err)
	}
	if perm.ID == "" {
		t.Fatal("expected ID to be generated")
	}

	perms, err := r.GetPendingPermissions(ctx, session.ID)
	if err != nil {
		t.Fatalf("get pending: %v", err)
	}
	if len(perms) != 1 {
		t.Fatalf("expected 1, got %d", len(perms))
	}

	p := perms[0]
	if p.RequestID != "abc12" {
		t.Errorf("request_id = %q, want %q", p.RequestID, "abc12")
	}
	if p.ToolName != "Bash" {
		t.Errorf("tool_name = %q, want %q", p.ToolName, "Bash")
	}
	if p.Status != "pending" {
		t.Errorf("status = %q, want %q", p.Status, "pending")
	}
	if p.ResolvedAt != nil {
		t.Error("resolved_at should be nil for pending permission")
	}
}

func TestChannelRepo_ResolvePermission(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ChannelRepo{db: db.db}

	session := &repo.ChannelSession{
		Name: "resolve-test", WorkspaceID: "ws-1", Status: "connected",
		BridgeVersion: "0.1.0", Metadata: "{}",
	}
	if err := r.CreateSession(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	perm := &repo.ChannelPermission{
		SessionID:    session.ID,
		RequestID:    "xyz99",
		ToolName:     "Edit",
		Description:  "Edit a file",
		InputPreview: `{"path":"main.go"}`,
	}
	if err := r.CreatePermission(ctx, perm); err != nil {
		t.Fatalf("create permission: %v", err)
	}

	if err := r.ResolvePermission(ctx, "xyz99", "allowed", "admin-user"); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// Pending permissions should now be empty.
	perms, err := r.GetPendingPermissions(ctx, session.ID)
	if err != nil {
		t.Fatalf("get pending: %v", err)
	}
	if len(perms) != 0 {
		t.Errorf("expected 0 pending after resolve, got %d", len(perms))
	}
}

func TestChannelRepo_ResolvePermission_NotFound(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ChannelRepo{db: db.db}

	err := r.ResolvePermission(ctx, "nonexistent", "denied", "someone")
	if err == nil {
		t.Fatal("expected error for nonexistent request_id")
	}
}

func TestChannelRepo_ResolvePermission_AlreadyResolved(t *testing.T) {
	db, ctx := setupTestDB(t)
	r := &ChannelRepo{db: db.db}

	session := &repo.ChannelSession{
		Name: "double-resolve", WorkspaceID: "ws-1", Status: "connected",
		BridgeVersion: "0.1.0", Metadata: "{}",
	}
	if err := r.CreateSession(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	perm := &repo.ChannelPermission{
		SessionID:    session.ID,
		RequestID:    "dup01",
		ToolName:     "Write",
		Description:  "Write file",
		InputPreview: "{}",
	}
	if err := r.CreatePermission(ctx, perm); err != nil {
		t.Fatalf("create: %v", err)
	}

	// First resolve should succeed.
	if err := r.ResolvePermission(ctx, "dup01", "denied", "admin"); err != nil {
		t.Fatalf("first resolve: %v", err)
	}

	// Second resolve on same request_id should fail (no longer pending).
	err := r.ResolvePermission(ctx, "dup01", "allowed", "admin")
	if err == nil {
		t.Fatal("expected error on double-resolve")
	}
}

// ---------------------------------------------------------------------------
// Store wiring test
// ---------------------------------------------------------------------------

func TestNewStore_ChannelsWired(t *testing.T) {
	db, _ := setupTestDB(t)
	store := db.NewStore()

	if store.Channels == nil {
		t.Error("Channels repo not wired in NewStore")
	}
}
