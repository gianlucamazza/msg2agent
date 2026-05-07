package oauth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// ── GenerateToken / HashToken ────────────────────────────────────────────────

func TestGenerateToken(t *testing.T) {
	plain, hash, err := GenerateToken(32)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if len(plain) == 0 || len(hash) == 0 {
		t.Fatal("expected non-empty token and hash")
	}
	if plain == hash {
		t.Fatal("plaintext and hash must differ")
	}
	if HashToken(plain) != hash {
		t.Fatal("HashToken(plain) must equal returned hash")
	}
}

func TestGenerateToken_unique(t *testing.T) {
	a, _, _ := GenerateToken(32)
	b, _, _ := GenerateToken(32)
	if a == b {
		t.Fatal("expected unique tokens")
	}
}

// ── pkceVerify ───────────────────────────────────────────────────────────────

func TestPKCEVerify_valid(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])
	if err := pkceVerify(verifier, challenge); err != nil {
		t.Fatalf("pkceVerify: %v", err)
	}
}

func TestPKCEVerify_invalid(t *testing.T) {
	if err := pkceVerify("wrong-verifier", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"); err == nil {
		t.Fatal("expected error for mismatched verifier")
	}
}

// ── JWTIssuer / JWTVerifier round-trip ───────────────────────────────────────

func newTestKeypair(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return priv
}

func TestJWT_roundtrip(t *testing.T) {
	priv := newTestKeypair(t)
	issuer := NewJWTIssuer(priv, "key-1", "https://example.com")
	verifier := NewJWTVerifier(priv, "https://example.com", "https://example.com/mcp")

	token, err := issuer.IssueAccessToken("tenant-1", "cli_abc", "mcp:tools:read", "https://example.com/mcp", "jti-1")
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	tenantID, err := verifier.ValidateClaims(token)
	if err != nil {
		t.Fatalf("ValidateClaims: %v", err)
	}
	if tenantID != "tenant-1" {
		t.Fatalf("got tenant %q, want %q", tenantID, "tenant-1")
	}
}

func TestJWT_sessionCookie_roundtrip(t *testing.T) {
	priv := newTestKeypair(t)
	issuer := NewJWTIssuer(priv, "key-1", "https://example.com")
	verifier := NewJWTVerifier(priv, "https://example.com", "https://example.com/mcp")

	cookie, err := issuer.IssueSessionCookie("tenant-42")
	if err != nil {
		t.Fatalf("IssueSessionCookie: %v", err)
	}

	tenantID, err := verifier.ValidateSessionCookie(cookie)
	if err != nil {
		t.Fatalf("ValidateSessionCookie: %v", err)
	}
	if tenantID != "tenant-42" {
		t.Fatalf("got %q, want %q", tenantID, "tenant-42")
	}
}

func TestJWT_wrongKey(t *testing.T) {
	priv1 := newTestKeypair(t)
	priv2 := newTestKeypair(t)
	issuer := NewJWTIssuer(priv1, "k1", "https://example.com")
	verifier := NewJWTVerifier(priv2, "https://example.com", "https://example.com/mcp")

	token, _ := issuer.IssueAccessToken("t", "c", "s", "https://example.com/mcp", "j")
	if _, err := verifier.ValidateClaims(token); err == nil {
		t.Fatal("expected error when verifying with wrong key")
	}
}

// ── DCRHandler ───────────────────────────────────────────────────────────────

func TestDCRHandler_success(t *testing.T) {
	store := newMemStore()
	h := DCRHandler(store)

	body := `{"client_name":"smoke","redirect_uris":["http://localhost:9999/cb"]}`
	req := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status %d, want 201; body: %s", rr.Code, rr.Body)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type %q, want application/json", ct)
	}
	// Response must include client_id starting with "cli_"
	body = rr.Body.String()
	if !strings.Contains(body, `"cli_`) {
		t.Fatalf("expected client_id with cli_ prefix, got: %s", body)
	}
}

func TestDCRHandler_missingClientName(t *testing.T) {
	h := DCRHandler(newMemStore())
	body := `{"redirect_uris":["http://localhost:9999/cb"]}`
	req := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", rr.Code)
	}
}

func TestDCRHandler_invalidMethod(t *testing.T) {
	h := DCRHandler(newMemStore())
	req := httptest.NewRequest(http.MethodGet, "/oauth/register", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d, want 405", rr.Code)
	}
}

// ── TokenHandler ─────────────────────────────────────────────────────────────

func setupTokenTest(t *testing.T) (Store, *JWTIssuer) {
	t.Helper()
	priv := newTestKeypair(t)
	issuer := NewJWTIssuer(priv, "k1", "https://example.com")
	store := newMemStore()

	// Register a test client.
	_ = store.PutClient(&Client{
		ClientID:                "cli_test",
		ClientName:              "Test Client",
		RedirectURIs:            []string{"http://localhost:9999/cb"},
		GrantTypes:              []string{"authorization_code"},
		TokenEndpointAuthMethod: "none",
	})

	return store, issuer
}

func storeCodeForTest(t *testing.T, store Store, verifier, redirectURI string) string {
	t.Helper()
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	plain, hash, err := GenerateToken(32)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	_ = store.PutCode(&Code{
		CodeHash:            hash,
		ClientID:            "cli_test",
		TenantID:            "tenant-1",
		RedirectURI:         redirectURI,
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		ExpiresAt:           time.Now().Add(time.Minute),
	})
	return plain
}

func TestTokenHandler_authCode(t *testing.T) {
	store, issuer := setupTokenTest(t)
	h := TokenHandler(store, issuer, "https://example.com/mcp")

	const redirectURI = "http://localhost:9999/cb"
	const verifier = "test-verifier-long-enough-to-be-valid-0123456789"
	code := storeCodeForTest(t, store, verifier, redirectURI)

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("code_verifier", verifier)
	form.Set("client_id", "cli_test")
	form.Set("redirect_uri", redirectURI)

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, want 200; body: %s", rr.Code, rr.Body)
	}
	if !strings.Contains(rr.Body.String(), `"access_token"`) {
		t.Fatalf("response missing access_token: %s", rr.Body)
	}
}

func TestTokenHandler_wrongVerifier(t *testing.T) {
	store, issuer := setupTokenTest(t)
	h := TokenHandler(store, issuer, "https://example.com/mcp")

	const redirectURI = "http://localhost:9999/cb"
	code := storeCodeForTest(t, store, "correct-verifier-padding000000000000000000", redirectURI)

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("code_verifier", "wrong-verifier-padding000000000000000000000")
	form.Set("client_id", "cli_test")
	form.Set("redirect_uri", redirectURI)

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400; body: %s", rr.Code, rr.Body)
	}
}

func TestTokenHandler_unsupportedGrant(t *testing.T) {
	store, issuer := setupTokenTest(t)
	h := TokenHandler(store, issuer, "https://example.com/mcp")

	form := url.Values{"grant_type": {"client_credentials"}}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", rr.Code)
	}
}
