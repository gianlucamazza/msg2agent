package agent

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"testing"
	"time"

	"github.com/gianlucamazza/msg2agent/pkg/billing"
	"github.com/gianlucamazza/msg2agent/pkg/identity"
	"github.com/gianlucamazza/msg2agent/pkg/messaging"
	"github.com/gianlucamazza/msg2agent/pkg/protocol"
	"github.com/gianlucamazza/msg2agent/pkg/registry"
)

// setupGatewayAgent creates an agent wired to a mock transport acting as the relay.
func setupGatewayAgent(t *testing.T) (*Agent, *mockTransport) {
	t.Helper()
	a, err := New(Config{Domain: "example.com", AgentID: "gateway"})
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := a.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { a.Stop() }) //nolint:errcheck

	mock := &mockTransport{
		addr:      "ws://relay:8080",
		connected: true,
		incoming:  make(chan []byte, 100),
		outgoing:  make(chan []byte, 100),
	}
	a.mu.Lock()
	a.relayAddr = "ws://relay:8080"
	a.peers["ws://relay:8080"] = mock
	a.mu.Unlock()
	return a, mock
}

// makeTenantIdent builds a deterministic *identity.Identity from an index byte.
func makeTenantIdent(t *testing.T, tenantID string, seed0 byte) *identity.Identity {
	t.Helper()
	seed := make([]byte, 32)
	seed[0] = seed0
	ident, err := billing.DeriveTenantIdentity("example.com", tenantID, seed)
	if err != nil {
		t.Fatalf("DeriveTenantIdentity: %v", err)
	}
	return ident
}

// decodeOutgoingMessage reads one item from mock.outgoing and parses it as a messaging.Message.
func decodeOutgoingMessage(t *testing.T, mock *mockTransport) messaging.Message {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	raw, err := mock.getOutgoing(ctx)
	if err != nil {
		t.Fatalf("getOutgoing: %v", err)
	}
	var rpc protocol.JSONRPCRequest
	if err := json.Unmarshal(raw, &rpc); err != nil {
		t.Fatalf("unmarshal rpc: %v", err)
	}
	var msg messaging.Message
	if err := rpc.ParseParams(&msg); err != nil {
		t.Fatalf("parse params: %v", err)
	}
	return msg
}

// gatewaySigningPub extracts the Ed25519 signing public key from an agent's registry record.
func gatewaySigningPub(a *Agent) []byte {
	for _, k := range a.Record().PublicKeys {
		if k.Purpose == "signing" {
			return k.Key
		}
	}
	return nil
}

func TestSendAsAsync_From(t *testing.T) {
	gw, mock := setupGatewayAgent(t)
	tenantIdent := makeTenantIdent(t, "t_test1", 42)

	_, err := gw.SendAsAsync(context.Background(), tenantIdent, "did:wba:example.com:agent:bob", "ping", nil)
	if err != nil {
		t.Fatalf("SendAsAsync: %v", err)
	}

	msg := decodeOutgoingMessage(t, mock)

	if msg.From != tenantIdent.String() {
		t.Errorf("From = %q, want %q", msg.From, tenantIdent.String())
	}
	if msg.ActorDID != gw.DID() {
		t.Errorf("ActorDID = %q, want %q", msg.ActorDID, gw.DID())
	}
	if len(msg.ActorProof) == 0 {
		t.Error("ActorProof must not be empty")
	}
	if !msg.RequestAck {
		t.Error("SendAsAsync must set RequestAck = true")
	}
}

func TestSendAsAsync_MessageSignatureVerifiable(t *testing.T) {
	gw, mock := setupGatewayAgent(t)
	tenantIdent := makeTenantIdent(t, "t_sigtest", 77)

	_, err := gw.SendAsAsync(context.Background(), tenantIdent, "did:wba:example.com:agent:bob", "test", map[string]string{"k": "v"})
	if err != nil {
		t.Fatalf("SendAsAsync: %v", err)
	}

	msg := decodeOutgoingMessage(t, mock)

	// The signature domain excludes Signature, ActorDID, ActorProof (gateway envelope).
	sig := msg.Signature
	msg.Signature = nil
	msg.ActorDID = ""
	msg.ActorProof = nil
	msgBytes, _ := json.Marshal(msg)
	if !ed25519.Verify(tenantIdent.SigningPublicKey(), msgBytes, sig) {
		t.Error("Signature does not verify against tenant's signing public key")
	}
}

func TestSendAsAsync_ActorProofVerifiable(t *testing.T) {
	gw, mock := setupGatewayAgent(t)
	tenantIdent := makeTenantIdent(t, "t_prooftest", 55)

	_, err := gw.SendAsAsync(context.Background(), tenantIdent, "did:wba:example.com:agent:bob", "test", nil)
	if err != nil {
		t.Fatalf("SendAsAsync: %v", err)
	}

	msg := decodeOutgoingMessage(t, mock)

	// ActorProof = gateway.Sign(actor_did + ":" + from + ":" + msg_id)
	proofInput := []byte(gw.DID() + ":" + tenantIdent.String() + ":" + msg.ID.String())
	gwPub := gatewaySigningPub(gw)
	if gwPub == nil {
		t.Fatal("gateway has no signing public key in record")
	}
	if !ed25519.Verify(gwPub, proofInput, msg.ActorProof) {
		t.Error("ActorProof does not verify against gateway's signing public key")
	}
}

func TestSendAsAsync_FromAndActorDIDAreDistinct(t *testing.T) {
	gw, mock := setupGatewayAgent(t)
	tenantIdent := makeTenantIdent(t, "t_distinct", 99)

	gw.SendAsAsync(context.Background(), tenantIdent, "did:wba:example.com:agent:bob", "test", nil)
	msg := decodeOutgoingMessage(t, mock)

	if msg.From == msg.ActorDID {
		t.Errorf("From (%q) must differ from ActorDID (%q)", msg.From, msg.ActorDID)
	}
}

func TestSendAsAsync_DifferentTenantsProduceDifferentFromDIDs(t *testing.T) {
	gw, mock := setupGatewayAgent(t)

	identA := makeTenantIdent(t, "t_A", 0xAA)
	gw.SendAsAsync(context.Background(), identA, "did:wba:example.com:agent:bob", "m", nil)
	msgA := decodeOutgoingMessage(t, mock)

	identB := makeTenantIdent(t, "t_B", 0xBB)
	gw.SendAsAsync(context.Background(), identB, "did:wba:example.com:agent:bob", "m", nil)
	msgB := decodeOutgoingMessage(t, mock)

	if msgA.From == msgB.From {
		t.Errorf("distinct tenants share the same From DID: %q", msgA.From)
	}
}

// Compile-time import guard.
var _ registry.PublicKey
