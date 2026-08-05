package syncstore

import (
	"bytes"
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

const (
	localAccountBindingRowID = "local"
	maxRemoteRecordJSONBytes = 1 << 20 // 1 MiB prevents unbounded remote allocations.
	maxRemoteBatchJSONBytes  = 8 << 20 // Bound aggregate memory and transaction size.
	maxRemoteBatchSize       = 1000
)

var (
	// ErrSyncAccountMismatch means this database is already bound to another
	// authenticated account. Callers must fail closed rather than mix data.
	ErrSyncAccountMismatch = errors.New("sync account does not match local binding")
	// ErrRemoteMutationConflict covers stale/equal versions, reused operation
	// IDs, and otherwise inconsistent remote entity history.
	ErrRemoteMutationConflict = errors.New("remote mutation conflicts with local entity version")
)

// RemoteMutation is a decrypted, authenticated remote operation ready for
// persistence. RecordJSON is the vault_items Record representation; this
// package validates only its routing identity and keeps crypto concerns out.
type RemoteMutation struct {
	AccountID     string
	RecordID      string
	RecordType    string
	Operation     replicationsync.Operation
	OperationID   string
	EntityVersion uint32
	RecordJSON    []byte
}

// SQLRemoteStore atomically applies authenticated pull batches to vault_items,
// the version/tombstone ledger, and the opaque pull cursor.
type SQLRemoteStore struct {
	db *sql.DB
}

func NewSQLRemoteStore(db *sql.DB) *SQLRemoteStore {
	return &SQLRemoteStore{db: db}
}

// ValidateAuthenticatedAccount rejects an already-bound account mismatch
// without claiming an unbound database. The first durable claim is deferred to
// the user's explicit SyncNow action.
func (s *SQLRemoteStore) ValidateAuthenticatedAccount(ctx context.Context, authenticatedAccountID string) error {
	if s == nil || s.db == nil {
		return errors.New("remote sync store is nil")
	}
	ctx = nonNilContext(ctx)
	if err := validateSyncIdentifier("account id", authenticatedAccountID, 256); err != nil {
		return err
	}
	var bound string
	err := s.db.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT account_id FROM %s WHERE id = ?", TableSyncAccountBinding,
	), localAccountBindingRowID).Scan(&bound)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if bound != authenticatedAccountID {
		return ErrSyncAccountMismatch
	}
	return nil
}

// BoundAccountID returns the account this local database is bound to, if any.
// Conflict resolution runs offline and needs the account without a live session.
func (s *SQLRemoteStore) BoundAccountID(ctx context.Context) (string, bool, error) {
	if s == nil || s.db == nil {
		return "", false, errors.New("remote sync store is nil")
	}
	ctx = nonNilContext(ctx)
	var bound string
	err := s.db.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT account_id FROM %s WHERE id = ?", TableSyncAccountBinding,
	), localAccountBindingRowID).Scan(&bound)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return bound, true, nil
}

// BindAuthenticatedAccount permanently associates this local database with
// the server-authoritative account before any push or pull is attempted. The
// binding is intentionally one-way; switching accounts requires an explicit
// future reset flow that can safely clear account-owned local sync state.
func (s *SQLRemoteStore) BindAuthenticatedAccount(ctx context.Context, authenticatedAccountID string) (err error) {
	if s == nil || s.db == nil {
		return errors.New("remote sync store is nil")
	}
	ctx = nonNilContext(ctx)
	if err := validateSyncIdentifier("account id", authenticatedAccountID, 256); err != nil {
		return err
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
	if err = bindAuthenticatedAccount(ctx, tx, authenticatedAccountID); err != nil {
		return err
	}
	return tx.Commit()
}

// ApplyRemoteBatch binds the first authenticated account and applies every
// mutation in one SQLite transaction. Any validation, version, storage, or
// context error rolls back item changes, ledger changes, binding, and cursor.
func (s *SQLRemoteStore) ApplyRemoteBatch(
	ctx context.Context,
	authenticatedAccountID, cursorName, nextCursor string,
	mutations []RemoteMutation,
) (err error) {
	if s == nil || s.db == nil {
		return errors.New("remote sync store is nil")
	}
	ctx = nonNilContext(ctx)
	if err := validateSyncIdentifier("account id", authenticatedAccountID, 256); err != nil {
		return err
	}
	if err := validateSyncIdentifier("cursor name", cursorName, 128); err != nil {
		return err
	}
	if err := validateOpaqueCursor(nextCursor); err != nil {
		return err
	}
	if len(mutations) > maxRemoteBatchSize {
		return fmt.Errorf("remote mutation batch exceeds %d items", maxRemoteBatchSize)
	}
	validated := make([]RemoteMutation, len(mutations))
	totalJSONBytes := 0
	for i := range mutations {
		if err := validateRemoteMutation(authenticatedAccountID, mutations[i]); err != nil {
			return fmt.Errorf("remote mutation %d: %w", i, err)
		}
		totalJSONBytes += len(mutations[i].RecordJSON)
		if totalJSONBytes > maxRemoteBatchJSONBytes {
			return fmt.Errorf("remote mutation batch JSON exceeds %d bytes", maxRemoteBatchJSONBytes)
		}
		validated[i] = mutations[i]
		validated[i].RecordJSON = append([]byte(nil), mutations[i].RecordJSON...)
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

	if err = bindAuthenticatedAccount(ctx, tx, authenticatedAccountID); err != nil {
		return err
	}
	for i := range validated {
		if err = applyRemoteMutation(ctx, tx, validated[i]); err != nil {
			return fmt.Errorf("apply remote mutation %d: %w", i, err)
		}
	}
	if _, err = tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (name, cursor, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET cursor = excluded.cursor, updated_at = excluded.updated_at`, TableSyncCursors),
		cursorName, nextCursor, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	return tx.Commit()
}

func validateRemoteMutation(authenticatedAccountID string, mutation RemoteMutation) error {
	if err := validateSyncIdentifier("mutation account id", mutation.AccountID, 256); err != nil {
		return err
	}
	if mutation.AccountID != authenticatedAccountID {
		return ErrSyncAccountMismatch
	}
	if err := validateSyncIdentifier("record id", mutation.RecordID, 256); err != nil {
		return err
	}
	if err := validateSyncIdentifier("record type", mutation.RecordType, 128); err != nil {
		return err
	}
	if err := validateSyncIdentifier("operation id", mutation.OperationID, 256); err != nil {
		return err
	}
	if mutation.EntityVersion == 0 {
		return errors.New("entity version must be positive")
	}

	switch mutation.Operation {
	case replicationsync.OperationCreate, replicationsync.OperationUpdate:
		if len(mutation.RecordJSON) == 0 {
			return errors.New("record JSON is required")
		}
		if len(mutation.RecordJSON) > maxRemoteRecordJSONBytes {
			return fmt.Errorf("record JSON exceeds %d bytes", maxRemoteRecordJSONBytes)
		}
		var identity struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(mutation.RecordJSON, &identity); err != nil {
			return errors.New("record JSON is invalid")
		}
		if identity.ID != mutation.RecordID || identity.Type != mutation.RecordType {
			return errors.New("record JSON identity does not match mutation")
		}
	case replicationsync.OperationDelete:
		if len(bytes.TrimSpace(mutation.RecordJSON)) != 0 {
			return errors.New("delete mutation must not include record JSON")
		}
	default:
		return fmt.Errorf("invalid remote operation %q", mutation.Operation)
	}
	return nil
}

func validateOpaqueCursor(cursor string) error {
	if len(cursor) > 512 {
		return errors.New("cursor token is too long")
	}
	if strings.IndexFunc(cursor, unicode.IsControl) >= 0 {
		return errors.New("cursor token contains control characters")
	}
	return nil
}

func bindAuthenticatedAccount(ctx context.Context, tx *sql.Tx, accountID string) error {
	var boundAccountID string
	err := tx.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT account_id FROM %s WHERE id = ?", TableSyncAccountBinding,
	), localAccountBindingRowID).Scan(&boundAccountID)
	if err == nil {
		if boundAccountID != accountID {
			return ErrSyncAccountMismatch
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.ExecContext(ctx, fmt.Sprintf(
		"INSERT INTO %s (id, account_id, bound_at) VALUES (?, ?, ?)", TableSyncAccountBinding,
	), localAccountBindingRowID, accountID, formatTime(time.Now().UTC()))
	return err
}

type remoteEntityVersion struct {
	recordType    string
	entityVersion uint32
	operationID   string
	operation     replicationsync.Operation
	tombstone     bool
}

func applyRemoteMutation(ctx context.Context, tx *sql.Tx, mutation RemoteMutation) error {
	applied, found, err := loadAppliedRemoteOperation(ctx, tx, mutation.AccountID, mutation.OperationID)
	if err != nil {
		return err
	}
	if found {
		if applied.matches(mutation) {
			return nil // Historical exact replay must not rewrite current state.
		}
		return ErrRemoteMutationConflict
	}

	current, exists, err := loadRemoteEntityVersion(ctx, tx, mutation.AccountID, mutation.RecordID)
	if err != nil {
		return err
	}
	pendingLocal, err := hasPendingLocalIntent(ctx, tx, mutation.RecordID)
	if err != nil {
		return err
	}
	if pendingLocal {
		return ErrRemoteMutationConflict
	}
	if exists && current.recordType != mutation.RecordType {
		return ErrRemoteMutationConflict
	}
	// Remote clients may compact several local mutations into the latest
	// snapshot before syncing. Therefore versions must increase strictly, but
	// they need not be contiguous and the first accepted version may exceed 1.
	if exists && mutation.EntityVersion <= current.entityVersion {
		// A same-version divergence for a record that lost a push is not a stale
		// replay: capture the authenticated remote snapshot as "theirs" so the
		// user can resolve it, and leave ours untouched. Otherwise fail closed.
		captured, err := captureRemoteConflict(ctx, tx, current, mutation)
		if err != nil {
			return err
		}
		if captured {
			return nil
		}
		return ErrRemoteMutationConflict
	}
	if exists {
		if err := rejectUnsyncedLocalState(ctx, tx, current, mutation); err != nil {
			return err
		}
	}
	if !exists {
		// m9 intentionally does not backfill vault_items. If an untracked local
		// row nevertheless collides with a remote ID, fail closed rather than
		// overwrite data whose version history is unknown.
		var existingType string
		err := tx.QueryRowContext(ctx, fmt.Sprintf(
			"SELECT type FROM %s WHERE id = ?", TableVaultItems,
		), mutation.RecordID).Scan(&existingType)
		if err == nil {
			return ErrRemoteMutationConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}

	if mutation.Operation == replicationsync.OperationDelete {
		// A compacted first mutation may be a delete. Deleting an absent row is
		// valid because the durable tombstone below is the authoritative state.
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE id = ? AND type = ?", TableVaultItems), mutation.RecordID, mutation.RecordType); err != nil {
			return err
		}
	} else {
		// Create and update both carry the latest full record snapshot. Upsert is
		// required when compaction makes an update the first mutation we see.
		result, err := tx.ExecContext(ctx, fmt.Sprintf(`
			INSERT INTO %s (id, type, data) VALUES (?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET type = excluded.type, data = excluded.data
			WHERE %s.type = excluded.type`, TableVaultItems, TableVaultItems),
			mutation.RecordID, mutation.RecordType, mutation.RecordJSON)
		if err != nil {
			return err
		}
		if rows, err := result.RowsAffected(); err != nil || rows != 1 {
			return ErrRemoteMutationConflict
		}
	}

	tombstone := 0
	if mutation.Operation == replicationsync.OperationDelete {
		tombstone = 1
	}
	if _, err = tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (
			account_id, record_id, record_type, entity_version, operation_id, operation, tombstone, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id, record_id) DO UPDATE SET
			entity_version = excluded.entity_version,
			operation_id = excluded.operation_id,
			operation = excluded.operation,
			tombstone = excluded.tombstone,
			updated_at = excluded.updated_at`, TableSyncEntityVersions),
		mutation.AccountID, mutation.RecordID, mutation.RecordType, mutation.EntityVersion,
		mutation.OperationID, string(mutation.Operation), tombstone, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	// Keep the account-neutral local head aligned with accepted remote state so
	// the next offline/local mutation can atomically allocate N+1 even when no
	// authenticated session is active at mutation time. This direct remote path
	// deliberately does not create a local mutation intent.
	if _, err = tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (
			record_id, record_type, entity_version, operation_id, operation, tombstone, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(record_id) DO UPDATE SET
			record_type = excluded.record_type,
			entity_version = excluded.entity_version,
			operation_id = excluded.operation_id,
			operation = excluded.operation,
			tombstone = excluded.tombstone,
			updated_at = excluded.updated_at`, TableSyncLocalEntities),
		mutation.RecordID, mutation.RecordType, mutation.EntityVersion,
		mutation.OperationID, string(mutation.Operation), tombstone,
		formatTime(time.Now().UTC())); err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (
			account_id, operation_id, record_id, record_type, entity_version, operation, applied_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)`, TableSyncAppliedOps),
		mutation.AccountID, mutation.OperationID, mutation.RecordID, mutation.RecordType,
		mutation.EntityVersion, string(mutation.Operation), formatTime(time.Now().UTC()))
	return err
}

// rejectUnsyncedLocalState protects local edits while mutation capture is not
// yet wired into vault_items writes. Remote snapshots may replace only records
// still marked synced. A missing live row is treated as a local deletion unless
// the ledger already contains a tombstone or the incoming operation is also a
// delete, both of which preserve the same absent state.
func rejectUnsyncedLocalState(ctx context.Context, tx *sql.Tx, current remoteEntityVersion, mutation RemoteMutation) error {
	var recordType string
	var recordJSON []byte
	err := tx.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT type, data FROM %s WHERE id = ?", TableVaultItems,
	), mutation.RecordID).Scan(&recordType, &recordJSON)
	if errors.Is(err, sql.ErrNoRows) {
		if current.tombstone {
			unsynced, checkErr := hasUnsyncedLocalOperation(ctx, tx, mutation.AccountID, current.operationID)
			if checkErr != nil {
				return checkErr
			}
			if unsynced {
				return ErrRemoteMutationConflict
			}
			return nil
		}
		if mutation.Operation == replicationsync.OperationDelete {
			return nil
		}
		return ErrRemoteMutationConflict
	}
	if err != nil {
		return err
	}
	if recordType != mutation.RecordType {
		return ErrRemoteMutationConflict
	}
	var local struct {
		SyncState string `json:"sync_state"`
	}
	if err := json.Unmarshal(recordJSON, &local); err != nil || local.SyncState != "synced" {
		return ErrRemoteMutationConflict
	}
	return nil
}

func hasPendingLocalIntent(ctx context.Context, tx *sql.Tx, recordID string) (bool, error) {
	// A local mutation can commit after SyncNow's materialization pass but before
	// pull/apply. Its account-neutral intent is durable proof that local state is
	// newer, including a physically missing row after delete.
	var count int
	if err := tx.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT COUNT(*) FROM %s WHERE record_id = ?", TableSyncLocalMutations,
	), recordID).Scan(&count); err != nil {
		return false, err
	}
	return count != 0, nil
}

func hasUnsyncedLocalOperation(ctx context.Context, tx *sql.Tx, accountID, operationID string) (bool, error) {
	var status string
	err := tx.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT status FROM %s WHERE account_id = ? AND operation_id = ?`, TableSyncOutbox),
		accountID, operationID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil // The head came from remote apply, not a local outbox.
	}
	if err != nil {
		return false, err
	}
	return status != string(replicationsync.StatusSynced), nil
}

type appliedRemoteOperation struct {
	recordID      string
	recordType    string
	entityVersion uint32
	operation     replicationsync.Operation
}

func (applied appliedRemoteOperation) matches(mutation RemoteMutation) bool {
	return applied.recordID == mutation.RecordID &&
		applied.recordType == mutation.RecordType &&
		applied.entityVersion == mutation.EntityVersion &&
		applied.operation == mutation.Operation
}

func loadAppliedRemoteOperation(
	ctx context.Context,
	tx *sql.Tx,
	accountID, operationID string,
) (appliedRemoteOperation, bool, error) {
	var (
		applied       appliedRemoteOperation
		entityVersion int64
		operation     string
	)
	err := tx.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT record_id, record_type, entity_version, operation
		FROM %s WHERE account_id = ? AND operation_id = ?`, TableSyncAppliedOps),
		accountID, operationID).Scan(&applied.recordID, &applied.recordType, &entityVersion, &operation)
	if errors.Is(err, sql.ErrNoRows) {
		return appliedRemoteOperation{}, false, nil
	}
	if err != nil {
		return appliedRemoteOperation{}, false, err
	}
	if entityVersion <= 0 || entityVersion > int64(^uint32(0)) {
		return appliedRemoteOperation{}, false, errors.New("stored applied operation version is invalid")
	}
	applied.entityVersion = uint32(entityVersion)
	applied.operation = replicationsync.Operation(operation)
	return applied, true, nil
}

func loadRemoteEntityVersion(ctx context.Context, tx *sql.Tx, accountID, recordID string) (remoteEntityVersion, bool, error) {
	var (
		current       remoteEntityVersion
		entityVersion int64
		operation     string
		tombstone     int
	)
	err := tx.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT record_type, entity_version, operation_id, operation, tombstone
		FROM %s WHERE account_id = ? AND record_id = ?`, TableSyncEntityVersions),
		accountID, recordID).Scan(&current.recordType, &entityVersion, &current.operationID, &operation, &tombstone)
	if errors.Is(err, sql.ErrNoRows) {
		return remoteEntityVersion{}, false, nil
	}
	if err != nil {
		return remoteEntityVersion{}, false, err
	}
	if entityVersion <= 0 || entityVersion > int64(^uint32(0)) || (tombstone != 0 && tombstone != 1) {
		return remoteEntityVersion{}, false, errors.New("stored remote entity version is invalid")
	}
	current.entityVersion = uint32(entityVersion)
	current.operation = replicationsync.Operation(operation)
	current.tombstone = tombstone == 1
	return current, true, nil
}
