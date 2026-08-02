package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const queueIdempotencyNamespace = "dev-kit/sync/queue/v1"

// QueuePayload seals one complete record payload and adds it to the local
// outbox. The outbox only receives the serialized replication envelope, never
// the plaintext record bytes.
//
// The idempotency key is derived from the stable record identity, operation,
// and creation timestamp. Encryption is intentionally performed only when the
// logical change is not already queued because SealPayload uses a fresh nonce
// for every encryption attempt.
func QueuePayload(
	vault Vault,
	outbox Outbox,
	recordID string,
	recordType string,
	keyID string,
	keyVersion uint32,
	operation Operation,
	plaintextRecord []byte,
	createdAt time.Time,
) (Change, error) {
	if vault == nil {
		return Change{}, errors.New("vault is required")
	}
	if outbox == nil {
		return Change{}, errors.New("outbox is required")
	}
	if err := validateQueueInputs(recordID, recordType, keyID, keyVersion, operation, plaintextRecord, createdAt); err != nil {
		return Change{}, err
	}

	idempotencyKey := queueIdempotencyKey(recordID, operation, createdAt)
	if existing, err := outbox.Get(idempotencyKey); err == nil {
		if !sameQueueIdentity(existing, recordID, recordType, keyVersion, operation, createdAt) {
			return Change{}, ErrIdempotencyConflict
		}
		return cloneChange(existing), nil
	} else if !errors.Is(err, ErrNotFound) {
		return Change{}, err
	}

	envelope, err := SealPayload(vault, recordID, recordType, keyID, keyVersion, plaintextRecord)
	if err != nil {
		return Change{}, err
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return Change{}, errors.New("serialize replication envelope")
	}

	change := NewChange(
		idempotencyKey,
		recordID,
		recordType,
		operation,
		payload,
		keyVersion,
		SyncContext{
			AccountID:       "pending",
			DeviceID:        "pending",
			ProtocolVersion: SyncProtocolVersion,
			OperationID:     "pending",
			EntityVersion:   1,
		},
		keyID,
		"pending",
		createdAt,
	)
	if err := outbox.Enqueue(change); err != nil {
		// Another caller may have queued the same logical change after the
		// initial lookup. Since encryption is nonce-randomized, its ciphertext
		// will differ even though the logical idempotency key is the same.
		if errors.Is(err, ErrIdempotencyConflict) {
			existing, getErr := outbox.Get(idempotencyKey)
			if getErr == nil && sameQueueIdentity(existing, recordID, recordType, keyVersion, operation, createdAt) {
				return cloneChange(existing), nil
			}
		}
		return Change{}, err
	}

	// Get returns the outbox's clone-safe representation and also preserves
	// implementation-specific normalization such as pending timestamps.
	queued, err := outbox.Get(idempotencyKey)
	if err != nil {
		return Change{}, err
	}
	return cloneChange(queued), nil
}

func validateQueueInputs(recordID, recordType, keyID string, keyVersion uint32, operation Operation, plaintextRecord []byte, createdAt time.Time) error {
	if err := validateReplicationInputs(recordID, recordType, keyID, keyVersion, plaintextRecord); err != nil {
		return err
	}
	switch operation {
	case OperationCreate, OperationUpdate, OperationDelete:
	default:
		return fmt.Errorf("invalid operation %q", operation)
	}
	if createdAt.IsZero() {
		return errors.New("created time is required")
	}
	return nil
}

func queueIdempotencyKey(recordID string, operation Operation, createdAt time.Time) string {
	canonical := fmt.Sprintf("%s\x00%s\x00%s", queueIdempotencyNamespace, recordID, operationTimestamp(operation, createdAt))
	digest := sha256.Sum256([]byte(canonical))
	return queueIdempotencyNamespace + ":" + hex.EncodeToString(digest[:])
}

func operationTimestamp(operation Operation, createdAt time.Time) string {
	return string(operation) + "\x00" + createdAt.UTC().Format(time.RFC3339Nano)
}

func sameQueueIdentity(change Change, recordID, recordType string, keyVersion uint32, operation Operation, createdAt time.Time) bool {
	return change.RecordID == recordID &&
		change.RecordType == recordType &&
		change.KeyVersion == keyVersion &&
		change.Operation == operation &&
		change.CreatedAt.Equal(createdAt)
}
