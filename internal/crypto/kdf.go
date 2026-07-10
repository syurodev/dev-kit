package crypto

import (
	"crypto/rand"
	"errors"

	"golang.org/x/crypto/argon2"
)

const AlgorithmArgon2id = "argon2id"

// KDFParams holds Argon2id parameters plus the salt. Persist it next to the
// wrapped key so derivation stays reproducible after upgrades.
type KDFParams struct {
	Algorithm   string `json:"algorithm"`
	Memory      uint32 `json:"memory"`
	Iterations  uint32 `json:"iterations"`
	Parallelism uint8  `json:"parallelism"`
	KeyLength   uint32 `json:"key_length"`
	Salt        []byte `json:"salt"`
}

// DefaultKDFParams returns interactive-strength Argon2id params with a fresh
// random 16-byte salt.
// ponytail: fixed cost (64MB, 3 passes, 4 lanes); add a "hardened" tier later if needed.
func DefaultKDFParams() (KDFParams, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return KDFParams{}, err
	}
	return KDFParams{
		Algorithm:   AlgorithmArgon2id,
		Memory:      64 * 1024,
		Iterations:  3,
		Parallelism: 4,
		KeyLength:   32,
		Salt:        salt,
	}, nil
}

// DeriveKey derives a key of params.KeyLength bytes from the password.
func DeriveKey(password string, params KDFParams) ([]byte, error) {
	if password == "" {
		return nil, errors.New("password is required")
	}
	if params.Algorithm != AlgorithmArgon2id {
		return nil, errors.New("unsupported kdf algorithm")
	}
	if len(params.Salt) == 0 {
		return nil, errors.New("salt is required")
	}
	return argon2.IDKey([]byte(password), params.Salt, params.Iterations, params.Memory, params.Parallelism, params.KeyLength), nil
}
