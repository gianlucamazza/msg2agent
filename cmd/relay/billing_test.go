package main

import (
	"encoding/json"
	"testing"

	"github.com/gianlucamazza/msg2agent/pkg/billing"
	"github.com/gianlucamazza/msg2agent/pkg/messaging"
	"github.com/gianlucamazza/msg2agent/pkg/protocol"
	"github.com/gianlucamazza/msg2agent/pkg/registry"
)

// testHubWithBilling creates a hub wired with a billing store and tenant pool.
func testHubWithBilling(t *testing.T) (*RelayHub, billing.Store) {
	t.Helper()
	cfg := DefaultRelayConfig()
	cfg.RequireDIDProof = false
	hub := NewRelayHub(cfg, testLogger())

	store := billing.NewMemoryStore()
	hub.billingStore = store
	hub.tenantPool = billing.NewTenantRateLimiterPool(store)
	return hub, store
}

// registerTenantAndKey creates a tenant, issues an API key, returns both.
func registerTenantAndKey(t *testing.T, store billing.Store, name, email string, plan billing.Plan) (*billing.Tenant, string) {
	t.Helper()
	tenant := billing.NewTenant(name, email, plan)
	if err := store.PutTenant(tenant); err != nil {
		t.Fatalf("PutTenant: %v", err)
	}
	plaintext, key, err := billing.GenerateAPIKey(tenant.ID, "test")
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if err := store.PutAPIKey(key); err != nil {
		t.Fatalf("PutAPIKey: %v", err)
	}
	return tenant, plaintext
}

// testClientWithTenant creates a test client with a TenantID.
func testClientWithTenant(hub *RelayHub, id, did, tenantID string) *Client {
	c := testClient(hub, id, did)
	c.TenantID = tenantID
	return c
}

// TestMaxAgentDIDs_quotaEnforced verifies that a tenant on PlanFree (max 3 DIDs)
// cannot register a 4th DID.
func TestMaxAgentDIDs_quotaEnforced(t *testing.T) {
	hub, store := testHubWithBilling(t)

	tenant, _ := registerTenantAndKey(t, store, "Free Corp", "free@example.com", billing.PlanFree)
	// PlanFree allows 3 DIDs.

	registerDID := func(n int, expectSuccess bool) {
		t.Helper()
		did := "did:wba:example.com:agent:free" + string(rune('a'+n))
		c := testClientWithTenant(hub, did, "", tenant.ID)
		hub.Register(c)

		// Use RegistrationRequest (same as handleRegister expects) with a proper UUID.
		agent := registry.NewAgent(did, "Free Agent")
		regReq := RegistrationRequest{Agent: *agent}
		req, _ := protocol.NewRequest("1", "relay.register", regReq)
		c.handleRegister(req)

		select {
		case data := <-c.SendCh:
			var resp protocol.JSONRPCResponse
			json.Unmarshal(data, &resp)
			if expectSuccess && resp.Error != nil {
				t.Errorf("DID %d: expected success, got error: %v", n, resp.Error)
			}
			if !expectSuccess && resp.Error == nil {
				t.Errorf("DID %d: expected quota error, got success", n)
			}
			if !expectSuccess && resp.Error != nil && resp.Error.Code != protocol.CodeQuotaExceeded {
				t.Errorf("DID %d: error code = %d, want %d", n, resp.Error.Code, protocol.CodeQuotaExceeded)
			}
		default:
			t.Errorf("DID %d: no response received", n)
		}
	}

	registerDID(0, true)
	registerDID(1, true)
	registerDID(2, true)
	// 4th DID should be rejected.
	registerDID(3, false)
}

// TestMaxAgentDIDs_twoTenants verifies that two tenants have independent DID quotas
// and both are visible in relay.discover (shared network model).
func TestMaxAgentDIDs_twoTenants(t *testing.T) {
	hub, store := testHubWithBilling(t)

	tenantA, _ := registerTenantAndKey(t, store, "Corp A", "a@example.com", billing.PlanFree)    // 3 DIDs
	tenantB, _ := registerTenantAndKey(t, store, "Corp B", "b@example.com", billing.PlanStarter) // 5 DIDs

	registerFor := func(tenant *billing.Tenant, suffix string) {
		t.Helper()
		did := "did:wba:example.com:agent:" + suffix
		c := testClientWithTenant(hub, did, "", tenant.ID)
		hub.Register(c)

		agent := registry.NewAgent(did, suffix)
		regReq := RegistrationRequest{Agent: *agent}
		req, _ := protocol.NewRequest("1", "relay.register", regReq)
		c.handleRegister(req)

		select {
		case data := <-c.SendCh:
			var resp protocol.JSONRPCResponse
			json.Unmarshal(data, &resp)
			if resp.Error != nil {
				t.Errorf("register %s: unexpected error: %v", suffix, resp.Error)
			}
		default:
			t.Errorf("register %s: no response", suffix)
		}
	}

	// Register 2 DIDs for tenant A.
	registerFor(tenantA, "a1")
	registerFor(tenantA, "a2")

	// Register 3 DIDs for tenant B.
	registerFor(tenantB, "b1")
	registerFor(tenantB, "b2")
	registerFor(tenantB, "b3")

	// Both tenants' DIDs are visible in discover (shared network).
	discoverer := testClient(hub, "discoverer", "")
	hub.Register(discoverer)
	req, _ := protocol.NewRequest("1", "relay.discover", nil)
	discoverer.handleDiscover(req)

	select {
	case data := <-discoverer.SendCh:
		var resp protocol.JSONRPCResponse
		json.Unmarshal(data, &resp)
		if resp.Error != nil {
			t.Fatalf("discover error: %v", resp.Error)
		}
		var agents []*registry.Agent
		json.Unmarshal(resp.Result, &agents)
		if len(agents) < 5 {
			t.Errorf("discover returned %d agents, want ≥5 (shared network)", len(agents))
		}
	default:
		t.Error("no discover response")
	}

	// Tenant A's 3rd DID succeeds (still within free quota of 3).
	registerFor(tenantA, "a3")

	// Tenant A's 4th DID is rejected.
	did := "did:wba:example.com:agent:a4"
	c := testClientWithTenant(hub, did, "", tenantA.ID)
	hub.Register(c)
	agent4 := registry.NewAgent(did, "a4")
	req, _ = protocol.NewRequest("1", "relay.register", RegistrationRequest{Agent: *agent4})
	c.handleRegister(req)

	select {
	case data := <-c.SendCh:
		var resp protocol.JSONRPCResponse
		json.Unmarshal(data, &resp)
		if resp.Error == nil || resp.Error.Code != protocol.CodeQuotaExceeded {
			t.Errorf("tenantA 4th DID: expected CodeQuotaExceeded, got %v", resp.Error)
		}
	default:
		t.Error("no response for 4th DID attempt")
	}

	// Tenant B can still register up to its limit of 5.
	registerFor(tenantB, "b4")
	registerFor(tenantB, "b5")
}

// TestTenantRateLimit_dualGate verifies the dual-gate: per-client AND per-tenant rate limits.
func TestTenantRateLimit_dualGate(t *testing.T) {
	hub, store := testHubWithBilling(t)

	// Create a tenant with very small rate limit.
	tenant := billing.NewTenant("Throttled Corp", "th@example.com", billing.PlanFree)
	tenant.Quota.RateLimitMsgPerSec = 0.001 // near-zero
	tenant.Quota.RateLimitBurstSize = 1
	if err := store.PutTenant(tenant); err != nil {
		t.Fatalf("PutTenant: %v", err)
	}

	sender := testClientWithTenant(hub, "throttled-sender", "did:wba:example.com:agent:throttled", tenant.ID)
	hub.Register(sender)

	recipient := testClient(hub, "throttled-recipient", "did:wba:example.com:agent:recv")
	hub.Register(recipient)

	// First message should pass (burst of 1).
	msg := messaging.Message{
		From:   "did:wba:example.com:agent:throttled",
		To:     "did:wba:example.com:agent:recv",
		Method: "test.method",
	}
	req, _ := protocol.NewRequest("1", "test.method", msg)
	data, _ := protocol.Encode(req)
	sender.handleMessage(data)

	// Drain any response.
	for len(sender.SendCh) > 0 {
		<-sender.SendCh
	}
	for len(recipient.SendCh) > 0 {
		<-recipient.SendCh
	}

	// Second message should hit the tenant rate limit.
	sender.handleMessage(data)

	select {
	case respData := <-sender.SendCh:
		var resp protocol.JSONRPCResponse
		json.Unmarshal(respData, &resp)
		if resp.Error == nil {
			t.Error("expected rate limit error")
		}
		if resp.Error != nil && resp.Error.Code != protocol.CodeRateLimited {
			t.Errorf("error code = %d, want %d", resp.Error.Code, protocol.CodeRateLimited)
		}
	default:
		// Rate limit may also be silently dropped — either is acceptable.
		// The key invariant is no panic and no delivery to recipient.
		if len(recipient.SendCh) > 0 {
			t.Error("throttled message delivered to recipient despite rate limit")
		}
	}
}

// TestNoBillingStore_selfHosted verifies that when no billing store is configured,
// registration works normally without quota enforcement (up to rate limit burst per client).
func TestNoBillingStore_selfHosted(t *testing.T) {
	cfg := DefaultRelayConfig()
	cfg.RequireDIDProof = false
	cfg.RegisterRateLimit = 100 // high rate for test
	hub := NewRelayHub(cfg, testLogger())
	// hub.billingStore is nil → self-hosted mode.

	// Use one client per DID so each has its own fresh rate limiter.
	for i := range 10 {
		did := "did:wba:example.com:agent:sh" + string(rune('a'+i))
		c := testClient(hub, did, "")
		hub.Register(c)

		agent := registry.NewAgent(did, did)
		req, _ := protocol.NewRequest("1", "relay.register", RegistrationRequest{Agent: *agent})
		c.handleRegister(req)
		select {
		case data := <-c.SendCh:
			var resp protocol.JSONRPCResponse
			json.Unmarshal(data, &resp)
			if resp.Error != nil {
				t.Errorf("self-hosted DID %d: unexpected error: %v", i, resp.Error)
			}
		default:
			t.Errorf("no response for DID %d", i)
		}
	}
}
