package crypto

import (
	"testing"

	"golang.org/x/crypto/chacha20poly1305"
)

func TestEnvelopeValidateAcceptsSupportedEnvelope(t *testing.T) {
	envelope := Envelope{
		Version:    EnvelopeVersionV2,
		Algorithm:  AlgorithmXChaCha20Poly1305,
		KeyID:      "key-1",
		Nonce:      make([]byte, chacha20poly1305.NonceSizeX),
		Ciphertext: make([]byte, chacha20poly1305.Overhead),
	}

	if err := envelope.Validate(); err != nil {
		t.Fatalf("Validate() returned error: %v", err)
	}
}

func TestEnvelopeValidateRejectsMalformedXChaCha20Poly1305Sizes(t *testing.T) {
	tests := []struct {
		name       string
		nonce      []byte
		ciphertext []byte
	}{
		{
			name:       "short nonce",
			nonce:      make([]byte, chacha20poly1305.NonceSizeX-1),
			ciphertext: make([]byte, chacha20poly1305.Overhead),
		},
		{
			name:       "long nonce",
			nonce:      make([]byte, chacha20poly1305.NonceSizeX+1),
			ciphertext: make([]byte, chacha20poly1305.Overhead),
		},
		{
			name:       "ciphertext shorter than authentication tag",
			nonce:      make([]byte, chacha20poly1305.NonceSizeX),
			ciphertext: make([]byte, chacha20poly1305.Overhead-1),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			envelope := Envelope{
				Version:    EnvelopeVersionV2,
				Algorithm:  AlgorithmXChaCha20Poly1305,
				KeyID:      "key-1",
				Nonce:      tc.nonce,
				Ciphertext: tc.ciphertext,
			}

			if err := envelope.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want malformed envelope error")
			}
		})
	}
}

func TestEnvelopeValidateRejectsV1(t *testing.T) {
	envelope := Envelope{
		Version:    1,
		Algorithm:  AlgorithmXChaCha20Poly1305,
		KeyID:      "key-1",
		Nonce:      []byte{1, 2, 3},
		Ciphertext: []byte{4, 5, 6},
	}

	if err := envelope.Validate(); err == nil {
		t.Fatal("Validate() must reject version 1 envelopes")
	}
}

func TestEnvelopeValidateRejectsMissingKeyID(t *testing.T) {
	envelope := Envelope{
		Version:    EnvelopeVersionV2,
		Algorithm:  AlgorithmAES256GCM,
		Nonce:      []byte{1, 2, 3},
		Ciphertext: []byte{4, 5, 6},
	}

	err := envelope.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
}

func TestEnvelopeValidateRejectsAES256GCM(t *testing.T) {
	envelope := Envelope{
		Version:    EnvelopeVersionV2,
		Algorithm:  AlgorithmAES256GCM,
		KeyID:      "key-1",
		Nonce:      []byte{1, 2, 3},
		Ciphertext: []byte{4, 5, 6},
	}

	if err := envelope.Validate(); err == nil {
		t.Fatal("Validate() must reject unsupported AES-256-GCM")
	}
}

func TestEnvelopeValidateRejectsUnknownVersion(t *testing.T) {
	envelope := Envelope{
		Version:    3,
		Algorithm:  AlgorithmXChaCha20Poly1305,
		KeyID:      "key-1",
		Nonce:      []byte{1, 2, 3},
		Ciphertext: []byte{4, 5, 6},
	}

	if err := envelope.Validate(); err == nil {
		t.Fatal("Validate() must reject unknown envelope versions")
	}
}
