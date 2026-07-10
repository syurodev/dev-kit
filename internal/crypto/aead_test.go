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

func TestOpenWithADRejectsChangedRecordID(t *testing.T) {
	key := newTestKey(t)
	ad := AssociatedData{RecordType: "note", RecordID: "record-1", KeyID: "note-body"}
	env, err := SealV2(ad, key, []byte("secret"))
	if err != nil {
		t.Fatalf("SealV2: %v", err)
	}

	_, err = OpenWithAD(key, env, AssociatedData{RecordType: "note", RecordID: "record-2", KeyID: "note-body"})
	if err == nil {
		t.Fatal("OpenWithAD must fail when record ID changes")
	}
}

func TestOpenWithADReadsV1Envelope(t *testing.T) {
	key := newTestKey(t)
	env, err := Seal("note-body", key, []byte("legacy secret"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	plain, err := OpenWithAD(key, env, AssociatedData{RecordType: "note", RecordID: "record-1", KeyID: "note-body"})
	if err != nil {
		t.Fatalf("OpenWithAD: %v", err)
	}
	if !bytes.Equal(plain, []byte("legacy secret")) {
		t.Fatalf("plaintext = %q", plain)
	}
}

func TestOpenRejectsV2EnvelopeWithoutAssociatedData(t *testing.T) {
	key := newTestKey(t)
	env, err := Seal("note-body", key, []byte("secret"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	env.Version = EnvelopeVersionV2

	if plain, err := Open(key, env); err == nil || plain != nil {
		t.Fatalf("Open(V2) = %q, %v; want nil plaintext and an error", plain, err)
	}
}

func TestSealV2RejectsNULInAssociatedData(t *testing.T) {
	key := newTestKey(t)
	valid := AssociatedData{RecordType: "note", RecordID: "record-1", KeyID: "note-body"}
	cases := []struct {
		name string
		ad   AssociatedData
	}{
		{name: "record type", ad: AssociatedData{RecordType: "note\x00other", RecordID: valid.RecordID, KeyID: valid.KeyID}},
		{name: "record ID", ad: AssociatedData{RecordType: valid.RecordType, RecordID: "record\x00other", KeyID: valid.KeyID}},
		{name: "key ID", ad: AssociatedData{RecordType: valid.RecordType, RecordID: valid.RecordID, KeyID: "key\x00other"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := SealV2(tc.ad, key, []byte("secret")); err == nil {
				t.Fatal("SealV2 must reject NUL in associated data")
			}
		})
	}
}
