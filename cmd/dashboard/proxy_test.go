package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gianlucamazza/msg2agent/pkg/billing"
)

// TestProxyToRelay verifies that proxyToRelay forwards the POST body and
// returns whatever the upstream relay responds with.
func TestProxyToRelay_forwardsRequest(t *testing.T) {
	const wantBody = `{"session":"abc123"}`

	// Minimal upstream relay that echoes back a fixed JSON response.
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("relay: method = %q, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/api/billing/portal") {
			t.Errorf("relay: path = %q, want .../api/billing/portal", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(wantBody))
	}))
	defer relay.Close()

	app, store := testApp(t)
	app.relayURL = relay.URL

	tenant, err := billing.NewTenant("Proxy", "proxy@example.com", billing.PlanFree)
	if err != nil {
		t.Fatalf("NewTenant: %v", err)
	}
	if err := store.PutTenant(tenant); err != nil {
		t.Fatalf("PutTenant: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/portal", strings.NewReader(`{}`))
	req = req.WithContext(withTenant(req.Context(), tenant))
	rr := httptest.NewRecorder()
	app.proxyToRelay(rr, req, "/api/billing/portal")

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, want 200; body: %s", rr.Code, rr.Body)
	}
	got, _ := io.ReadAll(rr.Body)
	if string(got) != wantBody {
		t.Errorf("body = %q, want %q", got, wantBody)
	}
}

// TestProxyToRelay_noRelayURL verifies that a 501 is returned when relayURL is empty.
func TestProxyToRelay_noRelayURL(t *testing.T) {
	app, store := testApp(t)
	// relayURL is empty (zero value)
	tenant, _ := billing.NewTenant("ProxyNoURL", "proxynourl@example.com", billing.PlanFree)
	_ = store.PutTenant(tenant)

	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/checkout", strings.NewReader(`{"plan":"pro"}`))
	req = req.WithContext(withTenant(req.Context(), tenant))
	rr := httptest.NewRecorder()
	app.proxyToRelay(rr, req, "/api/billing/checkout")

	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("status %d, want 501", rr.Code)
	}
}

// TestProxyToRelay_propagatesMethod verifies that the relay receives a POST
// regardless of the originating handler path.
func TestProxyToRelay_propagatesMethod(t *testing.T) {
	var gotMethod string
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer relay.Close()

	app, store := testApp(t)
	app.relayURL = relay.URL

	tenant, _ := billing.NewTenant("PropMethod", "propmethod@example.com", billing.PlanFree)
	_ = store.PutTenant(tenant)

	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/checkout", strings.NewReader(`{}`))
	req = req.WithContext(withTenant(req.Context(), tenant))
	rr := httptest.NewRecorder()
	app.proxyToRelay(rr, req, "/api/billing/checkout")

	if gotMethod != http.MethodPost {
		t.Errorf("relay received method %q, want POST", gotMethod)
	}
}
