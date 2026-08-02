package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	replicationsync "changeme/internal/sync"
)

const testRemoteAccountID = "account-1"

func newTestRemoteStore(t *testing.T) (*sql.DB, *SQLRemoteStore) {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db, NewSQLRemoteStore(db)
}

func remoteRecordJSON(id, recordType, marker string) []byte {
	return []byte(fmt.Sprintf(`{"id":%q,"type":%q,"metadata":{"marker":%q},"sync_state":"synced"}`, id, recordType, marker))
}

func remoteMutation(id string, operation replicationsync.Operation, version uint32) RemoteMutation {
	mutation := RemoteMutation{
		AccountID:     testRemoteAccountID,
		RecordID:      id,
		RecordType:    "note",
		Operation:     operation,
		OperationID:   fmt.Sprintf("operation-%s-%d", id, version),
		EntityVersion: version,
	}
	if operation != replicationsync.OperationDelete {
		mutation.RecordJSON = remoteRecordJSON(id, mutation.RecordType, mutation.OperationID)
	}
	return mutation
}

func TestApplyRemoteBatchBindsAuthenticatedAccountAndRejectsMismatch(t *testing.T) {
	db, store := newTestRemoteStore(t)
	mutation := remoteMutation("record-1", replicationsync.OperationCreate, 3)
	if err := store.ApplyRemoteBatch(context.Background(), testRemoteAccountID, "pull", "cursor-1", []RemoteMutation{mutation}); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	var boundAccount, deviceID string
	if err := db.QueryRow("SELECT account_id FROM sync_account_binding WHERE id = 'local'").Scan(&boundAccount); err != nil {
		t.Fatalf("read account binding: %v", err)
	}
	if boundAccount != testRemoteAccountID {
		t.Fatalf("bound account = %q, want %q", boundAccount, testRemoteAccountID)
	}
	if err := db.QueryRow("SELECT device_id FROM sync_identity WHERE id = 'local'").Scan(&deviceID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("account binding must not create device identity; device=%q err=%v", deviceID, err)
	}

	other := remoteMutation("record-2", replicationsync.OperationCreate, 1)
	other.AccountID = "account-2"
	err := store.ApplyRemoteBatch(context.Background(), "account-2", "pull", "cursor-2", []RemoteMutation{other})
	if !errors.Is(err, ErrSyncAccountMismatch) {
		t.Fatalf("account mismatch error = %v, want %v", err, ErrSyncAccountMismatch)
	}
	assertCursor(t, db, "pull", "cursor-1")
}

func TestBindAuthenticatedAccountIsIdempotentAndRejectsSwitch(t *testing.T) {
	db, store := newTestRemoteStore(t)
	ctx := context.Background()
	if err := store.BindAuthenticatedAccount(ctx, testRemoteAccountID); err != nil {
		t.Fatal(err)
	}
	if err := store.BindAuthenticatedAccount(ctx, testRemoteAccountID); err != nil {
		t.Fatalf("idempotent account bind: %v", err)
	}
	if err := store.BindAuthenticatedAccount(ctx, "account-2"); !errors.Is(err, ErrSyncAccountMismatch) {
		t.Fatalf("different account bind error = %v, want %v", err, ErrSyncAccountMismatch)
	}
	var accountID string
	if err := db.QueryRow("SELECT account_id FROM sync_account_binding WHERE id = 'local'").Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if accountID != testRemoteAccountID {
		t.Fatalf("bound account = %q", accountID)
	}
}

func TestApplyRemoteBatchExactReplayIsNoOp(t *testing.T) {
	db, store := newTestRemoteStore(t)
	mutation := remoteMutation("record-1", replicationsync.OperationUpdate, 7)
	if err := store.ApplyRemoteBatch(context.Background(), testRemoteAccountID, "pull", "cursor-1", []RemoteMutation{mutation}); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	replay := mutation
	replay.RecordJSON = remoteRecordJSON(replay.RecordID, replay.RecordType, "tampered-replay-body")
	if err := store.ApplyRemoteBatch(context.Background(), testRemoteAccountID, "pull", "cursor-2", []RemoteMutation{replay}); err != nil {
		t.Fatalf("exact replay: %v", err)
	}
	assertVaultData(t, db, mutation.RecordID, mutation.RecordJSON)
	assertCursor(t, db, "pull", "cursor-2")
}

func TestApplyRemoteBatchAcceptsVersionGapAndRejectsStaleOrEqual(t *testing.T) {
	db, store := newTestRemoteStore(t)
	first := remoteMutation("record-1", replicationsync.OperationUpdate, 4)
	if err := store.ApplyRemoteBatch(context.Background(), testRemoteAccountID, "pull", "cursor-4", []RemoteMutation{first}); err != nil {
		t.Fatalf("first compacted snapshot: %v", err)
	}
	gap := remoteMutation("record-1", replicationsync.OperationUpdate, 11)
	if err := store.ApplyRemoteBatch(context.Background(), testRemoteAccountID, "pull", "cursor-11", []RemoteMutation{gap}); err != nil {
		t.Fatalf("version gap should be accepted: %v", err)
	}

	for _, version := range []uint32{10, 11} {
		stale := remoteMutation("record-1", replicationsync.OperationUpdate, version)
		stale.OperationID = fmt.Sprintf("different-operation-%d", version)
		err := store.ApplyRemoteBatch(context.Background(), testRemoteAccountID, "pull", "bad-cursor", []RemoteMutation{stale})
		if !errors.Is(err, ErrRemoteMutationConflict) {
			t.Fatalf("version %d error = %v, want conflict", version, err)
		}
	}
	assertVaultData(t, db, gap.RecordID, gap.RecordJSON)
	assertCursor(t, db, "pull", "cursor-11")
}

func TestApplyRemoteBatchRejectsOverwriteOfUnsyncedLocalEdit(t *testing.T) {
	db, store := newTestRemoteStore(t)
	first := remoteMutation("record-local", replicationsync.OperationCreate, 1)
	if err := store.ApplyRemoteBatch(context.Background(), testRemoteAccountID, "pull", "cursor-1", []RemoteMutation{first}); err != nil {
		t.Fatal(err)
	}
	localJSON := remoteRecordJSON(first.RecordID, first.RecordType, "local-edit")
	var local map[string]any
	if err := json.Unmarshal(localJSON, &local); err != nil {
		t.Fatal(err)
	}
	local["sync_state"] = "local"
	localJSON, _ = json.Marshal(local)
	if _, err := db.Exec("UPDATE vault_items SET data = ? WHERE id = ?", localJSON, first.RecordID); err != nil {
		t.Fatal(err)
	}

	remote := remoteMutation(first.RecordID, replicationsync.OperationUpdate, 2)
	if err := store.ApplyRemoteBatch(context.Background(), testRemoteAccountID, "pull", "cursor-2", []RemoteMutation{remote}); !errors.Is(err, ErrRemoteMutationConflict) {
		t.Fatalf("remote overwrite error = %v, want conflict", err)
	}
	assertVaultData(t, db, first.RecordID, localJSON)
	assertCursor(t, db, "pull", "cursor-1")
}

func TestApplyRemoteBatchDoesNotResurrectLocallyDeletedRecord(t *testing.T) {
	db, store := newTestRemoteStore(t)
	first := remoteMutation("record-deleted", replicationsync.OperationCreate, 1)
	if err := store.ApplyRemoteBatch(context.Background(), testRemoteAccountID, "pull", "cursor-1", []RemoteMutation{first}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM vault_items WHERE id = ?", first.RecordID); err != nil {
		t.Fatal(err)
	}
	remote := remoteMutation(first.RecordID, replicationsync.OperationUpdate, 2)
	if err := store.ApplyRemoteBatch(context.Background(), testRemoteAccountID, "pull", "cursor-2", []RemoteMutation{remote}); !errors.Is(err, ErrRemoteMutationConflict) {
		t.Fatalf("remote resurrection error = %v, want conflict", err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM vault_items WHERE id = ?", first.RecordID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("remote update resurrected locally deleted record")
	}
	assertCursor(t, db, "pull", "cursor-1")
}

func TestApplyRemoteBatchHistoricalReplayDoesNotDowngradeAndOperationIDCannotBeReused(t *testing.T) {
	db, store := newTestRemoteStore(t)
	version4 := remoteMutation("record-1", replicationsync.OperationUpdate, 4)
	if err := store.ApplyRemoteBatch(context.Background(), testRemoteAccountID, "pull", "cursor-4", []RemoteMutation{version4}); err != nil {
		t.Fatalf("apply version 4: %v", err)
	}
	version11 := remoteMutation("record-1", replicationsync.OperationUpdate, 11)
	if err := store.ApplyRemoteBatch(context.Background(), testRemoteAccountID, "pull", "cursor-11", []RemoteMutation{version11}); err != nil {
		t.Fatalf("apply version 11: %v", err)
	}

	replayVersion4 := version4
	replayVersion4.RecordJSON = remoteRecordJSON(version4.RecordID, version4.RecordType, "changed-replay-content")
	if err := store.ApplyRemoteBatch(context.Background(), testRemoteAccountID, "pull", "cursor-replay", []RemoteMutation{replayVersion4}); err != nil {
		t.Fatalf("historical exact replay: %v", err)
	}
	assertVaultData(t, db, version11.RecordID, version11.RecordJSON)
	assertCursor(t, db, "pull", "cursor-replay")

	var currentVersion uint32
	var currentOperationID string
	if err := db.QueryRow(`SELECT entity_version, operation_id FROM sync_entity_versions
		WHERE account_id = ? AND record_id = ?`, testRemoteAccountID, version11.RecordID).
		Scan(&currentVersion, &currentOperationID); err != nil {
		t.Fatalf("read current entity version: %v", err)
	}
	if currentVersion != version11.EntityVersion || currentOperationID != version11.OperationID {
		t.Fatalf("current ledger = version %d operation %q, want %d/%q",
			currentVersion, currentOperationID, version11.EntityVersion, version11.OperationID)
	}
	var appliedCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sync_applied_operations
		WHERE account_id = ? AND record_id = ?`, testRemoteAccountID, version11.RecordID).Scan(&appliedCount); err != nil {
		t.Fatalf("count applied operations: %v", err)
	}
	if appliedCount != 2 {
		t.Fatalf("applied operation count = %d, want 2", appliedCount)
	}

	reused := remoteMutation("record-2", replicationsync.OperationCreate, 20)
	reused.OperationID = version4.OperationID
	err := store.ApplyRemoteBatch(context.Background(), testRemoteAccountID, "pull", "cursor-reused", []RemoteMutation{reused})
	if !errors.Is(err, ErrRemoteMutationConflict) {
		t.Fatalf("reused historical operation ID error = %v, want conflict", err)
	}
	assertCursor(t, db, "pull", "cursor-replay")
	var reusedRecordCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM vault_items WHERE id = ?", reused.RecordID).Scan(&reusedRecordCount); err != nil {
		t.Fatalf("count reused-operation record: %v", err)
	}
	if reusedRecordCount != 0 {
		t.Fatal("reused historical operation ID changed vault items")
	}
}

func TestApplyRemoteBatchRejectsTamperedRecordIdentity(t *testing.T) {
	_, store := newTestRemoteStore(t)
	for _, test := range []struct {
		name       string
		recordJSON []byte
	}{
		{name: "id", recordJSON: remoteRecordJSON("other-id", "note", "x")},
		{name: "type", recordJSON: remoteRecordJSON("record-1", "db-profile", "x")},
	} {
		t.Run(test.name, func(t *testing.T) {
			mutation := remoteMutation("record-1", replicationsync.OperationCreate, 1)
			mutation.RecordJSON = test.recordJSON
			if err := store.ApplyRemoteBatch(context.Background(), testRemoteAccountID, "pull", "cursor", []RemoteMutation{mutation}); err == nil {
				t.Fatal("tampered record identity was accepted")
			}
		})
	}
}

func TestApplyRemoteBatchRollsBackEntireBatchAndCursor(t *testing.T) {
	db, store := newTestRemoteStore(t)
	seed := remoteMutation("existing", replicationsync.OperationCreate, 5)
	if err := store.ApplyRemoteBatch(context.Background(), testRemoteAccountID, "pull", "cursor-before", []RemoteMutation{seed}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	valid := remoteMutation("new-record", replicationsync.OperationCreate, 1)
	stale := remoteMutation("existing", replicationsync.OperationUpdate, 4)
	err := store.ApplyRemoteBatch(context.Background(), testRemoteAccountID, "pull", "cursor-after", []RemoteMutation{valid, stale})
	if !errors.Is(err, ErrRemoteMutationConflict) {
		t.Fatalf("batch error = %v, want conflict", err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM vault_items WHERE id = ?", valid.RecordID).Scan(&count); err != nil {
		t.Fatalf("count rolled-back item: %v", err)
	}
	if count != 0 {
		t.Fatal("first mutation was not rolled back")
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM sync_entity_versions WHERE record_id = ?", valid.RecordID).Scan(&count); err != nil {
		t.Fatalf("count rolled-back ledger: %v", err)
	}
	if count != 0 {
		t.Fatal("first ledger mutation was not rolled back")
	}
	assertCursor(t, db, "pull", "cursor-before")
}

func TestApplyRemoteBatchDeleteKeepsTombstone(t *testing.T) {
	db, store := newTestRemoteStore(t)
	create := remoteMutation("record-1", replicationsync.OperationCreate, 2)
	if err := store.ApplyRemoteBatch(context.Background(), testRemoteAccountID, "pull", "cursor-2", []RemoteMutation{create}); err != nil {
		t.Fatalf("create: %v", err)
	}
	deleteMutation := remoteMutation("record-1", replicationsync.OperationDelete, 9)
	if err := store.ApplyRemoteBatch(context.Background(), testRemoteAccountID, "pull", "cursor-9", []RemoteMutation{deleteMutation}); err != nil {
		t.Fatalf("delete: %v", err)
	}

	var itemCount, tombstone int
	var version uint32
	if err := db.QueryRow("SELECT COUNT(*) FROM vault_items WHERE id = ?", create.RecordID).Scan(&itemCount); err != nil {
		t.Fatalf("count item: %v", err)
	}
	if itemCount != 0 {
		t.Fatal("deleted vault item still exists")
	}
	if err := db.QueryRow(`SELECT entity_version, tombstone FROM sync_entity_versions
		WHERE account_id = ? AND record_id = ?`, testRemoteAccountID, create.RecordID).Scan(&version, &tombstone); err != nil {
		t.Fatalf("read tombstone: %v", err)
	}
	if version != 9 || tombstone != 1 {
		t.Fatalf("ledger = version %d tombstone %d, want 9/1", version, tombstone)
	}
}

func TestApplyRemoteBatchHonorsCancellation(t *testing.T) {
	db, store := newTestRemoteStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := store.ApplyRemoteBatch(ctx, testRemoteAccountID, "pull", "cursor", []RemoteMutation{
		remoteMutation("record-1", replicationsync.OperationCreate, 1),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled apply error = %v, want %v", err, context.Canceled)
	}
	for _, table := range []string{
		TableSyncAccountBinding, TableSyncEntityVersions, TableSyncAppliedOps,
		TableVaultItems, TableSyncCursors,
	} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s changed after cancellation", table)
		}
	}
}

func TestApplyRemoteBatchValidatesBoundsAndControlCharacters(t *testing.T) {
	_, store := newTestRemoteStore(t)
	mutation := remoteMutation("record-1", replicationsync.OperationCreate, 1)
	mutation.OperationID = "bad\noperation"
	if err := store.ApplyRemoteBatch(context.Background(), testRemoteAccountID, "pull", "cursor", []RemoteMutation{mutation}); err == nil {
		t.Fatal("control character was accepted")
	}
	mutation = remoteMutation("record-1", replicationsync.OperationCreate, 1)
	mutation.RecordJSON = []byte(`{"id":"record-1","type":"note","padding":"` + strings.Repeat("x", maxRemoteRecordJSONBytes) + `"}`)
	if err := store.ApplyRemoteBatch(context.Background(), testRemoteAccountID, "pull", "cursor", []RemoteMutation{mutation}); err == nil {
		t.Fatal("oversized record JSON was accepted")
	}
	if err := store.ApplyRemoteBatch(context.Background(), testRemoteAccountID, "pull", "bad\ncursor", nil); err == nil {
		t.Fatal("cursor control character was accepted")
	}
}

func assertCursor(t *testing.T, db *sql.DB, name, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow("SELECT cursor FROM sync_cursors WHERE name = ?", name).Scan(&got); err != nil {
		t.Fatalf("read cursor %q: %v", name, err)
	}
	if got != want {
		t.Fatalf("cursor %q = %q, want %q", name, got, want)
	}
}

func assertVaultData(t *testing.T, db *sql.DB, id string, want []byte) {
	t.Helper()
	var got []byte
	if err := db.QueryRow("SELECT data FROM vault_items WHERE id = ?", id).Scan(&got); err != nil {
		t.Fatalf("read vault item %q: %v", id, err)
	}
	if string(got) != string(want) {
		t.Fatalf("vault item data = %s, want %s", got, want)
	}
}
