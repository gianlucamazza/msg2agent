package oauth

import (
	"crypto/ed25519"
	"fmt"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

const (
	// AccessTokenTTL is the access token lifetime.
	AccessTokenTTL = time.Hour
	// RefreshTokenTTL is the refresh token lifetime.
	RefreshTokenTTL = 30 * 24 * time.Hour
	// SessionCookieTTL is the consent-screen session lifetime.
	SessionCookieTTL = 10 * time.Minute
)

// JWTIssuer signs JWT access tokens and session cookies.
type JWTIssuer struct {
	priv   ed25519.PrivateKey
	kid    string
	issuer string
}

// NewJWTIssuer creates a JWTIssuer. issuer should be the base URL (e.g. "https://msg2agent.example.com").
func NewJWTIssuer(priv ed25519.PrivateKey, kid, issuer string) *JWTIssuer {
	return &JWTIssuer{priv: priv, kid: kid, issuer: issuer}
}

// IssueAccessToken mints an EdDSA-signed JWT access token.
func (j *JWTIssuer) IssueAccessToken(tenantID, clientID, scope, audience, jti string) (string, error) {
	now := time.Now().UTC()
	tok, err := jwt.NewBuilder().
		Issuer(j.issuer).
		Subject(tenantID).
		Audience([]string{audience}).
		IssuedAt(now).
		Expiration(now.Add(AccessTokenTTL)).
		JwtID(jti).
		Claim("client_id", clientID).
		Claim("scope", scope).
		Build()
	if err != nil {
		return "", fmt.Errorf("oauth: build access token: %w", err)
	}
	priv, err := privJWK(j.priv, j.kid)
	if err != nil {
		return "", err
	}
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.EdDSA, priv))
	if err != nil {
		return "", fmt.Errorf("oauth: sign access token: %w", err)
	}
	return string(signed), nil
}

// IssueSessionCookie mints a short-lived JWT to carry tenant_id across the consent redirect.
func (j *JWTIssuer) IssueSessionCookie(tenantID string) (string, error) {
	now := time.Now().UTC()
	tok, err := jwt.NewBuilder().
		Issuer(j.issuer).
		Subject(tenantID).
		IssuedAt(now).
		Expiration(now.Add(SessionCookieTTL)).
		Build()
	if err != nil {
		return "", fmt.Errorf("oauth: build session token: %w", err)
	}
	priv, err := privJWK(j.priv, j.kid)
	if err != nil {
		return "", err
	}
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.EdDSA, priv))
	if err != nil {
		return "", fmt.Errorf("oauth: sign session token: %w", err)
	}
	return string(signed), nil
}

// JWTVerifier validates EdDSA JWT access tokens and resolves the tenant.
type JWTVerifier struct {
	pubKey   ed25519.PublicKey
	issuer   string
	audience string
}

// NewJWTVerifier creates a verifier for access tokens produced by JWTIssuer.
func NewJWTVerifier(priv ed25519.PrivateKey, issuer, audience string) *JWTVerifier {
	return &JWTVerifier{
		pubKey:   priv.Public().(ed25519.PublicKey),
		issuer:   issuer,
		audience: audience,
	}
}

// ValidateClaims parses and verifies a JWT access token, returning the tenant ID on success.
// The caller is responsible for loading the full Tenant from the store using the returned ID.
func (v *JWTVerifier) ValidateClaims(token string) (tenantID string, err error) {
	pub, err := pubJWK(v.pubKey)
	if err != nil {
		return "", err
	}
	tok, err := jwt.Parse([]byte(token),
		jwt.WithKey(jwa.EdDSA, pub),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithValidate(true),
	)
	if err != nil {
		return "", fmt.Errorf("oauth: invalid access token: %w", err)
	}
	tenantID = tok.Subject()
	if tenantID == "" {
		return "", fmt.Errorf("oauth: access token missing sub claim")
	}
	return tenantID, nil
}

// ValidateSessionCookie parses a session JWT and returns the tenant ID.
func (v *JWTVerifier) ValidateSessionCookie(token string) (string, error) {
	pub, err := pubJWK(v.pubKey)
	if err != nil {
		return "", err
	}
	tok, err := jwt.Parse([]byte(token),
		jwt.WithKey(jwa.EdDSA, pub),
		jwt.WithIssuer(v.issuer),
		jwt.WithValidate(true),
	)
	if err != nil {
		return "", fmt.Errorf("oauth: invalid session cookie: %w", err)
	}
	return tok.Subject(), nil
}

// privJWK wraps an Ed25519 private key as a jwk.Key with the given kid.
func privJWK(priv ed25519.PrivateKey, kid string) (interface{}, error) {
	k, err := jwk.FromRaw(priv)
	if err != nil {
		return nil, fmt.Errorf("oauth: build private JWK: %w", err)
	}
	if err := k.Set(jwk.KeyIDKey, kid); err != nil {
		return nil, fmt.Errorf("oauth: set kid on private JWK: %w", err)
	}
	return k, nil
}

// pubJWK wraps an Ed25519 public key as a jwk.Key.
func pubJWK(pub ed25519.PublicKey) (interface{}, error) {
	k, err := jwk.FromRaw(pub)
	if err != nil {
		return nil, fmt.Errorf("oauth: build public JWK: %w", err)
	}
	return k, nil
}
