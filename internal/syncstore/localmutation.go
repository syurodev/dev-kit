package syncstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	replicationsync "devkit/internal/sync"
)

// LocalMutationIntent is an account-neutral local mutation captured in the
// same transaction as vault_items. Snapshot contains only plaintext metadata
// already present locally plus the module's encrypted inner payload.
type LocalMutationIntent struct {
	Sequence      int64
	OperationID   string
	RecordID      string
	RecordType    string
	EntityVersion uint32
	Operation     replicationsync.Operation
	Snapshot      []byte
	KeyVersion    uint32
	KeyID         string
	CreatedAt     time.Time
}

// SQLLocalMutationStore exposes durable intents to the authenticated sync
// client. Sequence preserves transaction order across equal timestamps.
type SQLLocalMutationStore struct{ db *sql.DB }

func NewSQLLocalMutationStore(db *sql.DB) *SQLLocalMutationStore {
	return &SQLLocalMutationStore{db: db}
}

func (s *SQLLocalMutationStore) Pending(ctx context.Context, limit int) ([]LocalMutationIntent, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("local mutation store is nil")
	}
	ctx = nonNilContext(ctx)
	if limit <= 0 || limit > maxRemoteBatchSize {
		return nil, errors.New("local mutation limit is invalid")
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT sequence, operation_id, record_id, record_type, entity_version,
			operation,
			CASE WHEN length(snapshot) <= ? THEN snapshot ELSE NULL END,
			key_version, key_id, created_at
		FROM %s ORDER BY sequence ASC LIMIT ?`, TableSyncLocalMutations),
		replicationsync.MaxRecordSnapshotBytes, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	intents := make([]LocalMutationIntent, 0)
	for rows.Next() {
		var intent LocalMutationIntent
		var version, keyVersion int64
		var operation, createdAt string
		if err := rows.Scan(&intent.Sequence, &intent.OperationID, &intent.RecordID, &intent.RecordType,
			&version, &operation, &intent.Snapshot, &keyVersion, &intent.KeyID, &createdAt); err != nil {
			return nil, err
		}
		if intent.Snapshot == nil {
			return nil, errors.New("stored local mutation snapshot is oversized")
		}
		if version <= 0 || version > int64(^uint32(0)) || keyVersion <= 0 || keyVersion > int64(^uint32(0)) {
			return nil, errors.New("stored local mutation version is invalid")
		}
		intent.EntityVersion = uint32(version)
		intent.KeyVersion = uint32(keyVersion)
		intent.Operation = replicationsync.Operation(operation)
		intent.CreatedAt, err = time.Parse(syncTimeLayout, createdAt)
		if err != nil {
			return nil, errors.New("stored local mutation time is invalid")
		}
		intent.Snapshot = append([]byte(nil), intent.Snapshot...)
		intents = append(intents, intent)
	}
	return intents, rows.Err()
}

// CommitMaterialized verifies that QueuePayload durably inserted the matching
// encrypted outbox row, advances the account-bound entity head, then removes
// the account-neutral intent in one transaction. A crash before this method is
// safe: QueuePayload retries by operation ID and authenticates the ciphertext.
func (s *SQLLocalMutationStore) CommitMaterialized(
	ctx context.Context,
	accountID string,
	intent LocalMutationIntent,
	change replicationsync.Change,
) (err error) {
	if s == nil || s.db == nil {
		return errors.New("local mutation store is nil")
	}
	ctx = nonNilContext(ctx)
	if err := validateSyncIdentifier("account id", accountID, 256); err != nil {
		return err
	}
	if err := change.Validate(); err != nil {
		return err
	}
	if change.AccountID != accountID || change.OperationID != intent.OperationID ||
		change.RecordID != intent.RecordID || change.RecordType != intent.RecordType ||
		change.EntityVersion != intent.EntityVersion || change.Operation != intent.Operation ||
		change.KeyVersion != intent.KeyVersion || change.KeyID != intent.KeyID {
		return errors.New("materialized change does not match local mutation")
	}

	tx, err := s.db.BeginTx(ctx, nil)
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

	var (
		storedAccount, storedOperationID, storedRecordID, storedRecordType string
		storedOperation, storedKeyID                                       string
		storedVersion, storedKeyVersion                                    int64
	)
	if err = tx.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT account_id, operation_id, record_id, record_type, entity_version,
			operation, key_version, key_id
		FROM %s WHERE idempotency_key = ?`, TableSyncOutbox), change.IdempotencyKey).
		Scan(&storedAccount, &storedOperationID, &storedRecordID, &storedRecordType,
			&storedVersion, &storedOperation, &storedKeyVersion, &storedKeyID); err != nil {
		return err
	}
	if storedAccount != accountID || storedOperationID != intent.OperationID ||
		storedRecordID != intent.RecordID || storedRecordType != intent.RecordType ||
		storedVersion != int64(intent.EntityVersion) || storedOperation != string(intent.Operation) ||
		storedKeyVersion != int64(intent.KeyVersion) || storedKeyID != intent.KeyID {
		return errors.New("stored outbox row does not match local mutation")
	}

	var currentType, currentOperationID, currentOperation string
	var currentVersion int64
	var currentTombstone int
	loadErr := tx.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT record_type, entity_version, operation_id, operation, tombstone
		FROM %s WHERE account_id = ? AND record_id = ?`, TableSyncEntityVersions),
		accountID, intent.RecordID).
		Scan(&currentType, &currentVersion, &currentOperationID, &currentOperation, &currentTombstone)
	if loadErr == nil {
		if currentType != intent.RecordType || currentVersion >= int64(intent.EntityVersion) {
			return ErrRemoteMutationConflict
		}
	} else if !errors.Is(loadErr, sql.ErrNoRows) {
		return loadErr
	}
	tombstone := 0
	if intent.Operation == replicationsync.OperationDelete {
		tombstone = 1
	}
	if _, err = tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (
			account_id, record_id, record_type, entity_version, operation_id, operation, tombstone, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id, record_id) DO UPDATE SET
			record_type=excluded.record_type, entity_version=excluded.entity_version,
			operation_id=excluded.operation_id, operation=excluded.operation,
			tombstone=excluded.tombstone, updated_at=excluded.updated_at`, TableSyncEntityVersions),
		accountID, intent.RecordID, intent.RecordType, intent.EntityVersion,
		intent.OperationID, string(intent.Operation), tombstone, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, fmt.Sprintf(
		"DELETE FROM %s WHERE operation_id = ? AND record_id = ? AND entity_version = ?",
		TableSyncLocalMutations,
	), intent.OperationID, intent.RecordID, intent.EntityVersion)
	if err != nil {
		return err
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
		return errors.New("local mutation intent changed during materialization")
	}
	return tx.Commit()
}
