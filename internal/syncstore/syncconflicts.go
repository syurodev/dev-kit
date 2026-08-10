package syncstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	replicationsync "devkit/internal/sync"
)

// ConflictRecord is the captured "theirs" side of a per-record sync conflict:
// the authenticated remote snapshot that lost the same-version race, kept next
// to the untouched local record ("ours") until the user resolves it. The
// snapshot is the same encrypted record JSON stored in vault_items — never a
// plaintext secret.
type ConflictRecord struct {
	AccountID           string
	RecordID            string
	RecordType          string
	RemoteOperationID   string
	RemoteOperation     replicationsync.Operation
	RemoteEntityVersion uint32
	TheirsSnapshot      []byte
	LocalChangeKey      string
	DetectedAt          time.Time
}

// captureRemoteConflict stores the divergent remote op as "theirs" when the
// target record has an active same-version push conflict, so a subsequent pull
// no longer silently drops the winning remote snapshot. It returns true when it
// captured (the caller must then treat the mutation as handled and leave ours
// untouched); false means this is an ordinary stale replay that should keep
// failing closed. Runs inside the ApplyRemoteBatch transaction.
func captureRemoteConflict(ctx context.Context, tx *sql.Tx, current remoteEntityVersion, mutation RemoteMutation) (bool, error) {
	// Only a same-version divergence is a conflict; a strictly older remote op is
	// a genuine stale replay and must keep being rejected.
	if mutation.EntityVersion != current.entityVersion {
		return false, nil
	}
	localChangeKey, found, err := loadConflictedLocalChangeKey(ctx, tx, mutation.AccountID, mutation.RecordID, current.entityVersion)
	if err != nil || !found {
		return false, err
	}
	// Upsert keeps re-pull of the same divergent op idempotent. Ours
	// (vault_items + sync_entity_versions) is deliberately not touched here.
	_, err = tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (
			account_id, record_id, record_type, remote_operation_id, remote_operation,
			remote_entity_version, theirs_snapshot, local_change_key, detected_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id, record_id) DO UPDATE SET
			record_type = excluded.record_type,
			remote_operation_id = excluded.remote_operation_id,
			remote_operation = excluded.remote_operation,
			remote_entity_version = excluded.remote_entity_version,
			theirs_snapshot = excluded.theirs_snapshot,
			local_change_key = excluded.local_change_key,
			detected_at = excluded.detected_at`, TableSyncConflicts),
		mutation.AccountID, mutation.RecordID, mutation.RecordType, mutation.OperationID,
		string(mutation.Operation), mutation.EntityVersion, mutation.RecordJSON,
		localChangeKey, formatTime(time.Now().UTC()))
	if err != nil {
		return false, err
	}
	return true, nil
}

// loadConflictedLocalChangeKey returns the idempotency key of the conflicted
// outbox row for this record at the losing version, if one exists.
func loadConflictedLocalChangeKey(ctx context.Context, tx *sql.Tx, accountID, recordID string, version uint32) (string, bool, error) {
	var key string
	err := tx.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT idempotency_key FROM %s
		WHERE account_id = ? AND record_id = ? AND status = ? AND entity_version = ?
		ORDER BY created_at ASC, idempotency_key ASC
		LIMIT 1`, TableSyncOutbox),
		accountID, recordID, string(replicationsync.StatusConflict), version).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return key, true, nil
}

// SQLConflictStore reads captured conflict sides for presentation and
// resolution. Account-bound; callers pass the authenticated account ID.
type SQLConflictStore struct{ db *sql.DB }

func NewSQLConflictStore(db *sql.DB) *SQLConflictStore {
	return &SQLConflictStore{db: db}
}

// Get returns the captured "theirs" side for a record, if present.
func (s *SQLConflictStore) Get(ctx context.Context, accountID, recordID string) (ConflictRecord, bool, error) {
	if s == nil || s.db == nil {
		return ConflictRecord{}, false, errors.New("conflict store is nil")
	}
	ctx = nonNilContext(ctx)
	if err := validateSyncIdentifier("account id", accountID, 256); err != nil {
		return ConflictRecord{}, false, err
	}
	if err := validateSyncIdentifier("record id", recordID, 256); err != nil {
		return ConflictRecord{}, false, err
	}
	row := s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT account_id, record_id, record_type, remote_operation_id, remote_operation,
			remote_entity_version, theirs_snapshot, local_change_key, detected_at
		FROM %s WHERE account_id = ? AND record_id = ?`, TableSyncConflicts), accountID, recordID)
	record, err := scanConflictRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ConflictRecord{}, false, nil
	}
	if err != nil {
		return ConflictRecord{}, false, err
	}
	return record, true, nil
}

// List returns every captured conflict side for the account, oldest first.
func (s *SQLConflictStore) List(ctx context.Context, accountID string) ([]ConflictRecord, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("conflict store is nil")
	}
	ctx = nonNilContext(ctx)
	if err := validateSyncIdentifier("account id", accountID, 256); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT account_id, record_id, record_type, remote_operation_id, remote_operation,
			remote_entity_version, theirs_snapshot, local_change_key, detected_at
		FROM %s WHERE account_id = ?
		ORDER BY detected_at ASC, record_id ASC`, TableSyncConflicts), accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]ConflictRecord, 0)
	for rows.Next() {
		record, err := scanConflictRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

type conflictRow interface {
	Scan(dest ...any) error
}

func scanConflictRecord(row conflictRow) (ConflictRecord, error) {
	var (
		record        ConflictRecord
		operation     string
		entityVersion int64
		detectedAt    string
	)
	if err := row.Scan(&record.AccountID, &record.RecordID, &record.RecordType,
		&record.RemoteOperationID, &operation, &entityVersion, &record.TheirsSnapshot,
		&record.LocalChangeKey, &detectedAt); err != nil {
		return ConflictRecord{}, err
	}
	if entityVersion <= 0 || entityVersion > int64(^uint32(0)) {
		return ConflictRecord{}, errors.New("stored conflict version is invalid")
	}
	record.RemoteEntityVersion = uint32(entityVersion)
	record.RemoteOperation = replicationsync.Operation(operation)
	parsed, err := time.Parse(syncTimeLayout, detectedAt)
	if err != nil {
		return ConflictRecord{}, errors.New("stored conflict time is invalid")
	}
	record.DetectedAt = parsed
	record.TheirsSnapshot = append([]byte(nil), record.TheirsSnapshot...)
	return record, nil
}
