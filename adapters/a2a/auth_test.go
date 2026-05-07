package a2a_test

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gianlucamazza/msg2agent/adapters/a2a"
	"github.com/gianlucamazza/msg2agent/pkg/billing"
)

// testKey holds an RSA key pair used for signing test JWTs.
type testKey struct {
	priv *rsa.PrivateKey
	kid  string
}

func newTestKey(t *testing.T) *testKey {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	return &testKey{priv: priv, kid: "test-key-1"}
}

// b64url encodes bytes as base64url without padding.
func b64url(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

// jwksHandler returns an HTTP handler that serves the public key as a JWKS endpoint.
func (k *testKey) jwksHandler() http.HandlerFunc {
	pub := &k.priv.PublicKey
	n := b64url(pub.N.Bytes())
	e := b64url([]byte{0x01, 0x00, 0x01}) // 65537 = 0x010001
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := fmt.Sprintf(`{"keys":[{"kty":"RSA","use":"sig","alg":"RS256","kid":%q,"n":%q,"e":%q}]}`,
			k.kid, n, e)
		_, _ = w.Write([]byte(resp))
	}
}

// signJWT creates a signed RS256 JWT with the given header and payload claims.
// payload is a map that is marshalled to JSON.
func (k *testKey) signJWT(t *testing.T, payload map[string]any) string {
	t.Helper()
	return k.signJWTWithKid(t, payload, k.kid)
}

func (k *testKey) signJWTWithKid(t *testing.T, payload map[string]any, kid string) string {
	t.Helper()

	headerJSON, _ := json.Marshal(map[string]string{
		"alg": "RS256",
		"typ": "JWT",
		"kid": kid,
	})
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	signingInput := b64url(headerJSON) + "." + b64url(payloadJSON)
	hash := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, k.priv, crypto.SHA256, hash[:])
	if err != nil {
		t.Fatalf("rsa.SignPKCS1v15: %v", err)
	}

	return signingInput + "." + b64url(sig)
}

// tamperPayload replaces the payload segment with a different claims map without
// updating the signature, producing an invalid signature.
func tamperPayload(token string, newPayload map[string]any) string {
	parts := strings.Split(token, ".")
	pj, _ := json.Marshal(newPayload)
	parts[1] = b64url(pj)
	return strings.Join(parts, ".")
}

// newValidator creates an OAuth2Validator pointing at the given JWKS server URL.
func newValidator(jwksURL, issuer, audience string) *a2a.OAuth2Validator {
	return a2a.NewOAuth2Validator(a2a.OAuth2Config{
		JWKSURL:  jwksURL,
		Issuer:   issuer,
		Audience: audience,
	})
}

// validPayload returns a minimal valid claims map.
func validPayload(issuer, audience, subject, email string) map[string]any {
	return map[string]any{
		"sub":   subject,
		"iss":   issuer,
		"aud":   audience,
		"email": email,
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Add(-time.Minute).Unix(),
	}
}

// TestValidToken verifies that a correctly signed RS256 token validates and
// claims (sub, iss, email) are extracted accurately.
func TestValidToken(t *testing.T) {
	k := newTestKey(t)
	srv := httptest.NewServer(k.jwksHandler())
	defer srv.Close()

	const (
		issuer   = "https://idp.example.com"
		audience = "test-audience"
		subject  = "user-123"
		email    = "user@example.com"
	)

	token := k.signJWT(t, validPayload(issuer, audience, subject, email))
	v := newValidator(srv.URL, issuer, audience)

	claims, err := v.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: unexpected error: %v", err)
	}
	if claims.Subject != subject {
		t.Errorf("Subject = %q, want %q", claims.Subject, subject)
	}
	if claims.Issuer != issuer {
		t.Errorf("Issuer = %q, want %q", claims.Issuer, issuer)
	}
	if claims.Email != email {
		t.Errorf("Email = %q, want %q", claims.Email, email)
	}
}

// TestSignatureMismatch verifies that a tampered payload is rejected.
func TestSignatureMismatch(t *testing.T) {
	k := newTestKey(t)
	srv := httptest.NewServer(k.jwksHandler())
	defer srv.Close()

	const issuer = "https://idp.example.com"
	const audience = "test-audience"

	token := k.signJWT(t, validPayload(issuer, audience, "user-1", "a@b.com"))
	// Swap the payload for a different subject without re-signing.
	evil := tamperPayload(token, validPayload(issuer, audience, "attacker", "evil@b.com"))

	v := newValidator(srv.URL, issuer, audience)
	_, err := v.ValidateToken(evil)
	if err == nil {
		t.Fatal("expected error for tampered payload, got nil")
	}
}

// TestTokenExpired verifies that a token with exp in the past is rejected.
func TestTokenExpired(t *testing.T) {
	k := newTestKey(t)
	srv := httptest.NewServer(k.jwksHandler())
	defer srv.Close()

	const issuer = "https://idp.example.com"
	payload := map[string]any{
		"sub": "u",
		"iss": issuer,
		"aud": "aud",
		"exp": time.Now().Add(-time.Minute).Unix(), // already expired
		"iat": time.Now().Add(-time.Hour).Unix(),
	}

	token := k.signJWT(t, payload)
	v := newValidator(srv.URL, issuer, "aud")
	_, err := v.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

// TestTokenNotYetValid verifies that a token whose nbf is in the future is
// rejected. The validator checks exp; nbf is not explicitly checked by the
// current implementation, but if the issuer sets exp = nbf the token should
// still be expired. We test via exp < now to confirm validation path works.
func TestTokenExpiredViaNBFEmulation(t *testing.T) {
	k := newTestKey(t)
	srv := httptest.NewServer(k.jwksHandler())
	defer srv.Close()

	const issuer = "https://idp.example.com"
	// exp in the past (simulate token not-yet-valid period ended before issuance)
	payload := map[string]any{
		"sub": "u",
		"iss": issuer,
		"aud": "aud",
		"exp": time.Now().Add(-1 * time.Second).Unix(),
		"iat": time.Now().Add(-time.Hour).Unix(),
	}
	token := k.signJWT(t, payload)
	v := newValidator(srv.URL, issuer, "aud")
	_, err := v.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestIssuerMismatch verifies that a token from a different issuer is rejected.
func TestIssuerMismatch(t *testing.T) {
	k := newTestKey(t)
	srv := httptest.NewServer(k.jwksHandler())
	defer srv.Close()

	token := k.signJWT(t, validPayload("https://evil.idp.com", "aud", "u", ""))
	v := newValidator(srv.URL, "https://idp.example.com", "aud")
	_, err := v.ValidateToken(token)
	if err == nil {
		t.Fatal("expected issuer mismatch error, got nil")
	}
}

// TestAudienceMismatch verifies that a token with a different audience is rejected.
func TestAudienceMismatch(t *testing.T) {
	k := newTestKey(t)
	srv := httptest.NewServer(k.jwksHandler())
	defer srv.Close()

	const issuer = "https://idp.example.com"
	token := k.signJWT(t, validPayload(issuer, "wrong-audience", "u", ""))
	v := newValidator(srv.URL, issuer, "expected-audience")
	_, err := v.ValidateToken(token)
	if err == nil {
		t.Fatal("expected audience mismatch error, got nil")
	}
}

// TestJWKSCache verifies that ValidateToken does not re-fetch JWKS on the second
// call: the JWKS server is shut down after the first successful validation, and
// the second call must still succeed using the cached keyset.
func TestJWKSCache(t *testing.T) {
	k := newTestKey(t)
	srv := httptest.NewServer(k.jwksHandler())

	const issuer = "https://idp.example.com"
	const audience = "aud"

	v := newValidator(srv.URL, issuer, audience)

	// First call — fetches JWKS.
	token := k.signJWT(t, validPayload(issuer, audience, "u1", ""))
	if _, err := v.ValidateToken(token); err != nil {
		t.Fatalf("first ValidateToken: %v", err)
	}

	// Shut down JWKS server.
	srv.Close()

	// Second call — must use cached JWKS.
	token2 := k.signJWT(t, validPayload(issuer, audience, "u2", ""))
	if _, err := v.ValidateToken(token2); err != nil {
		t.Fatalf("second ValidateToken (should use cache): %v", err)
	}
}

// TestJWKSKeyRotation verifies the key rotation scenario: a token signed with a
// new kid that is not in the stale cache causes ErrKeyNotFound, but a fresh
// validator (which fetches the updated JWKS) accepts the token successfully.
// The current implementation does NOT re-fetch on cache-miss; it only re-fetches
// after TTL expiry. This test documents that behaviour and confirms a new
// validator pointing at the rotated JWKS succeeds.
func TestJWKSKeyRotation(t *testing.T) {
	k1 := newTestKey(t)
	k2, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate k2: %v", err)
	}
	newKid := "rotated-key"

	// Start server; initially serves k1, will be switched to k2.
	var mu sync.Mutex
	servedKey := k1
	servedKid := k1.kid

	mux := http.NewServeMux()
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		pk := servedKey
		kid := servedKid
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		pub := &pk.priv.PublicKey
		n := b64url(pub.N.Bytes())
		e := b64url([]byte{0x01, 0x00, 0x01})
		resp := fmt.Sprintf(`{"keys":[{"kty":"RSA","use":"sig","alg":"RS256","kid":%q,"n":%q,"e":%q}]}`,
			kid, n, e)
		_, _ = w.Write([]byte(resp))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	const issuer = "https://idp.example.com"
	const audience = "aud"

	// Prime a validator with k1 in the cache.
	v1 := newValidator(srv.URL+"/jwks", issuer, audience)
	token1 := k1.signJWT(t, validPayload(issuer, audience, "u1", ""))
	if _, err := v1.ValidateToken(token1); err != nil {
		t.Fatalf("prime cache with k1: %v", err)
	}

	// Rotate server to k2.
	mu.Lock()
	servedKey = &testKey{priv: k2, kid: newKid}
	servedKid = newKid
	mu.Unlock()

	// Sign token2 with k2 and the new kid.
	headerJSON, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": newKid})
	payloadJSON, _ := json.Marshal(validPayload(issuer, audience, "u2", ""))
	signingInput := b64url(headerJSON) + "." + b64url(payloadJSON)
	hash := sha256.Sum256([]byte(signingInput))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, k2, crypto.SHA256, hash[:])
	token2 := signingInput + "." + b64url(sig)

	// v1 still has the stale k1 cache → token2 should fail (kid not found).
	_, errStale := v1.ValidateToken(token2)
	if errStale == nil {
		t.Error("expected error from stale cache validator, got nil")
	}

	// A fresh validator fetches the updated JWKS → token2 must succeed.
	v2 := newValidator(srv.URL+"/jwks", issuer, audience)
	if _, err := v2.ValidateToken(token2); err != nil {
		t.Fatalf("fresh validator with rotated key: %v", err)
	}
}

// TestMalformedJWT verifies that tokens without three dot-separated parts are
// immediately rejected.
func TestMalformedJWT(t *testing.T) {
	k := newTestKey(t)
	srv := httptest.NewServer(k.jwksHandler())
	defer srv.Close()

	v := newValidator(srv.URL, "https://idp.example.com", "aud")

	cases := []string{
		"",
		"notajwt",
		"only.two",
		"a.b.c.d", // four parts
	}
	for _, tc := range cases {
		_, err := v.ValidateToken(tc)
		if err == nil {
			t.Errorf("ValidateToken(%q): expected error, got nil", tc)
		}
	}
}

// TestBillingValidatorAdapterMapping verifies that ValidateTokenToBillingClaims
// correctly maps OAuth2 claims to *billing.OAuthClaims.
func TestBillingValidatorAdapterMapping(t *testing.T) {
	k := newTestKey(t)
	srv := httptest.NewServer(k.jwksHandler())
	defer srv.Close()

	const (
		issuer   = "https://idp.example.com"
		audience = "test-audience"
		subject  = "sub-xyz"
		email    = "mapped@example.com"
	)

	token := k.signJWT(t, validPayload(issuer, audience, subject, email))
	v := newValidator(srv.URL, issuer, audience)
	adapter := a2a.NewBillingValidator(v)

	claims, err := adapter.ValidateTokenToBillingClaims(token)
	if err != nil {
		t.Fatalf("ValidateTokenToBillingClaims: %v", err)
	}
	if claims.Subject != subject {
		t.Errorf("OAuthClaims.Subject = %q, want %q", claims.Subject, subject)
	}
	if claims.Issuer != issuer {
		t.Errorf("OAuthClaims.Issuer = %q, want %q", claims.Issuer, issuer)
	}
	if claims.Email != email {
		t.Errorf("OAuthClaims.Email = %q, want %q", claims.Email, email)
	}
	// Ensure return type is the billing package type.
	var _ *billing.OAuthClaims = claims
}

// TestBillingValidatorAdapterPropagatesError ensures that errors from the
// underlying OAuth2Validator are propagated unchanged through the adapter.
func TestBillingValidatorAdapterPropagatesError(t *testing.T) {
	k := newTestKey(t)
	srv := httptest.NewServer(k.jwksHandler())
	defer srv.Close()

	v := newValidator(srv.URL, "https://idp.example.com", "correct-aud")
	adapter := a2a.NewBillingValidator(v)

	// Token with wrong audience.
	token := k.signJWT(t, validPayload("https://idp.example.com", "wrong-aud", "u", ""))
	_, err := adapter.ValidateTokenToBillingClaims(token)
	if err == nil {
		t.Fatal("expected error for wrong audience via adapter, got nil")
	}
}

// edTestKey holds an Ed25519 key pair for signing test JWTs (alg=EdDSA, RFC 8037).
type edTestKey struct {
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
	kid  string
}

func newEdTestKey(t *testing.T) *edTestKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	return &edTestKey{priv: priv, pub: pub, kid: "ed-key-1"}
}

func (k *edTestKey) jwksHandler() http.HandlerFunc {
	x := b64url(k.pub)
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := fmt.Sprintf(`{"keys":[{"kty":"OKP","crv":"Ed25519","alg":"EdDSA","use":"sig","kid":%q,"x":%q}]}`,
			k.kid, x)
		_, _ = w.Write([]byte(resp))
	}
}

func (k *edTestKey) signJWT(t *testing.T, payload map[string]any) string {
	t.Helper()
	headerJSON, _ := json.Marshal(map[string]string{
		"alg": "EdDSA",
		"typ": "JWT",
		"kid": k.kid,
	})
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	signingInput := b64url(headerJSON) + "." + b64url(payloadJSON)
	sig := ed25519.Sign(k.priv, []byte(signingInput))
	return signingInput + "." + b64url(sig)
}

// TestValidToken_EdDSA covers the OKP/Ed25519 path used by msg2agent's own
// authorization server. Without it, every token issued by the AS is rejected
// because the legacy validator hard-coded RS256.
func TestValidToken_EdDSA(t *testing.T) {
	k := newEdTestKey(t)
	srv := httptest.NewServer(k.jwksHandler())
	defer srv.Close()

	const (
		issuer   = "https://msg2agent.example.com"
		audience = "https://msg2agent.example.com/mcp"
		subject  = "t_abc123"
		email    = "alice@example.com"
	)

	token := k.signJWT(t, validPayload(issuer, audience, subject, email))
	v := newValidator(srv.URL, issuer, audience)

	claims, err := v.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: unexpected error: %v", err)
	}
	if claims.Subject != subject {
		t.Errorf("Subject = %q, want %q", claims.Subject, subject)
	}
	if claims.Issuer != issuer {
		t.Errorf("Issuer = %q, want %q", claims.Issuer, issuer)
	}
	if claims.Email != email {
		t.Errorf("Email = %q, want %q", claims.Email, email)
	}
}

// TestEdDSA_InvalidSignature ensures tampered EdDSA tokens are rejected and
// don't sneak through under "unknown alg" or similar.
func TestEdDSA_InvalidSignature(t *testing.T) {
	k := newEdTestKey(t)
	srv := httptest.NewServer(k.jwksHandler())
	defer srv.Close()

	const issuer = "https://msg2agent.example.com"
	const audience = "https://msg2agent.example.com/mcp"

	token := k.signJWT(t, validPayload(issuer, audience, "t_x", ""))
	tampered := tamperPayload(token, validPayload(issuer, audience, "t_y", "other@example.com"))

	v := newValidator(srv.URL, issuer, audience)
	if _, err := v.ValidateToken(tampered); err == nil {
		t.Fatal("expected error for tampered EdDSA token, got nil")
	}
}
