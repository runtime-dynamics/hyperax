package delegation

import (
	"context"
	"testing"

	"github.com/hyperax/hyperax/pkg/types"
)

func TestResolveSessionGrants_NilRepo(t *testing.T) {
	grants, err := ResolveSessionGrants(context.Background(), nil, "agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if grants.ElevatedLevel != -1 {
		t.Fatalf("expected -1, got %d", grants.ElevatedLevel)
	}
}

func TestResolveSessionGrants_ClearanceElevation(t *testing.T) {
	ctx := context.Background()
	r := newMockDelegationRepo()

	r.delegations["dlg1"] = &types.Delegation{
		ID:            "dlg1",
		GranterID:     "admin",
		GranteeID:     "agent",
		GrantType:     types.GrantClearanceElevation,
		ElevatedLevel: 2,
	}

	grants, err := ResolveSessionGrants(ctx, r, "agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if grants.ElevatedLevel != 2 {
		t.Fatalf("expected elevated level 2, got %d", grants.ElevatedLevel)
	}
	if len(grants.DelegatedBy) != 1 || grants.DelegatedBy[0] != "admin" {
		t.Fatalf("expected delegated_by=[admin], got %v", grants.DelegatedBy)
	}
}

func TestResolveSessionGrants_ScopeAccess(t *testing.T) {
	ctx := context.Background()
	r := newMockDelegationRepo()

	r.delegations["dlg1"] = &types.Delegation{
		ID:        "dlg1",
		GranterID: "admin",
		GranteeID: "agent",
		GrantType: types.GrantScopeAccess,
		Scopes:    []string{"tools:write", "events:manage"},
	}

	grants, err := ResolveSessionGrants(ctx, r, "agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(grants.AdditionalScopes) != 2 {
		t.Fatalf("expected 2 additional scopes, got %d", len(grants.AdditionalScopes))
	}
}

func TestResolveSessionGrants_CredentialPassthrough(t *testing.T) {
	ctx := context.Background()
	r := newMockDelegationRepo()

	r.delegations["dlg1"] = &types.Delegation{
		ID:            "dlg1",
		GranterID:     "admin",
		GranteeID:     "agent",
		GrantType:     types.GrantCredentialPassthrough,
		CredentialKey: "delegation:dlg1",
	}

	grants, err := ResolveSessionGrants(ctx, r, "agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !grants.HasCredentials {
		t.Fatal("expected HasCredentials=true")
	}
}

func TestResolveSessionGrants_MultipleGrants(t *testing.T) {
	ctx := context.Background()
	r := newMockDelegationRepo()

	r.delegations["dlg1"] = &types.Delegation{
		ID: "dlg1", GranterID: "admin", GranteeID: "agent",
		GrantType: types.GrantClearanceElevation, ElevatedLevel: 1,
	}
	r.delegations["dlg2"] = &types.Delegation{
		ID: "dlg2", GranterID: "admin", GranteeID: "agent",
		GrantType: types.GrantClearanceElevation, ElevatedLevel: 2,
	}
	r.delegations["dlg3"] = &types.Delegation{
		ID: "dlg3", GranterID: "sysadmin", GranteeID: "agent",
		GrantType: types.GrantScopeAccess, Scopes: []string{"admin:read"},
	}

	grants, err := ResolveSessionGrants(ctx, r, "agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should pick the highest elevated level.
	if grants.ElevatedLevel != 2 {
		t.Fatalf("expected 2, got %d", grants.ElevatedLevel)
	}
	// Should have 2 unique granters.
	if len(grants.DelegatedBy) != 2 {
		t.Fatalf("expected 2 granters, got %d", len(grants.DelegatedBy))
	}
	if len(grants.AdditionalScopes) != 1 {
		t.Fatalf("expected 1 scope, got %d", len(grants.AdditionalScopes))
	}
}

func TestResolveSessionGrants_SkipsRevoked(t *testing.T) {
	ctx := context.Background()
	r := newMockDelegationRepo()

	r.delegations["dlg1"] = &types.Delegation{
		ID: "dlg1", GranterID: "admin", GranteeID: "agent",
		GrantType: types.GrantClearanceElevation, ElevatedLevel: 2,
		RevokedAt: "2026-01-01T00:00:00Z",
	}

	// ListByGrantee in our mock filters by RevokedAt==""
	grants, err := ResolveSessionGrants(ctx, r, "agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if grants.ElevatedLevel != -1 {
		t.Fatalf("expected -1 (no active grants), got %d", grants.ElevatedLevel)
	}
}

func TestResolveSessionGrants_NoGrants(t *testing.T) {
	ctx := context.Background()
	r := newMockDelegationRepo()

	grants, err := ResolveSessionGrants(ctx, r, "agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if grants.ElevatedLevel != -1 {
		t.Fatalf("expected -1, got %d", grants.ElevatedLevel)
	}
	if len(grants.DelegatedBy) != 0 {
		t.Fatalf("expected empty DelegatedBy, got %v", grants.DelegatedBy)
	}
}

func TestSessionGrants_ApplyToClearance(t *testing.T) {
	tests := []struct {
		original int
		elevated int
		want     int
	}{
		{0, -1, 0},  // No elevation
		{0, 2, 2},   // Elevated above original
		{2, 1, 2},   // Elevation below original -> keep original
		{1, 1, 1},   // Same level
	}

	for _, tt := range tests {
		sg := &SessionGrants{ElevatedLevel: tt.elevated}
		got := sg.ApplyToClearance(tt.original)
		if got != tt.want {
			t.Errorf("ApplyToClearance(%d) with elevation=%d: got %d, want %d",
				tt.original, tt.elevated, got, tt.want)
		}
	}
}

func TestSessionGrants_MergeScopes(t *testing.T) {
	sg := &SessionGrants{
		AdditionalScopes: []string{"tools:write", "events:manage", "tools:read"},
	}

	original := []string{"tools:read", "config:read"}
	merged := sg.MergeScopes(original)

	if len(merged) != 4 {
		t.Fatalf("expected 4 merged scopes (deduped), got %d: %v", len(merged), merged)
	}

	// Verify no duplicates.
	seen := make(map[string]bool)
	for _, s := range merged {
		if seen[s] {
			t.Fatalf("duplicate scope: %s", s)
		}
		seen[s] = true
	}
}

func TestSessionGrants_MergeScopes_NoAdditional(t *testing.T) {
	sg := &SessionGrants{}
	original := []string{"tools:read"}
	merged := sg.MergeScopes(original)

	if len(merged) != 1 {
		t.Fatalf("expected 1 scope, got %d", len(merged))
	}
}
