# Core Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the DevKit core foundation so future modules can use a stable Wails bridge, command registry, local storage, crypto envelope, a Master-Password-protected vault, permission model, and Capability Gateway.

**Architecture:** Keep Wails as a thin transport layer. Go core packages own application behavior, storage, crypto, vault, commands, permissions, and capability checks. Frontend modules call typed bridge adapters instead of generated Wails bindings directly.

**Tech Stack:** Go 1.25, Wails v3, React 19, TypeScript, Vite, built-in Go tests, TypeScript compiler, `golang.org/x/crypto` (Argon2id + XChaCha20-Poly1305).

**Security decisions (locked):**

- A **Master Password is mandatory and is set once at first-run setup**. A brand-new install starts in a `not_configured` state and cannot reach an unlocked vault until `Setup` is called with a Master Password.
- Encryption follows the password-manager model from `docs/architecture/devkit-architecture.md` §9: `Master Password ──Argon2id──▶ KEK ──unwrap──▶ random DEK ──XChaCha20-Poly1305──▶ records`. The DEK is random and wrapped; changing the Master Password rewraps the DEK without re-encrypting records.
- A **recovery key** is generated at setup (zero-knowledge: losing the Master Password otherwise means losing the data) and returned exactly once to be shown to the user; it is never stored in plaintext.
- Do not write custom crypto: use `golang.org/x/crypto` primitives only.

**Extensibility decision (locked):**

- External/untrusted plugins, marketplace, signing, and sandboxing are a **far, non-committed vision**. Phase 0 builds an **internal module platform only**.
- The internal Extension API, permission model, and Capability Gateway must be built to the **extensibility invariants** in `docs/architecture/devkit-architecture.md` §6.10 so an untrusted runtime can be inserted later without rearchitecting. In particular: internal modules go through the Capability Gateway (never call core services directly), carry a subject identity, and cross the boundary with serializable DTOs + opaque secret handles (no live Go pointers/plaintext).

---

## Scope

This plan implements Phase 0 from `docs/architecture/devkit-architecture.md` and `docs/architecture/project-structure-and-code-rules.md`.

Included:

- Target project structure.
- Go application composition root.
- Structured error package.
- Logging/redaction package.
- Local storage abstraction and migration runner skeleton.
- Crypto envelope package.
- Password key derivation (Argon2id) and AEAD seal/open (XChaCha20-Poly1305).
- Key hierarchy / keyring: a random Data Encryption Key wrapped by a password-derived Key Encryption Key, plus a recovery key.
- Vault service with a **mandatory Master Password** set at first-run setup, unlock/lock, and recovery-key unlock.
- Keyring persistence (in-memory for tests + JSON file store for the app).
- Vault bridge methods and a frontend setup/unlock gate that blocks the app until the vault is unlocked.
- Command registry.
- Wails bridge service.
- Frontend bridge adapter layer.
- Minimal workbench shell.
- Internal module registry.
- Permission model skeleton.
- Capability Gateway skeleton.
- First simple core module: hash/encoding tools.

Excluded:

- External plugin loading.
- SaaS backend.
- SQLCipher production integration (records are encrypted at the application layer via the crypto envelope; only the keyring is persisted in Phase 0).
- Persistent encrypted vault item repository (vault item storage/CRUD comes in a later phase).
- Notes/snippets persistence.
- SSH terminal.
- DB manager.
- Marketplace.

## File Map

Create Go files:

- `cmd/devkit/main.go`: future app entrypoint and composition root caller.
- `internal/app/app.go`: wires all core services.
- `internal/app/container.go`: dependency container for app services.
- `internal/apperrors/errors.go`: structured application errors.
- `internal/audit/redact.go`: redaction helpers for logs and audit events.
- `internal/storage/storage.go`: storage interfaces and transaction boundary.
- `internal/storage/migrations.go`: migration runner skeleton.
- `internal/crypto/envelope.go`: versioned encryption envelope structs.
- `internal/crypto/envelope_test.go`: envelope validation tests.
- `internal/crypto/kdf.go`: Argon2id password key derivation and params.
- `internal/crypto/kdf_test.go`: key derivation tests.
- `internal/crypto/aead.go`: XChaCha20-Poly1305 seal/open over the envelope.
- `internal/crypto/aead_test.go`: AEAD round-trip and tamper tests.
- `internal/crypto/keyring.go`: DEK/KEK key hierarchy, wrap/unwrap, recovery key.
- `internal/crypto/keyring_test.go`: keyring unlock/recovery/change-password tests.
- `internal/vault/service.go`: vault service with mandatory Master Password, unlock/lock.
- `internal/vault/service_test.go`: vault state and setup/unlock tests.
- `internal/vault/keyringstore.go`: in-memory and JSON file keyring stores.
- `internal/vault/keyringstore_test.go`: keyring persistence tests.
- `internal/bridge/vault.go`: Wails-facing vault methods (status/setup/unlock/lock).
- `internal/bridge/vault_test.go`: vault bridge tests.
- `frontend/src/bridge/vault.ts`: typed vault adapter over the generated bindings.
- `frontend/src/workbench/VaultGate.tsx`: first-run setup + unlock gate.
- `internal/commands/registry.go`: command registry and dispatch.
- `internal/commands/registry_test.go`: command registry tests.
- `internal/modules/registry.go`: built-in module registry.
- `internal/permissions/model.go`: permission model and scope types.
- `internal/permissions/policy.go`: permission policy checks.
- `internal/permissions/policy_test.go`: permission policy tests.
- `internal/capabilities/gateway.go`: Capability Gateway skeleton.
- `internal/capabilities/gateway_test.go`: gateway permission tests.
- `internal/services/tools/hash.go`: hash/encoding service.
- `internal/services/tools/hash_test.go`: hash/encoding tests.
- `internal/bridge/app_service.go`: Wails-facing app service.
- `internal/bridge/dto.go`: bridge request/response DTOs.

Modify Go files:

- `main.go`: shrink current starter entrypoint or delegate to `cmd/devkit` after Wails config is migrated.
- `greetservice.go`: delete after `AppService` is registered and frontend no longer imports `GreetService`.
- `go.mod`: keep module name stable for now, add packages only when required.

Create frontend files:

- `frontend/src/bridge/errors.ts`: typed frontend error shape.
- `frontend/src/bridge/client.ts`: wrapper over generated Wails bindings.
- `frontend/src/bridge/commands.ts`: command execution adapter.
- `frontend/src/workbench/Workbench.tsx`: app shell.
- `frontend/src/workbench/ActivityBar.tsx`: left navigation placeholder.
- `frontend/src/workbench/CommandPalette.tsx`: command palette skeleton.
- `frontend/src/modules/registry.ts`: frontend module registry.
- `frontend/src/modules/tools/manifest.ts`: tools module manifest.
- `frontend/src/modules/tools/ToolsView.tsx`: hash/encoding UI.

Modify frontend files:

- `frontend/src/App.tsx`: replace starter Wails demo with workbench shell.
- `frontend/src/main.tsx`: keep React bootstrapping unchanged unless it imports deleted starter assets.
- `frontend/package.json`: add test scripts only if test tooling is introduced.

## Task 1: Establish Target Directories

**Files:**

- Create directories only:
  - `cmd/devkit`
  - `internal/app`
  - `internal/apperrors`
  - `internal/audit`
  - `internal/bridge`
  - `internal/capabilities`
  - `internal/commands`
  - `internal/crypto`
  - `internal/modules`
  - `internal/permissions`
  - `internal/services/tools`
  - `internal/storage`
  - `internal/vault`
  - `frontend/src/bridge`
  - `frontend/src/workbench`
  - `frontend/src/modules/tools`

- [ ] **Step 1: Create directories**

Run:

```bash
mkdir -p cmd/devkit internal/app internal/apperrors internal/audit internal/bridge internal/capabilities internal/commands internal/crypto internal/modules internal/permissions internal/services/tools internal/storage internal/vault frontend/src/bridge frontend/src/workbench frontend/src/modules/tools
```

Expected: command exits `0`.

- [ ] **Step 2: Verify directories exist**

Run:

```bash
find internal frontend/src -maxdepth 3 -type d | sort
```

Expected: output includes every directory listed in this task.

- [ ] **Step 3: Run current build before code changes**

Run:

```bash
go test ./...
```

Expected: existing Go packages pass before structural work begins.

- [ ] **Step 4: Run frontend typecheck before code changes**

Run:

```bash
cd frontend && npm run build
```

Expected: existing frontend builds before structural work begins.

- [ ] **Step 5: Commit**

```bash
git add .
git commit -m "chore: create core foundation directories"
```

Expected: commit succeeds if repository has been initialized. If this workspace is still not a git repository, record the command output and continue without committing.

## Task 2: Add Structured Error Package

**Files:**

- Create: `internal/apperrors/errors.go`
- Test: `internal/apperrors/errors_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/apperrors/errors_test.go`:

```go
package apperrors

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestNewCreatesStructuredError(t *testing.T) {
	err := New("vault.locked", "Vault is locked", true)

	if err.Code != "vault.locked" {
		t.Fatalf("Code = %q, want %q", err.Code, "vault.locked")
	}
	if err.Message != "Vault is locked" {
		t.Fatalf("Message = %q", err.Message)
	}
	if !err.Recoverable {
		t.Fatal("Recoverable = false, want true")
	}
}

func TestWrapPreservesCauseWithoutLeakingInJSON(t *testing.T) {
	cause := errors.New("raw database password leaked")
	err := Wrap("storage.failed", "Storage operation failed", false, cause)

	if !errors.Is(err, cause) {
		t.Fatal("wrapped error should match original cause")
	}

	payload, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("json.Marshal returned error: %v", marshalErr)
	}

	if string(payload) == "" {
		t.Fatal("payload should not be empty")
	}
	if contains(string(payload), "raw database password leaked") {
		t.Fatalf("json payload leaked cause: %s", payload)
	}
}

func contains(value string, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/apperrors
```

Expected: FAIL because `New`, `Wrap`, and `AppError` are not defined.

- [ ] **Step 3: Implement structured errors**

Create `internal/apperrors/errors.go`:

```go
package apperrors

type AppError struct {
	Code        string         `json:"code"`
	Message     string         `json:"message"`
	Recoverable bool           `json:"recoverable"`
	Details     map[string]any `json:"details,omitempty"`
	cause       error
}

func New(code string, message string, recoverable bool) *AppError {
	return &AppError{
		Code:        code,
		Message:     message,
		Recoverable: recoverable,
	}
}

func WithDetails(code string, message string, recoverable bool, details map[string]any) *AppError {
	return &AppError{
		Code:        code,
		Message:     message,
		Recoverable: recoverable,
		Details:     details,
	}
}

func Wrap(code string, message string, recoverable bool, cause error) *AppError {
	return &AppError{
		Code:        code,
		Message:     message,
		Recoverable: recoverable,
		cause:       cause,
	}
}

func (e *AppError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

func (e *AppError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/apperrors
```

Expected: PASS.

- [ ] **Step 5: Run all Go tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/apperrors
git commit -m "feat: add structured app errors"
```

## Task 3: Add Redaction Helpers

**Files:**

- Create: `internal/audit/redact.go`
- Test: `internal/audit/redact_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/audit/redact_test.go`:

```go
package audit

import "testing"

func TestRedactSensitiveKeys(t *testing.T) {
	input := map[string]any{
		"username": "alice",
		"password": "secret",
		"token": "abc",
		"nested": map[string]any{
			"private_key": "ssh-key",
			"host": "localhost",
		},
	}

	output := RedactMap(input)

	if output["username"] != "alice" {
		t.Fatalf("username = %v", output["username"])
	}
	if output["password"] != RedactedValue {
		t.Fatalf("password was not redacted")
	}
	if output["token"] != RedactedValue {
		t.Fatalf("token was not redacted")
	}

	nested, ok := output["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested map type = %T", output["nested"])
	}
	if nested["private_key"] != RedactedValue {
		t.Fatalf("private_key was not redacted")
	}
	if nested["host"] != "localhost" {
		t.Fatalf("host = %v", nested["host"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/audit
```

Expected: FAIL because `RedactMap` and `RedactedValue` are not defined.

- [ ] **Step 3: Implement redaction**

Create `internal/audit/redact.go`:

```go
package audit

import "strings"

const RedactedValue = "[REDACTED]"

var sensitiveKeyParts = []string{
	"password",
	"passphrase",
	"private_key",
	"privatekey",
	"secret",
	"token",
	"api_key",
	"apikey",
	"recovery",
	"encryption_key",
}

func RedactMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		if IsSensitiveKey(key) {
			output[key] = RedactedValue
			continue
		}
		switch typed := value.(type) {
		case map[string]any:
			output[key] = RedactMap(typed)
		default:
			output[key] = value
		}
	}
	return output
}

func IsSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	for _, part := range sensitiveKeyParts {
		if strings.Contains(normalized, part) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/audit
```

Expected: PASS.

- [ ] **Step 5: Run all Go tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/audit
git commit -m "feat: add audit redaction helpers"
```

## Task 4: Add Storage Interfaces and Migration Runner

**Files:**

- Create: `internal/storage/storage.go`
- Create: `internal/storage/migrations.go`
- Test: `internal/storage/migrations_test.go`

- [ ] **Step 1: Write failing migration tests**

Create `internal/storage/migrations_test.go`:

```go
package storage

import (
	"context"
	"testing"
)

func TestMigrationRunnerRunsPendingMigrationsInOrder(t *testing.T) {
	ctx := context.Background()
	var applied []int

	store := NewMemoryMigrationStore(1)
	runner := NewMigrationRunner(store, []Migration{
		{
			Version: 1,
			Name:    "already applied",
			Up: func(ctx context.Context) error {
				applied = append(applied, 1)
				return nil
			},
		},
		{
			Version: 2,
			Name:    "second",
			Up: func(ctx context.Context) error {
				applied = append(applied, 2)
				return nil
			},
		},
		{
			Version: 3,
			Name:    "third",
			Up: func(ctx context.Context) error {
				applied = append(applied, 3)
				return nil
			},
		},
	})

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(applied) != 2 || applied[0] != 2 || applied[1] != 3 {
		t.Fatalf("applied = %v, want [2 3]", applied)
	}

	version, err := store.CurrentVersion(ctx)
	if err != nil {
		t.Fatalf("CurrentVersion returned error: %v", err)
	}
	if version != 3 {
		t.Fatalf("version = %d, want 3", version)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/storage
```

Expected: FAIL because migration types are not defined.

- [ ] **Step 3: Add storage interfaces**

Create `internal/storage/storage.go`:

```go
package storage

import "context"

type Transaction interface {
	Commit() error
	Rollback() error
}

type Transactioner interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context, tx Transaction) error) error
}
```

- [ ] **Step 4: Add migration runner**

Create `internal/storage/migrations.go`:

```go
package storage

import (
	"context"
	"fmt"
	"sort"
)

type Migration struct {
	Version int
	Name    string
	Up      func(ctx context.Context) error
}

type MigrationStore interface {
	CurrentVersion(ctx context.Context) (int, error)
	MarkApplied(ctx context.Context, version int) error
}

type MigrationRunner struct {
	store      MigrationStore
	migrations []Migration
}

func NewMigrationRunner(store MigrationStore, migrations []Migration) *MigrationRunner {
	copied := append([]Migration(nil), migrations...)
	sort.Slice(copied, func(i int, j int) bool {
		return copied[i].Version < copied[j].Version
	})
	return &MigrationRunner{store: store, migrations: copied}
}

func (r *MigrationRunner) Run(ctx context.Context) error {
	current, err := r.store.CurrentVersion(ctx)
	if err != nil {
		return fmt.Errorf("read current migration version: %w", err)
	}

	for _, migration := range r.migrations {
		if migration.Version <= current {
			continue
		}
		if migration.Up == nil {
			return fmt.Errorf("migration %d %q has no Up function", migration.Version, migration.Name)
		}
		if err := migration.Up(ctx); err != nil {
			return fmt.Errorf("run migration %d %q: %w", migration.Version, migration.Name, err)
		}
		if err := r.store.MarkApplied(ctx, migration.Version); err != nil {
			return fmt.Errorf("mark migration %d applied: %w", migration.Version, err)
		}
		current = migration.Version
	}

	return nil
}

type MemoryMigrationStore struct {
	version int
}

func NewMemoryMigrationStore(version int) *MemoryMigrationStore {
	return &MemoryMigrationStore{version: version}
}

func (s *MemoryMigrationStore) CurrentVersion(ctx context.Context) (int, error) {
	return s.version, nil
}

func (s *MemoryMigrationStore) MarkApplied(ctx context.Context, version int) error {
	s.version = version
	return nil
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./internal/storage
```

Expected: PASS.

- [ ] **Step 6: Run all Go tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/storage
git commit -m "feat: add storage migration skeleton"
```

## Task 5: Add Crypto Envelope

**Files:**

- Create: `internal/crypto/envelope.go`
- Test: `internal/crypto/envelope_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/crypto/envelope_test.go`:

```go
package crypto

import "testing"

func TestEncryptedRecordEnvelopeValidate(t *testing.T) {
	envelope := EncryptedRecordEnvelope{
		Version:    1,
		Algorithm:  AlgorithmXChaCha20Poly1305,
		KeyID:      "dek_1",
		Nonce:      []byte("nonce"),
		Ciphertext: []byte("ciphertext"),
		AssociatedData: map[string]string{
			"entityType": "note",
			"entityId":   "note_1",
		},
	}

	if err := envelope.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestEncryptedRecordEnvelopeRejectsMissingKeyID(t *testing.T) {
	envelope := EncryptedRecordEnvelope{
		Version:    1,
		Algorithm:  AlgorithmXChaCha20Poly1305,
		Nonce:      []byte("nonce"),
		Ciphertext: []byte("ciphertext"),
	}

	if err := envelope.Validate(); err == nil {
		t.Fatal("Validate returned nil, want error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/crypto
```

Expected: FAIL because `EncryptedRecordEnvelope` is not defined.

- [ ] **Step 3: Implement envelope**

Create `internal/crypto/envelope.go`:

```go
package crypto

import "errors"

const (
	AlgorithmXChaCha20Poly1305 = "xchacha20-poly1305"
	AlgorithmAES256GCM         = "aes-256-gcm"
)

type EncryptedRecordEnvelope struct {
	Version        int               `json:"version"`
	Algorithm      string            `json:"algorithm"`
	KeyID          string            `json:"keyId"`
	Nonce          []byte            `json:"nonce"`
	AssociatedData map[string]string `json:"associatedData,omitempty"`
	Ciphertext     []byte            `json:"ciphertext"`
}

func (e EncryptedRecordEnvelope) Validate() error {
	if e.Version <= 0 {
		return errors.New("version is required")
	}
	if e.Algorithm == "" {
		return errors.New("algorithm is required")
	}
	if e.Algorithm != AlgorithmXChaCha20Poly1305 && e.Algorithm != AlgorithmAES256GCM {
		return errors.New("algorithm is unsupported")
	}
	if e.KeyID == "" {
		return errors.New("key id is required")
	}
	if len(e.Nonce) == 0 {
		return errors.New("nonce is required")
	}
	if len(e.Ciphertext) == 0 {
		return errors.New("ciphertext is required")
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/crypto
```

Expected: PASS.

- [ ] **Step 5: Run all Go tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/crypto
git commit -m "feat: add versioned crypto envelope"
```

## Task 5.1: Add Password Key Derivation (Argon2id)

**Files:**

- Create: `internal/crypto/kdf.go`
- Test: `internal/crypto/kdf_test.go`
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add the crypto dependency**

Run:

```bash
go get golang.org/x/crypto/argon2
```

Expected: command exits `0`; `golang.org/x/crypto` appears in `go.mod`.

- [ ] **Step 2: Write failing tests**

Create `internal/crypto/kdf_test.go`:

```go
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
```

- [ ] **Step 3: Run test to verify it fails**

Run:

```bash
go test ./internal/crypto
```

Expected: FAIL because `KDFParams`, `DefaultKDFParams`, `DeriveKey` are not defined.

- [ ] **Step 4: Implement key derivation**

Create `internal/crypto/kdf.go`:

```go
package crypto

import (
	"crypto/rand"
	"errors"

	"golang.org/x/crypto/argon2"
)

const AlgorithmArgon2id = "argon2id"

// KDFParams holds Argon2id parameters plus the salt. Persist it next to the
// wrapped key so derivation stays reproducible after upgrades.
type KDFParams struct {
	Algorithm   string `json:"algorithm"`
	Memory      uint32 `json:"memory"`
	Iterations  uint32 `json:"iterations"`
	Parallelism uint8  `json:"parallelism"`
	KeyLength   uint32 `json:"keyLength"`
	Salt        []byte `json:"salt"`
}

// DefaultKDFParams returns interactive-strength Argon2id params with a fresh
// random 16-byte salt.
// ponytail: fixed cost (64MB, 3 passes, 4 lanes); add a "hardened" tier later if needed.
func DefaultKDFParams() (KDFParams, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return KDFParams{}, err
	}
	return KDFParams{
		Algorithm:   AlgorithmArgon2id,
		Memory:      64 * 1024,
		Iterations:  3,
		Parallelism: 4,
		KeyLength:   32,
		Salt:        salt,
	}, nil
}

// DeriveKey derives a key of params.KeyLength bytes from the password.
func DeriveKey(password string, params KDFParams) ([]byte, error) {
	if password == "" {
		return nil, errors.New("password is required")
	}
	if params.Algorithm != AlgorithmArgon2id {
		return nil, errors.New("unsupported kdf algorithm")
	}
	if len(params.Salt) == 0 {
		return nil, errors.New("salt is required")
	}
	return argon2.IDKey([]byte(password), params.Salt, params.Iterations, params.Memory, params.Parallelism, params.KeyLength), nil
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./internal/crypto
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/crypto go.mod go.sum
git commit -m "feat: add argon2id key derivation"
```

## Task 5.2: Add AEAD Seal/Open (XChaCha20-Poly1305)

**Files:**

- Create: `internal/crypto/aead.go`
- Test: `internal/crypto/aead_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/crypto/aead_test.go`:

```go
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
	ad := map[string]string{"entityType": "note", "entityId": "n1"}

	env, err := Seal("dek_1", key, []byte("top secret"), ad)
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
	env, err := Seal("dek_1", key, []byte("secret"), nil)
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
	env, err := Seal("dek_1", key, []byte("secret"), map[string]string{"a": "b"})
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	env.Ciphertext[0] ^= 0xFF
	if _, err := Open(key, env); err == nil {
		t.Fatal("Open must fail when ciphertext is tampered")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/crypto
```

Expected: FAIL because `Seal` and `Open` are not defined.

- [ ] **Step 3: Implement AEAD**

Create `internal/crypto/aead.go`:

```go
package crypto

import (
	"crypto/rand"
	"errors"
	"sort"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
)

// Seal encrypts plaintext with key using XChaCha20-Poly1305 and returns a
// versioned envelope. associatedData is authenticated but not encrypted.
func Seal(keyID string, key []byte, plaintext []byte, associatedData map[string]string) (EncryptedRecordEnvelope, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return EncryptedRecordEnvelope{}, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return EncryptedRecordEnvelope{}, err
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, encodeAD(associatedData))
	envelope := EncryptedRecordEnvelope{
		Version:        1,
		Algorithm:      AlgorithmXChaCha20Poly1305,
		KeyID:          keyID,
		Nonce:          nonce,
		AssociatedData: associatedData,
		Ciphertext:     ciphertext,
	}
	return envelope, envelope.Validate()
}

// Open verifies and decrypts an envelope produced by Seal.
func Open(key []byte, envelope EncryptedRecordEnvelope) ([]byte, error) {
	if err := envelope.Validate(); err != nil {
		return nil, err
	}
	if envelope.Algorithm != AlgorithmXChaCha20Poly1305 {
		return nil, errors.New("unsupported aead algorithm")
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	return aead.Open(nil, envelope.Nonce, envelope.Ciphertext, encodeAD(envelope.AssociatedData))
}

// encodeAD serializes associated data deterministically so authentication is
// stable regardless of map iteration order.
func encodeAD(ad map[string]string) []byte {
	if len(ad) == 0 {
		return nil
	}
	keys := make([]string, 0, len(ad))
	for k := range ad {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(ad[k])
		b.WriteByte(';')
	}
	return []byte(b.String())
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/crypto
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/crypto
git commit -m "feat: add xchacha20-poly1305 aead"
```

## Task 5.3: Add Key Hierarchy / Keyring (DEK wrap + recovery key)

**Files:**

- Create: `internal/crypto/keyring.go`
- Test: `internal/crypto/keyring_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/crypto/keyring_test.go`:

```go
package crypto

import (
	"bytes"
	"testing"
)

func TestKeyringUnlockWithPasswordAndRecovery(t *testing.T) {
	kr, recovery, err := NewKeyring("master-pw")
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	if recovery == "" {
		t.Fatal("recovery key must be returned")
	}

	dek1, err := kr.UnlockWithPassword("master-pw")
	if err != nil {
		t.Fatalf("UnlockWithPassword: %v", err)
	}
	dek2, err := kr.UnlockWithRecovery(recovery)
	if err != nil {
		t.Fatalf("UnlockWithRecovery: %v", err)
	}
	if !bytes.Equal(dek1, dek2) {
		t.Fatal("password and recovery must unwrap the same DEK")
	}
}

func TestKeyringRejectsWrongPassword(t *testing.T) {
	kr, _, err := NewKeyring("master-pw")
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	if _, err := kr.UnlockWithPassword("wrong"); err == nil {
		t.Fatal("wrong password must error")
	}
}

func TestKeyringRequiresMasterPassword(t *testing.T) {
	if _, _, err := NewKeyring(""); err == nil {
		t.Fatal("empty master password must error")
	}
}

func TestKeyringChangePasswordKeepsDEK(t *testing.T) {
	kr, _, err := NewKeyring("old-pw")
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	before, _ := kr.UnlockWithPassword("old-pw")

	if err := kr.ChangePassword("old-pw", "new-pw"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if _, err := kr.UnlockWithPassword("old-pw"); err == nil {
		t.Fatal("old password must stop working")
	}
	after, err := kr.UnlockWithPassword("new-pw")
	if err != nil {
		t.Fatalf("UnlockWithPassword(new): %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("DEK must stay the same after password change")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/crypto
```

Expected: FAIL because `Keyring` and `NewKeyring` are not defined.

- [ ] **Step 3: Implement the keyring**

Create `internal/crypto/keyring.go`:

```go
package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
)

const keyringDEKKeyID = "dek"

// Keyring is the persisted key material for a vault. It never contains the
// plaintext DEK: the random DEK is wrapped once by the master-password key and
// once by the recovery key.
type Keyring struct {
	Version         int                     `json:"version"`
	MasterKDF       KDFParams               `json:"masterKdf"`
	MasterWrapped   EncryptedRecordEnvelope `json:"masterWrapped"`
	RecoveryKDF     KDFParams               `json:"recoveryKdf"`
	RecoveryWrapped EncryptedRecordEnvelope `json:"recoveryWrapped"`
}

// NewKeyring generates a random DEK, wraps it with a key derived from
// masterPassword, and also wraps it with a freshly generated recovery key. The
// plaintext recovery key is returned once and is never stored.
func NewKeyring(masterPassword string) (*Keyring, string, error) {
	if masterPassword == "" {
		return nil, "", errors.New("master password is required")
	}

	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		return nil, "", err
	}

	masterKDF, err := DefaultKDFParams()
	if err != nil {
		return nil, "", err
	}
	masterKey, err := DeriveKey(masterPassword, masterKDF)
	if err != nil {
		return nil, "", err
	}
	masterWrapped, err := Seal(keyringDEKKeyID, masterKey, dek, nil)
	if err != nil {
		return nil, "", err
	}

	recoveryKey, err := newRecoveryKey()
	if err != nil {
		return nil, "", err
	}
	recoveryKDF, err := DefaultKDFParams()
	if err != nil {
		return nil, "", err
	}
	recoveryDerived, err := DeriveKey(recoveryKey, recoveryKDF)
	if err != nil {
		return nil, "", err
	}
	recoveryWrapped, err := Seal(keyringDEKKeyID, recoveryDerived, dek, nil)
	if err != nil {
		return nil, "", err
	}

	return &Keyring{
		Version:         1,
		MasterKDF:       masterKDF,
		MasterWrapped:   masterWrapped,
		RecoveryKDF:     recoveryKDF,
		RecoveryWrapped: recoveryWrapped,
	}, recoveryKey, nil
}

// UnlockWithPassword returns the plaintext DEK when masterPassword is correct.
func (k *Keyring) UnlockWithPassword(masterPassword string) ([]byte, error) {
	key, err := DeriveKey(masterPassword, k.MasterKDF)
	if err != nil {
		return nil, err
	}
	dek, err := Open(key, k.MasterWrapped)
	if err != nil {
		return nil, errors.New("invalid master password")
	}
	return dek, nil
}

// UnlockWithRecovery returns the plaintext DEK when recoveryKey is correct.
func (k *Keyring) UnlockWithRecovery(recoveryKey string) ([]byte, error) {
	key, err := DeriveKey(recoveryKey, k.RecoveryKDF)
	if err != nil {
		return nil, err
	}
	dek, err := Open(key, k.RecoveryWrapped)
	if err != nil {
		return nil, errors.New("invalid recovery key")
	}
	return dek, nil
}

// ChangePassword rewraps the existing DEK under a new master password. The DEK
// is unchanged, so existing records stay decryptable.
func (k *Keyring) ChangePassword(oldPassword string, newPassword string) error {
	if newPassword == "" {
		return errors.New("new master password is required")
	}
	dek, err := k.UnlockWithPassword(oldPassword)
	if err != nil {
		return err
	}
	kdf, err := DefaultKDFParams()
	if err != nil {
		return err
	}
	newKey, err := DeriveKey(newPassword, kdf)
	if err != nil {
		return err
	}
	wrapped, err := Seal(keyringDEKKeyID, newKey, dek, nil)
	if err != nil {
		return err
	}
	k.MasterKDF = kdf
	k.MasterWrapped = wrapped
	return nil
}

func newRecoveryKey() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/crypto
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/crypto
git commit -m "feat: add key hierarchy keyring with recovery key"
```

## Task 6: Add Vault Service (Master Password Mandatory)

> Design decision: the Master Password is **mandatory** and set once at first-run
> setup. A new install starts `not_configured`; the only paths to `unlocked` are
> `Setup` (first run) or `Unlock`/`UnlockWithRecovery` (later runs). The service
> holds the decrypted DEK in memory only while unlocked and zeroizes it on `Lock`.

**Files:**

- Create: `internal/vault/service.go`
- Test: `internal/vault/service_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/vault/service_test.go` (uses a test-only in-memory store; the
real stores land in Task 6.1):

```go
package vault

import (
	"errors"
	"testing"

	"changeme/internal/crypto"
)

type fakeStore struct {
	keyring *crypto.Keyring
}

func (f *fakeStore) Exists() (bool, error) { return f.keyring != nil, nil }

func (f *fakeStore) Load() (*crypto.Keyring, error) {
	if f.keyring == nil {
		return nil, errors.New("not configured")
	}
	return f.keyring, nil
}

func (f *fakeStore) Save(keyring *crypto.Keyring) error {
	f.keyring = keyring
	return nil
}

func TestVaultStartsNotConfigured(t *testing.T) {
	service := NewService(&fakeStore{})
	state, err := service.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if state != StateNotConfigured {
		t.Fatalf("state = %q, want %q", state, StateNotConfigured)
	}
}

func TestVaultSetupUnlocksAndReturnsRecovery(t *testing.T) {
	service := NewService(&fakeStore{})

	recovery, err := service.Setup("master-password")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if recovery == "" {
		t.Fatal("Setup must return a recovery key")
	}

	state, _ := service.Status()
	if state != StateUnlocked {
		t.Fatalf("state = %q, want unlocked", state)
	}
}

func TestVaultSetupRejectsSecondSetup(t *testing.T) {
	service := NewService(&fakeStore{})
	if _, err := service.Setup("master-password"); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if _, err := service.Setup("another-password"); err == nil {
		t.Fatal("second Setup must fail")
	}
}

func TestVaultSetupRejectsShortPassword(t *testing.T) {
	service := NewService(&fakeStore{})
	if _, err := service.Setup("short"); err == nil {
		t.Fatal("short master password must fail")
	}
}

func TestVaultLockThenUnlock(t *testing.T) {
	service := NewService(&fakeStore{})
	if _, err := service.Setup("master-password"); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	service.Lock()
	if !service.IsLocked() {
		t.Fatal("vault should be locked")
	}

	if err := service.Unlock("wrong-password"); err == nil {
		t.Fatal("wrong password must fail unlock")
	}
	if err := service.Unlock("master-password"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if service.IsLocked() {
		t.Fatal("vault should be unlocked")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/vault
```

Expected: FAIL because `NewService`, `State`, and store types are not defined.

- [ ] **Step 3: Implement the vault service**

Create `internal/vault/service.go`:

```go
package vault

import (
	"errors"
	"sync"

	"changeme/internal/crypto"
)

type State string

const (
	StateNotConfigured State = "not_configured"
	StateLocked        State = "locked"
	StateUnlocked      State = "unlocked"
)

// MinMasterPasswordLength is the minimum length enforced at setup.
// ponytail: length-only policy for now; add a strength meter later if needed.
const MinMasterPasswordLength = 8

// KeyringStore persists the vault keyring (wrapped DEK + KDF params).
type KeyringStore interface {
	Exists() (bool, error)
	Load() (*crypto.Keyring, error)
	Save(keyring *crypto.Keyring) error
}

// Service owns vault lock state and holds the decrypted DEK in memory only
// while unlocked. A Master Password is mandatory: a new install starts
// NotConfigured and must call Setup before anything can be unlocked.
type Service struct {
	mu    sync.RWMutex
	store KeyringStore
	dek   []byte
}

func NewService(store KeyringStore) *Service {
	return &Service{store: store}
}

// Status reports whether the vault still needs setup, is locked, or unlocked.
func (s *Service) Status() (State, error) {
	s.mu.RLock()
	unlocked := s.dek != nil
	s.mu.RUnlock()
	if unlocked {
		return StateUnlocked, nil
	}
	exists, err := s.store.Exists()
	if err != nil {
		return "", err
	}
	if !exists {
		return StateNotConfigured, nil
	}
	return StateLocked, nil
}

// Setup creates the vault on first run. It fails if a keyring already exists,
// enforcing that a Master Password is chosen exactly once. It returns the
// recovery key, which must be shown to the user and is never stored.
func (s *Service) Setup(masterPassword string) (string, error) {
	if len(masterPassword) < MinMasterPasswordLength {
		return "", errors.New("master password too short")
	}
	exists, err := s.store.Exists()
	if err != nil {
		return "", err
	}
	if exists {
		return "", errors.New("vault already configured")
	}

	keyring, recoveryKey, err := crypto.NewKeyring(masterPassword)
	if err != nil {
		return "", err
	}
	if err := s.store.Save(keyring); err != nil {
		return "", err
	}
	dek, err := keyring.UnlockWithPassword(masterPassword)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	s.dek = dek
	s.mu.Unlock()
	return recoveryKey, nil
}

// Unlock decrypts the DEK using the Master Password.
func (s *Service) Unlock(masterPassword string) error {
	keyring, err := s.store.Load()
	if err != nil {
		return err
	}
	dek, err := keyring.UnlockWithPassword(masterPassword)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.dek = dek
	s.mu.Unlock()
	return nil
}

// UnlockWithRecovery decrypts the DEK using the recovery key.
func (s *Service) UnlockWithRecovery(recoveryKey string) error {
	keyring, err := s.store.Load()
	if err != nil {
		return err
	}
	dek, err := keyring.UnlockWithRecovery(recoveryKey)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.dek = dek
	s.mu.Unlock()
	return nil
}

// Lock zeroizes the in-memory DEK.
func (s *Service) Lock() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.dek {
		s.dek[i] = 0
	}
	s.dek = nil
}

func (s *Service) IsLocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dek == nil
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/vault
```

Expected: PASS.

- [ ] **Step 5: Run all Go tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/vault
git commit -m "feat: add vault service with mandatory master password"
```

## Task 6.1: Add Keyring Persistence Stores

**Files:**

- Create: `internal/vault/keyringstore.go`
- Test: `internal/vault/keyringstore_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/vault/keyringstore_test.go`:

```go
package vault

import (
	"os"
	"path/filepath"
	"testing"

	"changeme/internal/crypto"
)

func TestFileKeyringStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keyring.json")
	store := NewFileKeyringStore(path)

	exists, err := store.Exists()
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Fatal("store should not exist yet")
	}

	keyring, _, err := crypto.NewKeyring("master-password")
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	if err := store.Save(keyring); err != nil {
		t.Fatalf("Save: %v", err)
	}

	exists, _ = store.Exists()
	if !exists {
		t.Fatal("store should exist after Save")
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := loaded.UnlockWithPassword("master-password"); err != nil {
		t.Fatalf("loaded keyring must unlock: %v", err)
	}
}

func TestFileKeyringStoreModeIsRestrictive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keyring.json")
	store := NewFileKeyringStore(path)
	keyring, _, _ := crypto.NewKeyring("master-password")
	if err := store.Save(keyring); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("keyring file mode = %v, want no group/other access", info.Mode().Perm())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/vault
```

Expected: FAIL because `NewFileKeyringStore` is not defined.

- [ ] **Step 3: Implement the stores**

Create `internal/vault/keyringstore.go`:

```go
package vault

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"changeme/internal/crypto"
)

// MemoryKeyringStore keeps the keyring in memory. Useful for tests and
// ephemeral runs; it does not survive a restart.
type MemoryKeyringStore struct {
	mu      sync.RWMutex
	keyring *crypto.Keyring
}

func NewMemoryKeyringStore() *MemoryKeyringStore {
	return &MemoryKeyringStore{}
}

func (s *MemoryKeyringStore) Exists() (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.keyring != nil, nil
}

func (s *MemoryKeyringStore) Load() (*crypto.Keyring, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.keyring == nil {
		return nil, errors.New("keyring not found")
	}
	return s.keyring, nil
}

func (s *MemoryKeyringStore) Save(keyring *crypto.Keyring) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keyring = keyring
	return nil
}

// FileKeyringStore persists the keyring as JSON. The file only holds wrapped
// key material and KDF params, never the plaintext DEK or the Master Password.
type FileKeyringStore struct {
	path string
}

func NewFileKeyringStore(path string) *FileKeyringStore {
	return &FileKeyringStore{path: path}
}

func (s *FileKeyringStore) Exists() (bool, error) {
	_, err := os.Stat(s.path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (s *FileKeyringStore) Load() (*crypto.Keyring, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	var keyring crypto.Keyring
	if err := json.Unmarshal(data, &keyring); err != nil {
		return nil, err
	}
	return &keyring, nil
}

func (s *FileKeyringStore) Save(keyring *crypto.Keyring) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(keyring, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/vault
```

Expected: PASS.

- [ ] **Step 5: Run all Go tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/vault
git commit -m "feat: add keyring persistence stores"
```

## Task 7: Add Command Registry

**Files:**

- Create: `internal/commands/registry.go`
- Test: `internal/commands/registry_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/commands/registry_test.go`:

```go
package commands

import (
	"context"
	"testing"
)

func TestRegistryExecutesRegisteredCommand(t *testing.T) {
	registry := NewRegistry()
	err := registry.Register(Command{
		ID:    "tools.hash.sha256",
		Title: "SHA-256",
		Handler: func(ctx context.Context, input any) (any, error) {
			return "ok", nil
		},
	})
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	result, err := registry.Execute(context.Background(), "tools.hash.sha256", nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result != "ok" {
		t.Fatalf("result = %v, want ok", result)
	}
}

func TestRegistryRejectsDuplicateCommand(t *testing.T) {
	registry := NewRegistry()
	command := Command{ID: "x", Title: "X", Handler: func(ctx context.Context, input any) (any, error) { return nil, nil }}

	if err := registry.Register(command); err != nil {
		t.Fatalf("first Register returned error: %v", err)
	}
	if err := registry.Register(command); err == nil {
		t.Fatal("second Register returned nil, want error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/commands
```

Expected: FAIL because registry types are not defined.

- [ ] **Step 3: Implement registry**

Create `internal/commands/registry.go`:

```go
package commands

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type Handler func(ctx context.Context, input any) (any, error)

type Command struct {
	ID          string
	Title       string
	Permissions []string
	Handler     Handler
}

type Registry struct {
	mu       sync.RWMutex
	commands map[string]Command
}

func NewRegistry() *Registry {
	return &Registry{commands: map[string]Command{}}
}

func (r *Registry) Register(command Command) error {
	if command.ID == "" {
		return errors.New("command id is required")
	}
	if command.Title == "" {
		return errors.New("command title is required")
	}
	if command.Handler == nil {
		return errors.New("command handler is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.commands[command.ID]; exists {
		return fmt.Errorf("command %q is already registered", command.ID)
	}
	r.commands[command.ID] = command
	return nil
}

func (r *Registry) Execute(ctx context.Context, id string, input any) (any, error) {
	r.mu.RLock()
	command, exists := r.commands[id]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("command %q is not registered", id)
	}
	return command.Handler(ctx, input)
}

func (r *Registry) List() []Command {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Command, 0, len(r.commands))
	for _, command := range r.commands {
		result = append(result, command)
	}
	return result
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/commands
```

Expected: PASS.

- [ ] **Step 5: Run all Go tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/commands
git commit -m "feat: add command registry"
```

## Task 8: Add Tools Hash Service

**Files:**

- Create: `internal/services/tools/hash.go`
- Test: `internal/services/tools/hash_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/services/tools/hash_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/services/tools
```

Expected: FAIL because `NewHashService` is not defined.

- [ ] **Step 3: Implement hash service**

Create `internal/services/tools/hash.go`:

```go
package tools

import (
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
)

type HashService struct{}

type HashRequest struct {
	Algorithm string
	Input     string
}

type HashResult struct {
	Algorithm string
	Output    string
}

func NewHashService() *HashService {
	return &HashService{}
}

func (s *HashService) Hash(request HashRequest) (HashResult, error) {
	switch request.Algorithm {
	case "sha256":
		sum := sha256.Sum256([]byte(request.Input))
		return HashResult{Algorithm: request.Algorithm, Output: hex.EncodeToString(sum[:])}, nil
	case "sha512":
		sum := sha512.Sum512([]byte(request.Input))
		return HashResult{Algorithm: request.Algorithm, Output: hex.EncodeToString(sum[:])}, nil
	case "md5":
		sum := md5.Sum([]byte(request.Input))
		return HashResult{Algorithm: request.Algorithm, Output: hex.EncodeToString(sum[:])}, nil
	default:
		return HashResult{}, fmt.Errorf("unsupported hash algorithm %q", request.Algorithm)
	}
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/services/tools
```

Expected: PASS.

- [ ] **Step 5: Run all Go tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/services/tools
git commit -m "feat: add hash tool service"
```

## Task 9: Add Permission Model and Policy

**Files:**

- Create: `internal/permissions/model.go`
- Create: `internal/permissions/policy.go`
- Test: `internal/permissions/policy_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/permissions/policy_test.go`:

```go
package permissions

import "testing"

func TestPolicyAllowsMatchingGrant(t *testing.T) {
	policy := NewPolicy([]Grant{
		{
			SubjectID:  "builtin.tools",
			Capability: "tools:hash",
			Scope:      "*",
		},
	})

	decision := policy.Check(Request{
		SubjectID:  "builtin.tools",
		Capability: "tools:hash",
		Scope:      "sha256",
	})

	if !decision.Allowed {
		t.Fatalf("Allowed = false, reason = %q", decision.Reason)
	}
}

func TestPolicyDeniesMissingGrant(t *testing.T) {
	policy := NewPolicy(nil)

	decision := policy.Check(Request{
		SubjectID:  "plugin.example",
		Capability: "vault:read",
		Scope:      "secret:connection/postgres/main",
	})

	if decision.Allowed {
		t.Fatal("Allowed = true, want false")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/permissions
```

Expected: FAIL because permission types are not defined.

- [ ] **Step 3: Implement permission model**

Create `internal/permissions/model.go`:

```go
package permissions

type Grant struct {
	SubjectID  string
	Capability string
	Scope      string
}

type Request struct {
	SubjectID  string
	Capability string
	Scope      string
}

type Decision struct {
	Allowed bool
	Reason  string
}
```

- [ ] **Step 4: Implement policy**

Create `internal/permissions/policy.go`:

```go
package permissions

type Policy struct {
	grants []Grant
}

func NewPolicy(grants []Grant) *Policy {
	return &Policy{grants: append([]Grant(nil), grants...)}
}

func (p *Policy) Check(request Request) Decision {
	if request.SubjectID == "" {
		return Decision{Allowed: false, Reason: "subject id is required"}
	}
	if request.Capability == "" {
		return Decision{Allowed: false, Reason: "capability is required"}
	}

	for _, grant := range p.grants {
		if grant.SubjectID != request.SubjectID {
			continue
		}
		if grant.Capability != request.Capability {
			continue
		}
		if grant.Scope == "*" || grant.Scope == request.Scope {
			return Decision{Allowed: true}
		}
	}

	return Decision{Allowed: false, Reason: "permission grant not found"}
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./internal/permissions
```

Expected: PASS.

- [ ] **Step 6: Run all Go tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/permissions
git commit -m "feat: add permission policy skeleton"
```

## Task 10: Add Capability Gateway Skeleton

**Files:**

- Create: `internal/capabilities/gateway.go`
- Test: `internal/capabilities/gateway_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/capabilities/gateway_test.go`:

```go
package capabilities

import (
	"context"
	"testing"

	"changeme/internal/permissions"
)

func TestGatewayDeniesWithoutPermission(t *testing.T) {
	policy := permissions.NewPolicy(nil)
	gateway := NewGateway(policy)

	_, err := gateway.Call(context.Background(), CallRequest{
		SubjectID:  "plugin.example",
		Capability: "vault:read",
		Scope:      "secret:main",
		Handler: func(ctx context.Context) (any, error) {
			return "secret", nil
		},
	})

	if err == nil {
		t.Fatal("Call returned nil error, want permission error")
	}
}

func TestGatewayCallsHandlerWhenAllowed(t *testing.T) {
	policy := permissions.NewPolicy([]permissions.Grant{
		{SubjectID: "builtin.tools", Capability: "tools:hash", Scope: "*"},
	})
	gateway := NewGateway(policy)

	result, err := gateway.Call(context.Background(), CallRequest{
		SubjectID:  "builtin.tools",
		Capability: "tools:hash",
		Scope:      "sha256",
		Handler: func(ctx context.Context) (any, error) {
			return "ok", nil
		},
	})

	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if result != "ok" {
		t.Fatalf("result = %v, want ok", result)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/capabilities
```

Expected: FAIL because `NewGateway` is not defined.

- [ ] **Step 3: Implement gateway**

Create `internal/capabilities/gateway.go`:

```go
package capabilities

import (
	"context"
	"fmt"

	"changeme/internal/permissions"
)

type Handler func(ctx context.Context) (any, error)

type CallRequest struct {
	SubjectID  string
	Capability string
	Scope      string
	Handler    Handler
}

type Gateway struct {
	policy *permissions.Policy
}

func NewGateway(policy *permissions.Policy) *Gateway {
	return &Gateway{policy: policy}
}

func (g *Gateway) Call(ctx context.Context, request CallRequest) (any, error) {
	if request.Handler == nil {
		return nil, fmt.Errorf("capability handler is required")
	}

	decision := g.policy.Check(permissions.Request{
		SubjectID:  request.SubjectID,
		Capability: request.Capability,
		Scope:      request.Scope,
	})
	if !decision.Allowed {
		return nil, fmt.Errorf("permission denied: %s", decision.Reason)
	}

	return request.Handler(ctx)
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/capabilities
```

Expected: PASS.

- [ ] **Step 5: Run all Go tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/capabilities
git commit -m "feat: add capability gateway skeleton"
```

## Task 11: Add Module Registry

**Files:**

- Create: `internal/modules/registry.go`
- Test: `internal/modules/registry_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/modules/registry_test.go`:

```go
package modules

import "testing"

func TestRegistryRegistersModule(t *testing.T) {
	registry := NewRegistry()
	err := registry.Register(Module{
		ID:    "tools",
		Title: "Tools",
		Commands: []string{
			"tools.hash",
		},
	})
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	modules := registry.List()
	if len(modules) != 1 {
		t.Fatalf("len(modules) = %d, want 1", len(modules))
	}
	if modules[0].ID != "tools" {
		t.Fatalf("module id = %q", modules[0].ID)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/modules
```

Expected: FAIL because `NewRegistry` is not defined.

- [ ] **Step 3: Implement module registry**

Create `internal/modules/registry.go`:

```go
package modules

import (
	"errors"
	"fmt"
	"sync"
)

type Module struct {
	ID       string
	Title    string
	Commands []string
	Views    []string
}

type Registry struct {
	mu      sync.RWMutex
	modules map[string]Module
}

func NewRegistry() *Registry {
	return &Registry{modules: map[string]Module{}}
}

func (r *Registry) Register(module Module) error {
	if module.ID == "" {
		return errors.New("module id is required")
	}
	if module.Title == "" {
		return errors.New("module title is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.modules[module.ID]; exists {
		return fmt.Errorf("module %q is already registered", module.ID)
	}
	r.modules[module.ID] = module
	return nil
}

func (r *Registry) List() []Module {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Module, 0, len(r.modules))
	for _, module := range r.modules {
		result = append(result, module)
	}
	return result
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/modules
```

Expected: PASS.

- [ ] **Step 5: Run all Go tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/modules
git commit -m "feat: add internal module registry"
```

## Task 12: Add Application Container

**Files:**

- Create: `internal/app/container.go`
- Create: `internal/app/app.go`
- Test: `internal/app/app_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/app/app_test.go`:

```go
package app

import "testing"

func TestNewContainerWiresCoreServices(t *testing.T) {
	container := NewContainer()

	if container.Commands == nil {
		t.Fatal("Commands is nil")
	}
	if container.Modules == nil {
		t.Fatal("Modules is nil")
	}
	if container.Vault == nil {
		t.Fatal("Vault is nil")
	}
	if container.Capabilities == nil {
		t.Fatal("Capabilities is nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/app
```

Expected: FAIL because `NewContainer` is not defined.

- [ ] **Step 3: Implement container**

Create `internal/app/container.go`:

```go
package app

import (
	"changeme/internal/capabilities"
	"changeme/internal/commands"
	"changeme/internal/modules"
	"changeme/internal/permissions"
	"changeme/internal/services/tools"
	"changeme/internal/vault"
)

type Container struct {
	Commands     *commands.Registry
	Modules      *modules.Registry
	Permissions  *permissions.Policy
	Capabilities *capabilities.Gateway
	Vault         *vault.Service
	HashTools     *tools.HashService
}

// NewContainer wires core services with an in-memory keyring store, suitable
// for tests and ephemeral runs. The desktop/server entrypoint (Task 14) should
// call NewContainerWithKeyringStore with a FileKeyringStore so the Master
// Password set at setup persists across restarts.
func NewContainer() *Container {
	return NewContainerWithKeyringStore(vault.NewMemoryKeyringStore())
}

func NewContainerWithKeyringStore(keyringStore vault.KeyringStore) *Container {
	permissionPolicy := permissions.NewPolicy([]permissions.Grant{
		{SubjectID: "builtin.tools", Capability: "tools:hash", Scope: "*"},
	})

	return &Container{
		Commands:     commands.NewRegistry(),
		Modules:      modules.NewRegistry(),
		Permissions:  permissionPolicy,
		Capabilities: capabilities.NewGateway(permissionPolicy),
		Vault:        vault.NewService(keyringStore),
		HashTools:    tools.NewHashService(),
	}
}
```

- [ ] **Step 4: Implement app registration**

Create `internal/app/app.go`:

```go
package app

import (
	"context"

	"changeme/internal/commands"
	"changeme/internal/modules"
	"changeme/internal/services/tools"
)

func RegisterBuiltins(container *Container) error {
	if err := container.Modules.Register(modules.Module{
		ID:       "tools",
		Title:    "Tools",
		Commands: []string{"tools.hash"},
		Views:    []string{"tools.hash"},
	}); err != nil {
		return err
	}

	return container.Commands.Register(commands.Command{
		ID:    "tools.hash",
		Title: "Hash text",
		Handler: func(ctx context.Context, input any) (any, error) {
			request, ok := input.(tools.HashRequest)
			if !ok {
				return nil, nil
			}
			return container.HashTools.Hash(request)
		},
	})
}
```

- [ ] **Step 5: Add registration test**

Append to `internal/app/app_test.go`:

```go
func TestRegisterBuiltinsRegistersToolsModule(t *testing.T) {
	container := NewContainer()

	if err := RegisterBuiltins(container); err != nil {
		t.Fatalf("RegisterBuiltins returned error: %v", err)
	}

	if len(container.Modules.List()) != 1 {
		t.Fatalf("module count = %d, want 1", len(container.Modules.List()))
	}
}
```

- [ ] **Step 6: Run tests**

Run:

```bash
go test ./internal/app
```

Expected: PASS.

- [ ] **Step 7: Run all Go tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/app
git commit -m "feat: add core application container"
```

## Task 13: Add Wails Bridge Service

**Files:**

- Create: `internal/bridge/dto.go`
- Create: `internal/bridge/app_service.go`
- Test: `internal/bridge/app_service_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/bridge/app_service_test.go`:

```go
package bridge

import (
	"context"
	"testing"

	"changeme/internal/app"
)

func TestAppServiceListsModules(t *testing.T) {
	container := app.NewContainer()
	if err := app.RegisterBuiltins(container); err != nil {
		t.Fatalf("RegisterBuiltins returned error: %v", err)
	}

	service := NewAppService(container)
	modules := service.ListModules()

	if len(modules) != 1 {
		t.Fatalf("len(modules) = %d, want 1", len(modules))
	}
	if modules[0].ID != "tools" {
		t.Fatalf("module id = %q", modules[0].ID)
	}
}

func TestAppServiceExecutesHashCommand(t *testing.T) {
	container := app.NewContainer()
	if err := app.RegisterBuiltins(container); err != nil {
		t.Fatalf("RegisterBuiltins returned error: %v", err)
	}

	service := NewAppService(container)
	result, err := service.ExecuteCommand(context.Background(), ExecuteCommandRequest{
		ID: "tools.hash",
		Payload: map[string]any{
			"algorithm": "sha256",
			"input":     "hello",
		},
	})
	if err != nil {
		t.Fatalf("ExecuteCommand returned error: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/bridge
```

Expected: FAIL because `NewAppService` is not defined.

- [ ] **Step 3: Implement DTOs**

Create `internal/bridge/dto.go`:

```go
package bridge

type ModuleDTO struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Commands []string `json:"commands"`
	Views    []string `json:"views"`
}

type ExecuteCommandRequest struct {
	ID      string         `json:"id"`
	Payload map[string]any `json:"payload"`
}
```

- [ ] **Step 4: Implement bridge service**

Create `internal/bridge/app_service.go`:

```go
package bridge

import (
	"context"
	"fmt"

	"changeme/internal/app"
	"changeme/internal/services/tools"
)

type AppService struct {
	container *app.Container
}

func NewAppService(container *app.Container) *AppService {
	return &AppService{container: container}
}

func (s *AppService) ListModules() []ModuleDTO {
	modules := s.container.Modules.List()
	result := make([]ModuleDTO, 0, len(modules))
	for _, module := range modules {
		result = append(result, ModuleDTO{
			ID:       module.ID,
			Title:    module.Title,
			Commands: module.Commands,
			Views:    module.Views,
		})
	}
	return result
}

func (s *AppService) ExecuteCommand(ctx context.Context, request ExecuteCommandRequest) (any, error) {
	var payload any = request.Payload
	if request.ID == "tools.hash" {
		payload = tools.HashRequest{
			Algorithm: fmt.Sprint(request.Payload["algorithm"]),
			Input:     fmt.Sprint(request.Payload["input"]),
		}
	}
	return s.container.Commands.Execute(ctx, request.ID, payload)
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./internal/bridge
```

Expected: PASS.

- [ ] **Step 6: Run all Go tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/bridge
git commit -m "feat: add app bridge service"
```

## Task 13.1: Add Vault Bridge Methods

**Files:**

- Create: `internal/bridge/vault.go`
- Test: `internal/bridge/vault_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/bridge/vault_test.go`:

```go
package bridge

import (
	"testing"

	"changeme/internal/app"
)

func TestVaultBridgeSetupAndStatus(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)

	status, err := service.VaultStatus()
	if err != nil {
		t.Fatalf("VaultStatus: %v", err)
	}
	if status.State != "not_configured" {
		t.Fatalf("state = %q, want not_configured", status.State)
	}

	recovery, err := service.SetupVault("master-password")
	if err != nil {
		t.Fatalf("SetupVault: %v", err)
	}
	if recovery == "" {
		t.Fatal("SetupVault must return a recovery key")
	}

	status, _ = service.VaultStatus()
	if status.State != "unlocked" {
		t.Fatalf("state = %q, want unlocked", status.State)
	}
}

func TestVaultBridgeLockThenUnlock(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)

	if _, err := service.SetupVault("master-password"); err != nil {
		t.Fatalf("SetupVault: %v", err)
	}
	if err := service.LockVault(); err != nil {
		t.Fatalf("LockVault: %v", err)
	}
	if err := service.UnlockVault("master-password"); err != nil {
		t.Fatalf("UnlockVault: %v", err)
	}
	status, _ := service.VaultStatus()
	if status.State != "unlocked" {
		t.Fatalf("state = %q, want unlocked", status.State)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/bridge
```

Expected: FAIL because the vault bridge methods are not defined.

- [ ] **Step 3: Implement vault bridge methods**

Create `internal/bridge/vault.go` (methods live on the existing `AppService`):

```go
package bridge

type VaultStatusDTO struct {
	State string `json:"state"`
}

func (s *AppService) VaultStatus() (VaultStatusDTO, error) {
	state, err := s.container.Vault.Status()
	if err != nil {
		return VaultStatusDTO{}, err
	}
	return VaultStatusDTO{State: string(state)}, nil
}

// SetupVault creates the vault with a mandatory Master Password (first run
// only) and returns the recovery key to show the user exactly once.
func (s *AppService) SetupVault(masterPassword string) (string, error) {
	return s.container.Vault.Setup(masterPassword)
}

func (s *AppService) UnlockVault(masterPassword string) error {
	return s.container.Vault.Unlock(masterPassword)
}

func (s *AppService) UnlockVaultWithRecovery(recoveryKey string) error {
	return s.container.Vault.UnlockWithRecovery(recoveryKey)
}

func (s *AppService) LockVault() error {
	s.container.Vault.Lock()
	return nil
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/bridge
```

Expected: PASS.

- [ ] **Step 5: Run all Go tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/bridge
git commit -m "feat: add vault bridge methods"
```

## Task 14: Wire Core Container into Current Wails Entry

**Files:**

- Modify: `main.go`
- Delete: `greetservice.go`
- Generated after Wails binding generation:
  - `frontend/bindings/changeme/appservice.ts`
  - `frontend/bindings/changeme/index.ts`

- [ ] **Step 1: Inspect current Wails service registration**

Run:

```bash
sed -n '1,220p' main.go
```

Expected: output shows current Wails app setup and service registration.

- [ ] **Step 2: Replace starter service registration in `main.go`**

Replace `main.go` with this Wails v3 entrypoint, preserving the current starter API shape:

```go
package main

import (
	"embed"
	"log"
	"os"
	"path/filepath"
	"time"

	"changeme/internal/app"
	"changeme/internal/bridge"
	"changeme/internal/vault"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func init() {
	application.RegisterEvent[string]("time")
}

func main() {
	// Persist the vault keyring so the Master Password set at first-run setup
	// survives restarts. Only wrapped key material is written to disk.
	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	keyringPath := filepath.Join(configDir, "DevKit", "keyring.json")
	container := app.NewContainerWithKeyringStore(vault.NewFileKeyringStore(keyringPath))
	if err := app.RegisterBuiltins(container); err != nil {
		log.Fatal(err)
	}

	appService := bridge.NewAppService(container)

	wailsApp := application.New(application.Options{
		Name:        "DevKit",
		Description: "Developer toolkit",
		Services: []application.Service{
			application.NewService(appService),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "DevKit",
		Width:  1200,
		Height: 760,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(16, 18, 20),
		URL:              "/",
	})

	go func() {
		for {
			now := time.Now().Format(time.RFC1123)
			wailsApp.Event.Emit("time", now)
			time.Sleep(time.Second)
		}
	}()

	if err := wailsApp.Run(); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 3: Delete starter greet service**

Delete `greetservice.go` because no frontend code should call the starter `GreetService` after the workbench migration.

- [ ] **Step 4: Regenerate Wails bindings**

Run:

```bash
wails3 generate bindings
```

Expected:

- `frontend/bindings/changeme/appservice.ts` exists.
- `frontend/bindings/changeme/index.ts` exports `AppService`.

If the command is not available, run:

```bash
wails3 dev
```

Expected:

- Wails dev command regenerates bindings, or the command output shows the exact supported binding generation command.

- [ ] **Step 5: Run Go tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 6: Run frontend build**

Run:

```bash
cd frontend && npm run build
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add main.go greetservice.go frontend/bindings
git commit -m "feat: wire core app bridge into wails"
```

## Task 15: Add Frontend Bridge Adapters

**Files:**

- Create: `frontend/src/bridge/errors.ts`
- Create: `frontend/src/bridge/client.ts`
- Create: `frontend/src/bridge/commands.ts`

- [ ] **Step 1: Create typed error shape**

Create `frontend/src/bridge/errors.ts`:

```ts
export type AppError = {
  code: string;
  message: string;
  recoverable: boolean;
  details?: Record<string, unknown>;
};

export function toAppError(error: unknown): AppError {
  if (
    typeof error === "object" &&
    error !== null &&
    "code" in error &&
    "message" in error
  ) {
    const value = error as Partial<AppError>;
    return {
      code: String(value.code),
      message: String(value.message),
      recoverable: Boolean(value.recoverable),
      details: value.details,
    };
  }

  if (error instanceof Error) {
    return {
      code: "unknown",
      message: error.message,
      recoverable: false,
    };
  }

  return {
    code: "unknown",
    message: String(error),
    recoverable: false,
  };
}
```

- [ ] **Step 2: Create bridge client wrapper**

Create `frontend/src/bridge/client.ts`:

```ts
import { toAppError } from "./errors";

export async function callBridge<T>(operation: () => Promise<T>): Promise<T> {
  try {
    return await operation();
  } catch (error) {
    throw toAppError(error);
  }
}
```

- [ ] **Step 3: Create command adapter**

Create `frontend/src/bridge/commands.ts`:

```ts
import { callBridge } from "./client";
import { AppService } from "../../bindings/changeme";

export type ExecuteCommandRequest = {
  id: string;
  payload: Record<string, unknown>;
};

export type ModuleDTO = {
  id: string;
  title: string;
  commands: string[];
  views: string[];
};

export function listModules(): Promise<ModuleDTO[]> {
  return callBridge(() => AppService.ListModules());
}

export function executeCommand<T>(request: ExecuteCommandRequest): Promise<T> {
  return callBridge(() => AppService.ExecuteCommand(request) as Promise<T>);
}
```

- [ ] **Step 4: Verify generated binding shape**

Run:

```bash
sed -n '1,220p' frontend/bindings/changeme/appservice.ts
```

Expected:

- `ListModules` is exported.
- `ExecuteCommand` is exported.
- `ExecuteCommand` accepts one request object matching `ExecuteCommandRequest`.

- [ ] **Step 5: Run frontend typecheck/build**

Run:

```bash
cd frontend && npm run build
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/bridge
git commit -m "feat: add frontend bridge adapters"
```

## Task 16: Add Frontend Module Registry

**Files:**

- Create: `frontend/src/modules/registry.ts`
- Create: `frontend/src/modules/tools/manifest.ts`

- [ ] **Step 1: Create module registry types**

Create `frontend/src/modules/registry.ts`:

```ts
import type { ComponentType } from "react";

export type ModuleView = {
  id: string;
  title: string;
  component: ComponentType;
};

export type ModuleCommand = {
  id: string;
  title: string;
};

export type ModuleDefinition = {
  id: string;
  title: string;
  commands: ModuleCommand[];
  views: ModuleView[];
};

const modules = new Map<string, ModuleDefinition>();

export function registerModule(module: ModuleDefinition): void {
  if (modules.has(module.id)) {
    throw new Error(`Module ${module.id} is already registered`);
  }
  modules.set(module.id, module);
}

export function listRegisteredModules(): ModuleDefinition[] {
  return Array.from(modules.values());
}

export function getModule(id: string): ModuleDefinition | undefined {
  return modules.get(id);
}
```

- [ ] **Step 2: Create tools manifest placeholder**

Create `frontend/src/modules/tools/manifest.ts`:

```ts
import type { ModuleDefinition } from "../registry";
import { ToolsView } from "./ToolsView";

export const toolsModule: ModuleDefinition = {
  id: "tools",
  title: "Tools",
  commands: [
    {
      id: "tools.hash",
      title: "Hash text",
    },
  ],
  views: [
    {
      id: "tools.hash",
      title: "Hash",
      component: ToolsView,
    },
  ],
};
```

- [ ] **Step 3: Create temporary ToolsView to satisfy imports**

Create `frontend/src/modules/tools/ToolsView.tsx`:

```tsx
export function ToolsView() {
  return (
    <section>
      <h2>Hash</h2>
    </section>
  );
}
```

- [ ] **Step 4: Run frontend build**

Run:

```bash
cd frontend && npm run build
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules
git commit -m "feat: add frontend module registry"
```

## Task 17: Add Minimal Workbench Shell

**Files:**

- Create: `frontend/src/workbench/Workbench.tsx`
- Create: `frontend/src/workbench/ActivityBar.tsx`
- Create: `frontend/src/workbench/CommandPalette.tsx`
- Modify: `frontend/src/App.tsx`

- [ ] **Step 1: Create ActivityBar**

Create `frontend/src/workbench/ActivityBar.tsx`:

```tsx
type ActivityBarProps = {
  activeModuleId: string;
  modules: Array<{ id: string; title: string }>;
  onSelectModule: (moduleId: string) => void;
};

export function ActivityBar({
  activeModuleId,
  modules,
  onSelectModule,
}: ActivityBarProps) {
  return (
    <nav className="activity-bar" aria-label="Modules">
      {modules.map((module) => (
        <button
          key={module.id}
          type="button"
          className={
            module.id === activeModuleId
              ? "activity-item is-active"
              : "activity-item"
          }
          onClick={() => onSelectModule(module.id)}
        >
          {module.title}
        </button>
      ))}
    </nav>
  );
}
```

- [ ] **Step 2: Create CommandPalette skeleton**

Create `frontend/src/workbench/CommandPalette.tsx`:

```tsx
type CommandPaletteProps = {
  open: boolean;
  commands: Array<{ id: string; title: string }>;
  onClose: () => void;
};

export function CommandPalette({
  open,
  commands,
  onClose,
}: CommandPaletteProps) {
  if (!open) {
    return null;
  }

  return (
    <div className="command-palette" role="dialog" aria-modal="true">
      <div className="command-palette-panel">
        <div className="command-palette-header">
          <span>Commands</span>
          <button type="button" onClick={onClose}>
            Close
          </button>
        </div>
        <ul>
          {commands.map((command) => (
            <li key={command.id}>{command.title}</li>
          ))}
        </ul>
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Create Workbench**

Create `frontend/src/workbench/Workbench.tsx`:

```tsx
import { useMemo, useState } from "react";
import { ActivityBar } from "./ActivityBar";
import { CommandPalette } from "./CommandPalette";
import { listRegisteredModules, registerModule } from "../modules/registry";
import { toolsModule } from "../modules/tools/manifest";

let registered = false;

function ensureModulesRegistered() {
  if (registered) {
    return;
  }
  registerModule(toolsModule);
  registered = true;
}

export function Workbench() {
  ensureModulesRegistered();

  const modules = useMemo(() => listRegisteredModules(), []);
  const [activeModuleId, setActiveModuleId] = useState(modules[0]?.id ?? "");
  const [paletteOpen, setPaletteOpen] = useState(false);

  const activeModule =
    modules.find((module) => module.id === activeModuleId) ?? modules[0];
  const activeView = activeModule?.views[0];
  const ActiveView = activeView?.component;
  const commands = modules.flatMap((module) => module.commands);

  return (
    <main className="workbench">
      <ActivityBar
        activeModuleId={activeModule?.id ?? ""}
        modules={modules}
        onSelectModule={setActiveModuleId}
      />
      <section className="workbench-main">
        <header className="workbench-header">
          <strong>{activeModule?.title ?? "DevKit"}</strong>
          <button type="button" onClick={() => setPaletteOpen(true)}>
            Commands
          </button>
        </header>
        <section className="workbench-content">
          {ActiveView ? <ActiveView /> : null}
        </section>
      </section>
      <CommandPalette
        open={paletteOpen}
        commands={commands}
        onClose={() => setPaletteOpen(false)}
      />
    </main>
  );
}
```

- [ ] **Step 4: Replace starter App**

Replace `frontend/src/App.tsx` with:

```tsx
import { Workbench } from "./workbench/Workbench";

function App() {
  return <Workbench />;
}

export default App;
```

- [ ] **Step 5: Add minimal CSS**

Append to the active stylesheet used by `frontend/src/main.tsx`:

```css
.workbench {
  display: flex;
  min-height: 100vh;
  background: #101214;
  color: #f4f4f5;
}

.activity-bar {
  width: 72px;
  border-right: 1px solid #2a2f35;
  padding: 8px;
}

.activity-item {
  width: 100%;
  min-height: 40px;
  margin-bottom: 6px;
}

.activity-item.is-active {
  font-weight: 700;
}

.workbench-main {
  display: flex;
  min-width: 0;
  flex: 1;
  flex-direction: column;
}

.workbench-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 48px;
  border-bottom: 1px solid #2a2f35;
  padding: 0 16px;
}

.workbench-content {
  flex: 1;
  min-height: 0;
  padding: 16px;
}

.command-palette {
  position: fixed;
  inset: 0;
  display: grid;
  place-items: start center;
  padding-top: 80px;
  background: rgba(0, 0, 0, 0.5);
}

.command-palette-panel {
  width: min(640px, calc(100vw - 32px));
  border: 1px solid #3f4650;
  border-radius: 8px;
  background: #181b20;
}

.command-palette-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid #2a2f35;
  padding: 12px;
}
```

- [ ] **Step 6: Run frontend build**

Run:

```bash
cd frontend && npm run build
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add frontend/src
git commit -m "feat: add minimal workbench shell"
```

## Task 17.1: Add Frontend Vault Gate (Mandatory Setup + Unlock)

> This enforces the Master Password in the UI: the Workbench does not render
> until the vault is set up (first run) and unlocked.

**Files:**

- Create: `frontend/src/bridge/vault.ts`
- Create: `frontend/src/workbench/VaultGate.tsx`
- Modify: `frontend/src/App.tsx`

- [ ] **Step 1: Create the vault bridge adapter**

UI must not import generated bindings directly (§8.3 and §6.10 invariant 10);
route vault calls through a typed adapter. Create `frontend/src/bridge/vault.ts`:

```ts
import { callBridge } from "./client";
import { AppService } from "../../bindings/changeme";

export type VaultState = "not_configured" | "locked" | "unlocked";

export function vaultStatus(): Promise<VaultState> {
  return callBridge(async () => (await AppService.VaultStatus()).state as VaultState);
}

export function setupVault(masterPassword: string): Promise<string> {
  return callBridge(() => AppService.SetupVault(masterPassword));
}

export function unlockVault(masterPassword: string): Promise<void> {
  return callBridge(() => AppService.UnlockVault(masterPassword) as Promise<void>);
}
```

- [ ] **Step 2: Create the gate component**

Create `frontend/src/workbench/VaultGate.tsx`:

```tsx
import { useEffect, useState, type ReactNode } from "react";
import { setupVault, unlockVault, vaultStatus, type VaultState } from "../bridge/vault";

type Status = "loading" | VaultState;

export function VaultGate({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<Status>("loading");
  const [password, setPassword] = useState("");
  const [recoveryKey, setRecoveryKey] = useState("");
  const [error, setError] = useState("");

  async function refresh() {
    setStatus(await vaultStatus());
  }

  useEffect(() => {
    refresh().catch((e) => setError(String(e)));
  }, []);

  async function onSetup() {
    setError("");
    try {
      const recovery = await setupVault(password);
      setRecoveryKey(recovery);
      setPassword("");
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  async function onUnlock() {
    setError("");
    try {
      await unlockVault(password);
      setPassword("");
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  if (status === "loading") {
    return <div className="vault-gate">Loading…</div>;
  }

  if (status === "unlocked") {
    return (
      <>
        {recoveryKey ? (
          <div className="vault-recovery" role="alert">
            Save your recovery key now — it is shown only once and cannot be
            recovered later: <code>{recoveryKey}</code>
            <button type="button" onClick={() => setRecoveryKey("")}>
              I saved it
            </button>
          </div>
        ) : null}
        {children}
      </>
    );
  }

  const isSetup = status === "not_configured";
  return (
    <div className="vault-gate">
      <h1>{isSetup ? "Create Master Password" : "Unlock DevKit"}</h1>
      <p>
        {isSetup
          ? "A Master Password is required to protect your vault. Without your recovery key it cannot be recovered."
          : "Enter your Master Password to unlock the vault."}
      </p>
      <input
        type="password"
        value={password}
        placeholder="Master Password"
        onChange={(e) => setPassword(e.target.value)}
      />
      <button type="button" onClick={isSetup ? onSetup : onUnlock}>
        {isSetup ? "Create" : "Unlock"}
      </button>
      {error ? <p role="alert">{error}</p> : null}
    </div>
  );
}
```

- [ ] **Step 3: Wrap the Workbench with the gate**

Replace `frontend/src/App.tsx`:

```tsx
import { VaultGate } from "./workbench/VaultGate";
import { Workbench } from "./workbench/Workbench";

function App() {
  return (
    <VaultGate>
      <Workbench />
    </VaultGate>
  );
}

export default App;
```

- [ ] **Step 4: Run frontend build**

Run:

```bash
cd frontend && npm run build
```

Expected: PASS (requires the vault bindings from Task 13.1 + Task 14 regeneration).

- [ ] **Step 5: Commit**

```bash
git add frontend/src
git commit -m "feat: gate app behind mandatory vault setup/unlock"
```

## Task 18: Connect ToolsView to Hash Command

**Files:**

- Modify: `frontend/src/modules/tools/ToolsView.tsx`

- [ ] **Step 1: Replace ToolsView with working hash UI**

Replace `frontend/src/modules/tools/ToolsView.tsx`:

```tsx
import { useState } from "react";
import { executeCommand } from "../../bridge/commands";

type HashResult = {
  algorithm: string;
  output: string;
};

export function ToolsView() {
  const [input, setInput] = useState("");
  const [algorithm, setAlgorithm] = useState("sha256");
  const [output, setOutput] = useState("");
  const [error, setError] = useState("");

  async function runHash() {
    setError("");
    try {
      const result = await executeCommand<HashResult>({
        id: "tools.hash",
        payload: {
          algorithm,
          input,
        },
      });
      setOutput(result.output);
    } catch (err) {
      setOutput("");
      setError(err instanceof Error ? err.message : String(err));
    }
  }

  return (
    <section className="tools-view">
      <h2>Hash</h2>
      <label>
        Algorithm
        <select
          value={algorithm}
          onChange={(event) => setAlgorithm(event.target.value)}
        >
          <option value="sha256">SHA-256</option>
          <option value="sha512">SHA-512</option>
          <option value="md5">MD5</option>
        </select>
      </label>
      <label>
        Input
        <textarea
          value={input}
          onChange={(event) => setInput(event.target.value)}
        />
      </label>
      <button type="button" onClick={runHash}>
        Hash
      </button>
      {output ? <pre>{output}</pre> : null}
      {error ? <p role="alert">{error}</p> : null}
    </section>
  );
}
```

- [ ] **Step 2: Run frontend build**

Run:

```bash
cd frontend && npm run build
```

Expected: PASS.

- [ ] **Step 3: Run Go tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 4: Run app manually**

Run:

```bash
wails3 dev
```

Expected:

- App opens on the "Create Master Password" screen (first run); after creating one the recovery key is shown once.
- After setup/unlock, the Workbench shell appears.
- Tools module appears.
- Hashing `hello` with SHA-256 returns `2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824`.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules/tools
git commit -m "feat: connect hash tool to command bridge"
```

## Task 19: Add Core Documentation Index

**Files:**

- Create: `docs/architecture/core-foundation.md`

- [ ] **Step 1: Create core foundation doc**

Create `docs/architecture/core-foundation.md`:

```md
# Core Foundation

This document tracks the implemented DevKit core foundation.

## Implemented Packages

- `internal/app`: application composition root and core service wiring.
- `internal/apperrors`: structured application errors.
- `internal/audit`: redaction helpers for logs and audit events.
- `internal/storage`: storage interfaces and migration runner skeleton.
- `internal/crypto`: encrypted record envelope, Argon2id KDF, XChaCha20-Poly1305 AEAD, and the DEK/KEK keyring with recovery key.
- `internal/vault`: vault service with a mandatory Master Password (setup/unlock/lock, recovery-key unlock) and keyring persistence.
- `internal/commands`: command registry.
- `internal/modules`: built-in module registry.
- `internal/permissions`: permission grants and policy checks.
- `internal/capabilities`: permission-aware capability gateway.
- `internal/services/tools`: hash/encoding services.
- `internal/bridge`: Wails-facing app service and DTOs.

## Current Rules

- Frontend code calls `frontend/src/bridge`, not generated Wails bindings directly.
- A Master Password is mandatory and set once at first-run setup; the vault holds the decrypted DEK in memory only while unlocked.
- Sensitive plugin-style calls must pass through the Capability Gateway.
- Encrypted record formats are versioned from the first implementation.
- Long-running future operations must accept cancellation.

## Next Modules

1. Notes/snippets with encrypted storage.
2. Vault item persistence.
3. Local search after vault unlock.
4. SSH profile storage.
5. PostgreSQL connection profile and query runner.
```

- [ ] **Step 2: Verify docs exist**

Run:

```bash
find docs/architecture docs/superpowers/plans -maxdepth 1 -type f | sort
```

Expected: output includes:

- `docs/architecture/core-foundation.md`
- `docs/architecture/devkit-architecture.md`
- `docs/architecture/project-structure-and-code-rules.md`
- `docs/superpowers/plans/2026-06-28-core-foundation.md`

- [ ] **Step 3: Commit**

```bash
git add docs/architecture/core-foundation.md
git commit -m "docs: document core foundation packages"
```

## Task 20: Final Verification

**Files:**

- Verify all files changed by Tasks 1-19.

- [ ] **Step 1: Run Go tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 2: Run frontend build**

Run:

```bash
cd frontend && npm run build
```

Expected: PASS.

- [ ] **Step 3: Run Wails development app**

Run:

```bash
wails3 dev
```

Expected:

- App starts on the vault gate: "Create Master Password" on first run, or "Unlock DevKit" on later runs.
- After setup/unlock, the Workbench shell renders.
- Tools module renders.
- Hash tool works.
- No frontend console errors.
- No Go runtime errors.

- [ ] **Step 4: Review package dependency direction**

Run:

```bash
go list -deps ./internal/...
```

Expected:

- Command exits `0`.
- No import cycle errors.

- [ ] **Step 5: Search for forbidden patterns**

Run:

```bash
rg -n "password|private_key|token|secret" internal frontend/src
```

Expected:

- Only intentional model names, redaction rules, tests, or UI labels appear.
- No hardcoded secret values appear.

- [ ] **Step 6: Confirm current project state**

Run:

```bash
git status --short
```

Expected:

- Empty output if using git and all work has been committed.
- If not using git, note that this workspace is not yet initialized as a repository.

## Self-Review

Spec coverage:

- Project structure: covered by Tasks 1 and 12.
- Structured errors: covered by Task 2.
- Logging/redaction: covered by Task 3.
- Local storage and migrations: covered by Task 4.
- Crypto envelope: covered by Task 5.
- Password key derivation, AEAD, key hierarchy/keyring: covered by Tasks 5.1–5.3.
- Master-Password vault (mandatory setup, unlock/lock, recovery) + persistence: covered by Tasks 6 and 6.1.
- Vault bridge + frontend setup/unlock gate: covered by Tasks 13.1 and 17.1.
- Command registry: covered by Task 7.
- First core module: covered by Task 8 and Task 18.
- Permission model: covered by Task 9.
- Capability Gateway: covered by Task 10.
- Module registry: covered by Task 11 and Task 16.
- Wails bridge: covered by Tasks 13-15.
- Workbench shell: covered by Task 17.
- Verification: covered by Task 20.

Extensibility invariants (§6.10) upheld by these tasks:

- Single choke point + caller identity: capability calls go through the Gateway keyed on subject + capability + scope (Tasks 9, 10).
- Manifest-driven contributions + permissions from day one: modules register metadata and grants (Tasks 11, 12).
- UI through the bridge, not raw runtime (Task 15).

Known gaps intentionally deferred:

- Real encrypted SQLite/SQLCipher integration (records use the app-layer crypto envelope; only the keyring is persisted).
- Persistent encrypted vault item repository (item CRUD comes later).
- External/untrusted plugin runtime, marketplace, signing, sandboxing (far, non-committed vision per §6.0).
- SaaS sync.
- SSH and DB manager modules.
