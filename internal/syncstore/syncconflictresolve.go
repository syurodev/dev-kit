package syncstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	replicationsync "devkit/internal/sync"
)

// ErrConflictNotFound means no captured conflict side exists for the record.
var ErrConflictNotFound = errors.New("no conflict to resolve for record")

// LocalResolution carries pre-sealed content for the "use ours" / "edit-merge"
// strategies: the caller (which holds the vault key) builds the new record and
// its sync snapshot; the resolver only writes them at remote_head + 1 in one
// account-bound transaction. Snapshot is the same encrypted snapshot the normal
// local-write path stores — never plaintext.
type LocalResolution struct {
	OperationID string
	Operation   replicationsync.Operation
	KeyVersion  uint32
	KeyID       string
	RecordJSON  []byte
	Snapshot    []byte
	CreatedAt   time.Time
}

// SQLConflictResolver converges a captured same-version conflict by an explicit
// user choice. Every strategy runs in a single transaction so a record can
// never be left both conflicted and dirty.
type SQLConflictResolver struct{ db *sql.DB }

func NewSQLConflictResolver(db *sql.DB) *SQLConflictResolver {
	return &SQLConflictResolver{db: db}
}

// ResolveUseTheirs adopts the captured remote snapshot as the winner: it
// replaces ours with theirs, advances the ledgers to the remote version and
// records the remote op as applied, then clears the conflict and the losing
// outbox push. No new push is needed; the device now matches the server.
func (r *SQLConflictResolver) ResolveUseTheirs(ctx context.Context, accountID, recordID string) (err error) {
	if r == nil || r.db == nil {
		return errors.New("conflict resolver is nil")
	}
	ctx = nonNilContext(ctx)
	if err := validateSyncIdentifier("account id", accountID, 256); err != nil {
		return err
	}
	if err := validateSyncIdentifier("record id", recordID, 256); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = bindAuthenticatedAccount(ctx, tx, accountID); err != nil {
		return err
	}

	conflict, err := loadConflictForUpdate(ctx, tx, accountID, recordID)
	if err != nil {
		return err
	}

	now := formatTime(time.Now().UTC())
	tombstone := 0
	if conflict.RemoteOperation == replicationsync.OperationDelete {
		tombstone = 1
		if _, err = tx.ExecContext(ctx, fmt.Sprintf(
			"DELETE FROM %s WHERE id = ? AND type = ?", TableVaultItems,
		), conflict.RecordID, conflict.RecordType); err != nil {
			return err
		}
	} else {
		if _, err = tx.ExecContext(ctx, fmt.Sprintf(`
			INSERT INTO %s (id, type, data) VALUES (?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET type = excluded.type, data = excluded.data`, TableVaultItems),
			conflict.RecordID, conflict.RecordType, conflict.TheirsSnapshot); err != nil {
			return err
		}
	}

	// Advance the account and local ledgers to the remote op so future pulls and
	// local edits build on theirs, and record the remote op as applied so a
	// replay is a no-op.
	if _, err = tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (
			account_id, record_id, record_type, entity_version, operation_id, operation, tombstone, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id, record_id) DO UPDATE SET
			record_type = excluded.record_type, entity_version = excluded.entity_version,
			operation_id = excluded.operation_id, operation = excluded.operation,
			tombstone = excluded.tombstone, updated_at = excluded.updated_at`, TableSyncEntityVersions),
		accountID, conflict.RecordID, conflict.RecordType, conflict.RemoteEntityVersion,
		conflict.RemoteOperationID, string(conflict.RemoteOperation), tombstone, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (
			record_id, record_type, entity_version, operation_id, operation, tombstone, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(record_id) DO UPDATE SET
			record_type = excluded.record_type, entity_version = excluded.entity_version,
			operation_id = excluded.operation_id, operation = excluded.operation,
			tombstone = excluded.tombstone, updated_at = excluded.updated_at`, TableSyncLocalEntities),
		conflict.RecordID, conflict.RecordType, conflict.RemoteEntityVersion,
		conflict.RemoteOperationID, string(conflict.RemoteOperation), tombstone, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT OR IGNORE INTO %s (
			account_id, operation_id, record_id, record_type, entity_version, operation, applied_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)`, TableSyncAppliedOps),
		accountID, conflict.RemoteOperationID, conflict.RecordID, conflict.RecordType,
		conflict.RemoteEntityVersion, string(conflict.RemoteOperation), now); err != nil {
		return err
	}

	if err = clearConflict(ctx, tx, accountID, recordID, conflict.LocalChangeKey); err != nil {
		return err
	}
	return tx.Commit()
}

// ResolveUseLocal keeps the chosen local content ("use ours" or an edited
// merge) by re-materialising it as a new local mutation at remote_head + 1, so
// the normal outbox flow pushes it and both devices converge on it. The losing
// outbox push and the conflict side are cleared in the same transaction.
func (r *SQLConflictResolver) ResolveUseLocal(ctx context.Context, accountID, recordID string, res LocalResolution) (err error) {
	if r == nil || r.db == nil {
		return errors.New("conflict resolver is nil")
	}
	ctx = nonNilContext(ctx)
	if err := validateSyncIdentifier("account id", accountID, 256); err != nil {
		return err
	}
	if err := validateSyncIdentifier("record id", recordID, 256); err != nil {
		return err
	}
	if err := validateSyncIdentifier("operation id", res.OperationID, 256); err != nil {
		return err
	}
	if res.Operation != replicationsync.OperationUpdate && res.Operation != replicationsync.OperationDelete {
		return errors.New("unsupported resolution operation")
	}
	if res.KeyVersion == 0 {
		return errors.New("resolution key version must be positive")
	}
	if len(res.Snapshot) == 0 || len(res.Snapshot) > replicationsync.MaxRecordSnapshotBytes {
		return errors.New("resolution snapshot size is invalid")
	}
	if res.Operation == replicationsync.OperationUpdate && len(res.RecordJSON) == 0 {
		return errors.New("resolution record is required")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = bindAuthenticatedAccount(ctx, tx, accountID); err != nil {
		return err
	}

	conflict, err := loadConflictForUpdate(ctx, tx, accountID, recordID)
	if err != nil {
		return err
	}

	version, err := resolveNextVersion(ctx, tx, accountID, recordID, conflict.RecordType)
	if err != nil {
		return err
	}

	now := formatTime(time.Now().UTC())
	tombstone := 0
	if res.Operation == replicationsync.OperationDelete {
		tombstone = 1
		if _, err = tx.ExecContext(ctx, fmt.Sprintf(
			"DELETE FROM %s WHERE id = ? AND type = ?", TableVaultItems,
		), conflict.RecordID, conflict.RecordType); err != nil {
			return err
		}
	} else {
		if _, err = tx.ExecContext(ctx, fmt.Sprintf(`
			INSERT INTO %s (id, type, data) VALUES (?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET type = excluded.type, data = excluded.data
			WHERE %s.type = excluded.type`, TableVaultItems, TableVaultItems),
			conflict.RecordID, conflict.RecordType, res.RecordJSON); err != nil {
			return err
		}
	}

	if _, err = tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (
			record_id, record_type, entity_version, operation_id, operation, tombstone, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(record_id) DO UPDATE SET
			record_type = excluded.record_type, entity_version = excluded.entity_version,
			operation_id = excluded.operation_id, operation = excluded.operation,
			tombstone = excluded.tombstone, updated_at = excluded.updated_at`, TableSyncLocalEntities),
		conflict.RecordID, conflict.RecordType, version, res.OperationID, string(res.Operation), tombstone, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (
			operation_id, record_id, record_type, entity_version, operation,
			snapshot, key_version, key_id, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, TableSyncLocalMutations),
		res.OperationID, conflict.RecordID, conflict.RecordType, version, string(res.Operation),
		res.Snapshot, res.KeyVersion, res.KeyID, formatTime(res.CreatedAt.UTC())); err != nil {
		return err
	}

	if err = clearConflict(ctx, tx, accountID, recordID, conflict.LocalChangeKey); err != nil {
		return err
	}
	return tx.Commit()
}

// loadConflictForUpdate reads the captured conflict row inside the resolution
// transaction, returning ErrConflictNotFound when there is nothing to resolve.
func loadConflictForUpdate(ctx context.Context, tx *sql.Tx, accountID, recordID string) (ConflictRecord, error) {
	row := tx.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT account_id, record_id, record_type, remote_operation_id, remote_operation,
			remote_entity_version, theirs_snapshot, local_change_key, detected_at
		FROM %s WHERE account_id = ? AND record_id = ?`, TableSyncConflicts), accountID, recordID)
	record, err := scanConflictRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ConflictRecord{}, ErrConflictNotFound
	}
	if err != nil {
		return ConflictRecord{}, err
	}
	return record, nil
}

// resolveNextVersion returns remote_head + 1, tolerating a higher local head so
// the new mutation always supersedes both sides.
func resolveNextVersion(ctx context.Context, tx *sql.Tx, accountID, recordID, recordType string) (uint32, error) {
	head := uint32(0)
	read := func(query string, args ...any) error {
		var storedType string
		var version int64
		err := tx.QueryRowContext(ctx, query, args...).Scan(&storedType, &version)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if storedType != recordType || version <= 0 || version >= int64(^uint32(0)) {
			return errors.New("stored entity version is invalid")
		}
		if uint32(version) > head {
			head = uint32(version)
		}
		return nil
	}
	if err := read(fmt.Sprintf("SELECT record_type, entity_version FROM %s WHERE account_id = ? AND record_id = ?", TableSyncEntityVersions), accountID, recordID); err != nil {
		return 0, err
	}
	if err := read(fmt.Sprintf("SELECT record_type, entity_version FROM %s WHERE record_id = ?", TableSyncLocalEntities), recordID); err != nil {
		return 0, err
	}
	if head == 0 {
		return 0, errors.New("cannot resolve a record with no version history")
	}
	return head + 1, nil
}

// clearConflict removes the conflict side and the losing outbox push.
func clearConflict(ctx context.Context, tx *sql.Tx, accountID, recordID, localChangeKey string) error {
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(
		"DELETE FROM %s WHERE account_id = ? AND record_id = ?", TableSyncConflicts,
	), accountID, recordID); err != nil {
		return err
	}
	if localChangeKey != "" {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(
			"DELETE FROM %s WHERE idempotency_key = ?", TableSyncOutbox,
		), localChangeKey); err != nil {
			return err
		}
	}
	return nil
}
