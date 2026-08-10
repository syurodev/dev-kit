package vaultitem

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"devkit/internal/crypto"
	replicationsync "devkit/internal/sync"
)

const (
	localVaultItemsTable = "vault_items"
	localEntitiesTable   = "sync_local_entity_versions"
	localMutationsTable  = "sync_local_mutations"
	accountEntitiesTable = "sync_entity_versions"
	localSnapshotKeyID   = "vault-item"
)

// SQLMutationRepository is the production local-mutation unit of work. A
// local Put/Delete commits the vault row, local entity head and durable sync
// intent atomically. Remote apply uses its own storage path and cannot loop.
type SQLMutationRepository struct{ db *sql.DB }

var _ Repository = (*SQLMutationRepository)(nil)

func NewSQLMutationRepository(db *sql.DB) *SQLMutationRepository {
	return &SQLMutationRepository{db: db}
}

func (r *SQLMutationRepository) List() ([]Record, error) {
	rows, err := r.db.QueryContext(context.Background(), "SELECT data FROM "+localVaultItemsTable)
	if err != nil {
		return nil, err
	}
	return scanMutationRows(rows)
}

func (r *SQLMutationRepository) ListByType(itemType string) ([]Record, error) {
	rows, err := r.db.QueryContext(context.Background(), "SELECT data FROM "+localVaultItemsTable+" WHERE type = ?", itemType)
	if err != nil {
		return nil, err
	}
	return scanMutationRows(rows)
}

func (r *SQLMutationRepository) Get(id string) (Record, error) {
	var data []byte
	err := r.db.QueryRowContext(context.Background(), "SELECT data FROM "+localVaultItemsTable+" WHERE id = ?", id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}
	return decodeMutationRecord(data)
}

func (r *SQLMutationRepository) Put(record Record) (err error) {
	if r == nil || r.db == nil {
		return errors.New("vault item repository is nil")
	}
	record.SyncState = syncStateLocal
	if err := validateMutationRecord(record); err != nil {
		return err
	}
	operationID, err := replicationsync.NewOperationID()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	recordJSON, snapshotJSON, err := marshalMutationRecord(record)
	if err != nil {
		return err
	}
	if len(snapshotJSON) > replicationsync.MaxRecordSnapshotBytes {
		return fmt.Errorf("vault item snapshot exceeds %d bytes", replicationsync.MaxRecordSnapshotBytes)
	}
	tx, err := r.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	operation := replicationsync.OperationCreate
	var existingType string
	lookupErr := tx.QueryRow("SELECT type FROM "+localVaultItemsTable+" WHERE id = ?", record.ID).Scan(&existingType)
	if lookupErr == nil {
		if existingType != record.Type {
			return errors.New("vault item type cannot change")
		}
		operation = replicationsync.OperationUpdate
	} else if !errors.Is(lookupErr, sql.ErrNoRows) {
		return lookupErr
	}
	version, err := nextMutationVersion(tx, record.ID, record.Type)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO `+localVaultItemsTable+` (id, type, data) VALUES (?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET type = excluded.type, data = excluded.data
		WHERE `+localVaultItemsTable+`.type = excluded.type`, record.ID, record.Type, recordJSON); err != nil {
		return err
	}
	if err = insertMutation(tx, operationID, record.ID, record.Type, version, operation,
		snapshotJSON, uint32(record.KeyVersion), false, now); err != nil {
		return err
	}
	return tx.Commit()
}

// Delete missing is an idempotent no-op and does not invent a tombstone.
func (r *SQLMutationRepository) Delete(id string) (err error) {
	if r == nil || r.db == nil {
		return errors.New("vault item repository is nil")
	}
	operationID, err := replicationsync.NewOperationID()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	tx, err := r.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var data []byte
	lookupErr := tx.QueryRow("SELECT data FROM "+localVaultItemsTable+" WHERE id = ?", id).Scan(&data)
	if errors.Is(lookupErr, sql.ErrNoRows) {
		_ = tx.Rollback()
		return nil
	}
	if lookupErr != nil {
		return lookupErr
	}
	record, err := decodeMutationRecord(data)
	if err != nil {
		return err
	}
	version, err := nextMutationVersion(tx, record.ID, record.Type)
	if err != nil {
		return err
	}
	tombstone, err := json.Marshal(struct {
		RecordID   string `json:"record_id"`
		RecordType string `json:"record_type"`
		Deleted    bool   `json:"deleted"`
	}{record.ID, record.Type, true})
	if err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM "+localVaultItemsTable+" WHERE id = ?", record.ID); err != nil {
		return err
	}
	if err = insertMutation(tx, operationID, record.ID, record.Type, version,
		replicationsync.OperationDelete, tombstone, uint32(record.KeyVersion), true, now); err != nil {
		return err
	}
	return tx.Commit()
}

func scanMutationRows(rows *sql.Rows) ([]Record, error) {
	defer rows.Close()
	records := make([]Record, 0)
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		record, err := decodeMutationRecord(data)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// DecodeRecord parses an at-rest vault item Record JSON (e.g. a captured remote
// conflict snapshot) into a Record. The payload stays an encrypted envelope.
func DecodeRecord(data []byte) (Record, error) {
	return decodeMutationRecord(data)
}

func decodeMutationRecord(data []byte) (Record, error) {
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return Record{}, err
	}
	return record, nil
}

func validateMutationRecord(record Record) error {
	if record.ID == "" || len(record.ID) > 256 || !IsSupportedItemType(record.Type) {
		return ErrInvalidRecordSchema
	}
	if record.CreatedAt.IsZero() || record.UpdatedAt.IsZero() || record.UpdatedAt.Before(record.CreatedAt) {
		return errors.New("vault item timestamps are invalid")
	}
	if record.KeyVersion <= 0 || uint64(record.KeyVersion) > uint64(^uint32(0)) {
		return errors.New("vault item key version is invalid")
	}
	return ValidateRecordSchema(record.Type, record.Metadata, record.Payload)
}

// MarshalForResolution builds the (full record JSON, sync snapshot JSON) pair a
// conflict resolution needs to re-materialise a chosen local record at
// remote_head + 1. It forces sync_state = local like the normal write path and
// enforces the same snapshot size bound. The snapshot excludes sync_state and
// carries only already-encrypted payload — never plaintext.
func MarshalForResolution(record Record) (recordJSON, snapshotJSON []byte, err error) {
	record.SyncState = syncStateLocal
	if err := validateMutationRecord(record); err != nil {
		return nil, nil, err
	}
	recordJSON, snapshotJSON, err = marshalMutationRecord(record)
	if err != nil {
		return nil, nil, err
	}
	if len(snapshotJSON) > replicationsync.MaxRecordSnapshotBytes {
		return nil, nil, fmt.Errorf("vault item snapshot exceeds %d bytes", replicationsync.MaxRecordSnapshotBytes)
	}
	return recordJSON, snapshotJSON, nil
}

func marshalMutationRecord(record Record) ([]byte, []byte, error) {
	recordJSON, err := json.Marshal(record)
	if err != nil {
		return nil, nil, err
	}
	snapshotJSON, err := json.Marshal(struct {
		ID         string           `json:"id"`
		Type       string           `json:"type"`
		Metadata   json.RawMessage  `json:"metadata"`
		Payload    *crypto.Envelope `json:"payload"`
		CreatedAt  time.Time        `json:"created_at"`
		UpdatedAt  time.Time        `json:"updated_at"`
		KeyVersion int              `json:"key_version"`
	}{record.ID, record.Type, record.Metadata, record.Payload, record.CreatedAt, record.UpdatedAt, record.KeyVersion})
	return recordJSON, snapshotJSON, err
}

func nextMutationVersion(tx *sql.Tx, recordID, recordType string) (uint32, error) {
	var storedType string
	var version int64
	err := tx.QueryRow("SELECT record_type, entity_version FROM "+localEntitiesTable+" WHERE record_id = ?", recordID).Scan(&storedType, &version)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRow("SELECT record_type, entity_version FROM "+accountEntitiesTable+" WHERE record_id = ? ORDER BY entity_version DESC LIMIT 1", recordID).Scan(&storedType, &version)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	if storedType != recordType || version <= 0 || version >= int64(^uint32(0)) {
		return 0, errors.New("local entity version is invalid")
	}
	return uint32(version + 1), nil
}

func insertMutation(tx *sql.Tx, operationID, recordID, recordType string, version uint32,
	operation replicationsync.Operation, snapshot []byte, keyVersion uint32, tombstone bool, now time.Time,
) error {
	tombstoneValue := 0
	if tombstone {
		tombstoneValue = 1
	}
	if _, err := tx.Exec(`INSERT INTO `+localEntitiesTable+` (
		record_id, record_type, entity_version, operation_id, operation, tombstone, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(record_id) DO UPDATE SET record_type=excluded.record_type,
		entity_version=excluded.entity_version, operation_id=excluded.operation_id,
		operation=excluded.operation, tombstone=excluded.tombstone, updated_at=excluded.updated_at`,
		recordID, recordType, version, operationID, string(operation), tombstoneValue, now.Format(time.RFC3339Nano)); err != nil {
		return err
	}
	_, err := tx.Exec(`INSERT INTO `+localMutationsTable+` (
		operation_id, record_id, record_type, entity_version, operation,
		snapshot, key_version, key_id, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, operationID, recordID, recordType,
		version, string(operation), snapshot, keyVersion, localSnapshotKeyID, now.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("capture local sync mutation: %w", err)
	}
	return nil
}
