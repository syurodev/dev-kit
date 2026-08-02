// Package sync contains the local-first replication contract.
//
// The package deliberately models ciphertext as opaque bytes. It does not
// know how records are encrypted and must not be used to carry plaintext
// record data.
package sync

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

const SyncProtocolVersion uint32 = 1

type SyncContext struct {
	AccountID       string
	DeviceID        string
	ProtocolVersion uint32
	OperationID     string
	EntityVersion   uint32
}

func (c SyncContext) Validate() error {
	if err := validateIdentifier("account id", c.AccountID, 256); err != nil {
		return err
	}
	if err := validateIdentifier("device id", c.DeviceID, 256); err != nil {
		return err
	}
	if c.ProtocolVersion != SyncProtocolVersion {
		return fmt.Errorf("unsupported sync protocol version %d", c.ProtocolVersion)
	}
	if err := validateIdentifier("operation id", c.OperationID, 256); err != nil {
		return err
	}
	if c.EntityVersion == 0 {
		return errors.New("entity version must be positive")
	}
	return nil
}

func NewOperationID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// Operation identifies the change applied to a replicated record.
type Operation string

const (
	OperationCreate Operation = "create"
	OperationUpdate Operation = "update"
	OperationDelete Operation = "delete"
)

// Status identifies the local delivery state of a change.
type Status string

const (
	StatusPending  Status = "pending"
	StatusSyncing  Status = "syncing"
	StatusSynced   Status = "synced"
	StatusFailed   Status = "failed"
	StatusConflict Status = "conflict"
)

// FailureReason is the small, non-sensitive error vocabulary persisted in the
// outbox. Raw transport or service errors must not be persisted here.
type FailureReason string

const (
	FailureNetwork         FailureReason = "network"
	FailureTimeout         FailureReason = "timeout"
	FailureInvalidResponse FailureReason = "invalid_response"
	FailureUnknown         FailureReason = "unknown"
)

// Cursor is an opaque replication position. An empty cursor represents the
// initial position and is valid.
type Cursor struct {
	Token string
}

func (c Cursor) Validate() error {
	if len(c.Token) > 512 {
		return errors.New("cursor token is too long")
	}
	if hasControl(c.Token) {
		return errors.New("cursor token contains control characters")
	}
	return nil
}

// Conflict records safe identifiers for a conflict. It intentionally has no
// plaintext record payload or remote error text.
type Conflict struct {
	ID              string
	RecordID        string
	RecordType      string
	LocalChangeKey  string
	RemoteChangeKey string
	DetectedAt      time.Time
}

func (c Conflict) Validate() error {
	if err := validateIdentifier("conflict id", c.ID, 256); err != nil {
		return err
	}
	if err := validateIdentifier("record id", c.RecordID, 256); err != nil {
		return err
	}
	if err := validateIdentifier("record type", c.RecordType, 128); err != nil {
		return err
	}
	if err := validateIdentifier("local change key", c.LocalChangeKey, 256); err != nil {
		return err
	}
	if err := validateIdentifier("remote change key", c.RemoteChangeKey, 256); err != nil {
		return err
	}
	if c.LocalChangeKey == c.RemoteChangeKey {
		return errors.New("local and remote change keys must differ")
	}
	if c.DetectedAt.IsZero() {
		return errors.New("conflict detected time is required")
	}
	return nil
}

// Change is an encrypted local replication operation. Payload must contain
// ciphertext (including for deletes); this type never interprets its bytes.
type Change struct {
	IdempotencyKey  string
	RecordID        string
	RecordType      string
	Operation       Operation
	Payload         []byte
	KeyVersion      uint32
	AccountID       string
	DeviceID        string
	ProtocolVersion uint32
	OperationID     string
	EntityVersion   uint32
	KeyID           string
	ContentDigest   string
	CreatedAt       time.Time
	Status          Status
	Attempts        int
	NextAttemptAt   time.Time
	LastError       FailureReason
	Conflict        *Conflict
	SyncingSince    time.Time
}

// NewChange creates a pending change with safe defaults.
func NewChange(
	idempotencyKey, recordID, recordType string,
	operation Operation,
	payload []byte,
	keyVersion uint32,
	ctx SyncContext,
	keyID, contentDigest string,
	createdAt time.Time,
) Change {
	return Change{
		IdempotencyKey:  idempotencyKey,
		RecordID:        recordID,
		RecordType:      recordType,
		Operation:       operation,
		Payload:         append([]byte(nil), payload...),
		KeyVersion:      keyVersion,
		AccountID:       ctx.AccountID,
		DeviceID:        ctx.DeviceID,
		ProtocolVersion: ctx.ProtocolVersion,
		OperationID:     ctx.OperationID,
		EntityVersion:   ctx.EntityVersion,
		KeyID:           keyID,
		ContentDigest:   contentDigest,
		CreatedAt:       createdAt,
		Status:          StatusPending,
		NextAttemptAt:   createdAt,
	}
}

// Validate checks both the immutable envelope and its public state. Empty
// status/next-attempt values are accepted for ergonomic construction and are
// filled by MemoryOutbox.Enqueue.
func (c Change) Validate() error {
	if err := validateIdentifier("idempotency key", c.IdempotencyKey, 256); err != nil {
		return err
	}
	if err := validateIdentifier("record id", c.RecordID, 256); err != nil {
		return err
	}
	if err := validateIdentifier("record type", c.RecordType, 128); err != nil {
		return err
	}
	if c.Operation != OperationCreate && c.Operation != OperationUpdate && c.Operation != OperationDelete {
		return fmt.Errorf("invalid operation %q", c.Operation)
	}
	if len(c.Payload) == 0 {
		return errors.New("encrypted payload is required")
	}
	if c.KeyVersion == 0 {
		return errors.New("key version must be positive")
	}
	ctx := SyncContext{
		AccountID: c.AccountID, DeviceID: c.DeviceID,
		ProtocolVersion: c.ProtocolVersion, OperationID: c.OperationID,
		EntityVersion: c.EntityVersion,
	}
	if err := ctx.Validate(); err != nil {
		return err
	}
	if err := validateIdentifier("key id", c.KeyID, 256); err != nil {
		return err
	}
	if err := validateIdentifier("content digest", c.ContentDigest, 128); err != nil {
		return err
	}
	if c.CreatedAt.IsZero() {
		return errors.New("created time is required")
	}
	if c.Attempts < 0 {
		return errors.New("attempts cannot be negative")
	}
	if c.Status != "" && !validStatus(c.Status) {
		return fmt.Errorf("invalid status %q", c.Status)
	}
	if c.LastError != "" && !validFailureReason(c.LastError) {
		return fmt.Errorf("invalid failure reason %q", c.LastError)
	}
	if c.Status == StatusConflict {
		if c.Conflict == nil {
			return errors.New("conflict details are required")
		}
		if err := c.Conflict.Validate(); err != nil {
			return err
		}
	} else if c.Conflict != nil {
		return errors.New("conflict details require conflict status")
	}
	return nil
}

func validStatus(status Status) bool {
	switch status {
	case StatusPending, StatusSyncing, StatusSynced, StatusFailed, StatusConflict:
		return true
	default:
		return false
	}
}

func validFailureReason(reason FailureReason) bool {
	switch reason {
	case FailureNetwork, FailureTimeout, FailureInvalidResponse, FailureUnknown:
		return true
	default:
		return false
	}
}

func validateIdentifier(label, value string, max int) error {
	if value == "" {
		return fmt.Errorf("%s is required", label)
	}
	if len(value) > max {
		return fmt.Errorf("%s is too long", label)
	}
	if hasControl(value) {
		return fmt.Errorf("%s contains control characters", label)
	}
	return nil
}

func hasControl(value string) bool {
	return strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) }) >= 0
}
