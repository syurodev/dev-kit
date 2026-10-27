package harnessconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"devkit/internal/harness"
	"devkit/internal/identity"
)

// A stored profile that has known settings next to stale ones.
func mixedProfile(p *harness.Profile) {
	p.ToolGroups = map[string]bool{"git": false, "gone": true, "pdf": false}
	p.ResultBudget = &harness.BudgetProfile{
		Tools:  map[string]harness.BudgetRule{"git.status": {Bytes: intp(8192)}, "gone.tool": {Bytes: intp(4096), Cut: harness.CutHeadTail}},
		Groups: map[string]harness.BudgetRule{"tasks": {Cut: harness.CutHeadTail}, "ghost": {Bytes: intp(3000)}},
	}
	p.DisabledMCP = []string{"other"}
}

// ---- the pure helper -------------------------------------------------------------

func TestInspectStaleFindsOnlyUnknownNamesAndKeepsTheRest(t *testing.T) {
	p := &harness.Profile{Schema: harness.SchemaVersion}
	mixedProfile(p)
	got := InspectStale(p, registry())
	want := []StaleEntry{
		{Kind: StaleGroup, Name: "gone", On: boolp(true)},
		{Kind: StaleGroup, Name: "pdf", On: boolp(false)},
		{Kind: StaleGroupBudget, Name: "ghost", Bytes: intp(3000)},
		{Kind: StaleToolBudget, Name: "gone.tool", Bytes: intp(4096), Cut: "headTail"},
	}
	if a, b := mustJSON(t, got), mustJSON(t, want); a != b {
		t.Fatalf("stale = %s\nwant    %s", a, b)
	}
	repaired := RepairedProfile(p, got)
	wantKept := &harness.Profile{Schema: harness.SchemaVersion,
		ToolGroups: map[string]bool{"git": false},
		ResultBudget: &harness.BudgetProfile{
			Tools:  map[string]harness.BudgetRule{"git.status": {Bytes: intp(8192)}},
			Groups: map[string]harness.BudgetRule{"tasks": {Cut: harness.CutHeadTail}}},
		DisabledMCP: []string{"other"}}
	if a, b := mustJSON(t, repaired), mustJSON(t, wantKept); a != b {
		t.Fatalf("repaired = %s\nwant       %s", a, b)
	}
	if len(p.ToolGroups) != 3 || len(p.ResultBudget.Tools) != 2 {
		t.Fatal("the stored value was modified in place")
	}
}

func TestAnExemptToolWithABudgetIsNotStale(t *testing.T) {
	// The tool exists; a budget on it is a different (invalid) problem.
	p := &harness.Profile{Schema: harness.SchemaVersion, ResultBudget: &harness.BudgetProfile{
		Tools: map[string]harness.BudgetRule{"codegraph.readFile": {Bytes: intp(4096)}}}}
	if got := InspectStale(p, registry()); len(got) != 0 {
		t.Fatalf("stale = %+v", got)
	}
}

func TestEmptyMapsAreAbsentAfterTheRepairLikeTheBudgetEditorLeavesThem(t *testing.T) {
	p := &harness.Profile{Schema: harness.SchemaVersion, ToolGroups: map[string]bool{"gone": false},
		ResultBudget: &harness.BudgetProfile{Tools: map[string]harness.BudgetRule{"gone.t": {Bytes: intp(2048)}}}}
	r := RepairedProfile(p, InspectStale(p, registry()))
	if r.ToolGroups != nil || r.ResultBudget != nil {
		t.Fatalf("empty containers must be absent: %+v", r)
	}
	if got := mustJSON(t, r); got != `{"schema":1,"revision":0}` {
		t.Fatalf("repaired body = %s", got)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ---- diagnosis -------------------------------------------------------------------

func TestDiagnosisIsForTheWindowOnly(t *testing.T) {
	a := newApplyEnv(t)
	a.seedProfile("w1", mixedProfile)
	for name, ctx := range map[string]context.Context{
		"agent":    agent("s1"),
		"terminal": identity.WithCaller(context.Background(), identity.UserTerminal()),
		"unknown":  context.Background(),
	} {
		if _, err := a.svc.DiagnoseStale(ctx, "w1"); code(t, err) != harness.CodeProfileDenied {
			t.Errorf("%s diagnosis = %v", name, err)
		}
		if _, err := a.svc.PrepareRepair(ctx, "w1"); code(t, err) != harness.CodeProfileDenied {
			t.Errorf("%s prepare = %v", name, err)
		}
	}
	if _, err := a.svc.DiagnoseStale(ui(), "nope"); !errors.Is(err, ErrScopeGone) {
		t.Fatalf("absent workspace = %v", err)
	}
	if _, err := a.svc.DiagnoseStale(ui(), ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty workspace = %v", err)
	}
}

func TestDiagnosisNeedsNoSessionAndShowsEveryEntryAndItsValue(t *testing.T) {
	a := newApplyEnv(t)
	st := a.seedProfile("w1", mixedProfile)
	d, err := a.svc.DiagnoseStale(ui(), "w1")
	if err != nil {
		t.Fatal(err)
	}
	if d.State != StaleRepairable || d.Total != 4 || d.Truncated || d.ProfileRevision != st.Revision || !d.ProfilePresent {
		t.Fatalf("diagnosis = %+v", d)
	}
	var names []string
	for _, e := range d.Entries {
		names = append(names, string(e.Kind)+":"+e.Name)
	}
	if got := strings.Join(names, ","); got != "group:gone,group:pdf,groupBudget:ghost,toolBudget:gone.tool" {
		t.Fatalf("entries = %s", got)
	}
	if d.Entries[1].On == nil || *d.Entries[1].On {
		t.Fatal("the stale off setting must be shown as off")
	}
	// Nothing was written and nothing is pending.
	if _, now := a.profile("w1"); now != st || a.svc.Pending() != 0 {
		t.Fatal("a diagnosis must write nothing and prepare nothing")
	}
}

func TestNoProfileAndAHealthyProfileAreNoChange(t *testing.T) {
	a := newApplyEnv(t)
	for _, seed := range []bool{false, true} {
		if seed {
			a.seedProfile("w1", func(p *harness.Profile) { p.ToolGroups = map[string]bool{"git": false} })
		}
		d, err := a.svc.DiagnoseStale(ui(), "w1")
		if err != nil || d.State != StaleNone || len(d.Entries) != 0 {
			t.Fatalf("seed=%v diagnosis = %+v %v", seed, d, err)
		}
		if _, err := a.svc.PrepareRepair(ui(), "w1"); !errors.Is(err, ErrNoChange) {
			t.Fatalf("seed=%v prepare = %v", seed, err)
		}
	}
}

func TestWhatTheRepairCannotFixIsRefusedWithoutAPartialRepair(t *testing.T) {
	cases := map[string]func(a *applyEnv){
		"corrupt body": func(a *applyEnv) {
			a.seedProfile("w1", mixedProfile)
			if _, err := a.db.Exec(`UPDATE workspace_harness_profiles SET body='{bad' WHERE workspace_id='w1'`); err != nil {
				a.t.Fatal(err)
			}
		},
		"unsupported schema": func(a *applyEnv) {
			a.seedProfile("w1", mixedProfile)
			if _, err := a.db.Exec(`UPDATE workspace_harness_profiles SET schema_version=99 WHERE workspace_id='w1'`); err != nil {
				a.t.Fatal(err)
			}
		},
		"invalid known budget next to a stale key": func(a *applyEnv) {
			a.seedProfile("w1", func(p *harness.Profile) {
				mixedProfile(p)
				p.ResultBudget.Tools["git.status"] = harness.BudgetRule{Bytes: intp(10)}
			})
		},
		"budget on a tool that is never cut": func(a *applyEnv) {
			a.seedProfile("w1", func(p *harness.Profile) {
				mixedProfile(p)
				p.ResultBudget.Tools["codegraph.readFile"] = harness.BudgetRule{Bytes: intp(4096)}
			})
		},
		"other invalid setting": func(a *applyEnv) {
			a.seedProfile("w1", func(p *harness.Profile) {
				mixedProfile(p)
				p.DisabledMCP = []string{"devkit"}
			})
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			a := newApplyEnv(t)
			setup(a)
			before := a.dump("w1")
			d, err := a.svc.DiagnoseStale(ui(), "w1")
			if err != nil || d.State != StaleUnrepairable || d.Message == "" || len(d.Entries) != 0 {
				t.Fatalf("diagnosis = %+v %v", d, err)
			}
			if _, err := a.svc.PrepareRepair(ui(), "w1"); !errors.Is(err, ErrRepairUnavailable) {
				t.Fatalf("prepare = %v", err)
			}
			if a.dump("w1") != before || a.svc.Pending() != 0 {
				t.Fatal("nothing may be written or left pending")
			}
		})
	}
}

func (a *applyEnv) dump(ws string) string {
	a.t.Helper()
	var body string
	var rev int64
	if err := a.db.QueryRow(`SELECT revision, body FROM workspace_harness_profiles WHERE workspace_id=?`, ws).Scan(&rev, &body); err != nil {
		return "none"
	}
	return fmt.Sprintf("%d|%s", rev, body)
}

func TestAnUnavailableRegistryCannotTellStaleFromKnown(t *testing.T) {
	a := newApplyEnv(t)
	a.seedProfile("w1", mixedProfile)
	svc := New(Config{Store: a.store, Directory: a.dir, Vendors: a.vendors, Policy: a.policy, Validator: a.validator,
		Registry: func() harness.Registry { return harness.Registry{} }, Confirmer: a.confirm, Boundary: a.boundary})
	d, err := svc.DiagnoseStale(ui(), "w1")
	if err != nil || d.State != StaleUnrepairable {
		t.Fatalf("an empty registry would make everything stale: %+v %v", d, err)
	}
	if _, err := svc.PrepareRepair(ui(), "w1"); !errors.Is(err, ErrRepairUnavailable) {
		t.Fatalf("prepare = %v", err)
	}
}

func TestMoreStaleEntriesThanCanBeShownCannotBePrepared(t *testing.T) {
	a := newApplyEnv(t)
	a.seedProfile("w1", func(p *harness.Profile) {
		p.ToolGroups = map[string]bool{}
		for i := 0; i < MaxStaleEntries+3; i++ {
			p.ToolGroups[fmt.Sprintf("gone%03d", i)] = false
		}
	})
	d, err := a.svc.DiagnoseStale(ui(), "w1")
	if err != nil || d.State != StaleRepairable || !d.Truncated || d.Total != MaxStaleEntries+3 || len(d.Entries) != MaxStaleEntries {
		t.Fatalf("diagnosis = total %d shown %d truncated %v %v", d.Total, len(d.Entries), d.Truncated, err)
	}
	if _, err := a.svc.PrepareRepair(ui(), "w1"); !errors.Is(err, ErrRepairTooLarge) {
		t.Fatalf("prepare = %v", err)
	}
}

func TestAStoredNameIsShownBoundedAndWithoutControlCharacters(t *testing.T) {
	a := newApplyEnv(t)
	hostile := "gone\n\x1b[31m" + strings.Repeat("x", 300)
	a.seedProfile("w1", func(p *harness.Profile) { p.ToolGroups = map[string]bool{hostile: false} })
	d, err := a.svc.DiagnoseStale(ui(), "w1")
	if err != nil {
		t.Fatal(err)
	}
	n := d.Entries[0].Name
	if strings.ContainsAny(n, "\n\x1b") || len([]rune(n)) > maxStaleNameRunes+1 {
		t.Fatalf("name = %q", n)
	}
	p, err := a.svc.PrepareRepair(ui(), "w1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(p.View.Summary, "\x1b") || strings.Contains(p.View.Summary, "gone\n") {
		t.Fatalf("summary carries the raw name: %q", p.View.Summary)
	}
}

// ---- preparation and approval ----------------------------------------------------

func (a *applyEnv) prepareRepair() Prepared {
	a.t.Helper()
	p, err := a.svc.PrepareRepair(ui(), "w1")
	if err != nil {
		a.t.Fatal(err)
	}
	return p
}

func TestTheReviewListsEveryRemovalAndTheConsequences(t *testing.T) {
	a := newApplyEnv(t)
	a.seedProfile("w1", mixedProfile)
	p := a.prepareRepair()
	if p.View.Tier != "widening" || !p.View.NeedsConfirmation || p.View.Target != TargetRepair || len(p.View.Changes) != 4 {
		t.Fatalf("view = %+v", p.View)
	}
	for _, want := range []string{
		`remove the saved "on" setting for tool group "gone"`,
		`remove the saved "off" setting for tool group "pdf"`,
		`remove the saved result limit for tool "gone.tool" (size 4096 bytes, cut headTail)`,
		`remove the saved result limit for tool group "ghost" (size 3000 bytes)`,
		"if that group returns later", "Removed result limits are not restored", "No session is reloaded now",
		"restore the missing module or build",
	} {
		if !strings.Contains(p.View.Summary, want) {
			t.Errorf("summary lacks %q:\n%s", want, p.View.Summary)
		}
	}
	for _, kept := range []string{"git.status", `"git"`, "tasks", "other"} {
		if strings.Contains(p.View.Summary, kept) {
			t.Errorf("the review mentions a setting that is kept: %q", kept)
		}
	}
	if a.confirm.calls != 0 {
		t.Fatal("preparation must not ask")
	}
}

func TestApprovingRemovesExactlyTheStaleKeysAndAdvancesTheRevisionOnce(t *testing.T) {
	a := newApplyEnv(t)
	st := a.seedProfile("w1", mixedProfile)
	sOverride := a.seedOverride("w1", "s1", "pdf")
	before, _ := a.profile("w1")
	p := a.prepareRepair()
	res, err := a.svc.Apply(ui(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Commit != CommitCommitted || res.Live || !res.ReloadRequired || res.Publication.State != PublicationNone {
		t.Fatalf("result = %+v", res)
	}
	after, now := a.profile("w1")
	if now.Revision != st.Revision+1 || res.Profile != now {
		t.Fatalf("revision %d -> %d, result %+v", st.Revision, now.Revision, res.Profile)
	}
	want := RepairedProfile(before, InspectStale(before, registry()))
	if mustJSON(t, after) != mustJSON(t, withRevision(want, now.Revision)) {
		t.Fatalf("stored = %s\nwant   %s", mustJSON(t, after), mustJSON(t, withRevision(want, now.Revision)))
	}
	if _, o := a.override("w1", "s1"); o != sOverride {
		t.Fatalf("a session override must not change: %+v", o)
	}
	// It is now an ordinary, resolvable profile: nothing is stale and a normal edit validates.
	if d, _ := a.svc.DiagnoseStale(ui(), "w1"); d.State != StaleNone {
		t.Fatalf("after the repair: %+v", d)
	}
	if _, err := a.svc.Prepare(ui(), Request{WorkspaceID: "w1", Target: TargetWorkspace,
		Patch: Patch{Workspace: &ProfilePatch{Groups: map[string]*bool{"tasks": boolp(false)}}}}); err != nil {
		t.Fatalf("an ordinary edit after the repair: %v", err)
	}
	if a.confirm.calls != 1 || a.confirm.asked[0].Target != TargetRepair || a.confirm.asked[0].Tier != "widening" {
		t.Fatalf("confirmations = %+v", a.confirm.asked)
	}
	for _, ev := range a.events {
		if ev.Target == TargetRepair && (ev.Outcome != "applied" || ev.Tier != "widening" || ev.WorkspaceID != "w1" || ev.Profile != now.Revision || ev.SessionID != "") {
			t.Fatalf("audit = %+v", ev)
		}
		if strings.Contains(fmt.Sprintf("%+v", ev), "gone") {
			t.Fatalf("the audit carries a stale name: %+v", ev)
		}
	}
}

func TestDenyingCancellingOrReplayingChangesNothing(t *testing.T) {
	a := newApplyEnv(t)
	st := a.seedProfile("w1", mixedProfile)
	same := func(step string) {
		t.Helper()
		if _, now := a.profile("w1"); now != st {
			t.Fatalf("%s changed the profile: %+v", step, now)
		}
	}
	p := a.prepareRepair()
	a.confirm.answer = ErrNotConfirmed
	if _, err := a.svc.Apply(ui(), p.ID); !errors.Is(err, ErrNotConfirmed) {
		t.Fatalf("denied = %v", err)
	}
	same("a denial")
	a.confirm.answer = nil
	if _, err := a.svc.Apply(ui(), p.ID); !errors.Is(err, ErrUnknownProposal) {
		t.Fatalf("a spent id = %v", err)
	}
	p = a.prepareRepair()
	if err := a.svc.Cancel(ui(), p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.svc.Apply(ui(), p.ID); !errors.Is(err, ErrUnknownProposal) {
		t.Fatalf("a cancelled id = %v", err)
	}
	same("a cancel")
	p = a.prepareRepair()
	if _, err := a.svc.Apply(ui(), p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.svc.Apply(ui(), p.ID); !errors.Is(err, ErrUnknownProposal) {
		t.Fatalf("a replay = %v", err)
	}
}

func TestAnotherCallerCannotApplyOrCancelAPreparedRepair(t *testing.T) {
	a := newApplyEnv(t)
	st := a.seedProfile("w1", mixedProfile)
	p := a.prepareRepair()
	for name, ctx := range map[string]context.Context{
		"agent": agent("s1"), "terminal": identity.WithCaller(context.Background(), identity.UserTerminal()), "unknown": context.Background(),
	} {
		if _, err := a.svc.Apply(ctx, p.ID); !errors.Is(err, ErrUnknownProposal) {
			t.Errorf("%s apply = %v", name, err)
		}
		if err := a.svc.Cancel(ctx, p.ID); !errors.Is(err, ErrUnknownProposal) {
			t.Errorf("%s cancel = %v", name, err)
		}
	}
	if _, now := a.profile("w1"); now != st || a.svc.Pending() != 1 {
		t.Fatal("a refused caller must not spend or change anything")
	}
}

func TestNobodyToAskRefusesAndAutoNarrowingNeverSkipsTheQuestion(t *testing.T) {
	a := newApplyEnv(t)
	st := a.seedProfile("w1", mixedProfile)
	p := a.prepareRepair()
	a.confirm.available = false
	if _, err := a.svc.Apply(ui(), p.ID); !errors.Is(err, ErrConfirmUnavailable) {
		t.Fatalf("headless = %v", err)
	}
	if _, now := a.profile("w1"); now != st || a.confirm.calls != 0 {
		t.Fatal("headless must neither ask nor write")
	}
	a.confirm.available = true
	a.policy.mu.Lock()
	a.policy.p = Policy{Revision: "p2", AutoNarrowing: true}
	a.policy.mu.Unlock()
	p = a.prepareRepair()
	if !p.View.NeedsConfirmation {
		t.Fatal("a repair always needs confirmation")
	}
	if _, err := a.svc.Apply(ui(), p.ID); err != nil {
		t.Fatal(err)
	}
	if a.confirm.calls != 1 {
		t.Fatalf("with auto-narrowing on the repair asked %d times, want once", a.confirm.calls)
	}
}

// ---- staleness while the person decides ---------------------------------------------

func TestAChangeWhileTheQuestionIsOpenMakesTheRepairStaleAndOverwritesNothing(t *testing.T) {
	type change func(a *applyEnv)
	cases := map[string]struct {
		do       change
		wantCode string
	}{
		"workspace edit": {func(a *applyEnv) {
			a.seedProfile("w1", func(p *harness.Profile) {
				mixedProfile(p)
				p.ToolGroups["tasks"] = false // a new, known setting
			})
		}, harness.CodeProfileConflict},
		"vendor revision": {func(a *applyEnv) { a.vendors.set("claude", func(s *VendorState) { s.Vendor.Revision++ }) }, harness.CodeProposalStale},
		"policy revision": {func(a *applyEnv) {
			a.policy.mu.Lock()
			a.policy.p = Policy{Revision: "p2", AutoNarrowing: true}
			a.policy.mu.Unlock()
		}, harness.CodeProposalStale},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			a := newApplyEnv(t)
			a.seedProfile("w1", mixedProfile)
			p := a.prepareRepair()
			a.confirm.during = func() { tc.do(a) }
			_, err := a.svc.Apply(ui(), p.ID)
			if code(t, err) != tc.wantCode {
				t.Fatalf("apply = %v", err)
			}
			got, _ := a.profile("w1")
			if !contains(got, "gone") {
				t.Fatalf("a refused repair removed something: %s", mustJSON(t, got))
			}
			if name == "workspace edit" && !contains(got, "tasks") {
				t.Fatalf("the newer setting was overwritten: %s", mustJSON(t, got))
			}
		})
	}
}

func contains(p *harness.Profile, group string) bool {
	_, ok := p.ToolGroups[group]
	return ok
}

// The registry the service reads can change between preparing and applying (a
// module wired or unwired); the repair was judged against the old one.
func TestARegistryChangeWhileTheQuestionIsOpenMakesTheRepairStale(t *testing.T) {
	a := newApplyEnv(t)
	a.seedProfile("w1", mixedProfile)
	var mu sync.Mutex
	reg := registry()
	a.svc = New(Config{Store: a.store, Directory: a.dir, Vendors: a.vendors, Policy: a.policy, Validator: a.validator,
		Registry: func() harness.Registry { mu.Lock(); defer mu.Unlock(); return reg }, Confirmer: a.confirm, Boundary: a.boundary})
	p := a.prepareRepair()
	a.confirm.during = func() {
		mu.Lock()
		defer mu.Unlock()
		reg.Tools = append(reg.Tools, harness.Tool{Name: "gone.status", DefinitionBytes: 100}) // the missing group came back
	}
	_, err := a.svc.Apply(ui(), p.ID)
	if code(t, err) != harness.CodeProposalStale {
		t.Fatalf("apply = %v", err)
	}
	if got, _ := a.profile("w1"); !contains(got, "gone") {
		t.Fatal("the group came back, so its saved setting must not be removed")
	}
}

// ---- the store ---------------------------------------------------------------------

// flakyStore wraps the real store: Apply can report an unknown commit, with or
// without having written, and reads can fail afterwards.
type flakyStore struct {
	Store
	unknown   bool
	writeIt   bool
	failReads atomic.Bool
	applies   atomic.Int32
}

func (f *flakyStore) Apply(ctx context.Context, w harness.StoreWrite) (harness.Applied, error) {
	f.applies.Add(1)
	if !f.unknown {
		return f.Store.Apply(ctx, w)
	}
	if f.writeIt {
		if _, err := f.Store.Apply(ctx, w); err != nil {
			return harness.Applied{}, err
		}
	}
	f.failReads.Store(!f.writeIt)
	return harness.Applied{}, fmt.Errorf("%w: connection dropped", harness.ErrStoreCommitUnknown)
}

func (f *flakyStore) GetProfile(ctx context.Context, ws string) (*harness.Profile, harness.Stamp, error) {
	if f.failReads.Load() {
		return nil, harness.Stamp{}, errors.New("read failed")
	}
	return f.Store.GetProfile(ctx, ws)
}

func (a *applyEnv) withStore(s Store) {
	a.svc = New(Config{Store: s, Directory: a.dir, Vendors: a.vendors, Policy: a.policy, Validator: a.validator,
		Registry: registry, Canonical: a.canonical.resolve, Confirmer: a.confirm, Boundary: a.boundary})
}

func TestAnAmbiguousCommitIsReadBackAndNeverRetried(t *testing.T) {
	t.Run("it was written", func(t *testing.T) {
		a := newApplyEnv(t)
		st := a.seedProfile("w1", mixedProfile)
		fs := &flakyStore{Store: a.store, unknown: true, writeIt: true}
		a.withStore(fs)
		p := a.prepareRepair()
		res, err := a.svc.Apply(ui(), p.ID)
		if err != nil || res.Commit != CommitCommitted || res.Profile.Revision != st.Revision+1 {
			t.Fatalf("read back as written: %+v %v", res, err)
		}
		if fs.applies.Load() != 1 {
			t.Fatalf("the write was attempted %d times", fs.applies.Load())
		}
	})
	t.Run("it was not written", func(t *testing.T) {
		a := newApplyEnv(t)
		st := a.seedProfile("w1", mixedProfile)
		// The commit fails as unknown but nothing reached the store, and reads work:
		// the read-back proves the rollback.
		fs := &absentOnReadBack{flakyStore: &flakyStore{Store: a.store}}
		a.withStore(fs)
		p := a.prepareRepair()
		if _, err := a.svc.Apply(ui(), p.ID); code(t, err) != harness.CodeProposalStale {
			t.Fatalf("a proven rollback is a stale proposal, got %v", err)
		}
		if _, now := a.profile("w1"); now != st || fs.applies.Load() != 1 {
			t.Fatal("nothing must be written, and the write must not be retried")
		}
	})
	t.Run("it cannot be known", func(t *testing.T) {
		a := newApplyEnv(t)
		a.seedProfile("w1", mixedProfile)
		fs := &flakyStore{Store: a.store, unknown: true, writeIt: false}
		a.withStore(fs)
		p := a.prepareRepair()
		res, err := a.svc.Apply(ui(), p.ID)
		var pe *PublicationError
		if !errors.As(err, &pe) || res.Commit != CommitUnknown {
			t.Fatalf("want an unknown commit, got %+v %v", res, err)
		}
		out, oerr := OutcomeOf(TargetRepair, res, err)
		if oerr != nil || out.Status != OutcomeUnknown || out.Live || strings.Contains(strings.ToLower(out.Recovery+out.Message), "reconcile") {
			t.Fatalf("outcome = %+v %v", out, oerr)
		}
		if fs.applies.Load() != 1 {
			t.Fatalf("the write was attempted %d times", fs.applies.Load())
		}
	})
}

// absentOnReadBack makes the first Apply fail as unknown (writing nothing) and lets
// reads through, so the read-back proves the rollback.
type absentOnReadBack struct{ *flakyStore }

func (a *absentOnReadBack) Apply(ctx context.Context, w harness.StoreWrite) (harness.Applied, error) {
	a.applies.Add(1)
	return harness.Applied{}, fmt.Errorf("%w: connection dropped", harness.ErrStoreCommitUnknown)
}

func TestARepairedOutcomeSaysSavedNotLive(t *testing.T) {
	a := newApplyEnv(t)
	a.seedProfile("w1", mixedProfile)
	p := a.prepareRepair()
	res, err := a.svc.Apply(ui(), p.ID)
	out, oerr := OutcomeOf(TargetRepair, res, err)
	if err != nil || oerr != nil || out.Status != OutcomeApplied || out.Live || !out.ReloadRequired || out.Target != string(TargetRepair) ||
		!strings.Contains(out.Recovery, "keep what they opened with") {
		t.Fatalf("outcome = %+v", out)
	}
}
