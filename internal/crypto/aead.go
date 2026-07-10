package crypto

import (
	"crypto/rand"
	"errors"

	"golang.org/x/crypto/chacha20poly1305"
)

// Seal encrypts plaintext with key using XChaCha20-Poly1305 and returns a
// versioned Envelope. key must be 32 bytes.
func Seal(keyID string, key []byte, plaintext []byte) (Envelope, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return Envelope{}, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return Envelope{}, err
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, nil)
	envelope := Envelope{
		Version:    1,
		Algorithm:  AlgorithmXChaCha20Poly1305,
		KeyID:      keyID,
		Nonce:      nonce,
		Ciphertext: ciphertext,
	}
	return envelope, envelope.Validate()
}

// Open verifies and decrypts an Envelope produced by Seal.
func Open(key []byte, envelope Envelope) ([]byte, error) {
	if err := envelope.Validate(); err != nil {
		return nil, err
	}
	if envelope.Algorithm != AlgorithmXChaCha20Poly1305 {
		return nil, errors.New("unsupported aead algorithm")
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	return aead.Open(nil, envelope.Nonce, envelope.Ciphertext, nil)
}
