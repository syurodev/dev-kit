package sync

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func testSyncContext() SyncContext {
	return SyncContext{
		AccountID:       "acct-1",
		DeviceID:        "dev-1",
		ProtocolVersion: SyncProtocolVersion,
		OperationID:     "op-1",
		EntityVersion:   1,
	}
}

func validChange(key string, createdAt time.Time) Change {
	ctx := testSyncContext()
	ctx.OperationID = key
	envelope, err := SealPayload(testVault{key: bytes.Repeat([]byte{0x42}, 32)}, ctx, "record-1", "note", "key-1", 1, OperationCreate, []byte("record"))
	if err != nil {
		panic(err)
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		panic(err)
	}
	return NewChange(key, "record-1", "note", OperationCreate, payload, 1, ctx, "key-1", createdAt)
}

func TestSyncContextValidate(t *testing.T) {
	valid := SyncContext{
		AccountID:       "acct-1",
		DeviceID:        "dev-1",
		ProtocolVersion: SyncProtocolVersion,
		OperationID:     "op-1",
		EntityVersion:   1,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid context: %v", err)
	}
	bad := valid
	bad.ProtocolVersion = SyncProtocolVersion + 1
	if err := bad.Validate(); err == nil {
		t.Fatal("expected protocol mismatch error")
	}
	bad = valid
	bad.EntityVersion = 0
	if err := bad.Validate(); err == nil {
		t.Fatal("expected entity version error")
	}
}

func TestNewOperationIDUnique(t *testing.T) {
	a, err := NewOperationID()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewOperationID()
	if err != nil {
		t.Fatal(err)
	}
	if a == "" || a == b {
		t.Fatalf("ids not unique: %q %q", a, b)
	}
}

func TestChangeValidateRejectsPlainInvalidEnvelope(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Change)
	}{
		{"missing key", func(c *Change) { c.IdempotencyKey = "" }},
		{"invalid operation", func(c *Change) { c.Operation = "read" }},
		{"empty ciphertext", func(c *Change) { c.Payload = nil }},
		{"invalid key version", func(c *Change) { c.KeyVersion = 0 }},
		{"invalid status", func(c *Change) { c.Status = "unknown" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			change := validChange("key", time.Now())
			test.edit(&change)
			if err := change.Validate(); err == nil {
				t.Fatal("Validate() = nil, want error")
			}
		})
	}
}

func TestCursorAndConflictValidate(t *testing.T) {
	if err := (Cursor{Token: strings.Repeat("x", 513)}).Validate(); err == nil {
		t.Fatal("long cursor accepted")
	}
	if err := (Cursor{Token: "cursor-1"}).Validate(); err != nil {
		t.Fatalf("valid cursor: %v", err)
	}
	conflict := Conflict{
		ID:              "conflict-1",
		RecordID:        "record-1",
		RecordType:      "note",
		LocalChangeKey:  "local-1",
		RemoteChangeKey: "remote-1",
		DetectedAt:      time.Now(),
	}
	if err := conflict.Validate(); err != nil {
		t.Fatalf("valid conflict: %v", err)
	}
	conflict.RemoteChangeKey = conflict.LocalChangeKey
	if err := conflict.Validate(); err == nil {
		t.Fatal("same local/remote change keys accepted")
	}
}
