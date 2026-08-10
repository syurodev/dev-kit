package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"changeme/internal/storage"
	"changeme/internal/syncclient"
	"changeme/internal/syncstore"
	"changeme/internal/vault"
	"changeme/internal/vaultitem"
)

func TestHarnessCompiles(t *testing.T) {}

func requireDeployed(t *testing.T) (baseURL, token string) {
	t.Helper()
	baseURL = os.Getenv("DEVKIT_DEPLOYED_E2E_URL")
	token = os.Getenv("DEVKIT_DEPLOYED_E2E_TOKEN")
	if baseURL == "" || token == "" {
		t.Skip("set DEVKIT_DEPLOYED_E2E_URL and DEVKIT_DEPLOYED_E2E_TOKEN to run deployed matrix")
	}
	return baseURL, token
}

func newSharedVault(t *testing.T) *vault.Service {
	t.Helper()
	vlt := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := vlt.Setup("master-password"); err != nil {
		t.Fatalf("vault setup: %v", err)
	}
	return vlt
}

type device struct {
	repo      *vaultitem.SQLMutationRepository
	items     *vaultitem.Store
	sync      *syncclient.Service
	resolver  *syncstore.SQLConflictResolver
	conflicts *syncstore.SQLConflictStore
	remote    *syncstore.SQLRemoteStore
}

func newDevice(t *testing.T, baseURL, token, enrollmentToken string, vlt *vault.Service) *device {
	t.Helper()
	_ = enrollmentToken
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
		DeviceIdentity: syncstore.NewSQLDeviceIdentityStore(db),
		Cursors:        syncstore.NewSQLCursorStore(db),
		Remote:         syncstore.NewSQLRemoteStore(db),
		LocalMutations: syncstore.NewSQLLocalMutationStore(db),
		Outbox:         syncstore.NewSQLOutbox(db),
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
		resolver:  syncstore.NewSQLConflictResolver(db),
		conflicts: syncstore.NewSQLConflictStore(db),
		remote:    syncstore.NewSQLRemoteStore(db),
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
