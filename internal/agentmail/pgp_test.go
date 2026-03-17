package agentmail

import (
	"crypto/ed25519"
	"encoding/json"
	"testing"

	"github.com/hyperax/hyperax/pkg/types"
)

func newTestMail() *types.AgentMail {
	return &types.AgentMail{
		ID:          "test-mail-001",
		From:        "instance-a",
		To:          "instance-b",
		WorkspaceID: "ws-1",
		Priority:    types.MailPriorityStandard,
		Payload:     json.RawMessage(`{"action":"ping","data":"hello"}`),
	}
}

func TestGenerateKeyPair(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if len(kp.PublicKey) != ed25519.PublicKeySize {
		t.Errorf("public key length = %d, want %d", len(kp.PublicKey), ed25519.PublicKeySize)
	}
	if len(kp.PrivateKey) != ed25519.PrivateKeySize {
		t.Errorf("private key length = %d, want %d", len(kp.PrivateKey), ed25519.PrivateKeySize)
	}
}

func TestKeyPairBase64Roundtrip(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	restored, err := KeyPairFromBase64(kp.PublicKeyBase64(), kp.PrivateKeyBase64())
	if err != nil {
		t.Fatalf("KeyPairFromBase64: %v", err)
	}

	if !kp.PublicKey.Equal(restored.PublicKey) {
		t.Error("public key mismatch after roundtrip")
	}
	if !kp.PrivateKey.Equal(restored.PrivateKey) {
		t.Error("private key mismatch after roundtrip")
	}
}

func TestSignAndVerify(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	mail := newTestMail()

	if err := SignMail(mail, kp.PrivateKey); err != nil {
		t.Fatalf("SignMail: %v", err)
	}

	if mail.PGPSignature == "" {
		t.Fatal("signature should not be empty after signing")
	}

	if err := VerifyMail(mail, kp.PublicKey); err != nil {
		t.Fatalf("VerifyMail: %v", err)
	}
}

func TestVerifyDetectsTampering(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	mail := newTestMail()

	if err := SignMail(mail, kp.PrivateKey); err != nil {
		t.Fatalf("SignMail: %v", err)
	}

	// Tamper with the payload.
	mail.Payload = json.RawMessage(`{"action":"tampered"}`)

	if err := VerifyMail(mail, kp.PublicKey); err == nil {
		t.Fatal("expected verification to fail after tampering")
	}
}

func TestVerifyRejectsWrongKey(t *testing.T) {
	kp1, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair 1: %v", err)
	}
	kp2, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair 2: %v", err)
	}
	mail := newTestMail()

	if err := SignMail(mail, kp1.PrivateKey); err != nil {
		t.Fatalf("SignMail: %v", err)
	}

	if err := VerifyMail(mail, kp2.PublicKey); err == nil {
		t.Fatal("expected verification to fail with wrong public key")
	}
}

func TestEncryptDecryptPayload(t *testing.T) {
	secret := []byte("shared-secret-for-testing-256bit")
	mail := newTestMail()
	original := string(mail.Payload)

	if err := EncryptPayload(mail, secret); err != nil {
		t.Fatalf("EncryptPayload: %v", err)
	}

	if !mail.Encrypted {
		t.Fatal("mail.Encrypted should be true after encryption")
	}
	if string(mail.Payload) == original {
		t.Fatal("payload should differ after encryption")
	}

	if err := DecryptPayload(mail, secret); err != nil {
		t.Fatalf("DecryptPayload: %v", err)
	}

	if mail.Encrypted {
		t.Fatal("mail.Encrypted should be false after decryption")
	}
	if string(mail.Payload) != original {
		t.Errorf("decrypted payload = %q, want %q", string(mail.Payload), original)
	}
}

func TestDecryptWithWrongSecret(t *testing.T) {
	secret1 := []byte("secret-one-aaaaaaaaaaaaaaaaaaaaaa")
	secret2 := []byte("secret-two-bbbbbbbbbbbbbbbbbbbbbb")
	mail := newTestMail()

	if err := EncryptPayload(mail, secret1); err != nil {
		t.Fatalf("EncryptPayload: %v", err)
	}

	if err := DecryptPayload(mail, secret2); err == nil {
		t.Fatal("expected decryption to fail with wrong secret")
	}
}

func TestSealAndOpenEnvelope(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	secret := []byte("envelope-shared-secret-test-1234")
	mail := newTestMail()
	original := string(mail.Payload)

	if err := SealEnvelope(mail, kp, secret); err != nil {
		t.Fatalf("SealEnvelope: %v", err)
	}

	if !mail.Encrypted {
		t.Fatal("mail should be encrypted after sealing")
	}
	if mail.PGPSignature == "" {
		t.Fatal("mail should be signed after sealing")
	}

	if err := OpenEnvelope(mail, kp.PublicKey, secret); err != nil {
		t.Fatalf("OpenEnvelope: %v", err)
	}

	if mail.Encrypted {
		t.Fatal("mail should be decrypted after opening")
	}
	if string(mail.Payload) != original {
		t.Errorf("opened payload = %q, want %q", string(mail.Payload), original)
	}
}

func TestSealEnvelopeSignOnly(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	mail := newTestMail()

	if err := SealEnvelope(mail, kp, nil); err != nil {
		t.Fatalf("SealEnvelope (sign only): %v", err)
	}

	if mail.Encrypted {
		t.Fatal("mail should not be encrypted when no secret provided")
	}
	if mail.PGPSignature == "" {
		t.Fatal("mail should be signed")
	}

	if err := OpenEnvelope(mail, kp.PublicKey, nil); err != nil {
		t.Fatalf("OpenEnvelope (verify only): %v", err)
	}
}

func TestSignMailNilErrors(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	if err := SignMail(nil, kp.PrivateKey); err == nil {
		t.Fatal("expected error for nil mail")
	}
}

func TestVerifyMailNoSignature(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	mail := newTestMail()

	if err := VerifyMail(mail, kp.PublicKey); err == nil {
		t.Fatal("expected error for unsigned mail")
	}
}

func TestKeyPairFromBase64InvalidInput(t *testing.T) {
	if _, err := KeyPairFromBase64("invalid", "invalid"); err == nil {
		t.Fatal("expected error for invalid base64")
	}
}
