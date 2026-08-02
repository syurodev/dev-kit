package crypto

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	AlgorithmXChaCha20Poly1305 = "xchacha20-poly1305"
	AlgorithmAES256GCM         = "aes-256-gcm"
)

const (
	EnvelopeVersionV2 uint32 = 2
)

// AssociatedData is the immutable record identity authenticated by SealV2
// envelopes. It is not persisted in the envelope: the owning record supplies
// it when decrypting.
type AssociatedData struct {
	RecordType string
	RecordID   string
	KeyID      string
}

type Envelope struct {
	Version    uint32 `json:"version"`
	Algorithm  string `json:"algorithm"`
	KeyID      string `json:"key_id"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

func (e Envelope) Validate() error {
	if e.Version != EnvelopeVersionV2 {
		return errors.New("envelope version is unsupported")
	}
	if !isSupportedAlgorithm(e.Algorithm) {
		return errors.New("algorithm must be supported")
	}
	if e.KeyID == "" {
		return errors.New("key ID is required")
	}
	// XChaCha20-Poly1305 panics when Open receives a nonce with an invalid
	// length. Enforce the algorithm-specific size at the envelope boundary so
	// malformed or untrusted envelopes always fail with an error instead.
	if len(e.Nonce) != chacha20poly1305.NonceSizeX {
		return fmt.Errorf("nonce must be exactly %d bytes", chacha20poly1305.NonceSizeX)
	}
	// A sealed XChaCha20-Poly1305 value always contains at least the
	// authentication tag, even when the plaintext is empty.
	if len(e.Ciphertext) < chacha20poly1305.Overhead {
		return fmt.Errorf("ciphertext must be at least %d bytes", chacha20poly1305.Overhead)
	}

	return nil
}

func isSupportedAlgorithm(algorithm string) bool {
	switch algorithm {
	case AlgorithmXChaCha20Poly1305:
		return true
	default:
		return false
	}
}
