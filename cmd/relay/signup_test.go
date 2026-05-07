package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gianlucamazza/msg2agent/pkg/billing"
)

func TestSignupHandler_success(t *testing.T) {
	store := billing.NewMemoryStore()
	handler := signupHandler(store, nil, nil, "", testLogger())

	body, _ := json.Marshal(map[string]string{
		"name":  "Acme Corp",
		"email": "admin@acme.io",
		"plan":  "free",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tenants", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	var resp signupResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.TenantID == "" || resp.APIKey == "" {
		t.Errorf("missing tenant_id or api_key: %+v", resp)
	}
	if resp.Status != "active" {
		t.Errorf("status = %q, want active", resp.Status)
	}
}

func TestSignupHandler_paidPlanNoStripe(t *testing.T) {
	store := billing.NewMemoryStore()
	// nil stripeClient → paid plans should return 503
	handler := signupHandler(store, nil, nil, "", testLogger())

	body, _ := json.Marshal(map[string]string{
		"name":  "Paid Corp",
		"email": "paid@corp.io",
		"plan":  "starter",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tenants", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 when Stripe is not configured", rec.Code)
	}
}

func TestSignupHandler_defaultPlan(t *testing.T) {
	store := billing.NewMemoryStore()
	handler := signupHandler(store, nil, nil, "", testLogger())

	// Omit plan — should default to "free".
	body, _ := json.Marshal(map[string]string{
		"name":  "Free User",
		"email": "user@example.com",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tenants", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
}

func TestSignupHandler_invalidEmail(t *testing.T) {
	store := billing.NewMemoryStore()
	handler := signupHandler(store, nil, nil, "", testLogger())

	body, _ := json.Marshal(map[string]string{"name": "Test", "email": "not-an-email", "plan": "free"})
	req := httptest.NewRequest(http.MethodPost, "/api/tenants", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestSignupHandler_nameTooShort(t *testing.T) {
	store := billing.NewMemoryStore()
	handler := signupHandler(store, nil, nil, "", testLogger())

	body, _ := json.Marshal(map[string]string{"name": "X", "email": "x@example.com", "plan": "free"})
	req := httptest.NewRequest(http.MethodPost, "/api/tenants", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestSignupHandler_invalidPlan(t *testing.T) {
	store := billing.NewMemoryStore()
	handler := signupHandler(store, nil, nil, "", testLogger())

	body, _ := json.Marshal(map[string]string{"name": "Corp", "email": "corp@example.com", "plan": "enterprise"})
	req := httptest.NewRequest(http.MethodPost, "/api/tenants", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestSignupHandler_methodNotAllowed(t *testing.T) {
	store := billing.NewMemoryStore()
	handler := signupHandler(store, nil, nil, "", testLogger())

	req := httptest.NewRequest(http.MethodGet, "/api/tenants", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestSignupHandler_rateLimit(t *testing.T) {
	store := billing.NewMemoryStore()
	handler := signupHandler(store, nil, nil, "", testLogger())

	makeBody := func() *bytes.Reader {
		body, _ := json.Marshal(map[string]string{"name": "X Corp", "email": "x@corp.io", "plan": "free"})
		return bytes.NewReader(body)
	}

	// First 5 requests from the same IP should not be rate limited.
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/tenants", makeBody())
		req.RemoteAddr = "9.9.9.9:12345"
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d rate limited unexpectedly", i+1)
		}
	}

	// 6th request from the same IP should be rate limited.
	req6 := httptest.NewRequest(http.MethodPost, "/api/tenants", makeBody())
	req6.RemoteAddr = "9.9.9.9:12345"
	rec6 := httptest.NewRecorder()
	handler(rec6, req6)
	if rec6.Code != http.StatusTooManyRequests {
		t.Errorf("6th request: status = %d, want 429", rec6.Code)
	}

	// A different IP should not be rate limited.
	reqOther := httptest.NewRequest(http.MethodPost, "/api/tenants", makeBody())
	reqOther.RemoteAddr = "1.2.3.4:9999"
	recOther := httptest.NewRecorder()
	handler(recOther, reqOther)
	if recOther.Code == http.StatusTooManyRequests {
		t.Errorf("different IP was rate limited unexpectedly")
	}
}

func TestSignupHandler_realIPHeaders(t *testing.T) {
	store := billing.NewMemoryStore()
	handler := signupHandler(store, nil, nil, "", testLogger())

	makeBody := func() *bytes.Reader {
		body, _ := json.Marshal(map[string]string{"name": "Header Corp", "email": "h@corp.io", "plan": "free"})
		return bytes.NewReader(body)
	}

	// Exhaust rate limit for the forwarded IP.
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/tenants", makeBody())
		req.Header.Set("X-Real-IP", "10.0.0.1")
		rec := httptest.NewRecorder()
		handler(rec, req)
	}

	// 6th request with same X-Real-IP should be limited.
	req := httptest.NewRequest(http.MethodPost, "/api/tenants", makeBody())
	req.Header.Set("X-Real-IP", "10.0.0.1")
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("X-Real-IP rate limit: status = %d, want 429", rec.Code)
	}
}

func TestSignupHandler_tenantStoredCorrectly(t *testing.T) {
	store := billing.NewMemoryStore()
	handler := signupHandler(store, nil, nil, "", testLogger())

	body, _ := json.Marshal(map[string]string{
		"name":  "Test Tenant",
		"email": "TENANT@Example.COM", // should be lowercased
		"plan":  "free",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tenants", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}

	var resp signupResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Verify the tenant was actually stored.
	tenant, err := store.GetTenant(resp.TenantID)
	if err != nil {
		t.Fatalf("GetTenant: %v", err)
	}
	if tenant.Email != "tenant@example.com" {
		t.Errorf("email = %q, want lowercased", tenant.Email)
	}
	if tenant.Plan != billing.PlanFree {
		t.Errorf("plan = %q, want free", tenant.Plan)
	}

	// Verify the API key was stored and is valid.
	hash, err := billing.HashAPIKey(resp.APIKey)
	if err != nil {
		t.Fatalf("HashAPIKey: %v", err)
	}
	key, err := store.GetAPIKeyByHash(hash)
	if err != nil {
		t.Fatalf("GetAPIKeyByHash: %v", err)
	}
	if key.TenantID != resp.TenantID {
		t.Errorf("key.TenantID = %q, want %q", key.TenantID, resp.TenantID)
	}
	if !key.IsValid() {
		t.Error("key should be valid")
	}
}
