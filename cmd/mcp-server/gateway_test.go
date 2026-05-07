package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gianlucamazza/msg2agent/pkg/billing"
	"github.com/gianlucamazza/msg2agent/pkg/messaging"
)

// ── messageWrapper ────────────────────────────────────────────────────────────

func TestMessageWrapper_IsError_false(t *testing.T) {
	msg := &messaging.Message{Type: messaging.TypeResponse, Body: []byte(`{"ok":true}`)}
	w := &messageWrapper{m: msg}
	if w.IsError() {
		t.Fatal("expected IsError() == false for response message")
	}
}

func TestMessageWrapper_IsError_true(t *testing.T) {
	msg := &messaging.Message{Type: messaging.TypeError, Body: []byte(`{"code":-32600}`)}
	w := &messageWrapper{m: msg}
	if !w.IsError() {
		t.Fatal("expected IsError() == true for error message")
	}
}

func TestMessageWrapper_RawBody(t *testing.T) {
	body := json.RawMessage(`{"result":"pong"}`)
	msg := &messaging.Message{Body: []byte(body)}
	w := &messageWrapper{m: msg}
	got := w.RawBody()
	if string(got) != string(body) {
		t.Fatalf("RawBody() = %q, want %q", got, body)
	}
}

// ── gatewayBridge.tenantIdentity ──────────────────────────────────────────────

func makeTestGateway() *gatewayBridge {
	return &gatewayBridge{domain: "test.example"}
}

func ctxWithTenant(tenant *billing.Tenant) context.Context {
	return context.WithValue(context.Background(), billing.TenantContextKey(), tenant)
}

func TestGatewayBridge_tenantIdentity_noTenant(t *testing.T) {
	g := makeTestGateway()
	if _, err := g.tenantIdentity(context.Background()); err == nil {
		t.Fatal("expected error when no tenant in context")
	}
}

func TestGatewayBridge_tenantIdentity_noDIDSeed(t *testing.T) {
	g := makeTestGateway()
	tenant := &billing.Tenant{ID: "t-noseed"}
	if _, err := g.tenantIdentity(ctxWithTenant(tenant)); err == nil {
		t.Fatal("expected error when DIDSeed is nil")
	}
}

func TestGatewayBridge_tenantIdentity_shortSeed(t *testing.T) {
	g := makeTestGateway()
	tenant := &billing.Tenant{ID: "t-short", DIDSeed: make([]byte, 16)}
	if _, err := g.tenantIdentity(ctxWithTenant(tenant)); err == nil {
		t.Fatal("expected error when DIDSeed is shorter than 32 bytes")
	}
}

func TestGatewayBridge_tenantIdentity_success(t *testing.T) {
	g := makeTestGateway()
	tenant := billing.NewTenant("Alice", "alice@example.com", billing.PlanFree)
	// Pre-mark as registered to skip the goroutine that calls g.a (nil in tests).
	g.registered = map[string]bool{tenant.ID: true}

	ident, err := g.tenantIdentity(ctxWithTenant(tenant))
	if err != nil {
		t.Fatalf("tenantIdentity: %v", err)
	}
	if ident == nil {
		t.Fatal("expected non-nil identity")
	}
	if ident.String() == "" {
		t.Fatal("expected non-empty DID string")
	}
}

func TestGatewayBridge_tenantIdentity_cached(t *testing.T) {
	g := makeTestGateway()
	tenant := billing.NewTenant("Bob", "bob@example.com", billing.PlanFree)
	g.registered = map[string]bool{tenant.ID: true}

	ident1, err := g.tenantIdentity(ctxWithTenant(tenant))
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	ident2, err := g.tenantIdentity(ctxWithTenant(tenant))
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if ident1 != ident2 {
		t.Fatal("expected second call to return cached identity (same pointer)")
	}
}
