package crypto

import (
	"bytes"
	"testing"
)

func TestDeriveKeyIsDeterministic(t *testing.T) {
	params, err := DefaultKDFParams()
	if err != nil {
		t.Fatalf("DefaultKDFParams: %v", err)
	}

	a, err := DeriveKey("correct horse", params)
	if err != nil {
		t.Fatalf("DeriveKey: %v", err)
	}
	b, err := DeriveKey("correct horse", params)
	if err != nil {
		t.Fatalf("DeriveKey: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("same password+params must derive the same key")
	}
	if len(a) != int(params.KeyLength) {
		t.Fatalf("key length = %d, want %d", len(a), params.KeyLength)
	}
}

func TestDeriveKeyDiffersBySalt(t *testing.T) {
	p1, _ := DefaultKDFParams()
	p2, _ := DefaultKDFParams()

	k1, _ := DeriveKey("pw", p1)
	k2, _ := DeriveKey("pw", p2)
	if bytes.Equal(k1, k2) {
		t.Fatal("different salts must derive different keys")
	}
}

func TestDeriveKeyRequiresPassword(t *testing.T) {
	params, _ := DefaultKDFParams()
	if _, err := DeriveKey("", params); err == nil {
		t.Fatal("empty password must error")
	}
}
