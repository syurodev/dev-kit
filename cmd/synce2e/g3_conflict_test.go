package main

import (
	"context"
	"testing"
	"time"

	replicationsync "devkit/internal/sync"
	"devkit/internal/syncstore"
	"devkit/internal/vaultitem"
)

// setupConflict brings two devices to the post-conflict state: both edit the
// same note offline from v1, A wins the push, and B's next sync loses and
// captures theirs. It returns both devices, the shared account, and the note id.
func setupConflict(t *testing.T) (a, b *device, account, recordID string) {
	t.Helper()
	baseURL, token := requireDeployed(t)
	resetDeployedSyncState(t)
	vlt := newSharedVault(t)

	a = newBootstrappedDevice(t, baseURL, token, vlt)
	b = newEnrolledDevice(t, baseURL, token, a, vlt)

	recordID = a.createNote(t, "Roadmap", "v1 body")
	a.syncNow(t) // push v1
	b.syncNow(t) // pull v1

	a.editNote(t, recordID, "A edit v2")
	b.editNote(t, recordID, "B edit v2")

	a.syncNow(t) // A wins v2
	report := b.syncNow(t)
	if report.PushConflicts < 1 {
		t.Fatalf("G3: expected a push conflict for B, report = %#v", report)
	}
	account = b.account(t)
	if _, found, err := b.conflicts.Get(context.Background(), account, recordID); err != nil || !found {
		t.Fatalf("G3: theirs not captured for B: found=%v err=%v", found, err)
	}
	if conflicts, err := b.sync.Conflicts(context.Background()); err != nil || len(conflicts) != 1 {
		t.Fatalf("G3: B conflicts = %d err=%v, want 1", len(conflicts), err)
	}
	return a, b, account, recordID
}

func TestG3ResolveUseTheirsConverges(t *testing.T) {
	a, b, account, recordID := setupConflict(t)

	if err := b.resolver.ResolveUseTheirs(context.Background(), account, recordID); err != nil {
		t.Fatalf("G3: resolve use theirs: %v", err)
	}
	b.syncNow(t)
	a.syncNow(t)
	b.syncNow(t)

	if got := b.body(t, recordID); got != "A edit v2" {
		t.Fatalf("G3: B body = %q, want theirs", got)
	}
	if got := a.body(t, recordID); got != "A edit v2" {
		t.Fatalf("G3: A body = %q, want theirs", got)
	}
	assertNoConflicts(t, b)
}

func TestG3ResolveUseOursConverges(t *testing.T) {
	a, b, account, recordID := setupConflict(t)

	resolveUseLocal(t, b, account, recordID, b.mustGet(t, recordID))
	b.syncNow(t) // push B's v3
	a.syncNow(t) // A pulls v3
	b.syncNow(t)

	if got := a.body(t, recordID); got != "B edit v2" {
		t.Fatalf("G3: A body = %q, want ours", got)
	}
	if got := b.body(t, recordID); got != "B edit v2" {
		t.Fatalf("G3: B body = %q, want ours", got)
	}
	assertNoConflicts(t, b)
}

func TestG3ResolveMergeConverges(t *testing.T) {
	a, b, account, recordID := setupConflict(t)

	merged, err := b.items.Reseal(b.mustGet(t, recordID), "note-body", []byte("MERGED: keep both ideas"))
	if err != nil {
		t.Fatalf("G3: reseal merged: %v", err)
	}
	resolveUseLocal(t, b, account, recordID, merged)
	b.syncNow(t)
	a.syncNow(t)
	b.syncNow(t)

	if got := a.body(t, recordID); got != "MERGED: keep both ideas" {
		t.Fatalf("G3: A body = %q, want merged", got)
	}
	if got := b.body(t, recordID); got != "MERGED: keep both ideas" {
		t.Fatalf("G3: B body = %q, want merged", got)
	}
	assertNoConflicts(t, b)
}

func (d *device) mustGet(t *testing.T, id string) vaultitem.Record {
	t.Helper()
	record, err := d.items.Get(id)
	if err != nil {
		t.Fatalf("G3: get note: %v", err)
	}
	return record
}

// resolveUseLocal mirrors the bridge's use-ours / edit-merge orchestration:
// re-materialise the chosen record as a new local mutation at remote_head + 1.
func resolveUseLocal(t *testing.T, d *device, account, recordID string, record vaultitem.Record) {
	t.Helper()
	recordJSON, snapshot, err := vaultitem.MarshalForResolution(record)
	if err != nil {
		t.Fatalf("G3: marshal for resolution: %v", err)
	}
	operationID, err := replicationsync.NewOperationID()
	if err != nil {
		t.Fatalf("G3: operation id: %v", err)
	}
	if err := d.resolver.ResolveUseLocal(context.Background(), account, recordID, syncstore.LocalResolution{
		OperationID: operationID,
		Operation:   replicationsync.OperationUpdate,
		KeyVersion:  uint32(record.KeyVersion),
		KeyID:       "vault-item",
		RecordJSON:  recordJSON,
		Snapshot:    snapshot,
		CreatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("G3: resolve use local: %v", err)
	}
}

func assertNoConflicts(t *testing.T, d *device) {
	t.Helper()
	conflicts, err := d.sync.Conflicts(context.Background())
	if err != nil {
		t.Fatalf("G3: list conflicts: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("G3: conflicts remaining = %d, want 0", len(conflicts))
	}
}
