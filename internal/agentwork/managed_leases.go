package agentwork

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

var (
	ErrManagedLeaseHeld         = errors.New("managed writer ownership is held")
	ErrManagedLeaseExpired      = errors.New("managed writer lease expired")
	ErrManagedLeaseRequired     = errors.New("managed writer lease is required")
	ErrManagedQuiescenceUnknown = errors.New("managed writer quiescence is unknown")
	ErrManagedLeaseStale        = errors.New("managed writer lease generation or revision is stale")
	ErrManagedLeaseScope        = errors.New("managed writer scope mismatch")
	ErrManagedLeaseInvalid      = errors.New("invalid managed writer lease")
)

const (
	ManagedWriterLeaseSchemaVersion = 1
	ManagedWriterLeaseTTL           = 30 * time.Second
	ManagedQuiescenceProofMaxAge    = 5 * time.Second
)

// ManagedWriterBinding is constructed by the host adapter from the current
// task, session and canonical worktree. ResourceKey is an opaque stable key;
// caller-supplied paths are never accepted or persisted.
type ManagedWriterBinding struct {
	WorkspaceID, BoardID, ParentTaskID, TaskID string
	GroupID, WorkItemID, AttemptID             string
	SessionID, ResourceKey                     string
}

// ManagedWriterResolution distinguishes an ordinary session from one attached
// to managed work. Managed=false is safe only after the host checked the live
// session and its task association.
type ManagedWriterResolution struct {
	Managed bool
	Binding ManagedWriterBinding
}

// ManagedWriterLease is durable ownership metadata. Expiration only blocks
// writes; it never releases the task, session or resource slot.
type ManagedWriterLease struct {
	ID                                string
	Binding                           ManagedWriterBinding
	AttemptGeneration, Generation     int64
	SchemaVersion                     int
	Revision                          int64
	HeartbeatAt, ExpiresAt, CreatedAt time.Time
	ReleasedAt                        *time.Time
	ReleaseReason                     string
}

// ManagedWriterQuiescence is returned only by the host's fresh process and
// isolation check. A missing/unknown result never releases durable ownership.
type ManagedWriterQuiescence struct {
	Binding                    ManagedWriterBinding
	Generation                 int64
	CheckedAt                  time.Time
	Known, Quiescent, Isolated bool
}

type ManagedLeaseHost interface {
	ResolveManagedWriter(context.Context, Attempt) (ManagedWriterBinding, error)
	ResolveCurrentWriter(context.Context, string) (ManagedWriterResolution, error)
	CheckManagedWriterQuiescence(context.Context, ManagedWriterLease) (ManagedWriterQuiescence, error)
}

type ManagedLeaseStore interface {
	GetManagedAttempt(context.Context, string) (Attempt, error)
	GetCurrentManagedAttempt(context.Context, string) (Attempt, error)
	GetManagedWorkItem(context.Context, string) (WorkItem, error)
	GetManagedWorkGroup(context.Context, string) (GroupState, error)
	ManagedSessionAssociation(context.Context, string) (Attempt, bool, error)
	CreateManagedWriterLease(context.Context, ManagedWriterLease, int64) (ManagedWriterLease, error)
	GetManagedWriterLease(context.Context, string) (ManagedWriterLease, error)
	HeartbeatManagedWriterLease(context.Context, string, int64, int64, ManagedWriterBinding, time.Time, time.Time) (ManagedWriterLease, error)
	ReleaseManagedWriterLease(context.Context, string, int64, int64, ManagedWriterQuiescence, time.Time) (ManagedWriterLease, error)
	AuthorizeManagedWriter(context.Context, ManagedWriterBinding, time.Time) error
	GetActiveManagedWriterLease(context.Context, ManagedWriterBinding, time.Time) (ManagedWriterLease, error)
}

type ManagedLeaseService struct {
	host  ManagedLeaseHost
	store ManagedLeaseStore
	now   func() time.Time
}

func NewManagedLeaseService(host ManagedLeaseHost, store ManagedLeaseStore) *ManagedLeaseService {
	return NewManagedLeaseServiceWithClock(host, store, time.Now)
}

func NewManagedLeaseServiceWithClock(host ManagedLeaseHost, store ManagedLeaseStore, now func() time.Time) *ManagedLeaseService {
	if now == nil {
		now = time.Now
	}
	return &ManagedLeaseService{host: host, store: store, now: now}
}

// Acquire creates the first lease generation for the current attempt. It does
// not dispatch work. A held or expired generation must be explicitly released
// after host proof before a newer attempt can claim the resource.
func (s *ManagedLeaseService) Acquire(ctx context.Context, attemptID string, expectedAttemptRevision int64) (ManagedWriterLease, error) {
	if s == nil || s.host == nil || s.store == nil {
		return ManagedWriterLease{}, ErrManagedHostUnavailable
	}
	a, item, group, err := s.currentAttemptScope(ctx, attemptID, expectedAttemptRevision, true)
	if err != nil {
		return ManagedWriterLease{}, err
	}
	binding, err := s.host.ResolveManagedWriter(ctx, a)
	if err != nil {
		return ManagedWriterLease{}, err
	}
	if err = validateManagedBinding(binding, a, item, group); err != nil {
		return ManagedWriterLease{}, err
	}
	id, err := managedLeaseID()
	if err != nil {
		return ManagedWriterLease{}, err
	}
	now := s.now().UTC()
	lease := ManagedWriterLease{ID: id, Binding: binding, AttemptGeneration: a.Generation, SchemaVersion: ManagedWriterLeaseSchemaVersion, Revision: 1, HeartbeatAt: now, ExpiresAt: now.Add(ManagedWriterLeaseTTL), CreatedAt: now}
	return s.store.CreateManagedWriterLease(ctx, lease, expectedAttemptRevision)
}

// Heartbeat renews only the same still-current generation and exact host
// binding. Expired leases can be renewed while their original generation still
// owns the slot; expiry itself never lets another writer take over.
func (s *ManagedLeaseService) Heartbeat(ctx context.Context, leaseID string, generation int64, expectedRevision int64) (ManagedWriterLease, error) {
	if s == nil || s.host == nil || s.store == nil {
		return ManagedWriterLease{}, ErrManagedHostUnavailable
	}
	lease, err := s.store.GetManagedWriterLease(ctx, leaseID)
	if err != nil {
		return ManagedWriterLease{}, err
	}
	if lease.Generation != generation || lease.Revision != expectedRevision || lease.ReleasedAt != nil {
		return ManagedWriterLease{}, ErrManagedLeaseStale
	}
	a, item, group, err := s.currentAttemptScope(ctx, lease.Binding.AttemptID, 0, false)
	if err != nil {
		return ManagedWriterLease{}, err
	}
	if a.Generation != lease.AttemptGeneration {
		return ManagedWriterLease{}, ErrManagedLeaseStale
	}
	binding, err := s.host.ResolveManagedWriter(ctx, a)
	if err != nil {
		return ManagedWriterLease{}, err
	}
	if err = validateManagedBinding(binding, a, item, group); err != nil {
		return ManagedWriterLease{}, err
	}
	if binding != lease.Binding {
		return ManagedWriterLease{}, ErrManagedLeaseScope
	}
	now := s.now().UTC()
	return s.store.HeartbeatManagedWriterLease(ctx, leaseID, generation, expectedRevision, binding, now, now.Add(ManagedWriterLeaseTTL))
}

// Release performs a fresh host quiescence/isolation check itself. No caller
// can pass a boolean or proof assertion to release ownership.
func (s *ManagedLeaseService) Release(ctx context.Context, leaseID string, generation int64, expectedRevision int64) (ManagedWriterLease, error) {
	if s == nil || s.host == nil || s.store == nil {
		return ManagedWriterLease{}, ErrManagedHostUnavailable
	}
	lease, err := s.store.GetManagedWriterLease(ctx, leaseID)
	if err != nil {
		return ManagedWriterLease{}, err
	}
	if lease.Generation != generation || lease.Revision != expectedRevision || lease.ReleasedAt != nil {
		return ManagedWriterLease{}, ErrManagedLeaseStale
	}
	proof, err := s.host.CheckManagedWriterQuiescence(ctx, lease)
	if err != nil {
		return ManagedWriterLease{}, err
	}
	now := s.now().UTC()
	if proof.Binding != lease.Binding || proof.Generation != lease.Generation || proof.CheckedAt.IsZero() || proof.CheckedAt.After(now.Add(time.Second)) || now.Sub(proof.CheckedAt) > ManagedQuiescenceProofMaxAge || !proof.Known || !proof.Quiescent || !proof.Isolated {
		return ManagedWriterLease{}, ErrManagedQuiescenceUnknown
	}
	return s.store.ReleaseManagedWriterLease(ctx, leaseID, generation, expectedRevision, proof, now)
}

// AuthorizeCurrentWriter is the pre-invoke/pre-steer fence. Unmanaged sessions
// pass only when the host positively identifies them as unmanaged.
func (s *ManagedLeaseService) AuthorizeCurrentWriter(ctx context.Context, sessionID string) error {
	if s == nil || s.host == nil || s.store == nil {
		return ErrManagedHostUnavailable
	}
	resolution, err := s.host.ResolveCurrentWriter(ctx, sessionID)
	if err != nil {
		return err
	}
	if !resolution.Managed {
		return nil
	}
	if resolution.Binding.SessionID != sessionID || resolution.Binding.ResourceKey == "" {
		return ErrManagedLeaseScope
	}
	return s.store.AuthorizeManagedWriter(ctx, resolution.Binding, s.now().UTC())
}

// AuthorizeAttemptWriter is an internal fence for host-owned artifact writes.
// It derives the session and resource binding from the current attempt and
// host; callers cannot supply a lease, session, or path assertion.
func (s *ManagedLeaseService) AuthorizeAttemptWriter(ctx context.Context, attemptID string) error {
	if s == nil || s.host == nil || s.store == nil {
		return ErrManagedHostUnavailable
	}
	a, item, group, err := s.currentAttemptScope(ctx, attemptID, 0, true)
	if err != nil {
		return err
	}
	binding, err := s.host.ResolveManagedWriter(ctx, a)
	if err != nil {
		return err
	}
	if err = validateManagedBinding(binding, a, item, group); err != nil {
		return err
	}
	return s.store.AuthorizeManagedWriter(ctx, binding, s.now().UTC())
}

// CurrentAttemptLease returns the persisted generation only for the currently
// host-resolved attempt and an active, unexpired lease. It accepts no caller
// resource key, session, or lease identifier.
func (s *ManagedLeaseService) CurrentAttemptLease(ctx context.Context, attemptID string) (ManagedWriterLease, error) {
	if s == nil || s.host == nil || s.store == nil {
		return ManagedWriterLease{}, ErrManagedHostUnavailable
	}
	a, item, group, err := s.currentAttemptScope(ctx, attemptID, 0, true)
	if err != nil {
		return ManagedWriterLease{}, err
	}
	binding, err := s.host.ResolveManagedWriter(ctx, a)
	if err != nil {
		return ManagedWriterLease{}, err
	}
	if err = validateManagedBinding(binding, a, item, group); err != nil {
		return ManagedWriterLease{}, err
	}
	return s.store.GetActiveManagedWriterLease(ctx, binding, s.now().UTC())
}

// ManagedSessionAssociation lets the trusted app adapter conservatively
// classify a historical managed session even when its lease was released.
func (s *ManagedLeaseService) ManagedSessionAssociation(ctx context.Context, sessionID string) (Attempt, bool, error) {
	if s == nil || s.store == nil {
		return Attempt{}, false, ErrManagedHostUnavailable
	}
	return s.store.ManagedSessionAssociation(ctx, sessionID)
}

func (s *ManagedLeaseService) currentAttemptScope(ctx context.Context, attemptID string, expectedRevision int64, requireEnabled bool) (Attempt, WorkItem, GroupState, error) {
	a, err := s.store.GetManagedAttempt(ctx, attemptID)
	if err != nil {
		return Attempt{}, WorkItem{}, GroupState{}, err
	}
	if expectedRevision > 0 && a.Revision != expectedRevision {
		return Attempt{}, WorkItem{}, GroupState{}, ErrStaleRevision
	}
	item, err := s.store.GetManagedWorkItem(ctx, a.WorkItemID)
	if err != nil {
		return Attempt{}, WorkItem{}, GroupState{}, err
	}
	group, err := s.store.GetManagedWorkGroup(ctx, a.GroupID)
	if err != nil {
		return Attempt{}, WorkItem{}, GroupState{}, err
	}
	if item.CurrentAttemptID != a.ID || item.GroupID != a.GroupID || group.EndedAt != nil || requireEnabled && (!group.Enabled || group.Paused) || item.EndedAt != nil || item.TombstoneReason != "" {
		return Attempt{}, WorkItem{}, GroupState{}, ErrManagedLeaseScope
	}
	if a.State != "session_created" && a.State != "task_assigned" && a.State != "prompt_enqueued" && a.State != "running" {
		return Attempt{}, WorkItem{}, GroupState{}, ErrManagedLeaseInvalid
	}
	return a, item, group, nil
}

func validateManagedBinding(b ManagedWriterBinding, a Attempt, i WorkItem, g GroupState) error {
	if b.WorkspaceID != a.WorkspaceID || b.BoardID != a.BoardID || b.ParentTaskID != a.ParentTaskID || b.GroupID != a.GroupID || b.WorkItemID != a.WorkItemID || b.AttemptID != a.ID || b.TaskID != i.TaskID || b.WorkspaceID != i.WorkspaceID || b.BoardID != i.BoardID || b.ParentTaskID != i.ParentTaskID || b.GroupID != g.ID || b.SessionID == "" || b.ResourceKey == "" {
		return fmt.Errorf("%w: host writer binding does not match current managed scope", ErrManagedLeaseScope)
	}
	if a.SessionID != "" && a.SessionID != b.SessionID {
		return ErrManagedLeaseScope
	}
	return nil
}

func managedLeaseID() (string, error) {
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}
