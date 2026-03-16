package auth

import (
	"context"
	"testing"

	"github.com/hyperax/hyperax/internal/mcp"
	"github.com/hyperax/hyperax/pkg/types"
)

func TestGetSession_Unauthenticated(t *testing.T) {
	ctx := context.Background()
	session := GetSession(ctx)

	if session.Authenticated {
		t.Error("expected unauthenticated session")
	}
	if session.ClearanceLevel != 0 {
		t.Errorf("expected clearance 0, got %d", session.ClearanceLevel)
	}
}

func TestGetSession_Authenticated(t *testing.T) {
	ac := types.AuthContext{
		PersonaID:      "p1",
		TokenID:        "tok1",
		ClearanceLevel: 2,
		Scopes:         []string{"admin"},
		Authenticated:  true,
	}
	ctx := context.WithValue(context.Background(), mcp.AuthContextKey(), ac)
	session := GetSession(ctx)

	if !session.Authenticated {
		t.Error("expected authenticated session")
	}
	if session.PersonaID != "p1" {
		t.Errorf("expected persona p1, got %s", session.PersonaID)
	}
	if session.ClearanceLevel != 2 {
		t.Errorf("expected clearance 2, got %d", session.ClearanceLevel)
	}
}

func TestCheckABACPermissions_ClearanceCheck(t *testing.T) {
	session := Session{ClearanceLevel: 1, Authenticated: true}

	if !CheckABACPermissions(session, 0, PermRead, "") {
		t.Error("clearance 1 should pass clearance 0 check")
	}
	if !CheckABACPermissions(session, 1, PermWrite, "") {
		t.Error("clearance 1 should pass clearance 1 check")
	}
	if CheckABACPermissions(session, 2, PermAdmin, "") {
		t.Error("clearance 1 should fail clearance 2 check")
	}
}

func TestCheckABACPermissions_ScopeCheck(t *testing.T) {
	session := Session{
		ClearanceLevel: 2,
		Scopes:         []string{"read", "write"},
		Authenticated:  true,
	}

	if !CheckABACPermissions(session, 0, PermRead, "read") {
		t.Error("should pass with matching scope")
	}
	if CheckABACPermissions(session, 0, PermRead, "admin") {
		t.Error("should fail with non-matching scope")
	}
}

func TestCheckABACPermissions_WildcardScope(t *testing.T) {
	session := Session{
		ClearanceLevel: 2,
		Scopes:         []string{"*"},
		Authenticated:  true,
	}

	if !CheckABACPermissions(session, 0, PermRead, "anything") {
		t.Error("wildcard scope should match any required scope")
	}
}

func TestCheckABACPermissions_NoScopeRestrictions(t *testing.T) {
	session := Session{
		ClearanceLevel: 1,
		Scopes:         nil, // No scope restrictions.
		Authenticated:  true,
	}

	if !CheckABACPermissions(session, 0, PermRead, "read") {
		t.Error("no scope restrictions should allow any scope")
	}
}

func TestSession_HasClearance(t *testing.T) {
	s := Session{ClearanceLevel: 1}
	if !s.HasClearance(0) {
		t.Error("1 >= 0")
	}
	if !s.HasClearance(1) {
		t.Error("1 >= 1")
	}
	if s.HasClearance(2) {
		t.Error("1 < 2")
	}
}

func TestSession_HasScope(t *testing.T) {
	s := Session{Scopes: []string{"read", "write"}}
	if !s.HasScope("read") {
		t.Error("should have read scope")
	}
	if s.HasScope("admin") {
		t.Error("should not have admin scope")
	}

	// Empty scopes = no restrictions.
	s2 := Session{Scopes: nil}
	if !s2.HasScope("anything") {
		t.Error("empty scopes should allow anything")
	}
}

func TestSession_ToAuthContext(t *testing.T) {
	s := Session{
		PersonaID:      "p1",
		TokenID:        "tok1",
		ClearanceLevel: 2,
		Scopes:         []string{"admin"},
		Authenticated:  true,
	}
	ac := s.ToAuthContext()
	if ac.PersonaID != "p1" {
		t.Errorf("expected p1, got %s", ac.PersonaID)
	}
	if ac.ClearanceLevel != 2 {
		t.Errorf("expected 2, got %d", ac.ClearanceLevel)
	}
	if !ac.Authenticated {
		t.Error("expected authenticated")
	}
}
