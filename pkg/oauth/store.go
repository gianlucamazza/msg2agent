package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// Sentinel errors.
var (
	ErrClientNotFound       = errors.New("oauth: client not found")
	ErrCodeNotFound         = errors.New("oauth: authorization code not found")
	ErrCodeExpiredOrUsed    = errors.New("oauth: authorization code expired or already used")
	ErrRefreshTokenNotFound = errors.New("oauth: refresh token not found")
	ErrRefreshTokenRevoked  = errors.New("oauth: refresh token revoked or expired")
)

// Client is a registered OAuth 2.0 client (created via DCR).
type Client struct {
	ClientID                string   `json:"client_id"`
	ClientSecretHash        string   `json:"client_secret_hash,omitempty"` // empty for public clients
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	Scope                   string   `json:"scope,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientSecretExpiresAt   int64    `json:"client_secret_expires_at"` // 0 = never
	CreatedIP               string   `json:"created_ip,omitempty"`
}

// Code is a single-use authorization code.
type Code struct {
	CodeHash            string // sha256(plaintext code), stored; plaintext returned once
	ClientID            string
	TenantID            string
	RedirectURI         string
	CodeChallenge       string
	CodeChallengeMethod string // always "S256"
	Scope               string
	ExpiresAt           time.Time
	Used                bool
}

// RefreshToken is a persisted (hashed) refresh token.
type RefreshToken struct {
	TokenHash string // sha256(plaintext token)
	ClientID  string
	TenantID  string
	Scope     string
	ExpiresAt time.Time
	Revoked   bool
}

// Store persists OAuth clients, codes, and refresh tokens.
// Implementations must be goroutine-safe.
type Store interface {
	// Client operations
	PutClient(c *Client) error
	GetClient(clientID string) (*Client, error)

	// Authorization code operations
	PutCode(code *Code) error
	// UseCode atomically looks up the code by its hash, verifies it is unused and
	// not expired, marks it used, and returns it.
	UseCode(codeHash string) (*Code, error)

	// Refresh token operations
	PutRefreshToken(rt *RefreshToken) error
	// RotateRefreshToken atomically marks the old token revoked and stores the new one.
	// Returns ErrRefreshTokenNotFound or ErrRefreshTokenRevoked if the old hash is invalid.
	RotateRefreshToken(oldHash string, newRT *RefreshToken) (*RefreshToken, error)
	// RevokeRefreshToken marks the token with the given hash as revoked (RFC 7009).
	// Returns nil if the token does not exist (idempotent).
	RevokeRefreshToken(hash string) error

	// Cleanup removes expired codes and tokens.
	CleanupOAuthExpired() error
}

// HashToken returns SHA-256(plaintext) as a lowercase hex string.
func HashToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// GenerateToken returns a cryptographically random URL-safe token and its hash.
func GenerateToken(n int) (plaintext, hash string, err error) {
	b := make([]byte, n)
	if _, err = rand.Read(b); err != nil {
		return "", "", fmt.Errorf("oauth: generate token: %w", err)
	}
	plaintext = hex.EncodeToString(b) // 2n hex chars, URL-safe
	hash = HashToken(plaintext)
	return plaintext, hash, nil
}
