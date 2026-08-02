package sync

import (
	"errors"
	"sort"
	stdsync "sync"
	"time"
)

const defaultOutboxLease = 5 * time.Minute

var (
	ErrNotFound            = errors.New("change not found")
	ErrIdempotencyConflict = errors.New("idempotency key already contains different change")
	ErrInvalidState        = errors.New("invalid change state transition")
	ErrNotReady            = errors.New("change is not ready for delivery")
)

// Outbox stores encrypted changes until a future transport delivers them.
// Implementations must not expose raw transport errors through Change.
type Outbox interface {
	Enqueue(change Change) error
	Get(idempotencyKey string) (Change, error)
	Pending(now time.Time, limit int) ([]Change, error)
	MarkSyncing(idempotencyKey string, at time.Time) error
	RecoverStaleSyncing(now time.Time) error
	MarkSent(idempotencyKey string) error
	MarkRetry(idempotencyKey string, nextAttemptAt time.Time, reason FailureReason) error
	MarkFailed(idempotencyKey string, reason FailureReason) error
	MarkConflict(idempotencyKey string, conflict Conflict) error
}

// MemoryOutboxConfig configures lease duration for stale syncing reclaim.
type MemoryOutboxConfig struct {
	Lease time.Duration
}

// MemoryOutbox is a thread-safe in-memory implementation for the local
// foundation and tests. It is intentionally ephemeral and makes no server or
// SaaS assumptions.
type MemoryOutbox struct {
	mu      stdsync.RWMutex
	changes map[string]Change
	lease   time.Duration
}

func NewMemoryOutbox(config ...MemoryOutboxConfig) *MemoryOutbox {
	outbox := &MemoryOutbox{
		changes: make(map[string]Change),
		lease:   defaultOutboxLease,
	}
	if len(config) > 0 && config[0].Lease > 0 {
		outbox.lease = config[0].Lease
	}
	return outbox
}

func (o *MemoryOutbox) Enqueue(change Change) error {
	change = normalizeChange(change)
	if err := change.Validate(); err != nil {
		return err
	}
	change = cloneChange(change)

	o.mu.Lock()
	defer o.mu.Unlock()
	if existing, ok := o.changes[change.IdempotencyKey]; ok {
		if sameImmutableChange(existing, change) {
			return nil
		}
		return ErrIdempotencyConflict
	}
	o.changes[change.IdempotencyKey] = change
	return nil
}

func (o *MemoryOutbox) Get(idempotencyKey string) (Change, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	change, ok := o.changes[idempotencyKey]
	if !ok {
		return Change{}, ErrNotFound
	}
	return cloneChange(change), nil
}

func (o *MemoryOutbox) Pending(now time.Time, limit int) ([]Change, error) {
	if limit < 0 {
		return nil, errors.New("limit cannot be negative")
	}
	o.mu.RLock()
	defer o.mu.RUnlock()

	changes := make([]Change, 0, len(o.changes))
	for _, change := range o.changes {
		if change.Status == StatusPending && !change.NextAttemptAt.After(now) {
			changes = append(changes, cloneChange(change))
			continue
		}
		if o.isStaleSyncing(change, now) {
			changes = append(changes, cloneChange(change))
		}
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].CreatedAt.Equal(changes[j].CreatedAt) {
			return changes[i].IdempotencyKey < changes[j].IdempotencyKey
		}
		return changes[i].CreatedAt.Before(changes[j].CreatedAt)
	})
	if limit > 0 && len(changes) > limit {
		changes = changes[:limit]
	}
	return changes, nil
}

func (o *MemoryOutbox) RecoverStaleSyncing(now time.Time) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	for key, change := range o.changes {
		if !o.isStaleSyncing(change, now) {
			continue
		}
		change.Status = StatusPending
		change.SyncingSince = time.Time{}
		if change.NextAttemptAt.IsZero() {
			change.NextAttemptAt = now
		}
		o.changes[key] = cloneChange(change)
	}
	return nil
}

func (o *MemoryOutbox) MarkSyncing(idempotencyKey string, at time.Time) error {
	return o.transition(idempotencyKey, func(change *Change) error {
		switch change.Status {
		case StatusPending:
		case StatusSyncing:
			if !change.SyncingSince.IsZero() && change.SyncingSince.After(at.Add(-o.lease)) {
				return ErrInvalidState
			}
		default:
			return ErrInvalidState
		}
		change.Status = StatusSyncing
		change.SyncingSince = at
		return nil
	})
}

func (o *MemoryOutbox) MarkSent(idempotencyKey string) error {
	return o.transition(idempotencyKey, func(change *Change) error {
		if change.Status != StatusPending && change.Status != StatusSyncing {
			return ErrInvalidState
		}
		change.Status = StatusSynced
		change.LastError = ""
		change.NextAttemptAt = time.Time{}
		change.SyncingSince = time.Time{}
		return nil
	})
}

func (o *MemoryOutbox) MarkRetry(idempotencyKey string, nextAttemptAt time.Time, reason FailureReason) error {
	if nextAttemptAt.IsZero() {
		return errors.New("next attempt time is required")
	}
	if !validFailureReason(reason) {
		return errors.New("invalid failure reason")
	}
	return o.transition(idempotencyKey, func(change *Change) error {
		if change.Status != StatusPending && change.Status != StatusSyncing {
			return ErrInvalidState
		}
		change.Status = StatusPending
		change.Attempts++
		change.NextAttemptAt = nextAttemptAt
		change.LastError = reason
		change.SyncingSince = time.Time{}
		return nil
	})
}

func (o *MemoryOutbox) MarkFailed(idempotencyKey string, reason FailureReason) error {
	if !validFailureReason(reason) {
		return errors.New("invalid failure reason")
	}
	return o.transition(idempotencyKey, func(change *Change) error {
		if change.Status != StatusPending && change.Status != StatusSyncing {
			return ErrInvalidState
		}
		change.Status = StatusFailed
		change.Attempts++
		change.NextAttemptAt = time.Time{}
		change.LastError = reason
		change.SyncingSince = time.Time{}
		return nil
	})
}

func (o *MemoryOutbox) MarkConflict(idempotencyKey string, conflict Conflict) error {
	if err := conflict.Validate(); err != nil {
		return err
	}
	return o.transition(idempotencyKey, func(change *Change) error {
		if change.Status != StatusPending && change.Status != StatusSyncing {
			return ErrInvalidState
		}
		if conflict.RecordID != change.RecordID || conflict.RecordType != change.RecordType || conflict.LocalChangeKey != change.IdempotencyKey {
			return errors.New("conflict does not match change")
		}
		change.Status = StatusConflict
		change.NextAttemptAt = time.Time{}
		change.LastError = ""
		change.SyncingSince = time.Time{}
		change.Conflict = &Conflict{
			ID:              conflict.ID,
			RecordID:        conflict.RecordID,
			RecordType:      conflict.RecordType,
			LocalChangeKey:  conflict.LocalChangeKey,
			RemoteChangeKey: conflict.RemoteChangeKey,
			DetectedAt:      conflict.DetectedAt,
		}
		return nil
	})
}

func (o *MemoryOutbox) isStaleSyncing(change Change, now time.Time) bool {
	if change.Status != StatusSyncing {
		return false
	}
	if change.SyncingSince.IsZero() {
		return true
	}
	return !change.SyncingSince.After(now.Add(-o.lease))
}

func (o *MemoryOutbox) transition(idempotencyKey string, fn func(*Change) error) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	change, ok := o.changes[idempotencyKey]
	if !ok {
		return ErrNotFound
	}
	if err := fn(&change); err != nil {
		return err
	}
	o.changes[idempotencyKey] = cloneChange(change)
	return nil
}

func normalizeChange(change Change) Change {
	if change.Status == "" {
		change.Status = StatusPending
	}
	if change.NextAttemptAt.IsZero() {
		change.NextAttemptAt = change.CreatedAt
	}
	return change
}

func sameImmutableChange(a, b Change) bool {
	if !sameChangeIdentity(a, b) {
		return false
	}
	if len(a.Payload) != len(b.Payload) {
		return false
	}
	for i := range a.Payload {
		if a.Payload[i] != b.Payload[i] {
			return false
		}
	}
	return true
}

func sameChangeIdentity(a, b Change) bool {
	return a.IdempotencyKey == b.IdempotencyKey &&
		a.RecordID == b.RecordID &&
		a.RecordType == b.RecordType &&
		a.Operation == b.Operation &&
		a.KeyVersion == b.KeyVersion &&
		a.AccountID == b.AccountID &&
		a.DeviceID == b.DeviceID &&
		a.ProtocolVersion == b.ProtocolVersion &&
		a.OperationID == b.OperationID &&
		a.EntityVersion == b.EntityVersion &&
		a.KeyID == b.KeyID &&
		a.ContentDigest == b.ContentDigest
}

func cloneChange(change Change) Change {
	change.Payload = append([]byte(nil), change.Payload...)
	if change.Conflict != nil {
		conflict := *change.Conflict
		change.Conflict = &conflict
	}
	return change
}
