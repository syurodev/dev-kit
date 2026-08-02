package sync

import (
	"context"
	"errors"
	"sort"
	stdsync "sync"
)

// Transport delivers opaque encrypted changes. Implementations must not
// decrypt, parse, or otherwise interpret Change.Payload.
type Transport interface {
	Push(ctx context.Context, changes []Change) (PushResult, error)
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
	received       map[string]Change
	forcedError    FailureReason
	forcedConflict map[string]Conflict
}

func NewMemoryTransport(config ...MemoryTransportConfig) *MemoryTransport {
	transport := &MemoryTransport{
		received:       make(map[string]Change),
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
func (t *MemoryTransport) Received() []Change {
	t.mu.RLock()
	defer t.mu.RUnlock()
	keys := make([]string, 0, len(t.received))
	for key := range t.received {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	changes := make([]Change, 0, len(keys))
	for _, key := range keys {
		changes = append(changes, cloneChange(t.received[key]))
	}
	return changes
}

func (t *MemoryTransport) Push(ctx context.Context, changes []Change) (PushResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return PushResult{}, err
	}

	// Validate only the public envelope and state. Payload remains opaque; a
	// non-empty payload is enough for this transport foundation.
	seen := make(map[string]struct{}, len(changes))
	for _, change := range changes {
		if err := change.Validate(); err != nil {
			return PushResult{}, TransportError{Reason: FailureInvalidResponse}
		}
		if _, ok := seen[change.IdempotencyKey]; ok {
			return PushResult{}, TransportError{Reason: FailureInvalidResponse}
		}
		seen[change.IdempotencyKey] = struct{}{}
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.forcedError != "" {
		return PushResult{}, TransportError{Reason: t.forcedError}
	}

	result := PushResult{
		SentIdempotencyKeys: make([]string, 0, len(changes)),
		Conflicts:           make([]Conflict, 0),
	}
	for _, change := range changes {
		if err := ctx.Err(); err != nil {
			return PushResult{}, err
		}
		if conflict, ok := t.forcedConflict[change.IdempotencyKey]; ok {
			result.Conflicts = append(result.Conflicts, cloneConflict(conflict))
			continue
		}
		if existing, ok := t.received[change.IdempotencyKey]; ok {
			if !sameTransportChange(existing, change) {
				return PushResult{}, TransportError{Reason: FailureInvalidResponse}
			}
			result.SentIdempotencyKeys = append(result.SentIdempotencyKeys, change.IdempotencyKey)
			continue
		}
		t.received[change.IdempotencyKey] = cloneChange(change)
		result.SentIdempotencyKeys = append(result.SentIdempotencyKeys, change.IdempotencyKey)
	}
	return result, nil
}

func sameTransportChange(a, b Change) bool {
	return sameImmutableChange(a, b)
}

func cloneConflict(conflict Conflict) Conflict {
	return conflict
}
