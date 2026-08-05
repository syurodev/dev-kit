package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"changeme/internal/storage"
	replicationsync "changeme/internal/sync"
	"changeme/internal/syncclient"
	"changeme/internal/vault"
	"changeme/internal/vaultitem"
)

// This is the two-device end-to-end test the conflict-resolution design calls
// for: two full device stacks (own SQLite DB + sync client) talk real HTTP to
// this in-memory mock, diverge on the same note, and then converge via each
// resolution strategy. Both devices share one unlocked vault (same DEK), which
// models the zero-knowledge invariant that only encrypted envelopes travel.

const e2eToken = "user-token"

func newMockServer(t *testing.T) string {
	t.Helper()
	s := newServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/sync/session", s.handleSession)
	mux.HandleFunc("/v1/sync/push", s.handlePush)
	mux.HandleFunc("/v1/sync/pull", s.handlePull)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

type device struct {
	repo      *vaultitem.SQLMutationRepository
	items     *vaultitem.Store
	sync      *syncclient.Service
	resolver  *storage.SQLConflictResolver
	conflicts *storage.SQLConflictStore
	remote    *storage.SQLRemoteStore
}

func newDevice(t *testing.T, baseURL, token string, vlt *vault.Service) *device {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "device.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := vaultitem.NewSQLMutationRepository(db)
	sync, err := syncclient.New(syncclient.Dependencies{
		DeviceIdentity: storage.NewSQLDeviceIdentityStore(db),
		Cursors:        storage.NewSQLCursorStore(db),
		Remote:         storage.NewSQLRemoteStore(db),
		LocalMutations: storage.NewSQLLocalMutationStore(db),
		Outbox:         storage.NewSQLOutbox(db),
		Vault:          vlt,
	})
	if err != nil {
		t.Fatalf("new sync client: %v", err)
	}
	if err := sync.Connect(ctx, baseURL, token); err != nil {
		t.Fatalf("connect: %v", err)
	}
	return &device{
		repo:      repo,
		items:     vaultitem.NewStore(repo, vlt),
		sync:      sync,
		resolver:  storage.NewSQLConflictResolver(db),
		conflicts: storage.NewSQLConflictStore(db),
		remote:    storage.NewSQLRemoteStore(db),
	}
}

func (d *device) createNote(t *testing.T, title, body string) string {
	t.Helper()
	meta, _ := json.Marshal(struct {
		Title string `json:"title"`
	}{title})
	record, err := d.items.Create("note", "note-body", meta, []byte(body))
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	return record.ID
}

func (d *device) editNote(t *testing.T, id, body string) {
	t.Helper()
	record, err := d.items.Get(id)
	if err != nil {
		t.Fatalf("get note: %v", err)
	}
	edited, err := d.items.Reseal(record, "note-body", []byte(body))
	if err != nil {
		t.Fatalf("reseal note: %v", err)
	}
	if err := d.repo.Put(edited); err != nil {
		t.Fatalf("put note: %v", err)
	}
}

func (d *device) body(t *testing.T, id string) string {
	t.Helper()
	record, err := d.items.Get(id)
	if err != nil {
		t.Fatalf("get note: %v", err)
	}
	plain, err := d.items.OpenPayload(record, "note-body")
	if err != nil {
		t.Fatalf("open note: %v", err)
	}
	return string(plain)
}

func (d *device) syncNow(t *testing.T) syncclient.Report {
	t.Helper()
	report, err := d.sync.SyncNow(context.Background())
	if err != nil {
		t.Fatalf("sync now: %v", err)
	}
	return report
}

func (d *device) account(t *testing.T) string {
	t.Helper()
	account, bound, err := d.remote.BoundAccountID(context.Background())
	if err != nil || !bound {
		t.Fatalf("bound account: bound=%v err=%v", bound, err)
	}
	return account
}

// setupConflict brings two devices to the post-conflict state: both edit the
// same note offline from v1, A wins the push, and B's next sync loses and
// captures theirs. It returns both devices, the shared account, and the note id.
func setupConflict(t *testing.T) (a, b *device, account, recordID string) {
	t.Helper()
	baseURL := newMockServer(t)
	vlt := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := vlt.Setup("master-password"); err != nil {
		t.Fatalf("vault setup: %v", err)
	}

	a = newDevice(t, baseURL, e2eToken, vlt)
	b = newDevice(t, baseURL, e2eToken, vlt)

	recordID = a.createNote(t, "Roadmap", "v1 body")
	a.syncNow(t) // push v1
	b.syncNow(t) // pull v1

	a.editNote(t, recordID, "A edit v2")
	b.editNote(t, recordID, "B edit v2")

	a.syncNow(t) // A wins v2
	report := b.syncNow(t)
	if report.PushConflicts < 1 {
		t.Fatalf("expected a push conflict for B, report = %#v", report)
	}
	account = b.account(t)
	if _, found, err := b.conflicts.Get(context.Background(), account, recordID); err != nil || !found {
		t.Fatalf("theirs not captured for B: found=%v err=%v", found, err)
	}
	if conflicts, err := b.sync.Conflicts(context.Background()); err != nil || len(conflicts) != 1 {
		t.Fatalf("B conflicts = %d err=%v, want 1", len(conflicts), err)
	}
	return a, b, account, recordID
}

func TestE2EResolveUseTheirsConverges(t *testing.T) {
	a, b, account, recordID := setupConflict(t)

	if err := b.resolver.ResolveUseTheirs(context.Background(), account, recordID); err != nil {
		t.Fatalf("resolve use theirs: %v", err)
	}
	b.syncNow(t)
	a.syncNow(t)
	b.syncNow(t)

	if got := b.body(t, recordID); got != "A edit v2" {
		t.Fatalf("B body = %q, want theirs", got)
	}
	if got := a.body(t, recordID); got != "A edit v2" {
		t.Fatalf("A body = %q, want theirs", got)
	}
	assertNoConflicts(t, b)
}

func TestE2EResolveUseOursConverges(t *testing.T) {
	a, b, account, recordID := setupConflict(t)

	resolveUseLocal(t, b, account, recordID, b.mustGet(t, recordID))
	b.syncNow(t) // push B's v3
	a.syncNow(t) // A pulls v3
	b.syncNow(t)

	if got := a.body(t, recordID); got != "B edit v2" {
		t.Fatalf("A body = %q, want ours", got)
	}
	if got := b.body(t, recordID); got != "B edit v2" {
		t.Fatalf("B body = %q, want ours", got)
	}
	assertNoConflicts(t, b)
}

func TestE2EResolveMergeConverges(t *testing.T) {
	a, b, account, recordID := setupConflict(t)

	merged, err := b.items.Reseal(b.mustGet(t, recordID), "note-body", []byte("MERGED: keep both ideas"))
	if err != nil {
		t.Fatalf("reseal merged: %v", err)
	}
	resolveUseLocal(t, b, account, recordID, merged)
	b.syncNow(t)
	a.syncNow(t)
	b.syncNow(t)

	if got := a.body(t, recordID); got != "MERGED: keep both ideas" {
		t.Fatalf("A body = %q, want merged", got)
	}
	if got := b.body(t, recordID); got != "MERGED: keep both ideas" {
		t.Fatalf("B body = %q, want merged", got)
	}
	assertNoConflicts(t, b)
}

func (d *device) mustGet(t *testing.T, id string) vaultitem.Record {
	t.Helper()
	record, err := d.items.Get(id)
	if err != nil {
		t.Fatalf("get note: %v", err)
	}
	return record
}

// resolveUseLocal mirrors the bridge's use-ours / edit-merge orchestration:
// re-materialise the chosen record as a new local mutation at remote_head + 1.
func resolveUseLocal(t *testing.T, d *device, account, recordID string, record vaultitem.Record) {
	t.Helper()
	recordJSON, snapshot, err := vaultitem.MarshalForResolution(record)
	if err != nil {
		t.Fatalf("marshal for resolution: %v", err)
	}
	operationID, err := replicationsync.NewOperationID()
	if err != nil {
		t.Fatalf("operation id: %v", err)
	}
	if err := d.resolver.ResolveUseLocal(context.Background(), account, recordID, storage.LocalResolution{
		OperationID: operationID,
		Operation:   replicationsync.OperationUpdate,
		KeyVersion:  uint32(record.KeyVersion),
		KeyID:       "vault-item",
		RecordJSON:  recordJSON,
		Snapshot:    snapshot,
		CreatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("resolve use local: %v", err)
	}
}

func assertNoConflicts(t *testing.T, d *device) {
	t.Helper()
	conflicts, err := d.sync.Conflicts(context.Background())
	if err != nil {
		t.Fatalf("list conflicts: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("conflicts remaining = %d, want 0", len(conflicts))
	}
}
