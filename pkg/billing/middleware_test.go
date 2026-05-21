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
