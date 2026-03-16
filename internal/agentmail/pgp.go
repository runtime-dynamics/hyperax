package agentmail

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"

	"github.com/hyperax/hyperax/pkg/types"
)

// KeyPair holds an Ed25519 signing key pair.
type KeyPair struct {
	PublicKey  ed25519.PublicKey  `json:"public_key"`
	PrivateKey ed25519.PrivateKey `json:"private_key"`
}

// GenerateKeyPair creates a new Ed25519 signing key pair using crypto/rand.
func GenerateKeyPair() (*KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 keypair: %w", err)
	}
	return &KeyPair{
		PublicKey:  pub,
		PrivateKey: priv,
	}, nil
}

// PublicKeyBase64 returns the base64-encoded public key for storage/exchange.
func (kp *KeyPair) PublicKeyBase64() string {
	return base64.StdEncoding.EncodeToString(kp.PublicKey)
}

// PrivateKeyBase64 returns the base64-encoded private key for secure storage.
func (kp *KeyPair) PrivateKeyBase64() string {
	return base64.StdEncoding.EncodeToString(kp.PrivateKey)
}

// KeyPairFromBase64 reconstructs a KeyPair from base64-encoded public and private keys.
func KeyPairFromBase64(pubB64, privB64 string) (*KeyPair, error) {
	pub, err := base64.StdEncoding.DecodeString(pubB64)
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}
	priv, err := base64.StdEncoding.DecodeString(privB64)
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key length: %d", len(pub))
	}
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid private key length: %d", len(priv))
	}
	return &KeyPair{
		PublicKey:  ed25519.PublicKey(pub),
		PrivateKey: ed25519.PrivateKey(priv),
	}, nil
}

// signablePayload builds the canonical byte sequence that is signed/verified.
// It includes mail ID, from, to, workspace, and payload to prevent tampering.
func signablePayload(mail *types.AgentMail) []byte {
	h := sha256.New()
	h.Write([]byte(mail.ID))
	h.Write([]byte(mail.From))
	h.Write([]byte(mail.To))
	h.Write([]byte(mail.WorkspaceID))
	h.Write(mail.Payload)
	return h.Sum(nil)
}

// SignMail signs the mail envelope with the sender's Ed25519 private key.
// The signature is stored as a base64-encoded string in mail.PGPSignature.
func SignMail(mail *types.AgentMail, privateKey ed25519.PrivateKey) error {
	if mail == nil {
		return fmt.Errorf("mail must not be nil")
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("invalid private key length: %d", len(privateKey))
	}

	digest := signablePayload(mail)
	sig := ed25519.Sign(privateKey, digest)
	mail.PGPSignature = base64.StdEncoding.EncodeToString(sig)
	return nil
}

// VerifyMail verifies the mail envelope signature against the sender's public key.
// Returns nil if the signature is valid, or an error describing the failure.
func VerifyMail(mail *types.AgentMail, publicKey ed25519.PublicKey) error {
	if mail == nil {
		return fmt.Errorf("mail must not be nil")
	}
	if mail.PGPSignature == "" {
		return fmt.Errorf("mail has no signature")
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key length: %d", len(publicKey))
	}

	sig, err := base64.StdEncoding.DecodeString(mail.PGPSignature)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}

	digest := signablePayload(mail)
	if !ed25519.Verify(publicKey, digest, sig) {
		return fmt.Errorf("signature verification failed")
	}
	return nil
}

// EncryptPayload encrypts the mail payload using AES-256-GCM.
// The encryption key is derived from a SHA-256 hash of the shared secret.
// The nonce is prepended to the ciphertext and stored as base64 in mail.Payload.
// mail.Encrypted is set to true after successful encryption.
func EncryptPayload(mail *types.AgentMail, sharedSecret []byte) error {
	if mail == nil {
		return fmt.Errorf("mail must not be nil")
	}
	if len(sharedSecret) == 0 {
		return fmt.Errorf("shared secret must not be empty")
	}

	// Derive a 256-bit key from the shared secret.
	key := sha256.Sum256(sharedSecret)

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return fmt.Errorf("create aes cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("create gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("generate nonce: %w", err)
	}

	// Seal prepends the nonce to the ciphertext for self-contained decryption.
	ciphertext := gcm.Seal(nonce, nonce, mail.Payload, nil)
	encoded := base64.StdEncoding.EncodeToString(ciphertext)

	mail.Payload = json.RawMessage(`"` + encoded + `"`)
	mail.Encrypted = true
	return nil
}

// DecryptPayload decrypts the mail payload using AES-256-GCM.
// Expects the payload to be a base64-encoded JSON string with nonce prepended.
// mail.Encrypted is set to false after successful decryption.
func DecryptPayload(mail *types.AgentMail, sharedSecret []byte) error {
	if mail == nil {
		return fmt.Errorf("mail must not be nil")
	}
	if !mail.Encrypted {
		return fmt.Errorf("mail is not encrypted")
	}
	if len(sharedSecret) == 0 {
		return fmt.Errorf("shared secret must not be empty")
	}

	// Unmarshal the base64 string from the JSON payload.
	var encoded string
	if err := json.Unmarshal(mail.Payload, &encoded); err != nil {
		return fmt.Errorf("unmarshal encrypted payload: %w", err)
	}

	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("decode ciphertext: %w", err)
	}

	key := sha256.Sum256(sharedSecret)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return fmt.Errorf("create aes cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("create gcm: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return fmt.Errorf("decrypt payload: %w", err)
	}

	mail.Payload = json.RawMessage(plaintext)
	mail.Encrypted = false
	return nil
}

// SealEnvelope signs and optionally encrypts an AgentMail envelope.
// If sharedSecret is non-nil, the payload is encrypted before signing.
func SealEnvelope(mail *types.AgentMail, kp *KeyPair, sharedSecret []byte) error {
	if sharedSecret != nil {
		if err := EncryptPayload(mail, sharedSecret); err != nil {
			return fmt.Errorf("encrypt: %w", err)
		}
	}
	if err := SignMail(mail, kp.PrivateKey); err != nil {
		return fmt.Errorf("sign: %w", err)
	}
	return nil
}

// OpenEnvelope verifies the signature and optionally decrypts an AgentMail envelope.
// If sharedSecret is non-nil and mail.Encrypted is true, decryption is attempted.
func OpenEnvelope(mail *types.AgentMail, senderPubKey ed25519.PublicKey, sharedSecret []byte) error {
	if err := VerifyMail(mail, senderPubKey); err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	if mail.Encrypted && sharedSecret != nil {
		if err := DecryptPayload(mail, sharedSecret); err != nil {
			return fmt.Errorf("decrypt: %w", err)
		}
	}
	return nil
}
