package crypto

import (
	"bytes"
	"testing"
)

func newTestKey(t *testing.T) []byte {
	t.Helper()
	params, err := DefaultKDFParams()
	if err != nil {
		t.Fatalf("params: %v", err)
	}
	key, err := DeriveKey("master-password", params)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	return key
}

func TestSealOpenRoundTrip(t *testing.T) {
	key := newTestKey(t)

	env, err := Seal("dek_1", key, []byte("top secret"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	plain, err := Open(key, env)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(plain, []byte("top secret")) {
		t.Fatalf("plaintext = %q", plain)
	}
}

func TestOpenFailsWithWrongKey(t *testing.T) {
	key := newTestKey(t)
	env, err := Seal("dek_1", key, []byte("secret"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	wrong := make([]byte, len(key))
	copy(wrong, key)
	wrong[0] ^= 0xFF
	if _, err := Open(wrong, env); err == nil {
		t.Fatal("Open must fail with wrong key")
	}
}

func TestOpenFailsWhenTampered(t *testing.T) {
	key := newTestKey(t)
	env, err := Seal("dek_1", key, []byte("secret"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	env.Ciphertext[0] ^= 0xFF
	if _, err := Open(key, env); err == nil {
		t.Fatal("Open must fail when ciphertext is tampered")
	}
}
