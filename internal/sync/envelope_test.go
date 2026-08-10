package sync

import (
	"bytes"
	"encoding/json"
	"testing"

	"devkit/internal/crypto"
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

func testContext(opID string) SyncContext {
	return SyncContext{
		AccountID: "acct-1", DeviceID: "dev-1",
		ProtocolVersion: SyncProtocolVersion,
		OperationID:     opID, EntityVersion: 1,
	}
}

func TestSealPayloadBindsSyncContext(t *testing.T) {
	vault := testVault{key: bytes.Repeat([]byte{0x42}, 32)}
	plaintext := []byte("secret")
	env, err := SealPayload(vault, testContext("op-1"), "record-1", "note", "note-body", 1, OperationCreate, plaintext)
	if err != nil {
		t.Fatalf("SealPayload: %v", err)
	}
	if env.EnvelopeVersion != 2 || env.AccountID != "acct-1" || env.OperationID != "op-1" {
		t.Fatalf("envelope context not set: %+v", env)
	}
	if _, err := OpenPayload(vault, env, "note-body"); err != nil {
		t.Fatalf("open: %v", err)
	}
	tampered := env
	tampered.AccountID = "other"
	if _, err := OpenPayload(vault, tampered, "note-body"); err == nil {
		t.Fatal("account swap must fail")
	}
}

func TestReplicationEnvelopeRejectsV1(t *testing.T) {
	env := ReplicationEnvelope{EnvelopeVersion: 1, RecordID: "r", RecordType: "note", KeyVersion: 1, Ciphertext: []byte("{}")}
	if err := env.Validate(); err == nil {
		t.Fatal("v1 must be rejected")
	}
}

func TestSealPayloadOpenPayloadRoundTrip(t *testing.T) {
	vault := testVault{key: bytes.Repeat([]byte{0x42}, 32)}
	plaintext := []byte(`{"record":"secret-marker","payload":{"version":2}}`)

	envelope, err := SealPayload(vault, testContext("op-roundtrip"), "record-1", "note", "note-body", 3, OperationUpdate, plaintext)
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
	envelope, err := SealPayload(vault, testContext("op-serialize"), "record-1", "note", "note-body", 1, OperationCreate, marker)
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
	envelope, err := SealPayload(vault, testContext("op-aad"), "record-1", "note", "note-body", 1, OperationUpdate, []byte("secret"))
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
		{name: "account ID", mutate: func(e *ReplicationEnvelope) { e.AccountID = "other-acct" }, keyID: "note-body"},
		{name: "device ID", mutate: func(e *ReplicationEnvelope) { e.DeviceID = "other-dev" }, keyID: "note-body"},
		{name: "operation ID", mutate: func(e *ReplicationEnvelope) { e.OperationID = "other-op" }, keyID: "note-body"},
		{name: "entity version", mutate: func(e *ReplicationEnvelope) { e.EntityVersion = 2 }, keyID: "note-body"},
		{name: "operation", mutate: func(e *ReplicationEnvelope) { e.Operation = OperationDelete }, keyID: "note-body"},
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
	envelope, err := SealPayload(vault, testContext("op-wrong-key"), "record-1", "note", "note-body", 1, OperationCreate, []byte("secret"))
	if err != nil {
		t.Fatalf("SealPayload: %v", err)
	}
	if _, err := OpenPayload(wrongVault, envelope, "note-body"); err == nil {
		t.Fatal("OpenPayload must reject the wrong key")
	}
}

func TestOpenPayloadRejectsTampering(t *testing.T) {
	vault := testVault{key: bytes.Repeat([]byte{0x51}, 32)}
	envelope, err := SealPayload(vault, testContext("op-tamper"), "record-1", "note", "note-body", 1, OperationCreate, []byte("secret"))
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
	envelope, err := SealPayload(vault, testContext("op-version"), "record-1", "note", "note-body", 1, OperationCreate, []byte("secret"))
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

func TestSyncIdentifierRejectsPathSeparatorsForAADSafety(t *testing.T) {
	// Replication AAD uses slash-delimited labeled segments. Keep each caller
	// supplied identifier to one segment so the encoding stays canonical and
	// human-auditable across future parsers and protocol implementations.
	ctxA := SyncContext{
		AccountID: "x", DeviceID: "y/z",
		ProtocolVersion: SyncProtocolVersion,
		OperationID:     "op-1", EntityVersion: 1,
	}
	ctxB := SyncContext{
		AccountID: "x/y", DeviceID: "z",
		ProtocolVersion: SyncProtocolVersion,
		OperationID:     "op-1", EntityVersion: 1,
	}
	vault := testVault{key: bytes.Repeat([]byte{0x42}, 32)}
	if _, err := SealPayload(vault, ctxA, "record-1", "note", "note-body", 1, OperationCreate, []byte("secret")); err == nil {
		t.Fatal("context with slash in device id must be rejected")
	}
	if _, err := SealPayload(vault, ctxB, "record-1", "note", "note-body", 1, OperationCreate, []byte("secret")); err == nil {
		t.Fatal("context with slash in account id must be rejected")
	}
	if _, err := SealPayload(vault, testContext("op-1"), "rec/ord", "note", "note-body", 1, OperationCreate, []byte("secret")); err == nil {
		t.Fatal("record id with slash must be rejected")
	}
	if _, err := SealPayload(vault, testContext("op-1"), "record-1", "no/te", "note-body", 1, OperationCreate, []byte("secret")); err == nil {
		t.Fatal("record type with slash must be rejected")
	}
	if _, err := SealPayload(vault, testContext("op-1"), "record-1", "note", "note/body", 1, OperationCreate, []byte("secret")); err == nil {
		t.Fatal("key id with slash must be rejected")
	}
}

func TestReplicationEnvelopeRejectsInvalidInputs(t *testing.T) {
	vault := testVault{key: bytes.Repeat([]byte{0x71}, 32)}
	ctx := testContext("op-invalid")
	cases := []struct {
		name       string
		ctx        SyncContext
		recordID   string
		recordType string
		keyID      string
		keyVersion uint32
		plaintext  []byte
	}{
		{name: "empty record ID", ctx: ctx, recordID: "", recordType: "note", keyID: "note-body", keyVersion: 1, plaintext: []byte("x")},
		{name: "empty record type", ctx: ctx, recordID: "record-1", recordType: "", keyID: "note-body", keyVersion: 1, plaintext: []byte("x")},
		{name: "empty key ID", ctx: ctx, recordID: "record-1", recordType: "note", keyID: "", keyVersion: 1, plaintext: []byte("x")},
		{name: "zero key version", ctx: ctx, recordID: "record-1", recordType: "note", keyID: "note-body", keyVersion: 0, plaintext: []byte("x")},
		{name: "empty payload", ctx: ctx, recordID: "record-1", recordType: "note", keyID: "note-body", keyVersion: 1, plaintext: nil},
		{name: "invalid context", ctx: SyncContext{}, recordID: "record-1", recordType: "note", keyID: "note-body", keyVersion: 1, plaintext: []byte("x")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := SealPayload(vault, tc.ctx, tc.recordID, tc.recordType, tc.keyID, tc.keyVersion, OperationCreate, tc.plaintext); err == nil {
				t.Fatal("SealPayload must reject invalid input")
			}
		})
	}
}
