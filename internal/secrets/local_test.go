package secrets

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperax/hyperax/internal/repo"
)

// fakeSecretRepo is a minimal in-memory SecretRepo for testing LocalProvider.
type fakeSecretRepo struct {
	data map[string]string // "key:scope" -> value
}

func newFakeSecretRepo() *fakeSecretRepo {
	return &fakeSecretRepo{data: make(map[string]string)}
}

func (r *fakeSecretRepo) Get(_ context.Context, key, scope string) (string, error) {
	v, ok := r.data[key+":"+scope]
	if !ok {
		return "", errors.New("secret \"" + key + "\" in scope \"" + scope + "\" not found")
	}
	return v, nil
}

func (r *fakeSecretRepo) Set(_ context.Context, key, value, scope string) error {
	r.data[key+":"+scope] = value
	return nil
}

func (r *fakeSecretRepo) Delete(_ context.Context, key, scope string) error {
	k := key + ":" + scope
	if _, ok := r.data[k]; !ok {
		return errors.New("secret \"" + key + "\" in scope \"" + scope + "\" not found")
	}
	delete(r.data, k)
	return nil
}

func (r *fakeSecretRepo) List(_ context.Context, scope string) ([]string, error) {
	var keys []string
	suffix := ":" + scope
	for k := range r.data {
		if len(k) > len(suffix) && k[len(k)-len(suffix):] == suffix {
			keys = append(keys, k[:len(k)-len(suffix)])
		}
	}
	return keys, nil
}

func (r *fakeSecretRepo) SetWithAccess(_ context.Context, key, value, scope, accessScope string) error {
	r.data[key+":"+scope] = value
	return nil
}

func (r *fakeSecretRepo) ListEntries(_ context.Context, scope string) ([]repo.SecretEntry, error) {
	var entries []repo.SecretEntry
	suffix := ":" + scope
	for k := range r.data {
		if len(k) > len(suffix) && k[len(k)-len(suffix):] == suffix {
			entries = append(entries, repo.SecretEntry{
				Key:         k[:len(k)-len(suffix)],
				Scope:       scope,
				AccessScope: "global",
			})
		}
	}
	return entries, nil
}

func (r *fakeSecretRepo) GetAccessScope(_ context.Context, key, scope string) (string, error) {
	if _, ok := r.data[key+":"+scope]; !ok {
		return "", errors.New("secret \"" + key + "\" in scope \"" + scope + "\" not found")
	}
	return "global", nil
}

func (r *fakeSecretRepo) UpdateAccessScope(_ context.Context, key, scope, _ string) error {
	if _, ok := r.data[key+":"+scope]; !ok {
		return errors.New("secret \"" + key + "\" in scope \"" + scope + "\" not found")
	}
	return nil
}

func TestLocalProvider_Name(t *testing.T) {
	p := NewLocalProvider(newFakeSecretRepo())
	if p.Name() != "local" {
		t.Fatalf("expected name=local, got %s", p.Name())
	}
}

func TestLocalProvider_NilRepo(t *testing.T) {
	p := NewLocalProvider(nil)
	if p != nil {
		t.Fatal("expected nil for nil repo")
	}
}

func TestLocalProvider_SetAndGet(t *testing.T) {
	ctx := context.Background()
	p := NewLocalProvider(newFakeSecretRepo())

	if err := p.Set(ctx, "api_key", "secret123", "global"); err != nil {
		t.Fatalf("set: %v", err)
	}

	val, err := p.Get(ctx, "api_key", "global")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if val != "secret123" {
		t.Fatalf("expected secret123, got %s", val)
	}
}

func TestLocalProvider_GetNotFound(t *testing.T) {
	ctx := context.Background()
	p := NewLocalProvider(newFakeSecretRepo())

	_, err := p.Get(ctx, "missing", "global")
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("expected ErrSecretNotFound, got %v", err)
	}
}

func TestLocalProvider_Delete(t *testing.T) {
	ctx := context.Background()
	p := NewLocalProvider(newFakeSecretRepo())

	_ = p.Set(ctx, "key1", "val1", "global")
	if err := p.Delete(ctx, "key1", "global"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err := p.Get(ctx, "key1", "global")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}

func TestLocalProvider_DeleteNotFound(t *testing.T) {
	ctx := context.Background()
	p := NewLocalProvider(newFakeSecretRepo())

	err := p.Delete(ctx, "missing", "global")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("expected ErrSecretNotFound, got %v", err)
	}
}

func TestLocalProvider_List(t *testing.T) {
	ctx := context.Background()
	p := NewLocalProvider(newFakeSecretRepo())

	_ = p.Set(ctx, "a", "1", "global")
	_ = p.Set(ctx, "b", "2", "global")
	_ = p.Set(ctx, "c", "3", "workspace")

	keys, err := p.List(ctx, "global")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
}

func TestLocalProvider_Rotate(t *testing.T) {
	ctx := context.Background()
	p := NewLocalProvider(newFakeSecretRepo())

	_ = p.Set(ctx, "token", "old_value", "global")

	old, err := p.Rotate(ctx, "token", "new_value", "global")
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if old != "old_value" {
		t.Fatalf("expected old_value, got %s", old)
	}

	val, _ := p.Get(ctx, "token", "global")
	if val != "new_value" {
		t.Fatalf("expected new_value after rotate, got %s", val)
	}
}

func TestLocalProvider_RotateNotFound(t *testing.T) {
	ctx := context.Background()
	p := NewLocalProvider(newFakeSecretRepo())

	_, err := p.Rotate(ctx, "missing", "new", "global")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("expected ErrSecretNotFound, got %v", err)
	}
}

func TestLocalProvider_Health(t *testing.T) {
	p := NewLocalProvider(newFakeSecretRepo())
	if err := p.Health(context.Background()); err != nil {
		t.Fatalf("health: %v", err)
	}
}

// failingSetRepo wraps fakeSecretRepo and makes Set fail on demand.
type failingSetRepo struct {
	*fakeSecretRepo
	failSet bool
}

func (r *failingSetRepo) Set(ctx context.Context, key, value, scope string) error {
	if r.failSet {
		return errors.New("simulated write failure")
	}
	return r.fakeSecretRepo.Set(ctx, key, value, scope)
}

func TestLocalProvider_RotateRollbackOnSetFailure(t *testing.T) {
	ctx := context.Background()
	fr := &failingSetRepo{fakeSecretRepo: newFakeSecretRepo()}
	p := NewLocalProvider(fr)

	// Seed a secret.
	_ = p.Set(ctx, "token", "original", "global")

	// Make Set fail for the rotation attempt.
	fr.failSet = true

	_, err := p.Rotate(ctx, "token", "new_value", "global")
	if err == nil {
		t.Fatal("expected error from failed Set")
	}

	// Verify the old value is preserved (rollback succeeded since failSet
	// only affects the first Set call in Rotate — the rollback Set also
	// fails, but the original value was never overwritten).
	fr.failSet = false
	val, _ := p.Get(ctx, "token", "global")
	if val != "original" {
		t.Fatalf("expected original value preserved after rollback, got %s", val)
	}
}
