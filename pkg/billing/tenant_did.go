package billing

import (
	"crypto/ed25519"
	"crypto/sha512"
	"fmt"

	"golang.org/x/crypto/curve25519"

	pkgcrypto "github.com/gianlucamazza/msg2agent/pkg/crypto"
	"github.com/gianlucamazza/msg2agent/pkg/identity"
)

// TenantDIDString returns the canonical DID for a billing tenant.
// Form: did:wba:<domain>:tenant:<tenantID>
func TenantDIDString(domain, tenantID string) string {
	return fmt.Sprintf("did:wba:%s:tenant:%s", domain, tenantID)
}

// DeriveTenantIdentity creates a deterministic Ed25519 + X25519 identity from a
// 32-byte seed stored in the billing DB. The same seed always produces the same
// key pair, so tenant DIDs are stable across restarts.
//
// Key derivation:
//   - Ed25519 private key: RFC 8032 §5.1.5 — ed25519.NewKeyFromSeed(seed)
//   - X25519 private key: SHA-512(seed)[0:32] clamped per RFC 7748 §5
func DeriveTenantIdentity(domain, tenantID string, seed []byte) (*identity.Identity, error) {
	if len(seed) != 32 {
		return nil, fmt.Errorf("billing: did seed must be 32 bytes, got %d", len(seed))
	}

	// Ed25519 from seed.
	edPriv := ed25519.NewKeyFromSeed(seed)
	signingPriv := []byte(edPriv)                             // 64 bytes (seed||pub)
	signingPub := []byte(edPriv.Public().(ed25519.PublicKey)) // 32 bytes

	// X25519 from SHA-512(seed)[0:32] — same derivation as libsodium sign_to_curve25519.
	h := sha512.Sum512(seed)
	encPriv := make([]byte, 32)
	copy(encPriv, h[:32])
	encPriv[0] &= 248
	encPriv[31] &= 127
	encPriv[31] |= 64
	encPub, err := curve25519.X25519(encPriv, curve25519.Basepoint)
	if err != nil {
		return nil, fmt.Errorf("billing: X25519 derivation: %w", err)
	}

	keys := &pkgcrypto.AgentKeys{
		Signing: &pkgcrypto.SigningKeyPair{
			KeyPair: pkgcrypto.KeyPair{PublicKey: signingPub, PrivateKey: signingPriv},
		},
		Encryption: &pkgcrypto.EncryptionKeyPair{
			KeyPair: pkgcrypto.KeyPair{PublicKey: encPub, PrivateKey: encPriv},
		},
	}

	return identity.NewIdentityFromKeysTenant(domain, tenantID, keys)
}
