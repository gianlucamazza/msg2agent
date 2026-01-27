package crypto

import (
	"bytes"
	"testing"
)

func TestGenerateSigningKeyPair(t *testing.T) {
	kp, err := GenerateSigningKeyPair()
	if err != nil {
		t.Fatalf("failed to generate signing key pair: %v", err)
	}

	if len(kp.PublicKey) != Ed25519PublicKeySize {
		t.Errorf("expected public key size %d, got %d", Ed25519PublicKeySize, len(kp.PublicKey))
	}
	if len(kp.PrivateKey) != Ed25519PrivateKeySize {
		t.Errorf("expected private key size %d, got %d", Ed25519PrivateKeySize, len(kp.PrivateKey))
	}
}

func TestGenerateEncryptionKeyPair(t *testing.T) {
	kp, err := GenerateEncryptionKeyPair()
	if err != nil {
		t.Fatalf("failed to generate encryption key pair: %v", err)
	}

	if len(kp.PublicKey) != X25519KeySize {
		t.Errorf("expected public key size %d, got %d", X25519KeySize, len(kp.PublicKey))
	}
	if len(kp.PrivateKey) != X25519KeySize {
		t.Errorf("expected private key size %d, got %d", X25519KeySize, len(kp.PrivateKey))
	}
}

func TestSignAndVerify(t *testing.T) {
	kp, err := GenerateSigningKeyPair()
	if err != nil {
		t.Fatalf("failed to generate signing key pair: %v", err)
	}

	message := []byte("test message for signing")
	signature := kp.Sign(message)

	if !kp.Verify(message, signature) {
		t.Error("signature verification failed")
	}

	// Tampered message should fail
	tampered := []byte("tampered message")
	if kp.Verify(tampered, signature) {
		t.Error("tampered message verification should fail")
	}
}

func TestVerifySignatureStatic(t *testing.T) {
	kp, err := GenerateSigningKeyPair()
	if err != nil {
		t.Fatalf("failed to generate signing key pair: %v", err)
	}

	message := []byte("test message")
	signature := kp.Sign(message)

	if !VerifySignature(kp.PublicKey, message, signature) {
		t.Error("static signature verification failed")
	}
}

func TestEncryptDecrypt(t *testing.T) {
	alice, err := GenerateEncryptionKeyPair()
	if err != nil {
		t.Fatalf("failed to generate Alice's key pair: %v", err)
	}

	bob, err := GenerateEncryptionKeyPair()
	if err != nil {
		t.Fatalf("failed to generate Bob's key pair: %v", err)
	}

	plaintext := []byte("secret message from Alice to Bob")

	ciphertext, err := Encrypt(plaintext, alice.PrivateKey, bob.PublicKey)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	// Bob decrypts
	decrypted, err := Decrypt(ciphertext, bob.PrivateKey, alice.PublicKey)
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("decrypted message doesn't match: got %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptDecryptAnonymous(t *testing.T) {
	recipient, err := GenerateEncryptionKeyPair()
	if err != nil {
		t.Fatalf("failed to generate recipient key pair: %v", err)
	}

	plaintext := []byte("anonymous secret message")

	ciphertext, err := EncryptAnonymous(plaintext, recipient.PublicKey)
	if err != nil {
		t.Fatalf("anonymous encryption failed: %v", err)
	}

	decrypted, err := DecryptAnonymous(ciphertext, recipient.PrivateKey)
	if err != nil {
		t.Fatalf("anonymous decryption failed: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("decrypted message doesn't match: got %q, want %q", decrypted, plaintext)
	}
}

func TestComputeSharedSecret(t *testing.T) {
	alice, err := GenerateEncryptionKeyPair()
	if err != nil {
		t.Fatalf("failed to generate Alice's key pair: %v", err)
	}

	bob, err := GenerateEncryptionKeyPair()
	if err != nil {
		t.Fatalf("failed to generate Bob's key pair: %v", err)
	}

	// Alice computes shared secret with Bob's public key
	aliceSecret, err := alice.ComputeSharedSecret(bob.PublicKey)
	if err != nil {
		t.Fatalf("Alice failed to compute shared secret: %v", err)
	}

	// Bob computes shared secret with Alice's public key
	bobSecret, err := bob.ComputeSharedSecret(alice.PublicKey)
	if err != nil {
		t.Fatalf("Bob failed to compute shared secret: %v", err)
	}

	// Both should arrive at the same shared secret
	if !bytes.Equal(aliceSecret, bobSecret) {
		t.Error("shared secrets don't match")
	}
}

func TestDecryptWithWrongKey(t *testing.T) {
	alice, _ := GenerateEncryptionKeyPair()
	bob, _ := GenerateEncryptionKeyPair()
	eve, _ := GenerateEncryptionKeyPair()

	plaintext := []byte("secret message")
	ciphertext, _ := Encrypt(plaintext, alice.PrivateKey, bob.PublicKey)

	// Eve shouldn't be able to decrypt
	_, err := Decrypt(ciphertext, eve.PrivateKey, alice.PublicKey)
	if err == nil {
		t.Error("decryption with wrong key should fail")
	}
}

func TestGenerateAgentKeys(t *testing.T) {
	keys, err := GenerateAgentKeys()
	if err != nil {
		t.Fatalf("failed to generate agent keys: %v", err)
	}

	if keys.Signing == nil {
		t.Error("signing keys should not be nil")
	}
	if keys.Encryption == nil {
		t.Error("encryption keys should not be nil")
	}
}
