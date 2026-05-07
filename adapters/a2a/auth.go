// Package a2a provides A2A protocol compatibility.
package a2a

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gianlucamazza/msg2agent/pkg/billing"
)

// OAuth2 validation errors.
var (
	ErrMissingToken     = errors.New("missing authorization token")
	ErrInvalidToken     = errors.New("invalid token format")
	ErrTokenExpired     = errors.New("token has expired")
	ErrInvalidIssuer    = errors.New("invalid token issuer")
	ErrInvalidAudience  = errors.New("invalid token audience")
	ErrInvalidSignature = errors.New("invalid token signature")
	ErrJWKSFetchFailed  = errors.New("failed to fetch JWKS")
	ErrKeyNotFound      = errors.New("signing key not found in JWKS")
)

// OAuth2Config configures the OAuth2 validator.
type OAuth2Config struct {
	// JWKSURL is the URL to fetch JSON Web Key Set (for signature verification)
	JWKSURL string

	// Issuer is the expected token issuer (iss claim)
	Issuer string

	// Audience is the expected token audience (aud claim)
	Audience string

	// RequiredScopes are scopes that must be present in the token
	RequiredScopes []string

	// SkipValidation disables token validation (for testing)
	SkipValidation bool
}

// DefaultGoogleOAuth2Config returns a config for Google OAuth2/OpenID Connect.
func DefaultGoogleOAuth2Config(audience string) OAuth2Config {
	return OAuth2Config{
		JWKSURL:  "https://www.googleapis.com/oauth2/v3/certs",
		Issuer:   "https://accounts.google.com",
		Audience: audience,
	}
}

// Claims represents the claims in a JWT token.
type Claims struct {
	Subject   string   `json:"sub"`
	Issuer    string   `json:"iss"`
	Audience  []string `json:"aud"`
	Email     string   `json:"email"`
	ExpiresAt int64    `json:"exp"`
	IssuedAt  int64    `json:"iat"`
	Scopes    []string `json:"scope,omitempty"`

	// Raw claims map for custom claims
	Raw map[string]any `json:"-"`
}

// UnmarshalJSON implements custom JSON unmarshaling for Claims.
func (c *Claims) UnmarshalJSON(data []byte) error {
	// Unmarshal into raw map first
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	c.Raw = raw

	// Extract known fields
	if sub, ok := raw["sub"].(string); ok {
		c.Subject = sub
	}
	if iss, ok := raw["iss"].(string); ok {
		c.Issuer = iss
	}
	if email, ok := raw["email"].(string); ok {
		c.Email = email
	}
	if exp, ok := raw["exp"].(float64); ok {
		c.ExpiresAt = int64(exp)
	}
	if iat, ok := raw["iat"].(float64); ok {
		c.IssuedAt = int64(iat)
	}

	// Handle audience (can be string or array)
	switch aud := raw["aud"].(type) {
	case string:
		c.Audience = []string{aud}
	case []any:
		for _, a := range aud {
			if s, ok := a.(string); ok {
				c.Audience = append(c.Audience, s)
			}
		}
	}

	// Handle scope (can be string or array)
	switch scope := raw["scope"].(type) {
	case string:
		c.Scopes = strings.Split(scope, " ")
	case []any:
		for _, s := range scope {
			if str, ok := s.(string); ok {
				c.Scopes = append(c.Scopes, str)
			}
		}
	}

	return nil
}

// OAuth2Validator validates OAuth2/OIDC tokens.
type OAuth2Validator struct {
	config OAuth2Config

	// JWKS cache
	jwks    *JWKS
	jwksMu  sync.RWMutex
	jwksExp time.Time
	jwksTTL time.Duration
	client  *http.Client
}

// JWKS represents a JSON Web Key Set.
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// JWK represents a JSON Web Key. Supports RSA (RS256) and OKP/Ed25519 (EdDSA).
type JWK struct {
	Kid string `json:"kid"`           // Key ID
	Kty string `json:"kty"`           // Key Type ("RSA" or "OKP")
	Alg string `json:"alg"`           // Algorithm ("RS256" or "EdDSA")
	Use string `json:"use"`           // Key Use (sig)
	N   string `json:"n,omitempty"`   // RSA modulus (base64url)
	E   string `json:"e,omitempty"`   // RSA exponent (base64url)
	Crv string `json:"crv,omitempty"` // OKP curve ("Ed25519")
	X   string `json:"x,omitempty"`   // OKP public key (base64url)
}

// NewOAuth2Validator creates a new OAuth2 token validator.
func NewOAuth2Validator(cfg OAuth2Config) *OAuth2Validator {
	return &OAuth2Validator{
		config:  cfg,
		jwksTTL: 1 * time.Hour,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ValidateToken validates an OAuth2 bearer token and returns the claims.
func (v *OAuth2Validator) ValidateToken(token string) (*Claims, error) {
	if v.config.SkipValidation {
		// Return dummy claims for testing
		return &Claims{
			Subject: "test-user",
			Issuer:  v.config.Issuer,
		}, nil
	}

	// Parse JWT
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}

	// Decode header
	header, err := base64URLDecode(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid header", ErrInvalidToken)
	}

	var headerMap map[string]any
	if err := json.Unmarshal(header, &headerMap); err != nil {
		return nil, fmt.Errorf("%w: malformed header", ErrInvalidToken)
	}

	kid, _ := headerMap["kid"].(string)

	// Decode payload
	payload, err := base64URLDecode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid payload", ErrInvalidToken)
	}

	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("%w: malformed payload", ErrInvalidToken)
	}

	// Validate expiration
	if claims.ExpiresAt > 0 && time.Now().Unix() > claims.ExpiresAt {
		return nil, ErrTokenExpired
	}

	// Validate issuer
	if v.config.Issuer != "" && claims.Issuer != v.config.Issuer {
		// Google can issue tokens with or without trailing slash
		if claims.Issuer != v.config.Issuer && claims.Issuer != "accounts.google.com" {
			return nil, ErrInvalidIssuer
		}
	}

	// Validate audience
	if v.config.Audience != "" {
		found := false
		for _, aud := range claims.Audience {
			if aud == v.config.Audience {
				found = true
				break
			}
		}
		if !found {
			return nil, ErrInvalidAudience
		}
	}

	// Validate required scopes
	for _, required := range v.config.RequiredScopes {
		found := false
		for _, scope := range claims.Scopes {
			if scope == required {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("missing required scope: %s", required)
		}
	}

	// Verify signature
	if v.config.JWKSURL != "" {
		alg, _ := headerMap["alg"].(string)
		if err := v.verifySignature(parts[0]+"."+parts[1], parts[2], kid, alg); err != nil {
			return nil, err
		}
	}

	return &claims, nil
}

// verifySignature verifies the JWT signature using JWKS. Supports RS256 (RSA)
// and EdDSA (Ed25519); the algorithm is selected by the JWT header `alg` and
// the JWK `kty` (RSA → RS256, OKP → EdDSA).
func (v *OAuth2Validator) verifySignature(signingInput, signature, kid, alg string) error {
	// Get JWKS (with caching)
	jwks, err := v.getJWKS()
	if err != nil {
		return err
	}

	// Find the key
	var key *JWK
	for i := range jwks.Keys {
		if jwks.Keys[i].Kid == kid || kid == "" {
			key = &jwks.Keys[i]
			break
		}
	}
	if key == nil {
		return ErrKeyNotFound
	}

	// Decode signature
	sig, err := base64URLDecode(signature)
	if err != nil {
		return fmt.Errorf("%w: invalid signature encoding", ErrInvalidSignature)
	}

	switch {
	case alg == "EdDSA" || key.Kty == "OKP":
		pubKey, err := jwkToEd25519PublicKey(key)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidSignature, err)
		}
		if !ed25519.Verify(pubKey, []byte(signingInput), sig) {
			return ErrInvalidSignature
		}
	case alg == "RS256" || key.Kty == "RSA":
		pubKey, err := jwkToRSAPublicKey(key)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidSignature, err)
		}
		if err := verifyRS256([]byte(signingInput), sig, pubKey); err != nil {
			return ErrInvalidSignature
		}
	default:
		return fmt.Errorf("%w: unsupported alg %q / kty %q", ErrInvalidSignature, alg, key.Kty)
	}

	return nil
}

// getJWKS fetches and caches the JWKS.
func (v *OAuth2Validator) getJWKS() (*JWKS, error) {
	v.jwksMu.RLock()
	if v.jwks != nil && time.Now().Before(v.jwksExp) {
		jwks := v.jwks
		v.jwksMu.RUnlock()
		return jwks, nil
	}
	v.jwksMu.RUnlock()

	// Fetch fresh JWKS
	v.jwksMu.Lock()
	defer v.jwksMu.Unlock()

	// Double-check after acquiring write lock
	if v.jwks != nil && time.Now().Before(v.jwksExp) {
		return v.jwks, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.config.JWKSURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrJWKSFetchFailed, err)
	}

	resp, err := v.client.Do(req) //nolint:gosec // URL from trusted configuration
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrJWKSFetchFailed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrJWKSFetchFailed, resp.StatusCode)
	}

	var jwks JWKS
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrJWKSFetchFailed, err)
	}

	v.jwks = &jwks
	v.jwksExp = time.Now().Add(v.jwksTTL)

	return &jwks, nil
}

// Middleware returns an HTTP middleware that validates OAuth2 tokens.
func (v *OAuth2Validator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip validation for agent card and health endpoints
		if r.URL.Path == "/.well-known/agent.json" || r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		// Extract token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "missing authorization header", http.StatusUnauthorized)
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
			http.Error(w, "invalid authorization header format", http.StatusUnauthorized)
			return
		}

		token := parts[1]

		// Validate token
		claims, err := v.ValidateToken(token)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		// Add claims to request context
		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ClaimsFromContext extracts claims from the request context.
func ClaimsFromContext(ctx context.Context) *Claims {
	claims, _ := ctx.Value(claimsContextKey).(*Claims)
	return claims
}

type contextKey string

const claimsContextKey contextKey = "oauth2_claims"

// billingValidatorAdapter wraps OAuth2Validator to satisfy billing.JWTValidator
// without the billing package needing to import adapters/a2a.
type billingValidatorAdapter struct {
	v *OAuth2Validator
}

// NewBillingValidator wraps an OAuth2Validator so it satisfies billing.JWTValidator.
// Use this when passing an a2a validator to billing.OAuth2Middleware.
func NewBillingValidator(v *OAuth2Validator) billingValidatorAdapter {
	return billingValidatorAdapter{v: v}
}

// ValidateTokenToBillingClaims implements billing.JWTValidator.
func (a billingValidatorAdapter) ValidateTokenToBillingClaims(token string) (*billing.OAuthClaims, error) {
	c, err := a.v.ValidateToken(token)
	if err != nil {
		return nil, err
	}
	return &billing.OAuthClaims{
		Subject: c.Subject,
		Issuer:  c.Issuer,
		Email:   c.Email,
	}, nil
}

// base64URLDecode decodes a base64url encoded string (without padding).
func base64URLDecode(s string) ([]byte, error) {
	// Add padding if needed
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.URLEncoding.DecodeString(s)
}

// jwkToRSAPublicKey converts a JWK to an RSA public key.
func jwkToRSAPublicKey(jwk *JWK) (*rsa.PublicKey, error) {
	if jwk.Kty != "RSA" {
		return nil, fmt.Errorf("unsupported key type: %s", jwk.Kty)
	}

	// Decode modulus
	nBytes, err := base64URLDecode(jwk.N)
	if err != nil {
		return nil, fmt.Errorf("invalid modulus: %w", err)
	}
	n := new(big.Int).SetBytes(nBytes)

	// Decode exponent
	eBytes, err := base64URLDecode(jwk.E)
	if err != nil {
		return nil, fmt.Errorf("invalid exponent: %w", err)
	}
	e := new(big.Int).SetBytes(eBytes)

	return &rsa.PublicKey{
		N: n,
		E: int(e.Int64()),
	}, nil
}

// verifyRS256 verifies an RS256 (RSASSA-PKCS1-v1_5 with SHA-256) signature.
func verifyRS256(message, signature []byte, pubKey *rsa.PublicKey) error {
	hash := sha256.Sum256(message)
	return rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hash[:], signature)
}

// jwkToEd25519PublicKey converts an OKP/Ed25519 JWK (RFC 8037) to an
// ed25519.PublicKey. The `x` field carries the 32-byte public key, base64url-encoded.
func jwkToEd25519PublicKey(jwk *JWK) (ed25519.PublicKey, error) {
	if jwk.Kty != "OKP" {
		return nil, fmt.Errorf("unsupported key type for Ed25519: %s", jwk.Kty)
	}
	if jwk.Crv != "" && jwk.Crv != "Ed25519" {
		return nil, fmt.Errorf("unsupported OKP curve: %s", jwk.Crv)
	}
	xBytes, err := base64URLDecode(jwk.X)
	if err != nil {
		return nil, fmt.Errorf("invalid Ed25519 public key: %w", err)
	}
	if len(xBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid Ed25519 public key length: %d", len(xBytes))
	}
	return ed25519.PublicKey(xBytes), nil
}
