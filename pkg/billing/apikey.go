package billing

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const apiKeyPrefix = "msg2a_"

// APIKey represents an issued API key (plaintext only available at creation time).
type APIKey struct {
	ID        string     `json:"id"`
	TenantID  string     `json:"tenant_id"`
	Name      string     `json:"name"`     // human label, e.g. "production"
	KeyHash   string     `json:"key_hash"` // SHA-256 of the raw key, stored in DB
	Prefix    string     `json:"prefix"`   // first 8 chars after "msg2a_", for display
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
func GenerateAPIKey(tenantID, name string) (plaintext string, record *APIKey, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("failed to generate API key entropy: %w", err)
	}

	encoded := base64.RawURLEncoding.EncodeToString(raw)
	plaintext = apiKeyPrefix + encoded

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
// Use this to look up keys in the store without storing the plaintext.
func HashAPIKey(plaintext string) (string, error) {
	if !strings.HasPrefix(plaintext, apiKeyPrefix) {
		return "", ErrInvalidAPIKey
	}
	return hashKey(plaintext), nil
}

func hashKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
