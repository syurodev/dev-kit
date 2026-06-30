package crypto

import "errors"

const (
	AlgorithmXChaCha20Poly1305 = "xchacha20-poly1305"
	AlgorithmAES256GCM         = "aes-256-gcm"
)

type Envelope struct {
	Version    uint32 `json:"version"`
	Algorithm  string `json:"algorithm"`
	KeyID      string `json:"key_id"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

func (e Envelope) Validate() error {
	if e.Version == 0 {
		return errors.New("version must be greater than zero")
	}
	if !isSupportedAlgorithm(e.Algorithm) {
		return errors.New("algorithm must be supported")
	}
	if e.KeyID == "" {
		return errors.New("key ID is required")
	}
	if len(e.Nonce) == 0 {
		return errors.New("nonce is required")
	}
	if len(e.Ciphertext) == 0 {
		return errors.New("ciphertext is required")
	}

	return nil
}

func isSupportedAlgorithm(algorithm string) bool {
	switch algorithm {
	case AlgorithmXChaCha20Poly1305, AlgorithmAES256GCM:
		return true
	default:
		return false
	}
}
