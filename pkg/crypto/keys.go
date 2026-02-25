// Package crypto provides cryptographic operations for agent communication.
package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"

	"golang.org/x/crypto/curve25519"
)

// Key errors.
var (
	ErrInvalidKeySize    = errors.New("invalid key size")
	ErrKeyGenerationFail = errors.New("key generation failed")
)

// Key sizes.
const (
	Ed25519PublicKeySize  = ed25519.PublicKeySize  // 32 bytes
	Ed25519PrivateKeySize = ed25519.PrivateKeySize // 64 bytes
	X25519KeySize         = 32
)

// KeyPair holds a public and private key pair.
type KeyPair struct {
	PublicKey  []byte
	PrivateKey []byte //nolint:gosec // intentional: crypto key material struct
}

// SigningKeyPair is an Ed25519 key pair for digital signatures.
type SigningKeyPair struct {
	KeyPair
}

// EncryptionKeyPair is an X25519 key pair for key exchange/encryption.
type EncryptionKeyPair struct {
	KeyPair
}

// GenerateSigningKeyPair generates a new Ed25519 key pair for signing.
func GenerateSigningKeyPair() (*SigningKeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &SigningKeyPair{
		KeyPair: KeyPair{
			PublicKey:  pub,
			PrivateKey: priv,
		},
	}, nil
}

// GenerateEncryptionKeyPair generates a new X25519 key pair for encryption.
func GenerateEncryptionKeyPair() (*EncryptionKeyPair, error) {
	var privateKey [X25519KeySize]byte
	if _, err := io.ReadFull(rand.Reader, privateKey[:]); err != nil {
		return nil, err
	}

	publicKey, err := curve25519.X25519(privateKey[:], curve25519.Basepoint)
	if err != nil {
		return nil, err
	}

	return &EncryptionKeyPair{
		KeyPair: KeyPair{
			PublicKey:  publicKey,
			PrivateKey: privateKey[:],
		},
	}, nil
}

// AgentKeys holds all cryptographic keys for an agent.
type AgentKeys struct {
	Signing    *SigningKeyPair
	Encryption *EncryptionKeyPair
}

// GenerateAgentKeys generates a complete set of keys for an agent.
func GenerateAgentKeys() (*AgentKeys, error) {
	signing, err := GenerateSigningKeyPair()
	if err != nil {
		return nil, err
	}

	encryption, err := GenerateEncryptionKeyPair()
	if err != nil {
		return nil, err
	}

	return &AgentKeys{
		Signing:    signing,
		Encryption: encryption,
	}, nil
}

// Sign signs the data with the private key.
func (k *SigningKeyPair) Sign(data []byte) []byte {
	return ed25519.Sign(k.PrivateKey, data)
}

// Verify verifies the signature against the data and public key.
func (k *SigningKeyPair) Verify(data, signature []byte) bool {
	return ed25519.Verify(k.PublicKey, data, signature)
}

// VerifySignature verifies a signature using only the public key.
func VerifySignature(publicKey, data, signature []byte) bool {
	if len(publicKey) != Ed25519PublicKeySize {
		return false
	}
	return ed25519.Verify(publicKey, data, signature)
}

// ComputeSharedSecret computes a shared secret using X25519 key exchange.
func (k *EncryptionKeyPair) ComputeSharedSecret(peerPublicKey []byte) ([]byte, error) {
	if len(peerPublicKey) != X25519KeySize {
		return nil, ErrInvalidKeySize
	}
	return curve25519.X25519(k.PrivateKey, peerPublicKey)
}
