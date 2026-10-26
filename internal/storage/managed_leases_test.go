package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devkit/internal/agentwork"
)

type leaseHostFake struct {
	bindings map[string]agentwork.ManagedWriterBinding
	current  map[string]agentwork.ManagedWriterResolution
	proof    agentwork.ManagedWriterQuiescence
	proofErr error
}

func (h *leaseHostFake) ResolveManagedWriter(_ context.Context, a agentwork.Attempt) (agentwork.ManagedWriterBinding, error) {
	b, ok := h.bindings[a.ID]
	if !ok {
		return b, agentwork.ErrManagedHostUnavailable
	}
	return b, nil
}
func (h *leaseHostFake) ResolveCurrentWriter(_ context.Context, session string) (agentwork.ManagedWriterResolution, error) {
	r, ok := h.current[session]
	if !ok {
		return agentwork.ManagedWriterResolution{}, agentwork.ErrManagedHostUnavailable
	}
	return r, nil
}
func (h *leaseHostFake) CheckManagedWriterQuiescence(_ context.Context, l agentwork.ManagedWriterLease) (agentwork.ManagedWriterQuiescence, error) {
	return h.proof, h.proofErr
}

func openManagedLeaseTest(t *testing.T, now func() time.Time) (*ManagedLedger, *sql.DB) {
	t.Helper()
	db, l := openManagedTestLedger(t, filepath.Join(t.TempDir(), "leases.db"), now)
	if err := ManagedLeasesMigration(db).Up(context.Background()); err != nil {
		db.Close()
		t.Fatal(err)
	}
	return l, db
}

func createLeaseAttempt(t *testing.T, l *ManagedLedger, groupID, parentID, itemID, taskID, attemptID, sessionID string) (ManagedWorkItem, ManagedWorkAttempt) {
	t.Helper()
	ctx := context.Background()
	g, err := l.CreateWorkGroup(ctx, ManagedWorkGroup{ID: groupID, WorkspaceID: "ws", BoardID: "board", ParentTaskID: parentID, CoordinatorID: "coord", PolicyRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = l.UpdateGroupPolicy(ctx, g.ID, g.Revision, 2, true, false); err != nil {
		t.Fatal(err)
	}
	i, err := l.CreateWorkItem(ctx, ManagedWorkItem{ID: itemID, GroupID: g.ID, WorkspaceID: g.WorkspaceID, BoardID: g.BoardID, ParentTaskID: g.ParentTaskID, TaskID: taskID, Role: "implementer", TaskRevision: 1, RequirementsRevision: "sha256:v2:test"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := l.BeginAttempt(ctx, ManagedWorkAttempt{ID: attemptID, WorkItemID: i.ID, SessionID: sessionID, TaskRevision: i.TaskRevision, RequirementsRevision: i.RequirementsRevision}, i.Revision)
	if err != nil {
		t.Fatal(err)
	}
	a, err = l.TransitionAttempt(ctx, a.ID, a.Revision, "session_created")
	if err != nil {
		t.Fatal(err)
	}
	i, err = l.GetWorkItem(ctx, i.ID)
	if err != nil {
		t.Fatal(err)
	}
	return i, a
}

func TestManagedWriterLeaseExpiryDoesNotTransferOwnershipAndReleaseAdvancesGeneration(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 26, 4, 5, 6, 0, time.UTC)
	clock := func() time.Time { return now }
	l, db := openManagedLeaseTest(t, clock)
	defer db.Close()
	i1, a1 := createLeaseAttempt(t, l, "g1", "p1", "i1", "task1", "a1", "session1")
	b1 := agentwork.ManagedWriterBinding{WorkspaceID: "ws", BoardID: "board", ParentTaskID: "p1", TaskID: "task1", GroupID: "g1", WorkItemID: i1.ID, AttemptID: a1.ID, SessionID: "session1", ResourceKey: "worktree-key-1"}
	h := &leaseHostFake{bindings: map[string]agentwork.ManagedWriterBinding{"a1": b1}, current: map[string]agentwork.ManagedWriterResolution{"session1": {Managed: true, Binding: b1}}, proof: agentwork.ManagedWriterQuiescence{Binding: b1, Generation: 1, CheckedAt: now, Known: false}}
	svc := agentwork.NewManagedLeaseServiceWithClock(h, l, clock)
	lease, err := svc.Acquire(ctx, a1.ID, a1.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Generation != 1 || lease.Revision != 1 || lease.ExpiresAt.Sub(now) != agentwork.ManagedWriterLeaseTTL {
		t.Fatalf("initial lease=%+v", lease)
	}
	active, err := svc.CurrentAttemptLease(ctx, a1.ID)
	if err != nil || active.ID != lease.ID || active.Generation != lease.Generation {
		t.Fatalf("current attempt lease=%+v err=%v", active, err)
	}
	if err := svc.AuthorizeCurrentWriter(ctx, "session1"); err != nil {
		t.Fatalf("fresh current writer denied: %v", err)
	}
	// A different group and task resolving to the same canonical worktree cannot
	// acquire even after the first heartbeat expiry.
	_, a2 := createLeaseAttempt(t, l, "g2", "p2", "i2", "task2", "a2", "session2")
	b2 := agentwork.ManagedWriterBinding{WorkspaceID: "ws", BoardID: "board", ParentTaskID: "p2", TaskID: "task2", GroupID: "g2", WorkItemID: "i2", AttemptID: "a2", SessionID: "session2", ResourceKey: "worktree-key-1"}
	h.bindings["a2"] = b2
	if _, err := svc.Acquire(ctx, a2.ID, a2.Revision); !errors.Is(err, ErrManagedLeaseConflict) {
		t.Fatalf("same-worktree cross-group acquire=%v", err)
	}
	now = now.Add(agentwork.ManagedWriterLeaseTTL + time.Second)
	if err := svc.AuthorizeCurrentWriter(ctx, "session1"); !errors.Is(err, agentwork.ErrManagedLeaseExpired) {
		t.Fatalf("expired writer gate=%v", err)
	}
	if _, err := svc.CurrentAttemptLease(ctx, a1.ID); !errors.Is(err, agentwork.ErrManagedLeaseExpired) {
		t.Fatalf("expired current lease lookup=%v", err)
	}
	if _, err := svc.Acquire(ctx, a2.ID, a2.Revision); !errors.Is(err, ErrManagedLeaseConflict) {
		t.Fatalf("expiry freed live/unknown writer slot: %v", err)
	}
	// Same generation can recover its own expired heartbeat, but an old CAS
	// revision cannot heartbeat after the renewal.
	lease, err = svc.Heartbeat(ctx, lease.ID, lease.Generation, lease.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Revision != 2 || lease.ExpiresAt.Before(now) {
		t.Fatalf("heartbeat did not renew same generation: %+v", lease)
	}
	if _, err := svc.Heartbeat(ctx, lease.ID, lease.Generation, 1); !errors.Is(err, agentwork.ErrManagedLeaseStale) {
		t.Fatalf("stale heartbeat=%v", err)
	}
	// Unknown or alive host results do not release any row or clear the current
	// pointer. The service accepts only an internal, fresh quiescence proof.
	h.proof = agentwork.ManagedWriterQuiescence{Binding: b1, Generation: lease.Generation, CheckedAt: now, Known: false, Quiescent: false, Isolated: false}
	if _, err := svc.Release(ctx, lease.ID, lease.Generation, lease.Revision); !errors.Is(err, agentwork.ErrManagedQuiescenceUnknown) {
		t.Fatalf("unknown release=%v", err)
	}
	h.proof = agentwork.ManagedWriterQuiescence{Binding: b1, Generation: lease.Generation, CheckedAt: now, Known: true, Quiescent: false, Isolated: true}
	if _, err := svc.Release(ctx, lease.ID, lease.Generation, lease.Revision); !errors.Is(err, agentwork.ErrManagedQuiescenceUnknown) {
		t.Fatalf("alive release=%v", err)
	}
	if current, err := l.GetCurrentManagedAttempt(ctx, i1.ID); err != nil || current.ID != a1.ID {
		t.Fatalf("unproved release moved current attempt: %+v err=%v", current, err)
	}
	// Fresh positive process+isolation evidence releases atomically and keeps the
	// old attempt immutable; only then can a new generation take the slot.
	h.proof = agentwork.ManagedWriterQuiescence{Binding: b1, Generation: lease.Generation, CheckedAt: now, Known: true, Quiescent: true, Isolated: true}
	released, err := svc.Release(ctx, lease.ID, lease.Generation, lease.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if released.ReleasedAt == nil || released.Generation != 1 || released.Revision != 3 {
		t.Fatalf("release result=%+v", released)
	}
	if _, err := svc.Heartbeat(ctx, lease.ID, lease.Generation, released.Revision); !errors.Is(err, agentwork.ErrManagedLeaseStale) {
		t.Fatalf("released generation heartbeat=%v", err)
	}
	if _, err := l.GetCurrentManagedAttempt(ctx, i1.ID); !errors.Is(err, ErrManagedNotFound) {
		t.Fatalf("current pointer survived proven release: %v", err)
	}
	if _, err := l.TransitionAttempt(ctx, a1.ID, a1.Revision, "failed"); !errors.Is(err, ErrManagedScope) {
		t.Fatalf("old generation transition after release=%v", err)
	}
	i1, err = l.GetWorkItem(ctx, i1.ID)
	if err != nil {
		t.Fatal(err)
	}
	a3, err := l.BeginAttempt(ctx, ManagedWorkAttempt{ID: "a3", WorkItemID: i1.ID, SessionID: "session3", TaskRevision: i1.TaskRevision, RequirementsRevision: i1.RequirementsRevision}, i1.Revision)
	if err != nil {
		t.Fatal(err)
	}
	a3, err = l.TransitionAttempt(ctx, a3.ID, a3.Revision, "session_created")
	if err != nil {
		t.Fatal(err)
	}
	b3 := b1
	b3.AttemptID = a3.ID
	b3.SessionID = "session3"
	h.bindings[a3.ID] = b3
	h.current[b3.SessionID] = agentwork.ManagedWriterResolution{Managed: true, Binding: b3}
	newLease, err := svc.Acquire(ctx, a3.ID, a3.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if newLease.Generation != 2 {
		t.Fatalf("new resource generation=%d, want 2", newLease.Generation)
	}
	if old, managed, err := svc.ManagedSessionAssociation(ctx, "session1"); err != nil || !managed || old.ID != a1.ID {
		t.Fatalf("released session association=%+v managed=%v err=%v", old, managed, err)
	}
	h.current["session1"] = agentwork.ManagedWriterResolution{Managed: true, Binding: b1}
	if err := svc.AuthorizeCurrentWriter(ctx, "session1"); !errors.Is(err, agentwork.ErrManagedLeaseRequired) {
		t.Fatalf("released old session regained write gate: %v", err)
	}
}

func TestManagedWriterLeaseContentionIsAtomicAcrossGroups(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 26, 5, 6, 7, 0, time.UTC)
	l, db := openManagedLeaseTest(t, func() time.Time { return now })
	defer db.Close()
	i, a := createLeaseAttempt(t, l, "g1", "p1", "i1", "t1", "a1", "s1")
	b := agentwork.ManagedWriterBinding{WorkspaceID: "ws", BoardID: "board", ParentTaskID: "p1", TaskID: "t1", GroupID: "g1", WorkItemID: i.ID, AttemptID: a.ID, SessionID: "s1", ResourceKey: "shared-worktree"}
	h := &leaseHostFake{bindings: map[string]agentwork.ManagedWriterBinding{"a1": b}, current: map[string]agentwork.ManagedWriterResolution{"s1": {Managed: true, Binding: b}}}
	svc := agentwork.NewManagedLeaseServiceWithClock(h, l, func() time.Time { return now })
	var wg sync.WaitGroup
	var success atomic.Int32
	var held atomic.Int32
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.Acquire(ctx, a.ID, a.Revision)
			if err == nil {
				success.Add(1)
			} else if errors.Is(err, ErrManagedLeaseConflict) {
				held.Add(1)
			} else {
				t.Errorf("concurrent lease acquire: %v", err)
			}
		}()
	}
	wg.Wait()
	if success.Load() != 1 || held.Load() != 1 {
		t.Fatalf("success=%d held=%d", success.Load(), held.Load())
	}
}

func TestManagedWriterLeaseHeartbeatWhilePausedAndAmbiguousOwnership(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 26, 6, 7, 8, 0, time.UTC)
	l, db := openManagedLeaseTest(t, func() time.Time { return now })
	defer db.Close()
	item, attempt := createLeaseAttempt(t, l, "g1", "p1", "i1", "task1", "a1", "session1")
	binding := agentwork.ManagedWriterBinding{WorkspaceID: "ws", BoardID: "board", ParentTaskID: "p1", TaskID: "task1", GroupID: "g1", WorkItemID: item.ID, AttemptID: attempt.ID, SessionID: "session1", ResourceKey: "resource1"}
	host := &leaseHostFake{bindings: map[string]agentwork.ManagedWriterBinding{attempt.ID: binding}, current: map[string]agentwork.ManagedWriterResolution{"session1": {Managed: true, Binding: binding}}}
	service := agentwork.NewManagedLeaseServiceWithClock(host, l, func() time.Time { return now })
	lease, err := service.Acquire(ctx, attempt.ID, attempt.Revision)
	if err != nil {
		t.Fatal(err)
	}

	group, err := l.GetWorkGroup(ctx, "g1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = l.UpdateGroupPolicy(ctx, group.ID, group.Revision, group.PolicyRevision+1, false, true); err != nil {
		t.Fatal(err)
	}
	if err = service.AuthorizeCurrentWriter(ctx, "session1"); !errors.Is(err, ErrManagedPolicyClosed) {
		t.Fatalf("paused group authorized invoke: %v", err)
	}
	lease, err = service.Heartbeat(ctx, lease.ID, lease.Generation, lease.Revision)
	if err != nil {
		t.Fatalf("paused group could not retain its existing lease: %v", err)
	}

	group, err = l.GetWorkGroup(ctx, "g1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = l.UpdateGroupPolicy(ctx, group.ID, group.Revision, group.PolicyRevision+1, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err = l.TransitionAttempt(ctx, attempt.ID, attempt.Revision, "task_assigned"); err != nil {
		t.Fatal(err)
	}
	currentAttempt, err := l.GetManagedAttempt(ctx, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = l.TransitionAttempt(ctx, attempt.ID, currentAttempt.Revision, "prompt_enqueued"); err != nil {
		t.Fatal(err)
	}
	currentAttempt, err = l.GetManagedAttempt(ctx, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = l.MarkAttemptAmbiguous(ctx, attempt.ID, currentAttempt.Revision); err != nil {
		t.Fatal(err)
	}
	currentAttempt, err = l.GetManagedAttempt(ctx, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Acquire(ctx, attempt.ID, currentAttempt.Revision); !errors.Is(err, agentwork.ErrManagedLeaseInvalid) {
		t.Fatalf("ambiguous attempt acquired another lease: %v", err)
	}
	stored, err := l.GetManagedWriterLease(ctx, lease.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ReleasedAt != nil {
		t.Fatalf("ambiguous attempt released its writer ownership: %+v", stored)
	}
	if err = service.AuthorizeCurrentWriter(ctx, "session1"); !errors.Is(err, ErrManagedScope) {
		t.Fatalf("ambiguous attempt was authorized to write: %v", err)
	}
}
