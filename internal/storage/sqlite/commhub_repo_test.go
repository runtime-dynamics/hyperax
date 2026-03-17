package sqlite

import (
	"context"
	"testing"

	"github.com/hyperax/hyperax/pkg/types"
)

func newCommHubRepo(t *testing.T) (*CommHubRepo, context.Context) {
	t.Helper()
	db, ctx := setupTestDB(t)

	// setupTestDB runs db.Migrate which applies the consolidated 001_initial
	// migration containing all tables including commhub.
	return &CommHubRepo{db: db.db}, ctx
}

// --- Hierarchy Tests ---

func TestCommHubRepo_SetAndGetRelationship(t *testing.T) {
	r, ctx := newCommHubRepo(t)

	rel := &types.AgentRelationship{
		ParentAgent:  "boss",
		ChildAgent:   "worker",
		Relationship: "supervisor",
	}

	if err := r.SetRelationship(ctx, rel); err != nil {
		t.Fatalf("set: %v", err)
	}
	if rel.ID == "" {
		t.Fatal("expected non-empty ID after set")
	}

	got, err := r.GetRelationship(ctx, "boss", "worker")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ParentAgent != "boss" {
		t.Errorf("parent_agent = %q, want %q", got.ParentAgent, "boss")
	}
	if got.ChildAgent != "worker" {
		t.Errorf("child_agent = %q, want %q", got.ChildAgent, "worker")
	}
	if got.Relationship != "supervisor" {
		t.Errorf("relationship = %q, want %q", got.Relationship, "supervisor")
	}
}

func TestCommHubRepo_GetRelationship_NotFound(t *testing.T) {
	r, ctx := newCommHubRepo(t)

	_, err := r.GetRelationship(ctx, "nobody", "nothing")
	if err == nil {
		t.Error("expected error for nonexistent relationship")
	}
}

func TestCommHubRepo_GetChildren(t *testing.T) {
	r, ctx := newCommHubRepo(t)

	_ = r.SetRelationship(ctx, &types.AgentRelationship{
		ParentAgent: "boss", ChildAgent: "worker-a", Relationship: "supervisor",
	})
	_ = r.SetRelationship(ctx, &types.AgentRelationship{
		ParentAgent: "boss", ChildAgent: "worker-b", Relationship: "supervisor",
	})
	_ = r.SetRelationship(ctx, &types.AgentRelationship{
		ParentAgent: "other-boss", ChildAgent: "worker-c", Relationship: "supervisor",
	})

	children, err := r.GetChildren(ctx, "boss")
	if err != nil {
		t.Fatalf("get children: %v", err)
	}
	if len(children) != 2 {
		t.Errorf("expected 2 children, got %d", len(children))
	}
}

func TestCommHubRepo_GetParent(t *testing.T) {
	r, ctx := newCommHubRepo(t)

	_ = r.SetRelationship(ctx, &types.AgentRelationship{
		ParentAgent: "lead", ChildAgent: "dev", Relationship: "supervisor",
	})

	parent, err := r.GetParent(ctx, "dev")
	if err != nil {
		t.Fatalf("get parent: %v", err)
	}
	if parent.ParentAgent != "lead" {
		t.Errorf("parent_agent = %q, want %q", parent.ParentAgent, "lead")
	}
}

func TestCommHubRepo_GetParent_NotFound(t *testing.T) {
	r, ctx := newCommHubRepo(t)

	_, err := r.GetParent(ctx, "orphan")
	if err == nil {
		t.Error("expected error for agent with no parent")
	}
}

func TestCommHubRepo_GetFullHierarchy(t *testing.T) {
	r, ctx := newCommHubRepo(t)

	_ = r.SetRelationship(ctx, &types.AgentRelationship{
		ParentAgent: "boss", ChildAgent: "w1", Relationship: "supervisor",
	})
	_ = r.SetRelationship(ctx, &types.AgentRelationship{
		ParentAgent: "boss", ChildAgent: "w2", Relationship: "peer",
	})

	rels, err := r.GetFullHierarchy(ctx)
	if err != nil {
		t.Fatalf("get full hierarchy: %v", err)
	}
	if len(rels) != 2 {
		t.Errorf("expected 2 relationships, got %d", len(rels))
	}
}

func TestCommHubRepo_DeleteRelationship(t *testing.T) {
	r, ctx := newCommHubRepo(t)

	rel := &types.AgentRelationship{
		ParentAgent: "boss", ChildAgent: "worker", Relationship: "supervisor",
	}
	_ = r.SetRelationship(ctx, rel)

	if err := r.DeleteRelationship(ctx, rel.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err := r.GetRelationship(ctx, "boss", "worker")
	if err == nil {
		t.Error("expected relationship to be deleted")
	}
}

func TestCommHubRepo_DeleteRelationship_NotFound(t *testing.T) {
	r, ctx := newCommHubRepo(t)

	err := r.DeleteRelationship(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent relationship")
	}
}

// --- Comm Log Tests ---

func TestCommHubRepo_LogAndGetCommLog(t *testing.T) {
	r, ctx := newCommHubRepo(t)

	entry := &types.CommLogEntry{
		FromAgent: "agent-a", ToAgent: "agent-b",
		ContentType: "text", Content: "hello", Trust: "internal", Direction: "sent",
	}

	if err := r.LogMessage(ctx, entry); err != nil {
		t.Fatalf("log: %v", err)
	}
	if entry.ID == "" {
		t.Fatal("expected non-empty ID")
	}

	entries, err := r.GetCommLog(ctx, "agent-a", 10)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Content != "hello" {
		t.Errorf("content = %q, want %q", entries[0].Content, "hello")
	}
}

func TestCommHubRepo_GetCommLog_AsRecipient(t *testing.T) {
	r, ctx := newCommHubRepo(t)

	_ = r.LogMessage(ctx, &types.CommLogEntry{
		FromAgent: "agent-a", ToAgent: "agent-b",
		ContentType: "text", Content: "msg", Trust: "internal", Direction: "sent",
	})

	// Query as the recipient.
	entries, err := r.GetCommLog(ctx, "agent-b", 10)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry as recipient, got %d", len(entries))
	}
}

func TestCommHubRepo_GetCommLogBetween(t *testing.T) {
	r, ctx := newCommHubRepo(t)

	_ = r.LogMessage(ctx, &types.CommLogEntry{
		FromAgent: "a", ToAgent: "b", ContentType: "text", Content: "1",
		Trust: "internal", Direction: "sent",
	})
	_ = r.LogMessage(ctx, &types.CommLogEntry{
		FromAgent: "b", ToAgent: "a", ContentType: "text", Content: "2",
		Trust: "internal", Direction: "sent",
	})
	_ = r.LogMessage(ctx, &types.CommLogEntry{
		FromAgent: "a", ToAgent: "c", ContentType: "text", Content: "3",
		Trust: "internal", Direction: "sent",
	})

	entries, err := r.GetCommLogBetween(ctx, "a", "b", 10)
	if err != nil {
		t.Fatalf("get between: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries between a and b, got %d", len(entries))
	}
}

func TestCommHubRepo_GetCommLog_Limit(t *testing.T) {
	r, ctx := newCommHubRepo(t)

	for i := 0; i < 5; i++ {
		_ = r.LogMessage(ctx, &types.CommLogEntry{
			FromAgent: "agent", ToAgent: "other",
			ContentType: "text", Content: "msg", Trust: "internal", Direction: "sent",
		})
	}

	entries, err := r.GetCommLog(ctx, "agent", 2)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries with limit, got %d", len(entries))
	}
}

// --- Permission Tests ---

func TestCommHubRepo_GrantAndCheckPermission(t *testing.T) {
	r, ctx := newCommHubRepo(t)

	perm := &types.CommPermission{
		AgentID:    "agent-a",
		TargetID:   "agent-b",
		Permission: "both",
	}
	if err := r.GrantPermission(ctx, perm); err != nil {
		t.Fatalf("grant: %v", err)
	}

	ok, err := r.CheckPermission(ctx, "agent-a", "agent-b")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !ok {
		t.Error("expected permission to be granted")
	}
}

func TestCommHubRepo_CheckPermission_NotGranted(t *testing.T) {
	r, ctx := newCommHubRepo(t)

	ok, err := r.CheckPermission(ctx, "nobody", "nothing")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if ok {
		t.Error("expected permission to not be granted")
	}
}

func TestCommHubRepo_CheckPermission_Wildcard(t *testing.T) {
	r, ctx := newCommHubRepo(t)

	_ = r.GrantPermission(ctx, &types.CommPermission{
		AgentID: "admin", TargetID: "*", Permission: "send",
	})

	ok, err := r.CheckPermission(ctx, "admin", "any-agent")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !ok {
		t.Error("expected wildcard permission to match")
	}
}

func TestCommHubRepo_CheckPermission_ReceiveOnly(t *testing.T) {
	r, ctx := newCommHubRepo(t)

	_ = r.GrantPermission(ctx, &types.CommPermission{
		AgentID: "listener", TargetID: "speaker", Permission: "receive",
	})

	// "receive" should not grant send access.
	ok, err := r.CheckPermission(ctx, "listener", "speaker")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if ok {
		t.Error("expected receive-only permission to not grant send access")
	}
}

func TestCommHubRepo_GrantPermission_Upsert(t *testing.T) {
	r, ctx := newCommHubRepo(t)

	_ = r.GrantPermission(ctx, &types.CommPermission{
		AgentID: "a", TargetID: "b", Permission: "send",
	})
	// Update to "both".
	_ = r.GrantPermission(ctx, &types.CommPermission{
		AgentID: "a", TargetID: "b", Permission: "both",
	})

	perms, err := r.ListPermissions(ctx, "a")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(perms) != 1 {
		t.Fatalf("expected 1 permission after upsert, got %d", len(perms))
	}
	if perms[0].Permission != "both" {
		t.Errorf("permission = %q, want %q", perms[0].Permission, "both")
	}
}

func TestCommHubRepo_RevokePermission(t *testing.T) {
	r, ctx := newCommHubRepo(t)

	_ = r.GrantPermission(ctx, &types.CommPermission{
		AgentID: "a", TargetID: "b", Permission: "both",
	})

	if err := r.RevokePermission(ctx, "a", "b"); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	ok, err := r.CheckPermission(ctx, "a", "b")
	if err != nil {
		t.Fatalf("check permission: %v", err)
	}
	if ok {
		t.Error("expected permission to be revoked")
	}
}

func TestCommHubRepo_RevokePermission_NotFound(t *testing.T) {
	r, ctx := newCommHubRepo(t)

	err := r.RevokePermission(ctx, "nobody", "nothing")
	if err == nil {
		t.Error("expected error for revoking nonexistent permission")
	}
}

func TestCommHubRepo_ListPermissions(t *testing.T) {
	r, ctx := newCommHubRepo(t)

	_ = r.GrantPermission(ctx, &types.CommPermission{
		AgentID: "a", TargetID: "b", Permission: "send",
	})
	_ = r.GrantPermission(ctx, &types.CommPermission{
		AgentID: "a", TargetID: "c", Permission: "both",
	})
	_ = r.GrantPermission(ctx, &types.CommPermission{
		AgentID: "x", TargetID: "y", Permission: "both",
	})

	perms, err := r.ListPermissions(ctx, "a")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(perms) != 2 {
		t.Errorf("expected 2 permissions for agent a, got %d", len(perms))
	}
}
