package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gianlucamazza/msg2agent/pkg/billing"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// withTenant injects t into ctx using the billing context key.
func withTenant(ctx context.Context, t *billing.Tenant) context.Context {
	return context.WithValue(ctx, billing.TenantContextKey(), t)
}

func testApp(t *testing.T) (*application, billing.Store) {
	t.Helper()
	store := billing.NewMemoryStore()
	app := &application{
		store:  store,
		logger: newTestLogger(),
	}
	return app, store
}

// ── handleMe ──────────────────────────────────────────────────────────────────

func TestHandleMe_unauthenticated(t *testing.T) {
	app, _ := testApp(t)
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/me", nil)
	rr := httptest.NewRecorder()
	app.handleMe(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", rr.Code)
	}
}

func TestHandleMe_authenticated(t *testing.T) {
	app, store := testApp(t)
	tenant, err := billing.NewTenant("Alice", "alice@example.com", billing.PlanFree)
	if err != nil {
		t.Fatalf("NewTenant: %v", err)
	}
	if err := store.PutTenant(tenant); err != nil {
		t.Fatalf("PutTenant: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/me", nil)
	req = req.WithContext(withTenant(req.Context(), tenant))
	rr := httptest.NewRecorder()
	app.handleMe(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, want 200; body: %s", rr.Code, rr.Body)
	}
	var resp meResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Email != "alice@example.com" {
		t.Fatalf("email %q, want alice@example.com", resp.Email)
	}
	if resp.Plan != billing.PlanFree {
		t.Fatalf("plan %q, want free", resp.Plan)
	}
}

func TestHandleMe_wrongMethod(t *testing.T) {
	app, _ := testApp(t)
	tenant, _ := billing.NewTenant("Bob", "bob@example.com", billing.PlanFree)
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/me", nil)
	req = req.WithContext(withTenant(req.Context(), tenant))
	rr := httptest.NewRecorder()
	app.handleMe(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d, want 405", rr.Code)
	}
}

// ── handleKeys ────────────────────────────────────────────────────────────────

func TestHandleKeys_getEmpty(t *testing.T) {
	app, _ := testApp(t)
	tenant, _ := billing.NewTenant("C", "c@example.com", billing.PlanFree)

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/keys", nil)
	req = req.WithContext(withTenant(req.Context(), tenant))
	rr := httptest.NewRecorder()
	app.handleKeys(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rr.Code)
	}
	var resp page[keyListItem]
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 0 {
		t.Fatalf("expected total=0, got %d", resp.Total)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(resp.Items))
	}
}

func TestHandleKeys_createAndList(t *testing.T) {
	app, store := testApp(t)
	tenant, err := billing.NewTenant("D", "d@example.com", billing.PlanFree)
	if err != nil {
		t.Fatalf("NewTenant: %v", err)
	}
	if err := store.PutTenant(tenant); err != nil {
		t.Fatalf("PutTenant: %v", err)
	}

	// POST — create key
	body := `{"label":"my-key"}`
	postReq := httptest.NewRequest(http.MethodPost, "/api/dashboard/keys", strings.NewReader(body))
	postReq = postReq.WithContext(withTenant(postReq.Context(), tenant))
	postRR := httptest.NewRecorder()
	app.handleKeys(postRR, postReq)

	if postRR.Code != http.StatusCreated {
		t.Fatalf("POST status %d, want 201; body: %s", postRR.Code, postRR.Body)
	}
	var created createKeyResponse
	if err := json.Unmarshal(postRR.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Key == "" || created.ID == "" {
		t.Fatal("expected non-empty key and id")
	}

	// GET — list should include the new key
	getReq := httptest.NewRequest(http.MethodGet, "/api/dashboard/keys", nil)
	getReq = getReq.WithContext(withTenant(getReq.Context(), tenant))
	getRR := httptest.NewRecorder()
	app.handleKeys(getRR, getReq)

	if getRR.Code != http.StatusOK {
		t.Fatalf("GET status %d, want 200", getRR.Code)
	}
	var resp page[keyListItem]
	if err := json.Unmarshal(getRR.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if resp.Total != 1 {
		t.Fatalf("expected total=1, got %d", resp.Total)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
	if resp.Items[0].Label != "my-key" {
		t.Fatalf("label %q, want my-key", resp.Items[0].Label)
	}
}

func TestHandleKeys_pagination(t *testing.T) {
	app, store := testApp(t)
	tenant, err := billing.NewTenant("Pager", "pager@example.com", billing.PlanFree)
	if err != nil {
		t.Fatalf("NewTenant: %v", err)
	}
	if err := store.PutTenant(tenant); err != nil {
		t.Fatalf("PutTenant: %v", err)
	}

	// Create 3 keys directly in the store.
	for _, label := range []string{"k1", "k2", "k3"} {
		_, key, err := billing.GenerateAPIKey(tenant.ID, label)
		if err != nil {
			t.Fatalf("GenerateAPIKey(%s): %v", label, err)
		}
		if err := store.PutAPIKey(key); err != nil {
			t.Fatalf("PutAPIKey: %v", err)
		}
	}

	// First page: limit=2, offset=0 → 2 items, total=3.
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/keys?limit=2&offset=0", nil)
	req = req.WithContext(withTenant(req.Context(), tenant))
	rr := httptest.NewRecorder()
	app.handleKeys(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rr.Code)
	}
	var p1 page[keyListItem]
	if err := json.Unmarshal(rr.Body.Bytes(), &p1); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p1.Total != 3 {
		t.Fatalf("total=%d, want 3", p1.Total)
	}
	if len(p1.Items) != 2 {
		t.Fatalf("items=%d, want 2", len(p1.Items))
	}

	// Second page: limit=2, offset=2 → 1 item, total=3.
	req2 := httptest.NewRequest(http.MethodGet, "/api/dashboard/keys?limit=2&offset=2", nil)
	req2 = req2.WithContext(withTenant(req2.Context(), tenant))
	rr2 := httptest.NewRecorder()
	app.handleKeys(rr2, req2)

	var p2 page[keyListItem]
	if err := json.Unmarshal(rr2.Body.Bytes(), &p2); err != nil {
		t.Fatalf("decode page2: %v", err)
	}
	if p2.Total != 3 {
		t.Fatalf("page2 total=%d, want 3", p2.Total)
	}
	if len(p2.Items) != 1 {
		t.Fatalf("page2 items=%d, want 1", len(p2.Items))
	}

	// Beyond end: offset=10 → 0 items, total=3.
	req3 := httptest.NewRequest(http.MethodGet, "/api/dashboard/keys?offset=10", nil)
	req3 = req3.WithContext(withTenant(req3.Context(), tenant))
	rr3 := httptest.NewRecorder()
	app.handleKeys(rr3, req3)

	var p3 page[keyListItem]
	if err := json.Unmarshal(rr3.Body.Bytes(), &p3); err != nil {
		t.Fatalf("decode page3: %v", err)
	}
	if p3.Total != 3 {
		t.Fatalf("page3 total=%d, want 3", p3.Total)
	}
	if len(p3.Items) != 0 {
		t.Fatalf("page3 items=%d, want 0", len(p3.Items))
	}
}

// ── handleKeyByID ─────────────────────────────────────────────────────────────

func TestHandleKeyByID_revoke(t *testing.T) {
	app, store := testApp(t)
	tenant, err := billing.NewTenant("E", "e@example.com", billing.PlanFree)
	if err != nil {
		t.Fatalf("NewTenant: %v", err)
	}
	if err := store.PutTenant(tenant); err != nil {
		t.Fatalf("PutTenant: %v", err)
	}

	// Create a key directly via store.
	_, key, err := billing.GenerateAPIKey(tenant.ID, "to-revoke")
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if err := store.PutAPIKey(key); err != nil {
		t.Fatalf("PutAPIKey: %v", err)
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/api/dashboard/keys/"+key.ID, nil)
	delReq = delReq.WithContext(withTenant(delReq.Context(), tenant))
	delRR := httptest.NewRecorder()
	app.handleKeyByID(delRR, delReq)

	if delRR.Code != http.StatusNoContent {
		t.Fatalf("DELETE status %d, want 204; body: %s", delRR.Code, delRR.Body)
	}
}

func TestHandleKeyByID_notFound(t *testing.T) {
	app, _ := testApp(t)
	tenant, _ := billing.NewTenant("F", "f@example.com", billing.PlanFree)

	delReq := httptest.NewRequest(http.MethodDelete, "/api/dashboard/keys/nonexistent-id", nil)
	delReq = delReq.WithContext(withTenant(delReq.Context(), tenant))
	delRR := httptest.NewRecorder()
	app.handleKeyByID(delRR, delReq)

	if delRR.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", delRR.Code)
	}
}

func TestHandleKeyByID_wrongTenant(t *testing.T) {
	app, store := testApp(t)

	owner, _ := billing.NewTenant("Owner", "owner@example.com", billing.PlanFree)
	attacker, _ := billing.NewTenant("Attacker", "attacker@example.com", billing.PlanFree)
	_ = store.PutTenant(owner)
	_ = store.PutTenant(attacker)

	_, key, _ := billing.GenerateAPIKey(owner.ID, "owner-key")
	_ = store.PutAPIKey(key)

	delReq := httptest.NewRequest(http.MethodDelete, "/api/dashboard/keys/"+key.ID, nil)
	delReq = delReq.WithContext(withTenant(delReq.Context(), attacker))
	delRR := httptest.NewRecorder()
	app.handleKeyByID(delRR, delReq)

	if delRR.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant revoke: status %d, want 404 (key not visible to attacker tenant)", delRR.Code)
	}
}
