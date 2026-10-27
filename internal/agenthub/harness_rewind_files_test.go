package agenthub_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"devkit/internal/agenthub"
	"devkit/internal/checkpoint"
	"devkit/internal/harness"
	"devkit/internal/storage"
)

// A rewind of conversation and files against a real git repository, a real
// database and the policy endpoint, with hooks at the two moments an S commit
// can land: while the fork is opening (before the files are touched) and while
// the files are being restored (after the launch was last checked).

// fakeServing is the live-publication handle of one staged or published endpoint:
// enough of the real endpoint's contract (incarnation, admission, close) for the
// coordinator to own it.
type fakeServing struct {
	id          string
	fingerprint string
	mu          sync.Mutex
	paused      bool
	closed      bool
	tickets     uint64
}

func (f *fakeServing) Incarnation() string { return f.id }
func (f *fakeServing) State() agenthub.EndpointState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return agenthub.EndpointState{Incarnation: f.id, Paused: f.paused, Closed: f.closed, Fingerprint: f.fingerprint}
}
func (f *fakeServing) Quarantine() (agenthub.EndpointTicket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paused = true
	f.tickets++
	return agenthub.EndpointTicket(f.tickets), nil
}
func (f *fakeServing) Restore(agenthub.EndpointTicket) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paused = false
	return nil
}
func (f *fakeServing) Install(_ agenthub.EndpointTicket, eff harness.Effective, _ harness.ServingIdentity) (agenthub.InstallReport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paused, f.fingerprint = false, eff.Fingerprint
	return agenthub.InstallReport{}, nil
}
func (f *fakeServing) close() { f.mu.Lock(); f.closed = true; f.mu.Unlock() }

// liveEndpoints is the policy endpoint plus a live handle, so launches are staged with
// the session's coordinator as they are on the real host.
type liveEndpoints struct{ *policyEndpoints }

func (l liveEndpoints) OpenWithPolicy(sessionID string, e harness.Effective) (agenthub.MCPEndpoint, error) {
	ep, err := l.policyEndpoints.OpenWithPolicy(sessionID, e)
	if err != nil {
		return ep, err
	}
	live := &fakeServing{id: uuid.NewString(), fingerprint: e.Fingerprint}
	inner := ep.Close
	ep.Close = func() { live.close(); inner() }
	ep.Live = live
	return ep, nil
}

// basisSnapshot is a snapshot with a pinned serving basis, as the provider returns.
func basisSnapshot(t *testing.T, session, ws, vendor string) (harness.Snapshot, error) {
	t.Helper()
	in := harness.Inputs{Registry: tinyRegistry, Vendor: harness.Vendor{Revision: 4}, Workspace: &harness.Profile{Schema: 1, Revision: 1}}
	snap := snapshotFor(t, session, ws, vendor, func(i *harness.Inputs) { *i = in })
	basis, err := harness.NewServingBasis(in, snap.Effective, harness.NewCanonRecorder(func(p string) (string, error) { return p, nil }),
		harness.Stamp{Revision: 1, Present: true}, harness.Stamp{})
	if err != nil {
		return harness.Snapshot{}, err
	}
	snap.Basis = basis
	return snap, nil
}

// forkHookAdapter runs a hook as the nth fork starts. A non-nil error fails it.
type forkHookAdapter struct {
	replyingAdapter
	onFork func(n int) error
	forks  *atomic.Int32
}

func (a forkHookAdapter) Fork(ctx context.Context, spec agenthub.OpenSpec, src, messageID string) (agenthub.SessionRuntime, error) {
	n := int(a.forks.Add(1))
	if a.onFork != nil {
		if err := a.onFork(n); err != nil {
			return nil, err
		}
	}
	return a.replyingAdapter.Fork(ctx, spec, src, messageID)
}

// probeStore sees the two writes a rewind makes through the service's own store:
// the restore's safety row (the files are about to change) and the cut's transaction.
type probeStore struct {
	*agenthub.SQLStore
	mu             sync.Mutex
	onRestore      func(n int)
	restores       int
	cuts           int
	failCutWith    error
	restoreStarted func()
}

func (p *probeStore) InsertCheckpoint(ctx context.Context, rec agenthub.CheckpointRecord) error {
	if rec.Kind == agenthub.CheckpointRestoreSafety {
		p.mu.Lock()
		p.restores++
		n, hook := p.restores, p.onRestore
		p.mu.Unlock()
		if hook != nil {
			hook(n)
		}
	}
	return p.SQLStore.InsertCheckpoint(ctx, rec)
}

func (p *probeStore) RewindSession(ctx context.Context, spec agenthub.RewindSpec) error {
	p.mu.Lock()
	p.cuts++
	fail := p.failCutWith
	p.mu.Unlock()
	if fail != nil {
		return fail
	}
	return p.SQLStore.RewindSession(ctx, spec)
}

func (p *probeStore) counts() (restores, cuts int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.restores, p.cuts
}

type filesRig struct {
	t       *testing.T
	svc     *agenthub.Service
	store   *probeStore
	project string
	session agenthub.AgentSession
	prompts []string
	ev      *events
	forks   *atomic.Int32
	hooks   *forkHooks
}

type forkHooks struct {
	mu sync.Mutex
	fn func(n int) error
}

func (h *forkHooks) set(fn func(n int) error) { h.mu.Lock(); h.fn = fn; h.mu.Unlock() }
func (h *forkHooks) call(n int) error {
	h.mu.Lock()
	fn := h.fn
	h.mu.Unlock()
	if fn == nil {
		return nil
	}
	return fn(n)
}

func newFilesRig(t *testing.T) (*filesRig, func(sql string)) {
	t.Helper()
	git := checkpoint.Locate(context.Background())
	if !git.OK() {
		t.Skip("checkpoint: " + git.Reason)
	}
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = storage.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO workspaces (id, name, root) VALUES ('w1', 'w1', '')"); err != nil {
		t.Fatal(err)
	}
	raw := agenthub.NewSQLStore(db)
	store := &probeStore{SQLStore: raw}
	project := t.TempDir()
	turn := 0
	hooks := &forkHooks{}
	forks := &atomic.Int32{}
	reg := agenthub.NewRegistry()
	reg.Register(forkHookAdapter{
		replyingAdapter: replyingAdapter{MockAdapter: agenthub.NewMockAdapter(), closed: &atomic.Bool{}, writeDuringTurn: func() {
			turn++
			writeFile(t, project, "turn"+itoa(turn)+".txt", "from turn "+itoa(turn)+"\n")
		}},
		onFork: hooks.call,
		forks:  forks,
	})
	svc := agenthub.NewService(reg, store, func(string, json.RawMessage) {})
	svc.SetCheckpoints(agenthub.NewEngineCheckpoints(checkpoint.New(filepath.Join(t.TempDir(), "checkpoints"), git, checkpoint.Limits{}), raw, raw))
	svc.MatchingWorkspaces = func(string) ([]string, error) { return []string{"w1"}, nil }
	ev := &events{}
	svc.MCPEndpoints = liveEndpoints{&policyEndpoints{plainEndpoints: plainEndpoints{ev: ev}}}
	fh := &fakeHarness{ev: ev}
	fh.set(func(session, ws, vendor string) (harness.Snapshot, error) {
		return basisSnapshot(t, session, ws, vendor)
	})
	svc.Harness = fh

	writeFile(t, project, "kept.txt", "never touched\n")
	session, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: project, WorkspaceID: "w1"})
	if err != nil {
		t.Fatal(err)
	}
	var prompts []string
	for i := range 2 {
		if _, err = svc.Invoke(ctx, agenthub.InvokeInput{SessionID: session.ID, Prompt: "prompt " + itoa(i+1)}); err != nil {
			t.Fatal(err)
		}
		prompts = append(prompts, waitForCheckpoint(t, raw, session.ID, i+1)[i].PromptMessageID)
	}
	forks.Store(0)
	r := &filesRig{t: t, svc: svc, store: store, project: project, session: session, prompts: prompts, ev: ev, forks: forks, hooks: hooks}
	return r, func(q string) {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

// moveS is an S commit as the coordinator sees it: the epoch moves and any launch
// that reserved the old one is stale.
func (r *filesRig) moveS() {
	r.t.Helper()
	if err := r.svc.CoordinateServing(context.Background(), r.session.ID, func(txn *agenthub.ServingTxn) error { txn.MarkPending(); return nil }); err != nil {
		r.t.Fatal(err)
	}
}

func (r *filesRig) rewind(ctx context.Context) (agenthub.RewindResult, error) {
	r.t.Helper()
	preview, err := r.svc.PlanRewind(ctx, agenthub.RewindRequest{SessionID: r.session.ID, PromptMessageID: r.prompts[1], Mode: agenthub.RewindBoth})
	if err != nil {
		r.t.Fatal(err)
	}
	return r.svc.ApplyRewind(ctx, preview.Token, nil)
}

func (r *filesRig) exists(name string) bool {
	_, err := os.Stat(filepath.Join(r.project, name))
	return err == nil
}

func (r *filesRig) messages() int { return len(transcriptTexts(r.t, r.store, r.session.ID)) }

func (r *filesRig) safetyRow() agenthub.CheckpointRecord {
	r.t.Helper()
	records, err := r.store.ListCheckpoints(context.Background(), r.session.ID)
	if err != nil {
		r.t.Fatal(err)
	}
	for _, rec := range records {
		if rec.Kind == agenthub.CheckpointRewindSafety {
			return rec
		}
	}
	r.t.Fatal("no rewind safety row")
	return agenthub.CheckpointRecord{}
}

// endpointBalance: opens and closes since the given counts. Every endpoint a rewind
// stages but does not publish must have been closed.
func (r *filesRig) endpointDelta(opens, closes int) (int, int) {
	return r.ev.count("endpoint.openWithPolicy") - opens, r.ev.count("endpoint.close") - closes
}

func (r *filesRig) counts() (opens, closes int) {
	return r.ev.count("endpoint.openWithPolicy"), r.ev.count("endpoint.close")
}

// A rewind of conversation and files whose launch goes stale while the fork opens
// (an S commit) must refuse before it touches a file: a refusal that says "nothing
// was cut" is then true of the project folder too.
func TestAFilesRewindWhoseLaunchGoesStaleChangesNoFile(t *testing.T) {
	r, _ := newFilesRig(t)
	r.hooks.set(func(n int) error {
		if n == 1 {
			r.moveS()
		}
		return nil
	})
	if _, err := r.rewind(context.Background()); !errors.Is(err, agenthub.ErrHarnessLaunchStale) || errors.Is(err, agenthub.ErrRewindFilesRestored) {
		t.Fatalf("rewind = %v, want a stale refusal that did not restore anything", err)
	}
	if !r.exists("turn2.txt") || !r.exists("turn1.txt") || r.messages() != 2 {
		t.Fatal("a rewind refused before the restore changed the project or the conversation")
	}
	restores, cuts := r.store.counts()
	if restores != 0 || cuts != 0 {
		t.Fatalf("restores=%d cuts=%d", restores, cuts)
	}
	// The retry is a whole new rewind and completes.
	r.hooks.set(nil)
	if _, err := r.rewind(context.Background()); err != nil {
		t.Fatalf("the retry after a stale launch: %v", err)
	}
	if r.exists("turn2.txt") || r.messages() != 1 {
		t.Fatal("the completed rewind left the dropped turn")
	}
}

// S commits while the files are being restored. The cut is finished forward: the fork
// and endpoint prepared for the old S are discarded, a launch is prepared again, the
// files are not restored a second time, and the cut, the turn rows, the safety row
// and the new launch's applied reference land in one transaction.
func TestAnSChangeDuringTheRestoreFinishesTheRewindForward(t *testing.T) {
	r, _ := newFilesRig(t)
	ctx := context.Background()
	opens, closes := r.counts()
	r.store.onRestore = func(n int) { r.moveS() }

	if _, err := r.rewind(ctx); err != nil {
		t.Fatalf("a rewind whose S moved during the restore must finish: %v", err)
	}
	restores, cuts := r.store.counts()
	if restores != 1 {
		t.Fatalf("the files were restored %d times, want once", restores)
	}
	if cuts != 1 {
		t.Fatalf("the cut was attempted %d times: the stale attempt must be refused before the transaction", cuts)
	}
	if got := int(r.forks.Load()); got != 2 {
		t.Fatalf("forks = %d, want the first and one re-prepared", got)
	}
	if r.exists("turn2.txt") || !r.exists("turn1.txt") || !r.exists("kept.txt") || r.messages() != 1 {
		t.Fatal("files and conversation must both be rewound")
	}
	// One endpoint is live; the old session endpoint and the discarded staged one are closed.
	do, dc := r.endpointDelta(opens, closes)
	if do != 2 || dc != 2 {
		t.Fatalf("endpoint opens=%d closes=%d, want 2 and 2 (the old one and the discarded staged one)", do, dc)
	}
	view := r.svc.ServingTarget(r.session.ID)
	if !view.Active || !view.Acknowledged || view.Pending {
		t.Fatalf("the published endpoint must be the new, acknowledged one: %+v", view)
	}
	// The applied reference and the cut committed together, and describe the new launch.
	durable, enforced, found, err := r.store.GetHarnessApplied(ctx, r.session.ID)
	mem, _, _ := r.svc.HarnessApplied(r.session.ID)
	if err != nil || !found || !enforced || !durable.Equal(mem) {
		t.Fatalf("durable reference %+v (found %v, enforced %v, %v) != memory %+v", durable, found, enforced, err, mem)
	}
	// The original rewind's safety row went with the cut (it belongs to the dropped
	// turn); none is left pending, and the retry took no second one.
	if pending, err := r.svc.InterruptedRewinds(ctx); err != nil || len(pending) != 0 {
		t.Fatalf("a finished rewind leaves no interrupted safety row: %v %v", pending, err)
	}
	rows, err := r.store.ListTurns(ctx, r.session.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("turns = %d: %v", len(rows), err)
	}
}

// S keeps changing: the retry is finite. When the attempts are used up the answer says
// the files were restored and the conversation was not, the safety row is the one of
// the original rewind, nothing is left staged, and a fresh rewind completes.
func TestRepeatedSChangesAreRetriedAFiniteNumberOfTimes(t *testing.T) {
	r, _ := newFilesRig(t)
	ctx := context.Background()
	opens, closes := r.counts()
	r.store.onRestore = func(int) { r.moveS() }
	r.hooks.set(func(n int) error {
		if n >= 2 {
			r.moveS() // every re-prepared launch goes stale too
		}
		return nil
	})

	_, err := r.rewind(ctx)
	var partial *agenthub.RewindPartialError
	if !errors.As(err, &partial) || !errors.Is(err, agenthub.ErrRewindFilesRestored) || !errors.Is(err, agenthub.ErrHarnessLaunchStale) {
		t.Fatalf("want a partial rewind caused by a stale launch, got %v", err)
	}
	if got := int(r.forks.Load()); got != 3 {
		t.Fatalf("forks = %d, want the first and two re-prepared (3 attempts in all)", got)
	}
	restores, cuts := r.store.counts()
	if restores != 1 || cuts != 0 {
		t.Fatalf("restores=%d cuts=%d: one restore, and no cut once every attempt was stale", restores, cuts)
	}
	if r.exists("turn2.txt") || r.messages() != 2 {
		t.Fatal("files are restored and the conversation is not cut")
	}
	row := r.safetyRow()
	if row.Status != agenthub.CheckpointFailed || partial.SafetyCheckpointID != row.ID {
		t.Fatalf("safety row %s (%s) vs error's %s: the original rewind's checkpoint is the way back", row.ID, row.Status, partial.SafetyCheckpointID)
	}
	do, dc := r.endpointDelta(opens, closes)
	if do != 3 || dc != 4 {
		t.Fatalf("endpoint opens=%d closes=%d, want 3 staged all discarded plus the old one closed (3 and 4)", do, dc)
	}
	if v := r.svc.ServingTarget(r.session.ID); v.Active {
		t.Fatalf("a refused rewind must leave no endpoint staged or serving: %+v", v)
	}

	// The recovery the error names: the same rewind again.
	r.store.onRestore = nil
	r.hooks.set(nil)
	if _, err := r.rewind(ctx); err != nil {
		t.Fatalf("rewinding the same message again must finish: %v", err)
	}
	if r.messages() != 1 || r.exists("turn2.txt") {
		t.Fatal("the second rewind did not finish the job")
	}
}

// Only S is retried. A Close that completed, or one whose cleanup failed and so keeps
// the fence, ends the rewind without another fork.
func TestACloseBetweenRetriesEndsTheRewindWithoutAnotherAttempt(t *testing.T) {
	for name, failCleanup := range map[string]bool{"completed close": false, "close whose cleanup failed": true} {
		t.Run(name, func(t *testing.T) {
			r, _ := newFilesRig(t)
			ctx := context.Background()
			if failCleanup {
				r.svc.HarnessLifecycle = failingLifecycle{}
			}
			r.store.onRestore = func(int) { r.moveS() }
			r.hooks.set(func(n int) error {
				if n == 2 {
					_ = r.svc.CloseSession(ctx, r.session.ID)
				}
				return nil
			})
			_, err := r.rewind(ctx)
			if !errors.Is(err, agenthub.ErrRewindFilesRestored) {
				t.Fatalf("want a partial rewind, got %v", err)
			}
			if got := int(r.forks.Load()); got != 2 {
				t.Fatalf("forks = %d: a Close must not be retried", got)
			}
			if r.messages() != 2 || r.exists("turn2.txt") {
				t.Fatal("files restored, conversation uncut")
			}
			if _, cuts := r.store.counts(); cuts != 0 {
				t.Fatal("a closed session must not get a cut")
			}
			if v := r.svc.ServingTarget(r.session.ID); v.Active {
				t.Fatalf("nothing may be left serving a closed session: %+v", v)
			}
		})
	}
}

type failingLifecycle struct{}

func (failingLifecycle) SessionClosed(context.Context, string) error {
	return errors.New("cleanup failed")
}

// A fork that fails on the re-prepared launch, a store that fails the cut, and a
// cancelled caller are not S: none is retried, each says what state it leaves, and
// no endpoint stays staged.
func TestRefusalsOtherThanSAreNotRetried(t *testing.T) {
	t.Run("the re-prepared fork fails", func(t *testing.T) {
		r, _ := newFilesRig(t)
		opens, closes := r.counts()
		r.store.onRestore = func(int) { r.moveS() }
		boom := errors.New("vendor refused the fork")
		r.hooks.set(func(n int) error {
			if n >= 2 {
				return boom
			}
			return nil
		})
		_, err := r.rewind(context.Background())
		if !errors.Is(err, agenthub.ErrRewindFilesRestored) || !errors.Is(err, boom) {
			t.Fatalf("want a partial rewind caused by the fork failure, got %v", err)
		}
		if r.messages() != 2 || r.exists("turn2.txt") {
			t.Fatal("files restored, conversation uncut")
		}
		do, dc := r.endpointDelta(opens, closes)
		if do != dc-1 || r.svc.ServingTarget(r.session.ID).Active {
			t.Fatalf("a failed re-prepared launch left an endpoint behind (opens %d, closes %d)", do, dc)
		}
	})

	t.Run("the cut transaction fails", func(t *testing.T) {
		r, exec := newFilesRig(t)
		ctx := context.Background()
		oldDurable, _, _, err := r.store.GetHarnessApplied(ctx, r.session.ID)
		if err != nil {
			t.Fatal(err)
		}
		// The applied reference is part of the same transaction as the cut: failing its
		// write rolls the cut back too.
		exec("CREATE TRIGGER fail_ref BEFORE UPDATE ON agent_session_harness_applied BEGIN SELECT RAISE(ABORT,'injected'); END")
		_, err = r.rewind(ctx)
		if !errors.Is(err, agenthub.ErrRewindFilesRestored) || errors.Is(err, agenthub.ErrHarnessLaunchStale) {
			t.Fatalf("want a partial rewind that is not a stale launch, got %v", err)
		}
		if got := int(r.forks.Load()); got != 1 {
			t.Fatalf("forks = %d: a store failure must not be retried", got)
		}
		rows, terr := r.store.ListTurns(ctx, r.session.ID)
		if terr != nil || len(rows) != 2 || r.messages() != 2 {
			t.Fatalf("the cut must roll back with the reference: turns %d, messages %d (%v)", len(rows), r.messages(), terr)
		}
		durable, _, _, _ := r.store.GetHarnessApplied(ctx, r.session.ID)
		if !durable.Equal(oldDurable) {
			t.Fatal("a failed transaction changed the durable reference")
		}
		if r.exists("turn2.txt") {
			t.Fatal("the files were restored before the failing cut")
		}
		if r.svc.ServingTarget(r.session.ID).Active {
			t.Fatal("a failed cut left its staged endpoint")
		}
	})

	t.Run("the caller is cancelled", func(t *testing.T) {
		r, _ := newFilesRig(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		r.store.onRestore = func(int) { r.moveS() }
		r.hooks.set(func(n int) error {
			if n == 2 {
				r.moveS() // the retry would go stale again, but the caller has gone
				cancel()
			}
			return nil
		})
		_, err := r.rewind(ctx)
		if !errors.Is(err, agenthub.ErrRewindFilesRestored) {
			t.Fatalf("want a partial rewind, got %v", err)
		}
		if got := int(r.forks.Load()); got != 2 {
			t.Fatalf("forks = %d: a cancelled caller must not start another attempt", got)
		}
		if r.messages() != 2 || r.svc.ServingTarget(r.session.ID).Active {
			t.Fatal("conversation uncut and nothing left serving")
		}
	})
}
