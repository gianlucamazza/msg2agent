// Package identity provides W3C DID support and identity management.
package identity

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	ssi "github.com/nuts-foundation/go-did"
	"github.com/nuts-foundation/go-did/did"
	"golang.org/x/crypto/curve25519"

	"github.com/gianluca/msg2agent/pkg/crypto"
)

// DID errors.
var (
	ErrInvalidDID        = errors.New("invalid DID")
	ErrUnsupportedMethod = errors.New("unsupported DID method")
)

const (
	// MethodWBA is the Web-Based Agent DID method (did:wba)
	MethodWBA = "wba"
)

// Identity represents an agent's decentralized identity.
type Identity struct {
	DID      did.DID
	Document *did.Document
	Keys     *crypto.AgentKeys
}

// NewIdentity creates a new identity with generated keys.
func NewIdentity(domain, agentID string) (*Identity, error) {
	keys, err := crypto.GenerateAgentKeys()
	if err != nil {
		return nil, fmt.Errorf("failed to generate keys: %w", err)
	}

	// Create DID: did:wba:domain:agent:id
	didStr := fmt.Sprintf("did:%s:%s:agent:%s", MethodWBA, domain, agentID)
	d, err := did.ParseDID(didStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DID: %w", err)
	}

	identity := &Identity{
		DID:  *d,
		Keys: keys,
	}

	// Build DID Document
	identity.Document = identity.buildDocument()

	return identity, nil
}

// serializedIdentity is the on-disk format for identity keys.
type serializedIdentity struct {
	SigningPrivateKey    []byte `json:"signing_private_key"`
	EncryptionPrivateKey []byte `json:"encryption_private_key"`
}

// SaveToFile persists the identity's private keys to a file.
func SaveToFile(ident *Identity, path string) error {
	data, err := json.Marshal(serializedIdentity{
		SigningPrivateKey:    ident.Keys.Signing.PrivateKey,
		EncryptionPrivateKey: ident.Keys.Encryption.PrivateKey,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal identity: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

// LoadFromFile loads an identity from a persisted key file.
func LoadFromFile(path, domain, agentID string) (*Identity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read identity file: %w", err)
	}

	var s serializedIdentity
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("failed to unmarshal identity: %w", err)
	}

	// Reconstruct Ed25519 public key (private key is seed||pub, 64 bytes)
	if len(s.SigningPrivateKey) != crypto.Ed25519PrivateKeySize {
		return nil, fmt.Errorf("invalid signing key size: %d", len(s.SigningPrivateKey))
	}
	signingPub := s.SigningPrivateKey[32:]

	// Reconstruct X25519 public key
	if len(s.EncryptionPrivateKey) != crypto.X25519KeySize {
		return nil, fmt.Errorf("invalid encryption key size: %d", len(s.EncryptionPrivateKey))
	}
	encryptionPub, err := curve25519.X25519(s.EncryptionPrivateKey, curve25519.Basepoint)
	if err != nil {
		return nil, fmt.Errorf("failed to derive encryption public key: %w", err)
	}

	keys := &crypto.AgentKeys{
		Signing: &crypto.SigningKeyPair{
			KeyPair: crypto.KeyPair{
				PublicKey:  signingPub,
				PrivateKey: s.SigningPrivateKey,
			},
		},
		Encryption: &crypto.EncryptionKeyPair{
			KeyPair: crypto.KeyPair{
				PublicKey:  encryptionPub,
				PrivateKey: s.EncryptionPrivateKey,
			},
		},
	}

	didStr := fmt.Sprintf("did:%s:%s:agent:%s", MethodWBA, domain, agentID)
	d, err := did.ParseDID(didStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DID: %w", err)
	}

	ident := &Identity{
		DID:  *d,
		Keys: keys,
	}
	ident.Document = ident.buildDocument()

	return ident, nil
}

// ParseDID parses a DID string.
func ParseDID(s string) (*did.DID, error) {
	d, err := did.ParseDID(s)
	if err != nil {
		return nil, ErrInvalidDID
	}
	return d, nil
}

// ValidateDID validates a DID string and returns true if valid.
func ValidateDID(s string) bool {
	_, err := did.ParseDID(s)
	return err == nil
}

// ExtractDomain extracts the domain from a did:wba DID.
func ExtractDomain(d *did.DID) (string, error) {
	if d.Method != MethodWBA {
		return "", ErrUnsupportedMethod
	}

	// did:wba:example.com:agent:123 -> example.com
	parts := strings.Split(d.ID, ":")
	if len(parts) < 1 {
		return "", ErrInvalidDID
	}

	// URL-decode the domain part
	domain, err := url.PathUnescape(parts[0])
	if err != nil {
		return "", ErrInvalidDID
	}

	return domain, nil
}

// X25519KeyAgreementKey2019 is the key type for X25519 key agreement.
const X25519KeyAgreementKey2019 = ssi.KeyType("X25519KeyAgreementKey2019")

// buildDocument builds a DID Document for the identity.
func (i *Identity) buildDocument() *did.Document {
	doc := &did.Document{
		Context: []interface{}{did.DIDContextV1URI()},
		ID:      i.DID,
	}

	// Add signing key
	signingKeyID := did.DIDURL{DID: i.DID, Fragment: "signing-key"}
	signingVM := &did.VerificationMethod{
		ID:              signingKeyID,
		Type:            ssi.ED25519VerificationKey2018,
		Controller:      i.DID,
		PublicKeyBase58: encodeBase58(i.Keys.Signing.PublicKey),
	}
	doc.AddAssertionMethod(signingVM)
	doc.AddAuthenticationMethod(signingVM)

	// Add encryption key
	encryptionKeyID := did.DIDURL{DID: i.DID, Fragment: "encryption-key"}
	encryptionVM := &did.VerificationMethod{
		ID:              encryptionKeyID,
		Type:            X25519KeyAgreementKey2019,
		Controller:      i.DID,
		PublicKeyBase58: encodeBase58(i.Keys.Encryption.PublicKey),
	}
	doc.AddKeyAgreement(encryptionVM)

	return doc
}

// String returns the DID as a string.
func (i *Identity) String() string {
	return i.DID.String()
}

// SigningPublicKey returns the signing public key.
func (i *Identity) SigningPublicKey() []byte {
	return i.Keys.Signing.PublicKey
}

// EncryptionPublicKey returns the encryption public key.
func (i *Identity) EncryptionPublicKey() []byte {
	return i.Keys.Encryption.PublicKey
}

// Sign signs data with the identity's signing key.
func (i *Identity) Sign(data []byte) []byte {
	return i.Keys.Signing.Sign(data)
}

// Simple base58 encoding (Bitcoin alphabet)
const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func encodeBase58(data []byte) string {
	if len(data) == 0 {
		return ""
	}

	// Count leading zeros
	zeros := 0
	for _, b := range data {
		if b == 0 {
			zeros++
		} else {
			break
		}
	}

	// Allocate enough space
	size := len(data)*138/100 + 1
	buf := make([]byte, size)

	// Process bytes
	for _, b := range data {
		carry := int(b)
		for j := size - 1; j >= 0; j-- {
			carry += 256 * int(buf[j])
			buf[j] = byte(carry % 58)
			carry /= 58
		}
	}

	// Skip leading zeros in buf
	i := 0
	for i < size && buf[i] == 0 {
		i++
	}

	// Build result
	result := make([]byte, zeros+size-i)
	for j := 0; j < zeros; j++ {
		result[j] = '1'
	}
	for j := zeros; i < size; i, j = i+1, j+1 {
		result[j] = base58Alphabet[buf[i]]
	}

	return string(result)
}
