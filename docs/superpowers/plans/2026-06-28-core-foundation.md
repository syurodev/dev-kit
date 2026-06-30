# Core Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the DevKit core foundation so future modules can use a stable Wails bridge, command registry, local storage, crypto envelope, vault skeleton, permission model, and Capability Gateway.

**Architecture:** Keep Wails as a thin transport layer. Go core packages own application behavior, storage, crypto, vault, commands, permissions, and capability checks. Frontend modules call typed bridge adapters instead of generated Wails bindings directly.

**Tech Stack:** Go 1.25, Wails v3, React 19, TypeScript, Vite, built-in Go tests, TypeScript compiler.

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
- Vault lock/unlock skeleton.
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
- SQLCipher production integration.
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
- `internal/vault/service.go`: vault lock/unlock skeleton.
- `internal/vault/service_test.go`: vault state tests.
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

## Task 6: Add Vault Skeleton

**Files:**

- Create: `internal/vault/service.go`
- Test: `internal/vault/service_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/vault/service_test.go`:

```go
package vault

import "testing"

func TestServiceStartsLocked(t *testing.T) {
	service := NewService()

	if !service.IsLocked() {
		t.Fatal("new vault service should start locked")
	}
}

func TestUnlockAndLock(t *testing.T) {
	service := NewService()

	if err := service.Unlock(UnlockRequest{KeyID: "dek_1"}); err != nil {
		t.Fatalf("Unlock returned error: %v", err)
	}
	if service.IsLocked() {
		t.Fatal("vault should be unlocked")
	}

	service.Lock()
	if !service.IsLocked() {
		t.Fatal("vault should be locked")
	}
}

func TestUnlockRequiresKeyID(t *testing.T) {
	service := NewService()

	if err := service.Unlock(UnlockRequest{}); err == nil {
		t.Fatal("Unlock returned nil, want error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/vault
```

Expected: FAIL because `NewService` is not defined.

- [ ] **Step 3: Implement vault state**

Create `internal/vault/service.go`:

```go
package vault

import (
	"errors"
	"sync"
)

type UnlockRequest struct {
	KeyID string
}

type Service struct {
	mu       sync.RWMutex
	locked   bool
	activeKey string
}

func NewService() *Service {
	return &Service{locked: true}
}

func (s *Service) IsLocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.locked
}

func (s *Service) Unlock(request UnlockRequest) error {
	if request.KeyID == "" {
		return errors.New("key id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeKey = request.KeyID
	s.locked = false
	return nil
}

func (s *Service) Lock() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeKey = ""
	s.locked = true
}

func (s *Service) ActiveKeyID() (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.locked || s.activeKey == "" {
		return "", false
	}
	return s.activeKey, true
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
git commit -m "feat: add vault lock state"
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

func NewContainer() *Container {
	permissionPolicy := permissions.NewPolicy([]permissions.Grant{
		{SubjectID: "builtin.tools", Capability: "tools:hash", Scope: "*"},
	})

	return &Container{
		Commands:     commands.NewRegistry(),
		Modules:      modules.NewRegistry(),
		Permissions:  permissionPolicy,
		Capabilities: capabilities.NewGateway(permissionPolicy),
		Vault:         vault.NewService(),
		HashTools:     tools.NewHashService(),
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
	"time"

	"changeme/internal/app"
	"changeme/internal/bridge"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func init() {
	application.RegisterEvent[string]("time")
}

func main() {
	container := app.NewContainer()
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

- App opens.
- Workbench shell appears.
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
- `internal/crypto`: encrypted record envelope types.
- `internal/vault`: vault lock/unlock skeleton.
- `internal/commands`: command registry.
- `internal/modules`: built-in module registry.
- `internal/permissions`: permission grants and policy checks.
- `internal/capabilities`: permission-aware capability gateway.
- `internal/services/tools`: hash/encoding services.
- `internal/bridge`: Wails-facing app service and DTOs.

## Current Rules

- Frontend code calls `frontend/src/bridge`, not generated Wails bindings directly.
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

- App starts.
- Workbench shell renders.
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
- Vault skeleton: covered by Task 6.
- Command registry: covered by Task 7.
- First core module: covered by Task 8 and Task 18.
- Permission model: covered by Task 9.
- Capability Gateway: covered by Task 10.
- Module registry: covered by Task 11 and Task 16.
- Wails bridge: covered by Tasks 13-15.
- Workbench shell: covered by Task 17.
- Verification: covered by Task 20.

Known gaps intentionally deferred:

- Real encrypted SQLite/SQLCipher integration.
- Real key derivation and encryption/decryption implementation.
- Persistent vault item repository.
- External plugin runtime.
- SaaS sync.
- SSH and DB manager modules.
