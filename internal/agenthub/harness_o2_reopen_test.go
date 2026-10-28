package agenthub_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/preferences"
)

// O-2 (R1a section 2): a session with no applied reference, reopened while the
// workspace profile W owns an executable, must not adopt it. Legacy launches
// never merged W, so the person opens a new session. The rule reads only: it
// writes no row and never stamps a reference. Create is a different act and is
// never refused for it.

// legacySession is a session that has never had an applied reference: it was
// created while no harness was wired (the row is absent), and the harness is wired
// afterwards on the same service, so its runtime is still the running one.
func (e *launchEnv) legacySession() agenthub.AgentSession {
	e.t.Helper()
	e.svc.Harness = nil
	s := e.mustCreate()
	e.svc.Harness = e.harness
	if _, _, found := e.durable(s.ID); found {
		e.t.Fatal("setup: a session created with no harness must have no reference")
	}
	if _, _, ok := e.svc.HarnessApplied(s.ID); ok {
		e.t.Fatal("setup: no in-memory reference either")
	}
	return s
}

// appliedRows is every stored reference, in a stable order, as raw text.
func (e *launchEnv) appliedRows() []string {
	e.t.Helper()
	rows, err := e.db.Query(`SELECT session_id||'|'||schema_version||'|'||ref||'|'||enforced||'|'||applied_at FROM agent_session_harness_applied ORDER BY session_id`)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func vendorOnlyServer(t *testing.T) func(string, string, string) (harness.Snapshot, error) {
	return func(s, ws, v string) (harness.Snapshot, error) {
		return snapshotFor(t, s, ws, v, func(in *harness.Inputs) {
			in.Workspace = &harness.Profile{Schema: 1, Revision: 1}
			in.Vendor = harness.Vendor{Revision: 9, Servers: []harness.Server{{Name: "vendor-db", Command: "c", Enabled: true}}}
		}), nil
	}
}

func budgetOnlyW(t *testing.T) func(string, string, string) (harness.Snapshot, error) {
	return func(s, ws, v string) (harness.Snapshot, error) {
		return snapshotFor(t, s, ws, v, func(in *harness.Inputs) {
			n := 2048
			in.Workspace = &harness.Profile{Schema: 1, Revision: 4,
				ToolGroups:   map[string]bool{"git": false},
				ResultBudget: &harness.BudgetProfile{Tools: map[string]harness.BudgetRule{"git.status": {Bytes: &n}}}}
		}), nil
	}
}

// Every reopen of a legacy session is refused when W owns a server, and the
// refusal leaves the store, the session and the running runtime as they were.
func TestO2ALegacySessionIsRefusedOnEveryColdReopen(t *testing.T) {
	ctx := context.Background()
	for name, reopen := range map[string]func(*agenthub.Service, string) error{
		"resume invoke": func(svc *agenthub.Service, id string) error {
			_, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: id, Prompt: "continue"})
			return err
		},
		"resume with reload": func(svc *agenthub.Service, id string) error { return svc.ResumeSession(ctx, id, true) },
	} {
		t.Run(name, func(t *testing.T) {
			e := newLaunchEnv(t, policy)
			s := e.legacySession()
			e.harness.set(workspaceServer(t, true))
			before := e.appliedRows()
			opens := len(e.adapter.specs)

			svc := e.restart() // a new process: no runtime, no in-memory record
			if err := reopen(svc, s.ID); !errors.Is(err, agenthub.ErrHarnessNewSessionRequired) {
				t.Fatalf("a legacy session adopted a workspace-owned server: %v", err)
			}
			if !sameStrings(before, e.appliedRows()) {
				t.Fatalf("the refusal wrote or changed a reference row: %v -> %v", before, e.appliedRows())
			}
			if len(e.adapter.specs) != opens {
				t.Fatal("a refused reopen still launched the vendor")
			}
			if _, _, ok := svc.HarnessApplied(s.ID); ok {
				t.Fatal("a refused reopen recorded a reference in memory")
			}
		})
	}
}

func TestO2BReloadOfALegacyRunningSessionIsRefusedAndLeavesItAlone(t *testing.T) {
	e := newLaunchEnv(t, policy)
	s := e.legacySession()
	e.harness.set(workspaceServer(t, true))
	rt, ok := e.svc.RuntimeForTest(s.ID)
	if !ok {
		t.Fatal("setup: the session must be running")
	}
	before, closes, opens := e.appliedRows(), e.ev.count("endpoint.close"), e.ev.count("endpoint.open")

	if err := e.reload(s.ID); !errors.Is(err, agenthub.ErrHarnessNewSessionRequired) {
		t.Fatalf("reload adopted a workspace-owned server on a legacy session: %v", err)
	}
	if now, ok := e.svc.RuntimeForTest(s.ID); !ok || now != rt {
		t.Fatal("a refused reload replaced or closed the running runtime")
	}
	if e.ev.count("endpoint.close") != closes || e.ev.count("endpoint.open") != opens {
		t.Fatalf("a refused reload touched the endpoint: %v", e.ev.all())
	}
	if !sameStrings(before, e.appliedRows()) {
		t.Fatal("a refused reload wrote a reference row")
	}
	// The running session still answers on the runtime it has.
	if _, err := e.svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: s.ID, Prompt: "still here"}); err != nil {
		t.Fatalf("the running session did not survive a refused reload: %v", err)
	}
}

// newLegacyRewindEnv is newHarnessRewindEnv for a session created while no
// harness was wired. The harness (and W's server) are wired after its two turns.
func newLegacyRewindEnv(t *testing.T) (*launchEnv, agenthub.AgentSession, string) {
	t.Helper()
	e := newLaunchEnv(t, policy)
	reg := agenthub.NewRegistry()
	reg.Register(replyingAdapter{MockAdapter: agenthub.NewMockAdapter()})
	e.svc = agenthub.NewService(reg, agenthub.NewSQLStore(e.db), func(string, json.RawMessage) {})
	e.svc.MatchingWorkspaces = func(string) ([]string, error) { return []string{"w1"}, nil }
	e.svc.MCPEndpoints = policy(e.ev)
	session := e.legacySession()
	for _, prompt := range []string{"first", "second"} {
		if _, err := e.svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: session.ID, Prompt: prompt}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := agenthub.NewSQLStore(e.db).ListTurns(context.Background(), session.ID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("turns = %d: %v", len(rows), err)
	}
	return e, session, rows[1].PromptMessageID
}

func TestO2CRewindPreflightRefusesALegacySessionAndLeavesItAlone(t *testing.T) {
	ctx := context.Background()
	e, session, promptID := newLegacyRewindEnv(t)
	store := agenthub.NewSQLStore(e.db)
	preview, err := e.svc.PlanRewind(ctx, agenthub.RewindRequest{SessionID: session.ID, PromptMessageID: promptID, Mode: agenthub.RewindConversation})
	if err != nil {
		t.Fatal(err)
	}
	e.harness.set(workspaceServer(t, true))
	before, closes, opens := e.appliedRows(), e.ev.count("endpoint.close"), e.ev.count("endpoint.openWithPolicy")

	if _, err = e.svc.ApplyRewind(ctx, preview.Token, nil); !errors.Is(err, agenthub.ErrHarnessNewSessionRequired) {
		t.Fatalf("rewind = %v, want ErrHarnessNewSessionRequired", err)
	}
	if e.ev.count("endpoint.close") != closes || e.ev.count("endpoint.openWithPolicy") != opens {
		t.Fatal("a refused rewind touched the running session's endpoint")
	}
	if !sameStrings(before, e.appliedRows()) {
		t.Fatal("a refused rewind wrote a reference row")
	}
	if texts := transcriptTexts(t, store, session.ID); len(texts) != 2 {
		t.Fatalf("a refused rewind cut the transcript: %v", texts)
	}
	if _, err := e.svc.Invoke(ctx, agenthub.InvokeInput{SessionID: session.ID, Prompt: "still here"}); err != nil {
		t.Fatalf("the running session did not survive a refused rewind: %v", err)
	}
}

// The fork's own launch repeats the gate (the preflight passed, then W moved).
// A fork is a reopen too: it must be refused there, not adopt the server.
func TestO2DRewindForkLaunchRefusesALegacySessionWithoutAdopting(t *testing.T) {
	ctx := context.Background()
	e, session, promptID := newLegacyRewindEnv(t)
	preview, err := e.svc.PlanRewind(ctx, agenthub.RewindRequest{SessionID: session.ID, PromptMessageID: promptID, Mode: agenthub.RewindConversation})
	if err != nil {
		t.Fatal(err)
	}
	// The preflight is the first snapshot the apply reads and sees no executable;
	// every later one (the fork's launch) sees W's server.
	var n atomic.Int32
	e.harness.set(func(s, ws, v string) (harness.Snapshot, error) {
		if n.Add(1) == 1 {
			return workspaceServer(t, false)(s, ws, v)
		}
		return workspaceServer(t, true)(s, ws, v)
	})
	before := e.appliedRows()
	_, err = e.svc.ApplyRewind(ctx, preview.Token, nil)
	if !errors.Is(err, agenthub.ErrHarnessNewSessionRequired) {
		t.Fatalf("a rewind fork adopted a workspace-owned server on a legacy session: %v", err)
	}
	if n.Load() < 2 {
		t.Fatal("test did not reach the fork's launch gate")
	}
	if rows := e.appliedRows(); len(rows) != len(before) {
		t.Fatalf("the refused fork left a row: %v", rows)
	}
}

// newAccountMoveEnv adds an account book and a second account to a launch
// environment, with the built-in account over its threshold: the next turn wants
// to move the session to "work".
func newAccountMoveEnv(t *testing.T) *launchEnv {
	t.Helper()
	e := newLaunchEnv(t, policy)
	book := agenthub.NewSQLAccountStore(e.db)
	if _, err := book.List(t.Context(), agenthub.AgentTypeMock); err != nil {
		t.Fatal(err)
	}
	if err := book.Put(t.Context(), agenthub.AccountRecord{ID: "work", Vendor: agenthub.AgentTypeMock, HomeDir: t.TempDir(), SignedIn: true}); err != nil {
		t.Fatal(err)
	}
	w := agenthub.LimitWindow{Label: "Weekly", UsedPercent: 95, ResetsAt: time.Now().Add(24 * time.Hour).Unix()}
	if err := book.SaveUsage(t.Context(), builtinMock, []agenthub.LimitWindow{w}, "observed", time.Now()); err != nil {
		t.Fatal(err)
	}
	e.svc.Accounts = book
	e.svc.AccountPolicy = func() (preferences.AgentAccountPolicy, error) {
		return preferences.AgentAccountPolicy{Vendors: map[string]preferences.AccountVendorPolicy{
			string(agenthub.AgentTypeMock): {Enabled: true, ThresholdPercent: 90, UsageMaxAgeSeconds: 300, Order: []string{builtinMock, "work"}},
		}}, nil
	}
	return e
}

func TestO2EAccountMovePreflightKeepsALegacySessionWhereItIs(t *testing.T) {
	ctx := context.Background()
	e := newAccountMoveEnv(t)
	s := e.legacySession()
	e.harness.set(workspaceServer(t, true))
	rt, ok := e.svc.RuntimeForTest(s.ID)
	if !ok {
		t.Fatal("setup: the session must be running")
	}
	before, closes := e.appliedRows(), e.ev.count("endpoint.close")

	if _, err := e.svc.Invoke(ctx, agenthub.InvokeInput{SessionID: s.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.GetStatus(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccountID == "work" {
		t.Fatal("the session moved to another account and reopened through a gate that should have refused it")
	}
	if now, ok := e.svc.RuntimeForTest(s.ID); !ok || now != rt || e.ev.count("endpoint.close") != closes {
		t.Fatal("a move the gate would refuse closed the running session")
	}
	if !sameStrings(before, e.appliedRows()) {
		t.Fatal("the refused move wrote a reference row")
	}
}

// The reopen after a move goes through the same launch gate as any resume.
func TestO2FAccountMoveReopenOfALegacySessionIsRefused(t *testing.T) {
	ctx := context.Background()
	e := newAccountMoveEnv(t)
	s := e.legacySession()
	e.harness.set(workspaceServer(t, true))
	before := e.appliedRows()
	if err := e.svc.MoveSessionForTest(ctx, s.ID, "work"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Invoke(ctx, agenthub.InvokeInput{SessionID: s.ID, Prompt: "after the move"}); !errors.Is(err, agenthub.ErrHarnessNewSessionRequired) {
		t.Fatalf("the reopen after an account move adopted a workspace-owned server: %v", err)
	}
	if !sameStrings(before, e.appliedRows()) {
		t.Fatal("the refused reopen wrote a reference row")
	}
}

// A W with only groups or budgets, no owned executable, or a vendor-only change
// is not what the rule is about: the session reopens and now has its reference.
func TestO2GLegacySessionReopensWhenWOwnsNoExecutable(t *testing.T) {
	ctx := context.Background()
	for name, w := range map[string]func(*testing.T) func(string, string, string) (harness.Snapshot, error){
		"groups only": func(t *testing.T) func(string, string, string) (harness.Snapshot, error) {
			return snapshotFn(t, gitOffW(3))
		},
		"budgets and groups": budgetOnlyW,
		"no owned executable": func(t *testing.T) func(string, string, string) (harness.Snapshot, error) {
			return workspaceServer(t, false)
		},
		"vendor-only server":   vendorOnlyServer,
		"no workspace profile": func(t *testing.T) func(string, string, string) (harness.Snapshot, error) { return snapshotFn(t, nil) },
	} {
		for _, path := range []string{"resume", "reload"} {
			t.Run(name+"/"+path, func(t *testing.T) {
				e := newLaunchEnv(t, policy)
				s := e.legacySession()
				e.harness.set(w(t))
				var err error
				if path == "resume" {
					_, err = e.restart().Invoke(ctx, agenthub.InvokeInput{SessionID: s.ID, Prompt: "continue"})
				} else {
					err = e.reload(s.ID)
				}
				if err != nil {
					t.Fatalf("a legacy session with no workspace executable was refused: %v", err)
				}
				ref, _, found := e.durable(s.ID)
				if !found || len(ref.WorkspaceOwned) != 0 {
					t.Fatalf("the reopen must write its reference: found=%v %+v", found, ref)
				}
			})
		}
	}
}

func snapshotFn(t *testing.T, mutate func(*harness.Inputs)) func(string, string, string) (harness.Snapshot, error) {
	return func(s, ws, v string) (harness.Snapshot, error) { return snapshotFor(t, s, ws, v, mutate), nil }
}

// Create is a new session, not a reopen: a W-owned server is part of what it is
// opened with, and it is recorded as that session's reference.
func TestO2HCreateWithAWorkspaceOwnedServerStillSucceeds(t *testing.T) {
	e := newLaunchEnv(t, policy)
	e.harness.set(workspaceServer(t, true))
	s := e.mustCreate()
	ref, _, found := e.durable(s.ID)
	if !found || len(ref.WorkspaceOwned) != 1 {
		t.Fatalf("a created session records its reference: found=%v owned=%d", found, len(ref.WorkspaceOwned))
	}
	// And the reopen of that session, which has a reference, is judged as before.
	if _, err := e.restart().Invoke(context.Background(), agenthub.InvokeInput{SessionID: s.ID, Prompt: "continue"}); err != nil {
		t.Fatalf("a session with its reference reopens unchanged: %v", err)
	}
}

// An unreadable reference is still unavailable, not "absent".
func TestO2IUnreadableReferenceIsStillUnavailable(t *testing.T) {
	ctx := context.Background()
	e := newLaunchEnv(t, policy)
	e.harness.set(workspaceServer(t, false))
	s := e.mustCreate()
	if _, err := e.db.Exec(`UPDATE agent_session_harness_applied SET ref='{not json'`); err != nil {
		t.Fatal(err)
	}
	e.harness.set(workspaceServer(t, true))
	_, err := e.restart().Invoke(ctx, agenthub.InvokeInput{SessionID: s.ID, Prompt: "continue"})
	if !errors.Is(err, agenthub.ErrHarnessUnavailable) || errors.Is(err, agenthub.ErrHarnessNewSessionRequired) {
		t.Fatalf("an unreadable reference must fail closed as unavailable: %v", err)
	}
}

// R1a probe 1, inverted: the first reopen after activation must not adopt W's server.
func TestO2JProbeLegacySessionDoesNotAdoptAWorkspaceExecutableOnFirstReopen(t *testing.T) {
	e := newLaunchEnv(t, policy)
	e.svc.Harness = nil
	s := e.mustCreate()
	e.harness.set(workspaceServer(t, true))
	svc := e.restart()
	specs := len(e.adapter.specs)
	_, err := svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: s.ID, Prompt: "continue"})
	if !errors.Is(err, agenthub.ErrHarnessNewSessionRequired) {
		t.Fatalf("first reopen after activation: %v", err)
	}
	if _, _, found := e.durable(s.ID); found {
		t.Fatal("a baseline was stamped from the current workspace profile")
	}
	if len(e.adapter.specs) != specs {
		t.Fatal("the vendor was launched on the refused reopen")
	}
	// The session stays readable and is not closed or altered.
	if got, err := svc.GetStatus(context.Background(), s.ID); err != nil || got.ID != s.ID {
		t.Fatalf("session after the refusal: %+v %v", got, err)
	}
}

// R1a probe 2, inverted: a rolled-back (inert) build deletes the reference; the
// guard still holds on re-activation. What an inert build deletes is the owner's
// open question and is not asserted here.
func TestO2KProbeRollbackBuildDoesNotLeaveAnOpeningForAWorkspaceExecutable(t *testing.T) {
	ctx := context.Background()
	e := newLaunchEnv(t, policy)
	e.harness.set(workspaceServer(t, false))
	s := e.mustCreate()
	e.harness.set(workspaceServer(t, true))
	if _, err := e.restart().Invoke(ctx, agenthub.InvokeInput{SessionID: s.ID, Prompt: "control"}); !errors.Is(err, agenthub.ErrHarnessNewSessionRequired) {
		t.Fatalf("control must be refused: %v", err)
	}
	e.harness.set(workspaceServer(t, false))
	rb := e.restart()
	rb.Harness = nil
	if _, err := rb.Invoke(ctx, agenthub.InvokeInput{SessionID: s.ID, Prompt: "rolled back"}); err != nil {
		t.Fatalf("rolled-back reopen: %v", err)
	}
	_, _, found := e.durable(s.ID)
	t.Logf("reference after an inert reopen: found=%v (unchanged behaviour; the owner has not decided)", found)

	e.harness.set(workspaceServer(t, true))
	specs := len(e.adapter.specs)
	if _, err := e.restart().Invoke(ctx, agenthub.InvokeInput{SessionID: s.ID, Prompt: "reactivated"}); !errors.Is(err, agenthub.ErrHarnessNewSessionRequired) {
		t.Fatalf("after rollback and re-activation the session adopted a workspace executable: %v (reference found after rollback: %v)", err, found)
	}
	if len(e.adapter.specs) != specs {
		t.Fatal("the vendor was launched on the refused reopen")
	}
}

// E: the window's status says so before the launch refuses, and agrees with it.
func TestO2LStatusSaysNewSessionRequiredBeforeTheLaunchRefuses(t *testing.T) {
	ctx := context.Background()
	e := newLaunchEnv(t, policy)
	s := e.legacySession()

	// No executable owned by W: unchanged, "not applied".
	e.harness.set(workspaceServer(t, false))
	if st := e.status(s.ID); st.State != agenthub.HarnessNotApplied || st.Applied != nil || st.Desired == nil {
		t.Fatalf("no workspace executable: %+v", st)
	}
	// A vendor-only server is not W's.
	e.harness.set(vendorOnlyServer(t))
	if st := e.status(s.ID); st.State != agenthub.HarnessNotApplied {
		t.Fatalf("vendor-only: %+v", st)
	}
	// W owns a server: a new session is needed, and nothing is invented for applied.
	e.harness.set(workspaceServer(t, true))
	st := e.status(s.ID)
	if st.State != agenthub.HarnessNewSessionRequired || st.Applied != nil || st.Desired == nil || len(st.Desired.WorkspaceOwned) != 1 {
		t.Fatalf("workspace-owned server with no reference: %+v", st)
	}
	if err := e.reload(s.ID); !errors.Is(err, agenthub.ErrHarnessNewSessionRequired) {
		t.Fatalf("the status must agree with what the reopen does: %v", err)
	}
	// An unreadable reference is unavailable, not new-session-required.
	e.harness.set(workspaceServer(t, false))
	created := e.mustCreate()
	if _, err := e.db.Exec(`UPDATE agent_session_harness_applied SET ref='{not json' WHERE session_id=?`, created.ID); err != nil {
		t.Fatal(err)
	}
	e.harness.set(workspaceServer(t, true))
	if st := e.status(created.ID); st.State != agenthub.HarnessUnavailable {
		t.Fatalf("unreadable: %+v", st)
	}
	_ = ctx
}

// The rule is stateless: nothing it does can be seen in the store. A refused
// reopen leaves every row, the session and the transcript byte for byte as they
// were, however often it is asked.
func TestO2MRefusalWritesNothing(t *testing.T) {
	ctx := context.Background()
	e, session, _ := newLegacyRewindEnv(t)
	other := e.mustCreate() // a session with a reference; it must stay as it is too
	store := agenthub.NewSQLStore(e.db)
	e.harness.set(workspaceServer(t, true))

	snapshot := func() string {
		sess, err := store.GetSession(ctx, session.ID)
		if err != nil {
			t.Fatal(err)
		}
		turns, err := store.ListTurns(ctx, session.ID)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(struct {
			Session agenthub.AgentSession
			Turns   int
			Texts   []string
			Rows    []string
		}{sess, len(turns), transcriptTexts(t, store, session.ID), e.appliedRows()})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	before := snapshot()
	if len(e.appliedRows()) != 1 {
		t.Fatalf("setup: exactly the other session (%s) has a reference: %v", other.ID, e.appliedRows())
	}
	svc := e.restart()
	for i := 0; i < 3; i++ {
		if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: session.ID, Prompt: "again"}); !errors.Is(err, agenthub.ErrHarnessNewSessionRequired) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if err := svc.ResumeSession(ctx, session.ID, true); !errors.Is(err, agenthub.ErrHarnessNewSessionRequired) {
		t.Fatalf("reload: %v", err)
	}
	if after := snapshot(); after != before {
		t.Fatalf("a refused reopen changed the store:\n before %s\n after  %s", before, after)
	}
}
