package agenthub_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
)

type fakeLifecycle struct {
	mu     sync.Mutex
	closed []string
	err    error
}

func (f *fakeLifecycle) SessionClosed(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = append(f.closed, id)
	return f.err
}

func (f *fakeLifecycle) setErr(err error) { f.mu.Lock(); f.err = err; f.mu.Unlock() }
func (f *fakeLifecycle) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.closed...)
}

// restart builds a second service on the same database, as a new process would.
func (e *launchEnv) restart() *agenthub.Service {
	reg := agenthub.NewRegistry()
	reg.Register(e.adapter)
	svc := agenthub.NewService(reg, agenthub.NewSQLStore(e.db), func(string, json.RawMessage) {})
	svc.MatchingWorkspaces = func(string) ([]string, error) { return []string{"w1"}, nil }
	svc.MCPEndpoints = e.svc.MCPEndpoints
	svc.Harness = e.harness
	return svc
}

func (e *launchEnv) durable(id string) (harness.SnapshotRef, bool, bool) {
	e.t.Helper()
	ref, enforced, found, err := agenthub.NewSQLStore(e.db).GetHarnessApplied(context.Background(), id)
	if err != nil {
		e.t.Fatal(err)
	}
	return ref, enforced, found
}

func TestTheAppliedReferenceIsDurableAndFollowsEveryReopen(t *testing.T) {
	e := newLaunchEnv(t, plain)
	s := e.mustCreate()
	ref, _, found := e.durable(s.ID)
	mem, _, _ := e.svc.HarnessApplied(s.ID)
	if !found || !ref.Equal(mem) || !ref.Default {
		t.Fatalf("durable %+v memory %+v found=%v", ref, mem, found)
	}
	e.harness.set(func(sid, ws, v string) (harness.Snapshot, error) {
		return snapshotFor(t, sid, ws, v, func(in *harness.Inputs) { in.Workspace = &harness.Profile{Schema: 1, Revision: 7} }), nil
	})
	if err := e.reload(s.ID); err != nil {
		t.Fatal(err)
	}
	if ref, _, _ := e.durable(s.ID); ref.Revisions.Workspace == nil || *ref.Revisions.Workspace != 7 {
		t.Fatalf("a reload must replace the durable reference: %+v", ref)
	}
	// The reference holds no definition, path or command.
	raw, _ := json.Marshal(ref)
	for _, forbidden := range []string{"command", "args", "path", "env", "token"} {
		if containsFold(string(raw), forbidden) {
			t.Errorf("the durable reference mentions %q: %s", forbidden, raw)
		}
	}
}

func containsFold(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if equalFold(s[i:i+len(sub)], sub) {
			return true
		}
	}
	return false
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}

func TestARestartStillRefusesToAdoptAChangedExecutableSet(t *testing.T) {
	e := newLaunchEnv(t, plain)
	withServer := func(rev int64, cmd string) func(string, string, string) (harness.Snapshot, error) {
		return func(s, ws, v string) (harness.Snapshot, error) {
			return snapshotFor(t, s, ws, v, func(in *harness.Inputs) {
				in.Workspace = &harness.Profile{Schema: 1, Revision: rev, Servers: []harness.Server{{Name: "db", Command: cmd, Enabled: true}}}
			}), nil
		}
	}
	e.harness.set(withServer(1, "a"))
	s := e.mustCreate()

	restarted := e.restart()
	if _, _, ok := restarted.HarnessApplied(s.ID); ok {
		t.Fatal("test premise: the new process has no in-memory record")
	}
	e.harness.set(withServer(2, "b"))
	opens := e.ev.count("adapter.")
	err := restarted.ResumeSession(context.Background(), s.ID, true)
	if !errors.Is(err, agenthub.ErrHarnessNewSessionRequired) {
		t.Fatalf("restart + reload adopting a new executable: %v", err)
	}
	if _, err := restarted.Invoke(context.Background(), agenthub.InvokeInput{SessionID: s.ID, Prompt: "hi"}); !errors.Is(err, agenthub.ErrHarnessNewSessionRequired) {
		t.Fatalf("the first turn after a restart resumes through the same gate: %v", err)
	}
	if e.ev.count("adapter.") != opens {
		t.Fatal("a process was started for a refused resume")
	}
	// The same executables resume fine and the reference moves to the new revision.
	e.harness.set(withServer(2, "a"))
	if _, err := restarted.Invoke(context.Background(), agenthub.InvokeInput{SessionID: s.ID, Prompt: "hi"}); err != nil {
		t.Fatal(err)
	}
	if ref, _, _ := e.durable(s.ID); *ref.Revisions.Workspace != 2 {
		t.Fatalf("durable reference = %+v", ref)
	}
}

func TestAnUnreadableDurableReferenceRefusesTheResume(t *testing.T) {
	e := newLaunchEnv(t, plain)
	s := e.mustCreate()
	if _, err := e.db.Exec(`UPDATE agent_session_harness_applied SET ref='{bad' WHERE session_id=?`, s.ID); err != nil {
		t.Fatal(err)
	}
	restarted := e.restart()
	if err := restarted.ResumeSession(context.Background(), s.ID, true); !errors.Is(err, agenthub.ErrHarnessUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestAFailedDurableWriteRefusesTheLaunchAndPublishesNothing(t *testing.T) {
	e := newLaunchEnv(t, plain)
	s := e.mustCreate()
	first, _, _ := e.svc.HarnessApplied(s.ID)
	e.harness.set(func(sid, ws, v string) (harness.Snapshot, error) {
		return snapshotFor(t, sid, ws, v, func(in *harness.Inputs) { in.Workspace = &harness.Profile{Schema: 1, Revision: 2} }), nil
	})
	if _, err := e.db.Exec(`CREATE TRIGGER fail_applied BEFORE INSERT ON agent_session_harness_applied BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`CREATE TRIGGER fail_applied_u BEFORE UPDATE ON agent_session_harness_applied BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if err := e.reload(s.ID); err == nil {
		t.Fatal("a launch whose reference could not be written was accepted")
	}
	if ref, _, _ := e.svc.HarnessApplied(s.ID); !ref.Equal(first) {
		t.Fatalf("the in-memory record moved: %+v", ref)
	}
	eps := e.svc.MCPEndpoints.(*plainEndpoints)
	if len(eps.closed) < 2 {
		t.Fatalf("the staged endpoint was not closed: %v", eps.closed)
	}
	// A new session whose reference cannot be written leaves no row behind.
	count := func() (n int) {
		if err := e.db.QueryRow(`SELECT count(*) FROM agent_sessions`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := count()
	if _, err := e.create(); err == nil {
		t.Fatal("create accepted a launch whose reference could not be written")
	}
	if after := count(); after != before {
		t.Fatalf("a failed create left a session row: %d -> %d", before, after)
	}
}

func TestExplicitCloseRemovesTheOverrideAndKeepsTheReference(t *testing.T) {
	e := newLaunchEnv(t, plain)
	life := &fakeLifecycle{}
	e.svc.HarnessLifecycle = life
	s := e.mustCreate()
	if err := e.svc.CloseSession(context.Background(), s.ID); err != nil {
		t.Fatal(err)
	}
	if got := life.calls(); len(got) != 1 || got[0] != s.ID {
		t.Fatalf("lifecycle calls = %v", got)
	}
	if _, _, ok := e.svc.HarnessApplied(s.ID); !ok {
		t.Fatal("Close lost the in-memory reopen guard")
	}
	if _, _, found := e.durable(s.ID); !found {
		t.Fatal("Close lost the durable reopen guard")
	}
}

func TestReloadRewindAndATeardownNeverRunTheCloseCleanup(t *testing.T) {
	e := newLaunchEnv(t, plain)
	life := &fakeLifecycle{}
	e.svc.HarnessLifecycle = life
	s := e.mustCreate()
	if err := e.reload(s.ID); err != nil {
		t.Fatal(err)
	}
	e.adapter.kill() // the process dies; the next turn resumes it
	if _, err := e.svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: s.ID, Prompt: "x"}); err != nil {
		t.Fatal(err)
	}
	if got := life.calls(); len(got) != 0 {
		t.Fatalf("a reload or a crash removed the override: %v", got)
	}
	if _, _, ok := e.svc.HarnessApplied(s.ID); !ok {
		t.Fatal("a teardown dropped the applied record")
	}
}

func TestACleanupFailureIsReportedAndClosingAgainRetries(t *testing.T) {
	e := newLaunchEnv(t, plain)
	life := &fakeLifecycle{err: errors.New("db busy")}
	e.svc.HarnessLifecycle = life
	s := e.mustCreate()
	err := e.svc.CloseSession(context.Background(), s.ID)
	if !errors.Is(err, agenthub.ErrHarnessCleanupFailed) {
		t.Fatalf("a failed cleanup was reported as a clean close: %v", err)
	}
	got, _ := e.svc.GetStatus(context.Background(), s.ID)
	if got.Status != agenthub.SessionStatusClosed {
		t.Fatalf("the session itself must be closed: %v", got.Status)
	}
	life.setErr(nil)
	if err := e.svc.CloseSession(context.Background(), s.ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if len(life.calls()) != 2 {
		t.Fatalf("calls = %v", life.calls())
	}
	// Reference deletion belongs to Delete, not Close. A failed Delete keeps
	// both guards until the rows can actually be removed.
	e2 := newLaunchEnv(t, plain)
	s2 := e2.mustCreate()
	if _, err := e2.db.Exec(`CREATE TRIGGER fail_del BEFORE DELETE ON agent_session_harness_applied BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if err := e2.svc.CloseSession(context.Background(), s2.ID); err != nil {
		t.Fatalf("Close must not attempt reference deletion: %v", err)
	}
	if err := e2.svc.DeleteSession(context.Background(), s2.ID); err == nil {
		t.Fatal("Delete ignored a failed reference cascade")
	}
	if _, _, found := e2.durable(s2.ID); !found {
		t.Fatal("failed Delete lost the durable guard")
	}
	if _, _, found := e2.svc.HarnessApplied(s2.ID); !found {
		t.Fatal("failed Delete lost the in-memory guard")
	}
	if _, err := e2.db.Exec(`DROP TRIGGER fail_del`); err != nil {
		t.Fatal(err)
	}
	if err := e2.svc.DeleteSession(context.Background(), s2.ID); err != nil {
		t.Fatalf("retry Delete: %v", err)
	}
	if _, _, found := e2.durable(s2.ID); found {
		t.Fatal("Delete retained the durable reference")
	}
	if _, _, found := e2.svc.HarnessApplied(s2.ID); found {
		t.Fatal("Delete retained the in-memory reference")
	}
}

func TestDeletingASessionCleansItsSubtree(t *testing.T) {
	e := newLaunchEnv(t, plain)
	life := &fakeLifecycle{}
	e.svc.HarnessLifecycle = life
	s := e.mustCreate()
	if err := e.svc.DeleteSession(context.Background(), s.ID); err != nil {
		t.Fatal(err)
	}
	if got := life.calls(); len(got) == 0 || got[0] != s.ID {
		t.Fatalf("lifecycle calls = %v", got)
	}
	var n int
	if err := e.db.QueryRow(`SELECT count(*) FROM agent_session_harness_applied`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("references left: %d %v", n, err)
	}
	if _, _, found := e.svc.HarnessApplied(s.ID); found {
		t.Fatal("Delete retained the in-memory reference")
	}
}
