package tools

import "testing"

func TestHashSHA256(t *testing.T) {
	service := NewHashService()

	result, err := service.Hash(HashRequest{Algorithm: "sha256", Input: "hello"})
	if err != nil {
		t.Fatalf("Hash returned error: %v", err)
	}

	const expected = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if result.Output != expected {
		t.Fatalf("Output = %q, want %q", result.Output, expected)
	}
}

func TestHashRejectsUnsupportedAlgorithm(t *testing.T) {
	service := NewHashService()

	if _, err := service.Hash(HashRequest{Algorithm: "unknown", Input: "hello"}); err == nil {
		t.Fatal("Hash returned nil error, want unsupported algorithm error")
	}
}
