// Package test provides integration tests for msg2agent.
package test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/gianluca/msg2agent/pkg/agent"
	"github.com/gianluca/msg2agent/pkg/crypto"
	"github.com/gianluca/msg2agent/pkg/messaging"
	"github.com/gianluca/msg2agent/pkg/registry"
	"github.com/gianluca/msg2agent/pkg/security"
)

// TestEndToEndSignatureFlow tests the complete signing and verification flow.
func TestEndToEndSignatureFlow(t *testing.T) {
	// Create Alice and Bob
	alice, err := agent.New(agent.Config{
		Domain:  "example.com",
		AgentID: "alice",
	})
	if err != nil {
		t.Fatalf("failed to create Alice: %v", err)
	}

	bob, err := agent.New(agent.Config{
		Domain:  "example.com",
		AgentID: "bob",
	})
	if err != nil {
		t.Fatalf("failed to create Bob: %v", err)
	}

	// Register Alice in Bob's store (simulating discovery)
	bob.Store().Put(alice.Record())

	// Alice creates and signs a message
	msg, _ := messaging.NewRequest(alice.DID(), bob.DID(), "greet", map[string]string{"text": "Hello Bob!"})

	// Sign message (as agent.Send() would do)
	// Note: msg.Signature would normally be set using Alice's signing key
	_ = msg // Suppress unused warning - this demonstrates the message creation flow

	// Verify the flow works end-to-end with proper keys
	aliceKeys, _ := crypto.GenerateAgentKeys()
	aliceRecord := registry.NewAgent(alice.DID(), "Alice")
	aliceRecord.AddPublicKey("signing", registry.KeyTypeEd25519, aliceKeys.Signing.PublicKey, "signing")
	bob.Store().Put(aliceRecord)

	// Sign with Alice's actual key
	msgForSign, _ := messaging.NewRequest(alice.DID(), bob.DID(), "greet", map[string]string{"text": "Hello!"})
	msgBytes, _ := json.Marshal(msgForSign)
	msgForSign.Signature = aliceKeys.Signing.Sign(msgBytes)

	// Verify signature using Bob's stored key
	signingKey := aliceRecord.GetSigningKey()
	if signingKey == nil {
		t.Fatal("signing key should exist")
	}

	msgCopy := msgForSign.Clone()
	msgCopy.Signature = nil
	msgBytesForVerify, _ := json.Marshal(msgCopy)

	if !crypto.VerifySignature(signingKey.Key, msgBytesForVerify, msgForSign.Signature) {
		t.Error("signature verification should succeed")
	}
}

// TestEndToEndEncryptionFlow tests the complete encryption/decryption flow.
func TestEndToEndEncryptionFlow(t *testing.T) {
	// Create key pairs for Alice and Bob
	aliceKeys, _ := crypto.GenerateAgentKeys()
	bobKeys, _ := crypto.GenerateAgentKeys()

	// Simulate the agent records with their encryption keys
	aliceRecord := registry.NewAgent("did:wba:example.com:agent:alice", "Alice")
	aliceRecord.AddPublicKey("encryption", registry.KeyTypeX25519, aliceKeys.Encryption.PublicKey, "encryption")

	bobRecord := registry.NewAgent("did:wba:example.com:agent:bob", "Bob")
	bobRecord.AddPublicKey("encryption", registry.KeyTypeX25519, bobKeys.Encryption.PublicKey, "encryption")

	// Alice encrypts a message for Bob
	plaintext := []byte(`{"secret": "top secret message"}`)

	// Get Bob's encryption key from his record
	bobEncKey := bobRecord.GetEncryptionKey()
	if bobEncKey == nil {
		t.Fatal("Bob's encryption key should exist")
	}

	// Alice encrypts using her private key and Bob's public key
	ciphertext, err := crypto.Encrypt(plaintext, aliceKeys.Encryption.PrivateKey, bobEncKey.Key)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	// Ciphertext should be different from plaintext
	if bytes.Equal(plaintext, ciphertext) {
		t.Error("ciphertext should differ from plaintext")
	}

	// Bob decrypts using his private key and Alice's public key
	aliceEncKey := aliceRecord.GetEncryptionKey()
	decrypted, err := crypto.Decrypt(ciphertext, bobKeys.Encryption.PrivateKey, aliceEncKey.Key)
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("decrypted message doesn't match: got %q, want %q", decrypted, plaintext)
	}
}

// TestEncryptionPreventsEavesdropping tests that Eve cannot decrypt Alice-Bob messages.
func TestEncryptionPreventsEavesdropping(t *testing.T) {
	aliceKeys, _ := crypto.GenerateAgentKeys()
	bobKeys, _ := crypto.GenerateAgentKeys()
	eveKeys, _ := crypto.GenerateAgentKeys()

	plaintext := []byte("secret message between Alice and Bob")

	// Alice encrypts for Bob
	ciphertext, _ := crypto.Encrypt(plaintext, aliceKeys.Encryption.PrivateKey, bobKeys.Encryption.PublicKey)

	// Eve intercepts and tries to decrypt
	_, err := crypto.Decrypt(ciphertext, eveKeys.Encryption.PrivateKey, aliceKeys.Encryption.PublicKey)
	if err == nil {
		t.Error("Eve should not be able to decrypt message intended for Bob")
	}
}

// TestSignaturePreventsImpersonation tests that Eve cannot create valid signatures as Alice.
func TestSignaturePreventsImpersonation(t *testing.T) {
	aliceKeys, _ := crypto.GenerateAgentKeys()
	eveKeys, _ := crypto.GenerateAgentKeys()

	// Eve creates a message pretending to be Alice
	fakeMsg := []byte(`{"from": "alice", "text": "send money to eve"}`)

	// Eve signs with her key
	eveSignature := eveKeys.Signing.Sign(fakeMsg)

	// Verification with Alice's key fails
	if crypto.VerifySignature(aliceKeys.Signing.PublicKey, fakeMsg, eveSignature) {
		t.Error("Eve's signature should not verify with Alice's key")
	}
}

// TestSignaturePreventsMessageTampering tests that tampering invalidates signature.
func TestSignaturePreventsMessageTampering(t *testing.T) {
	aliceKeys, _ := crypto.GenerateAgentKeys()

	originalMsg := []byte(`{"amount": 100, "to": "bob"}`)
	signature := aliceKeys.Signing.Sign(originalMsg)

	// Verify original
	if !crypto.VerifySignature(aliceKeys.Signing.PublicKey, originalMsg, signature) {
		t.Error("original message should verify")
	}

	// Tamper with message
	tamperedMsg := []byte(`{"amount": 10000, "to": "eve"}`)

	// Tampered message should not verify
	if crypto.VerifySignature(aliceKeys.Signing.PublicKey, tamperedMsg, signature) {
		t.Error("tampered message should not verify")
	}
}

// TestACLBlocksUnauthorizedAccess tests that ACL properly blocks unauthorized methods.
func TestACLBlocksUnauthorizedAccess(t *testing.T) {
	enforcer := security.NewACLEnforcer()

	// Create an agent with strict ACL
	bob := registry.NewAgent("did:wba:example.com:agent:bob", "Bob")
	bob.ACL = security.NewPolicyBuilder().
		SetDefaultAllow(false).
		Allow("did:wba:example.com:agent:alice", "read.*").
		Deny("*", "admin.*").
		Build()

	tests := []struct {
		name      string
		principal string
		method    string
		wantErr   bool
	}{
		{"alice can read", "did:wba:example.com:agent:alice", "read.file", false},
		{"alice cannot admin", "did:wba:example.com:agent:alice", "admin.delete", true},
		{"eve cannot read", "did:wba:example.com:agent:eve", "read.file", true},
		{"eve cannot admin", "did:wba:example.com:agent:eve", "admin.delete", true},
		{"wildcard admin blocked", "did:wba:evil.com:agent:hacker", "admin.shutdown", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := enforcer.CheckAccess(bob, tt.principal, tt.method)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckAccess() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestMessageClonePreservesData tests that Clone() correctly copies all fields.
func TestMessageClonePreservesData(t *testing.T) {
	msg, _ := messaging.NewRequest("from", "to", "method", map[string]string{"key": "value"})
	msg.Signature = []byte("test-signature")
	msg.Encrypted = true
	msg.TraceID = "trace-123"

	clone := msg.Clone()

	// Verify all fields are copied
	if clone.ID != msg.ID {
		t.Error("ID should match")
	}
	if clone.From != msg.From {
		t.Error("From should match")
	}
	if clone.To != msg.To {
		t.Error("To should match")
	}
	if clone.Method != msg.Method {
		t.Error("Method should match")
	}
	if !bytes.Equal(clone.Body, msg.Body) {
		t.Error("Body should match")
	}
	if !bytes.Equal(clone.Signature, msg.Signature) {
		t.Error("Signature should match")
	}
	if clone.Encrypted != msg.Encrypted {
		t.Error("Encrypted flag should match")
	}
	if clone.TraceID != msg.TraceID {
		t.Error("TraceID should match")
	}

	// Verify modifying clone doesn't affect original
	clone.Body[0] = 'X'
	clone.Signature[0] = 'Y'
	if bytes.Equal(clone.Body, msg.Body) {
		t.Error("Clone body modification should not affect original")
	}
	if bytes.Equal(clone.Signature, msg.Signature) {
		t.Error("Clone signature modification should not affect original")
	}
}

// TestAgentKeyDistribution verifies that agents properly share public keys.
func TestAgentKeyDistribution(t *testing.T) {
	alice, _ := agent.New(agent.Config{Domain: "test.com", AgentID: "alice"})
	bob, _ := agent.New(agent.Config{Domain: "test.com", AgentID: "bob"})

	// Verify Alice's record has both keys
	aliceRecord := alice.Record()
	if aliceRecord.GetSigningKey() == nil {
		t.Error("Alice should have signing key")
	}
	if aliceRecord.GetEncryptionKey() == nil {
		t.Error("Alice should have encryption key")
	}

	// Bob receives Alice's record (discovery)
	bob.Store().Put(aliceRecord)

	// Bob can retrieve Alice's keys
	storedAlice, _ := bob.Store().GetByDID(alice.DID())
	if storedAlice.GetSigningKey() == nil {
		t.Error("Stored Alice should have signing key")
	}
	if storedAlice.GetEncryptionKey() == nil {
		t.Error("Stored Alice should have encryption key")
	}

	// Keys should match
	if !bytes.Equal(aliceRecord.GetSigningKey().Key, storedAlice.GetSigningKey().Key) {
		t.Error("Signing keys should match")
	}
	if !bytes.Equal(aliceRecord.GetEncryptionKey().Key, storedAlice.GetEncryptionKey().Key) {
		t.Error("Encryption keys should match")
	}
}
