package crypto

import "testing"

func TestEnvelopeValidateAcceptsSupportedEnvelope(t *testing.T) {
	envelope := Envelope{
		Version:    1,
		Algorithm:  AlgorithmXChaCha20Poly1305,
		KeyID:      "key-1",
		Nonce:      []byte{1, 2, 3},
		Ciphertext: []byte{4, 5, 6},
	}

	if err := envelope.Validate(); err != nil {
		t.Fatalf("Validate() returned error: %v", err)
	}
}

func TestEnvelopeValidateRejectsMissingKeyID(t *testing.T) {
	envelope := Envelope{
		Version:    1,
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
		Version:    1,
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
