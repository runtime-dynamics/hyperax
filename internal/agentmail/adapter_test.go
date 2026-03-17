package agentmail

import (
	"context"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"

	"github.com/hyperax/hyperax/pkg/types"
)

// stubAdapter is a test double for MessengerAdapter.
type stubAdapter struct {
	name      string
	healthy   bool
	started   atomic.Bool
	stopped   atomic.Bool
	sent      []*types.AgentMail
	inbound   []*types.AgentMail
	sendErr   error
	recvErr   error
	startErr  error
}

func (s *stubAdapter) Name() string { return s.name }

func (s *stubAdapter) Send(_ context.Context, mail *types.AgentMail) error {
	if s.sendErr != nil {
		return s.sendErr
	}
	s.sent = append(s.sent, mail)
	return nil
}

func (s *stubAdapter) Receive(_ context.Context) ([]*types.AgentMail, error) {
	if s.recvErr != nil {
		return nil, s.recvErr
	}
	msgs := s.inbound
	s.inbound = nil
	return msgs, nil
}

func (s *stubAdapter) Start(_ context.Context) error {
	if s.startErr != nil {
		return s.startErr
	}
	s.started.Store(true)
	return nil
}

func (s *stubAdapter) Stop() error {
	s.stopped.Store(true)
	return nil
}

func (s *stubAdapter) Healthy() bool { return s.healthy }

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestAdapterRegistry_RegisterAndList(t *testing.T) {
	reg := NewAdapterRegistry(testLogger())

	a1 := &stubAdapter{name: "webhook", healthy: true}
	a2 := &stubAdapter{name: "slack", healthy: true}

	if err := reg.Register(a1); err != nil {
		t.Fatalf("register webhook: %v", err)
	}
	if err := reg.Register(a2); err != nil {
		t.Fatalf("register slack: %v", err)
	}

	names := reg.List()
	if len(names) != 2 {
		t.Fatalf("expected 2 adapters, got %d", len(names))
	}
}

func TestAdapterRegistry_Get(t *testing.T) {
	reg := NewAdapterRegistry(testLogger())
	a := &stubAdapter{name: "test", healthy: true}
	if err := reg.Register(a); err != nil {
		t.Fatalf("register: %v", err)
	}

	got := reg.Get("test")
	if got == nil {
		t.Fatal("expected adapter, got nil")
	}
	if got.Name() != "test" {
		t.Fatalf("expected name 'test', got %q", got.Name())
	}

	if reg.Get("nonexistent") != nil {
		t.Fatal("expected nil for nonexistent adapter")
	}
}

func TestAdapterRegistry_Deregister(t *testing.T) {
	reg := NewAdapterRegistry(testLogger())
	a := &stubAdapter{name: "test", healthy: true}
	if err := reg.Register(a); err != nil {
		t.Fatalf("register: %v", err)
	}

	if err := reg.Deregister("test"); err != nil {
		t.Fatalf("deregister: %v", err)
	}

	if !a.stopped.Load() {
		t.Fatal("expected adapter to be stopped on deregister")
	}

	if reg.Get("test") != nil {
		t.Fatal("expected nil after deregister")
	}

	if err := reg.Deregister("test"); err == nil {
		t.Fatal("expected error deregistering nonexistent adapter")
	}
}

func TestAdapterRegistry_ReplaceExisting(t *testing.T) {
	reg := NewAdapterRegistry(testLogger())
	a1 := &stubAdapter{name: "test", healthy: true}
	a2 := &stubAdapter{name: "test", healthy: false}

	if err := reg.Register(a1); err != nil {
		t.Fatalf("register a1: %v", err)
	}
	if err := reg.Register(a2); err != nil {
		t.Fatalf("register a2: %v", err)
	}

	if !a1.stopped.Load() {
		t.Fatal("expected original adapter to be stopped on replace")
	}

	got := reg.Get("test")
	if got.Healthy() {
		t.Fatal("expected replaced adapter (unhealthy)")
	}
}

func TestAdapterRegistry_Healthy(t *testing.T) {
	reg := NewAdapterRegistry(testLogger())
	if err := reg.Register(&stubAdapter{name: "ok", healthy: true}); err != nil {
		t.Fatalf("register ok: %v", err)
	}
	if err := reg.Register(&stubAdapter{name: "bad", healthy: false}); err != nil {
		t.Fatalf("register bad: %v", err)
	}

	health := reg.Healthy()
	if !health["ok"] {
		t.Fatal("expected 'ok' to be healthy")
	}
	if health["bad"] {
		t.Fatal("expected 'bad' to be unhealthy")
	}
}

func TestAdapterRegistry_StartAll(t *testing.T) {
	reg := NewAdapterRegistry(testLogger())
	a := &stubAdapter{name: "test", healthy: true}
	if err := reg.Register(a); err != nil {
		t.Fatalf("register: %v", err)
	}

	reg.StartAll(context.Background())

	if !a.started.Load() {
		t.Fatal("expected adapter to be started")
	}
}

func TestAdapterRegistry_StopAll(t *testing.T) {
	reg := NewAdapterRegistry(testLogger())
	a1 := &stubAdapter{name: "a", healthy: true}
	a2 := &stubAdapter{name: "b", healthy: true}
	if err := reg.Register(a1); err != nil {
		t.Fatalf("register a1: %v", err)
	}
	if err := reg.Register(a2); err != nil {
		t.Fatalf("register a2: %v", err)
	}

	reg.StopAll()

	if !a1.stopped.Load() || !a2.stopped.Load() {
		t.Fatal("expected all adapters to be stopped")
	}

	if len(reg.List()) != 0 {
		t.Fatal("expected empty registry after StopAll")
	}
}

func TestAdapterRegistry_RegisterNil(t *testing.T) {
	reg := NewAdapterRegistry(testLogger())
	if err := reg.Register(nil); err == nil {
		t.Fatal("expected error registering nil adapter")
	}
}

func TestAdapterRegistry_RegisterEmptyName(t *testing.T) {
	reg := NewAdapterRegistry(testLogger())
	a := &stubAdapter{name: "", healthy: true}
	if err := reg.Register(a); err == nil {
		t.Fatal("expected error registering adapter with empty name")
	}
}
