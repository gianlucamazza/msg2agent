// Package oauth implements an OAuth 2.1 Authorization Server for msg2agent.
// It provides RFC 8414 AS metadata, RFC 7591 Dynamic Client Registration,
// PKCE S256 authorization, JWT issuance, and JWKS publication.
package oauth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"

	"github.com/lestrrat-go/jwx/v2/jwk"
)

// LoadOrGenerateEd25519 reads an Ed25519 private key from path (PEM, PKCS#8 raw format).
// If the file does not exist it generates a new key pair, writes it, and returns it.
// The file is created with 0600 permissions and must be on a persistent volume.
func LoadOrGenerateEd25519(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("oauth: read signing key %s: %w", path, err)
		}
		return generateAndSave(path)
	}
	return parsePEM(data)
}

func generateAndSave(path string) (ed25519.PrivateKey, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("oauth: generate Ed25519 key: %w", err)
	}
	block := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: []byte(priv), // raw 64-byte seed||pub
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
		return nil, fmt.Errorf("oauth: write signing key %s: %w", path, err)
	}
	return priv, nil
}

func parsePEM(data []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("oauth: no PEM block in signing key file")
	}
	if len(block.Bytes) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("oauth: signing key PEM has wrong size %d (want %d)", len(block.Bytes), ed25519.PrivateKeySize)
	}
	return ed25519.PrivateKey(block.Bytes), nil
}

// BuildJWK constructs a JWK set from an Ed25519 private key.
// The kid is the first 16 hex chars of SHA-256(publicKey) — stable across restarts.
func BuildJWK(priv ed25519.PrivateKey) (jwk.Set, string, error) {
	pub := priv.Public().(ed25519.PublicKey)
	sum := sha256.Sum256(pub)
	kid := hex.EncodeToString(sum[:8])

	key, err := jwk.FromRaw(pub)
	if err != nil {
		return nil, "", fmt.Errorf("oauth: build JWK from Ed25519 public key: %w", err)
	}
	if err := key.Set(jwk.KeyIDKey, kid); err != nil {
		return nil, "", fmt.Errorf("oauth: set kid: %w", err)
	}
	if err := key.Set(jwk.AlgorithmKey, "EdDSA"); err != nil {
		return nil, "", fmt.Errorf("oauth: set alg: %w", err)
	}
	if err := key.Set(jwk.KeyUsageKey, "sig"); err != nil {
		return nil, "", fmt.Errorf("oauth: set use: %w", err)
	}

	set := jwk.NewSet()
	if err := set.AddKey(key); err != nil {
		return nil, "", fmt.Errorf("oauth: add key to set: %w", err)
	}
	return set, kid, nil
}
