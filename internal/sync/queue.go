package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const queueIdempotencyNamespace = "dev-kit/sync/queue/v2"

// QueuePayload seals one complete record payload and adds it to the local
// outbox. The outbox only receives the serialized replication envelope, never
// the plaintext record bytes.
//
// The idempotency key is derived from the caller-supplied operation_id.
// Encryption is intentionally performed only when the logical change is not
// already queued because SealPayload uses a fresh nonce for every encryption
// attempt.
func QueuePayload(
	vault Vault,
	outbox Outbox,
	ctx SyncContext,
	recordID, recordType, keyID string,
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
	if err := ctx.Validate(); err != nil {
		return Change{}, err
	}
	if err := validateQueueInputs(ctx, recordID, recordType, keyID, keyVersion, operation, plaintextRecord, createdAt); err != nil {
		return Change{}, err
	}

	// Local-only digest for idempotency; must not be sent on the wire as plaintext
	// (see Change.ContentDigest).
	digest := sha256.Sum256(plaintextRecord)
	contentDigest := hex.EncodeToString(digest[:])
	idempotencyKey := queueIdempotencyKey(ctx.OperationID)

	if existing, err := outbox.Get(idempotencyKey); err == nil {
		if !sameQueueIdentity(existing, ctx, recordID, recordType, keyID, keyVersion, operation, contentDigest) {
			return Change{}, ErrIdempotencyConflict
		}
		return cloneChange(existing), nil
	} else if !errors.Is(err, ErrNotFound) {
		return Change{}, err
	}

	envelope, err := SealPayload(vault, ctx, recordID, recordType, keyID, keyVersion, plaintextRecord)
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
		ctx,
		keyID,
		contentDigest,
		createdAt,
	)
	if err := outbox.Enqueue(change); err != nil {
		// Another caller may have queued the same logical change after the
		// initial lookup. Since encryption is nonce-randomized, its ciphertext
		// will differ even though the logical idempotency key is the same.
		if errors.Is(err, ErrIdempotencyConflict) {
			existing, getErr := outbox.Get(idempotencyKey)
			if getErr == nil && sameQueueIdentity(existing, ctx, recordID, recordType, keyID, keyVersion, operation, contentDigest) {
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

func validateQueueInputs(ctx SyncContext, recordID, recordType, keyID string, keyVersion uint32, operation Operation, plaintextRecord []byte, createdAt time.Time) error {
	if err := validateReplicationInputs(ctx, recordID, recordType, keyID, keyVersion, plaintextRecord); err != nil {
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

func queueIdempotencyKey(operationID string) string {
	canonical := queueIdempotencyNamespace + "\x00" + operationID
	sum := sha256.Sum256([]byte(canonical))
	return queueIdempotencyNamespace + ":" + hex.EncodeToString(sum[:])
}

func sameQueueIdentity(change Change, ctx SyncContext, recordID, recordType, keyID string, keyVersion uint32, operation Operation, contentDigest string) bool {
	return change.RecordID == recordID &&
		change.RecordType == recordType &&
		change.KeyVersion == keyVersion &&
		change.Operation == operation &&
		change.KeyID == keyID &&
		change.ContentDigest == contentDigest &&
		change.AccountID == ctx.AccountID &&
		change.DeviceID == ctx.DeviceID &&
		change.ProtocolVersion == ctx.ProtocolVersion &&
		change.OperationID == ctx.OperationID &&
		change.EntityVersion == ctx.EntityVersion
}
