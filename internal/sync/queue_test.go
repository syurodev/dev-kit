package sync

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestQueuePayloadSealsAndQueuesCompleteRecord(t *testing.T) {
	vault := testVault{key: bytes.Repeat([]byte{0x42}, 32)}
	outbox := NewMemoryOutbox()
	ctx := testContext("op-seal")
	createdAt := time.Unix(123, 456).UTC()
	plaintext := []byte(`{"record":"secret-marker","payload":{"version":2}}`)

	change, err := QueuePayload(vault, outbox, ctx, "record-1", "note", "note-body", 3, OperationUpdate, plaintext, createdAt)
	if err != nil {
		t.Fatalf("QueuePayload: %v", err)
	}
	if change.Status != StatusPending {
		t.Fatalf("status = %q, want %q", change.Status, StatusPending)
	}
	if bytes.Contains(change.Payload, plaintext) {
		t.Fatal("queued payload contains plaintext record")
	}
	if bytes.Contains([]byte(change.IdempotencyKey), plaintext) {
		t.Fatal("idempotency key contains plaintext record")
	}

	var envelope ReplicationEnvelope
	if err := json.Unmarshal(change.Payload, &envelope); err != nil {
		t.Fatalf("unmarshal replication envelope: %v", err)
	}
	opened, err := OpenPayload(vault, envelope, "note-body")
	if err != nil {
		t.Fatalf("OpenPayload: %v", err)
	}
	if !bytes.Equal(opened, plaintext) {
		t.Fatalf("opened payload = %q, want %q", opened, plaintext)
	}

	change.Payload[0] ^= 0xff
	stored, err := outbox.Get(change.IdempotencyKey)
	if err != nil {
		t.Fatalf("get queued change: %v", err)
	}
	if bytes.Equal(stored.Payload, change.Payload) {
		t.Fatal("returned change exposed mutable outbox payload")
	}
}

func TestQueuePayloadIdempotentSameOperation(t *testing.T) {
	vault := testVault{key: bytes.Repeat([]byte{0x24}, 32)}
	outbox := NewMemoryOutbox()
	ctx := testContext("op-same")
	plaintext := []byte("record contents")
	createdAt := time.Unix(200, 1).UTC()

	first, err := QueuePayload(vault, outbox, ctx, "record-1", "note", "note-body", 1, OperationCreate, plaintext, createdAt)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := QueuePayload(vault, outbox, ctx, "record-1", "note", "note-body", 1, OperationCreate, plaintext, createdAt.Add(time.Second))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.IdempotencyKey != second.IdempotencyKey || !bytes.Equal(first.Payload, second.Payload) {
		t.Fatal("same operation_id must return stored ciphertext")
	}
}

func TestQueuePayloadConflictOnDifferentPlaintext(t *testing.T) {
	vault := testVault{key: bytes.Repeat([]byte{0x24}, 32)}
	outbox := NewMemoryOutbox()
	ctx := testContext("op-conflict")
	createdAt := time.Unix(200, 1).UTC()
	if _, err := QueuePayload(vault, outbox, ctx, "record-1", "note", "note-body", 1, OperationCreate, []byte("a"), createdAt); err != nil {
		t.Fatal(err)
	}
	if _, err := QueuePayload(vault, outbox, ctx, "record-1", "note", "note-body", 1, OperationCreate, []byte("b"), createdAt); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("err = %v, want ErrIdempotencyConflict", err)
	}
}

func TestQueuePayloadConflictOnDifferentKeyID(t *testing.T) {
	vault := testVault{key: bytes.Repeat([]byte{0x24}, 32)}
	outbox := NewMemoryOutbox()
	ctx := testContext("op-key")
	createdAt := time.Unix(200, 1).UTC()
	plaintext := []byte("same")
	if _, err := QueuePayload(vault, outbox, ctx, "record-1", "note", "note-body", 1, OperationCreate, plaintext, createdAt); err != nil {
		t.Fatal(err)
	}
	if _, err := QueuePayload(vault, outbox, ctx, "record-1", "note", "other-key", 1, OperationCreate, plaintext, createdAt); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("err = %v, want ErrIdempotencyConflict", err)
	}
}

func TestQueuePayloadRequiresEncryptedDeleteTombstone(t *testing.T) {
	vault := testVault{key: bytes.Repeat([]byte{0x11}, 32)}
	outbox := NewMemoryOutbox()
	ctx := testContext("op-delete")
	now := time.Unix(300, 0).UTC()

	if _, err := QueuePayload(vault, outbox, ctx, "record-1", "note", "note-body", 1, OperationDelete, nil, now); err == nil {
		t.Fatal("delete with empty plaintext tombstone was accepted")
	}

	tombstone := []byte(`{"tombstone":true}`)
	change, err := QueuePayload(vault, outbox, ctx, "record-1", "note", "note-body", 1, OperationDelete, tombstone, now)
	if err != nil {
		t.Fatalf("QueuePayload delete tombstone: %v", err)
	}
	if bytes.Contains(change.Payload, tombstone) {
		t.Fatal("delete payload contains plaintext tombstone")
	}
}

func TestQueuePayloadRejectsInvalidInputs(t *testing.T) {
	vault := testVault{key: bytes.Repeat([]byte{0x31}, 32)}
	outbox := NewMemoryOutbox()
	ctx := testContext("op-invalid")
	now := time.Unix(400, 0).UTC()
	plaintext := []byte("payload")

	cases := []struct {
		name       string
		vault      Vault
		outbox     Outbox
		ctx        SyncContext
		recordID   string
		recordType string
		keyID      string
		keyVersion uint32
		operation  Operation
		plaintext  []byte
		createdAt  time.Time
	}{
		{name: "nil vault", vault: nil, outbox: outbox, ctx: ctx, recordID: "record-1", recordType: "note", keyID: "note-body", keyVersion: 1, operation: OperationCreate, plaintext: plaintext, createdAt: now},
		{name: "nil outbox", vault: vault, outbox: nil, ctx: ctx, recordID: "record-1", recordType: "note", keyID: "note-body", keyVersion: 1, operation: OperationCreate, plaintext: plaintext, createdAt: now},
		{name: "empty record ID", vault: vault, outbox: outbox, ctx: ctx, recordID: "", recordType: "note", keyID: "note-body", keyVersion: 1, operation: OperationCreate, plaintext: plaintext, createdAt: now},
		{name: "empty record type", vault: vault, outbox: outbox, ctx: ctx, recordID: "record-1", recordType: "", keyID: "note-body", keyVersion: 1, operation: OperationCreate, plaintext: plaintext, createdAt: now},
		{name: "empty key ID", vault: vault, outbox: outbox, ctx: ctx, recordID: "record-1", recordType: "note", keyID: "", keyVersion: 1, operation: OperationCreate, plaintext: plaintext, createdAt: now},
		{name: "zero key version", vault: vault, outbox: outbox, ctx: ctx, recordID: "record-1", recordType: "note", keyID: "note-body", keyVersion: 0, operation: OperationCreate, plaintext: plaintext, createdAt: now},
		{name: "invalid operation", vault: vault, outbox: outbox, ctx: ctx, recordID: "record-1", recordType: "note", keyID: "note-body", keyVersion: 1, operation: Operation("rotate"), plaintext: plaintext, createdAt: now},
		{name: "empty plaintext", vault: vault, outbox: outbox, ctx: ctx, recordID: "record-1", recordType: "note", keyID: "note-body", keyVersion: 1, operation: OperationCreate, plaintext: nil, createdAt: now},
		{name: "zero created time", vault: vault, outbox: outbox, ctx: ctx, recordID: "record-1", recordType: "note", keyID: "note-body", keyVersion: 1, operation: OperationCreate, plaintext: plaintext},
		{name: "oversized snapshot", vault: vault, outbox: outbox, ctx: ctx, recordID: "record-1", recordType: "note", keyID: "note-body", keyVersion: 1, operation: OperationCreate, plaintext: make([]byte, MaxRecordSnapshotBytes+1), createdAt: now},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := QueuePayload(tc.vault, tc.outbox, tc.ctx, tc.recordID, tc.recordType, tc.keyID, tc.keyVersion, tc.operation, tc.plaintext, tc.createdAt); err == nil {
				t.Fatal("QueuePayload accepted invalid input")
			}
		})
	}
}

func TestQueuePayloadRejectsLogicalIdempotencyCollision(t *testing.T) {
	vault := testVault{key: bytes.Repeat([]byte{0x51}, 32)}
	outbox := NewMemoryOutbox()
	ctx := testContext("op-collision")
	now := time.Unix(500, 0).UTC()

	if _, err := QueuePayload(vault, outbox, ctx, "record-1", "note", "note-body", 1, OperationCreate, []byte("first"), now); err != nil {
		t.Fatalf("first QueuePayload: %v", err)
	}
	if _, err := QueuePayload(vault, outbox, ctx, "record-1", "database", "database-body", 1, OperationCreate, []byte("second"), now); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("collision error = %v, want %v", err, ErrIdempotencyConflict)
	}
}
