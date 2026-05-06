package billing

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	apiKeyPrefixLive   = "sk_live_"
	apiKeyPrefixTest   = "sk_test_"
	apiKeyPrefixLegacy = "msg2a_"
)

// keyEnvPrefix returns the prefix for new API keys based on MSG2AGENT_ENV.
// "test", "dev", "development" → sk_test_; everything else → sk_live_.
func keyEnvPrefix() string {
	switch strings.ToLower(os.Getenv("MSG2AGENT_ENV")) {
	case "test", "development", "dev":
		return apiKeyPrefixTest
	default:
		return apiKeyPrefixLive
	}
}

// APIKey represents an issued API key (plaintext only available at creation time).
type APIKey struct {
	ID        string     `json:"id"`
	TenantID  string     `json:"tenant_id"`
	Name      string     `json:"name"`
	KeyHash   string     `json:"key_hash"`
	Prefix    string     `json:"prefix"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// IsValid returns true if the key has not been revoked and is not expired.
func (k *APIKey) IsValid() bool {
	if k.RevokedAt != nil {
		return false
	}
	if k.ExpiresAt != nil && time.Now().After(*k.ExpiresAt) {
		return false
	}
	return true
}

// GenerateAPIKey generates a new API key and returns the plaintext value and the
// APIKey record (which stores only the hash — never store the plaintext).
// The prefix is determined by MSG2AGENT_ENV: "test"|"dev" → sk_test_, default → sk_live_.
func GenerateAPIKey(tenantID, name string) (plaintext string, record *APIKey, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("failed to generate API key entropy: %w", err)
	}

	encoded := base64.RawURLEncoding.EncodeToString(raw)
	plaintext = keyEnvPrefix() + encoded

	hash := hashKey(plaintext)
	if len(encoded) < 8 {
		return "", nil, fmt.Errorf("billing: encoded key too short (%d chars)", len(encoded))
	}
	prefix := encoded[:8]

	now := time.Now().UTC()
	record = &APIKey{
		ID:        newID("k"),
		TenantID:  tenantID,
		Name:      name,
		KeyHash:   hash,
		Prefix:    prefix,
		CreatedAt: now,
	}
	return plaintext, record, nil
}

// HashAPIKey returns the SHA-256 hex digest of a plaintext API key.
// Accepts sk_live_, sk_test_, and the legacy msg2a_ prefix.
func HashAPIKey(plaintext string) (string, error) {
	switch {
	case strings.HasPrefix(plaintext, apiKeyPrefixLive),
		strings.HasPrefix(plaintext, apiKeyPrefixTest),
		strings.HasPrefix(plaintext, apiKeyPrefixLegacy):
		return hashKey(plaintext), nil
	}
	return "", ErrInvalidAPIKey
}

func hashKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
