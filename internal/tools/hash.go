package tools

import (
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
)

type HashService struct{}

type HashRequest struct {
	Algorithm string
	Input     string
}

type HashResult struct {
	Algorithm string
	Output    string
}

func NewHashService() *HashService {
	return &HashService{}
}

func (s *HashService) Hash(request HashRequest) (HashResult, error) {
	switch request.Algorithm {
	case "sha256":
		sum := sha256.Sum256([]byte(request.Input))
		return HashResult{Algorithm: request.Algorithm, Output: hex.EncodeToString(sum[:])}, nil
	case "sha512":
		sum := sha512.Sum512([]byte(request.Input))
		return HashResult{Algorithm: request.Algorithm, Output: hex.EncodeToString(sum[:])}, nil
	case "md5":
		sum := md5.Sum([]byte(request.Input))
		return HashResult{Algorithm: request.Algorithm, Output: hex.EncodeToString(sum[:])}, nil
	default:
		return HashResult{}, fmt.Errorf("unsupported hash algorithm %q", request.Algorithm)
	}
}
