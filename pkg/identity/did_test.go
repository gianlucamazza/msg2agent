package identity

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewIdentity tests identity creation with generated keys.
func TestNewIdentity(t *testing.T) {
	identity, err := NewIdentity("example.com", "agent123")
	if err != nil {
		t.Fatalf("NewIdentity failed: %v", err)
	}

	if identity == nil {
		t.Fatal("NewIdentity returned nil")
	}

	// Check DID format
	didStr := identity.DID.String()
	if !strings.HasPrefix(didStr, "did:wba:example.com:agent:agent123") {
		t.Errorf("DID = %q, want prefix did:wba:example.com:agent:agent123", didStr)
	}

	// Check keys are generated
	if identity.Keys == nil {
		t.Fatal("Keys should not be nil")
	}
	if identity.Keys.Signing == nil {
		t.Error("Signing key should not be nil")
	}
	if identity.Keys.Encryption == nil {
		t.Error("Encryption key should not be nil")
	}

	// Check document is built
	if identity.Document == nil {
		t.Fatal("Document should not be nil")
	}
}

// TestNewIdentityEncodedDomain tests identity creation with URL-encoded domain.
func TestNewIdentityEncodedDomain(t *testing.T) {
	// Domain with port
	identity, err := NewIdentity("localhost%3A8080", "test")
	if err != nil {
		t.Fatalf("NewIdentity with encoded domain failed: %v", err)
	}

	didStr := identity.String()
	if !strings.Contains(didStr, "localhost%3A8080") {
		t.Errorf("DID should contain encoded domain: %s", didStr)
	}
}

// TestIdentityString tests the String method.
func TestIdentityString(t *testing.T) {
	identity, err := NewIdentity("test.org", "myagent")
	if err != nil {
		t.Fatalf("NewIdentity failed: %v", err)
	}

	str := identity.String()
	if str != identity.DID.String() {
		t.Errorf("String() = %q, want %q", str, identity.DID.String())
	}
}

// TestIdentitySigningPublicKey tests retrieving the signing public key.
func TestIdentitySigningPublicKey(t *testing.T) {
	identity, err := NewIdentity("example.com", "agent1")
	if err != nil {
		t.Fatalf("NewIdentity failed: %v", err)
	}

	pubKey := identity.SigningPublicKey()
	if len(pubKey) != 32 { // Ed25519 public key is 32 bytes
		t.Errorf("SigningPublicKey length = %d, want 32", len(pubKey))
	}

	// Key should match what's in Keys
	if string(pubKey) != string(identity.Keys.Signing.PublicKey) {
		t.Error("SigningPublicKey should match Keys.Signing.PublicKey")
	}
}

// TestIdentityEncryptionPublicKey tests retrieving the encryption public key.
func TestIdentityEncryptionPublicKey(t *testing.T) {
	identity, err := NewIdentity("example.com", "agent1")
	if err != nil {
		t.Fatalf("NewIdentity failed: %v", err)
	}

	pubKey := identity.EncryptionPublicKey()
	if len(pubKey) != 32 { // X25519 public key is 32 bytes
		t.Errorf("EncryptionPublicKey length = %d, want 32", len(pubKey))
	}

	// Key should match what's in Keys
	if string(pubKey) != string(identity.Keys.Encryption.PublicKey) {
		t.Error("EncryptionPublicKey should match Keys.Encryption.PublicKey")
	}
}

// TestIdentitySign tests signing data.
func TestIdentitySign(t *testing.T) {
	identity, err := NewIdentity("example.com", "signer")
	if err != nil {
		t.Fatalf("NewIdentity failed: %v", err)
	}

	data := []byte("message to sign")
	signature := identity.Sign(data)

	if len(signature) != 64 { // Ed25519 signature is 64 bytes
		t.Errorf("signature length = %d, want 64", len(signature))
	}

	// Verify signature using the key pair
	if !identity.Keys.Signing.Verify(data, signature) {
		t.Error("signature verification failed")
	}
}

// TestIdentitySignDifferentMessages tests that different messages produce different signatures.
func TestIdentitySignDifferentMessages(t *testing.T) {
	identity, err := NewIdentity("example.com", "signer")
	if err != nil {
		t.Fatalf("NewIdentity failed: %v", err)
	}

	sig1 := identity.Sign([]byte("message1"))
	sig2 := identity.Sign([]byte("message2"))

	if string(sig1) == string(sig2) {
		t.Error("different messages should produce different signatures")
	}
}

// TestBuildDocument tests DID document structure.
func TestBuildDocument(t *testing.T) {
	identity, err := NewIdentity("example.com", "doctest")
	if err != nil {
		t.Fatalf("NewIdentity failed: %v", err)
	}

	doc := identity.Document
	if doc == nil {
		t.Fatal("Document should not be nil")
	}

	// Check context
	if len(doc.Context) == 0 {
		t.Error("Document should have context")
	}

	// Check ID matches identity DID
	if doc.ID.String() != identity.DID.String() {
		t.Errorf("Document ID = %s, want %s", doc.ID.String(), identity.DID.String())
	}

	// Check verification methods
	if len(doc.AssertionMethod) == 0 {
		t.Error("Document should have assertion method")
	}
	if len(doc.Authentication) == 0 {
		t.Error("Document should have authentication method")
	}
	if len(doc.KeyAgreement) == 0 {
		t.Error("Document should have key agreement")
	}
}

// TestParseDID tests DID parsing.
func TestParseDID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid did:wba",
			input:   "did:wba:example.com:agent:123",
			wantErr: false,
		},
		{
			name:    "valid did:key",
			input:   "did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK",
			wantErr: false,
		},
		{
			name:    "valid did:web",
			input:   "did:web:example.com",
			wantErr: false,
		},
		{
			name:    "invalid - empty",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid - no method",
			input:   "did::example",
			wantErr: true,
		},
		{
			name:    "invalid - not a DID",
			input:   "notadid",
			wantErr: true,
		},
		{
			name:    "invalid - missing did prefix",
			input:   "wba:example.com:agent:123",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := ParseDID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseDID(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && d == nil {
				t.Errorf("ParseDID(%q) returned nil without error", tt.input)
			}
		})
	}
}

// TestValidateDID tests DID validation.
func TestValidateDID(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"did:wba:example.com:agent:123", true},
		{"did:web:example.com", true},
		{"did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK", true},
		{"", false},
		{"notadid", false},
		{"did:", false},
		{"did::", false},
		{"did:wba:", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ValidateDID(tt.input)
			if got != tt.valid {
				t.Errorf("ValidateDID(%q) = %v, want %v", tt.input, got, tt.valid)
			}
		})
	}
}

// TestExtractDomain tests domain extraction from DIDs.
func TestExtractDomain(t *testing.T) {
	tests := []struct {
		name       string
		did        string
		wantDomain string
		wantErr    error
	}{
		{
			name:       "simple domain",
			did:        "did:wba:example.com:agent:123",
			wantDomain: "example.com",
			wantErr:    nil,
		},
		{
			name:       "subdomain",
			did:        "did:wba:sub.example.com:agent:456",
			wantDomain: "sub.example.com",
			wantErr:    nil,
		},
		{
			name:       "encoded domain with port",
			did:        "did:wba:localhost%3A8080:agent:test",
			wantDomain: "localhost:8080",
			wantErr:    nil,
		},
		{
			name:       "wrong method",
			did:        "did:web:example.com",
			wantDomain: "",
			wantErr:    ErrUnsupportedMethod,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := ParseDID(tt.did)
			if err != nil {
				if tt.wantErr != nil {
					return // Expected to fail at parsing
				}
				t.Fatalf("ParseDID failed: %v", err)
			}

			domain, err := ExtractDomain(d)
			if err != tt.wantErr {
				t.Errorf("ExtractDomain() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if domain != tt.wantDomain {
				t.Errorf("ExtractDomain() = %q, want %q", domain, tt.wantDomain)
			}
		})
	}
}

// TestEncodeBase58 tests Base58 encoding.
func TestEncodeBase58(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{
			name:  "empty",
			input: []byte{},
			want:  "",
		},
		{
			name:  "single zero",
			input: []byte{0},
			want:  "1",
		},
		{
			name:  "multiple zeros",
			input: []byte{0, 0, 0},
			want:  "111",
		},
		{
			name:  "hello",
			input: []byte("hello"),
			want:  "Cn8eVZg",
		},
		{
			name:  "leading zeros",
			input: []byte{0, 0, 1, 2, 3},
			want:  "11Ldp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := encodeBase58(tt.input)
			if got != tt.want {
				t.Errorf("encodeBase58(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestEncodeBase58PublicKey tests encoding actual public key bytes.
func TestEncodeBase58PublicKey(t *testing.T) {
	identity, err := NewIdentity("example.com", "keytest")
	if err != nil {
		t.Fatalf("NewIdentity failed: %v", err)
	}

	// Encode signing key
	sigEncoded := encodeBase58(identity.SigningPublicKey())
	if len(sigEncoded) == 0 {
		t.Error("encoded signing key should not be empty")
	}

	// Encode encryption key
	encEncoded := encodeBase58(identity.EncryptionPublicKey())
	if len(encEncoded) == 0 {
		t.Error("encoded encryption key should not be empty")
	}

	// Keys should produce different encodings
	if sigEncoded == encEncoded {
		t.Error("different keys should produce different encodings")
	}
}

// TestNewIdentityUniqueness tests that each identity has unique keys.
func TestNewIdentityUniqueness(t *testing.T) {
	id1, err := NewIdentity("example.com", "agent1")
	if err != nil {
		t.Fatalf("NewIdentity 1 failed: %v", err)
	}

	id2, err := NewIdentity("example.com", "agent2")
	if err != nil {
		t.Fatalf("NewIdentity 2 failed: %v", err)
	}

	// DIDs should be different
	if id1.String() == id2.String() {
		t.Error("different agents should have different DIDs")
	}

	// Signing keys should be different
	if string(id1.SigningPublicKey()) == string(id2.SigningPublicKey()) {
		t.Error("different identities should have different signing keys")
	}

	// Encryption keys should be different
	if string(id1.EncryptionPublicKey()) == string(id2.EncryptionPublicKey()) {
		t.Error("different identities should have different encryption keys")
	}
}

// TestNewIdentitySameDomainAgent tests that same domain/agent produces same DID string format.
func TestNewIdentitySameDomainAgent(t *testing.T) {
	id1, _ := NewIdentity("example.com", "sameagent")
	id2, _ := NewIdentity("example.com", "sameagent")

	// DID strings should be same (same domain+agent)
	if id1.DID.String() != id2.DID.String() {
		t.Errorf("same domain/agent should produce same DID: %s vs %s",
			id1.DID.String(), id2.DID.String())
	}

	// But keys should still be different (randomly generated)
	if string(id1.SigningPublicKey()) == string(id2.SigningPublicKey()) {
		t.Error("keys should be unique even for same DID string")
	}
}

// TestSaveLoadRoundTrip tests that saving and loading an identity preserves keys.
func TestSaveLoadRoundTrip(t *testing.T) {
	original, err := NewIdentity("example.com", "persist-test")
	if err != nil {
		t.Fatalf("NewIdentity failed: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "test.key")

	if err := SaveToFile(original, path); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	// Check file permissions
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}

	loaded, err := LoadFromFile(path, "example.com", "persist-test")
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	// DID should match
	if original.DID.String() != loaded.DID.String() {
		t.Errorf("DID mismatch: %s vs %s", original.DID.String(), loaded.DID.String())
	}

	// Keys should match
	if !bytes.Equal(original.Keys.Signing.PrivateKey, loaded.Keys.Signing.PrivateKey) {
		t.Error("signing private key mismatch")
	}
	if !bytes.Equal(original.Keys.Signing.PublicKey, loaded.Keys.Signing.PublicKey) {
		t.Error("signing public key mismatch")
	}
	if !bytes.Equal(original.Keys.Encryption.PrivateKey, loaded.Keys.Encryption.PrivateKey) {
		t.Error("encryption private key mismatch")
	}
	if !bytes.Equal(original.Keys.Encryption.PublicKey, loaded.Keys.Encryption.PublicKey) {
		t.Error("encryption public key mismatch")
	}

	// Signing should work with loaded identity
	data := []byte("test message")
	sig := loaded.Sign(data)
	if !original.Keys.Signing.Verify(data, sig) {
		t.Error("signature from loaded identity failed verification")
	}
}

// TestLoadFromFileNotFound tests loading from a non-existent file.
func TestLoadFromFileNotFound(t *testing.T) {
	_, err := LoadFromFile("/nonexistent/path.key", "example.com", "test")
	if err == nil {
		t.Error("LoadFromFile should fail for non-existent file")
	}
}

// TestMethodWBAConstant tests the method constant.
func TestMethodWBAConstant(t *testing.T) {
	if MethodWBA != "wba" {
		t.Errorf("MethodWBA = %q, want %q", MethodWBA, "wba")
	}
}

// TestErrorTypes tests error type constants.
func TestErrorTypes(t *testing.T) {
	if ErrInvalidDID == nil {
		t.Error("ErrInvalidDID should not be nil")
	}
	if ErrUnsupportedMethod == nil {
		t.Error("ErrUnsupportedMethod should not be nil")
	}
	if ErrInvalidDID.Error() == ErrUnsupportedMethod.Error() {
		t.Error("error messages should be different")
	}
}

// TestX25519KeyAgreementKey2019 tests the key type constant.
func TestX25519KeyAgreementKey2019(t *testing.T) {
	if X25519KeyAgreementKey2019 == "" {
		t.Error("X25519KeyAgreementKey2019 should not be empty")
	}
}

// TestBase58Alphabet validates the alphabet constant.
func TestBase58Alphabet(t *testing.T) {
	if len(base58Alphabet) != 58 {
		t.Errorf("base58Alphabet length = %d, want 58", len(base58Alphabet))
	}

	// Check no ambiguous characters (0, O, I, l)
	ambiguous := "0OIl"
	for _, c := range ambiguous {
		if strings.ContainsRune(base58Alphabet, c) {
			t.Errorf("base58Alphabet should not contain %q", string(c))
		}
	}
}
