package billing_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gianlucamazza/msg2agent/adapters/a2a"
	"github.com/gianlucamazza/msg2agent/pkg/billing"
)

func TestOAuth2Middleware_unknownIdentity_noAutoProvision(t *testing.T) {
	store := billing.NewMemoryStore()
	validator := a2a.NewBillingValidator(a2a.NewOAuth2Validator(a2a.OAuth2Config{SkipValidation: true}))
	mw := billing.OAuth2Middleware(validator, store, "")

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer some.jwt.token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (identity not registered)", rec.Code)
	}
}

func TestOAuth2Middleware_autoProvision(t *testing.T) {
	store := billing.NewMemoryStore()
	validator := a2a.NewBillingValidator(a2a.NewOAuth2Validator(a2a.OAuth2Config{SkipValidation: true}))
	mw := billing.OAuth2Middleware(validator, store, billing.PlanFree)

	var gotTenant *billing.Tenant
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTenant = billing.TenantFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer some.jwt.token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if gotTenant == nil {
		t.Fatal("no tenant in context")
	}
	if gotTenant.Plan != billing.PlanFree {
		t.Errorf("plan = %q, want PlanFree", gotTenant.Plan)
	}

	// Second request with same JWT → same tenant (no duplicate).
	rec2 := httptest.NewRecorder()
	var gotTenant2 *billing.Tenant
	handler2 := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTenant2 = billing.TenantFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req2 := httptest.NewRequest("GET", "/mcp", nil)
	req2.Header.Set("Authorization", "Bearer some.jwt.token")
	handler2.ServeHTTP(rec2, req2)

	if gotTenant2 == nil || gotTenant2.ID != gotTenant.ID {
		t.Error("second request got different or nil tenant — should reuse existing")
	}
}

func TestOAuth2Middleware_apiKeyPassthrough(t *testing.T) {
	store := billing.NewMemoryStore()
	validator := a2a.NewBillingValidator(a2a.NewOAuth2Validator(a2a.OAuth2Config{SkipValidation: true}))
	mw := billing.OAuth2Middleware(validator, store, billing.PlanFree)

	reached := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer sk_live_abc12345678")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Must pass through to next (API key path), not attempt JWT validation.
	if !reached {
		t.Error("OAuth2Middleware blocked an API key bearer token — should pass through")
	}
}

// TenantFromContext is exported; verify it works cross-package.
func TestTenantFromContext_nil(t *testing.T) {
	if billing.TenantFromContext(context.Background()) != nil {
		t.Error("expected nil for empty context")
	}
}
