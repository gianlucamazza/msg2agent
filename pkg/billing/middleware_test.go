package billing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) (*MemoryStore, *Tenant, string) {
	t.Helper()
	store := NewMemoryStore()
	tenant, err := NewTenant("Test Corp", "test@example.com", PlanStarter)
	if err != nil {
		t.Fatalf("NewTenant: %v", err)
	}
	_ = store.PutTenant(tenant)
	plaintext, key, err := GenerateAPIKey(tenant.ID, "test")
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	_ = store.PutAPIKey(key)
	return store, tenant, plaintext
}

func okHandler(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

func TestAPIKeyMiddleware_valid(t *testing.T) {
	store, _, plaintext := newTestStore(t)
	mw := APIKeyMiddleware(store, false)
	h := mw(http.HandlerFunc(okHandler))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestAPIKeyMiddleware_missingHeader_noAnon(t *testing.T) {
	store, _, _ := newTestStore(t)
	mw := APIKeyMiddleware(store, false)
	h := mw(http.HandlerFunc(okHandler))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestAPIKeyMiddleware_missingHeader_allowAnon(t *testing.T) {
	store, _, _ := newTestStore(t)
	mw := APIKeyMiddleware(store, true)
	h := mw(http.HandlerFunc(okHandler))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestAPIKeyMiddleware_invalidFormat(t *testing.T) {
	store, _, _ := newTestStore(t)
	mw := APIKeyMiddleware(store, false)
	h := mw(http.HandlerFunc(okHandler))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Token notabearer")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestAPIKeyMiddleware_revokedKey(t *testing.T) {
	store, _, plaintext := newTestStore(t)

	// Revoke the key.
	keys, _ := store.ListAPIKeys(store.tenants[func() string {
		for k := range store.tenants {
			return k
		}
		return ""
	}()].ID)
	_ = store.RevokeAPIKey(keys[0].ID)

	mw := APIKeyMiddleware(store, false)
	h := mw(http.HandlerFunc(okHandler))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestAPIKeyMiddleware_suspendedTenant(t *testing.T) {
	store, tenant, plaintext := newTestStore(t)
	_ = store.SuspendTenant(tenant.ID)

	mw := APIKeyMiddleware(store, false)
	h := mw(http.HandlerFunc(okHandler))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestAPIKeyMiddleware_health_skipped(t *testing.T) {
	store, _, _ := newTestStore(t)
	mw := APIKeyMiddleware(store, false)
	h := mw(http.HandlerFunc(okHandler))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("health endpoint: status = %d, want 200", rec.Code)
	}
}

func TestAPIKeyMiddleware_setsContextTenant(t *testing.T) {
	store, tenant, plaintext := newTestStore(t)
	mw := APIKeyMiddleware(store, false)

	var gotTenant *Tenant
	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotTenant = TenantFromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if gotTenant == nil {
		t.Fatal("tenant not set in context")
	}
	if gotTenant.ID != tenant.ID {
		t.Errorf("context tenant ID = %q, want %q", gotTenant.ID, tenant.ID)
	}
}

func TestTenantFromContext_nil(t *testing.T) {
	ctx := context.Background()
	if TenantFromContext(ctx) != nil {
		t.Error("expected nil for empty context")
	}
}

func TestAPIKeyMiddleware_unknownKey(t *testing.T) {
	store := NewMemoryStore()
	// No tenants or keys registered.
	plaintext := apiKeyPrefixLive + strings.Repeat("A", 43)
	mw := APIKeyMiddleware(store, false)
	h := mw(http.HandlerFunc(okHandler))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("unknown key: status = %d, want 401", rec.Code)
	}
}

func TestPaymentRequiredMessage_omitsURLWhenUnset(t *testing.T) {
	t.Setenv(PublicBaseURLEnv, "")
	got := paymentRequiredMessage()
	if got != "payment required: update your payment method" {
		t.Errorf("unset base URL: got %q", got)
	}
	if strings.Contains(got, "http") {
		t.Errorf("unset base URL should omit any URL, got %q", got)
	}
}

func TestPaymentRequiredMessage_includesURLWhenSet(t *testing.T) {
	t.Setenv(PublicBaseURLEnv, "https://relay.example.com")
	got := paymentRequiredMessage()
	want := "payment required: update your payment method at https://relay.example.com/app/"
	if got != want {
		t.Errorf("set base URL: got %q, want %q", got, want)
	}
}

func TestPaymentRequiredMessage_trimsTrailingSlash(t *testing.T) {
	t.Setenv(PublicBaseURLEnv, "https://relay.example.com/")
	got := paymentRequiredMessage()
	if strings.Contains(got, "//app/") {
		t.Errorf("trailing slash should be trimmed, got %q", got)
	}
	if !strings.Contains(got, "https://relay.example.com/app/") {
		t.Errorf("got %q, want URL ending in /app/", got)
	}
}

func TestAPIKeyMiddleware_pastDue_omitsURLWhenUnset(t *testing.T) {
	t.Setenv(PublicBaseURLEnv, "")
	store, tenant, plaintext := newTestStore(t)
	tenant.BillingStatus = "past_due"

	mw := APIKeyMiddleware(store, false)
	h := mw(http.HandlerFunc(okHandler))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("status = %d, want 402", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "payment required: update your payment method") {
		t.Errorf("body %q missing payment-required text", body)
	}
	if strings.Contains(body, "http") {
		t.Errorf("unset public base URL should omit URL from 402 body, got %q", body)
	}
}

func TestAPIKeyMiddleware_pastDue_includesURLWhenSet(t *testing.T) {
	t.Setenv(PublicBaseURLEnv, "https://relay.example.com")
	store, tenant, plaintext := newTestStore(t)
	tenant.BillingStatus = "incomplete"

	mw := APIKeyMiddleware(store, false)
	h := mw(http.HandlerFunc(okHandler))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("status = %d, want 402", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "https://relay.example.com/app/") {
		t.Errorf("body %q missing configured dashboard URL", body)
	}
}
