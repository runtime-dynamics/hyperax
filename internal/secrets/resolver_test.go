package secrets

import (
	"context"
	"testing"
)

func TestParseSecretRef(t *testing.T) {
	tests := []struct {
		ref       string
		wantKey   string
		wantScope string
		wantOK    bool
	}{
		{"secret:api_key", "api_key", "global", true},
		{"secret:api_key:workspace", "api_key", "workspace", true},
		{"secret:token:agent", "token", "agent", true},
		{"not_a_secret", "", "", false},
		{"secret:", "", "", false},
		{"", "", "", false},
		{"SECRET:key", "", "", false}, // case-sensitive
	}

	for _, tt := range tests {
		key, scope, ok := ParseSecretRef(tt.ref)
		if ok != tt.wantOK {
			t.Errorf("ParseSecretRef(%q) ok=%v, want %v", tt.ref, ok, tt.wantOK)
			continue
		}
		if key != tt.wantKey {
			t.Errorf("ParseSecretRef(%q) key=%q, want %q", tt.ref, key, tt.wantKey)
		}
		if scope != tt.wantScope {
			t.Errorf("ParseSecretRef(%q) scope=%q, want %q", tt.ref, scope, tt.wantScope)
		}
	}
}

func TestResolveSecretRef_Resolves(t *testing.T) {
	ctx := context.Background()
	reg := NewRegistry()
	p := newMockProvider("local")
	p.store["global/api_key"] = "secret123"
	reg.Register(p)

	val, err := ResolveSecretRef(ctx, reg, "secret:api_key")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if val != "secret123" {
		t.Fatalf("expected secret123, got %s", val)
	}
}

func TestResolveSecretRef_WithScope(t *testing.T) {
	ctx := context.Background()
	reg := NewRegistry()
	p := newMockProvider("local")
	p.store["workspace/token"] = "ws_tok"
	reg.Register(p)

	val, err := ResolveSecretRef(ctx, reg, "secret:token:workspace")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if val != "ws_tok" {
		t.Fatalf("expected ws_tok, got %s", val)
	}
}

func TestResolveSecretRef_NotARef(t *testing.T) {
	ctx := context.Background()
	reg := NewRegistry()
	reg.Register(newMockProvider("local"))

	val, err := ResolveSecretRef(ctx, reg, "plain_value")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if val != "plain_value" {
		t.Fatalf("expected plain_value to pass through, got %s", val)
	}
}

func TestResolveSecretRef_NotFound(t *testing.T) {
	ctx := context.Background()
	reg := NewRegistry()
	reg.Register(newMockProvider("local"))

	_, err := ResolveSecretRef(ctx, reg, "secret:missing")
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
}

func TestBuildResolverFunc(t *testing.T) {
	reg := NewRegistry()
	p := newMockProvider("local")
	p.store["global/my_key"] = "resolved_val"
	reg.Register(p)

	fn := BuildResolverFunc(reg)
	if fn == nil {
		t.Fatal("expected non-nil resolver func")
	}

	val, err := fn(context.Background(), "my_key")
	if err != nil {
		t.Fatalf("resolver func: %v", err)
	}
	if val != "resolved_val" {
		t.Fatalf("expected resolved_val, got %s", val)
	}
}

func TestBuildResolverFunc_WithScope(t *testing.T) {
	reg := NewRegistry()
	p := newMockProvider("local")
	p.store["agent/token"] = "agent_tok"
	reg.Register(p)

	fn := BuildResolverFunc(reg)
	val, err := fn(context.Background(), "token:agent")
	if err != nil {
		t.Fatalf("resolver func: %v", err)
	}
	if val != "agent_tok" {
		t.Fatalf("expected agent_tok, got %s", val)
	}
}

func TestBuildResolverFunc_Nil(t *testing.T) {
	fn := BuildResolverFunc(nil)
	if fn != nil {
		t.Fatal("expected nil for nil registry")
	}
}
