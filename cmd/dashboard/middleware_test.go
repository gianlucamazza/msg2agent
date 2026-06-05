package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── securityHeaders ────────────────────────────────────────────────────────────

func TestSecurityHeaders_present(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := securityHeaders(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rr.Code)
	}
	for _, hdr := range []string{"Content-Security-Policy", "X-Content-Type-Options", "Referrer-Policy"} {
		if v := rr.Header().Get(hdr); v == "" {
			t.Errorf("missing security header %q", hdr)
		}
	}
	if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := rr.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Errorf("Referrer-Policy = %q, want strict-origin-when-cross-origin", got)
	}
}

// ── requestIDMiddleware ────────────────────────────────────────────────────────

func TestRequestIDMiddleware_generates(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := requestIDMiddleware(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// No X-Request-Id inbound
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if got := rr.Header().Get("X-Request-Id"); got == "" {
		t.Fatal("expected X-Request-Id to be generated, got empty")
	}
}

func TestRequestIDMiddleware_passthrough(t *testing.T) {
	const want = "my-custom-id-123"
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := requestIDMiddleware(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-Id", want)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if got := rr.Header().Get("X-Request-Id"); got != want {
		t.Errorf("X-Request-Id = %q, want %q", got, want)
	}
}

// ── corsMiddleware ─────────────────────────────────────────────────────────────

func TestCorsMiddleware_noOrigins(t *testing.T) {
	// When no origins are configured, the middleware is a no-op.
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := corsMiddleware()(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no CORS header, got %q", got)
	}
}

func TestCorsMiddleware_matchedOrigin(t *testing.T) {
	const origin = "https://app.example.com"
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := corsMiddleware(origin)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", origin)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != origin {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, origin)
	}
}

func TestCorsMiddleware_preflight(t *testing.T) {
	const origin = "https://app.example.com"
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should not be called for preflight.
		w.WriteHeader(http.StatusOK)
	})
	h := corsMiddleware(origin)(next)

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", origin)
	req.Header.Set("Access-Control-Request-Method", "POST")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("preflight status %d, want 204", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != origin {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, origin)
	}
}

func TestCorsMiddleware_unknownOrigin(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := corsMiddleware("https://app.example.com")(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no CORS header for unknown origin, got %q", got)
	}
	// Next handler still runs (non-preflight).
	if rr.Code != http.StatusOK {
		t.Errorf("status %d, want 200", rr.Code)
	}
}

// ── keyCreateLimiter ──────────────────────────────────────────────────────────

func newTestLimiter() *keyCreateLimiter {
	return &keyCreateLimiter{buckets: make(map[string]*kcBucket)}
}

func TestKeyCreateLimiter_consumesTokens(t *testing.T) {
	l := newTestLimiter()
	const id = "tenant-limiter-test"
	// First 10 calls must succeed (full bucket).
	for i := 0; i < int(kcCapacity); i++ {
		if !l.allow(id) {
			t.Fatalf("call %d: expected allow=true", i+1)
		}
	}
}

func TestKeyCreateLimiter_blocksWhenExhausted(t *testing.T) {
	l := newTestLimiter()
	const id = "tenant-limiter-exhaust"
	// Drain the bucket.
	for i := 0; i < int(kcCapacity); i++ {
		l.allow(id)
	}
	// Next call must be rejected.
	if l.allow(id) {
		t.Fatal("expected allow=false after bucket exhaustion")
	}
}

func TestKeyCreateLimiter_retryAfterPositive(t *testing.T) {
	l := newTestLimiter()
	const id = "tenant-retry-after"
	for i := 0; i < int(kcCapacity); i++ {
		l.allow(id)
	}
	secs := l.retryAfterSecs(id)
	if secs <= 0 {
		t.Fatalf("retryAfterSecs = %d, want > 0 after exhaustion", secs)
	}
}

func TestKeyCreateLimiter_differentTenants(t *testing.T) {
	l := newTestLimiter()
	// Exhaust tenant A.
	for i := 0; i < int(kcCapacity); i++ {
		l.allow("tenant-A")
	}
	// Tenant B must still have a full bucket.
	if !l.allow("tenant-B") {
		t.Fatal("tenant B should not be rate-limited when only tenant A is exhausted")
	}
}

func TestCorsMiddleware_preflightUnknownOrigin(t *testing.T) {
	// Preflight with unknown origin: still returns 204 but without CORS headers.
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := corsMiddleware("https://app.example.com")(next)

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("preflight status %d, want 204", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no CORS header for unknown origin in preflight, got %q", got)
	}
}

// ── writeRateLimitError ────────────────────────────────────────────────────────

func TestWriteRateLimitError(t *testing.T) {
	rr := httptest.NewRecorder()
	writeRateLimitError(rr, 42)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("status %d, want 429", rr.Code)
	}
	if got := rr.Header().Get("Retry-After"); got != "42" {
		t.Errorf("Retry-After = %q, want 42", got)
	}
	if !strings.Contains(rr.Body.String(), "error") {
		t.Errorf("body missing error key: %s", rr.Body)
	}
}
