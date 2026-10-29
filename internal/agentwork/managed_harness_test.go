package agentwork

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fakeHarnessStore keeps one attempt, item, group and package in memory and
// records every call so a test can prove the binding wrote nothing else.
type fakeHarnessStore struct {
	attempt  Attempt
	item     WorkItem
	group    GroupState
	pkg      StoredContextPackage
	bindings map[string]HarnessBinding
	calls    []string
}

func (s *fakeHarnessStore) GetManagedAttempt(_ context.Context, id string) (Attempt, error) {
	s.calls = append(s.calls, "GetManagedAttempt")
	if id != s.attempt.ID {
		return Attempt{}, ErrManagedRecordNotFound
	}
	return s.attempt, nil
}

func (s *fakeHarnessStore) GetCurrentManagedAttempt(_ context.Context, itemID string) (Attempt, error) {
	s.calls = append(s.calls, "GetCurrentManagedAttempt")
	if itemID != s.item.ID || s.item.CurrentAttemptID != s.attempt.ID {
		return Attempt{}, ErrManagedRecordNotFound
	}
	return s.attempt, nil
}

func (s *fakeHarnessStore) GetWorkItem(context.Context, string) (WorkItem, error) {
	s.calls = append(s.calls, "GetWorkItem")
	return s.item, nil
}

func (s *fakeHarnessStore) GetWorkGroup(context.Context, string) (GroupState, error) {
	s.calls = append(s.calls, "GetWorkGroup")
	return s.group, nil
}

func (s *fakeHarnessStore) GetContextPackage(_ context.Context, id string) (StoredContextPackage, error) {
	s.calls = append(s.calls, "GetContextPackage")
	if id != s.pkg.ID {
		return StoredContextPackage{}, ErrManagedRecordNotFound
	}
	return s.pkg, nil
}

func (s *fakeHarnessStore) InsertHarnessBinding(_ context.Context, b HarnessBinding, expectedAttemptRevision int64) error {
	s.calls = append(s.calls, "InsertHarnessBinding")
	if expectedAttemptRevision != s.attempt.Revision {
		return ErrStaleRevision
	}
	key := b.AttemptID + "/" + b.Phase
	if _, ok := s.bindings[key]; ok {
		return ErrHarnessBindingExists
	}
	if s.bindings == nil {
		s.bindings = map[string]HarnessBinding{}
	}
	s.bindings[key] = b
	return nil
}

func (s *fakeHarnessStore) GetHarnessBinding(_ context.Context, attemptID, phase string) (HarnessBinding, error) {
	s.calls = append(s.calls, "GetHarnessBinding")
	b, ok := s.bindings[attemptID+"/"+phase]
	if !ok {
		return HarnessBinding{}, ErrManagedRecordNotFound
	}
	return b, nil
}

func (s *fakeHarnessStore) writes() int {
	n := 0
	for _, c := range s.calls {
		if c == "InsertHarnessBinding" {
			n++
		}
	}
	return n
}

type fakeHarnessHost struct {
	required                   map[string][]string
	intended, actual           HarnessResolution
	intendedErr, actualErr     error
	requiredErr                error
	intendedCalls, actualCalls int
	gotChild                   IntendedChild
}

func (h *fakeHarnessHost) RequiredGroups(_ context.Context, role string) ([]string, error) {
	return h.required[role], h.requiredErr
}

func (h *fakeHarnessHost) ResolveIntended(_ context.Context, _ PackageScope, child IntendedChild) (HarnessResolution, error) {
	h.intendedCalls++
	h.gotChild = child
	return h.intended, h.intendedErr
}

func (h *fakeHarnessHost) ResolveActual(context.Context, Attempt) (HarnessResolution, error) {
	h.actualCalls++
	return h.actual, h.actualErr
}

type harnessRig struct {
	store *fakeHarnessStore
	host  *fakeHarnessHost
	svc   *ManagedHarnessService
}

func newHarnessRig(t *testing.T) *harnessRig {
	t.Helper()
	required := []string{"agenthub", "git", "tasks"}
	facts := baseFacts()
	store := &fakeHarnessStore{
		group: GroupState{ID: "g1", WorkspaceID: "ws-1", BoardID: "b1", ParentTaskID: "p1", CoordinatorID: "coord", Enabled: true, SchemaVersion: ManagedSchemaVersion, Revision: 1},
		item: WorkItem{ID: "i1", GroupID: "g1", WorkspaceID: "ws-1", BoardID: "b1", ParentTaskID: "p1", TaskID: "t1", Role: "implementer",
			RequirementsRevision: "v2:req-a", CurrentAttemptID: "a1", Revision: 2, SchemaVersion: ManagedSchemaVersion},
		attempt: Attempt{ID: "a1", WorkItemID: "i1", GroupID: "g1", WorkspaceID: "ws-1", BoardID: "b1", ParentTaskID: "p1", Role: "implementer",
			State: "reserved", DispatchState: "reserved", Generation: 1, Revision: 1, RequirementsRevision: "v2:req-a", SchemaVersion: ManagedSchemaVersion},
		pkg: StoredContextPackage{ContextPackage: ContextPackage{ID: "pk1", WorkspaceID: "ws-1", BoardID: "b1", ParentTaskID: "p1", GroupID: "g1", WorkItemID: "i1", TaskID: "t1", AttemptID: "a1",
			Role: "implementer", RequirementsRevision: "v2:req-a", Generation: 1, SchemaVersion: ManagedPackageSchemaVersion, BodyHash: "sha256:abc", BodyBytes: 10, CreatedAt: time.Unix(1, 0)},
			ItemRevision: 2, AttemptRevision: 1, SealedBody: []byte("sealed")},
	}
	host := &fakeHarnessHost{
		required: map[string][]string{"implementer": required, "reviewer": {"agenthub", "tasks"}},
		intended: HarnessResolution{Facts: facts, Groups: required},
		actual:   HarnessResolution{Facts: facts, Groups: required},
	}
	return &harnessRig{store: store, host: host, svc: NewManagedHarnessService(store, host)}
}

func (r *harnessRig) child() IntendedChild {
	return IntendedChild{Vendor: "claude", Role: "implementer"}
}

// openChild moves the fake attempt to the state where its session exists.
func (r *harnessRig) openChild() {
	r.store.attempt.State, r.store.attempt.DispatchState = "session_created", "session_created"
	r.store.attempt.SessionID = "child-1"
	r.store.attempt.Revision = 2
}

func (r *harnessRig) bindBoth(t *testing.T) (HarnessBinding, HarnessBinding) {
	t.Helper()
	ctx := context.Background()
	in, err := r.svc.BindIntended(ctx, "pk1", r.child(), 1)
	if err != nil {
		t.Fatalf("bind intended: %v", err)
	}
	r.openChild()
	ac, err := r.svc.BindActual(ctx, "a1", 2)
	if err != nil {
		t.Fatalf("bind actual: %v", err)
	}
	return in, ac
}

func wantRefusal(t *testing.T, err error, code string) *HarnessRefusal {
	t.Helper()
	var r *HarnessRefusal
	if !errors.As(err, &r) {
		t.Fatalf("want refusal %s, got %v", code, err)
	}
	if r.Code != code {
		t.Fatalf("want refusal %s, got %s", code, r.Code)
	}
	return r
}

func TestPreflightIntendedMissingGroupReturnsConfigurationNeededAndWritesNothing(t *testing.T) {
	rig := newHarnessRig(t)
	rig.host.intended.Groups = []string{"tasks", "git"}
	_, err := rig.svc.PreflightIntended(context.Background(), "a1", rig.child())
	r := wantRefusal(t, err, HarnessRequiredGroupOff)
	if !reflect.DeepEqual(r.Groups, []string{"agenthub"}) || !ConfigurationNeeded(err) {
		t.Fatalf("groups = %v, configuration-needed = %v", r.Groups, ConfigurationNeeded(err))
	}
	if rig.store.writes() != 0 || len(rig.store.bindings) != 0 {
		t.Fatalf("a refused preflight must write nothing")
	}
}

func TestPreflightIntendedDistinguishesNotConfiguredFromResolvedDefault(t *testing.T) {
	rig := newHarnessRig(t)
	notConfigured := NewManagedHarnessService(rig.store, nil)
	_, err := notConfigured.PreflightIntended(context.Background(), "a1", rig.child())
	wantRefusal(t, err, HarnessNotConfigured)
	if !ConfigurationNeeded(err) {
		t.Fatalf("not configured is fixable by configuration")
	}
	rig.host.intendedErr = &HarnessRefusal{Code: HarnessNotConfigured}
	_, err = rig.svc.PreflightIntended(context.Background(), "a1", rig.child())
	wantRefusal(t, err, HarnessNotConfigured)

	rig.host.intendedErr = nil
	rig.host.intended.Facts.Default = true // positively resolved, unprofiled default: every group on
	res, err := rig.svc.PreflightIntended(context.Background(), "a1", rig.child())
	if err != nil || !res.Facts.Default {
		t.Fatalf("a resolved default must pass preflight: %v %+v", err, res)
	}
}

func TestPreflightIntendedChecksTheRoleAndTheGroup(t *testing.T) {
	rig := newHarnessRig(t)
	_, err := rig.svc.PreflightIntended(context.Background(), "a1", IntendedChild{Vendor: "claude", Role: "reviewer"})
	wantRefusal(t, err, HarnessChildScope)
	if rig.host.intendedCalls != 0 {
		t.Fatalf("a role that is not the attempt's must be refused before any resolve")
	}
	rig.store.group.Paused = true
	if _, err = rig.svc.PreflightIntended(context.Background(), "a1", rig.child()); !errors.Is(err, ErrHarnessPolicyClosed) {
		t.Fatalf("paused group: %v", err)
	}
	rig = newHarnessRig(t)
	rig.host.required["implementer"] = nil
	_, err = rig.svc.PreflightIntended(context.Background(), "a1", rig.child())
	wantRefusal(t, err, HarnessUnavailable)
}

func TestBindIntendedRefusesAnAttemptThatIsNotReserved(t *testing.T) {
	rig := newHarnessRig(t)
	rig.openChild()
	_, err := rig.svc.BindIntended(context.Background(), "pk1", rig.child(), 2)
	if !errors.Is(err, ErrHarnessAttemptState) {
		t.Fatalf("want attempt-state refusal, got %v", err)
	}
	if rig.store.writes() != 0 {
		t.Fatalf("nothing may be written")
	}
	rig = newHarnessRig(t)
	if _, err = rig.svc.BindIntended(context.Background(), "pk1", rig.child(), 7); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("a stale expected revision must be refused, got %v", err)
	}
	rig.store.item.CurrentAttemptID = "other"
	if _, err = rig.svc.BindIntended(context.Background(), "pk1", rig.child(), 1); err == nil {
		t.Fatalf("an attempt that is no longer current must be refused")
	}
	rig = newHarnessRig(t)
	rig.store.pkg.AttemptID = "a2"
	if _, err = rig.svc.BindIntended(context.Background(), "pk1", rig.child(), 1); err == nil || rig.store.writes() != 0 {
		t.Fatalf("a package of another attempt must be refused, got %v", err)
	}
}

func TestBindIntendedLeavesThePackageRowUntouched(t *testing.T) {
	rig := newHarnessRig(t)
	before := rig.store.pkg
	before.SealedBody = append([]byte(nil), before.SealedBody...)
	b, err := rig.svc.BindIntended(context.Background(), "pk1", rig.child(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, rig.store.pkg) {
		t.Fatalf("the package row changed")
	}
	for _, c := range rig.store.calls {
		if c != "GetContextPackage" && c != "GetManagedAttempt" && c != "GetCurrentManagedAttempt" && c != "GetWorkItem" && c != "GetWorkGroup" && c != "InsertHarnessBinding" {
			t.Fatalf("unexpected store call %s", c)
		}
	}
	if b.Phase != HarnessPhaseIntended || b.PackageID != "pk1" || b.AttemptID != "a1" || b.SessionID != "" || b.SchemaVersion != HarnessBindingSchemaVersion {
		t.Fatalf("binding = %+v", b)
	}
	if !reflect.DeepEqual(b.Required, []string{"agenthub", "git", "tasks"}) {
		t.Fatalf("required = %v", b.Required)
	}
}

func TestBindActualRequiresBoundSession(t *testing.T) {
	rig := newHarnessRig(t)
	if _, err := rig.svc.BindIntended(context.Background(), "pk1", rig.child(), 1); err != nil {
		t.Fatal(err)
	}
	rig.openChild()
	rig.store.attempt.SessionID = ""
	_, err := rig.svc.BindActual(context.Background(), "a1", 2)
	wantRefusal(t, err, HarnessChildUnbound)
	if rig.host.actualCalls != 0 || ConfigurationNeeded(err) {
		t.Fatalf("an unbound child must be refused before the host is asked")
	}
}

func TestBindActualOnlyRunsForSessionCreatedOrTaskAssigned(t *testing.T) {
	rig := newHarnessRig(t)
	if _, err := rig.svc.BindIntended(context.Background(), "pk1", rig.child(), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.svc.BindActual(context.Background(), "a1", 1); !errors.Is(err, ErrHarnessAttemptState) {
		t.Fatalf("reserved attempt: %v", err)
	}
	for _, state := range []string{"prompt_enqueued", "running", "failed"} {
		rig.openChild()
		rig.store.attempt.State = state
		if _, err := rig.svc.BindActual(context.Background(), "a1", 2); !errors.Is(err, ErrHarnessAttemptState) {
			t.Fatalf("%s attempt: %v", state, err)
		}
	}
	rig.store.attempt.State = "task_assigned"
	if _, err := rig.svc.BindActual(context.Background(), "a1", 2); err != nil {
		t.Fatalf("task_assigned must bind: %v", err)
	}
	if rig.host.actualCalls != 1 {
		t.Fatalf("the actual check ran %d times", rig.host.actualCalls)
	}
}

func TestBindActualRequiresTheIntendedBinding(t *testing.T) {
	rig := newHarnessRig(t)
	rig.openChild()
	_, err := rig.svc.BindActual(context.Background(), "a1", 2)
	wantRefusal(t, err, HarnessBindingMissing)
	if rig.host.actualCalls != 0 || rig.store.writes() != 0 {
		t.Fatalf("no host call and no write without an intended binding")
	}
}

func TestBindActualRefusesChildWithOwnOverride(t *testing.T) {
	rig := newHarnessRig(t)
	if _, err := rig.svc.BindIntended(context.Background(), "pk1", rig.child(), 1); err != nil {
		t.Fatal(err)
	}
	rig.openChild()
	// Every required group is served and the fingerprint even matches, but the
	// child has an S of its own: parent state must never be the source of it.
	rig.host.actual.Facts.SessionRevision = i64(4)
	_, err := rig.svc.BindActual(context.Background(), "a1", 2)
	wantRefusal(t, err, HarnessOverridePresent)
	if !ConfigurationNeeded(err) || rig.store.writes() != 1 {
		t.Fatalf("only the intended row may exist (writes=%d)", rig.store.writes())
	}
}

func TestBindActualDetectsWorkspaceOrVendorChangeBetweenIntendedAndOpen(t *testing.T) {
	for name, mutate := range map[string]func(*HarnessFacts){
		"workspace edit": func(f *HarnessFacts) { f.WorkspaceRevision = i64(3) },
		"vendor edit":    func(f *HarnessFacts) { f.VendorRevision = 4 },
	} {
		rig := newHarnessRig(t)
		if _, err := rig.svc.BindIntended(context.Background(), "pk1", rig.child(), 1); err != nil {
			t.Fatal(err)
		}
		rig.openChild()
		mutate(&rig.host.actual.Facts)
		_, err := rig.svc.BindActual(context.Background(), "a1", 2)
		if r := wantRefusal(t, err, HarnessConfigChanged); r == nil || !ConfigurationNeeded(err) {
			t.Fatalf("%s: %v", name, err)
		}
		if _, ok := rig.store.bindings["a1/actual"]; ok {
			t.Fatalf("%s: a changed configuration must not be bound", name)
		}
	}
}

func TestBindActualSameModelDifferentHarnessIsConfigChanged(t *testing.T) {
	// The service never sees a model. Two children with the same vendor and
	// model but different harness fingerprints are different configurations.
	rig := newHarnessRig(t)
	if _, err := rig.svc.BindIntended(context.Background(), "pk1", rig.child(), 1); err != nil {
		t.Fatal(err)
	}
	rig.openChild()
	rig.host.actual.Facts.Fingerprint = "fp-other-harness"
	_, err := rig.svc.BindActual(context.Background(), "a1", 2)
	wantRefusal(t, err, HarnessConfigChanged)
}

func TestBindActualRevokedGroupIsRequiredGroupOff(t *testing.T) {
	rig := newHarnessRig(t)
	if _, err := rig.svc.BindIntended(context.Background(), "pk1", rig.child(), 1); err != nil {
		t.Fatal(err)
	}
	rig.openChild()
	rig.host.actual.Groups = []string{"agenthub", "tasks"}
	_, err := rig.svc.BindActual(context.Background(), "a1", 2)
	r := wantRefusal(t, err, HarnessRequiredGroupOff)
	if !reflect.DeepEqual(r.Groups, []string{"git"}) {
		t.Fatalf("groups = %v", r.Groups)
	}
}

func TestBindActualConfigReadFailureIsUnavailableNotDefault(t *testing.T) {
	rig := newHarnessRig(t)
	if _, err := rig.svc.BindIntended(context.Background(), "pk1", rig.child(), 1); err != nil {
		t.Fatal(err)
	}
	rig.openChild()
	rig.host.actualErr = errors.New("store: disk I/O error with /secret/path")
	_, err := rig.svc.BindActual(context.Background(), "a1", 2)
	r := wantRefusal(t, err, HarnessUnavailable)
	if ConfigurationNeeded(err) || r.Groups != nil || strings.Contains(err.Error(), "secret") || errors.Is(err, rig.host.actualErr) {
		t.Fatalf("an unreadable configuration is retryable, never a default and never leaks the cause: %v", err)
	}
	rig.host.actualErr = &HarnessRefusal{Code: HarnessEndpointNotLive}
	_, err = rig.svc.BindActual(context.Background(), "a1", 2)
	wantRefusal(t, err, HarnessEndpointNotLive)
	for _, code := range []string{HarnessChildScope, HarnessChildMode} {
		rig.host.actualErr = &HarnessRefusal{Code: code}
		_, err = rig.svc.BindActual(context.Background(), "a1", 2)
		wantRefusal(t, err, code)
	}
}

func TestVerifyForEnqueueSendsNothingAndDetectsDrift(t *testing.T) {
	rig := newHarnessRig(t)
	rig.bindBoth(t)
	rig.store.calls = nil
	attemptBefore := rig.store.attempt
	if err := rig.svc.VerifyForEnqueue(context.Background(), "a1"); err != nil {
		t.Fatalf("an unchanged child must verify: %v", err)
	}
	rig.host.actual.Groups = []string{"agenthub", "tasks"} // revoked after launch
	err := rig.svc.VerifyForEnqueue(context.Background(), "a1")
	wantRefusal(t, err, HarnessRequiredGroupOff)
	rig.host.actual.Groups = []string{"agenthub", "git", "tasks"}
	rig.host.actual.Facts.Fingerprint = "drift"
	wantRefusal(t, rig.svc.VerifyForEnqueue(context.Background(), "a1"), HarnessConfigChanged)
	if rig.store.writes() != 0 || !reflect.DeepEqual(attemptBefore, rig.store.attempt) {
		t.Fatalf("verification must not write or transition anything")
	}
	rig.host.actual.Facts.Fingerprint = baseFacts().Fingerprint
	rig.store.attempt.State = "prompt_enqueued"
	if err = rig.svc.VerifyForEnqueue(context.Background(), "a1"); !errors.Is(err, ErrHarnessAttemptState) {
		t.Fatalf("verification belongs before the enqueue: %v", err)
	}
}

func TestVerifyForEnqueueRefusesWithoutAnActualBindingOrWithAnotherSession(t *testing.T) {
	rig := newHarnessRig(t)
	if _, err := rig.svc.BindIntended(context.Background(), "pk1", rig.child(), 1); err != nil {
		t.Fatal(err)
	}
	rig.openChild()
	wantRefusal(t, rig.svc.VerifyForEnqueue(context.Background(), "a1"), HarnessBindingMissing)
	if _, err := rig.svc.BindActual(context.Background(), "a1", 2); err != nil {
		t.Fatal(err)
	}
	rig.store.attempt.SessionID = "someone-else"
	wantRefusal(t, rig.svc.VerifyForEnqueue(context.Background(), "a1"), HarnessBindingStale)
	rig.store.attempt.SessionID = "child-1"
	rig.store.group.Enabled = false
	if err := rig.svc.VerifyForEnqueue(context.Background(), "a1"); !errors.Is(err, ErrHarnessPolicyClosed) {
		t.Fatalf("a disabled group closes the gate: %v", err)
	}
}

func TestCheckBindingStaleIsReportedWithoutMutationAndRebuildRequired(t *testing.T) {
	rig := newHarnessRig(t)
	ctx := context.Background()
	st, err := rig.svc.CheckBinding(ctx, "a1")
	if err != nil || st.State != HarnessStateMissing || st.RebuildRequired || st.Code != HarnessBindingMissing {
		t.Fatalf("no binding yet: %+v %v", st, err)
	}
	if _, err = rig.svc.BindIntended(ctx, "pk1", rig.child(), 1); err != nil {
		t.Fatal(err)
	}
	if st, err = rig.svc.CheckBinding(ctx, "a1"); err != nil || st.State != HarnessStateCurrent || st.RebuildRequired || st.Phase != HarnessPhaseIntended {
		t.Fatalf("intended binding, unchanged: %+v %v", st, err)
	}
	rig.host.intended.Facts.WorkspaceRevision = i64(5)
	snapshot := map[string]HarnessBinding{}
	for k, v := range rig.store.bindings {
		snapshot[k] = v
	}
	writes := rig.store.writes()
	st, err = rig.svc.CheckBinding(ctx, "a1")
	if err != nil || st.State != HarnessStateStale || st.Code != HarnessConfigChanged || !st.RebuildRequired {
		t.Fatalf("stale intended: %+v %v", st, err)
	}
	if rig.store.writes() != writes || !reflect.DeepEqual(snapshot, rig.store.bindings) {
		t.Fatalf("the check must not mutate")
	}
	// An actual binding takes over once the child exists.
	rig.host.intended.Facts.WorkspaceRevision = baseFacts().WorkspaceRevision
	rig.openChild()
	if _, err = rig.svc.BindActual(ctx, "a1", 2); err != nil {
		t.Fatal(err)
	}
	if st, _ = rig.svc.CheckBinding(ctx, "a1"); st.State != HarnessStateCurrent || st.Phase != HarnessPhaseActual {
		t.Fatalf("actual current: %+v", st)
	}
	rig.host.actual.Groups = []string{"agenthub", "tasks"}
	st, _ = rig.svc.CheckBinding(ctx, "a1")
	if st.State != HarnessStateStale || st.Code != HarnessRequiredGroupOff || !st.RebuildRequired {
		t.Fatalf("revoked group: %+v", st)
	}
	// A transient endpoint problem is stale-for-now but no reason to rebuild.
	rig.host.actualErr = &HarnessRefusal{Code: HarnessEndpointNotLive}
	if st, _ = rig.svc.CheckBinding(ctx, "a1"); st.State != HarnessStateStale || st.Code != HarnessEndpointNotLive || st.RebuildRequired {
		t.Fatalf("endpoint not live: %+v", st)
	}
	// A host failure is an error, not a status.
	rig.host.actualErr = errors.New("boom")
	if st, err = rig.svc.CheckBinding(ctx, "a1"); err == nil {
		t.Fatalf("a host failure must be an error, got %+v", st)
	}
	wantRefusal(t, err, HarnessUnavailable)
}

func TestCheckBindingReportsARoleTableThatChangedAsStale(t *testing.T) {
	rig := newHarnessRig(t)
	if _, err := rig.svc.BindIntended(context.Background(), "pk1", rig.child(), 1); err != nil {
		t.Fatal(err)
	}
	rig.host.required["implementer"] = []string{"agenthub", "git", "tasks", "extra"}
	st, err := rig.svc.CheckBinding(context.Background(), "a1")
	if err != nil || st.State != HarnessStateStale || st.Code != HarnessBindingStale || !st.RebuildRequired {
		t.Fatalf("%+v %v", st, err)
	}
}

func TestSecondBindForTheSameAttemptNeverReplacesTheFirst(t *testing.T) {
	rig := newHarnessRig(t)
	ctx := context.Background()
	first, err := rig.svc.BindIntended(ctx, "pk1", rig.child(), 1)
	if err != nil {
		t.Fatal(err)
	}
	rig.host.intended.Facts.Fingerprint = "fp-new"
	if _, err = rig.svc.BindIntended(ctx, "pk1", rig.child(), 1); !errors.Is(err, ErrHarnessBindingExists) {
		t.Fatalf("second intended bind: %v", err)
	}
	if got := rig.store.bindings["a1/intended"]; !reflect.DeepEqual(got, first) {
		t.Fatalf("the original binding changed: %+v", got)
	}
	rig.host.intended.Facts.Fingerprint = baseFacts().Fingerprint
	rig.openChild()
	firstActual, err := rig.svc.BindActual(ctx, "a1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = rig.svc.BindActual(ctx, "a1", 2); !errors.Is(err, ErrHarnessBindingExists) {
		t.Fatalf("second actual bind: %v", err)
	}
	if got := rig.store.bindings["a1/actual"]; !reflect.DeepEqual(got, firstActual) {
		t.Fatalf("the original actual binding changed")
	}
}

func TestRequirementsDigestAndHarnessStalenessAreIndependent(t *testing.T) {
	rig := newHarnessRig(t)
	ctx := context.Background()
	snap := RequirementsSnapshot{WorkspaceID: "ws-1", BoardID: "b1", TaskID: "t1", Branch: "feature/x", Scope: []string{"a"}, Dependencies: []string{}}
	digest, err := RequirementsDigest(snap)
	if err != nil {
		t.Fatal(err)
	}
	rig.store.item.RequirementsRevision, rig.store.attempt.RequirementsRevision, rig.store.pkg.RequirementsRevision = digest, digest, digest
	if _, err = rig.svc.BindIntended(ctx, "pk1", rig.child(), 1); err != nil {
		t.Fatal(err)
	}
	// A requirements change leaves the harness binding current.
	snap.Scope = []string{"a", "b"}
	changed, _ := RequirementsDigest(snap)
	if changed == digest {
		t.Fatalf("test needs a different requirements digest")
	}
	rig.store.item.RequirementsRevision = changed
	if st, err := rig.svc.CheckBinding(ctx, "a1"); err != nil || st.State != HarnessStateCurrent {
		t.Fatalf("a requirements change must not make the harness binding stale: %+v %v", st, err)
	}
	// A harness change leaves the requirements digest as it was.
	rig.host.intended.Facts.Fingerprint = "fp-2"
	if st, _ := rig.svc.CheckBinding(ctx, "a1"); st.State != HarnessStateStale {
		t.Fatalf("a harness change must make the binding stale: %+v", st)
	}
	snap.Scope = []string{"a"}
	again, _ := RequirementsDigest(snap)
	if again != digest {
		t.Fatalf("the requirements digest depends on harness state")
	}
}

func TestCheckBindingUnknownBindingSchemaFailsClosed(t *testing.T) {
	rig := newHarnessRig(t)
	if _, err := rig.svc.BindIntended(context.Background(), "pk1", rig.child(), 1); err != nil {
		t.Fatal(err)
	}
	b := rig.store.bindings["a1/intended"]
	b.SchemaVersion = 99
	rig.store.bindings["a1/intended"] = b
	st, err := rig.svc.CheckBinding(context.Background(), "a1")
	if err != nil || st.State != HarnessStateStale || st.Code != HarnessSchemaUnsupported {
		t.Fatalf("%+v %v", st, err)
	}
}
