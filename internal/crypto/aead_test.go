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

func testAssociatedData() AssociatedData {
	return AssociatedData{RecordType: "note", RecordID: "record-1", KeyID: "note-body"}
}

func TestSealV2OpenWithADRoundTrip(t *testing.T) {
	key := newTestKey(t)
	ad := testAssociatedData()

	env, err := SealV2(ad, key, []byte("top secret"))
	if err != nil {
		t.Fatalf("SealV2: %v", err)
	}
	plain, err := OpenWithAD(key, env, ad)
	if err != nil {
		t.Fatalf("OpenWithAD: %v", err)
	}
	if !bytes.Equal(plain, []byte("top secret")) {
		t.Fatalf("plaintext = %q", plain)
	}
}

func TestOpenWithADFailsWithWrongKey(t *testing.T) {
	key := newTestKey(t)
	ad := testAssociatedData()
	env, err := SealV2(ad, key, []byte("secret"))
	if err != nil {
		t.Fatalf("SealV2: %v", err)
	}

	wrong := make([]byte, len(key))
	copy(wrong, key)
	wrong[0] ^= 0xFF
	if _, err := OpenWithAD(wrong, env, ad); err == nil {
		t.Fatal("OpenWithAD must fail with wrong key")
	}
}

func TestOpenWithADFailsWhenTampered(t *testing.T) {
	key := newTestKey(t)
	ad := testAssociatedData()
	env, err := SealV2(ad, key, []byte("secret"))
	if err != nil {
		t.Fatalf("SealV2: %v", err)
	}
	env.Ciphertext[0] ^= 0xFF
	if _, err := OpenWithAD(key, env, ad); err == nil {
		t.Fatal("OpenWithAD must fail when ciphertext is tampered")
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

func TestOpenWithADRejectsV1Envelope(t *testing.T) {
	key := newTestKey(t)
	ad := AssociatedData{RecordType: "note", RecordID: "record-1", KeyID: "note-body"}
	env := Envelope{
		Version:    1,
		Algorithm:  AlgorithmXChaCha20Poly1305,
		KeyID:      ad.KeyID,
		Nonce:      make([]byte, 24),
		Ciphertext: []byte("not-a-real-ciphertext-just-for-version-gate"),
	}
	if _, err := OpenWithAD(key, env, ad); err == nil {
		t.Fatal("OpenWithAD must reject version 1 envelopes")
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
