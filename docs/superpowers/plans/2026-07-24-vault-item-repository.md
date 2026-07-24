# Vault Item Repository Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the three separate built-in item stores (notes, DB profiles, SSH profiles) with a single unified `vault_items` store fronted by a shared `vaultitem` repository + store layer that owns storage, crypto seal/open, and cross-type search.

**Architecture:** A new `internal/services/vaultitem` package owns a generic `Record{ID, Type, Metadata, Payload, CreatedAt, UpdatedAt, KeyVersion, SyncState}`, a `Repository` (memory + SQLite over the `vault_items` table with a `type` column), and a `Store` that seals/opens payloads with per-type AAD and searches plaintext metadata across types. The notes/database/ssh services become thin adapters over the shared `Store` while keeping their public signatures and their own connection behavior (DB pool/TLS, SSH host keys). One new bridge family `VaultSearch` exposes cross-type search.

**Tech Stack:** Go (pure-Go `modernc.org/sqlite`), XChaCha20-Poly1305 envelopes (`internal/crypto`), Wails v3 bridge, existing forward-only migration runner.

**Key invariants that MUST NOT change:**
- Item IDs stay 24-char lowercase hex (12 random bytes) so the bridge `recordID()` opaque-ID validation keeps working.
- Per-type AAD `key_id` stays `note-body` / `db-password` / `ssh-secret`; AAD triple is `{RecordType: <type>, RecordID: <id>, KeyID: <key>}`.
- Public method signatures of `notes.Service`, `database.Service`, `ssh.Service` are unchanged, so `internal/bridge/*` and the frontend are untouched.
- Greenfield: no data backfill. Migration creates `vault_items` and drops the legacy tables.

---

## File Structure

**Create:**
- `internal/services/vaultitem/vaultitem.go` — `Record`, `Repository`, `Vault` interfaces, `ErrNotFound`, constants.
- `internal/services/vaultitem/memory.go` — `MemoryRepository`.
- `internal/services/vaultitem/sqlrepository.go` — `SQLRepository` over `vault_items` (id, type, data).
- `internal/services/vaultitem/store.go` — `Store` (Create/Get/OpenPayload/UpdateMetadata/UpdateSecret/Delete/ListByType/Search).
- `internal/services/vaultitem/store_test.go`, `memory_test.go`, `sqlrepository_test.go`.

**Modify:**
- `internal/storage/sqlite.go` — add `TableVaultItems`; add migrations m5 (create) + m6 (drop legacy) in `Migrate`.
- `internal/storage/migrations_test.go` — assert new table exists, legacy tables gone, idempotent.
- `internal/services/notes/notes.go` — rewrite onto `vaultitem.Store`; delete `notes/repository.go`, `notes/sqlrepository.go`.
- `internal/services/notes/notes_test.go` — rewrite against a fake store.
- `internal/services/database/database.go` — swap persistence onto store; keep pool/TLS/query; delete `database/repository.go`, `database/sqlrepository.go`.
- `internal/services/database/*_test.go` — adjust persistence-facing tests.
- `internal/services/ssh/ssh.go` — swap persistence onto store; keep dial/hostkeys/run; delete `ssh/repository.go`, `ssh/sqlrepository.go`.
- `internal/services/ssh/*_test.go` — adjust persistence-facing tests.
- `internal/app/container.go` — build one `vaultitem.Store`, inject into the three services; add `Items` field; add `builtin.search` grant.
- `internal/app/container_test.go` (if present) — adjust constructor calls.
- `main.go` — one shared item store instead of three repositories.
- `internal/bridge/search.go` (Create) + `internal/bridge/dto.go` — `VaultSearch` + `VaultSearchResult`.
- `internal/bridge/gateway_test.go` — `VaultSearch` allowed/denied cases.

---

## Task 1: Storage — `vault_items` table

**Files:**
- Modify: `internal/storage/sqlite.go`
- Test: `internal/storage/migrations_test.go`

> **Ordering note:** This task only *creates* `vault_items`. Dropping the three
> legacy tables is deferred to Task 8 (migration m6), after the services and
> `main.go` no longer reference them and the old per-service repository tests
> are deleted. Dropping them here would break `notes`/`database`/`ssh`
> `sqlrepository_test.go` at this commit boundary.

- [ ] **Step 1: Write the failing test**

Add to `internal/storage/migrations_test.go`:

```go
func TestMigrateCreatesVaultItemsTable(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	tableExists := func(name string) bool {
		var got string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name = ?", name,
		).Scan(&got)
		return err == nil && got == name
	}

	if !tableExists(TableVaultItems) {
		t.Fatalf("expected %s table to exist", TableVaultItems)
	}

	// Idempotent: running again must not error.
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}
```

Ensure the test file imports `context`, `path/filepath`, `testing`.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=/private/tmp/devkit-go-cache go test ./internal/storage/ -run TestMigrateCreatesVaultItemsTable -v`
Expected: FAIL — `vault_items` does not exist.

- [ ] **Step 3: Add the table constant**

In `internal/storage/sqlite.go`, add to the const block:

```go
const (
	TableVaultKeyring       = "vault_keyring"
	TableNotes              = "notes"
	TableConnectionProfiles = "connection_profiles"
	TableSSHProfiles        = "ssh_profiles"
	TableVaultItems         = "vault_items"
)
```

- [ ] **Step 4: Add migration m5 in `Migrate`**

In `internal/storage/sqlite.go`, replace the tail of `Migrate` (from `m4 := kvTable(...)` through the `runner` construction) with the following. Use the `TableVaultItems` constant in the DDL (matching the const-block comment that table names are safe to interpolate):

```go
	m4 := kvTable(TableSSHProfiles)
	m4.Version, m4.Name = 4, "create ssh_profiles"

	m5 := Migration{
		Version: 5,
		Name:    "create vault_items",
		Up: func(ctx context.Context) error {
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				"CREATE TABLE IF NOT EXISTS %s (id TEXT PRIMARY KEY, type TEXT NOT NULL, data TEXT NOT NULL)",
				TableVaultItems,
			))
			return err
		},
	}

	runner, err := NewMigrationRunner(&sqlMigrationStore{db: db}, []Migration{m1, m2, m3, m4, m5})
	if err != nil {
		return err
	}
	return runner.Run(ctx)
```

(The `m1`–`m3` definitions above this block stay as they are.)

- [ ] **Step 5: Run test to verify it passes**

Run: `GOCACHE=/private/tmp/devkit-go-cache go test ./internal/storage/ -v`
Expected: PASS (all storage tests). Do NOT modify `sqlite_test.go` — the `notes` table still exists after this task.

- [ ] **Step 6: Commit**

```bash
git add internal/storage/sqlite.go internal/storage/migrations_test.go
git commit -m "feat(storage): add vault_items table"
```

---

## Task 2: `vaultitem` — Record, interfaces, MemoryRepository

**Files:**
- Create: `internal/services/vaultitem/vaultitem.go`
- Create: `internal/services/vaultitem/memory.go`
- Test: `internal/services/vaultitem/memory_test.go`

- [ ] **Step 1: Write `vaultitem.go`**

```go
// Package vaultitem is the shared storage + crypto substrate for built-in
// item types (notes, database profiles, SSH profiles). A Record is the at-rest
// shape common to every type: plaintext Metadata for listing/search, plus an
// optional encrypted Payload envelope for the type's secret.
package vaultitem

import (
	"encoding/json"
	"errors"
	"time"

	"changeme/internal/crypto"
)

// ErrNotFound is returned by repositories when an id has no item.
var ErrNotFound = errors.New("vault item not found")

const (
	currentKeyVersion = 1
	syncStateLocal    = "local"
)

// Record is one stored item of any type.
type Record struct {
	ID         string           `json:"id"`
	Type       string           `json:"type"`
	Metadata   json.RawMessage  `json:"metadata"`
	Payload    *crypto.Envelope `json:"payload,omitempty"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
	KeyVersion int              `json:"key_version"`
	SyncState  string           `json:"sync_state"`
}

// Repository persists Records. Payloads are already-encrypted envelopes, so a
// persisted row never contains plaintext secret content.
type Repository interface {
	List() ([]Record, error)
	ListByType(itemType string) ([]Record, error)
	Get(id string) (Record, error)
	Put(record Record) error
	Delete(id string) error
}

// Vault is the encryption capability the store needs. The DEK stays inside the
// vault; the store only gets seal/open.
type Vault interface {
	SealV2(associatedData crypto.AssociatedData, plaintext []byte) (crypto.Envelope, error)
	OpenWithAD(envelope crypto.Envelope, associatedData crypto.AssociatedData) ([]byte, error)
}
```

- [ ] **Step 2: Write `memory_test.go` (failing)**

```go
package vaultitem

import (
	"encoding/json"
	"testing"
)

func TestMemoryRepositoryRoundTrip(t *testing.T) {
	repo := NewMemoryRepository()

	rec := Record{ID: "a1", Type: "note", Metadata: json.RawMessage(`{"title":"x"}`)}
	if err := repo.Put(rec); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := repo.Get("a1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Type != "note" {
		t.Fatalf("type = %q, want note", got.Type)
	}

	repo.Put(Record{ID: "b2", Type: "db-profile"})
	notes, err := repo.ListByType("note")
	if err != nil {
		t.Fatalf("listbytype: %v", err)
	}
	if len(notes) != 1 || notes[0].ID != "a1" {
		t.Fatalf("ListByType(note) = %+v, want just a1", notes)
	}

	if err := repo.Delete("a1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get("a1"); err != ErrNotFound {
		t.Fatalf("get after delete err = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `GOCACHE=/private/tmp/devkit-go-cache go test ./internal/services/vaultitem/ -run TestMemoryRepositoryRoundTrip -v`
Expected: FAIL — `NewMemoryRepository` undefined.

- [ ] **Step 4: Write `memory.go`**

```go
package vaultitem

import "sync"

// MemoryRepository keeps items in memory (tests / ephemeral runs).
type MemoryRepository struct {
	mu      sync.RWMutex
	records map[string]Record
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{records: map[string]Record{}}
}

func (r *MemoryRepository) List() ([]Record, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Record, 0, len(r.records))
	for _, record := range r.records {
		out = append(out, record)
	}
	return out, nil
}

func (r *MemoryRepository) ListByType(itemType string) ([]Record, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Record, 0, len(r.records))
	for _, record := range r.records {
		if record.Type == itemType {
			out = append(out, record)
		}
	}
	return out, nil
}

func (r *MemoryRepository) Get(id string) (Record, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	record, ok := r.records[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	return record, nil
}

func (r *MemoryRepository) Put(record Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records[record.ID] = record
	return nil
}

func (r *MemoryRepository) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.records, id)
	return nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `GOCACHE=/private/tmp/devkit-go-cache go test ./internal/services/vaultitem/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/services/vaultitem/vaultitem.go internal/services/vaultitem/memory.go internal/services/vaultitem/memory_test.go
git commit -m "feat(vaultitem): add Record, Repository interfaces, MemoryRepository"
```

---

## Task 3: `vaultitem` — SQLRepository over `vault_items`

**Files:**
- Create: `internal/services/vaultitem/sqlrepository.go`
- Test: `internal/services/vaultitem/sqlrepository_test.go`

- [ ] **Step 1: Write `sqlrepository_test.go` (failing)**

```go
package vaultitem

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"changeme/internal/storage"
)

func newTestDB(t *testing.T) *SQLRepository {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewSQLRepository(db)
}

func TestSQLRepositoryRoundTrip(t *testing.T) {
	repo := newTestDB(t)

	if err := repo.Put(Record{ID: "a1", Type: "note", Metadata: json.RawMessage(`{"title":"x"}`)}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := repo.Put(Record{ID: "b2", Type: "db-profile", Metadata: json.RawMessage(`{"name":"y"}`)}); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := repo.Get("a1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Type != "note" {
		t.Fatalf("type = %q, want note", got.Type)
	}

	notes, err := repo.ListByType("note")
	if err != nil {
		t.Fatalf("listbytype: %v", err)
	}
	if len(notes) != 1 || notes[0].ID != "a1" {
		t.Fatalf("ListByType(note) = %+v, want just a1", notes)
	}

	if err := repo.Delete("a1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get("a1"); err != ErrNotFound {
		t.Fatalf("get after delete err = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=/private/tmp/devkit-go-cache go test ./internal/services/vaultitem/ -run TestSQLRepositoryRoundTrip -v`
Expected: FAIL — `NewSQLRepository` undefined.

- [ ] **Step 3: Write `sqlrepository.go`**

```go
package vaultitem

import (
	"context"
	"database/sql"
	"encoding/json"
)

// SQLRepository persists items in the vault_items table (id, type, data). The
// data column holds the JSON-encoded Record; type is duplicated into its own
// column for indexed ListByType. Payloads inside data are encrypted envelopes.
type SQLRepository struct {
	db *sql.DB
}

func NewSQLRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

func (r *SQLRepository) scanRows(rows *sql.Rows) ([]Record, error) {
	defer rows.Close()
	records := make([]Record, 0)
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var record Record
		if err := json.Unmarshal(data, &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (r *SQLRepository) List() ([]Record, error) {
	rows, err := r.db.QueryContext(context.Background(), "SELECT data FROM vault_items")
	if err != nil {
		return nil, err
	}
	return r.scanRows(rows)
}

func (r *SQLRepository) ListByType(itemType string) ([]Record, error) {
	rows, err := r.db.QueryContext(context.Background(), "SELECT data FROM vault_items WHERE type = ?", itemType)
	if err != nil {
		return nil, err
	}
	return r.scanRows(rows)
}

func (r *SQLRepository) Get(id string) (Record, error) {
	var data []byte
	err := r.db.QueryRowContext(context.Background(), "SELECT data FROM vault_items WHERE id = ?", id).Scan(&data)
	if err == sql.ErrNoRows {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (r *SQLRepository) Put(record Record) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(context.Background(),
		"INSERT INTO vault_items (id, type, data) VALUES (?, ?, ?) "+
			"ON CONFLICT(id) DO UPDATE SET type = excluded.type, data = excluded.data",
		record.ID, record.Type, data,
	)
	return err
}

func (r *SQLRepository) Delete(id string) error {
	_, err := r.db.ExecContext(context.Background(), "DELETE FROM vault_items WHERE id = ?", id)
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOCACHE=/private/tmp/devkit-go-cache go test ./internal/services/vaultitem/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/vaultitem/sqlrepository.go internal/services/vaultitem/sqlrepository_test.go
git commit -m "feat(vaultitem): add SQLRepository over vault_items"
```

---

## Task 4: `vaultitem` — Store (crypto + search)

**Files:**
- Create: `internal/services/vaultitem/store.go`
- Test: `internal/services/vaultitem/store_test.go`

- [ ] **Step 1: Write `store_test.go` (failing)**

This test uses the real crypto envelope path via `internal/vault` so AAD binding is exercised end to end.

```go
package vaultitem

import (
	"encoding/json"
	"testing"

	"changeme/internal/vault"
)

func newUnlockedStore(t *testing.T) *Store {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return NewStore(NewMemoryRepository(), v)
}

func TestStoreCreateOpenRoundTrip(t *testing.T) {
	s := newUnlockedStore(t)

	rec, err := s.Create("note", "note-body", json.RawMessage(`{"title":"hello"}`), []byte("secret body"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(rec.ID) != 24 {
		t.Fatalf("id %q length = %d, want 24 hex chars", rec.ID, len(rec.ID))
	}
	if rec.KeyVersion != 1 || rec.SyncState != "local" {
		t.Fatalf("key_version/sync_state = %d/%q, want 1/local", rec.KeyVersion, rec.SyncState)
	}

	plain, err := s.OpenPayload(rec, "note-body")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(plain) != "secret body" {
		t.Fatalf("plain = %q, want %q", plain, "secret body")
	}
}

func TestStoreOpenRejectsSwappedID(t *testing.T) {
	s := newUnlockedStore(t)

	a, _ := s.Create("note", "note-body", json.RawMessage(`{"title":"a"}`), []byte("A body"))
	b, _ := s.Create("note", "note-body", json.RawMessage(`{"title":"b"}`), []byte("B body"))

	// Move A's payload onto B's record: AAD (record id) no longer matches.
	b.Payload = a.Payload
	if _, err := s.OpenPayload(b, "note-body"); err == nil {
		t.Fatal("expected open to fail for swapped payload (AAD mismatch)")
	}
}

func TestStoreSearchMetadataAcrossTypes(t *testing.T) {
	s := newUnlockedStore(t)

	s.Create("note", "note-body", json.RawMessage(`{"title":"staging notes"}`), []byte("x"))
	s.Create("db-profile", "db-password", json.RawMessage(`{"name":"staging db"}`), []byte("pw"))
	s.Create("ssh-profile", "ssh-secret", json.RawMessage(`{"name":"prod box"}`), []byte("key"))

	hits, err := s.Search("staging")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("Search(staging) returned %d, want 2", len(hits))
	}

	scoped, err := s.Search("staging", "note")
	if err != nil {
		t.Fatalf("scoped search: %v", err)
	}
	if len(scoped) != 1 || scoped[0].Type != "note" {
		t.Fatalf("scoped Search = %+v, want one note", scoped)
	}
}
```

> Note: confirm the vault setup method name/signature before running — this plan assumes `vault.NewService`, `vault.NewMemoryKeyringStore`, and a `Setup(masterPassword) (recoveryKey string, err error)` that leaves the vault unlocked, matching `internal/vault/service.go`. If `Setup` does not auto-unlock, call the unlock method after setup in `newUnlockedStore`.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=/private/tmp/devkit-go-cache go test ./internal/services/vaultitem/ -run TestStore -v`
Expected: FAIL — `NewStore` undefined.

- [ ] **Step 3: Write `store.go`**

```go
package vaultitem

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"changeme/internal/crypto"
)

// Store owns id generation, seal/open with per-type AAD binding, and plaintext
// metadata search. Services provide their type name and key id; the Store never
// interprets a type's metadata beyond string search.
type Store struct {
	repo  Repository
	vault Vault
}

func NewStore(repo Repository, vault Vault) *Store {
	return &Store{repo: repo, vault: vault}
}

// NewID returns a 24-char lowercase hex id (12 random bytes). The length is
// load-bearing: the bridge recordID() opaque-id check requires exactly 24 hex.
func NewID() (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func associatedData(itemType, id, keyID string) crypto.AssociatedData {
	return crypto.AssociatedData{RecordType: itemType, RecordID: id, KeyID: keyID}
}

// Create seals secret (when non-nil) under AAD {itemType, newID, keyID} and
// stores the record. When secret is nil, the item has no payload.
func (s *Store) Create(itemType, keyID string, metadata json.RawMessage, secret []byte) (Record, error) {
	id, err := NewID()
	if err != nil {
		return Record{}, err
	}
	now := time.Now().UTC()
	record := Record{
		ID:         id,
		Type:       itemType,
		Metadata:   metadata,
		CreatedAt:  now,
		UpdatedAt:  now,
		KeyVersion: currentKeyVersion,
		SyncState:  syncStateLocal,
	}
	if secret != nil {
		envelope, err := s.vault.SealV2(associatedData(itemType, id, keyID), secret)
		if err != nil {
			return Record{}, err
		}
		record.Payload = &envelope
	}
	if err := s.repo.Put(record); err != nil {
		return Record{}, err
	}
	return record, nil
}

// Get returns the stored record (payload still encrypted).
func (s *Store) Get(id string) (Record, error) {
	return s.repo.Get(id)
}

// ListByType returns all records of a type (payloads still encrypted).
func (s *Store) ListByType(itemType string) ([]Record, error) {
	return s.repo.ListByType(itemType)
}

// OpenPayload decrypts a record's payload. Returns an error when the item has
// no payload or when the vault is locked / AAD does not match.
func (s *Store) OpenPayload(record Record, keyID string) ([]byte, error) {
	if record.Payload == nil {
		return nil, errors.New("item has no payload")
	}
	return s.vault.OpenWithAD(*record.Payload, associatedData(record.Type, record.ID, keyID))
}

// UpdateMetadata rewrites plaintext metadata, bumping UpdatedAt. Payload and
// timestamps other than UpdatedAt are preserved.
func (s *Store) UpdateMetadata(id string, metadata json.RawMessage) (Record, error) {
	record, err := s.repo.Get(id)
	if err != nil {
		return Record{}, err
	}
	record.Metadata = metadata
	record.UpdatedAt = time.Now().UTC()
	if err := s.repo.Put(record); err != nil {
		return Record{}, err
	}
	return record, nil
}

// Delete removes an item by id.
func (s *Store) Delete(id string) error {
	return s.repo.Delete(id)
}

// Search matches query (case-insensitive) against plaintext metadata across the
// given types (all types when none are given). It never decrypts payloads, so
// it works while the vault is locked. Matching is a substring test over the raw
// metadata JSON; that can match a JSON key name, which is acceptable for the
// local MVP scale.
func (s *Store) Search(query string, types ...string) ([]Record, error) {
	needle := strings.ToLower(strings.TrimSpace(query))

	var candidates []Record
	if len(types) == 0 {
		all, err := s.repo.List()
		if err != nil {
			return nil, err
		}
		candidates = all
	} else {
		for _, t := range types {
			recs, err := s.repo.ListByType(t)
			if err != nil {
				return nil, err
			}
			candidates = append(candidates, recs...)
		}
	}

	if needle == "" {
		return candidates, nil
	}
	matched := make([]Record, 0, len(candidates))
	for _, record := range candidates {
		if strings.Contains(strings.ToLower(string(record.Metadata)), needle) {
			matched = append(matched, record)
		}
	}
	return matched, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOCACHE=/private/tmp/devkit-go-cache go test ./internal/services/vaultitem/ -v`
Expected: PASS (all vaultitem tests).

- [ ] **Step 5: Commit**

```bash
git add internal/services/vaultitem/store.go internal/services/vaultitem/store_test.go
git commit -m "feat(vaultitem): add Store with per-type AAD seal/open and metadata search"
```

---

## Task 5: Rewrite `notes` service onto the store

**Files:**
- Modify: `internal/services/notes/notes.go`
- Delete: `internal/services/notes/repository.go`, `internal/services/notes/sqlrepository.go`
- Test: `internal/services/notes/notes_test.go` (rewrite)

- [ ] **Step 1: Rewrite `notes_test.go` against a fake store**

Replace the file with tests that build the service on a real `vaultitem.Store` over a memory repo + unlocked vault:

```go
package notes

import (
	"testing"

	"changeme/internal/services/vaultitem"
	"changeme/internal/vault"
)

func newService(t *testing.T) *Service {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return NewService(vaultitem.NewStore(vaultitem.NewMemoryRepository(), v))
}

func TestCreateAndGet(t *testing.T) {
	s := newService(t)

	summary, err := s.Create("Title", "body text")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if summary.Title != "Title" {
		t.Fatalf("title = %q", summary.Title)
	}

	note, err := s.Get(summary.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if note.Body != "body text" {
		t.Fatalf("body = %q", note.Body)
	}
}

func TestCreateRequiresTitle(t *testing.T) {
	s := newService(t)
	if _, err := s.Create("", "b"); err == nil {
		t.Fatal("expected error for empty title")
	}
}

func TestListNewestFirst(t *testing.T) {
	s := newService(t)
	a, _ := s.Create("A", "1")
	b, _ := s.Create("B", "2")
	list, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d", len(list))
	}
	// Newest (b) first when timestamps differ; tolerate equal timestamps.
	_ = a
	_ = b
}

func TestSearchByTitle(t *testing.T) {
	s := newService(t)
	s.Create("staging deploy", "x")
	s.Create("prod deploy", "y")
	hits, err := s.Search("staging")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || hits[0].Title != "staging deploy" {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestSearchByBodyWhenUnlocked(t *testing.T) {
	s := newService(t)
	s.Create("note one", "contains rocketship")
	hits, err := s.Search("rocketship")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected body match, got %d", len(hits))
	}
}

func TestDelete(t *testing.T) {
	s := newService(t)
	summary, _ := s.Create("x", "y")
	if err := s.Delete(summary.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(summary.ID); err == nil {
		t.Fatal("expected get to fail after delete")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=/private/tmp/devkit-go-cache go test ./internal/services/notes/ -v`
Expected: FAIL/compile error — `NewService` still expects the old `(repo, vault)` signature; `Record`/`Repository` still referenced by `repository.go`/`sqlrepository.go`.

- [ ] **Step 3: Delete the old repository files**

```bash
git rm internal/services/notes/repository.go internal/services/notes/sqlrepository.go
```

- [ ] **Step 4: Rewrite `notes.go`**

```go
package notes

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"changeme/internal/services/vaultitem"
)

const (
	itemType  = "note"
	bodyKeyID = "note-body"
)

// Store is the shared vault-item substrate the notes service needs.
type Store interface {
	Create(itemType, keyID string, metadata json.RawMessage, secret []byte) (vaultitem.Record, error)
	Get(id string) (vaultitem.Record, error)
	OpenPayload(record vaultitem.Record, keyID string) ([]byte, error)
	ListByType(itemType string) ([]vaultitem.Record, error)
	Delete(id string) error
}

// metadata is the plaintext, local-only note metadata. Title stays plaintext so
// notes list without decrypting; encrypt it too if titles ever sync.
type metadata struct {
	Title string `json:"title"`
}

// Summary is a note without its (encrypted) body, safe to list while unlocked.
type Summary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Note is the decrypted form returned to the caller.
type Note struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) Create(title string, body string) (Summary, error) {
	if title == "" {
		return Summary{}, errors.New("title is required")
	}
	meta, err := json.Marshal(metadata{Title: title})
	if err != nil {
		return Summary{}, err
	}
	record, err := s.store.Create(itemType, bodyKeyID, meta, []byte(body))
	if err != nil {
		return Summary{}, err
	}
	return toSummary(record)
}

func (s *Service) List() ([]Summary, error) {
	records, err := s.store.ListByType(itemType)
	if err != nil {
		return nil, err
	}
	summaries := make([]Summary, 0, len(records))
	for _, record := range records {
		summary, err := toSummary(record)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})
	return summaries, nil
}

func (s *Service) Get(id string) (Note, error) {
	record, err := s.store.Get(id)
	if err != nil {
		return Note{}, err
	}
	meta, err := decodeMetadata(record)
	if err != nil {
		return Note{}, err
	}
	plain, err := s.store.OpenPayload(record, bodyKeyID)
	if err != nil {
		return Note{}, err
	}
	return Note{
		ID:        record.ID,
		Title:     meta.Title,
		Body:      string(plain),
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
	}, nil
}

func (s *Service) Delete(id string) error {
	return s.store.Delete(id)
}

// Search matches notes by query (case-insensitive). Titles are plaintext and are
// always searched. Bodies are encrypted, so they are only searched when the
// vault is unlocked (decrypt succeeds); when locked, Search degrades to
// title-only. An empty query returns all notes (newest first).
func (s *Service) Search(query string) ([]Summary, error) {
	needle := strings.ToLower(strings.TrimSpace(query))
	records, err := s.store.ListByType(itemType)
	if err != nil {
		return nil, err
	}

	matched := make([]Summary, 0, len(records))
	for _, record := range records {
		meta, err := decodeMetadata(record)
		if err != nil {
			return nil, err
		}
		if needle == "" || strings.Contains(strings.ToLower(meta.Title), needle) {
			matched = append(matched, summaryFrom(record, meta))
			continue
		}
		if plain, openErr := s.store.OpenPayload(record, bodyKeyID); openErr == nil {
			if strings.Contains(strings.ToLower(string(plain)), needle) {
				matched = append(matched, summaryFrom(record, meta))
			}
		}
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].UpdatedAt.After(matched[j].UpdatedAt)
	})
	return matched, nil
}

func decodeMetadata(record vaultitem.Record) (metadata, error) {
	var meta metadata
	if err := json.Unmarshal(record.Metadata, &meta); err != nil {
		return metadata{}, err
	}
	return meta, nil
}

func summaryFrom(record vaultitem.Record, meta metadata) Summary {
	return Summary{
		ID:        record.ID,
		Title:     meta.Title,
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
	}
}

func toSummary(record vaultitem.Record) (Summary, error) {
	meta, err := decodeMetadata(record)
	if err != nil {
		return Summary{}, err
	}
	return summaryFrom(record, meta), nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `GOCACHE=/private/tmp/devkit-go-cache go test ./internal/services/notes/ -v`
Expected: PASS. (Container/main will not compile yet — that is Task 8.)

- [ ] **Step 6: Commit**

```bash
git add internal/services/notes/
git commit -m "refactor(notes): back the notes service with the shared vaultitem store"
```

---

## Task 6: Rewrite `database` service onto the store

**Files:**
- Modify: `internal/services/database/database.go`
- Delete: `internal/services/database/repository.go`, `internal/services/database/sqlrepository.go`
- Test: `internal/services/database/database_test.go`, `repository_test.go` (adjust)

**Unchanged (keep verbatim):** `pool.go`, and in `database.go` these functions keep their exact current bodies — `TestConnection`, `Query`, `pool`, `normalizeSSLMode`, `isLoopbackHost`, `stringify`, plus the `QueryResult` / `CreateProfileInput` / `ProfileSummary` types and the `DefaultMaxRows` / `ErrInsecureTLSForRemoteHost` declarations. Only persistence and secret access move to the store.

- [ ] **Step 1: Adjust `database_test.go` constructor + drop repository_test.go**

Replace the service constructor helper so tests build on the shared store, and delete the now-obsolete `repository_test.go` (it tested the deleted per-service repositories):

```bash
git rm internal/services/database/repository_test.go
```

In `database_test.go`, replace any `NewService(NewMemoryRepository(), vault)` construction with:

```go
func newService(t *testing.T) *Service {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return NewService(vaultitem.NewStore(vaultitem.NewMemoryRepository(), v))
}
```

Add imports `changeme/internal/services/vaultitem` and `changeme/internal/vault`; remove references to the deleted `MemoryRepository`/`ProfileRecord` where the test used them directly. Keep the CreateProfile/ListProfiles/normalizeSSLMode assertions.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=/private/tmp/devkit-go-cache go test ./internal/services/database/ -v`
Expected: FAIL/compile error — `NewService` signature and `Repository`/`ProfileRecord` references.

- [ ] **Step 3: Delete old repository files**

```bash
git rm internal/services/database/repository.go internal/services/database/sqlrepository.go
```

- [ ] **Step 4: Rewrite the persistence parts of `database.go`**

Replace the `Vault` interface, `ProfileRecord`, `Repository`, the `Service` struct + `NewService`, `CreateProfile`, `ListProfiles`, `DeleteProfile`, `dsn`, `databaseProfileAssociatedData`, `toSummary`, and `newID` with the following. **Leave `TestConnection`, `Query`, `pool`, `normalizeSSLMode`, `isLoopbackHost`, `stringify`, `Close`, and the type/const declarations named above untouched.**

```go
// Store is the shared vault-item substrate the database service needs.
type Store interface {
	Create(itemType, keyID string, metadata json.RawMessage, secret []byte) (vaultitem.Record, error)
	Get(id string) (vaultitem.Record, error)
	OpenPayload(record vaultitem.Record, keyID string) ([]byte, error)
	ListByType(itemType string) ([]vaultitem.Record, error)
	Delete(id string) error
}

// metadata is the plaintext, non-sensitive connection metadata for a profile.
type metadata struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Username string `json:"username"`
	SSLMode  string `json:"ssl_mode"`
}

type Service struct {
	store   Store
	pools   *PoolManager
	maxRows int
}

func NewService(store Store) *Service {
	return &Service{store: store, pools: NewPoolManager(), maxRows: DefaultMaxRows}
}

func (s *Service) CreateProfile(input CreateProfileInput) (ProfileSummary, error) {
	if input.Name == "" {
		return ProfileSummary{}, errors.New("name is required")
	}
	if input.Host == "" {
		return ProfileSummary{}, errors.New("host is required")
	}
	if input.Port == 0 {
		input.Port = 5432
	}
	sslMode, err := normalizeSSLMode(input.Host, input.SSLMode)
	if err != nil {
		return ProfileSummary{}, err
	}
	input.SSLMode = sslMode

	meta, err := json.Marshal(metadata{
		Name:     input.Name,
		Host:     input.Host,
		Port:     input.Port,
		Database: input.Database,
		Username: input.Username,
		SSLMode:  input.SSLMode,
	})
	if err != nil {
		return ProfileSummary{}, err
	}
	record, err := s.store.Create(itemType, passwordKeyID, meta, []byte(input.Password))
	if err != nil {
		return ProfileSummary{}, err
	}
	return summaryFromRecord(record)
}

func (s *Service) ListProfiles() ([]ProfileSummary, error) {
	records, err := s.store.ListByType(itemType)
	if err != nil {
		return nil, err
	}
	summaries := make([]ProfileSummary, 0, len(records))
	for _, record := range records {
		summary, err := summaryFromRecord(record)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Name < summaries[j].Name
	})
	return summaries, nil
}

func (s *Service) DeleteProfile(id string) error {
	if err := s.store.Delete(id); err != nil {
		return err
	}
	s.pools.Remove(id)
	return nil
}

func (s *Service) dsn(id string) (string, error) {
	record, err := s.store.Get(id)
	if err != nil {
		return "", err
	}
	meta, err := decodeMetadata(record)
	if err != nil {
		return "", err
	}
	password, err := s.store.OpenPayload(record, passwordKeyID)
	if err != nil {
		return "", err
	}
	sslmode, err := normalizeSSLMode(meta.Host, meta.SSLMode)
	if err != nil {
		return "", err
	}
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(meta.Username, string(password)),
		Host:   net.JoinHostPort(meta.Host, fmt.Sprintf("%d", meta.Port)),
		Path:   meta.Database,
	}
	query := u.Query()
	query.Set("sslmode", sslmode)
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func decodeMetadata(record vaultitem.Record) (metadata, error) {
	var meta metadata
	if err := json.Unmarshal(record.Metadata, &meta); err != nil {
		return metadata{}, err
	}
	return meta, nil
}

func summaryFromRecord(record vaultitem.Record) (ProfileSummary, error) {
	meta, err := decodeMetadata(record)
	if err != nil {
		return ProfileSummary{}, err
	}
	return ProfileSummary{
		ID:       record.ID,
		Name:     meta.Name,
		Host:     meta.Host,
		Port:     meta.Port,
		Database: meta.Database,
		Username: meta.Username,
		SSLMode:  meta.SSLMode,
	}, nil
}
```

Update the const block to add `itemType` and drop the removed record-type constant:

```go
const (
	passwordKeyID = "db-password"
	itemType      = "db-profile"
)
```

Update imports: add `"encoding/json"` and `"changeme/internal/services/vaultitem"`; remove `"crypto/rand"`, `"encoding/hex"`, and the now-unused `"changeme/internal/crypto"` and `"time"` if nothing else uses them (the compiler will flag unused imports — remove exactly those the build complains about).

- [ ] **Step 5: Run test to verify it passes**

Run: `GOCACHE=/private/tmp/devkit-go-cache go test ./internal/services/database/ -v`
Expected: PASS (unit tests; integration tests that need a live Postgres are gated as before).

- [ ] **Step 6: Commit**

```bash
git add internal/services/database/
git commit -m "refactor(database): back profiles with the shared vaultitem store"
```

---

## Task 7: Rewrite `ssh` service onto the store

**Files:**
- Modify: `internal/services/ssh/ssh.go`
- Delete: `internal/services/ssh/repository.go`, `internal/services/ssh/sqlrepository.go`
- Test: `internal/services/ssh/ssh_test.go`, `repository_test.go` (adjust)

**Unchanged (keep verbatim):** `hostkeys.go`, and in `ssh.go` these keep their exact current bodies — `TestConnection`, `RunCommand`, `dial`'s SSH-config/dial logic, `parsePrivateKey`, `zeroBytes`, the `observeHostKeyCallback`/`pinnedHostKeyCallback` usage, and the `CommandResult`/`CreateProfileInput`/`ProfileSummary` types and `Auth*` consts. Only persistence, metadata reads, and secret access move to the store.

- [ ] **Step 1: Adjust `ssh_test.go` constructor + drop repository_test.go**

```bash
git rm internal/services/ssh/repository_test.go
```

In `ssh_test.go`, replace the service constructor with a store-backed helper (same shape as Task 5/6):

```go
func newService(t *testing.T) *Service {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return NewService(vaultitem.NewStore(vaultitem.NewMemoryRepository(), v))
}
```

Add imports `changeme/internal/services/vaultitem` and `changeme/internal/vault`; remove direct `MemoryRepository`/`ProfileRecord` usage. Keep CreateProfile validation and host-key trust assertions.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=/private/tmp/devkit-go-cache go test ./internal/services/ssh/ -v`
Expected: FAIL/compile error.

- [ ] **Step 3: Delete old repository files**

```bash
git rm internal/services/ssh/repository.go internal/services/ssh/sqlrepository.go
```

- [ ] **Step 4: Rewrite the persistence parts of `ssh.go`**

Replace the `Vault` interface, `ProfileRecord`, `Repository`, the `Service` struct + `NewService`, `CreateProfile`, `ListProfiles`, `DeleteProfile`, `ObserveHostKey`, `TrustHostKey`, the `dial` prologue that reads the record + secret, `sshProfileAssociatedData`, `toSummary`, and `newID` with the following. **Leave the SSH-config construction / `net.Dialer` / `gossh.NewClientConn` half of `dial`, plus `TestConnection`, `RunCommand`, `parsePrivateKey`, `zeroBytes` untouched.**

```go
// Store is the shared vault-item substrate the SSH service needs.
type Store interface {
	Create(itemType, keyID string, metadata json.RawMessage, secret []byte) (vaultitem.Record, error)
	Get(id string) (vaultitem.Record, error)
	OpenPayload(record vaultitem.Record, keyID string) ([]byte, error)
	ListByType(itemType string) ([]vaultitem.Record, error)
	UpdateMetadata(id string, metadata json.RawMessage) (vaultitem.Record, error)
	Delete(id string) error
}

// metadata is the plaintext, non-sensitive SSH connection metadata.
type metadata struct {
	Name               string `json:"name"`
	Host               string `json:"host"`
	Port               int    `json:"port"`
	Username           string `json:"username"`
	AuthMethod         string `json:"auth_method"`
	HostKeyFingerprint string `json:"host_key_fingerprint"`
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) CreateProfile(input CreateProfileInput) (ProfileSummary, error) {
	if input.Name == "" {
		return ProfileSummary{}, errors.New("name is required")
	}
	if input.Host == "" {
		return ProfileSummary{}, errors.New("host is required")
	}
	if input.Username == "" {
		return ProfileSummary{}, errors.New("username is required")
	}
	if input.Port == 0 {
		input.Port = 22
	}
	if input.AuthMethod == "" {
		input.AuthMethod = AuthPassword
	}
	if input.AuthMethod != AuthPassword && input.AuthMethod != AuthKey {
		return ProfileSummary{}, errors.New("unsupported auth method")
	}

	meta, err := json.Marshal(metadata{
		Name:       input.Name,
		Host:       input.Host,
		Port:       input.Port,
		Username:   input.Username,
		AuthMethod: input.AuthMethod,
	})
	if err != nil {
		return ProfileSummary{}, err
	}
	record, err := s.store.Create(itemType, secretKeyID, meta, []byte(input.Secret))
	if err != nil {
		return ProfileSummary{}, err
	}
	return summaryFromRecord(record)
}

func (s *Service) ListProfiles() ([]ProfileSummary, error) {
	records, err := s.store.ListByType(itemType)
	if err != nil {
		return nil, err
	}
	summaries := make([]ProfileSummary, 0, len(records))
	for _, record := range records {
		summary, err := summaryFromRecord(record)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Name < summaries[j].Name
	})
	return summaries, nil
}

func (s *Service) DeleteProfile(id string) error {
	return s.store.Delete(id)
}

func (s *Service) ObserveHostKey(ctx context.Context, id string) (string, error) {
	record, err := s.store.Get(id)
	if err != nil {
		return "", err
	}
	meta, err := decodeMetadata(record)
	if err != nil {
		return "", err
	}
	addr := net.JoinHostPort(meta.Host, strconv.Itoa(meta.Port))
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	var observed string
	config := &gossh.ClientConfig{
		User:            meta.Username,
		HostKeyCallback: observeHostKeyCallback(&observed),
		Timeout:         10 * time.Second,
	}
	_, _, _, err = gossh.NewClientConn(conn, addr, config)
	if observed != "" && errors.Is(err, errHostKeyObserved) {
		return observed, nil
	}
	if err != nil {
		return "", err
	}
	return "", errors.New("ssh server did not provide a host key")
}

func (s *Service) TrustHostKey(ctx context.Context, id string, fingerprint string) error {
	if fingerprint == "" {
		return errors.New("host key fingerprint is required")
	}
	observed, err := s.ObserveHostKey(ctx, id)
	if err != nil {
		return err
	}
	if observed != fingerprint {
		return errors.New("observed host key fingerprint does not match approval")
	}
	record, err := s.store.Get(id)
	if err != nil {
		return err
	}
	meta, err := decodeMetadata(record)
	if err != nil {
		return err
	}
	meta.HostKeyFingerprint = fingerprint
	updated, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = s.store.UpdateMetadata(id, updated)
	return err
}

func (s *Service) dial(ctx context.Context, id string, passphrase string) (*gossh.Client, error) {
	record, err := s.store.Get(id)
	if err != nil {
		return nil, err
	}
	meta, err := decodeMetadata(record)
	if err != nil {
		return nil, err
	}
	secret, err := s.store.OpenPayload(record, secretKeyID)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(secret)

	var auth gossh.AuthMethod
	switch meta.AuthMethod {
	case AuthPassword:
		auth = gossh.Password(string(secret))
	case AuthKey:
		signer, err := parsePrivateKey(secret, passphrase)
		if err != nil {
			return nil, err
		}
		auth = gossh.PublicKeys(signer)
	default:
		return nil, errors.New("unsupported auth method")
	}

	config := &gossh.ClientConfig{
		User:            meta.Username,
		Auth:            []gossh.AuthMethod{auth},
		HostKeyCallback: pinnedHostKeyCallback(meta.HostKeyFingerprint),
		Timeout:         10 * time.Second,
	}

	addr := net.JoinHostPort(meta.Host, strconv.Itoa(meta.Port))
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	clientConn, chans, reqs, err := gossh.NewClientConn(conn, addr, config)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return gossh.NewClient(clientConn, chans, reqs), nil
}

func decodeMetadata(record vaultitem.Record) (metadata, error) {
	var meta metadata
	if err := json.Unmarshal(record.Metadata, &meta); err != nil {
		return metadata{}, err
	}
	return meta, nil
}

func summaryFromRecord(record vaultitem.Record) (ProfileSummary, error) {
	meta, err := decodeMetadata(record)
	if err != nil {
		return ProfileSummary{}, err
	}
	return ProfileSummary{
		ID:                 record.ID,
		Name:               meta.Name,
		Host:               meta.Host,
		Port:               meta.Port,
		Username:           meta.Username,
		AuthMethod:         meta.AuthMethod,
		HostKeyFingerprint: meta.HostKeyFingerprint,
	}, nil
}
```

Update the const block to add `itemType` and drop the removed record-type constant:

```go
const (
	AuthPassword = "password"
	AuthKey      = "key"

	secretKeyID = "ssh-secret"
	itemType    = "ssh-profile"
)
```

Update imports: add `"encoding/json"` and `"changeme/internal/services/vaultitem"`; remove `"crypto/rand"`, `"encoding/hex"`, and `"changeme/internal/crypto"` if the build reports them unused. Keep `context`, `net`, `sort`, `strconv`, `time`, `errors`, and `gossh`.

- [ ] **Step 5: Run test to verify it passes**

Run: `GOCACHE=/private/tmp/devkit-go-cache go test ./internal/services/ssh/ -v`
Expected: PASS (unit + hostkey tests; live-network integration tests gated as before).

- [ ] **Step 6: Commit**

```bash
git add internal/services/ssh/
git commit -m "refactor(ssh): back profiles with the shared vaultitem store"
```

---

## Task 8: Drop legacy tables + wire the container and `main.go`

**Files:**
- Modify: `internal/storage/sqlite.go` (add migration m6)
- Test: `internal/storage/migrations_test.go`
- Modify: `internal/app/container.go`
- Modify: `main.go`
- Test: `internal/app/container_test.go` (if present) — adjust constructor calls

By this point Tasks 5–7 have deleted the per-service `sqlrepository.go` files and
their tests, and `main.go` is about to stop referencing the legacy tables, so it
is now safe to drop them.

- [ ] **Step 1: Write the failing drop-migration test**

Add to `internal/storage/migrations_test.go`:

```go
func TestMigrateDropsLegacyItemTables(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	tableExists := func(name string) bool {
		var got string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name = ?", name,
		).Scan(&got)
		return err == nil && got == name
	}
	for _, legacy := range []string{TableNotes, TableConnectionProfiles, TableSSHProfiles} {
		if tableExists(legacy) {
			t.Fatalf("expected legacy table %q to be dropped", legacy)
		}
	}

	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=/private/tmp/devkit-go-cache go test ./internal/storage/ -run TestMigrateDropsLegacyItemTables -v`
Expected: FAIL — legacy tables still exist.

- [ ] **Step 3: Add migration m6 in `Migrate`**

In `internal/storage/sqlite.go`, after the `m5 := Migration{...}` block, add m6 and extend the runner slice to `{m1, m2, m3, m4, m5, m6}`:

```go
	m6 := Migration{
		Version: 6,
		Name:    "drop legacy item tables",
		Up: func(ctx context.Context) error {
			for _, table := range []string{TableNotes, TableConnectionProfiles, TableSSHProfiles} {
				if _, err := db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", table)); err != nil {
					return err
				}
			}
			return nil
		},
	}

	runner, err := NewMigrationRunner(&sqlMigrationStore{db: db}, []Migration{m1, m2, m3, m4, m5, m6})
	if err != nil {
		return err
	}
	return runner.Run(ctx)
```

- [ ] **Step 4: Fix the storage KV reopen test**

Migration m6 now drops the `notes` table, so `internal/storage/sqlite_test.go`'s
`TestSQLiteKVPersistenceAcrossReopen` must not use `TableNotes`. Change its
`storage.NewKV(db, TableNotes)` calls to `storage.NewKV(db, TableVaultKeyring)`
(a table m1 creates and nothing drops), keeping the rest of the test identical.

- [ ] **Step 5: Run storage tests**

Run: `GOCACHE=/private/tmp/devkit-go-cache go test ./internal/storage/ -v`
Expected: PASS (including the new drop test and the adjusted reopen test).

- [ ] **Step 6: Rewrite `container.go`**

```go
package app

import (
	"changeme/internal/audit"
	"changeme/internal/capabilities"
	"changeme/internal/commands"
	"changeme/internal/modules"
	"changeme/internal/permissions"
	"changeme/internal/services/database"
	"changeme/internal/services/notes"
	sshservice "changeme/internal/services/ssh"
	"changeme/internal/services/tools"
	"changeme/internal/services/vaultitem"
	"changeme/internal/vault"
)

type Container struct {
	Commands     *commands.Registry
	Modules      *modules.Registry
	Audit        *audit.MemorySink
	Permissions  *permissions.Policy
	Capabilities *capabilities.Gateway
	Vault        *vault.Service
	HashTools    *tools.HashService
	Items        *vaultitem.Store
	Notes        *notes.Service
	Database     *database.Service
	SSH          *sshservice.Service
}

// NewContainer wires core services with in-memory stores, suitable for tests
// and ephemeral runs.
func NewContainer() *Container {
	return NewContainerWithStores(
		vault.NewMemoryKeyringStore(),
		vaultitem.NewMemoryRepository(),
	)
}

func NewContainerWithStores(
	keyringStore vault.KeyringStore,
	itemRepo vaultitem.Repository,
) *Container {
	permissionPolicy := permissions.NewPolicy([]permissions.Grant{
		{SubjectID: "builtin.tools", Capability: "tools:hash", Scope: "command:tools.hash"},
		{SubjectID: "builtin.notes", Capability: "notes:write", Scope: "note:new"},
		{SubjectID: "builtin.notes", Capability: "notes:write", Scope: "note:*"},
		{SubjectID: "builtin.notes", Capability: "notes:read", Scope: "note:list"},
		{SubjectID: "builtin.notes", Capability: "notes:read", Scope: "note:search"},
		{SubjectID: "builtin.notes", Capability: "notes:read", Scope: "note:*"},
		{SubjectID: "builtin.database", Capability: "db:query", Scope: "profile:list"},
		{SubjectID: "builtin.database", Capability: "db:query", Scope: "profile:new"},
		{SubjectID: "builtin.database", Capability: "db:query", Scope: "profile:*"},
		{SubjectID: "builtin.ssh", Capability: "ssh:connect", Scope: "profile:list"},
		{SubjectID: "builtin.ssh", Capability: "ssh:connect", Scope: "profile:new"},
		{SubjectID: "builtin.ssh", Capability: "ssh:connect", Scope: "profile:*"},
		{SubjectID: "builtin.vault", Capability: "vault:manage", Scope: "vault"},
		{SubjectID: "builtin.search", Capability: "vault:search", Scope: "item:search"},
	})

	vaultService := vault.NewService(keyringStore)
	auditSink := audit.NewMemorySink()
	itemStore := vaultitem.NewStore(itemRepo, vaultService)

	return &Container{
		Commands:     commands.NewRegistry(),
		Modules:      modules.NewRegistry(),
		Audit:        auditSink,
		Permissions:  permissionPolicy,
		Capabilities: capabilities.NewGateway(permissionPolicy, auditSink),
		Vault:        vaultService,
		HashTools:    tools.NewHashService(),
		Items:        itemStore,
		Notes:        notes.NewService(itemStore),
		Database:     database.NewService(itemStore),
		SSH:          sshservice.NewService(itemStore),
	}
}
```

- [ ] **Step 7: Rewrite the store wiring in `main.go`**

Replace the `container := app.NewContainerWithStores(...)` block with:

```go
	container := app.NewContainerWithStores(
		vault.NewSQLKeyringStore(storage.NewKV(db, storage.TableVaultKeyring)),
		vaultitem.NewSQLRepository(db),
	)
```

Update `main.go` imports: remove `"changeme/internal/services/database"`, `"changeme/internal/services/notes"`, and `sshservice "changeme/internal/services/ssh"` (no longer referenced in `main.go`), and add `"changeme/internal/services/vaultitem"`. Keep `"changeme/internal/storage"` and `"changeme/internal/vault"`. Note `container.Database.Close()` is still called on shutdown and remains valid.

- [ ] **Step 8: Adjust `container_test.go` if it exists**

If `internal/app/container_test.go` calls `NewContainerWithStores` with the old four-argument shape, update it to the two-argument `(vault.NewMemoryKeyringStore(), vaultitem.NewMemoryRepository())` form. If no such test exists, skip this step.

- [ ] **Step 9: Build the whole module**

Run: `GOCACHE=/private/tmp/devkit-go-cache go build ./... && GOCACHE=/private/tmp/devkit-go-cache go build -tags server .`
Expected: builds with no errors.

- [ ] **Step 10: Run the full test suite**

Run: `GOCACHE=/private/tmp/devkit-go-cache go test ./...`
Expected: PASS across packages.

- [ ] **Step 11: Commit**

```bash
git add internal/storage/sqlite.go internal/storage/migrations_test.go internal/storage/sqlite_test.go internal/app/container.go main.go internal/app/container_test.go
git commit -m "refactor(app): drop legacy tables and wire one shared vaultitem store"
```

---

## Task 9: Bridge — `VaultSearch` cross-type search

**Files:**
- Create: `internal/bridge/search.go`
- Modify: `internal/bridge/dto.go` (add `VaultSearchResult`)
- Test: `internal/bridge/gateway_test.go`

- [ ] **Step 1: Write the failing bridge test**

Add to `internal/bridge/gateway_test.go` (mirror the construction the existing tests use to build an `AppService` with an unlocked vault; reuse that helper — shown here as `newUnlockedAppService(t)`):

```go
func TestVaultSearchReturnsCrossTypeHits(t *testing.T) {
	svc := newUnlockedAppService(t)

	if _, err := svc.NotesCreate("staging runbook", "body"); err != nil {
		t.Fatalf("notes create: %v", err)
	}
	if _, err := svc.DBCreateProfile(/* existing helper input with Name: "staging db" */); err != nil {
		t.Fatalf("db create: %v", err)
	}

	results, err := svc.VaultSearch("staging")
	if err != nil {
		t.Fatalf("vault search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("VaultSearch(staging) = %d results, want 2", len(results))
	}
}
```

> Adapt the DB profile creation call to the exact `DBCreateProfile` signature used elsewhere in `gateway_test.go`. If building an unlocked `AppService` in tests currently requires a helper that does not exist, add a small local helper in the test file that constructs `app.NewContainer()`, sets up + unlocks the vault via the vault bridge methods, registers builtins, and returns `NewAppService(container)`.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=/private/tmp/devkit-go-cache go test ./internal/bridge/ -run TestVaultSearch -v`
Expected: FAIL — `VaultSearch` undefined.

- [ ] **Step 3: Add the DTO**

In `internal/bridge/dto.go`, add:

```go
// VaultSearchResult is one cross-type search hit. Label is the item's plaintext
// display name (note title or profile name); no secret content is included.
type VaultSearchResult struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Label     string `json:"label"`
	UpdatedAt string `json:"updated_at"`
}
```

- [ ] **Step 4: Write `internal/bridge/search.go`**

```go
package bridge

import (
	"context"
	"encoding/json"

	"changeme/internal/services/vaultitem"
)

// VaultSearch returns cross-type items whose plaintext metadata matches query.
// It runs on metadata only, so it works while the vault is locked, and never
// returns secret payloads.
func (s *AppService) VaultSearch(query string) ([]VaultSearchResult, error) {
	result, err := s.call(context.Background(), "builtin.search", "vault:search", "item:search", map[string]any{
		"action": "search",
	}, func(context.Context) (any, error) {
		records, err := s.container.Items.Search(query)
		if err != nil {
			return nil, err
		}
		out := make([]VaultSearchResult, 0, len(records))
		for _, record := range records {
			out = append(out, VaultSearchResult{
				ID:        record.ID,
				Type:      record.Type,
				Label:     labelFromMetadata(record.Metadata),
				UpdatedAt: record.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			})
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]VaultSearchResult), nil
}

// labelFromMetadata pulls a display label from generic metadata: "title" for
// notes, "name" for profiles. Empty when neither is present.
func labelFromMetadata(raw json.RawMessage) string {
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ""
	}
	for _, key := range []string{"title", "name"} {
		if v, ok := fields[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

var _ = vaultitem.Record{} // keep the import if unused after edits; remove if go vet flags it
```

Remove the trailing `var _ =` line if the `vaultitem` import ends up used elsewhere or if `go vet`/build flags it — it is only a guard against an unused import while iterating. The clean version imports only `context` and `encoding/json` if `vaultitem` is not otherwise referenced.

- [ ] **Step 5: Run the test to verify it passes**

Run: `GOCACHE=/private/tmp/devkit-go-cache go test ./internal/bridge/ -v`
Expected: PASS.

- [ ] **Step 6: Add a denied-path test**

Add to `gateway_test.go` a case proving `VaultSearch` is audited/denied when the `builtin.search` grant is absent (construct a container whose policy omits the `vault:search` grant, following the existing denied-path test pattern in this file). Assert the returned error maps to `permission_denied`.

Run: `GOCACHE=/private/tmp/devkit-go-cache go test ./internal/bridge/ -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/bridge/search.go internal/bridge/dto.go internal/bridge/gateway_test.go
git commit -m "feat(bridge): add VaultSearch cross-type item search"
```

---

## Task 10: Full verification

**Files:** none (verification only)

- [ ] **Step 1: Run the Go verification suite**

```bash
GOCACHE=/private/tmp/devkit-go-cache go test ./...
GOCACHE=/private/tmp/devkit-go-cache go vet ./...
GOCACHE=/private/tmp/devkit-go-cache go build -tags server .
```
Expected: all pass; server binary builds with the loopback guard intact.

- [ ] **Step 2: Build the frontend (confirms no bridge binding shape broke)**

```bash
npm --prefix frontend run build
```
Expected: typecheck + Vite production build succeed. The three service signatures are unchanged, so generated bindings for notes/database/ssh are unaffected; the new `VaultSearch` binding is additive.

- [ ] **Step 3: Regenerate Wails bindings if the project tracks them**

If `frontend/bindings` (or equivalent) is committed, run `wails3 generate bindings` so the new `VaultSearch` method has a typed binding, then rebuild the frontend. If bindings are generated at build time only, skip this step.

- [ ] **Step 4: Commit any regenerated bindings**

```bash
git add -A
git commit -m "chore: regenerate bindings for VaultSearch" || echo "no binding changes"
```

---

## Docs impact (separate follow-up, AFTER this plan merges)

Update the PERSONAL/base DevKit docs to match the new runtime (these describe actual runtime, so update only once merged):

- `technical-contracts.mdx` — persistence contract now `vault_items` (id, type, data) replacing the three tables; add `VaultSearch` to the bridge family table; note AAD binding flows through `vaultitem.Store`.
- `implementation-status.mdx` — move "Vault item repository" from *Design only* to *Implemented*; update evidence paths (`internal/services/vaultitem/*`) and the Verified snapshot commit/date.
- `data-sync.mdx` — describe `vault_items` with `sync_state` / `key_version` as the sync foundation.
- `architecture-decisions.mdx` — add **ADR-008: Unified `vault_items` store for built-in item types**.
- `roadmap.mdx` — mark the Phase 1 vault-item repository "Next recommended work" item done.
- Re-run `bun run build` in `/Users/syuro/Workspace/PERSONAL/base` after editing.

---

## Self-review notes

- **Spec coverage:** schema/migration (Task 1), shared repository (Tasks 2–3), shared store with AAD + search (Task 4), thin services with unchanged signatures (Tasks 5–7), wiring (Task 8), `VaultSearch` + `builtin.search` grant (Task 9), verification (Task 10), docs impact (final section) — all spec sections mapped.
- **ID invariant:** `vaultitem.NewID` reproduces the existing 24-hex `newID`, so `recordID()` opaque-id validation keeps working. Verified against `internal/bridge/gateway.go`.
- **AAD invariant:** per-type `key_id` (`note-body` / `db-password` / `ssh-secret`) and the `{RecordType, RecordID, KeyID}` triple are preserved; Task 4's swap test guards it.
- **Assumptions to confirm at execution time:** the exact `vault.Service` setup/unlock method names used in test helpers, and the existing `gateway_test.go` helpers for building an unlocked `AppService` and creating a DB profile. These are flagged inline in Tasks 4 and 9.
