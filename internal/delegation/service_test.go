package delegation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"

	"github.com/hyperax/hyperax/internal/repo"
	"github.com/hyperax/hyperax/internal/secrets"
	"github.com/hyperax/hyperax/pkg/types"
)

// --- mock delegation repo ---

type mockDelegationRepo struct {
	delegations map[string]*types.Delegation
}

func newMockDelegationRepo() *mockDelegationRepo {
	return &mockDelegationRepo{delegations: make(map[string]*types.Delegation)}
}

func (m *mockDelegationRepo) Create(_ context.Context, d *types.Delegation) error {
	m.delegations[d.ID] = d
	return nil
}

func (m *mockDelegationRepo) GetByID(_ context.Context, id string) (*types.Delegation, error) {
	d, ok := m.delegations[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return d, nil
}

func (m *mockDelegationRepo) ListByGrantee(_ context.Context, granteeID string) ([]*types.Delegation, error) {
	var results []*types.Delegation
	for _, d := range m.delegations {
		if d.GranteeID == granteeID && d.RevokedAt == "" {
			results = append(results, d)
		}
	}
	return results, nil
}

func (m *mockDelegationRepo) ListByGranter(_ context.Context, granterID string) ([]*types.Delegation, error) {
	var results []*types.Delegation
	for _, d := range m.delegations {
		if d.GranterID == granterID {
			results = append(results, d)
		}
	}
	return results, nil
}

func (m *mockDelegationRepo) ListAll(_ context.Context) ([]*types.Delegation, error) {
	var results []*types.Delegation
	for _, d := range m.delegations {
		results = append(results, d)
	}
	return results, nil
}

func (m *mockDelegationRepo) Revoke(_ context.Context, id string) error {
	d, ok := m.delegations[id]
	if !ok {
		return repo.ErrNotFound
	}
	if d.RevokedAt != "" {
		return repo.ErrNotFound
	}
	d.RevokedAt = "2026-01-01T00:00:00Z"
	return nil
}

func (m *mockDelegationRepo) CleanupExpired(_ context.Context) (int, error) {
	return 0, nil
}

// --- mock secret provider ---

type mockSecretProvider struct {
	store map[string]string
}

func newMockSecretProvider() *mockSecretProvider {
	return &mockSecretProvider{store: make(map[string]string)}
}

func (p *mockSecretProvider) Name() string { return "mock" }

func (p *mockSecretProvider) Get(_ context.Context, key, scope string) (string, error) {
	v, ok := p.store[scope+"/"+key]
	if !ok {
		return "", fmt.Errorf("%w: %s", secrets.ErrSecretNotFound, key)
	}
	return v, nil
}

func (p *mockSecretProvider) Set(_ context.Context, key, value, scope string) error {
	p.store[scope+"/"+key] = value
	return nil
}

func (p *mockSecretProvider) Delete(_ context.Context, key, scope string) error {
	delete(p.store, scope+"/"+key)
	return nil
}

func (p *mockSecretProvider) List(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (p *mockSecretProvider) Rotate(_ context.Context, _, _, _ string) (string, error) {
	return "", nil
}

func (p *mockSecretProvider) Health(_ context.Context) error { return nil }

func (p *mockSecretProvider) SetWithAccess(_ context.Context, key, value, scope, _ string) error {
	p.store[scope+"/"+key] = value
	return nil
}

func (p *mockSecretProvider) ListEntries(_ context.Context, _ string) ([]repo.SecretEntry, error) {
	return nil, nil
}

func (p *mockSecretProvider) GetAccessScope(_ context.Context, _, _ string) (string, error) {
	return "global", nil
}

func (p *mockSecretProvider) UpdateAccessScope(_ context.Context, _, _, _ string) error {
	return nil
}

// --- test helpers ---

var testIDCounter atomic.Int64

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func testService(r repo.DelegationRepo, sec *secrets.Registry) *Service {
	svc := NewService(r, sec, nil, testLogger())
	svc.idFunc = func() string {
		return fmt.Sprintf("test_dlg_%d", testIDCounter.Add(1))
	}
	return svc
}

func testRegistry() (*secrets.Registry, *mockSecretProvider) {
	reg := secrets.NewRegistry()
	mp := newMockSecretProvider()
	reg.Register(mp)
	_ = reg.SetActive("mock")
	return reg, mp
}

// --- tests ---

func TestService_Grant_ClearanceElevation(t *testing.T) {
	ctx := context.Background()
	r := newMockDelegationRepo()
	svc := testService(r, nil)

	d, err := svc.Grant(ctx, GrantRequest{
		GranterID:     "persona_admin",
		GranteeID:     "persona_agent",
		GrantType:     types.GrantClearanceElevation,
		ElevatedLevel: 2,
		Reason:        "need elevated access",
	})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	if d.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if d.GrantType != types.GrantClearanceElevation {
		t.Fatalf("expected clearance_elevation, got %s", d.GrantType)
	}
	if d.ElevatedLevel != 2 {
		t.Fatalf("expected level 2, got %d", d.ElevatedLevel)
	}
}

func TestService_Grant_CredentialPassthrough(t *testing.T) {
	ctx := context.Background()
	r := newMockDelegationRepo()
	reg, mp := testRegistry()
	svc := testService(r, reg)

	d, err := svc.Grant(ctx, GrantRequest{
		GranterID:  "persona_admin",
		GranteeID:  "persona_agent",
		GrantType:  types.GrantCredentialPassthrough,
		Credential: "my-api-token",
		Reason:     "share API access",
	})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	if d.CredentialKey == "" {
		t.Fatal("expected credential_key to be set")
	}

	// Verify the credential was stored in the secret provider.
	stored, ok := mp.store["global/"+d.CredentialKey]
	if !ok {
		t.Fatal("credential not stored in secret provider")
	}
	if stored != "my-api-token" {
		t.Fatalf("expected my-api-token, got %s", stored)
	}
}

func TestService_Grant_CredentialPassthrough_MissingCredential(t *testing.T) {
	ctx := context.Background()
	svc := testService(newMockDelegationRepo(), nil)

	_, err := svc.Grant(ctx, GrantRequest{
		GranterID: "admin",
		GranteeID: "agent",
		GrantType: types.GrantCredentialPassthrough,
		Reason:    "test",
	})
	if err == nil {
		t.Fatal("expected error for missing credential")
	}
}

func TestService_Grant_SelfDelegation(t *testing.T) {
	ctx := context.Background()
	svc := testService(newMockDelegationRepo(), nil)

	_, err := svc.Grant(ctx, GrantRequest{
		GranterID: "same_id",
		GranteeID: "same_id",
		GrantType: types.GrantScopeAccess,
		Reason:    "test",
	})
	if err == nil {
		t.Fatal("expected error for self-delegation")
	}
}

func TestService_Grant_ScopeAccess(t *testing.T) {
	ctx := context.Background()
	svc := testService(newMockDelegationRepo(), nil)

	d, err := svc.Grant(ctx, GrantRequest{
		GranterID: "admin",
		GranteeID: "agent",
		GrantType: types.GrantScopeAccess,
		Scopes:    []string{"tools:write", "events:subscribe"},
		Reason:    "need extra scopes",
	})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	if len(d.Scopes) != 2 {
		t.Fatalf("expected 2 scopes, got %d", len(d.Scopes))
	}
}

func TestService_Revoke(t *testing.T) {
	ctx := context.Background()
	r := newMockDelegationRepo()
	svc := testService(r, nil)

	d, _ := svc.Grant(ctx, GrantRequest{
		GranterID:     "admin",
		GranteeID:     "agent",
		GrantType:     types.GrantClearanceElevation,
		ElevatedLevel: 2,
		Reason:        "test",
	})

	if err := svc.Revoke(ctx, d.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	got, _ := svc.GetByID(ctx, d.ID)
	if got.RevokedAt == "" {
		t.Fatal("expected revoked_at to be set")
	}
}

func TestService_Revoke_CleansUpCredential(t *testing.T) {
	ctx := context.Background()
	r := newMockDelegationRepo()
	reg, mp := testRegistry()
	svc := testService(r, reg)

	d, _ := svc.Grant(ctx, GrantRequest{
		GranterID:  "admin",
		GranteeID:  "agent",
		GrantType:  types.GrantCredentialPassthrough,
		Credential: "secret-token",
		Reason:     "test",
	})

	// Credential should exist.
	if _, ok := mp.store["global/"+d.CredentialKey]; !ok {
		t.Fatal("credential should exist before revoke")
	}

	if err := svc.Revoke(ctx, d.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	// Credential should be cleaned up.
	if _, ok := mp.store["global/"+d.CredentialKey]; ok {
		t.Fatal("credential should be cleaned up after revoke")
	}
}

func TestService_Revoke_NotFound(t *testing.T) {
	ctx := context.Background()
	svc := testService(newMockDelegationRepo(), nil)

	err := svc.Revoke(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent delegation")
	}
}

func TestService_GetCredential(t *testing.T) {
	ctx := context.Background()
	r := newMockDelegationRepo()
	reg, _ := testRegistry()
	svc := testService(r, reg)

	d, _ := svc.Grant(ctx, GrantRequest{
		GranterID:  "admin",
		GranteeID:  "agent",
		GrantType:  types.GrantCredentialPassthrough,
		Credential: "the-secret",
		Reason:     "test",
	})

	// Grantee can retrieve.
	val, err := svc.GetCredential(ctx, d.ID, "agent")
	if err != nil {
		t.Fatalf("get credential: %v", err)
	}
	if val != "the-secret" {
		t.Fatalf("expected the-secret, got %s", val)
	}

	// Granter can retrieve.
	val2, err := svc.GetCredential(ctx, d.ID, "admin")
	if err != nil {
		t.Fatalf("get credential by granter: %v", err)
	}
	if val2 != "the-secret" {
		t.Fatalf("expected the-secret, got %s", val2)
	}
}

func TestService_GetCredential_Unauthorized(t *testing.T) {
	ctx := context.Background()
	r := newMockDelegationRepo()
	reg, _ := testRegistry()
	svc := testService(r, reg)

	d, _ := svc.Grant(ctx, GrantRequest{
		GranterID:  "admin",
		GranteeID:  "agent",
		GrantType:  types.GrantCredentialPassthrough,
		Credential: "secret",
		Reason:     "test",
	})

	_, err := svc.GetCredential(ctx, d.ID, "outsider")
	if err == nil {
		t.Fatal("expected error for unauthorized requester")
	}
}

func TestService_GetCredential_WrongType(t *testing.T) {
	ctx := context.Background()
	r := newMockDelegationRepo()
	svc := testService(r, nil)

	d, _ := svc.Grant(ctx, GrantRequest{
		GranterID:     "admin",
		GranteeID:     "agent",
		GrantType:     types.GrantClearanceElevation,
		ElevatedLevel: 2,
		Reason:        "test",
	})

	_, err := svc.GetCredential(ctx, d.ID, "agent")
	if err == nil {
		t.Fatal("expected error for non-passthrough delegation")
	}
}

func TestService_ListByGrantee(t *testing.T) {
	ctx := context.Background()
	r := newMockDelegationRepo()
	svc := testService(r, nil)

	_, _ = svc.Grant(ctx, GrantRequest{
		GranterID: "admin", GranteeID: "agent",
		GrantType: types.GrantScopeAccess, Reason: "test",
	})
	_, _ = svc.Grant(ctx, GrantRequest{
		GranterID: "admin2", GranteeID: "agent",
		GrantType: types.GrantClearanceElevation, ElevatedLevel: 1, Reason: "test",
	})
	_, _ = svc.Grant(ctx, GrantRequest{
		GranterID: "admin", GranteeID: "other",
		GrantType: types.GrantScopeAccess, Reason: "test",
	})

	list, err := svc.ListByGrantee(ctx, "agent")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 delegations for agent, got %d", len(list))
	}
}

func TestService_Grant_MissingFields(t *testing.T) {
	ctx := context.Background()
	svc := testService(newMockDelegationRepo(), nil)

	tests := []struct {
		name string
		req  GrantRequest
	}{
		{"no granter", GrantRequest{GranteeID: "a", GrantType: types.GrantScopeAccess, Reason: "x"}},
		{"no grantee", GrantRequest{GranterID: "a", GrantType: types.GrantScopeAccess, Reason: "x"}},
	}

	for _, tt := range tests {
		_, err := svc.Grant(ctx, tt.req)
		if err == nil {
			t.Errorf("%s: expected error", tt.name)
		}
	}
}

func TestService_GetCredential_Revoked(t *testing.T) {
	ctx := context.Background()
	r := newMockDelegationRepo()
	reg, _ := testRegistry()
	svc := testService(r, reg)

	d, _ := svc.Grant(ctx, GrantRequest{
		GranterID:  "admin",
		GranteeID:  "agent",
		GrantType:  types.GrantCredentialPassthrough,
		Credential: "secret",
		Reason:     "test",
	})

	_ = svc.Revoke(ctx, d.ID)

	_, err := svc.GetCredential(ctx, d.ID, "agent")
	if err == nil {
		t.Fatal("expected error for revoked delegation credential")
	}
	if !errors.Is(err, nil) {
		// Just verify error contains useful message.
		if err.Error() == "" {
			t.Fatal("expected non-empty error")
		}
	}
}
