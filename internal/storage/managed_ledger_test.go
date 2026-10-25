package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devkit/internal/agentwork"
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

type managedServiceHostFake struct {
	workspace                          string
	parent                             agentwork.ManagedParentSnapshot
	task                               agentwork.ManagedTaskSnapshot
	authorized                         bool
	eligibilityErr, loadErr, parentErr error
}

func (h *managedServiceHostFake) AuthorizeManagedHuman(context.Context) error {
	if !h.authorized {
		return errors.New("human required")
	}
	return nil
}
func (h *managedServiceHostFake) CurrentManagedWorkspace(context.Context) (string, error) {
	return h.workspace, nil
}
func (h *managedServiceHostFake) ResolveManagedParent(context.Context, string) (agentwork.ManagedParentSnapshot, error) {
	return h.parent, h.parentErr
}
func (h *managedServiceHostFake) ResolveManagedParentForGroup(context.Context, agentwork.GroupState) (agentwork.ManagedParentSnapshot, error) {
	return h.parent, h.parentErr
}
func (h *managedServiceHostFake) LoadManagedTask(context.Context, agentwork.GroupState, string) (agentwork.ManagedTaskSnapshot, error) {
	return h.task, h.loadErr
}
func (h *managedServiceHostFake) ValidateManagedEligibility(context.Context, agentwork.ManagedParentSnapshot, agentwork.StatusMap) error {
	return h.eligibilityErr
}

func TestManagedServiceDerivesScopeAndReconcilesMetadataOnly(t *testing.T) {
	ctx := context.Background()
	db, l := openManagedTestLedger(t, filepath.Join(t.TempDir(), "service.db"), time.Now)
	defer db.Close()
	statuses := []agentwork.BoardStatus{{ID: "todo", Category: "open"}, {ID: "doing", Category: "active"}, {ID: "review", Category: "active"}, {ID: "shipped", Category: "terminal"}, {ID: "wont_do", Category: "terminal", WontDo: true}}
	h := &managedServiceHostFake{workspace: "ws-service", authorized: true}
	h.parent = agentwork.ManagedParentSnapshot{WorkspaceID: "ws-service", BoardID: "board-service", ParentTaskID: "parent-service", CoordinatorID: "session-real", Revision: 3, StatusID: "doing", Statuses: statuses}
	h.task = agentwork.ManagedTaskSnapshot{WorkspaceID: "ws-service", BoardID: "board-service", ParentTaskID: "parent-service", TaskID: "child-service", Revision: 1, StatusID: "todo", StatusCategory: "open", Requirements: agentwork.RequirementsSnapshot{Title: "host title", Description: "first"}}
	svc := agentwork.NewManagedService(h, l)
	sm := agentwork.StatusMap{Ready: "todo", Running: "doing", AwaitingReview: "review", Accepted: "shipped"}
	if _, err := svc.CreateGroup(ctx, h.parent.ParentTaskID, sm); err != nil {
		t.Fatal(err)
	}
	groups, err := svc.ListGroups(ctx, "", 10)
	if err != nil || len(groups.Items) != 1 {
		t.Fatalf("groups=%+v err=%v", groups, err)
	}
	g := groups.Items[0]
	if len(g.ID) != 24 || strings.Contains(g.ID, "mg_") || g.WorkspaceID != h.parent.WorkspaceID || g.CoordinatorID != "session-real" || g.Enabled || !g.Paused {
		t.Fatalf("derived group metadata: %+v", g)
	}
	if _, err := svc.ConfigureGroup(ctx, g.ID, g.Revision, 2, sm, true, false); err != nil {
		t.Fatal(err)
	}
	g, err = svc.GetGroup(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	i, err := svc.TrackWorkItem(ctx, g.ID, h.task.TaskID, "implementer")
	if err != nil {
		t.Fatal(err)
	}
	if i.State != "planned" || i.HostStatusID != "todo" || len(i.ID) != 24 {
		t.Fatalf("tracked item: %+v", i)
	}
	if ok, err := svc.GroupContainsTask(ctx, h.task.WorkspaceID, h.task.BoardID, h.task.TaskID); err != nil || !ok {
		t.Fatalf("membership=%v err=%v", ok, err)
	}
	h.task.StatusID = "review"
	h.task.StatusCategory = "active"
	h.task.Revision = 500
	i, err = svc.ReconcileWorkItem(ctx, i.ID)
	if err != nil {
		t.Fatal(err)
	}
	if i.State != "planned" || i.HostStatusID != "review" {
		t.Fatalf("board status became execution evidence: %+v", i)
	}
	h.task.Requirements.Title = "title edit is not a requirement change"
	h.task.Revision = 900
	i, err = svc.RefreshWorkItem(ctx, i.ID)
	if err != nil {
		t.Fatal(err)
	}
	if i.TaskRevision != 1 || i.State != "planned" {
		t.Fatalf("title-only edit fenced work: %+v", i)
	}
	h.task.Requirements.Description = "changed after tracking"
	h.task.Revision = 901
	i, err = svc.RefreshWorkItem(ctx, i.ID)
	if err != nil {
		t.Fatal(err)
	}
	if i.State != "stale" || i.TaskRevision != 2 {
		t.Fatalf("changed requirements state=%q", i.State)
	}
	h.task.StatusID = "shipped"
	h.task.StatusCategory = "terminal"
	h.task.Terminal = true
	h.task.Revision = 902
	i, err = svc.ReconcileWorkItem(ctx, i.ID)
	if err != nil {
		t.Fatal(err)
	}
	if i.EndedAt == nil || i.TombstoneReason != "terminal" || i.State != "ended" {
		t.Fatalf("terminal task wasn't tombstoned: %+v", i)
	}
	if ok, err := svc.GroupContainsTask(ctx, "ws-service", "board-service", "child-service"); err != nil || ok {
		t.Fatalf("terminal membership=%v err=%v", ok, err)
	}
	h.parent.Archived = true
	g, err = svc.ReconcileGroup(ctx, g.ID)
	if err != nil || g.EndedAt == nil || g.TombstoneReason != agentwork.TombstoneParentArchived {
		t.Fatalf("parent tombstone=%+v err=%v", g, err)
	}
	if _, err := svc.GetGroup(ctx, g.ID); err != nil {
		t.Fatalf("ended group should remain readable in workspace: %v", err)
	}
	h.workspace = "other-workspace"
	foreign, err := svc.GetGroup(ctx, g.ID)
	if !errors.Is(err, agentwork.ErrManagedTaskInvalid) {
		t.Fatalf("cross-workspace read: %v", err)
	}
	if foreign.ID != "" {
		t.Fatalf("cross-workspace GetGroup leaked data: %+v", foreign)
	}
	if foreign, err = svc.ConfigureGroup(ctx, g.ID, g.Revision, g.PolicyRevision+1, sm, false, true); !errors.Is(err, agentwork.ErrManagedTaskInvalid) || foreign.ID != "" {
		t.Fatalf("cross-workspace ConfigureGroup=%+v err=%v", foreign, err)
	}
	if foreign, err = svc.EndGroup(ctx, g.ID, g.Revision, agentwork.TombstoneUserEnded); !errors.Is(err, agentwork.ErrManagedTaskInvalid) || foreign.ID != "" {
		t.Fatalf("cross-workspace EndGroup=%+v err=%v", foreign, err)
	}
	if foreignItem, err := svc.TrackWorkItem(ctx, g.ID, "any-task", "implementer"); !errors.Is(err, agentwork.ErrManagedTaskInvalid) || foreignItem.ID != "" {
		t.Fatalf("cross-workspace TrackWorkItem=%+v err=%v", foreignItem, err)
	}
	if foreignItem, err := svc.ReconcileWorkItem(ctx, i.ID); !errors.Is(err, agentwork.ErrManagedTaskInvalid) || foreignItem.ID != "" {
		t.Fatalf("cross-workspace ReconcileWorkItem=%+v err=%v", foreignItem, err)
	}
}

func TestManagedServiceCanEndGroupWithLockedHostAndPinsAttemptOwnership(t *testing.T) {
	ctx := context.Background()
	db, l := openManagedTestLedger(t, filepath.Join(t.TempDir(), "end-locked.db"), time.Now)
	defer db.Close()
	statuses := []agentwork.BoardStatus{{ID: "todo", Category: "open"}, {ID: "doing", Category: "active"}, {ID: "review", Category: "active"}, {ID: "done", Category: "terminal"}}
	h := &managedServiceHostFake{workspace: "ws-end", authorized: true, parent: agentwork.ManagedParentSnapshot{WorkspaceID: "ws-end", BoardID: "board-end", ParentTaskID: "parent-end", CoordinatorID: "coord-real", Statuses: statuses}}
	h.task = agentwork.ManagedTaskSnapshot{WorkspaceID: "ws-end", BoardID: "board-end", ParentTaskID: "parent-end", TaskID: "child-end", StatusID: "todo", Requirements: agentwork.RequirementsSnapshot{Description: "bounded"}}
	svc := agentwork.NewManagedService(h, l)
	sm := agentwork.StatusMap{Ready: "todo", Running: "doing", AwaitingReview: "review", Accepted: "done"}
	g, err := svc.CreateGroup(ctx, "parent-end", sm)
	if err != nil {
		t.Fatal(err)
	}
	g, err = svc.ConfigureGroup(ctx, g.ID, g.Revision, 2, sm, true, false)
	if err != nil {
		t.Fatal(err)
	}
	i, err := svc.TrackWorkItem(ctx, g.ID, "child-end", "implementer")
	if err != nil {
		t.Fatal(err)
	}
	a, err := l.BeginAttempt(ctx, ManagedWorkAttempt{ID: "attempt-internal", WorkItemID: i.ID, TaskRevision: i.TaskRevision, RequirementsRevision: i.RequirementsRevision}, i.Revision)
	if err != nil {
		t.Fatal(err)
	}
	h.parentErr = agentwork.ErrManagedHostUnavailable
	h.parent.Statuses = nil
	ended, err := svc.EndGroup(ctx, g.ID, g.Revision, agentwork.TombstoneOperatorStopped)
	if err != nil {
		t.Fatal(err)
	}
	if ended.EndedAt == nil || ended.TombstoneReason != agentwork.TombstoneOperatorStopped || ended.Enabled || !ended.Paused {
		t.Fatalf("locked host couldn't safely end group: %+v", ended)
	}
	pinned, err := l.GetWorkItem(ctx, i.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pinned.EndedAt != nil || pinned.TombstoneReason != agentwork.TombstoneOperatorStopped || pinned.State != "blocked" || pinned.CurrentAttemptID != a.ID {
		t.Fatalf("active attempt ownership released: %+v", pinned)
	}
	if _, err = l.TransitionAttempt(ctx, a.ID, a.Revision, "session_created"); !errors.Is(err, ErrManagedScope) {
		t.Fatalf("ended/tombstoned attempt transition=%v", err)
	}
	if ok, err := svc.ManagedTaskMembership(ctx, "board-end", "parent-end"); err != nil || !ok {
		t.Fatalf("managed parent history membership=%v err=%v", ok, err)
	}
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
	group, err := l.GetWorkGroup(ctx, "g1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = l.EndWorkGroup(ctx, group.ID, group.Revision, agentwork.TombstoneUserEnded); err != nil {
		t.Fatal(err)
	}
	item, err = l.GetWorkItem(ctx, "i1")
	if err != nil {
		t.Fatal(err)
	}
	if item.EndedAt != nil || item.TombstoneReason != agentwork.TombstoneUserEnded || item.State != "blocked" {
		t.Fatalf("tombstoned item released a possibly live task slot: %+v", item)
	}
	if ok, err := l.ManagedTaskMembership(ctx, "board-1", "task-1"); err != nil || !ok {
		t.Fatalf("pinned membership=%v err=%v", ok, err)
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

func TestManagedLedgerStatusMapListCASAndTombstone(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "lifecycle.db")
	now := time.Date(2026, 10, 25, 2, 3, 4, 0, time.UTC)
	db, l := openManagedTestLedger(t, path, func() time.Time { return now })
	statusMap := agentwork.StatusMap{Ready: "todo", Running: "doing", AwaitingReview: "review", Accepted: "shipped"}
	g, err := l.CreateWorkGroup(ctx, ManagedWorkGroup{ID: "g-status", WorkspaceID: "ws-1", BoardID: "b-1", ParentTaskID: "p-1", CoordinatorID: "s-coord", PolicyRevision: 1, SchemaVersion: agentwork.ManagedSchemaVersion, StatusMap: statusMap})
	if err != nil {
		t.Fatal(err)
	}
	if g.Enabled || !g.Paused || g.SchemaVersion != agentwork.ManagedSchemaVersion || g.StatusMap != statusMap {
		t.Fatalf("initial group metadata: %+v", g)
	}
	if _, err = l.CreateWorkItem(ctx, ManagedWorkItem{ID: "item-a", GroupID: g.ID, WorkspaceID: g.WorkspaceID, BoardID: g.BoardID, ParentTaskID: g.ParentTaskID, TaskID: "task-shared", TaskRevision: 1, RequirementsRevision: "digest-1", Role: "implementer", HostStatusID: "todo"}); err != nil {
		t.Fatal(err)
	}
	if _, err = l.CreateWorkItem(ctx, ManagedWorkItem{ID: "item-b", GroupID: g.ID, WorkspaceID: g.WorkspaceID, BoardID: g.BoardID, ParentTaskID: g.ParentTaskID, TaskID: "task-shared", TaskRevision: 1, RequirementsRevision: "digest-1", Role: "implementer"}); !errors.Is(err, ErrManagedConflict) {
		t.Fatalf("duplicate active task association: %v", err)
	}
	page, err := l.ListWorkGroups(ctx, "ws-1", "", 10)
	if err != nil || len(page.Items) != 1 || page.Items[0].StatusMap != statusMap {
		t.Fatalf("group page=%+v err=%v", page, err)
	}
	updated, err := l.UpdateGroupConfiguration(ctx, g.ID, g.Revision, 2, statusMap, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = l.UpdateGroupConfiguration(ctx, g.ID, g.Revision, 3, statusMap, false, true); !errors.Is(err, ErrManagedConflict) {
		t.Fatalf("stale config CAS: %v", err)
	}
	item, err := l.GetWorkItem(ctx, "item-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = l.UpdateWorkItemHostState(ctx, item.ID, item.Revision, 1, item.RequirementsRevision, "todo", "planned", "sensitive freeform data"); !errors.Is(err, ErrManagedInvalid) {
		t.Fatalf("freeform tombstone accepted: %v", err)
	}
	if _, err = l.EndWorkGroup(ctx, g.ID, updated.Revision, "sensitive freeform data"); !errors.Is(err, ErrManagedInvalid) {
		t.Fatalf("freeform group reason accepted: %v", err)
	}
	marked, err := l.UpdateWorkItemHostState(ctx, item.ID, item.Revision, 2, "digest-2", "review", "awaiting_review", "")
	if err != nil {
		t.Fatal(err)
	}
	if marked.HostStatusID != "review" || marked.State != "awaiting_review" || marked.TaskRevision != 2 {
		t.Fatalf("host status mapping not persisted: %+v", marked)
	}
	if marked.Revision != item.Revision+1 {
		t.Fatalf("host CAS returned revision %d, want %d", marked.Revision, item.Revision+1)
	}
	ended, err := l.EndWorkGroup(ctx, g.ID, updated.Revision, agentwork.TombstoneParentArchived)
	if err != nil {
		t.Fatal(err)
	}
	if ended.EndedAt == nil || ended.TombstoneReason != agentwork.TombstoneParentArchived || ended.Enabled || !ended.Paused {
		t.Fatalf("group tombstone: %+v", ended)
	}
	item, err = l.GetWorkItem(ctx, "item-a")
	if err != nil {
		t.Fatal(err)
	}
	if item.EndedAt == nil || item.TombstoneReason != agentwork.TombstoneParentArchived {
		t.Fatalf("item tombstone: %+v", item)
	}
	if ok, err := l.GroupContainsTask(ctx, "ws-1", "b-1", "task-shared"); err != nil || ok {
		t.Fatalf("ended membership=%v err=%v", ok, err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, l = openManagedTestLedger(t, path, func() time.Time { return now.Add(time.Minute) })
	defer db.Close()
	reloaded, err := l.GetWorkGroup(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.EndedAt == nil || reloaded.StatusMap != statusMap || reloaded.SchemaVersion != agentwork.ManagedSchemaVersion {
		t.Fatalf("reopened lifecycle metadata: %+v", reloaded)
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
