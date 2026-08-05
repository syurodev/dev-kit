package syncstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	replicationsync "changeme/internal/sync"
)

const syncTimeLayout = time.RFC3339Nano

// SQLOutbox persists the local encrypted replication queue. It is deliberately
// a local persistence foundation: it does not perform network I/O or assume a
// SaaS transport exists.
type SQLOutbox struct {
	db    *sql.DB
	lease time.Duration
}

var _ replicationsync.Outbox = (*SQLOutbox)(nil)

func NewSQLOutbox(db *sql.DB, lease ...time.Duration) *SQLOutbox {
	l := 5 * time.Minute
	if len(lease) > 0 && lease[0] > 0 {
		l = lease[0]
	}
	return &SQLOutbox{db: db, lease: l}
}

// Enqueue stores a change once. A repeated identical idempotency key is a
// no-op; reusing the key for different immutable data is rejected.
func (o *SQLOutbox) Enqueue(change replicationsync.Change) error {
	change = normalizeSQLChange(change)
	if err := change.Validate(); err != nil {
		return err
	}
	if err := o.ensureDB(); err != nil {
		return err
	}

	conflictJSON, err := marshalConflict(change.Conflict)
	if err != nil {
		return err
	}
	result, err := o.db.ExecContext(context.Background(), fmt.Sprintf(
		`INSERT INTO %s (
			id, record_id, record_type, operation, payload, key_version, status,
			attempts, next_attempt_at, last_error, idempotency_key, conflict_json, created_at,
			syncing_since, account_id, device_id, protocol_version, operation_id,
			entity_version, key_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(idempotency_key) DO NOTHING`, TableSyncOutbox,
	),
		change.IdempotencyKey,
		change.RecordID,
		change.RecordType,
		string(change.Operation),
		append([]byte(nil), change.Payload...),
		change.KeyVersion,
		string(change.Status),
		change.Attempts,
		formatOptionalTime(change.NextAttemptAt),
		string(change.LastError),
		change.IdempotencyKey,
		conflictJSON,
		formatTime(change.CreatedAt),
		formatOptionalTime(change.SyncingSince),
		change.AccountID,
		change.DeviceID,
		change.ProtocolVersion,
		change.OperationID,
		change.EntityVersion,
		change.KeyID,
	)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err == nil && rows > 0 {
		return nil
	}

	existing, err := o.Get(change.IdempotencyKey)
	if err != nil {
		return err
	}
	if sameSQLImmutableChange(existing, change) {
		return nil
	}
	return replicationsync.ErrIdempotencyConflict
}

func (o *SQLOutbox) Get(idempotencyKey string) (replicationsync.Change, error) {
	if err := validateSyncIdentifier("idempotency key", idempotencyKey, 256); err != nil {
		return replicationsync.Change{}, err
	}
	if err := o.ensureDB(); err != nil {
		return replicationsync.Change{}, err
	}

	row := o.db.QueryRowContext(context.Background(), fmt.Sprintf(`
		SELECT idempotency_key, record_id, record_type, operation, payload, key_version, status,
			attempts, next_attempt_at, last_error, conflict_json, created_at,
			syncing_since, account_id, device_id, protocol_version, operation_id,
			entity_version, key_id
		FROM %s WHERE idempotency_key = ?`, TableSyncOutbox), idempotencyKey)
	change, err := scanSQLChange(row, idempotencyKey)
	if errors.Is(err, sql.ErrNoRows) {
		return replicationsync.Change{}, replicationsync.ErrNotFound
	}
	if err != nil {
		return replicationsync.Change{}, err
	}
	return cloneSQLChange(change), nil
}

func (o *SQLOutbox) Pending(now time.Time, limit int) ([]replicationsync.Change, error) {
	if limit < 0 {
		return nil, errors.New("limit cannot be negative")
	}
	if err := o.ensureDB(); err != nil {
		return nil, err
	}

	staleBefore := formatTime(now.Add(-o.lease))
	query := fmt.Sprintf(`
		SELECT idempotency_key, record_id, record_type, operation, payload, key_version, status,
			attempts, next_attempt_at, last_error, conflict_json, created_at,
			syncing_since, account_id, device_id, protocol_version, operation_id,
			entity_version, key_id
		FROM %s AS candidate
		WHERE (
			(status = ? AND next_attempt_at <= ?)
			OR (status = ? AND (syncing_since = '' OR syncing_since <= ?))
		)
		AND NOT EXISTS (
			SELECT 1 FROM %s AS earlier
			WHERE earlier.account_id = candidate.account_id
				AND earlier.record_id = candidate.record_id
				AND earlier.entity_version < candidate.entity_version
				AND earlier.status != ?
		)
		ORDER BY created_at ASC, record_id ASC, entity_version ASC, idempotency_key ASC`, TableSyncOutbox, TableSyncOutbox)
	args := []any{
		string(replicationsync.StatusPending), formatTime(now),
		string(replicationsync.StatusSyncing), staleBefore,
		string(replicationsync.StatusSynced),
	}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := o.db.QueryContext(context.Background(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	changes := make([]replicationsync.Change, 0)
	for rows.Next() {
		change, err := scanSQLChange(rows, "")
		if err != nil {
			return nil, err
		}
		changes = append(changes, cloneSQLChange(change))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return changes, nil
}

func (o *SQLOutbox) MarkSyncing(idempotencyKey string, at time.Time) error {
	staleBefore := formatTime(at.Add(-o.lease))
	return o.updateState(idempotencyKey, `
		UPDATE sync_outbox SET status = ?, syncing_since = ?
		WHERE idempotency_key = ? AND (
			status = ? OR (status = ? AND (syncing_since = '' OR syncing_since <= ?))
		)`,
		string(replicationsync.StatusSyncing), formatTime(at), idempotencyKey,
		string(replicationsync.StatusPending), string(replicationsync.StatusSyncing), staleBefore)
}

func (o *SQLOutbox) RecoverStaleSyncing(now time.Time) error {
	if err := o.ensureDB(); err != nil {
		return err
	}
	staleBefore := formatTime(now.Add(-o.lease))
	_, err := o.db.ExecContext(context.Background(), fmt.Sprintf(`
		UPDATE %s
		SET status = ?, syncing_since = '',
			next_attempt_at = CASE WHEN next_attempt_at = '' THEN ? ELSE next_attempt_at END
		WHERE status = ? AND (syncing_since = '' OR syncing_since <= ?)`, TableSyncOutbox),
		string(replicationsync.StatusPending), formatTime(now),
		string(replicationsync.StatusSyncing), staleBefore)
	return err
}

func (o *SQLOutbox) MarkSent(idempotencyKey string) error {
	if err := validateSyncIdentifier("idempotency key", idempotencyKey, 256); err != nil {
		return err
	}
	if err := o.ensureDB(); err != nil {
		return err
	}
	ctx := context.Background()
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var change struct {
		accountID, operationID, recordID, recordType, operation string
		entityVersion                                           int64
	}
	var status string
	err = tx.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT account_id, operation_id, record_id, record_type, entity_version, operation, status
		FROM %s WHERE idempotency_key = ?`, TableSyncOutbox), idempotencyKey).
		Scan(&change.accountID, &change.operationID, &change.recordID, &change.recordType,
			&change.entityVersion, &change.operation, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return replicationsync.ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != string(replicationsync.StatusPending) && status != string(replicationsync.StatusSyncing) {
		return replicationsync.ErrInvalidState
	}
	if _, err = tx.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s SET status = ?, last_error = '', next_attempt_at = '',
			conflict_json = NULL, syncing_since = ''
		WHERE idempotency_key = ? AND status IN (?, ?)`, TableSyncOutbox),
		string(replicationsync.StatusSynced), idempotencyKey,
		string(replicationsync.StatusPending), string(replicationsync.StatusSyncing)); err != nil {
		return err
	}

	// Legacy/test-only outbox rows may not belong to an authenticated binding.
	// Preserve their state transition without pretending they are replicated.
	var bindingExists int
	err = tx.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT 1 FROM %s WHERE account_id = ?", TableSyncAccountBinding,
	), change.accountID).Scan(&bindingExists)
	if errors.Is(err, sql.ErrNoRows) {
		return tx.Commit()
	}
	if err != nil {
		return err
	}

	if err := recordAcknowledgedOperation(ctx, tx, change.accountID, change.operationID,
		change.recordID, change.recordType, change.entityVersion, change.operation); err != nil {
		return err
	}
	if err := reconcileAcknowledgedLocalHead(ctx, tx, change.accountID, change.recordID); err != nil {
		return err
	}
	return tx.Commit()
}

func recordAcknowledgedOperation(ctx context.Context, tx *sql.Tx, accountID, operationID,
	recordID, recordType string, entityVersion int64, operation string,
) error {
	result, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (account_id, operation_id, record_id, record_type, entity_version, operation, applied_at)
		VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(account_id, operation_id) DO NOTHING`, TableSyncAppliedOps),
		accountID, operationID, recordID, recordType, entityVersion, operation, formatTime(time.Now().UTC()))
	if err != nil {
		return err
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr == nil && rows == 1 {
		return nil
	}
	var storedRecordID, storedRecordType, storedOperation string
	var storedVersion int64
	if err := tx.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT record_id, record_type, entity_version, operation FROM %s
		WHERE account_id = ? AND operation_id = ?`, TableSyncAppliedOps), accountID, operationID).
		Scan(&storedRecordID, &storedRecordType, &storedVersion, &storedOperation); err != nil {
		return err
	}
	if storedRecordID != recordID || storedRecordType != recordType ||
		storedVersion != entityVersion || storedOperation != operation {
		return ErrRemoteMutationConflict
	}
	return nil
}

func reconcileAcknowledgedLocalHead(ctx context.Context, tx *sql.Tx, accountID, recordID string) error {
	var headOperationID, headType string
	var headVersion int64
	var headTombstone int
	err := tx.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT operation_id, record_type, entity_version, tombstone FROM %s WHERE record_id = ?`, TableSyncLocalEntities),
		recordID).Scan(&headOperationID, &headType, &headVersion, &headTombstone)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if headTombstone == 1 {
		return nil
	}
	if headTombstone != 0 {
		return errors.New("local entity tombstone is invalid")
	}
	var headStatus string
	err = tx.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT status FROM %s WHERE account_id = ? AND operation_id = ?
			AND record_id = ? AND record_type = ? AND entity_version = ?`, TableSyncOutbox),
		accountID, headOperationID, recordID, headType, headVersion).Scan(&headStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // The current head came from remote apply.
	}
	if err != nil {
		return err
	}
	if headStatus != string(replicationsync.StatusSynced) {
		return nil
	}
	var unsyncedCount int
	if err := tx.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT
			(SELECT COUNT(*) FROM %s WHERE record_id = ?) +
			(SELECT COUNT(*) FROM %s WHERE account_id = ? AND record_id = ? AND status != ?)
		`, TableSyncLocalMutations, TableSyncOutbox),
		recordID, accountID, recordID, string(replicationsync.StatusSynced)).Scan(&unsyncedCount); err != nil {
		return err
	}
	if unsyncedCount != 0 {
		return nil
	}
	var storedType string
	var data []byte
	err = tx.QueryRowContext(ctx, fmt.Sprintf("SELECT type, data FROM %s WHERE id = ?", TableVaultItems), recordID).
		Scan(&storedType, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if storedType != headType {
		return ErrRemoteMutationConflict
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return errors.New("vault item JSON is invalid")
	}
	object["sync_state"] = json.RawMessage(`"synced"`)
	normalized, err := json.Marshal(object)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, fmt.Sprintf("UPDATE %s SET data = ? WHERE id = ? AND type = ?", TableVaultItems),
		normalized, recordID, headType)
	return err
}

func (o *SQLOutbox) MarkRetry(idempotencyKey string, nextAttemptAt time.Time, reason replicationsync.FailureReason) error {
	if nextAttemptAt.IsZero() {
		return errors.New("next attempt time is required")
	}
	if !validSQLFailureReason(reason) {
		return errors.New("invalid failure reason")
	}
	return o.updateState(idempotencyKey, `
		UPDATE sync_outbox
		SET status = ?, attempts = attempts + 1, next_attempt_at = ?, last_error = ?, conflict_json = NULL, syncing_since = ''
		WHERE idempotency_key = ? AND status IN (?, ?)`,
		string(replicationsync.StatusPending), formatTime(nextAttemptAt), string(reason), idempotencyKey,
		string(replicationsync.StatusPending), string(replicationsync.StatusSyncing))
}

func (o *SQLOutbox) MarkFailed(idempotencyKey string, reason replicationsync.FailureReason) error {
	if !validSQLFailureReason(reason) {
		return errors.New("invalid failure reason")
	}
	return o.updateState(idempotencyKey, `
		UPDATE sync_outbox
		SET status = ?, attempts = attempts + 1, next_attempt_at = '', last_error = ?, conflict_json = NULL, syncing_since = ''
		WHERE idempotency_key = ? AND status IN (?, ?)`,
		string(replicationsync.StatusFailed), string(reason), idempotencyKey,
		string(replicationsync.StatusPending), string(replicationsync.StatusSyncing))
}

func (o *SQLOutbox) MarkConflict(idempotencyKey string, conflict replicationsync.Conflict) error {
	if err := conflict.Validate(); err != nil {
		return err
	}
	change, err := o.Get(idempotencyKey)
	if err != nil {
		return err
	}
	if conflict.RecordID != change.RecordID || conflict.RecordType != change.RecordType || conflict.LocalChangeKey != change.IdempotencyKey {
		return errors.New("conflict does not match change")
	}
	conflictJSON, err := marshalConflict(&conflict)
	if err != nil {
		return err
	}
	return o.updateState(idempotencyKey, `
		UPDATE sync_outbox
		SET status = ?, next_attempt_at = '', last_error = '', conflict_json = ?, syncing_since = ''
		WHERE idempotency_key = ? AND status IN (?, ?)`,
		string(replicationsync.StatusConflict), conflictJSON, idempotencyKey,
		string(replicationsync.StatusPending), string(replicationsync.StatusSyncing))
}

func (o *SQLOutbox) Conflicts() ([]replicationsync.Change, error) {
	if err := o.ensureDB(); err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`
		SELECT idempotency_key, record_id, record_type, operation, payload, key_version, status,
			attempts, next_attempt_at, last_error, conflict_json, created_at,
			syncing_since, account_id, device_id, protocol_version, operation_id,
			entity_version, key_id
		FROM %s WHERE status = ?
		ORDER BY created_at ASC, record_id ASC, entity_version ASC, idempotency_key ASC`, TableSyncOutbox)
	rows, err := o.db.QueryContext(context.Background(), query, string(replicationsync.StatusConflict))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	changes := make([]replicationsync.Change, 0)
	for rows.Next() {
		change, err := scanSQLChange(rows, "")
		if err != nil {
			return nil, err
		}
		changes = append(changes, cloneSQLChange(change))
	}
	return changes, rows.Err()
}

func (o *SQLOutbox) updateState(idempotencyKey, query string, args ...any) error {
	if err := validateSyncIdentifier("idempotency key", idempotencyKey, 256); err != nil {
		return err
	}
	if err := o.ensureDB(); err != nil {
		return err
	}
	result, err := o.db.ExecContext(context.Background(), query, args...)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows > 0 {
		return nil
	}

	var status string
	err = o.db.QueryRowContext(context.Background(), fmt.Sprintf(
		"SELECT status FROM %s WHERE idempotency_key = ?", TableSyncOutbox,
	), idempotencyKey).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return replicationsync.ErrNotFound
	}
	if err != nil {
		return err
	}
	return replicationsync.ErrInvalidState
}

// SQLCursorStore stores only opaque replication cursors and a safe update time.
// It has no network behavior and is intentionally independent from the outbox.
type SQLCursorStore struct {
	db *sql.DB
}

func NewSQLCursorStore(db *sql.DB) *SQLCursorStore {
	return &SQLCursorStore{db: db}
}

// NewCursorStore is a short compatibility constructor for callers that do not
// need to distinguish the SQLite implementation in the name.
func NewCursorStore(db *sql.DB) *SQLCursorStore {
	return NewSQLCursorStore(db)
}

func (s *SQLCursorStore) GetCursor(ctx context.Context, name string) (replicationsync.Cursor, error) {
	if err := validateSyncIdentifier("cursor name", name, 128); err != nil {
		return replicationsync.Cursor{}, err
	}
	if err := ensureSQLDB(s.db); err != nil {
		return replicationsync.Cursor{}, err
	}
	var token string
	err := s.db.QueryRowContext(nonNilContext(ctx), fmt.Sprintf(
		"SELECT cursor FROM %s WHERE name = ?", TableSyncCursors,
	), name).Scan(&token)
	if errors.Is(err, sql.ErrNoRows) {
		return replicationsync.Cursor{}, nil
	}
	if err != nil {
		return replicationsync.Cursor{}, err
	}
	cursor := replicationsync.Cursor{Token: token}
	if err := cursor.Validate(); err != nil {
		return replicationsync.Cursor{}, errors.New("stored cursor is invalid")
	}
	return cursor, nil
}

func (s *SQLCursorStore) PutCursor(ctx context.Context, name string, cursor replicationsync.Cursor) error {
	if err := validateSyncIdentifier("cursor name", name, 128); err != nil {
		return err
	}
	if err := cursor.Validate(); err != nil {
		return err
	}
	if err := ensureSQLDB(s.db); err != nil {
		return err
	}
	_, err := s.db.ExecContext(nonNilContext(ctx), fmt.Sprintf(`
		INSERT INTO %s (name, cursor, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET cursor = excluded.cursor, updated_at = excluded.updated_at`, TableSyncCursors),
		name, cursor.Token, formatTime(time.Now().UTC()))
	return err
}

type sqlChangeRow interface {
	Scan(dest ...any) error
}

func scanSQLChange(row sqlChangeRow, idempotencyKey string) (replicationsync.Change, error) {
	var (
		storedIdempotencyKey, recordID, recordType, operation, status, nextAttemptAt, lastError, conflictJSON, createdAt string
		syncingSince, accountID, deviceID, operationID, keyID                                                            string
		payload                                                                                                          []byte
		keyVersion, attempts, protocolVersion, entityVersion                                                             int64
		storedConflict                                                                                                   sql.NullString
	)
	if err := row.Scan(
		&storedIdempotencyKey, &recordID, &recordType, &operation, &payload, &keyVersion, &status, &attempts,
		&nextAttemptAt, &lastError, &storedConflict, &createdAt,
		&syncingSince, &accountID, &deviceID, &protocolVersion, &operationID, &entityVersion, &keyID,
	); err != nil {
		return replicationsync.Change{}, err
	}
	if idempotencyKey == "" {
		idempotencyKey = storedIdempotencyKey
	}
	if storedConflict.Valid {
		conflictJSON = storedConflict.String
	}
	created, err := parseTime(createdAt)
	if err != nil {
		return replicationsync.Change{}, errors.New("stored change has invalid created time")
	}
	next, err := parseOptionalTime(nextAttemptAt)
	if err != nil {
		return replicationsync.Change{}, errors.New("stored change has invalid next attempt time")
	}
	synced, err := parseOptionalTime(syncingSince)
	if err != nil {
		return replicationsync.Change{}, errors.New("stored change has invalid syncing time")
	}
	conflict, err := unmarshalConflict(conflictJSON)
	if err != nil {
		return replicationsync.Change{}, err
	}
	if keyVersion <= 0 || attempts < 0 || protocolVersion < 0 || entityVersion < 0 {
		return replicationsync.Change{}, errors.New("stored change has invalid numeric state")
	}
	change := replicationsync.Change{
		IdempotencyKey:  idempotencyKey,
		RecordID:        recordID,
		RecordType:      recordType,
		Operation:       replicationsync.Operation(operation),
		Payload:         append([]byte(nil), payload...),
		KeyVersion:      uint32(keyVersion),
		AccountID:       accountID,
		DeviceID:        deviceID,
		ProtocolVersion: uint32(protocolVersion),
		OperationID:     operationID,
		EntityVersion:   uint32(entityVersion),
		KeyID:           keyID,
		CreatedAt:       created,
		Status:          replicationsync.Status(status),
		Attempts:        int(attempts),
		NextAttemptAt:   next,
		LastError:       replicationsync.FailureReason(lastError),
		Conflict:        conflict,
		SyncingSince:    synced,
	}
	if err := change.Validate(); err != nil {
		return replicationsync.Change{}, errors.New("stored change is invalid")
	}
	return change, nil
}

type persistedConflict struct {
	ID              string    `json:"id"`
	RecordID        string    `json:"record_id"`
	RecordType      string    `json:"record_type"`
	LocalChangeKey  string    `json:"local_change_key"`
	RemoteChangeKey string    `json:"remote_change_key"`
	DetectedAt      time.Time `json:"detected_at"`
}

func marshalConflict(conflict *replicationsync.Conflict) (any, error) {
	if conflict == nil {
		return nil, nil
	}
	if err := conflict.Validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(persistedConflict{
		ID:              conflict.ID,
		RecordID:        conflict.RecordID,
		RecordType:      conflict.RecordType,
		LocalChangeKey:  conflict.LocalChangeKey,
		RemoteChangeKey: conflict.RemoteChangeKey,
		DetectedAt:      conflict.DetectedAt.UTC(),
	})
	if err != nil {
		return nil, errors.New("encode conflict metadata")
	}
	return string(data), nil
}

func unmarshalConflict(data string) (*replicationsync.Conflict, error) {
	if data == "" {
		return nil, nil
	}
	var stored persistedConflict
	if err := json.Unmarshal([]byte(data), &stored); err != nil {
		return nil, errors.New("stored conflict metadata is invalid")
	}
	conflict := &replicationsync.Conflict{
		ID:              stored.ID,
		RecordID:        stored.RecordID,
		RecordType:      stored.RecordType,
		LocalChangeKey:  stored.LocalChangeKey,
		RemoteChangeKey: stored.RemoteChangeKey,
		DetectedAt:      stored.DetectedAt,
	}
	if err := conflict.Validate(); err != nil {
		return nil, errors.New("stored conflict metadata is invalid")
	}
	return conflict, nil
}

func normalizeSQLChange(change replicationsync.Change) replicationsync.Change {
	if change.Status == "" {
		change.Status = replicationsync.StatusPending
	}
	if change.NextAttemptAt.IsZero() && change.Status == replicationsync.StatusPending {
		change.NextAttemptAt = change.CreatedAt
	}
	change.Payload = append([]byte(nil), change.Payload...)
	if change.Conflict != nil {
		conflict := *change.Conflict
		change.Conflict = &conflict
	}
	return change
}

func sameSQLImmutableChange(a, b replicationsync.Change) bool {
	if a.IdempotencyKey != b.IdempotencyKey || a.RecordID != b.RecordID || a.RecordType != b.RecordType || a.Operation != b.Operation || a.KeyVersion != b.KeyVersion {
		return false
	}
	if a.AccountID != b.AccountID || a.DeviceID != b.DeviceID || a.ProtocolVersion != b.ProtocolVersion ||
		a.OperationID != b.OperationID || a.EntityVersion != b.EntityVersion ||
		a.KeyID != b.KeyID {
		return false
	}
	return string(a.Payload) == string(b.Payload)
}

func cloneSQLChange(change replicationsync.Change) replicationsync.Change {
	change.Payload = append([]byte(nil), change.Payload...)
	if change.Conflict != nil {
		conflict := *change.Conflict
		change.Conflict = &conflict
	}
	return change
}

func validSQLFailureReason(reason replicationsync.FailureReason) bool {
	switch reason {
	case replicationsync.FailureNetwork, replicationsync.FailureTimeout, replicationsync.FailureInvalidResponse, replicationsync.FailureUnknown:
		return true
	default:
		return false
	}
}

func validateSyncIdentifier(label, value string, max int) error {
	if value == "" {
		return fmt.Errorf("%s is required", label)
	}
	if len(value) > max {
		return fmt.Errorf("%s is too long", label)
	}
	if strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return fmt.Errorf("%s contains control characters", label)
	}
	return nil
}

func ensureSQLDB(db *sql.DB) error {
	if db == nil {
		return errors.New("SQLite database is nil")
	}
	return nil
}

func (o *SQLOutbox) ensureDB() error {
	if o == nil {
		return errors.New("sync outbox is nil")
	}
	return ensureSQLDB(o.db)
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func formatTime(value time.Time) string {
	return value.UTC().Format(syncTimeLayout)
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return formatTime(value)
}

func parseTime(value string) (time.Time, error) {
	return time.Parse(syncTimeLayout, value)
}

func parseOptionalTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return parseTime(value)
}
