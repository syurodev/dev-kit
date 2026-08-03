# Sync Security Gates Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the six pre-remote-transport security findings: sync cryptographic context, idempotency integrity, outbox syncing lease recovery, Go 1.26.5 pin, frontend audit fixes, and KDF parameter bounds.

**Architecture:** Extend the local `internal/sync` contract so every sealed/queued/pushed change binds account, device, protocol, operation, and entity version in public fields and AEAD AAD. Persist local `sync_identity`, recover stale `syncing` outbox rows via lease, harden `DeriveKey`, and pin/bump toolchains and frontend deps.

**Tech Stack:** Go 1.26.5 (`toolchain` pin), built-in Go tests, `golang.org/x/crypto` Argon2id, Bun + Vite frontend, SQLite migrations in `internal/storage`.

**Spec:** `docs/superpowers/specs/2026-08-02-sync-security-gates-design.md`

## Global Constraints

- Never stage or commit anything under `docs/superpowers/**` (specs, plans, or other superpowers files). Commit steps below only touch application code/lockfiles.
- Greenfield sync wire: bump `ReplicationEnvelopeVersion` to `2` and reject `1`; bump queue idempotency namespace to `v2`.
- No remote HTTP transport, auth session tokens, pull/apply, or sync UI in this plan.
- `QueuePayload` requires caller-supplied `operation_id`; use `NewOperationID()` helper — do not auto-mint inside `QueuePayload`.
- `entity_version` is separate from `key_version` and must be `>= 1`.
- Outbox lease default: 5 minutes; `Pending(now, limit)` signature stays; lease lives on outbox constructor/config.
- `created_at` is not part of idempotency conflict compare.
- Keep crypto `AssociatedData` shape unchanged; pack expanded sync binding into the existing RecordType/RecordID/KeyID AAD triple (same pattern as today).

---

## File Map

| File | Responsibility |
|---|---|
| `internal/crypto/kdf.go` | Bound-check Argon2 params in `DeriveKey` |
| `internal/crypto/kdf_test.go` | Bounds + default success tests |
| `internal/sync/contract.go` | `SyncContext`, `Change` context fields, `NewOperationID`, validation |
| `internal/sync/contract_test.go` | Context/Change validation tests |
| `internal/sync/envelope.go` | Envelope v2 fields, context-bound seal/open/AAD |
| `internal/sync/envelope_test.go` | Round-trip + context mismatch + v1 reject |
| `internal/sync/queue.go` | Operation-id idempotency + content digest compare |
| `internal/sync/queue_test.go` | Idempotent retry vs conflict on digest/keyID |
| `internal/sync/outbox.go` | Memory outbox lease, `SyncingSince`, `RecoverStaleSyncing` |
| `internal/sync/outbox_test.go` | Stale reclaim + recovery tests |
| `internal/sync/runner.go` | Pass time into `MarkSyncing`; optional `SyncingLease` |
| `internal/sync/runner_test.go` | Update for `MarkSyncing(key, at)` |
| `internal/sync/transport.go` | Expand `sameTransportChange` identity |
| `internal/storage/sqlite.go` | `TableSyncIdentity`, migration `m8` |
| `internal/storage/syncidentity.go` | Load-or-create local account/device IDs |
| `internal/storage/syncidentity_test.go` | Persist + stable reload |
| `internal/storage/syncoutbox.go` | Persist context columns + lease Pending/Recover |
| `internal/storage/syncoutbox_test.go` | SQL lease + identity columns |
| `internal/storage/migrations_test.go` | Assert m8 columns/table |
| `go.mod` | `go 1.26.5` + `toolchain go1.26.5` |
| `frontend/package.json` | Move `shadcn` to devDeps |
| `frontend/bun.lock` | Bumped transitive advisories |

---

### Task 1: KDF parameter bounds

**Files:**
- Modify: `internal/crypto/kdf.go`
- Modify: `internal/crypto/kdf_test.go`

**Interfaces:**
- Consumes: existing `KDFParams`, `DeriveKey`, `DefaultKDFParams`
- Produces: `DeriveKey` rejects out-of-range memory/iterations/parallelism/key length/salt length with clear errors; defaults still succeed

- [ ] **Step 1: Write the failing tests**

Append to `internal/crypto/kdf_test.go`:

```go
func TestDeriveKeyRejectsUnsafeParams(t *testing.T) {
	base, err := DefaultKDFParams()
	if err != nil {
		t.Fatalf("DefaultKDFParams: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*KDFParams)
	}{
		{name: "memory too low", mutate: func(p *KDFParams) { p.Memory = 512 }},
		{name: "memory too high", mutate: func(p *KDFParams) { p.Memory = 2097153 }},
		{name: "iterations zero", mutate: func(p *KDFParams) { p.Iterations = 0 }},
		{name: "iterations too high", mutate: func(p *KDFParams) { p.Iterations = 11 }},
		{name: "parallelism zero", mutate: func(p *KDFParams) { p.Parallelism = 0 }},
		{name: "parallelism too high", mutate: func(p *KDFParams) { p.Parallelism = 17 }},
		{name: "key length too low", mutate: func(p *KDFParams) { p.KeyLength = 8 }},
		{name: "key length too high", mutate: func(p *KDFParams) { p.KeyLength = 65 }},
		{name: "salt too short", mutate: func(p *KDFParams) { p.Salt = []byte("short") }},
		{name: "salt too long", mutate: func(p *KDFParams) { p.Salt = bytes.Repeat([]byte{1}, 65) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := base
			params.Salt = append([]byte(nil), base.Salt...)
			tc.mutate(&params)
			if _, err := DeriveKey("password", params); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestDeriveKeyAcceptsDefaultParams(t *testing.T) {
	params, err := DefaultKDFParams()
	if err != nil {
		t.Fatalf("DefaultKDFParams: %v", err)
	}
	if _, err := DeriveKey("password", params); err != nil {
		t.Fatalf("DeriveKey default: %v", err)
	}
}
```

Add `"bytes"` to the imports in `kdf_test.go` if missing.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/crypto -run 'TestDeriveKeyRejectsUnsafeParams|TestDeriveKeyAcceptsDefaultParams' -count=1`

Expected: `TestDeriveKeyRejectsUnsafeParams` FAIL (unsafe params currently accepted). `TestDeriveKeyAcceptsDefaultParams` PASS.

- [ ] **Step 3: Implement bounds in `DeriveKey`**

In `internal/crypto/kdf.go`, after existing algorithm/salt empty checks and before `argon2.IDKey`:

```go
const (
	kdfMinMemoryKiB   uint32 = 1024
	kdfMaxMemoryKiB   uint32 = 2097152
	kdfMinIterations  uint32 = 1
	kdfMaxIterations  uint32 = 10
	kdfMinParallelism uint8  = 1
	kdfMaxParallelism uint8  = 16
	kdfMinKeyLength   uint32 = 16
	kdfMaxKeyLength   uint32 = 64
	kdfMinSaltLength         = 8
	kdfMaxSaltLength         = 64
)

// inside DeriveKey:
if params.Memory < kdfMinMemoryKiB || params.Memory > kdfMaxMemoryKiB {
	return nil, errors.New("kdf memory is out of allowed range")
}
if params.Iterations < kdfMinIterations || params.Iterations > kdfMaxIterations {
	return nil, errors.New("kdf iterations are out of allowed range")
}
if params.Parallelism < kdfMinParallelism || params.Parallelism > kdfMaxParallelism {
	return nil, errors.New("kdf parallelism is out of allowed range")
}
if params.KeyLength < kdfMinKeyLength || params.KeyLength > kdfMaxKeyLength {
	return nil, errors.New("kdf key length is out of allowed range")
}
if len(params.Salt) < kdfMinSaltLength || len(params.Salt) > kdfMaxSaltLength {
	return nil, errors.New("kdf salt length is out of allowed range")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/crypto -count=1`

Expected: PASS

- [ ] **Step 5: Commit (code only — never `docs/superpowers/**`)**

```bash
git add internal/crypto/kdf.go internal/crypto/kdf_test.go
git commit -m "$(cat <<'EOF'
fix: bound Argon2id KDF params on unlock

Reject corrupted or hostile keyring parameters that would force unbounded
memory or time during DeriveKey.
EOF
)"
```

---

### Task 2: SyncContext + Change contract

**Files:**
- Modify: `internal/sync/contract.go`
- Modify: `internal/sync/contract_test.go`

**Interfaces:**
- Consumes: existing `Change`, `validateIdentifier`
- Produces:
  - `const SyncProtocolVersion uint32 = 1`
  - `type SyncContext struct { AccountID, DeviceID string; ProtocolVersion uint32; OperationID string; EntityVersion uint32 }`
  - `func (c SyncContext) Validate() error`
  - `func NewOperationID() (string, error)`
  - `Change` gains: `AccountID`, `DeviceID`, `ProtocolVersion`, `OperationID`, `EntityVersion`, `KeyID`, `ContentDigest string`
  - `NewChange` signature updated to accept context + keyID + contentDigest

- [ ] **Step 1: Write failing validation tests**

Add to `internal/sync/contract_test.go` (create file if needed; follow existing test style in `contract_test.go`):

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/sync -run 'TestSyncContextValidate|TestNewOperationIDUnique' -count=1`

Expected: FAIL (types/funcs undefined)

- [ ] **Step 3: Implement contract types**

In `internal/sync/contract.go` add imports `"crypto/rand"` and `"encoding/hex"` as needed, then:

```go
const SyncProtocolVersion uint32 = 1

type SyncContext struct {
	AccountID       string
	DeviceID        string
	ProtocolVersion uint32
	OperationID     string
	EntityVersion   uint32
}

func (c SyncContext) Validate() error {
	if err := validateIdentifier("account id", c.AccountID, 256); err != nil {
		return err
	}
	if err := validateIdentifier("device id", c.DeviceID, 256); err != nil {
		return err
	}
	if c.ProtocolVersion != SyncProtocolVersion {
		return fmt.Errorf("unsupported sync protocol version %d", c.ProtocolVersion)
	}
	if err := validateIdentifier("operation id", c.OperationID, 256); err != nil {
		return err
	}
	if c.EntityVersion == 0 {
		return errors.New("entity version must be positive")
	}
	return nil
}

func NewOperationID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
```

Extend `Change`:

```go
type Change struct {
	IdempotencyKey  string
	RecordID        string
	RecordType      string
	Operation       Operation
	Payload         []byte
	KeyVersion      uint32
	AccountID       string
	DeviceID        string
	ProtocolVersion uint32
	OperationID     string
	EntityVersion   uint32
	KeyID           string
	ContentDigest   string
	CreatedAt       time.Time
	Status          Status
	Attempts        int
	NextAttemptAt   time.Time
	LastError       FailureReason
	Conflict        *Conflict
	SyncingSince    time.Time
}
```

Update `NewChange` to:

```go
func NewChange(
	idempotencyKey, recordID, recordType string,
	operation Operation,
	payload []byte,
	keyVersion uint32,
	ctx SyncContext,
	keyID, contentDigest string,
	createdAt time.Time,
) Change {
	return Change{
		IdempotencyKey:  idempotencyKey,
		RecordID:        recordID,
		RecordType:      recordType,
		Operation:       operation,
		Payload:         append([]byte(nil), payload...),
		KeyVersion:      keyVersion,
		AccountID:       ctx.AccountID,
		DeviceID:        ctx.DeviceID,
		ProtocolVersion: ctx.ProtocolVersion,
		OperationID:     ctx.OperationID,
		EntityVersion:   ctx.EntityVersion,
		KeyID:           keyID,
		ContentDigest:   contentDigest,
		CreatedAt:       createdAt,
		Status:          StatusPending,
		NextAttemptAt:   createdAt,
	}
}
```

In `Validate()`, after existing checks, also validate sync context fields + key id + content digest:

```go
ctx := SyncContext{
	AccountID: c.AccountID, DeviceID: c.DeviceID,
	ProtocolVersion: c.ProtocolVersion, OperationID: c.OperationID,
	EntityVersion: c.EntityVersion,
}
if err := ctx.Validate(); err != nil {
	return err
}
if err := validateIdentifier("key id", c.KeyID, 256); err != nil {
	return err
}
if err := validateIdentifier("content digest", c.ContentDigest, 128); err != nil {
	return err
}
```

Update every existing test that calls `NewChange` / constructs `Change` literals across `internal/sync` and `internal/storage` to include the new required fields (use a shared test helper `testSyncContext()` in sync tests).

- [ ] **Step 4: Run package tests and fix call sites until green for compile**

Run: `go test ./internal/sync ./internal/storage -count=1`

Expected: may still fail behavioral tests until later tasks; must compile. If storage tests construct `Change`, update them now with placeholder context fields so the package builds.

- [ ] **Step 5: Commit (code only)**

```bash
git add internal/sync/contract.go internal/sync/contract_test.go \
  internal/sync/*.go internal/storage/syncoutbox.go internal/storage/syncoutbox_test.go
git commit -m "$(cat <<'EOF'
feat(sync): add SyncContext and operation identity fields

Carry account, device, protocol, operation, and entity version on Change
so later envelope/queue work can bind them cryptographically.
EOF
)"
```

Only stage files that actually changed; never `docs/superpowers/**`.

---

### Task 3: Replication envelope v2 + context AAD

**Files:**
- Modify: `internal/sync/envelope.go`
- Modify: `internal/sync/envelope_test.go`

**Interfaces:**
- Consumes: `SyncContext`
- Produces:
  - `ReplicationEnvelopeVersion = 2`
  - `ReplicationEnvelope` gains AccountID, DeviceID, ProtocolVersion, OperationID, EntityVersion
  - `SealPayload(vault, ctx SyncContext, recordID, recordType, keyID string, keyVersion uint32, plaintext []byte) (ReplicationEnvelope, error)`
  - `OpenPayload(vault, envelope, keyID string) ([]byte, error)` uses envelope context fields for AAD

Canonical AAD packing (into crypto.AssociatedData):

```go
func replicationAssociatedData(ctx SyncContext, recordID, recordType, keyID string, keyVersion uint32) crypto.AssociatedData {
	recordTypeAD := fmt.Sprintf(
		"%s/v%d/protocol/%d/account/%s/device/%s/key-version/%d/op/%s/entity/%d/%s",
		replicationAADNamespace,
		ReplicationEnvelopeVersion,
		ctx.ProtocolVersion,
		ctx.AccountID,
		ctx.DeviceID,
		keyVersion,
		ctx.OperationID,
		ctx.EntityVersion,
		recordType,
	)
	return crypto.AssociatedData{
		RecordType: recordTypeAD,
		RecordID:   recordID,
		KeyID:      keyID,
	}
}
```

- [ ] **Step 1: Rewrite envelope tests for context**

Replace `SealPayload` call sites with context-aware calls. Add:

```go
func testContext(opID string) SyncContext {
	return SyncContext{
		AccountID: "acct-1", DeviceID: "dev-1",
		ProtocolVersion: SyncProtocolVersion,
		OperationID: opID, EntityVersion: 1,
	}
}

func TestSealPayloadBindsSyncContext(t *testing.T) {
	vault := testVault{key: bytes.Repeat([]byte{0x42}, 32)}
	plaintext := []byte("secret")
	env, err := SealPayload(vault, testContext("op-1"), "record-1", "note", "note-body", 1, plaintext)
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
```

- [ ] **Step 2: Run failing tests**

Run: `go test ./internal/sync -run 'TestSealPayload|TestReplicationEnvelope' -count=1`

Expected: FAIL on new signatures / v2 expectations

- [ ] **Step 3: Implement envelope v2**

Update `ReplicationEnvelope` struct + `Validate` (require context identifiers, `EnvelopeVersion == 2`, `ProtocolVersion == SyncProtocolVersion`).

Update `SealPayload` / `OpenPayload` / `validateReplicationInputs` to take/validate `SyncContext` and populate public fields from it.

- [ ] **Step 4: Pass tests**

Run: `go test ./internal/sync -run 'TestSealPayload|TestReplicationEnvelope' -count=1`

Expected: PASS

- [ ] **Step 5: Commit (code only)**

```bash
git add internal/sync/envelope.go internal/sync/envelope_test.go
git commit -m "$(cat <<'EOF'
feat(sync): bind account/device/protocol/operation into replication AAD

Bump replication envelope to v2 so ciphertext cannot be replayed across
account, device, or operation context.
EOF
)"
```

---

### Task 4: Operation-id idempotency + content digest

**Files:**
- Modify: `internal/sync/queue.go`
- Modify: `internal/sync/queue_test.go`

**Interfaces:**
- Consumes: `SyncContext`, `SealPayload`, `NewChange`
- Produces:
  - `queueIdempotencyNamespace = "dev-kit/sync/queue/v2"`
  - `QueuePayload(vault, outbox, ctx SyncContext, recordID, recordType, keyID string, keyVersion uint32, operation Operation, plaintext []byte, createdAt time.Time) (Change, error)`
  - Idempotency key = namespace + ":" + hex(sha256(namespace || 0x00 || operation_id))
  - Conflict compare includes keyID + content digest + full sync context; excludes `created_at`

- [ ] **Step 1: Write failing queue tests**

```go
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
```

Update older queue tests to the new signature.

- [ ] **Step 2: Run failing tests**

Run: `go test ./internal/sync -run 'TestQueuePayload' -count=1`

Expected: FAIL

- [ ] **Step 3: Implement queue v2**

```go
const queueIdempotencyNamespace = "dev-kit/sync/queue/v2"

func QueuePayload(
	vault Vault,
	outbox Outbox,
	ctx SyncContext,
	recordID, recordType, keyID string,
	keyVersion uint32,
	operation Operation,
	plaintextRecord []byte,
	createdAt time.Time,
) (Change, error) {
	// validate deps + ctx.Validate() + validateQueueInputs...
	digest := sha256.Sum256(plaintextRecord)
	contentDigest := hex.EncodeToString(digest[:])
	idempotencyKey := queueIdempotencyKey(ctx.OperationID)

	if existing, err := outbox.Get(idempotencyKey); err == nil {
		if !sameQueueIdentity(existing, ctx, recordID, recordType, keyID, keyVersion, operation, contentDigest) {
			return Change{}, ErrIdempotencyConflict
		}
		return cloneChange(existing), nil
	} else if !errors.Is(err, ErrNotFound) {
		return Change{}, err
	}

	envelope, err := SealPayload(vault, ctx, recordID, recordType, keyID, keyVersion, plaintextRecord)
	// marshal, NewChange(..., ctx, keyID, contentDigest, createdAt), Enqueue with race re-get
}

func queueIdempotencyKey(operationID string) string {
	canonical := queueIdempotencyNamespace + "\x00" + operationID
	sum := sha256.Sum256([]byte(canonical))
	return queueIdempotencyNamespace + ":" + hex.EncodeToString(sum[:])
}

func sameQueueIdentity(change Change, ctx SyncContext, recordID, recordType, keyID string, keyVersion uint32, operation Operation, contentDigest string) bool {
	return change.RecordID == recordID &&
		change.RecordType == recordType &&
		change.KeyVersion == keyVersion &&
		change.Operation == operation &&
		change.KeyID == keyID &&
		change.ContentDigest == contentDigest &&
		change.AccountID == ctx.AccountID &&
		change.DeviceID == ctx.DeviceID &&
		change.ProtocolVersion == ctx.ProtocolVersion &&
		change.OperationID == ctx.OperationID &&
		change.EntityVersion == ctx.EntityVersion
}
```

- [ ] **Step 4: Pass tests**

Run: `go test ./internal/sync -count=1`

Expected: PASS for queue tests; fix any remaining call sites.

- [ ] **Step 5: Commit (code only)**

```bash
git add internal/sync/queue.go internal/sync/queue_test.go
git commit -m "$(cat <<'EOF'
fix(sync): key idempotency by operation id and content digest

Prevent silent data loss when the same operation id is reused with
different plaintext or key identity.
EOF
)"
```

---

### Task 5: Memory outbox lease + RecoverStaleSyncing

**Files:**
- Modify: `internal/sync/outbox.go`
- Modify: `internal/sync/outbox_test.go`
- Modify: `internal/sync/runner.go`
- Modify: `internal/sync/runner_test.go`
- Modify: `internal/sync/transport.go` (expand `sameTransportChange` / `sameImmutableChange`)

**Interfaces:**
- Consumes: `Change.SyncingSince`, context fields on `Change`
- Produces:
  - `type MemoryOutboxConfig struct { Lease time.Duration }`
  - `func NewMemoryOutbox(config ...MemoryOutboxConfig) *MemoryOutbox` (default lease 5m)
  - `Outbox` gains `RecoverStaleSyncing(now time.Time) error`
  - `MarkSyncing(idempotencyKey string, at time.Time) error`
  - `Pending` also returns stale syncing rows (`SyncingSince <= now-lease`)
  - Terminal transitions clear `SyncingSince`
  - `RunnerConfig.SyncingLease` documented to match outbox lease (runner does not own reclaim logic beyond calling MarkSyncing with `now`)

- [ ] **Step 1: Write failing lease tests**

```go
func TestMemoryOutboxReclaimsStaleSyncing(t *testing.T) {
	outbox := NewMemoryOutbox(MemoryOutboxConfig{Lease: time.Minute})
	now := time.Unix(1000, 0).UTC()
	change := testPendingChange("k1", now)
	if err := outbox.Enqueue(change); err != nil {
		t.Fatal(err)
	}
	if err := outbox.MarkSyncing("k1", now); err != nil {
		t.Fatal(err)
	}
	if pending, _ := outbox.Pending(now.Add(30*time.Second), 0); len(pending) != 0 {
		t.Fatal("fresh syncing must not be pending")
	}
	ready, err := outbox.Pending(now.Add(time.Minute+time.Second), 0)
	if err != nil || len(ready) != 1 {
		t.Fatalf("stale syncing reclaim: len=%d err=%v", len(ready), err)
	}
}

func TestRecoverStaleSyncing(t *testing.T) {
	outbox := NewMemoryOutbox(MemoryOutboxConfig{Lease: time.Minute})
	now := time.Unix(1000, 0).UTC()
	_ = outbox.Enqueue(testPendingChange("k1", now))
	_ = outbox.MarkSyncing("k1", now)
	if err := outbox.RecoverStaleSyncing(now.Add(2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, _ := outbox.Get("k1")
	if got.Status != StatusPending || !got.SyncingSince.IsZero() {
		t.Fatalf("recovered = %+v", got)
	}
}
```

Add `testPendingChange` helper that fills all required `Change` fields.

- [ ] **Step 2: Run failing tests**

Run: `go test ./internal/sync -run 'TestMemoryOutboxReclaimsStaleSyncing|TestRecoverStaleSyncing' -count=1`

Expected: FAIL

- [ ] **Step 3: Implement outbox lease + runner MarkSyncing(at)**

`MemoryOutbox` fields: `lease time.Duration` (default `5*time.Minute`).

`MarkSyncing(key, at)`: allow from `pending`, or from `syncing` when already stale (lease refresh); set `StatusSyncing`, `SyncingSince=at`.

`Pending`: include pending-ready OR stale-syncing.

`RecoverStaleSyncing`: for each stale syncing, set pending, clear SyncingSince (keep NextAttemptAt as now or existing — use `now` if zero).

Update `MarkSent`/`MarkRetry`/`MarkFailed`/`MarkConflict` to zero `SyncingSince`.

Update `sameImmutableChange` / `sameTransportChange` to include AccountID, DeviceID, ProtocolVersion, OperationID, EntityVersion, KeyID, ContentDigest (and payload bytes as today). Drop `CreatedAt` from immutable compare if still present — enqueue conflict path should match queue semantics; keep payload equality for transport duplicates.

Update runner:

```go
if err := r.outbox.MarkSyncing(change.IdempotencyKey, now); err != nil {
```

Fix all call sites/tests.

- [ ] **Step 4: Pass sync package tests**

Run: `go test ./internal/sync -count=1`

Expected: PASS

- [ ] **Step 5: Commit (code only)**

```bash
git add internal/sync/outbox.go internal/sync/outbox_test.go \
  internal/sync/runner.go internal/sync/runner_test.go internal/sync/transport.go
git commit -m "$(cat <<'EOF'
fix(sync): reclaim stale syncing outbox entries with a lease

Prevent permanently stuck syncing rows after cancel, crash, or failed
MarkSent by leasing claims and recovering on Pending/startup.
EOF
)"
```

---

### Task 6: SQL identity store + migration m8 + SQLOutbox lease

**Files:**
- Modify: `internal/storage/sqlite.go`
- Create: `internal/storage/syncidentity.go`
- Create: `internal/storage/syncidentity_test.go`
- Modify: `internal/storage/syncoutbox.go`
- Modify: `internal/storage/syncoutbox_test.go`
- Modify: `internal/storage/migrations_test.go`

**Interfaces:**
- Consumes: `replicationsync.Outbox`, `SyncContext` fields on `Change`
- Produces:
  - `const TableSyncIdentity = "sync_identity"`
  - `type SQLIdentityStore struct{ db *sql.DB }`
  - `func NewSQLIdentityStore(db *sql.DB) *SQLIdentityStore`
  - `func (s *SQLIdentityStore) EnsureLocal(ctx context.Context) (accountID, deviceID string, err error)`
  - Migration version **8**: create `sync_identity`; `ALTER TABLE sync_outbox ADD COLUMN` for each new field
  - `SQLOutbox` constructor takes lease (`NewSQLOutbox(db, lease time.Duration)` or config); implements `RecoverStaleSyncing`, lease-aware `Pending`, `MarkSyncing(key, at)`

Migration SQL (SQLite):

```sql
CREATE TABLE IF NOT EXISTS sync_identity (
  id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL,
  device_id TEXT NOT NULL,
  created_at TEXT NOT NULL
);

ALTER TABLE sync_outbox ADD COLUMN syncing_since TEXT NOT NULL DEFAULT '';
ALTER TABLE sync_outbox ADD COLUMN account_id TEXT NOT NULL DEFAULT '';
ALTER TABLE sync_outbox ADD COLUMN device_id TEXT NOT NULL DEFAULT '';
ALTER TABLE sync_outbox ADD COLUMN protocol_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sync_outbox ADD COLUMN operation_id TEXT NOT NULL DEFAULT '';
ALTER TABLE sync_outbox ADD COLUMN entity_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sync_outbox ADD COLUMN key_id TEXT NOT NULL DEFAULT '';
ALTER TABLE sync_outbox ADD COLUMN content_digest TEXT NOT NULL DEFAULT '';
```

`EnsureLocal`: if row `id='local'` missing, insert UUIDs (use `crypto/rand` + hex, same as `NewOperationID` style). Return existing values otherwise.

For `SQLOutbox.Enqueue`/`scan`: read/write new columns. `Get`/`Pending` must `Validate()` — empty context defaults from pre-m8 rows fail closed.

`Pending` SQL sketch:

```sql
WHERE (
  (status = 'pending' AND next_attempt_at <= ?)
  OR (status = 'syncing' AND syncing_since != '' AND syncing_since <= ?)
)
ORDER BY created_at ASC, idempotency_key ASC
```

Where the second bound is `formatTime(now.Add(-lease))`.

- [ ] **Step 1: Write failing identity + migration tests**

```go
func TestEnsureLocalPersistsStableIDs(t *testing.T) {
	db := openTestDB(t) // use existing test DB helper in storage tests
	store := NewSQLIdentityStore(db)
	a1, d1, err := store.EnsureLocal(context.Background())
	if err != nil || a1 == "" || d1 == "" {
		t.Fatalf("first: %v %q %q", err, a1, d1)
	}
	a2, d2, err := store.EnsureLocal(context.Background())
	if err != nil || a1 != a2 || d1 != d2 {
		t.Fatalf("second differed: %q/%q vs %q/%q err=%v", a1, d1, a2, d2, err)
	}
}
```

Extend `migrations_test.go` column list with the new outbox columns and assert `sync_identity` exists.

Add SQL outbox stale reclaim test mirroring memory test.

- [ ] **Step 2: Run failing tests**

Run: `go test ./internal/storage -run 'TestEnsureLocal|TestMigrate|SyncOutbox|sync_outbox' -count=1`

Expected: FAIL (missing table/columns/API)

- [ ] **Step 3: Implement m8 + identity + SQLOutbox updates**

Wire `m8` into `Migrate` after `m7`. Update all SQL statements in `syncoutbox.go`. Implement `RecoverStaleSyncing`. Update constructor call sites (`NewSQLOutbox`) — default lease 5m if no config.

- [ ] **Step 4: Pass storage + sync tests**

Run: `go test ./internal/storage ./internal/sync -count=1`

Expected: PASS

- [ ] **Step 5: Commit (code only)**

```bash
git add internal/storage/sqlite.go internal/storage/syncidentity.go \
  internal/storage/syncidentity_test.go internal/storage/syncoutbox.go \
  internal/storage/syncoutbox_test.go internal/storage/migrations_test.go
git commit -m "$(cat <<'EOF'
feat(storage): persist sync identity and outbox lease columns

Add local account/device identity and reclaimable syncing_since so the
SQL outbox matches the sync security contract.
EOF
)"
```

---

### Task 7: Pin Go toolchain to 1.26.5 + govulncheck

**Files:**
- Modify: `go.mod`
- Possibly: `go.sum` if toolchain download updates modules (usually not)

**Interfaces:**
- Produces: `go 1.26.5` and `toolchain go1.26.5` lines in `go.mod`

- [ ] **Step 1: Edit go.mod**

Replace the `go` line and add toolchain:

```go
go 1.26.5

toolchain go1.26.5
```

- [ ] **Step 2: Sync toolchain**

Run: `GOTOOLCHAIN=go1.26.5 go version`

Expected: `go version go1.26.5 ...` (may download toolchain)

Run: `go mod tidy`

- [ ] **Step 3: Install/run govulncheck**

Run:

```bash
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

Expected: GO-2026-5856 no longer reported as reachable via `application.App.Run`. If other unreachable findings remain, leave them; only fail the task if GO-2026-5856 is still reachable.

- [ ] **Step 4: Run full internal tests**

Run: `go test ./internal/... -count=1`

Expected: PASS

- [ ] **Step 5: Commit (code only)**

```bash
git add go.mod go.sum
git commit -m "$(cat <<'EOF'
chore: pin Go toolchain to 1.26.5 for crypto/tls fix

Pick up GO-2026-5856 remediation before remote sync work lands.
EOF
)"
```

---

### Task 8: Frontend audit fixes + move shadcn to devDependencies

**Files:**
- Modify: `frontend/package.json`
- Modify: `frontend/bun.lock`

**Interfaces:**
- Produces: `shadcn` under `devDependencies`; audited transitive versions updated

- [ ] **Step 1: Move shadcn**

In `frontend/package.json`, remove `"shadcn": "^4.12.0"` from `dependencies` and add it under `devDependencies`.

- [ ] **Step 2: Update lockfile / advisory packages**

From `frontend/`:

```bash
bun update
bun audit
```

If specific packages remain vulnerable, bump them explicitly, e.g.:

```bash
bun update postcss fast-uri brace-expansion @hono/node-server
bun audit
```

Expected: no high/moderate advisories from the review list, or document any residual that cannot be fixed without breaking Vite in the commit message body (prefer fix).

- [ ] **Step 3: Build frontend**

Run: `bun run build`

Expected: exit 0

- [ ] **Step 4: Commit (code only)**

```bash
git add frontend/package.json frontend/bun.lock
git commit -m "$(cat <<'EOF'
chore(frontend): clear bun audit findings and demote shadcn

Move the shadcn CLI to devDependencies and refresh the lockfile so
build-time advisories are not carried as runtime deps.
EOF
)"
```

---

### Task 9: Final verification gate

**Files:** none new

- [ ] **Step 1: Go tests**

Run: `go test ./internal/sync/... ./internal/storage/... ./internal/crypto/... -count=1`

Expected: PASS

- [ ] **Step 2: Race subset (sync/storage)**

Run: `go test ./internal/sync ./internal/storage -race -count=1`

Expected: PASS

- [ ] **Step 3: govulncheck**

Run: `go run golang.org/x/vuln/cmd/govulncheck@latest ./...`

Expected: GO-2026-5856 not reachable

- [ ] **Step 4: Frontend**

Run:

```bash
cd frontend && bun audit && bun run build
```

Expected: audit clean (or only pre-agreed residuals) and build OK

- [ ] **Step 5: Do not commit superpowers docs**

Confirm `git status` does not stage `docs/superpowers/**`. Leave plan/spec untracked or dirty locally without committing them.

---

## Spec coverage checklist

| Spec requirement | Task |
|---|---|
| Local `sync_identity` account/device | Task 6 |
| `SyncContext` required on seal/queue/push path | Tasks 2–4 |
| Envelope v2 + AAD bind account/device/protocol/op/entity | Task 3 |
| `Change` carries context; transport compares expanded identity | Tasks 2, 5 |
| Idempotency by `operation_id` + digest/key conflict | Task 4 |
| Outbox `syncing_since` lease + RecoverStaleSyncing | Tasks 5–6 |
| Go 1.26.5 toolchain pin + govulncheck | Tasks 7, 9 |
| Frontend audit + shadcn → devDependencies | Tasks 8–9 |
| KDF bounds | Task 1 |
| No remote transport / UI | Global constraints |
| Never commit `docs/superpowers/**` | Global constraints + every commit step |

## Self-review notes

- No TBD/placeholder steps remain; AAD packing format is explicit and reuses crypto.AssociatedData.
- `MarkSyncing(key, at time.Time)` is an intentional interface change called out in Task 5 so tests inject deterministic time.
- `NewSQLOutbox` signature change is confined to Task 6; update all call sites found by `go test`/compile.
- Commit steps never include `docs/superpowers/**`.
