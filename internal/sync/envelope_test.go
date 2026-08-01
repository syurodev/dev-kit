package sync

import (
	"bytes"
	"encoding/json"
	"testing"

	"changeme/internal/crypto"
)

type testVault struct {
	key []byte
}

func (v testVault) SealV2(ad crypto.AssociatedData, plaintext []byte) (crypto.Envelope, error) {
	return crypto.SealV2(ad, v.key, plaintext)
}

func (v testVault) OpenWithAD(envelope crypto.Envelope, ad crypto.AssociatedData) ([]byte, error) {
	return crypto.OpenWithAD(v.key, envelope, ad)
}

func TestSealPayloadOpenPayloadRoundTrip(t *testing.T) {
	vault := testVault{key: bytes.Repeat([]byte{0x42}, 32)}
	plaintext := []byte(`{"record":"secret-marker","payload":{"version":2}}`)

	envelope, err := SealPayload(vault, "record-1", "note", "note-body", 3, plaintext)
	if err != nil {
		t.Fatalf("SealPayload: %v", err)
	}
	if err := envelope.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	opened, err := OpenPayload(vault, envelope, "note-body")
	if err != nil {
		t.Fatalf("OpenPayload: %v", err)
	}
	if !bytes.Equal(opened, plaintext) {
		t.Fatalf("plaintext = %q, want %q", opened, plaintext)
	}
}

func TestReplicationEnvelopeDoesNotSerializePlaintext(t *testing.T) {
	marker := []byte("plaintext-marker")
	vault := testVault{key: bytes.Repeat([]byte{0x24}, 32)}
	envelope, err := SealPayload(vault, "record-1", "note", "note-body", 1, marker)
	if err != nil {
		t.Fatalf("SealPayload: %v", err)
	}
	serialized, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if bytes.Contains(serialized, marker) {
		t.Fatalf("serialized replication envelope contains plaintext marker: %s", serialized)
	}
}

func TestOpenPayloadRejectsChangedAADInputs(t *testing.T) {
	vault := testVault{key: bytes.Repeat([]byte{0x11}, 32)}
	envelope, err := SealPayload(vault, "record-1", "note", "note-body", 1, []byte("secret"))
	if err != nil {
		t.Fatalf("SealPayload: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*ReplicationEnvelope)
		keyID  string
	}{
		{name: "record ID", mutate: func(e *ReplicationEnvelope) { e.RecordID = "record-2" }, keyID: "note-body"},
		{name: "record type", mutate: func(e *ReplicationEnvelope) { e.RecordType = "database" }, keyID: "note-body"},
		{name: "key version", mutate: func(e *ReplicationEnvelope) { e.KeyVersion = 2 }, keyID: "note-body"},
		{name: "key ID", mutate: func(e *ReplicationEnvelope) {}, keyID: "other-key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := envelope
			tc.mutate(&candidate)
			if _, err := OpenPayload(vault, candidate, tc.keyID); err == nil {
				t.Fatal("OpenPayload must reject changed associated data")
			}
		})
	}
}

func TestOpenPayloadRejectsWrongKey(t *testing.T) {
	vault := testVault{key: bytes.Repeat([]byte{0x31}, 32)}
	wrongVault := testVault{key: bytes.Repeat([]byte{0x32}, 32)}
	envelope, err := SealPayload(vault, "record-1", "note", "note-body", 1, []byte("secret"))
	if err != nil {
		t.Fatalf("SealPayload: %v", err)
	}
	if _, err := OpenPayload(wrongVault, envelope, "note-body"); err == nil {
		t.Fatal("OpenPayload must reject the wrong key")
	}
}

func TestOpenPayloadRejectsTampering(t *testing.T) {
	vault := testVault{key: bytes.Repeat([]byte{0x51}, 32)}
	envelope, err := SealPayload(vault, "record-1", "note", "note-body", 1, []byte("secret"))
	if err != nil {
		t.Fatalf("SealPayload: %v", err)
	}
	envelope.Ciphertext[len(envelope.Ciphertext)-1] ^= 0x01
	if _, err := OpenPayload(vault, envelope, "note-body"); err == nil {
		t.Fatal("OpenPayload must reject tampered ciphertext")
	}
}

func TestReplicationEnvelopeRejectsUnsupportedVersion(t *testing.T) {
	vault := testVault{key: bytes.Repeat([]byte{0x61}, 32)}
	envelope, err := SealPayload(vault, "record-1", "note", "note-body", 1, []byte("secret"))
	if err != nil {
		t.Fatalf("SealPayload: %v", err)
	}
	envelope.EnvelopeVersion = ReplicationEnvelopeVersion + 1
	if err := envelope.Validate(); err == nil {
		t.Fatal("Validate must reject unsupported replication versions")
	}
	if _, err := OpenPayload(vault, envelope, "note-body"); err == nil {
		t.Fatal("OpenPayload must reject unsupported replication versions")
	}
}

func TestReplicationEnvelopeRejectsInvalidInputs(t *testing.T) {
	vault := testVault{key: bytes.Repeat([]byte{0x71}, 32)}
	cases := []struct {
		name       string
		recordID   string
		recordType string
		keyID      string
		keyVersion uint32
		plaintext  []byte
	}{
		{name: "empty record ID", recordID: "", recordType: "note", keyID: "note-body", keyVersion: 1, plaintext: []byte("x")},
		{name: "empty record type", recordID: "record-1", recordType: "", keyID: "note-body", keyVersion: 1, plaintext: []byte("x")},
		{name: "empty key ID", recordID: "record-1", recordType: "note", keyID: "", keyVersion: 1, plaintext: []byte("x")},
		{name: "zero key version", recordID: "record-1", recordType: "note", keyID: "note-body", keyVersion: 0, plaintext: []byte("x")},
		{name: "empty payload", recordID: "record-1", recordType: "note", keyID: "note-body", keyVersion: 1, plaintext: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := SealPayload(vault, tc.recordID, tc.recordType, tc.keyID, tc.keyVersion, tc.plaintext); err == nil {
				t.Fatal("SealPayload must reject invalid input")
			}
		})
	}
}
