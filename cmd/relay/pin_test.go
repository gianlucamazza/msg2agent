package main

import (
	"encoding/json"
	"testing"

	"github.com/gianlucamazza/msg2agent/pkg/protocol"
	"github.com/gianlucamazza/msg2agent/pkg/registry"
)

func registerWithKey(t *testing.T, hub *RelayHub, id string, key []byte) *protocol.JSONRPCResponse {
	t.Helper()
	client := testClient(hub, id, "")
	hub.Register(client)
	agent := registry.Agent{
		DID:        "did:wba:localhost:agent:alice",
		PublicKeys: []registry.PublicKey{{ID: "k", Type: "Ed25519", Key: key, Purpose: "signing"}},
	}
	req, _ := protocol.NewRequest("1", "relay.register", agent)
	client.handleRegister(req)
	var resp protocol.JSONRPCResponse
	if err := json.Unmarshal(<-client.SendCh, &resp); err != nil {
		t.Fatal(err)
	}
	return &resp
}

func TestPinDIDKeys(t *testing.T) {
	hub := testHub()
	hub.config.PinDIDKeys = true
	if resp := registerWithKey(t, hub, "c1", []byte("key-one")); resp.Error != nil {
		t.Fatalf("first registration: %v", resp.Error)
	}
	if resp := registerWithKey(t, hub, "c2", []byte("key-one")); resp.Error != nil {
		t.Fatalf("same key re-registration: %v", resp.Error)
	}
	resp := registerWithKey(t, hub, "c3", []byte("key-two"))
	if resp.Error == nil || resp.Error.Message != ErrDIDKeyMismatch.Error() {
		t.Fatalf("different key must be rejected, got %+v", resp.Error)
	}
}

func TestPinDIDKeysOffAllowsRekey(t *testing.T) {
	hub := testHub()
	registerWithKey(t, hub, "c1", []byte("key-one"))
	if resp := registerWithKey(t, hub, "c2", []byte("key-two")); resp.Error != nil {
		t.Fatalf("pinning off must keep the old behaviour: %v", resp.Error)
	}
}
