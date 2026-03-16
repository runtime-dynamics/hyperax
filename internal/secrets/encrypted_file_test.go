package secrets

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempVaultPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "vault.json.enc")
}

func TestEncryptedFileProvider_Name(t *testing.T) {
	p, err := NewEncryptedFileProvider(tempVaultPath(t), "testpass")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if p.Name() != "encrypted_file" {
		t.Fatalf("expected name 'encrypted_file', got %q", p.Name())
	}
}

func TestEncryptedFileProvider_EmptyPath(t *testing.T) {
	_, err := NewEncryptedFileProvider("", "pass")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestEncryptedFileProvider_EmptyPassphrase(t *testing.T) {
	_, err := NewEncryptedFileProvider(tempVaultPath(t), "")
	if err == nil {
		t.Fatal("expected error for empty passphrase")
	}
}

func TestEncryptedFileProvider_SetGet(t *testing.T) {
	path := tempVaultPath(t)
	p, err := NewEncryptedFileProvider(path, "testpass123")
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	ctx := context.Background()

	if err := p.Set(ctx, "api_key", "sk-12345", "global"); err != nil {
		t.Fatalf("set: %v", err)
	}

	val, err := p.Get(ctx, "api_key", "global")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if val != "sk-12345" {
		t.Fatalf("expected 'sk-12345', got %q", val)
	}

	// Verify file exists on disk.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("vault file should exist: %v", err)
	}
}

func TestEncryptedFileProvider_GetNotFound(t *testing.T) {
	p, _ := NewEncryptedFileProvider(tempVaultPath(t), "pass")
	ctx := context.Background()

	_, err := p.Get(ctx, "nonexistent", "global")
	if err == nil {
		t.Fatal("expected error for nonexistent key")
	}
	if !strings.Contains(err.Error(), "secret not found") {
		t.Fatalf("expected 'secret not found', got %q", err.Error())
	}
}

func TestEncryptedFileProvider_Delete(t *testing.T) {
	p, _ := NewEncryptedFileProvider(tempVaultPath(t), "pass")
	ctx := context.Background()

	_ = p.Set(ctx, "key1", "value1", "global")
	if err := p.Delete(ctx, "key1", "global"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err := p.Get(ctx, "key1", "global")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestEncryptedFileProvider_DeleteNotFound(t *testing.T) {
	p, _ := NewEncryptedFileProvider(tempVaultPath(t), "pass")
	ctx := context.Background()

	err := p.Delete(ctx, "nonexistent", "global")
	if err == nil {
		t.Fatal("expected error for nonexistent key")
	}
}

func TestEncryptedFileProvider_List(t *testing.T) {
	p, _ := NewEncryptedFileProvider(tempVaultPath(t), "pass")
	ctx := context.Background()

	_ = p.Set(ctx, "key1", "v1", "global")
	_ = p.Set(ctx, "key2", "v2", "global")
	_ = p.Set(ctx, "key3", "v3", "workspace")

	keys, err := p.List(ctx, "global")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
}

func TestEncryptedFileProvider_ListEmptyScope(t *testing.T) {
	p, _ := NewEncryptedFileProvider(tempVaultPath(t), "pass")
	keys, err := p.List(context.Background(), "empty_scope")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("expected 0 keys, got %d", len(keys))
	}
}

func TestEncryptedFileProvider_Rotate(t *testing.T) {
	p, _ := NewEncryptedFileProvider(tempVaultPath(t), "pass")
	ctx := context.Background()

	_ = p.Set(ctx, "api_key", "old_value", "global")

	oldVal, err := p.Rotate(ctx, "api_key", "new_value", "global")
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if oldVal != "old_value" {
		t.Fatalf("expected old value 'old_value', got %q", oldVal)
	}

	newVal, err := p.Get(ctx, "api_key", "global")
	if err != nil {
		t.Fatalf("get after rotate: %v", err)
	}
	if newVal != "new_value" {
		t.Fatalf("expected 'new_value', got %q", newVal)
	}
}

func TestEncryptedFileProvider_RotateNotFound(t *testing.T) {
	p, _ := NewEncryptedFileProvider(tempVaultPath(t), "pass")
	_, err := p.Rotate(context.Background(), "missing", "val", "global")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestEncryptedFileProvider_Persistence(t *testing.T) {
	path := tempVaultPath(t)
	ctx := context.Background()

	// Create and populate.
	p1, _ := NewEncryptedFileProvider(path, "mypassword")
	_ = p1.Set(ctx, "secret1", "value1", "global")
	_ = p1.Set(ctx, "secret2", "value2", "workspace")

	// Reload from disk with same passphrase.
	p2, err := NewEncryptedFileProvider(path, "mypassword")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}

	v1, err := p2.Get(ctx, "secret1", "global")
	if err != nil {
		t.Fatalf("get after reload: %v", err)
	}
	if v1 != "value1" {
		t.Fatalf("expected 'value1', got %q", v1)
	}

	v2, err := p2.Get(ctx, "secret2", "workspace")
	if err != nil {
		t.Fatalf("get scope after reload: %v", err)
	}
	if v2 != "value2" {
		t.Fatalf("expected 'value2', got %q", v2)
	}
}

func TestEncryptedFileProvider_WrongPassphrase(t *testing.T) {
	path := tempVaultPath(t)
	ctx := context.Background()

	// Create with one passphrase.
	p1, _ := NewEncryptedFileProvider(path, "correct_passphrase")
	_ = p1.Set(ctx, "key", "value", "global")

	// Reload with wrong passphrase — should succeed loading (structure is valid)
	// but fail to decrypt individual values.
	p2, err := NewEncryptedFileProvider(path, "wrong_passphrase")
	if err != nil {
		t.Fatalf("load should succeed: %v", err)
	}

	_, err = p2.Get(ctx, "key", "global")
	if err == nil {
		t.Fatal("expected decryption error with wrong passphrase")
	}
}

func TestEncryptedFileProvider_Health(t *testing.T) {
	p, _ := NewEncryptedFileProvider(tempVaultPath(t), "pass")
	if err := p.Health(context.Background()); err != nil {
		t.Fatalf("health: %v", err)
	}
}

func TestEncryptedFileProvider_MultipleScopes(t *testing.T) {
	p, _ := NewEncryptedFileProvider(tempVaultPath(t), "pass")
	ctx := context.Background()

	_ = p.Set(ctx, "shared_key", "global_val", "global")
	_ = p.Set(ctx, "shared_key", "ws_val", "workspace_1")

	globalVal, _ := p.Get(ctx, "shared_key", "global")
	wsVal, _ := p.Get(ctx, "shared_key", "workspace_1")

	if globalVal != "global_val" {
		t.Fatalf("expected 'global_val', got %q", globalVal)
	}
	if wsVal != "ws_val" {
		t.Fatalf("expected 'ws_val', got %q", wsVal)
	}
}

func TestEncryptedFileProvider_UpdateExistingKey(t *testing.T) {
	p, _ := NewEncryptedFileProvider(tempVaultPath(t), "pass")
	ctx := context.Background()

	_ = p.Set(ctx, "key", "first", "global")
	_ = p.Set(ctx, "key", "second", "global")

	val, _ := p.Get(ctx, "key", "global")
	if val != "second" {
		t.Fatalf("expected 'second', got %q", val)
	}
}

func TestEncryptedFileProvider_FilePermissions(t *testing.T) {
	path := tempVaultPath(t)
	p, _ := NewEncryptedFileProvider(path, "pass")
	_ = p.Set(context.Background(), "key", "val", "global")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	mode := info.Mode().Perm()
	if mode != 0o600 {
		t.Fatalf("expected file mode 0600, got %o", mode)
	}
}

func TestEncryptedFileProvider_LargeValue(t *testing.T) {
	p, _ := NewEncryptedFileProvider(tempVaultPath(t), "pass")
	ctx := context.Background()

	// 100KB value.
	largeVal := strings.Repeat("A", 100*1024)
	if err := p.Set(ctx, "large", largeVal, "global"); err != nil {
		t.Fatalf("set large: %v", err)
	}

	got, err := p.Get(ctx, "large", "global")
	if err != nil {
		t.Fatalf("get large: %v", err)
	}
	if got != largeVal {
		t.Fatalf("large value mismatch: got %d bytes, want %d", len(got), len(largeVal))
	}
}
