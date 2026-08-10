package syncclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"time"

	"changeme/internal/crypto"
	replicationsync "changeme/internal/sync"
	"changeme/internal/syncstore"
	"changeme/internal/vaultitem"
)

// SyncNow pushes the current outbox, pulls one bounded page, decrypts and
// validates each operation, then atomically applies it with the next cursor.
func (s *Service) SyncNow(ctx context.Context) (Report, error) {
	var report Report
	if s == nil {
		return report, ErrInvalidDependencies
	}
	if !s.runMu.TryLock() {
		return report, ErrSyncInProgress
	}
	defer s.runMu.Unlock()
	ctx = nonNilContext(ctx)
	if err := ctx.Err(); err != nil {
		return report, err
	}

	current, session, ok := s.snapshot()
	if !ok {
		return report, ErrNotConnected
	}
	if err := s.deps.Remote.BindAuthenticatedAccount(ctx, session.AccountID()); err != nil {
		logSyncStageFailure("bind_account", err)
		return report, safeStageError(ctx, ErrLocalCaptureFailed)
	}
	if err := s.materializeLocalMutations(ctx, session); err != nil {
		logSyncStageFailure("prepare_local_changes", err)
		return report, safeStageError(ctx, ErrLocalCaptureFailed)
	}
	push, err := replicationsync.NewRunner(s.deps.Outbox, current.transport).Run(ctx, s.deps.PushLimit)
	report.PushAttempted = push.Attempted
	report.PushSent = push.Sent
	report.PushRetried = push.Retried
	report.PushConflicts = push.Conflicts
	if err != nil {
		logSyncStageFailure("push", err)
		if errors.Is(err, replicationsync.ErrAuthentication) {
			s.clearConnection(current)
		}
		return report, safeStageError(ctx, ErrPushFailed)
	}

	cursor, err := s.deps.Cursors.GetCursor(ctx, s.deps.CursorName)
	if err != nil {
		logSyncStageFailure("load_cursor", err)
		return report, safeStageError(ctx, ErrPullFailed)
	}
	pulled, err := current.transport.Pull(ctx, cursor, s.deps.PullLimit)
	if err != nil {
		logSyncStageFailure("pull", err)
		if errors.Is(err, replicationsync.ErrAuthentication) {
			s.clearConnection(current)
		}
		return report, safeStageError(ctx, ErrPullFailed)
	}
	if len(pulled.Operations) > s.deps.PullLimit || pulled.NextCursor.Validate() != nil {
		logSyncStageFailure("validate_pull", replicationsync.ErrInvalidTransportResponse)
		return report, ErrInvalidRemoteData
	}
	report.Pulled = len(pulled.Operations)

	mutations := make([]syncstore.RemoteMutation, 0, len(pulled.Operations))
	defer func() { zeroMutations(mutations) }()
	for _, operation := range pulled.Operations {
		if err := ctx.Err(); err != nil {
			logSyncStageFailure("decrypt_pull", err)
			return report, err
		}
		mutation, err := s.decryptMutation(operation, session.AccountID())
		if err != nil {
			logSyncStageFailure("decrypt_pull", err)
			zeroMutations(mutations)
			return report, err
		}
		mutations = append(mutations, mutation)
	}
	if err := s.deps.Remote.ApplyRemoteBatch(ctx, session.AccountID(), s.deps.CursorName, pulled.NextCursor.Token, mutations); err != nil {
		logSyncStageFailure("apply_pull", err)
		return report, safeStageError(ctx, ErrApplyFailed)
	}
	report.Applied = len(mutations)
	return report, nil
}

// logSyncStageFailure records only the sync stage and a deliberately small
// failure category. Raw HTTP errors may contain endpoints, tokens, cursors or
// encrypted payload data, so they must never be emitted by the desktop app.
func logSyncStageFailure(stage string, err error) {
	slog.Warn("Sync operation failed", "stage", stage, "reason", safeFailureReason(err))
}

func safeFailureReason(err error) string {
	if err == nil {
		return "unknown"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, replicationsync.ErrAuthentication) {
		return "authentication"
	}
	if errors.Is(err, replicationsync.ErrInvalidTransportResponse) {
		return string(replicationsync.FailureInvalidResponse)
	}
	var reasonProvider interface {
		FailureReason() replicationsync.FailureReason
	}
	if errors.As(err, &reasonProvider) {
		return string(reasonProvider.FailureReason())
	}
	return string(replicationsync.FailureUnknown)
}

func (s *Service) materializeLocalMutations(ctx context.Context, session replicationsync.AuthSession) error {
	if s.deps.LocalMutations == nil {
		return nil
	}
	intents, err := s.deps.LocalMutations.Pending(ctx, s.deps.PushLimit)
	if err != nil {
		return err
	}
	for i := range intents {
		intent := intents[i]
		if err := ctx.Err(); err != nil {
			zeroBytes(intent.Snapshot)
			return err
		}
		change, queueErr := replicationsync.QueuePayload(
			s.deps.Vault,
			s.deps.Outbox,
			replicationsync.SyncContext{
				AccountID:       session.AccountID(),
				DeviceID:        session.DeviceID(),
				ProtocolVersion: replicationsync.SyncProtocolVersion,
				OperationID:     intent.OperationID,
				EntityVersion:   intent.EntityVersion,
			},
			intent.RecordID,
			intent.RecordType,
			intent.KeyID,
			intent.KeyVersion,
			intent.Operation,
			intent.Snapshot,
			intent.CreatedAt,
		)
		zeroBytes(intent.Snapshot)
		if queueErr != nil {
			return queueErr
		}
		if err := s.deps.LocalMutations.CommitMaterialized(ctx, session.AccountID(), intent, change); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) decryptMutation(operation replicationsync.PushOperation, accountID string) (syncstore.RemoteMutation, error) {
	if err := operation.Validate(); err != nil || operation.Envelope.AccountID != accountID || operation.Envelope.ProtocolVersion != replicationsync.SyncProtocolVersion {
		return syncstore.RemoteMutation{}, ErrInvalidRemoteData
	}
	if !vaultitem.IsSupportedItemType(operation.Envelope.RecordType) {
		return syncstore.RemoteMutation{}, ErrInvalidRemoteData
	}

	// The nested envelope's key ID is authenticated by OpenPayload's AAD. It is
	// parsed here only to select that authenticated key identity; no key bytes
	// leave the vault.
	var nested crypto.Envelope
	if err := json.Unmarshal(operation.Envelope.Ciphertext, &nested); err != nil || nested.Validate() != nil {
		return syncstore.RemoteMutation{}, ErrInvalidRemoteData
	}
	plaintext, err := replicationsync.OpenPayload(s.deps.Vault, operation.Envelope, nested.KeyID)
	if err != nil {
		return syncstore.RemoteMutation{}, ErrDecryptFailed
	}
	defer zeroBytes(plaintext)
	if len(plaintext) == 0 || len(plaintext) > maxRecordJSONSize {
		return syncstore.RemoteMutation{}, ErrInvalidRemoteData
	}

	mutation := syncstore.RemoteMutation{
		AccountID:     accountID,
		RecordID:      operation.Envelope.RecordID,
		RecordType:    operation.Envelope.RecordType,
		Operation:     operation.Operation,
		OperationID:   operation.Envelope.OperationID,
		EntityVersion: operation.Envelope.EntityVersion,
	}
	switch operation.Operation {
	case replicationsync.OperationCreate, replicationsync.OperationUpdate:
		normalized, innerPayload, err := normalizeSnapshot(plaintext, operation.Envelope)
		if err != nil {
			return syncstore.RemoteMutation{}, ErrInvalidRemoteData
		}
		// Authenticate the nested module payload before persistence. Structural
		// validation alone cannot detect a payload copied from another record or
		// an invalid authentication tag produced by a faulty authorized device.
		innerPlaintext, err := s.deps.Vault.OpenWithAD(*innerPayload, crypto.AssociatedData{
			RecordType: operation.Envelope.RecordType,
			RecordID:   operation.Envelope.RecordID,
			KeyID:      innerPayload.KeyID,
		})
		if err != nil {
			return syncstore.RemoteMutation{}, ErrDecryptFailed
		}
		zeroBytes(innerPlaintext)
		mutation.RecordJSON = normalized
	case replicationsync.OperationDelete:
		if !validTombstone(plaintext, mutation.RecordID, mutation.RecordType) {
			return syncstore.RemoteMutation{}, ErrInvalidRemoteData
		}
	default:
		return syncstore.RemoteMutation{}, ErrInvalidRemoteData
	}
	return mutation, nil
}

// snapshot is the complete cross-device representation of a vault item. It
// deliberately excludes local-only fields such as sync_state; those fields
// are derived by this device after the authenticated snapshot is accepted.
type snapshot struct {
	ID         string           `json:"id"`
	Type       string           `json:"type"`
	Metadata   json.RawMessage  `json:"metadata"`
	Payload    *crypto.Envelope `json:"payload,omitempty"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
	KeyVersion uint32           `json:"key_version"`
}

type localRecord struct {
	ID         string           `json:"id"`
	Type       string           `json:"type"`
	Metadata   json.RawMessage  `json:"metadata"`
	Payload    *crypto.Envelope `json:"payload,omitempty"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
	KeyVersion uint32           `json:"key_version"`
	SyncState  string           `json:"sync_state"`
}

func normalizeSnapshot(payload []byte, outer replicationsync.ReplicationEnvelope) ([]byte, *crypto.Envelope, error) {
	var remote snapshot
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&remote); err != nil {
		return nil, nil, ErrInvalidRemoteData
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, nil, ErrInvalidRemoteData
	}
	if remote.ID != outer.RecordID || remote.Type != outer.RecordType ||
		!json.Valid(remote.Metadata) || remote.CreatedAt.IsZero() || remote.UpdatedAt.IsZero() ||
		remote.UpdatedAt.Before(remote.CreatedAt) || remote.KeyVersion == 0 || remote.KeyVersion != outer.KeyVersion {
		return nil, nil, ErrInvalidRemoteData
	}
	if err := vaultitem.ValidateRecordSchema(remote.Type, remote.Metadata, remote.Payload); err != nil {
		return nil, nil, ErrInvalidRemoteData
	}

	normalized, err := json.Marshal(localRecord{
		ID:         remote.ID,
		Type:       remote.Type,
		Metadata:   append(json.RawMessage(nil), remote.Metadata...),
		Payload:    remote.Payload,
		CreatedAt:  remote.CreatedAt,
		UpdatedAt:  remote.UpdatedAt,
		KeyVersion: remote.KeyVersion,
		SyncState:  "synced",
	})
	if err != nil {
		return nil, nil, ErrInvalidRemoteData
	}
	return normalized, remote.Payload, nil
}

func validTombstone(payload []byte, recordID, recordType string) bool {
	var tombstone struct {
		RecordID   string `json:"record_id"`
		RecordType string `json:"record_type"`
		Deleted    bool   `json:"deleted"`
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&tombstone); err != nil {
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return false
	}
	return tombstone.Deleted && tombstone.RecordID == recordID && tombstone.RecordType == recordType
}

func zeroMutations(mutations []syncstore.RemoteMutation) {
	for i := range mutations {
		zeroBytes(mutations[i].RecordJSON)
		mutations[i].RecordJSON = nil
	}
}

func zeroBytes(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
