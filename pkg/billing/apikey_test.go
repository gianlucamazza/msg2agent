package billing

import (
	"strings"
	"testing"
)

func TestGenerateAPIKey(t *testing.T) {
	plaintext, record, err := GenerateAPIKey("t_001", "production")
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if !strings.HasPrefix(plaintext, apiKeyPrefixLive) {
		t.Errorf("plaintext %q missing prefix %q", plaintext, apiKeyPrefixLive)
	}
	if record.TenantID != "t_001" {
		t.Errorf("TenantID = %q, want %q", record.TenantID, "t_001")
	}
	if record.Name != "production" {
		t.Errorf("Name = %q, want %q", record.Name, "production")
	}
	if record.KeyHash == "" {
		t.Error("KeyHash is empty")
	}
	if len(record.Prefix) < 8 {
		t.Errorf("Prefix too short: %q", record.Prefix)
	}
	if record.RevokedAt != nil {
		t.Error("new key should not be revoked")
	}
}

func TestHashAPIKey_deterministic(t *testing.T) {
	plaintext, record, err := GenerateAPIKey("t_001", "test")
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	hash, err := HashAPIKey(plaintext)
	if err != nil {
		t.Fatalf("HashAPIKey: %v", err)
	}
	if hash != record.KeyHash {
		t.Errorf("hash mismatch: got %q want %q", hash, record.KeyHash)
	}
	// Second call must produce same hash.
	hash2, err := HashAPIKey(plaintext)
	if err != nil {
		t.Fatalf("HashAPIKey (second call): %v", err)
	}
	if hash != hash2 {
		t.Errorf("hash not deterministic: %q vs %q", hash, hash2)
	}
}

func TestHashAPIKey_invalidPrefix(t *testing.T) {
	_, err := HashAPIKey("sk_invalid_prefix")
	if err == nil {
		t.Error("expected error for invalid prefix, got nil")
	}
}

func TestAPIKey_IsValid(t *testing.T) {
	_, record, _ := GenerateAPIKey("t_001", "test")
	if !record.IsValid() {
		t.Error("new key should be valid")
	}
	now := record.CreatedAt
	record.RevokedAt = &now
	if record.IsValid() {
		t.Error("revoked key should be invalid")
	}
}

func TestGenerateAPIKey_uniqueness(t *testing.T) {
	k1, _, _ := GenerateAPIKey("t_001", "a")
	k2, _, _ := GenerateAPIKey("t_001", "b")
	if k1 == k2 {
		t.Error("two generated keys should not be equal")
	}
}

func TestHashAPIKey_allPrefixes(t *testing.T) {
	cases := []struct {
		key string
	}{
		{"sk_live_testonly"},
		{"sk_test_testonly"},
		{"msg2a_testonly"},
	}
	for _, tc := range cases {
		hash, err := HashAPIKey(tc.key)
		if err != nil {
			t.Errorf("HashAPIKey(%q): unexpected error: %v", tc.key, err)
		}
		if hash == "" {
			t.Errorf("HashAPIKey(%q): empty hash", tc.key)
		}
	}
}

func TestGenerateAPIKey_envPrefix(t *testing.T) {
	t.Setenv("MSG2AGENT_ENV", "test")
	plaintext, _, err := GenerateAPIKey("t_001", "env-test")
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if !strings.HasPrefix(plaintext, apiKeyPrefixTest) {
		t.Errorf("expected sk_test_ prefix, got %q", plaintext)
	}
}
