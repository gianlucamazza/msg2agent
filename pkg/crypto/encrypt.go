package crypto

import (
	"crypto/rand"
	"errors"
	"io"

	"golang.org/x/crypto/nacl/box"
)

// Encryption errors.
var (
	ErrDecryptionFailed = errors.New("decryption failed")
	ErrInvalidNonce     = errors.New("invalid nonce size")
)

// Encryption constants.
const (
	NonceSize   = 24 // NaCl nonce size
	OverheadLen = box.Overhead
)

// Encrypt encrypts a message using NaCl box (XSalsa20-Poly1305).
// It uses the sender's private key and recipient's public key.
func Encrypt(plaintext, senderPrivateKey, recipientPublicKey []byte) ([]byte, error) {
	if len(senderPrivateKey) != X25519KeySize || len(recipientPublicKey) != X25519KeySize {
		return nil, ErrInvalidKeySize
	}

	var nonce [NonceSize]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return nil, err
	}

	var senderPriv, recipientPub [X25519KeySize]byte
	copy(senderPriv[:], senderPrivateKey)
	copy(recipientPub[:], recipientPublicKey)

	// Prepend nonce to ciphertext
	ciphertext := make([]byte, NonceSize)
	copy(ciphertext, nonce[:])

	ciphertext = box.Seal(ciphertext, plaintext, &nonce, &recipientPub, &senderPriv)
	return ciphertext, nil
}

// Decrypt decrypts a message using NaCl box (XSalsa20-Poly1305).
// It uses the recipient's private key and sender's public key.
func Decrypt(ciphertext, recipientPrivateKey, senderPublicKey []byte) ([]byte, error) {
	if len(recipientPrivateKey) != X25519KeySize || len(senderPublicKey) != X25519KeySize {
		return nil, ErrInvalidKeySize
	}

	if len(ciphertext) < NonceSize+OverheadLen {
		return nil, ErrDecryptionFailed
	}

	var nonce [NonceSize]byte
	copy(nonce[:], ciphertext[:NonceSize])

	var recipientPriv, senderPub [X25519KeySize]byte
	copy(recipientPriv[:], recipientPrivateKey)
	copy(senderPub[:], senderPublicKey)

	plaintext, ok := box.Open(nil, ciphertext[NonceSize:], &nonce, &senderPub, &recipientPriv)
	if !ok {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

// EncryptAnonymous encrypts a message for a recipient without revealing the sender.
// Uses ephemeral key pair for the sender.
func EncryptAnonymous(plaintext, recipientPublicKey []byte) ([]byte, error) {
	if len(recipientPublicKey) != X25519KeySize {
		return nil, ErrInvalidKeySize
	}

	// Generate ephemeral key pair
	ephemeralPub, ephemeralPriv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	var nonce [NonceSize]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return nil, err
	}

	var recipientPub [X25519KeySize]byte
	copy(recipientPub[:], recipientPublicKey)

	// Format: ephemeral_public_key || nonce || ciphertext
	result := make([]byte, X25519KeySize+NonceSize)
	copy(result[:X25519KeySize], ephemeralPub[:])
	copy(result[X25519KeySize:], nonce[:])

	result = box.Seal(result, plaintext, &nonce, &recipientPub, ephemeralPriv)
	return result, nil
}

// DecryptAnonymous decrypts an anonymous message.
func DecryptAnonymous(ciphertext, recipientPrivateKey []byte) ([]byte, error) {
	if len(recipientPrivateKey) != X25519KeySize {
		return nil, ErrInvalidKeySize
	}

	if len(ciphertext) < X25519KeySize+NonceSize+OverheadLen {
		return nil, ErrDecryptionFailed
	}

	var ephemeralPub [X25519KeySize]byte
	copy(ephemeralPub[:], ciphertext[:X25519KeySize])

	var nonce [NonceSize]byte
	copy(nonce[:], ciphertext[X25519KeySize:X25519KeySize+NonceSize])

	var recipientPriv [X25519KeySize]byte
	copy(recipientPriv[:], recipientPrivateKey)

	plaintext, ok := box.Open(nil, ciphertext[X25519KeySize+NonceSize:], &nonce, &ephemeralPub, &recipientPriv)
	if !ok {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}
