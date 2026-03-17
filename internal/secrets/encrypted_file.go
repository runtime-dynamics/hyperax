package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/hyperax/hyperax/internal/repo"
	"golang.org/x/crypto/argon2"
)

// Compile-time interface assertion.
var _ Provider = (*EncryptedFileProvider)(nil)

// argon2Params defines the Argon2id key derivation parameters.
// These are recommended values for interactive use per RFC 9106.
type argon2Params struct {
	Time    uint32 `json:"time"`    // iterations
	Memory  uint32 `json:"memory"`  // memory in KiB
	Threads uint8  `json:"threads"` // parallelism
	KeyLen  uint32 `json:"key_len"` // derived key length in bytes
	Salt    []byte `json:"salt"`    // random salt
}

// defaultArgon2Params returns conservative Argon2id parameters.
func defaultArgon2Params() argon2Params {
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return argon2Params{
		Time:    3,
		Memory:  64 * 1024, // 64 MiB
		Threads: 4,
		KeyLen:  32, // AES-256
		Salt:    salt,
	}
}

// encryptedVault is the on-disk format for the encrypted secrets file.
type encryptedVault struct {
	Version int            `json:"version"`
	KDF     argon2Params   `json:"kdf"`
	Secrets map[string]any `json:"secrets"` // scope -> key -> encrypted_value
}

// EncryptedFileProvider stores secrets in an AES-256-GCM encrypted JSON file
// on disk with key derivation from a passphrase via Argon2id.
//
// File layout: { version, kdf_params, secrets: { scope: { key: base64(nonce+ciphertext) } } }
//
// The passphrase never touches disk — it is provided at construction time and
// held in memory only for the process lifetime.
type EncryptedFileProvider struct {
	mu         sync.RWMutex
	filePath   string
	passphrase string
	vault      *encryptedVault
	gcm        cipher.AEAD
}

// NewEncryptedFileProvider creates a provider that stores secrets in an
// encrypted JSON file at filePath. The passphrase is used to derive the
// AES-256 encryption key via Argon2id.
//
// If the file exists, it is loaded and decrypted. Otherwise, a new vault is
// created on the first Set operation.
func NewEncryptedFileProvider(filePath, passphrase string) (*EncryptedFileProvider, error) {
	if filePath == "" {
		return nil, fmt.Errorf("encrypted file path must not be empty")
	}
	if passphrase == "" {
		return nil, fmt.Errorf("passphrase must not be empty")
	}

	p := &EncryptedFileProvider{
		filePath:   filePath,
		passphrase: passphrase,
	}

	// Try to load existing vault.
	if _, err := os.Stat(filePath); err == nil {
		if loadErr := p.loadVault(); loadErr != nil {
			return nil, fmt.Errorf("load encrypted vault: %w", loadErr)
		}
	} else {
		// Create a fresh vault with new KDF params.
		params := defaultArgon2Params()
		p.vault = &encryptedVault{
			Version: 1,
			KDF:     params,
			Secrets: make(map[string]any),
		}
		key := deriveKey(passphrase, params)
		gcm, err := newGCM(key)
		if err != nil {
			return nil, fmt.Errorf("init cipher: %w", err)
		}
		p.gcm = gcm
	}

	return p, nil
}

// Name returns "encrypted_file".
func (p *EncryptedFileProvider) Name() string { return "encrypted_file" }

// Get retrieves a secret from the encrypted vault.
func (p *EncryptedFileProvider) Get(_ context.Context, key, scope string) (string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	val, err := p.getInternal(key, scope)
	if err != nil {
		return "", err
	}
	return val, nil
}

// Set creates or updates a secret in the encrypted vault and persists to disk.
func (p *EncryptedFileProvider) Set(_ context.Context, key, value, scope string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	encrypted, err := p.encrypt([]byte(value))
	if err != nil {
		return fmt.Errorf("encrypt secret: %w", err)
	}

	scopeMap := p.ensureScopeMap(scope)
	scopeMap[key] = encrypted

	return p.saveVault()
}

// Delete removes a secret from the encrypted vault and persists.
func (p *EncryptedFileProvider) Delete(_ context.Context, key, scope string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	scopeMap, ok := p.scopeMap(scope)
	if !ok {
		return fmt.Errorf("%w: %s in scope %s", ErrSecretNotFound, key, scope)
	}

	if _, exists := scopeMap[key]; !exists {
		return fmt.Errorf("%w: %s in scope %s", ErrSecretNotFound, key, scope)
	}

	delete(scopeMap, key)
	return p.saveVault()
}

// List returns all secret keys for a scope.
func (p *EncryptedFileProvider) List(_ context.Context, scope string) ([]string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	scopeMap, ok := p.scopeMap(scope)
	if !ok {
		return nil, nil
	}

	keys := make([]string, 0, len(scopeMap))
	for k := range scopeMap {
		keys = append(keys, k)
	}
	return keys, nil
}

// SetWithAccess stores a secret with an access scope restriction.
// The encrypted file provider does not support access scopes; delegates to Set.
func (p *EncryptedFileProvider) SetWithAccess(ctx context.Context, key, value, scope, accessScope string) error {
	return p.Set(ctx, key, value, scope)
}

// ListEntries returns secret metadata for a scope.
// The encrypted file provider does not store access_scope metadata, so entries
// are returned with access_scope="global" as a default.
func (p *EncryptedFileProvider) ListEntries(_ context.Context, scope string) ([]repo.SecretEntry, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	scopeMap, ok := p.scopeMap(scope)
	if !ok {
		return nil, nil
	}

	entries := make([]repo.SecretEntry, 0, len(scopeMap))
	for k := range scopeMap {
		entries = append(entries, repo.SecretEntry{
			Key:         k,
			Scope:       scope,
			AccessScope: "global",
		})
	}
	return entries, nil
}

// GetAccessScope returns the access_scope for a secret.
// The encrypted file provider does not store access_scope metadata; always returns "global".
func (p *EncryptedFileProvider) GetAccessScope(_ context.Context, _, _ string) (string, error) {
	return "global", nil
}

// UpdateAccessScope is not supported by the encrypted file provider.
func (p *EncryptedFileProvider) UpdateAccessScope(_ context.Context, _, _, _ string) error {
	return fmt.Errorf("UpdateAccessScope not supported by %s provider", p.Name())
}

// Rotate replaces a secret value, returning the old value.
func (p *EncryptedFileProvider) Rotate(_ context.Context, key, newValue, scope string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	oldVal, err := p.getInternal(key, scope)
	if err != nil {
		return "", err
	}

	encrypted, err := p.encrypt([]byte(newValue))
	if err != nil {
		return "", fmt.Errorf("encrypt new value: %w", err)
	}

	scopeMap := p.ensureScopeMap(scope)
	scopeMap[key] = encrypted

	if saveErr := p.saveVault(); saveErr != nil {
		// Rollback: restore old encrypted value.
		oldEnc, encErr := p.encrypt([]byte(oldVal))
		if encErr != nil {
			return "", fmt.Errorf("rotate save failed (%w) and rollback encryption also failed: %v", saveErr, encErr)
		}
		scopeMap[key] = oldEnc
		return "", fmt.Errorf("rotate save: %w", saveErr)
	}

	return oldVal, nil
}

// Health checks that the vault file is accessible.
func (p *EncryptedFileProvider) Health(_ context.Context) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.vault == nil {
		return fmt.Errorf("vault not initialized")
	}

	// Check if the directory exists and is writable.
	dir := filepath.Dir(p.filePath)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("vault directory: %w", err)
	}

	return nil
}

// getInternal retrieves and decrypts a secret without locking (caller must hold lock).
func (p *EncryptedFileProvider) getInternal(key, scope string) (string, error) {
	scopeMap, ok := p.scopeMap(scope)
	if !ok {
		return "", fmt.Errorf("%w: %s in scope %s", ErrSecretNotFound, key, scope)
	}

	encVal, ok := scopeMap[key]
	if !ok {
		return "", fmt.Errorf("%w: %s in scope %s", ErrSecretNotFound, key, scope)
	}

	encStr, ok := encVal.(string)
	if !ok {
		return "", fmt.Errorf("corrupt vault: value for %s/%s is not a string", scope, key)
	}

	plaintext, err := p.decrypt(encStr)
	if err != nil {
		return "", fmt.Errorf("decrypt %s/%s: %w", scope, key, err)
	}

	return string(plaintext), nil
}

// scopeMap returns the map for a given scope, or false if not present.
func (p *EncryptedFileProvider) scopeMap(scope string) (map[string]any, bool) {
	raw, ok := p.vault.Secrets[scope]
	if !ok {
		return nil, false
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	return m, true
}

// ensureScopeMap returns or creates the map for a given scope.
func (p *EncryptedFileProvider) ensureScopeMap(scope string) map[string]any {
	raw, ok := p.vault.Secrets[scope]
	if ok {
		if m, ok := raw.(map[string]any); ok {
			return m
		}
	}
	m := make(map[string]any)
	p.vault.Secrets[scope] = m
	return m
}

// encrypt encrypts plaintext using AES-256-GCM, returning base64-encoded nonce+ciphertext.
func (p *EncryptedFileProvider) encrypt(plaintext []byte) (string, error) {
	nonce := make([]byte, p.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := p.gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decrypt decodes and decrypts a base64 nonce+ciphertext string.
func (p *EncryptedFileProvider) decrypt(encoded string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode base64: %w", err)
	}

	nonceSize := p.gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce := data[:nonceSize]
	ciphertext := data[nonceSize:]

	plaintext, err := p.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}

// loadVault reads and parses the encrypted vault file.
func (p *EncryptedFileProvider) loadVault() error {
	data, err := os.ReadFile(p.filePath)
	if err != nil {
		return fmt.Errorf("read vault: %w", err)
	}

	var vault encryptedVault
	if err := json.Unmarshal(data, &vault); err != nil {
		return fmt.Errorf("parse vault: %w", err)
	}

	// Derive key from passphrase using stored KDF params.
	key := deriveKey(p.passphrase, vault.KDF)
	gcm, err := newGCM(key)
	if err != nil {
		return fmt.Errorf("init cipher: %w", err)
	}

	p.vault = &vault
	p.gcm = gcm
	return nil
}

// saveVault writes the vault to disk as JSON.
// The file is written atomically via temp-file + rename.
func (p *EncryptedFileProvider) saveVault() error {
	data, err := json.MarshalIndent(p.vault, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal vault: %w", err)
	}

	dir := filepath.Dir(p.filePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create vault directory: %w", err)
	}

	// Write to temp file then rename for atomicity.
	tmp := p.filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write temp vault: %w", err)
	}

	if err := os.Rename(tmp, p.filePath); err != nil {
		os.Remove(tmp) //nolint:errcheck
		return fmt.Errorf("rename vault: %w", err)
	}

	return nil
}

// deriveKey uses Argon2id to derive an encryption key from the passphrase.
func deriveKey(passphrase string, params argon2Params) []byte {
	return argon2.IDKey(
		[]byte(passphrase),
		params.Salt,
		params.Time,
		params.Memory,
		params.Threads,
		params.KeyLen,
	)
}

// newGCM creates an AES-256-GCM cipher from the given key.
func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	return gcm, nil
}
