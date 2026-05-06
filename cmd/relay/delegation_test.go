package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/gianlucamazza/msg2agent/pkg/messaging"
	"github.com/gianlucamazza/msg2agent/pkg/protocol"
	"github.com/gianlucamazza/msg2agent/pkg/registry"
)

// makeEd25519Pair generates a fresh Ed25519 signing key pair for tests.
func makeEd25519Pair(t *testing.T) (pub, priv []byte) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	return pub, priv
}

// gatewayClient builds a Client pre-configured as a gateway with the given namespace + signing key.
func gatewayClient(hub *RelayHub, gwDID, namespace string, signingPub []byte) *Client {
	c := testClient(hub, "gw-conn", gwDID)
	c.gatewayNamespace = namespace
	c.gatewaySigningKey = signingPub
	return c
}

// signActorProof signs the delegation proof over (actor_did + ":" + from + ":" + msg_id).
func signActorProof(privKey []byte, actorDID, fromDID, msgID string) []byte {
	input := []byte(actorDID + ":" + fromDID + ":" + msgID)
	return ed25519.Sign(privKey, input)
}

// ────────────────────────────────────────────────────────────────────────────
// validateSender — delegation path tests
// ────────────────────────────────────────────────────────────────────────────

func TestValidateSenderDelegation_ValidProof(t *testing.T) {
	hub := testHub()
	gwPub, gwPriv := makeEd25519Pair(t)

	gwDID := "did:wba:example.com:agent:gateway"
	tenantDID := "did:wba:example.com:tenant:t_001"
	namespace := "did:wba:example.com:tenant:*"

	c := gatewayClient(hub, gwDID, namespace, gwPub)
	msgID := uuid.Must(uuid.NewV7())
	proof := signActorProof(gwPriv, gwDID, tenantDID, msgID.String())

	msg := &messaging.Message{
		ID:         msgID,
		From:       tenantDID,
		To:         "did:wba:example.com:agent:bob",
		ActorDID:   gwDID,
		ActorProof: proof,
	}

	if err := c.validateSender(msg); err != nil {
		t.Errorf("valid delegation should be accepted, got: %v", err)
	}
}

func TestValidateSenderDelegation_NamespaceMismatch(t *testing.T) {
	hub := testHub()
	gwPub, gwPriv := makeEd25519Pair(t)

	gwDID := "did:wba:example.com:agent:gateway"
	// From DID is NOT in the namespace.
	tenantDID := "did:wba:other.com:tenant:t_999"
	namespace := "did:wba:example.com:tenant:*"

	c := gatewayClient(hub, gwDID, namespace, gwPub)
	msgID := uuid.Must(uuid.NewV7())
	proof := signActorProof(gwPriv, gwDID, tenantDID, msgID.String())

	msg := &messaging.Message{
		ID:         msgID,
		From:       tenantDID,
		To:         "did:wba:example.com:agent:bob",
		ActorDID:   gwDID,
		ActorProof: proof,
	}

	err := c.validateSender(msg)
	if err == nil {
		t.Error("expected error for out-of-namespace DID, got nil")
	}
}

func TestValidateSenderDelegation_WrongKey(t *testing.T) {
	hub := testHub()
	gwPub, _ := makeEd25519Pair(t)
	_, otherPriv := makeEd25519Pair(t)

	gwDID := "did:wba:example.com:agent:gateway"
	tenantDID := "did:wba:example.com:tenant:t_001"
	namespace := "did:wba:example.com:tenant:*"

	c := gatewayClient(hub, gwDID, namespace, gwPub)
	msgID := uuid.Must(uuid.NewV7())
	// Proof signed by a different (unregistered) key.
	proof := signActorProof(otherPriv, gwDID, tenantDID, msgID.String())

	msg := &messaging.Message{
		ID:         msgID,
		From:       tenantDID,
		To:         "did:wba:example.com:agent:bob",
		ActorDID:   gwDID,
		ActorProof: proof,
	}

	err := c.validateSender(msg)
	if err == nil {
		t.Error("expected error for wrong signing key, got nil")
	}
}

func TestValidateSenderDelegation_ActorDIDMismatch(t *testing.T) {
	hub := testHub()
	gwPub, gwPriv := makeEd25519Pair(t)

	gwDID := "did:wba:example.com:agent:gateway"
	tenantDID := "did:wba:example.com:tenant:t_001"
	namespace := "did:wba:example.com:tenant:*"

	// Client registered as gateway, but the message's ActorDID points elsewhere.
	c := gatewayClient(hub, gwDID, namespace, gwPub)
	msgID := uuid.Must(uuid.NewV7())
	proof := signActorProof(gwPriv, "did:wba:example.com:agent:impostor", tenantDID, msgID.String())

	msg := &messaging.Message{
		ID:         msgID,
		From:       tenantDID,
		To:         "did:wba:example.com:agent:bob",
		ActorDID:   "did:wba:example.com:agent:impostor", // != c.DID
		ActorProof: proof,
	}

	// Should fall through to ErrSenderMismatch because ActorDID != c.DID.
	err := c.validateSender(msg)
	if err == nil {
		t.Error("expected error when ActorDID doesn't match client DID")
	}
}

func TestValidateSenderDelegation_NoNamespaceOnClient(t *testing.T) {
	hub := testHub()
	gwDID := "did:wba:example.com:agent:plain"
	tenantDID := "did:wba:example.com:tenant:t_001"

	// Client registered without gateway namespace.
	c := testClient(hub, "conn", gwDID)
	msgID := uuid.Must(uuid.NewV7())

	msg := &messaging.Message{
		ID:       msgID,
		From:     tenantDID,
		To:       "did:wba:example.com:agent:bob",
		ActorDID: gwDID,
	}

	err := c.validateSender(msg)
	if err == nil {
		t.Error("client without gateway namespace must not delegate")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// didMatchesNamespace
// ────────────────────────────────────────────────────────────────────────────

func TestDidMatchesNamespace(t *testing.T) {
	tests := []struct {
		did, ns string
		want    bool
	}{
		{"did:wba:example.com:tenant:t_001", "did:wba:example.com:tenant:*", true},
		{"did:wba:example.com:tenant:", "did:wba:example.com:tenant:*", true},
		{"did:wba:other.com:tenant:t_001", "did:wba:example.com:tenant:*", false},
		{"did:wba:example.com:agent:x", "did:wba:example.com:tenant:*", false},
		{"did:wba:example.com:agent:x", "did:wba:example.com:agent:x", true},  // exact
		{"did:wba:example.com:agent:y", "did:wba:example.com:agent:x", false}, // exact mismatch
	}
	for _, tc := range tests {
		got := didMatchesNamespace(tc.did, tc.ns)
		if got != tc.want {
			t.Errorf("didMatchesNamespace(%q, %q) = %v, want %v", tc.did, tc.ns, got, tc.want)
		}
	}
}

// ────────────────────────────────────────────────────────────────────────────
// handleRegisterSubordinate
// ────────────────────────────────────────────────────────────────────────────

// sendAndCapture builds a Client with a buffered SendCh, invokes f, then reads
// the JSON-RPC response out of the SendCh.
func sendAndCapture(t *testing.T, c *Client, f func()) map[string]any {
	t.Helper()
	f()
	select {
	case raw := <-c.SendCh:
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		return out
	default:
		t.Fatal("no response sent to SendCh")
		return nil
	}
}

func buildSubordinateReq(id any, subDID string, keys []registry.PublicKey) *protocol.JSONRPCRequest {
	params := SubordinateRegisterRequest{
		DID:        subDID,
		PublicKeys: keys,
	}
	data, _ := json.Marshal(params)
	return &protocol.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "relay.register_subordinate",
		Params:  data,
	}
}

func TestHandleRegisterSubordinate_NonGatewayRejected(t *testing.T) {
	hub := testHub()
	c := testClient(hub, "conn", "did:wba:example.com:agent:plain")
	c.SendCh = make(chan []byte, 10)

	req := buildSubordinateReq(1, "did:wba:example.com:tenant:t_001", nil)
	resp := sendAndCapture(t, c, func() { c.handleRegisterSubordinate(req) })

	if resp["error"] == nil {
		t.Error("expected error for non-gateway client")
	}
}

func TestHandleRegisterSubordinate_NotRegisteredRejected(t *testing.T) {
	hub := testHub()
	c := testClient(hub, "conn", "") // not registered (no DID)
	c.SendCh = make(chan []byte, 10)

	req := buildSubordinateReq(1, "did:wba:example.com:tenant:t_001", nil)
	resp := sendAndCapture(t, c, func() { c.handleRegisterSubordinate(req) })

	if resp["error"] == nil {
		t.Error("expected error for unregistered client")
	}
}

func TestHandleRegisterSubordinate_OutOfNamespaceRejected(t *testing.T) {
	hub := testHub()
	gwPub, _ := makeEd25519Pair(t)
	c := gatewayClient(hub, "did:wba:example.com:agent:gw", "did:wba:example.com:tenant:*", gwPub)
	c.SendCh = make(chan []byte, 10)

	// DID is not in the gateway's namespace.
	req := buildSubordinateReq(1, "did:wba:other.com:tenant:t_001", nil)
	resp := sendAndCapture(t, c, func() { c.handleRegisterSubordinate(req) })

	if resp["error"] == nil {
		t.Error("expected error for out-of-namespace subordinate DID")
	}
}

func TestHandleRegisterSubordinate_ValidRequest(t *testing.T) {
	hub := testHub()
	gwPub, _ := makeEd25519Pair(t)
	tenantPub, _ := makeEd25519Pair(t)

	gwDID := "did:wba:example.com:agent:gw"
	tenantDID := "did:wba:example.com:tenant:t_new"

	c := gatewayClient(hub, gwDID, "did:wba:example.com:tenant:*", gwPub)
	c.SendCh = make(chan []byte, 10)

	keys := []registry.PublicKey{
		{ID: uuid.NewString(), Type: registry.KeyTypeEd25519, Key: tenantPub, Purpose: "signing"},
	}
	req := buildSubordinateReq(1, tenantDID, keys)
	resp := sendAndCapture(t, c, func() { c.handleRegisterSubordinate(req) })

	if resp["error"] != nil {
		t.Fatalf("unexpected error: %v", resp["error"])
	}

	// Tenant agent must now be in the registry.
	agent, err := hub.store.GetByDID(tenantDID)
	if err != nil {
		t.Fatalf("tenant DID not in registry after subordinate registration: %v", err)
	}
	if agent.Status != registry.StatusOffline {
		t.Errorf("subordinate agent status = %q, want %q", agent.Status, registry.StatusOffline)
	}
	if len(agent.PublicKeys) != 1 || string(agent.PublicKeys[0].Key) != string(tenantPub) {
		t.Error("subordinate agent public keys do not match what was registered")
	}
}
