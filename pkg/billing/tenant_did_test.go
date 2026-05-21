package billing

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"testing"
)

func TestDeriveTenantIdentity_Deterministic(t *testing.T) {
	seed, _ := hex.DecodeString("0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20")
	id1, err := DeriveTenantIdentity("example.com", "t_abc", seed)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	id2, err := DeriveTenantIdentity("example.com", "t_abc", seed)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if id1.String() != id2.String() {
		t.Errorf("DID not deterministic: %q vs %q", id1, id2)
	}
	if !bytes.Equal(id1.SigningPublicKey(), id2.SigningPublicKey()) {
		t.Error("signing public key not deterministic")
	}
	if !bytes.Equal(id1.EncryptionPublicKey(), id2.EncryptionPublicKey()) {
		t.Error("encryption public key not deterministic")
	}
}

func TestDeriveTenantIdentity_DIDFormat(t *testing.T) {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	id, err := DeriveTenantIdentity("msg2agent.example.com", "t_123", seed)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	want := "did:wba:msg2agent.example.com:tenant:t_123"
	if id.String() != want {
		t.Errorf("DID = %q, want %q", id, want)
	}
}

func TestDeriveTenantIdentity_DifferentSeeds_DifferentKeys(t *testing.T) {
	seed1 := make([]byte, 32)
	seed2 := make([]byte, 32)
	seed2[0] = 1

	id1, err1 := DeriveTenantIdentity("example.com", "t_x", seed1)
	id2, err2 := DeriveTenantIdentity("example.com", "t_x", seed2)
	if err1 != nil || err2 != nil {
		t.Fatalf("derive errors: %v %v", err1, err2)
	}
	if bytes.Equal(id1.SigningPublicKey(), id2.SigningPublicKey()) {
		t.Error("different seeds produced identical signing keys")
	}
}

func TestDeriveTenantIdentity_InvalidSeedLength(t *testing.T) {
	for _, badLen := range []int{0, 16, 31, 33, 64} {
		seed := make([]byte, badLen)
		if _, err := DeriveTenantIdentity("example.com", "t_x", seed); err == nil {
			t.Errorf("expected error for seed length %d, got nil", badLen)
		}
	}
}

func TestDeriveTenantIdentity_SigningKeyMatchesEd25519Seed(t *testing.T) {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i * 3)
	}
	id, err := DeriveTenantIdentity("example.com", "t_x", seed)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	// Ed25519 public key from seed must match standard derivation.
	edPriv := ed25519.NewKeyFromSeed(seed)
	wantPub := []byte(edPriv.Public().(ed25519.PublicKey))
	if !bytes.Equal(id.SigningPublicKey(), wantPub) {
		t.Error("signing public key does not match ed25519.NewKeyFromSeed derivation")
	}
}

func TestDeriveTenantIdentity_KeysAreDistinct(t *testing.T) {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i + 7)
	}
	id, err := DeriveTenantIdentity("example.com", "t_x", seed)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if bytes.Equal(id.SigningPublicKey(), id.EncryptionPublicKey()) {
		t.Error("signing and encryption public keys must not be identical")
	}
	if len(id.SigningPublicKey()) != 32 {
		t.Errorf("signing key: want 32 bytes, got %d", len(id.SigningPublicKey()))
	}
	if len(id.EncryptionPublicKey()) != 32 {
		t.Errorf("encryption key: want 32 bytes, got %d", len(id.EncryptionPublicKey()))
	}
}

func TestNewTenant_GeneratesDIDSeed(t *testing.T) {
	tenant, err := NewTenant("Alice Corp", "alice@example.com", PlanFree)
	if err != nil {
		t.Fatalf("NewTenant: %v", err)
	}
	if len(tenant.DIDSeed) != 32 {
		t.Errorf("DIDSeed: want 32 bytes, got %d", len(tenant.DIDSeed))
	}
	// Different tenants must have different seeds.
	tenant2, err := NewTenant("Bob Corp", "bob@example.com", PlanFree)
	if err != nil {
		t.Fatalf("NewTenant: %v", err)
	}
	if bytes.Equal(tenant.DIDSeed, tenant2.DIDSeed) {
		t.Error("two distinct tenants share the same DIDSeed")
	}
}
