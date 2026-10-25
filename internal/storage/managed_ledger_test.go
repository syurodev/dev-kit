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
)

func openManagedTestLedger(t *testing.T, path string, now func() time.Time) (*sql.DB, *ManagedLedger) {
	t.Helper()
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(context.Background(), db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	return db, NewManagedLedgerWithClock(db, now)
}

func seedManagedGroupItem(t *testing.T, l *ManagedLedger, groupID, itemID string) {
	t.Helper()
	ctx := context.Background()
	_, err := l.CreateWorkGroup(ctx, ManagedWorkGroup{ID: groupID, WorkspaceID: "ws-1", BoardID: "board-1", ParentTaskID: "parent-1", CoordinatorID: "session-coord", Enabled: true, Paused: false, PolicyRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, err = l.CreateWorkItem(ctx, ManagedWorkItem{ID: itemID, GroupID: groupID, WorkspaceID: "ws-1", BoardID: "board-1", ParentTaskID: "parent-1", TaskID: "task-1", TaskRevision: 7, RequirementsRevision: "sha256:v1:requirements", Role: "implementer"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestManagedLedgerDurableAndAmbiguousDispatchNotReplayed(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ledger.db")
	now := time.Date(2026, 10, 25, 1, 2, 3, 4, time.UTC)
	db, l := openManagedTestLedger(t, path, func() time.Time { return now })
	seedManagedGroupItem(t, l, "g1", "i1")
	g, err := l.GetWorkGroup(ctx, "g1")
	if err != nil {
		t.Fatal(err)
	}
	if g.Enabled || !g.Paused || g.PolicyRevision != 1 {
		t.Fatalf("unsafe initial policy: %+v", g)
	}
	if g.CreatedAt != now {
		t.Fatalf("created at %v, want injected %v", g.CreatedAt, now)
	}
	item, err := l.GetWorkItem(ctx, "i1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.BeginAttempt(ctx, ManagedWorkAttempt{ID: "disabled", WorkItemID: item.ID, TaskRevision: 7, RequirementsRevision: "sha256:v1:requirements"}, item.Revision); !errors.Is(err, ErrManagedPolicyClosed) {
		t.Fatalf("disabled group allowed an attempt: %v", err)
	}
	if _, err := l.UpdateGroupPolicy(ctx, "g1", g.Revision, 2, true, false); err != nil {
		t.Fatal(err)
	}
	item, err = l.GetWorkItem(ctx, "i1")
	if err != nil {
		t.Fatal(err)
	}
	a, err := l.BeginAttempt(ctx, ManagedWorkAttempt{ID: "a1", WorkItemID: item.ID, TaskRevision: 7, RequirementsRevision: "sha256:v1:requirements"}, item.Revision)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"session_created", "task_assigned", "prompt_enqueued", "running"} {
		a, err = l.TransitionAttempt(ctx, a.ID, a.Revision, state)
		if err != nil {
			t.Fatalf("transition to %s: %v", state, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, l = openManagedTestLedger(t, path, func() time.Time { return now.Add(time.Minute) })
	defer db.Close()
	recovered, err := l.ListAttempts(ctx, "i1", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered.Items) != 1 || recovered.Items[0].State != "running" {
		t.Fatalf("restart auto-mutated attempt: %+v", recovered)
	}
	active := recovered.Items[0]
	marked, err := l.MarkAttemptAmbiguous(ctx, active.ID, active.Revision)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err = l.ListAttempts(ctx, "i1", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered.Items) != 1 || recovered.Items[0].State != "interrupted_unknown" || recovered.Items[0].DispatchState != "ambiguous" {
		t.Fatalf("recovered attempt = %+v", recovered)
	}
	if _, err := l.MarkAttemptAmbiguous(ctx, active.ID, active.Revision); !errors.Is(err, ErrManagedConflict) {
		t.Fatalf("repeated stale recovery err=%v", err)
	}
	item, err = l.GetWorkItem(ctx, "i1")
	if err != nil {
		t.Fatal(err)
	}
	if item.State != "blocked" || item.CurrentAttemptID != "a1" {
		t.Fatalf("recovered item = %+v", item)
	}
	if _, err = l.TransitionAttempt(ctx, "a1", recovered.Items[0].Revision, "running"); !errors.Is(err, ErrManagedInvalid) {
		t.Fatalf("stale/terminal attempt transition err=%v", err)
	}
	if marked.State != "interrupted_unknown" {
		t.Fatalf("marked attempt = %+v", marked)
	}
}

func TestManagedLedgerConcurrentGroupCASAndAttemptClaim(t *testing.T) {
	ctx := context.Background()
	db, l := openManagedTestLedger(t, filepath.Join(t.TempDir(), "ledger.db"), time.Now)
	defer db.Close()
	seedManagedGroupItem(t, l, "g1", "i1")
	if _, err := l.UpdateGroupPolicy(ctx, "g1", 1, 2, true, false); err != nil {
		t.Fatal(err)
	}
	var successes, conflicts atomic.Int32
	var wg sync.WaitGroup
	group, err := l.GetWorkGroup(ctx, "g1")
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func(enabled bool) {
			defer wg.Done()
			_, err := l.UpdateGroupPolicy(ctx, "g1", group.Revision, 3, enabled, false)
			if err == nil {
				successes.Add(1)
			} else if errors.Is(err, ErrManagedConflict) {
				conflicts.Add(1)
			} else {
				t.Errorf("update policy: %v", err)
			}
		}(n == 0)
	}
	wg.Wait()
	if successes.Load() != 1 || conflicts.Load() != 1 {
		t.Fatalf("CAS successes=%d conflicts=%d", successes.Load(), conflicts.Load())
	}
	group, err = l.GetWorkGroup(ctx, "g1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.UpdateGroupPolicy(ctx, "g1", group.Revision, group.PolicyRevision+1, true, false); err != nil {
		t.Fatal(err)
	}
	item, err := l.GetWorkItem(ctx, "i1")
	if err != nil {
		t.Fatal(err)
	}
	successes.Store(0)
	conflicts.Store(0)
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, err := l.BeginAttempt(ctx, ManagedWorkAttempt{ID: id, WorkItemID: "i1", TaskRevision: 7, RequirementsRevision: "sha256:v1:requirements"}, item.Revision)
			if err == nil {
				successes.Add(1)
			} else if errors.Is(err, ErrManagedConflict) {
				conflicts.Add(1)
			} else {
				t.Errorf("begin attempt: %v", err)
			}
		}("a" + string(rune('0'+n)))
	}
	wg.Wait()
	if successes.Load() != 1 || conflicts.Load() != 1 {
		t.Fatalf("claim successes=%d conflicts=%d", successes.Load(), conflicts.Load())
	}
}

func TestManagedLedgerRejectsForeignScopeAndStaleRevision(t *testing.T) {
	ctx := context.Background()
	db, l := openManagedTestLedger(t, filepath.Join(t.TempDir(), "ledger.db"), time.Now)
	defer db.Close()
	seedManagedGroupItem(t, l, "g1", "i1")
	if _, err := l.UpdateGroupPolicy(ctx, "g1", 1, 2, true, false); err != nil {
		t.Fatal(err)
	}
	_, err := l.CreateWorkItem(ctx, ManagedWorkItem{ID: "bad", GroupID: "g1", WorkspaceID: "other", BoardID: "board-1", ParentTaskID: "parent-1", TaskID: "task-other", TaskRevision: 1, RequirementsRevision: "digest", Role: "implementer"})
	if !errors.Is(err, ErrManagedScope) {
		t.Fatalf("foreign scope error=%v", err)
	}
	item, err := l.GetWorkItem(ctx, "i1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = l.BeginAttempt(ctx, ManagedWorkAttempt{ID: "a1", WorkItemID: "i1", TaskRevision: 6, RequirementsRevision: "sha256:v1:requirements"}, item.Revision); !errors.Is(err, ErrManagedScope) {
		t.Fatalf("stale task revision error=%v", err)
	}
	if _, err = l.BeginAttempt(ctx, ManagedWorkAttempt{ID: "a1", WorkItemID: "i1", TaskRevision: 7, RequirementsRevision: "changed-digest"}, item.Revision); !errors.Is(err, ErrManagedScope) {
		t.Fatalf("stale requirements error=%v", err)
	}
	if _, err = l.UpdateGroupPolicy(ctx, "g1", 2, 3, true, false); err != nil {
		t.Fatal(err)
	}
	a, err := l.BeginAttempt(ctx, ManagedWorkAttempt{ID: "a1", WorkItemID: "i1", TaskRevision: 7, RequirementsRevision: "sha256:v1:requirements"}, item.Revision)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := l.RefreshWorkItemRequirements(ctx, "i1", item.Revision+1, 8, "new-digest")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = l.RefreshWorkItemRequirements(ctx, "i1", updated.Revision-1, 9, "another-digest"); !errors.Is(err, ErrManagedConflict) {
		t.Fatalf("stale requirements refresh error=%v", err)
	}
	if _, err = l.TransitionAttempt(ctx, a.ID, a.Revision, "session_created"); !errors.Is(err, ErrManagedScope) {
		t.Fatalf("stale in-flight attempt applied after requirements change: %v", err)
	}
	if _, err = l.TransitionAttempt(ctx, "missing", 1, "running"); !errors.Is(err, ErrManagedNotFound) {
		t.Fatalf("missing attempt error=%v", err)
	}
}

func TestManagedLedgerAttemptAndItemScopeReferencesProtectHistory(t *testing.T) {
	ctx := context.Background()
	db, l := openManagedTestLedger(t, filepath.Join(t.TempDir(), "ledger.db"), time.Now)
	defer db.Close()
	seedManagedGroupItem(t, l, "g1", "i1")
	if _, err := l.UpdateGroupPolicy(ctx, "g1", 1, 2, true, false); err != nil {
		t.Fatal(err)
	}
	item, err := l.GetWorkItem(ctx, "i1")
	if err != nil {
		t.Fatal(err)
	}
	a, err := l.BeginAttempt(ctx, ManagedWorkAttempt{ID: "a1", WorkItemID: item.ID, TaskRevision: 7, RequirementsRevision: "sha256:v1:requirements"}, item.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE agent_work_items SET current_attempt_id='foreign-attempt' WHERE id='i1'`); err == nil {
		t.Fatal("dangling current attempt pointer was accepted")
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM agent_work_groups WHERE id='g1'`); err == nil {
		t.Fatal("deleting group erased active attempt history")
	}
	if _, err := l.TransitionAttempt(ctx, a.ID, a.Revision, "session_created"); err != nil {
		t.Fatal(err)
	}
}

func TestManagedLedgerConcurrentAttemptTransitionCAS(t *testing.T) {
	ctx := context.Background()
	db, l := openManagedTestLedger(t, filepath.Join(t.TempDir(), "ledger.db"), time.Now)
	defer db.Close()
	seedManagedGroupItem(t, l, "g1", "i1")
	if _, err := l.UpdateGroupPolicy(ctx, "g1", 1, 2, true, false); err != nil {
		t.Fatal(err)
	}
	item, err := l.GetWorkItem(ctx, "i1")
	if err != nil {
		t.Fatal(err)
	}
	a, err := l.BeginAttempt(ctx, ManagedWorkAttempt{ID: "a1", WorkItemID: item.ID, TaskRevision: 7, RequirementsRevision: "sha256:v1:requirements"}, item.Revision)
	if err != nil {
		t.Fatal(err)
	}
	var successes, conflicts atomic.Int32
	var wg sync.WaitGroup
	for _, target := range []string{"session_created", "cancelled"} {
		wg.Add(1)
		go func(target string) {
			defer wg.Done()
			_, err := l.TransitionAttempt(ctx, a.ID, a.Revision, target)
			if err == nil {
				successes.Add(1)
			} else if errors.Is(err, ErrManagedConflict) {
				conflicts.Add(1)
			} else {
				t.Errorf("transition: %v", err)
			}
		}(target)
	}
	wg.Wait()
	if successes.Load() != 1 || conflicts.Load() != 1 {
		t.Fatalf("transition CAS successes=%d conflicts=%d", successes.Load(), conflicts.Load())
	}
}

func TestManagedLedgerPaginationIsBounded(t *testing.T) {
	ctx := context.Background()
	db, l := openManagedTestLedger(t, filepath.Join(t.TempDir(), "ledger.db"), time.Now)
	defer db.Close()
	_, err := l.CreateWorkGroup(ctx, ManagedWorkGroup{ID: "g", WorkspaceID: "ws", BoardID: "b", ParentTaskID: "p", CoordinatorID: "c", PolicyRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"i1", "i2", "i3"} {
		_, err = l.CreateWorkItem(ctx, ManagedWorkItem{ID: id, GroupID: "g", WorkspaceID: "ws", BoardID: "b", ParentTaskID: "p", TaskID: "task-" + id, TaskRevision: 1, RequirementsRevision: "digest", Role: "reviewer"})
		if err != nil {
			t.Fatal(err)
		}
	}
	p, err := l.ListWorkItems(ctx, "g", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 2 || p.Next != "i2" {
		t.Fatalf("page=%+v", p)
	}
	if _, err = l.ListWorkItems(ctx, "g", "", ManagedLedgerPageMax+1); !errors.Is(err, ErrManagedInvalid) {
		t.Fatalf("oversized page error=%v", err)
	}
}
