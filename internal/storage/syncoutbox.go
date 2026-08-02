package storage

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
	db *sql.DB
}

var _ replicationsync.Outbox = (*SQLOutbox)(nil)

func NewSQLOutbox(db *sql.DB) *SQLOutbox {
	return &SQLOutbox{db: db}
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
			attempts, next_attempt_at, last_error, idempotency_key, conflict_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
			attempts, next_attempt_at, last_error, conflict_json, created_at
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

	query := fmt.Sprintf(`
		SELECT idempotency_key, record_id, record_type, operation, payload, key_version, status,
			attempts, next_attempt_at, last_error, conflict_json, created_at
		FROM %s
		WHERE status = ? AND next_attempt_at <= ?
		ORDER BY created_at ASC, idempotency_key ASC`, TableSyncOutbox)
	args := []any{string(replicationsync.StatusPending), formatTime(now)}
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

func (o *SQLOutbox) MarkSyncing(idempotencyKey string, _ time.Time) error {
	return o.updateState(idempotencyKey, `
		UPDATE sync_outbox SET status = ?
		WHERE idempotency_key = ? AND status = ?`,
		string(replicationsync.StatusSyncing), idempotencyKey, string(replicationsync.StatusPending))
}

func (o *SQLOutbox) RecoverStaleSyncing(_ time.Time) error {
	return nil
}

func (o *SQLOutbox) MarkSent(idempotencyKey string) error {
	return o.updateState(idempotencyKey, `
		UPDATE sync_outbox
		SET status = ?, last_error = '', next_attempt_at = '', conflict_json = NULL
		WHERE idempotency_key = ? AND status IN (?, ?)`,
		string(replicationsync.StatusSynced), idempotencyKey,
		string(replicationsync.StatusPending), string(replicationsync.StatusSyncing))
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
		SET status = ?, attempts = attempts + 1, next_attempt_at = ?, last_error = ?, conflict_json = NULL
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
		SET status = ?, attempts = attempts + 1, next_attempt_at = '', last_error = ?, conflict_json = NULL
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
		SET status = ?, next_attempt_at = '', last_error = '', conflict_json = ?
		WHERE idempotency_key = ? AND status IN (?, ?)`,
		string(replicationsync.StatusConflict), conflictJSON, idempotencyKey,
		string(replicationsync.StatusPending), string(replicationsync.StatusSyncing))
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
		payload                                                                                                          []byte
		keyVersion, attempts                                                                                             int64
		storedConflict                                                                                                   sql.NullString
	)
	if err := row.Scan(&storedIdempotencyKey, &recordID, &recordType, &operation, &payload, &keyVersion, &status, &attempts, &nextAttemptAt, &lastError, &storedConflict, &createdAt); err != nil {
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
	conflict, err := unmarshalConflict(conflictJSON)
	if err != nil {
		return replicationsync.Change{}, err
	}
	if keyVersion <= 0 || attempts < 0 {
		return replicationsync.Change{}, errors.New("stored change has invalid numeric state")
	}
	change := replicationsync.Change{
		IdempotencyKey:  idempotencyKey,
		RecordID:        recordID,
		RecordType:      recordType,
		Operation:       replicationsync.Operation(operation),
		Payload:         append([]byte(nil), payload...),
		KeyVersion:      uint32(keyVersion),
		AccountID:       "pending",
		DeviceID:        "pending",
		ProtocolVersion: replicationsync.SyncProtocolVersion,
		OperationID:     "pending",
		EntityVersion:   1,
		KeyID:           "pending",
		ContentDigest:   "pending",
		CreatedAt:       created,
		Status:          replicationsync.Status(status),
		Attempts:        int(attempts),
		NextAttemptAt:   next,
		LastError:       replicationsync.FailureReason(lastError),
		Conflict:        conflict,
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
	if a.IdempotencyKey != b.IdempotencyKey || a.RecordID != b.RecordID || a.RecordType != b.RecordType || a.Operation != b.Operation || a.KeyVersion != b.KeyVersion || !a.CreatedAt.Equal(b.CreatedAt) {
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
