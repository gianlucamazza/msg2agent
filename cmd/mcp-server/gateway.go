package main

// gateway.go — per-tenant DID derivation and lazy subordinate registration.
//
// Each MCP API key is owned by a billing tenant. Rather than all tenants sharing
// the process-wide agent DID, the mcp-server acts as a DID-WBA gateway: it holds
// one WS connection to the relay and signs messages on behalf of per-tenant DIDs
// via ActorDID / ActorProof (gateway delegation pattern, RFC 8693 actor/subject).
//
// Tenant DIDs are derived deterministically from a 32-byte seed stored in the
// billing DB (billing.DeriveTenantIdentity). On first use they are registered as
// subordinates in the relay registry so recipients can look up their public keys.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"

	mcpadapter "github.com/gianlucamazza/msg2agent/adapters/mcp"
	"github.com/gianlucamazza/msg2agent/pkg/agent"
	"github.com/gianlucamazza/msg2agent/pkg/billing"
	"github.com/gianlucamazza/msg2agent/pkg/identity"
	"github.com/gianlucamazza/msg2agent/pkg/registry"
)

// gatewayBridge wraps agent.Agent and the billing store to provide per-tenant
// DID delegation on every Send / SendAsync call.
type gatewayBridge struct {
	a      *agent.Agent
	domain string
	store  billing.Store

	mu         sync.Mutex
	identCache map[string]*identity.Identity // tenantID → derived identity
	registered map[string]bool               // tenantID → already registered as subordinate
}

var _ mcpadapter.AgentCaller = (*gatewayBridge)(nil)

func (g *gatewayBridge) DID() string             { return g.a.DID() }
func (g *gatewayBridge) Record() *registry.Agent { return g.a.Record() }
func (g *gatewayBridge) CallRelay(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return g.a.CallRelay(ctx, method, params)
}

func (g *gatewayBridge) Send(ctx context.Context, to, method string, params any) (mcpadapter.AgentMessage, error) {
	ident, err := g.tenantIdentity(ctx)
	if err != nil {
		// No tenant in context (e.g. anonymous) — fall back to process identity.
		msg, err := g.a.Send(ctx, to, method, params)
		if err != nil {
			return nil, err
		}
		return &messageWrapper{msg}, nil
	}
	msg, err := g.a.SendAs(ctx, ident, to, method, params)
	if err != nil {
		return nil, err
	}
	return &messageWrapper{msg}, nil
}

func (g *gatewayBridge) SendAsync(ctx context.Context, to, method string, params any) (string, error) {
	ident, err := g.tenantIdentity(ctx)
	if err != nil {
		id, err := g.a.SendAsync(ctx, to, method, params)
		if err != nil {
			return "", err
		}
		return id.String(), nil
	}
	id, err := g.a.SendAsAsync(ctx, ident, to, method, params)
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// tenantIdentity returns the derived identity for the tenant in ctx.
// On first call for a tenant it registers the tenant DID as a subordinate in
// the relay registry so recipients can resolve the public keys.
func (g *gatewayBridge) tenantIdentity(ctx context.Context) (*identity.Identity, error) {
	tenant := billing.TenantFromContext(ctx)
	if tenant == nil {
		return nil, errors.New("no tenant in context")
	}
	if len(tenant.DIDSeed) != 32 {
		return nil, fmt.Errorf("tenant %s has no DID seed (pre-V5 account)", tenant.ID)
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if ident, ok := g.identCache[tenant.ID]; ok {
		return ident, nil
	}

	ident, err := billing.DeriveTenantIdentity(g.domain, tenant.ID, tenant.DIDSeed)
	if err != nil {
		return nil, fmt.Errorf("derive tenant identity: %w", err)
	}

	if g.identCache == nil {
		g.identCache = make(map[string]*identity.Identity)
	}
	g.identCache[tenant.ID] = ident

	// Lazy subordinate registration — fire-and-forget so we don't block the
	// first tool call. The relay stores the public keys for recipient lookup.
	if !g.registered[tenant.ID] {
		if g.registered == nil {
			g.registered = make(map[string]bool)
		}
		g.registered[tenant.ID] = true
		// #nosec G118 -- fire-and-forget registration must outlive the request, so context.Background is intentional
		go g.registerSubordinate(ident, tenant.ID)
	}

	return ident, nil
}

func (g *gatewayBridge) registerSubordinate(ident *identity.Identity, tenantID string) {
	subKeys := []registry.PublicKey{
		{
			ID:      uuid.NewString(),
			Type:    registry.KeyTypeEd25519,
			Key:     ident.SigningPublicKey(),
			Purpose: "signing",
		},
		{
			ID:      uuid.NewString(),
			Type:    registry.KeyTypeX25519,
			Key:     ident.EncryptionPublicKey(),
			Purpose: "encryption",
		},
	}
	params := map[string]any{
		"did":         ident.String(),
		"tenant_id":   tenantID,
		"public_keys": subKeys,
	}
	ctx := context.Background()
	if _, err := g.a.CallRelay(ctx, "relay.register_subordinate", params); err != nil {
		// Non-fatal: next Send will still carry ActorProof; the relay can still
		// route the message even if key lookup fails for the recipient.
		_ = err
	}
}
