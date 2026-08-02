package sync

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	stdsync "sync"
)

// ErrAuthentication indicates that the remote endpoint rejected or can no
// longer use the runtime-only authenticated session. Callers should discard
// the session and require an explicit reconnect; no response body is exposed.
var ErrAuthentication = errors.New("sync authentication failed")

// PushOperation is the explicit wire boundary for sync uploads. It omits all
// local-only outbox state, including retry counters, failure state, leases, and
// plaintext-derived comparison material.
type PushOperation struct {
	IdempotencyKey string              `json:"idempotency_key"`
	Operation      Operation           `json:"operation"`
	Envelope       ReplicationEnvelope `json:"envelope"`
}

func (o PushOperation) Validate() error {
	if err := validateIdentifier("idempotency key", o.IdempotencyKey, 256); err != nil {
		return err
	}
	if !validOperation(o.Operation) {
		return errors.New("invalid sync operation")
	}
	if err := o.Envelope.Validate(); err != nil {
		return err
	}
	if o.Envelope.Operation != o.Operation {
		return errors.New("operation does not match encrypted envelope")
	}
	return nil
}

func pushOperationFromChange(change Change) (PushOperation, error) {
	if err := change.Validate(); err != nil {
		return PushOperation{}, err
	}
	var envelope ReplicationEnvelope
	if err := json.Unmarshal(change.Payload, &envelope); err != nil {
		return PushOperation{}, errors.New("change contains an invalid replication envelope")
	}
	operation := PushOperation{
		IdempotencyKey: change.IdempotencyKey,
		Operation:      change.Operation,
		Envelope:       cloneReplicationEnvelope(envelope),
	}
	if err := operation.Validate(); err != nil {
		return PushOperation{}, err
	}
	if envelope.RecordID != change.RecordID || envelope.RecordType != change.RecordType ||
		envelope.KeyVersion != change.KeyVersion || envelope.AccountID != change.AccountID ||
		envelope.DeviceID != change.DeviceID || envelope.ProtocolVersion != change.ProtocolVersion ||
		envelope.OperationID != change.OperationID || envelope.EntityVersion != change.EntityVersion {
		return PushOperation{}, errors.New("change metadata does not match encrypted envelope")
	}
	return operation, nil
}

// Transport delivers only wire-safe encrypted operations. Implementations must
// not decrypt or interpret PushOperation.Envelope.Ciphertext.
type Transport interface {
	Push(ctx context.Context, operations []PushOperation) (PushResult, error)
}

// PushResult contains only acknowledgements and safe conflict metadata. A
// change is not considered delivered unless its idempotency key is included
// in SentIdempotencyKeys or it has a matching entry in Conflicts.
type PushResult struct {
	SentIdempotencyKeys []string
	Conflicts           []Conflict
}

// TransportError is a safe transport error for local implementations and
// tests. The runner persists only its FailureReason, never the error text from
// an underlying transport.
type TransportError struct {
	Reason FailureReason
}

func (e TransportError) Error() string {
	return "sync transport error"
}

func (e TransportError) FailureReason() FailureReason {
	if validFailureReason(e.Reason) {
		return e.Reason
	}
	return FailureUnknown
}

// MemoryTransportConfig configures deterministic failures for tests. Forced
// conflicts are keyed by Conflict.LocalChangeKey and contain only safe
// identifiers; the transport never receives plaintext record data.
type MemoryTransportConfig struct {
	ForcedError     FailureReason
	ForcedConflicts []Conflict
}

// MemoryTransport is a thread-safe, in-memory transport foundation. It
// acknowledges a repeated identical ciphertext change without creating a
// duplicate remote entry. It makes no network calls and does not decrypt the
// payload.
type MemoryTransport struct {
	mu             stdsync.RWMutex
	received       map[string]PushOperation
	forcedError    FailureReason
	forcedConflict map[string]Conflict
}

func NewMemoryTransport(config ...MemoryTransportConfig) *MemoryTransport {
	transport := &MemoryTransport{
		received:       make(map[string]PushOperation),
		forcedConflict: make(map[string]Conflict),
	}
	if len(config) > 0 {
		transport.forcedError = config[0].ForcedError
		for _, conflict := range config[0].ForcedConflicts {
			transport.forcedConflict[conflict.LocalChangeKey] = cloneConflict(conflict)
		}
	}
	return transport
}

// SetForcedError configures a safe error returned before processing a batch.
// Passing an empty reason clears the forced error.
func (t *MemoryTransport) SetForcedError(reason FailureReason) error {
	if reason != "" && !validFailureReason(reason) {
		return errors.New("invalid failure reason")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.forcedError = reason
	return nil
}

// SetForcedConflict configures a safe conflict for one local idempotency key.
func (t *MemoryTransport) SetForcedConflict(conflict Conflict) error {
	if err := conflict.Validate(); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.forcedConflict == nil {
		t.forcedConflict = make(map[string]Conflict)
	}
	t.forcedConflict[conflict.LocalChangeKey] = cloneConflict(conflict)
	return nil
}

func (t *MemoryTransport) ClearForcedConflicts() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.forcedConflict = make(map[string]Conflict)
}

// Received returns clone-safe changes accepted by the memory transport,
// ordered by idempotency key. It is intended for tests and diagnostics only.
func (t *MemoryTransport) Received() []PushOperation {
	t.mu.RLock()
	defer t.mu.RUnlock()
	keys := make([]string, 0, len(t.received))
	for key := range t.received {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	operations := make([]PushOperation, 0, len(keys))
	for _, key := range keys {
		operations = append(operations, clonePushOperation(t.received[key]))
	}
	return operations
}

func (t *MemoryTransport) Push(ctx context.Context, operations []PushOperation) (PushResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return PushResult{}, err
	}

	// Validate only the public envelope and state. Payload remains opaque; a
	// non-empty payload is enough for this transport foundation.
	seen := make(map[string]struct{}, len(operations))
	for _, operation := range operations {
		if err := operation.Validate(); err != nil {
			return PushResult{}, TransportError{Reason: FailureInvalidResponse}
		}
		if _, ok := seen[operation.IdempotencyKey]; ok {
			return PushResult{}, TransportError{Reason: FailureInvalidResponse}
		}
		seen[operation.IdempotencyKey] = struct{}{}
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.forcedError != "" {
		return PushResult{}, TransportError{Reason: t.forcedError}
	}

	result := PushResult{
		SentIdempotencyKeys: make([]string, 0, len(operations)),
		Conflicts:           make([]Conflict, 0),
	}
	for _, operation := range operations {
		if err := ctx.Err(); err != nil {
			return PushResult{}, err
		}
		if conflict, ok := t.forcedConflict[operation.IdempotencyKey]; ok {
			result.Conflicts = append(result.Conflicts, cloneConflict(conflict))
			continue
		}
		if existing, ok := t.received[operation.IdempotencyKey]; ok {
			if !sameTransportOperation(existing, operation) {
				return PushResult{}, TransportError{Reason: FailureInvalidResponse}
			}
			result.SentIdempotencyKeys = append(result.SentIdempotencyKeys, operation.IdempotencyKey)
			continue
		}
		t.received[operation.IdempotencyKey] = clonePushOperation(operation)
		result.SentIdempotencyKeys = append(result.SentIdempotencyKeys, operation.IdempotencyKey)
	}
	return result, nil
}

func sameTransportOperation(a, b PushOperation) bool {
	if a.IdempotencyKey != b.IdempotencyKey || a.Operation != b.Operation {
		return false
	}
	aJSON, errA := json.Marshal(a.Envelope)
	bJSON, errB := json.Marshal(b.Envelope)
	return errA == nil && errB == nil && string(aJSON) == string(bJSON)
}

func clonePushOperation(operation PushOperation) PushOperation {
	operation.Envelope = cloneReplicationEnvelope(operation.Envelope)
	return operation
}

func cloneReplicationEnvelope(envelope ReplicationEnvelope) ReplicationEnvelope {
	envelope.Ciphertext = append([]byte(nil), envelope.Ciphertext...)
	return envelope
}

func cloneConflict(conflict Conflict) Conflict {
	return conflict
}
