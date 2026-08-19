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

// captureRemoteMutation stores a divergent remote op as "theirs" whenever the
// apply-guard rejects it against local state: a version at or below a dirty
// local ledger head, a newer remote op against unsynced local edits, or an
// untracked local row colliding with a first remote op. The op may be a delete,
// whose "theirs" side has no record body (theirs_snapshot NULL; remote_operation
// distinguishes it). Upsert keeps re-pulls of the same or a newer divergent op
// idempotent. Ours (vault_items + ledgers) is deliberately not touched. Runs
// inside the ApplyRemoteBatch transaction; the op is consumed so the pull
// cursor advances instead of wedging the account.
func captureRemoteMutation(ctx context.Context, tx *sql.Tx, mutation RemoteMutation) error {
	localChangeKey, err := loadLocalChangeKey(ctx, tx, mutation.AccountID, mutation.RecordID)
	if err != nil {
		return err
	}
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
	return err
}

// loadLocalChangeKey returns the idempotency key of the record's losing local
// lineage: the conflicted outbox row when one exists (the push the server
// rejected), else the newest unsynced row. Empty when no local lineage is
// durable — the conflict is still captured; resolution simply has no outbox
// row to clear.
func loadLocalChangeKey(ctx context.Context, tx *sql.Tx, accountID, recordID string) (string, error) {
	var key string
	err := tx.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT idempotency_key FROM %s
		WHERE account_id = ? AND record_id = ? AND status != ?
		ORDER BY CASE status WHEN ? THEN 0 ELSE 1 END,
			entity_version DESC, created_at ASC, idempotency_key ASC
		LIMIT 1`, TableSyncOutbox),
		accountID, recordID, string(replicationsync.StatusSynced),
		string(replicationsync.StatusConflict)).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return key, nil
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
