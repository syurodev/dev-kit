package crypto

import (
	"crypto/rand"
	"errors"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
)

// SealV2 encrypts plaintext using canonical record identity as associated
// data. The record ID must already be allocated before calling this function.
func SealV2(associatedData AssociatedData, key []byte, plaintext []byte) (Envelope, error) {
	if err := validateAssociatedData(associatedData); err != nil {
		return Envelope{}, err
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return Envelope{}, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return Envelope{}, err
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, canonicalAssociatedData(associatedData))
	envelope := Envelope{
		Version:    EnvelopeVersionV2,
		Algorithm:  AlgorithmXChaCha20Poly1305,
		KeyID:      associatedData.KeyID,
		Nonce:      nonce,
		Ciphertext: ciphertext,
	}
	return envelope, envelope.Validate()
}

// OpenWithAD verifies and decrypts a V2 envelope using its record binding.
func OpenWithAD(key []byte, envelope Envelope, associatedData AssociatedData) ([]byte, error) {
	if envelope.Version != EnvelopeVersionV2 {
		return nil, errors.New("envelope version is unsupported")
	}
	if err := validateAssociatedData(associatedData); err != nil {
		return nil, err
	}
	if associatedData.KeyID != envelope.KeyID {
		return nil, errors.New("associated data key ID does not match envelope")
	}
	return open(key, envelope, canonicalAssociatedData(associatedData))
}

func open(key []byte, envelope Envelope, associatedData []byte) ([]byte, error) {
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
	return aead.Open(nil, envelope.Nonce, envelope.Ciphertext, associatedData)
}

func validateAssociatedData(associatedData AssociatedData) error {
	if associatedData.RecordType == "" {
		return errors.New("associated data record type is required")
	}
	if associatedData.RecordID == "" {
		return errors.New("associated data record ID is required")
	}
	if associatedData.KeyID == "" {
		return errors.New("associated data key ID is required")
	}
	if strings.ContainsRune(associatedData.RecordType, '\x00') ||
		strings.ContainsRune(associatedData.RecordID, '\x00') ||
		strings.ContainsRune(associatedData.KeyID, '\x00') {
		return errors.New("associated data must not contain NUL")
	}
	return nil
}

func canonicalAssociatedData(associatedData AssociatedData) []byte {
	return []byte(associatedData.RecordType + "\x00" + associatedData.RecordID + "\x00" + associatedData.KeyID)
}
