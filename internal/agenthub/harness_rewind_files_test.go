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

// liveLog records the live handles the endpoints hand out, so a test can lose one.
type liveLog struct {
	mu  sync.Mutex
	eps []*fakeServing
}

func (l *liveLog) add(f *fakeServing) {
	l.mu.Lock()
	l.eps = append(l.eps, f)
	l.mu.Unlock()
}

func (l *liveLog) last() *fakeServing {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.eps[len(l.eps)-1]
}

// liveEndpoints is the policy endpoint plus a live handle, so launches are staged with
// the session's coordinator as they are on the real host. fail makes the nth open
// (1-based, counted from the first rewind) refuse, as an endpoint that cannot be staged.
type liveEndpoints struct {
	*policyEndpoints
	log   *liveLog
	opens *atomic.Int32
	fail  func(n int) error
}

func (l liveEndpoints) OpenWithPolicy(sessionID string, e harness.Effective) (agenthub.MCPEndpoint, error) {
	if n := int(l.opens.Add(1)); l.fail != nil {
		if err := l.fail(n); err != nil {
			return agenthub.MCPEndpoint{}, err
		}
	}
	ep, err := l.policyEndpoints.OpenWithPolicy(sessionID, e)
	if err != nil {
		return ep, err
	}
	live := &fakeServing{id: uuid.NewString(), fingerprint: e.Fingerprint}
	l.log.add(live)
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

// forkProbe is one forked vendor runtime as a test sees it: whether the rewind closed it,
// whether it asked the vendor to forget it (Delete, which destroys the conversation) and
// which vendor sessions it was asked to delete (the source, after a rewind that won).
type forkProbe struct {
	agenthub.SessionRuntime
	mu              sync.Mutex
	closed          bool
	deleted         bool
	deletedSessions []string
	// modes are the modes it was asked to take, and each with whether the caller's
	// context was still alive; deleteCtxErrs the same for the source session's deletion.
	modes         []string
	modeCtxErrs   []error
	deleteCtxErrs []error
}

func (f *forkProbe) Close(ctx context.Context) error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return f.SessionRuntime.Close(ctx)
}

func (f *forkProbe) Delete(ctx context.Context) error {
	f.mu.Lock()
	f.deleted = true
	f.mu.Unlock()
	if d, ok := f.SessionRuntime.(agenthub.RuntimeDeleter); ok {
		return d.Delete(ctx)
	}
	return nil
}

func (f *forkProbe) DeleteSession(ctx context.Context, id string) error {
	f.mu.Lock()
	f.deletedSessions = append(f.deletedSessions, id)
	f.deleteCtxErrs = append(f.deleteCtxErrs, ctx.Err())
	f.mu.Unlock()
	if d, ok := f.SessionRuntime.(agenthub.VendorSessionDeleter); ok {
		return d.DeleteSession(ctx, id)
	}
	return nil
}

func (f *forkProbe) Unwrap() agenthub.SessionRuntime { return f.SessionRuntime }

func (f *forkProbe) SetMode(ctx context.Context, mode string) error {
	f.mu.Lock()
	f.modes = append(f.modes, mode)
	f.modeCtxErrs = append(f.modeCtxErrs, ctx.Err())
	f.mu.Unlock()
	if c, ok := f.SessionRuntime.(agenthub.RuntimeConfigurator); ok {
		return c.SetMode(ctx, mode)
	}
	return nil
}

func (f *forkProbe) SetConfigOption(ctx context.Context, id, value string) error {
	if c, ok := f.SessionRuntime.(agenthub.RuntimeConfigurator); ok {
		return c.SetConfigOption(ctx, id, value)
	}
	return nil
}

func (f *forkProbe) state() (closed, deleted bool, sessions []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed, f.deleted, append([]string(nil), f.deletedSessions...)
}

type probeLog struct {
	mu     sync.Mutex
	probes []*forkProbe
}

func (l *probeLog) add(p *forkProbe) {
	l.mu.Lock()
	l.probes = append(l.probes, p)
	l.mu.Unlock()
}

// fork returns the nth fork runtime the vendor made (1-based).
func (l *probeLog) fork(n int) *forkProbe {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.probes[n-1]
}

// forkHookAdapter runs a hook as the nth fork starts. A non-nil error fails it.
type forkHookAdapter struct {
	replyingAdapter
	onFork func(n int) error
	forks  *atomic.Int32
	probes *probeLog
}

func (a forkHookAdapter) Fork(ctx context.Context, spec agenthub.OpenSpec, src, messageID string) (agenthub.SessionRuntime, error) {
	n := int(a.forks.Add(1))
	if a.onFork != nil {
		if err := a.onFork(n); err != nil {
			return nil, err
		}
	}
	rt, err := a.replyingAdapter.Fork(ctx, spec, src, messageID)
	if err != nil {
		return nil, err
	}
	probe := &forkProbe{SessionRuntime: rt}
	a.probes.add(probe)
	return probe, nil
}

// probeStore sees the two writes a rewind makes through the service's own store:
// the restore's safety row (the files are about to change) and the cut's transaction.
type probeStore struct {
	*agenthub.SQLStore
	mu          sync.Mutex
	onRestore   func(n int)
	restores    int
	cuts        int
	failCutWith error
	// cutFn replaces the cut transaction: it is handed the real one, so a test can run it
	// and still report a failure (a commit whose acknowledgement was lost), or report one
	// without running it (a commit that did not happen).
	cutFn func(real func() error) error
	// failReads makes GetSession fail, as a store that cannot be read back.
	failReads atomic.Bool
	// failUpdates makes UpdateCheckpoint fail, as a store whose bookkeeping cannot land.
	failUpdates atomic.Bool
}

func (p *probeStore) GetSession(ctx context.Context, id string) (agenthub.AgentSession, error) {
	if p.failReads.Load() {
		return agenthub.AgentSession{}, errors.New("injected: the store cannot be read")
	}
	return p.SQLStore.GetSession(ctx, id)
}

func (p *probeStore) UpdateCheckpoint(ctx context.Context, rec agenthub.CheckpointRecord) error {
	if p.failUpdates.Load() {
		return errors.New("injected: the checkpoint cannot be updated")
	}
	return p.SQLStore.UpdateCheckpoint(ctx, rec)
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
	fail, cutFn := p.failCutWith, p.cutFn
	p.mu.Unlock()
	if fail != nil {
		return fail
	}
	real := func() error { return p.SQLStore.RewindSession(ctx, spec) }
	if cutFn != nil {
		return cutFn(real)
	}
	return real()
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
	lives   *liveLog
	opens   *atomic.Int32
	probes  *probeLog
	// failOpen makes the nth endpoint staged by the rewind refuse.
	failOpen atomic.Pointer[func(n int) error]
	harness  *fakeHarness
	// rewound counts OnSessionRewound; recordErrs the bookkeeping failures the service reported.
	rewound    atomic.Int32
	recordErrs atomic.Int32
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

// newFilesRig builds the rig. Each turnHook runs inside the mock vendor's turn after the
// turn's own file is written, so a test can give a turn more to undo than its one file.
func newFilesRig(t *testing.T, turnHooks ...func(project string, turn int)) (*filesRig, func(sql string)) {
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
	probes := &probeLog{}
	r := &filesRig{t: t, store: store, project: project, forks: forks, hooks: hooks, probes: probes, lives: &liveLog{}, opens: &atomic.Int32{}}
	reg := agenthub.NewRegistry()
	reg.Register(forkHookAdapter{
		replyingAdapter: replyingAdapter{MockAdapter: agenthub.NewMockAdapter(), closed: &atomic.Bool{}, writeDuringTurn: func() {
			turn++
			writeFile(t, project, "turn"+itoa(turn)+".txt", "from turn "+itoa(turn)+"\n")
			for _, hook := range turnHooks {
				hook(project, turn)
			}
		}},
		onFork: hooks.call,
		forks:  forks,
		probes: probes,
	})
	svc := agenthub.NewService(reg, store, func(string, json.RawMessage) {})
	svc.SetCheckpoints(agenthub.NewEngineCheckpoints(checkpoint.New(filepath.Join(t.TempDir(), "checkpoints"), git, checkpoint.Limits{}), raw, raw))
	svc.MatchingWorkspaces = func(string) ([]string, error) { return []string{"w1"}, nil }
	svc.OnSessionRewound = func(string, string) { r.rewound.Add(1) }
	svc.OnRecordError = func(string, error) { r.recordErrs.Add(1) }
	ev := &events{}
	svc.MCPEndpoints = liveEndpoints{
		policyEndpoints: &policyEndpoints{plainEndpoints: plainEndpoints{ev: ev}},
		log:             r.lives,
		opens:           r.opens,
		fail: func(n int) error {
			if f := r.failOpen.Load(); f != nil {
				return (*f)(n)
			}
			return nil
		},
	}
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
	r.opens.Store(0)
	r.svc, r.session, r.prompts, r.ev, r.harness = svc, session, prompts, ev, fh
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

// cappedMoveS is moveS for a hook that fires on every preparation or fork: it moves S at
// most limit times. A rewind whose launch budget holds can never ask for more than the
// budget allows; one whose budget is broken would otherwise keep a stale launch coming
// forever (each round stages an endpoint and a fork), so past the limit the test fails
// with a clear message and S stops moving, which lets the rewind end and the test fail
// fast instead of exhausting memory.
func (r *filesRig) cappedMoveS(limit int32) func() {
	var moves atomic.Int32
	var reported atomic.Bool
	return func() {
		if moves.Add(1) > limit {
			if reported.CompareAndSwap(false, true) {
				r.t.Errorf("S was moved for more than %d launches: the rewind's launch budget (%d) is not bounding it", limit, 3)
			}
			return
		}
		r.moveS()
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
	// Only the winning attempt is announced and attached, and only it deletes the source:
	// the abandoned fork is closed and forgotten, never the vendor's source conversation.
	if got := r.rewound.Load(); got != 1 {
		t.Fatalf("OnSessionRewound fired %d times, want once for the winning attempt", got)
	}
	if closed, deleted, sessions := r.probes.fork(1).state(); !closed || !deleted || len(sessions) != 0 {
		t.Fatalf("the abandoned fork: closed=%v deleted=%v deletedSessions=%v", closed, deleted, sessions)
	}
	winner := r.probes.fork(2)
	if closed, deleted, sessions := winner.state(); closed || deleted || len(sessions) != 1 || sessions[0] != r.session.ACPSessionID {
		t.Fatalf("the winning fork: closed=%v deleted=%v deletedSessions=%v, want it open and the source deleted once", closed, deleted, sessions)
	}
	if stored, err := r.store.SQLStore.GetSession(ctx, r.session.ID); err != nil || stored.ACPSessionID != winner.ACPSessionID() {
		t.Fatalf("the stored vendor id must be the winning fork's: %q vs %q (%v)", stored.ACPSessionID, winner.ACPSessionID(), err)
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
	moveCapped := r.cappedMoveS(5) // the budget allows 2 re-prepared forks; 5 is already a failure
	r.hooks.set(func(n int) error {
		if n >= 2 {
			moveCapped() // every re-prepared launch goes stale too
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

// ---- classification, budget, ambiguity, cleanup and recovery -----------------------
//
// moveS in this file is an epoch-only seam: it marks S as moved on the coordinator,
// which is all the launch fence reads. The same flow with an S edit committed through
// the real harness configuration service is exercised in internal/app
// (TestACommittedSChangeDuringTheFileRestoreFinishesTheRewind).

func (r *filesRig) dropped() bool { return !r.exists("turn2.txt") }

// assertPartial: the files were put back, the conversation was proven not rewound.
func (r *filesRig) assertPartial(err error) *agenthub.RewindPartialError {
	r.t.Helper()
	var partial *agenthub.RewindPartialError
	if !errors.As(err, &partial) || !errors.Is(err, agenthub.ErrRewindFilesRestored) || errors.Is(err, agenthub.ErrRewindOutcomeUnknown) {
		r.t.Fatalf("want a proven partial rewind, got %v", err)
	}
	if !r.dropped() || r.messages() != 2 {
		r.t.Fatal("files restored, conversation uncut")
	}
	return partial
}

// A commit of S together with the loss of the staged endpoint is not "S moved": the
// epoch moved too, but the endpoint is no longer this launch's, and re-preparing would
// stage a new one over whatever replaced it.
func TestAnSChangeTogetherWithALostEndpointIsNotRetriedAsS(t *testing.T) {
	r, _ := newFilesRig(t)
	r.store.onRestore = func(int) {
		r.moveS()
		r.lives.last().close() // the staged endpoint is gone
	}
	_, err := r.rewind(context.Background())
	partial := r.assertPartial(err)
	if !errors.Is(err, agenthub.ErrHarnessLaunchStale) {
		t.Fatalf("the cause is the refused publication, got %v", err)
	}
	if got := int(r.forks.Load()); got != 1 {
		t.Fatalf("forks = %d: a lost endpoint must not be answered with another fork", got)
	}
	if _, cuts := r.store.counts(); cuts != 0 {
		t.Fatal("a refused publication must not reach the cut")
	}
	if row := r.safetyRow(); row.Status != agenthub.CheckpointFailed || row.ID != partial.SafetyCheckpointID || row.BeforeTree == "" {
		t.Fatalf("the safety row must be settled with its tree kept: %+v", row)
	}
	if r.svc.ServingTarget(r.session.ID).Active {
		t.Fatal("nothing may be left serving")
	}
}

// The launch budget is one for the whole rewind. A re-prepared launch that goes stale
// while it is being prepared consumes the same budget the finish-forward rounds do,
// instead of getting a budget of its own (launchAttempts preparations in all: 3).
func TestEveryPreparationOfARewindSharesOneLaunchBudget(t *testing.T) {
	r, _ := newFilesRig(t)
	ctx := context.Background()
	opens, closes := r.counts()
	var armed atomic.Bool
	moveCapped := r.cappedMoveS(6) // the budget allows 3 preparations in all; 6 is already a failure
	r.store.onRestore = func(int) { armed.Store(true); r.moveS() }
	r.harness.set(func(session, ws, vendor string) (harness.Snapshot, error) {
		if armed.Load() {
			moveCapped() // every preparation after the restore goes stale before it is staged
		}
		return basisSnapshot(t, session, ws, vendor)
	})

	_, err := r.rewind(ctx)
	r.assertPartial(err)
	if !errors.Is(err, agenthub.ErrHarnessLaunchStale) {
		t.Fatalf("want the stale launch as the cause, got %v", err)
	}
	do, dc := r.endpointDelta(opens, closes)
	if do != 3 {
		t.Fatalf("prepared %d launches (each opens an endpoint), want at most 3 in all for one rewind", do)
	}
	if dc != do+1 {
		t.Fatalf("endpoint closes = %d, want every staged endpoint and the old one closed (%d)", dc, do+1)
	}
	if got := int(r.forks.Load()); got != 1 {
		t.Fatalf("forks = %d: a launch that never became current is never forked", got)
	}
	if restores, cuts := r.store.counts(); restores != 1 || cuts != 0 {
		t.Fatalf("restores=%d cuts=%d", restores, cuts)
	}
}

// The cut transaction can fail while committing, and then nobody knows whether it
// happened. The rewind reads the durable facts back before it deletes, reports or
// publishes anything.
func TestAnAmbiguousCutCommitIsReadBackBeforeAnythingIsDeleted(t *testing.T) {
	lost := func() error { return errors.Join(agenthub.ErrRewindCommitUnknown, errors.New("acknowledgement lost")) }

	t.Run("it did commit", func(t *testing.T) {
		r, _ := newFilesRig(t)
		r.store.cutFn = func(real func() error) error {
			if err := real(); err != nil {
				return err
			}
			return lost()
		}
		if _, err := r.rewind(context.Background()); err != nil {
			t.Fatalf("a cut that did commit is a rewind that succeeded: %v", err)
		}
		if !r.dropped() || r.messages() != 1 {
			t.Fatal("files and conversation must both be rewound")
		}
		if _, cuts := r.store.counts(); cuts != 1 {
			t.Fatalf("cuts = %d: a proven commit is never cut again", cuts)
		}
		fork := r.probes.fork(1)
		if closed, deleted, sessions := fork.state(); closed || deleted || len(sessions) != 1 || sessions[0] != r.session.ACPSessionID {
			t.Fatalf("the durable winner must stay and the source go once: closed=%v deleted=%v sessions=%v", closed, deleted, sessions)
		}
		stored, err := r.store.SQLStore.GetSession(context.Background(), r.session.ID)
		if err != nil || stored.ACPSessionID != fork.ACPSessionID() {
			t.Fatalf("the durable vendor id must be the fork's: %q vs %q (%v)", stored.ACPSessionID, fork.ACPSessionID(), err)
		}
		if view := r.svc.ServingTarget(r.session.ID); !view.Active || !view.Acknowledged {
			t.Fatalf("a proven commit publishes its endpoint: %+v", view)
		}
		if got := r.rewound.Load(); got != 1 {
			t.Fatalf("OnSessionRewound fired %d times, want once", got)
		}
		if pending, err := r.svc.InterruptedRewinds(context.Background()); err != nil || len(pending) != 0 {
			t.Fatalf("a committed rewind leaves no pending safety row: %v %v", pending, err)
		}
	})

	t.Run("it did not commit", func(t *testing.T) {
		r, _ := newFilesRig(t)
		r.store.cutFn = func(func() error) error { return lost() }
		_, err := r.rewind(context.Background())
		partial := r.assertPartial(err)
		if got := int(r.forks.Load()); got != 1 {
			t.Fatalf("forks = %d: a proven non-commit is not an S refusal and is not retried", got)
		}
		fork := r.probes.fork(1)
		if closed, deleted, sessions := fork.state(); !closed || !deleted || len(sessions) != 0 {
			t.Fatalf("a proven non-commit discards its own fork and never touches the source: closed=%v deleted=%v sessions=%v", closed, deleted, sessions)
		}
		if row := r.safetyRow(); row.Status != agenthub.CheckpointFailed || row.ID != partial.SafetyCheckpointID {
			t.Fatalf("safety row: %+v", row)
		}
		if r.svc.ServingTarget(r.session.ID).Active || r.rewound.Load() != 0 {
			t.Fatal("nothing is published and nobody is told the conversation was rewound")
		}
	})

	t.Run("it cannot be read back", func(t *testing.T) {
		r, _ := newFilesRig(t)
		r.store.cutFn = func(func() error) error {
			r.store.failReads.Store(true)
			return lost()
		}
		_, err := r.rewind(context.Background())
		r.store.failReads.Store(false)
		var unknown *agenthub.RewindUnknownError
		if !errors.As(err, &unknown) || !errors.Is(err, agenthub.ErrRewindOutcomeUnknown) || errors.Is(err, agenthub.ErrRewindFilesRestored) {
			t.Fatalf("want an unproven outcome, got %v", err)
		}
		fork := r.probes.fork(1)
		if closed, deleted, sessions := fork.state(); !closed || deleted || len(sessions) != 0 {
			t.Fatalf("a fork that may be the durable winner is closed, never deleted, and the source stays: closed=%v deleted=%v sessions=%v", closed, deleted, sessions)
		}
		row := r.safetyRow()
		if row.Status != agenthub.CheckpointPending || row.ID != unknown.SafetyCheckpointID || row.BeforeTree == "" {
			t.Fatalf("an unproven outcome keeps its safety row pending, tree kept: %+v", row)
		}
		if pending, err := r.svc.InterruptedRewinds(context.Background()); err != nil || len(pending) != 1 {
			t.Fatalf("the next start must still report it: %v %v", pending, err)
		}
		if r.svc.ServingTarget(r.session.ID).Active || r.rewound.Load() != 0 {
			t.Fatal("an unproven outcome publishes nothing and announces nothing")
		}
	})
}

// A conversation-only rewind restored no file, so an unprovable commit must not be
// reported with the file sentence; it is the commit sentinel itself, and nothing is deleted.
func TestAnUnprovableCutOfAConversationOnlyRewindClaimsNoFileChange(t *testing.T) {
	r, _ := newFilesRig(t)
	ctx := context.Background()
	r.store.cutFn = func(func() error) error {
		r.store.failReads.Store(true)
		return errors.Join(agenthub.ErrRewindCommitUnknown, errors.New("acknowledgement lost"))
	}
	preview, err := r.svc.PlanRewind(ctx, agenthub.RewindRequest{SessionID: r.session.ID, PromptMessageID: r.prompts[1], Mode: agenthub.RewindConversation})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.svc.ApplyRewind(ctx, preview.Token, nil)
	r.store.failReads.Store(false)
	if !errors.Is(err, agenthub.ErrRewindCommitUnknown) || errors.Is(err, agenthub.ErrRewindFilesRestored) || errors.Is(err, agenthub.ErrRewindOutcomeUnknown) {
		t.Fatalf("want the bare commit-unknown error, got %v", err)
	}
	if !r.exists("turn2.txt") {
		t.Fatal("a conversation-only rewind must not touch a file")
	}
	if closed, deleted, sessions := r.probes.fork(1).state(); !closed || deleted || len(sessions) != 0 {
		t.Fatalf("closed=%v deleted=%v sessions=%v: the fork may be the durable winner", closed, deleted, sessions)
	}
	if r.svc.ServingTarget(r.session.ID).Active || r.rewound.Load() != 0 {
		t.Fatal("nothing is published and nobody is told")
	}
}

// A caller that goes away must not strand the rewind's own bookkeeping: the safety row
// is settled and the abandoned forks are discarded on a bounded context of the host's.
func TestACancelledCallerStillSettlesTheSafetyRowAndDiscardsTheForks(t *testing.T) {
	r, _ := newFilesRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.store.onRestore = func(int) { r.moveS() }
	r.hooks.set(func(n int) error {
		if n == 2 {
			r.moveS()
			cancel()
		}
		return nil
	})
	_, err := r.rewind(ctx)
	partial := r.assertPartial(err)
	if row := r.safetyRow(); row.Status != agenthub.CheckpointFailed || row.ID != partial.SafetyCheckpointID || row.BeforeTree == "" {
		t.Fatalf("the safety row must be settled although the caller is gone: %+v", row)
	}
	// The second fork was refused by the vendor with the caller's context already
	// dead, so only the first one exists; it is the one owed a discard.
	if closed, deleted, _ := r.probes.fork(1).state(); !closed || !deleted {
		t.Fatalf("the abandoned fork: closed=%v deleted=%v, want both", closed, deleted)
	}
	if r.svc.ServingTarget(r.session.ID).Active {
		t.Fatal("nothing may be left serving")
	}
}

// When even the bookkeeping cannot be written, the outcome is still reported as it is
// and the failure is surfaced, not swallowed; the row is not claimed to be settled.
func TestASafetyRowThatCannotBeSettledIsReported(t *testing.T) {
	r, _ := newFilesRig(t)
	r.store.onRestore = func(int) { r.moveS() }
	r.hooks.set(func(n int) error {
		if n >= 2 {
			r.moveS()
		}
		if n == 3 {
			r.store.failUpdates.Store(true) // the restore's own update already landed
		}
		return nil
	})
	_, err := r.rewind(context.Background())
	r.store.failUpdates.Store(false)
	partial := r.assertPartial(err)
	if r.recordErrs.Load() == 0 {
		t.Fatal("a failed bookkeeping write must be reported through OnRecordError")
	}
	row := r.safetyRow()
	if row.ID != partial.SafetyCheckpointID || row.Status != agenthub.CheckpointPending || row.BeforeTree == "" {
		t.Fatalf("an unsettled row stays pending with its tree: %+v", row)
	}
}

// A process that dies while the finish-forward retry is running leaves the pending
// safety row with its pre-restore tree, and a restart replays nothing.
func TestACrashDuringTheRetryLeavesThePendingSafetyRowAndReplaysNothing(t *testing.T) {
	r, _ := newFilesRig(t)
	ctx := context.Background()
	r.store.onRestore = func(int) { r.moveS() }
	forksAtRestart := int32(-1)
	r.hooks.set(func(n int) error {
		if n != 2 {
			return nil
		}
		// The process "dies" here: a new Service over the same database is a restart.
		restarted := agenthub.NewService(agenthub.NewRegistry(), r.store.SQLStore, func(string, json.RawMessage) {})
		pending, err := restarted.InterruptedRewinds(ctx)
		if err != nil || len(pending) != 1 || pending[0].Kind != agenthub.CheckpointRewindSafety || pending[0].Status != agenthub.CheckpointPending || pending[0].BeforeTree == "" {
			t.Errorf("a restart during the retry must see one pending rewind with its tree: %+v %v", pending, err)
		}
		forksAtRestart = r.forks.Load()
		return nil
	})
	if _, err := r.rewind(ctx); err != nil {
		t.Fatalf("the rewind itself finishes: %v", err)
	}
	if forksAtRestart != 2 || r.forks.Load() != 2 {
		t.Fatalf("forks at restart %d, in all %d: a restart replays nothing", forksAtRestart, r.forks.Load())
	}
}

// W or V changing while the restore runs does not freeze the old permissions and does
// not bypass a gate: the re-preparation is refused by the ordinary rules, nothing
// executable is launched, and the answer is the truthful partial one.
func TestARetryTheLaunchGateRefusesIsPartialAndLaunchesNothing(t *testing.T) {
	changedServer := func(t *testing.T) func(string, string, string) (harness.Snapshot, error) {
		return func(s, ws, v string) (harness.Snapshot, error) {
			return snapshotFor(t, s, ws, v, func(in *harness.Inputs) {
				in.Workspace = &harness.Profile{Schema: 1, Revision: 2,
					Servers: []harness.Server{{Name: "db", Command: "a-different-command", Enabled: true}}}
			}), nil
		}
	}
	unreadable := func(*testing.T) func(string, string, string) (harness.Snapshot, error) {
		return func(string, string, string) (harness.Snapshot, error) {
			return harness.Snapshot{}, errors.New("the workspace profile could not be read")
		}
	}
	for name, tc := range map[string]struct {
		next func(*testing.T) func(string, string, string) (harness.Snapshot, error)
		want error
	}{
		"a new workspace executable": {changedServer, agenthub.ErrHarnessNewSessionRequired},
		"an unresolvable snapshot":   {unreadable, agenthub.ErrHarnessUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			r, _ := newFilesRig(t)
			opens, closes := r.counts()
			r.store.onRestore = func(int) {
				r.moveS()
				r.harness.set(tc.next(t))
			}
			_, err := r.rewind(context.Background())
			r.assertPartial(err)
			if !errors.Is(err, tc.want) {
				t.Fatalf("want the gate's refusal %v as the cause, got %v", tc.want, err)
			}
			if got := int(r.forks.Load()); got != 1 {
				t.Fatalf("forks = %d: a refused gate launches nothing", got)
			}
			if do, dc := r.endpointDelta(opens, closes); do != 1 || dc != 2 {
				t.Fatalf("endpoint opens=%d closes=%d, want the first staged one and the old one closed (1 and 2)", do, dc)
			}
			if r.svc.ServingTarget(r.session.ID).Active {
				t.Fatal("nothing may be left serving")
			}
		})
	}
}

// An endpoint that cannot be staged on the re-prepared launch leaves nothing behind and
// the first, discarded attempt's resources are all closed.
func TestAnEndpointThatCannotBeStagedOnTheRetryLeavesNothingBehind(t *testing.T) {
	r, _ := newFilesRig(t)
	opens, closes := r.counts()
	boom := errors.New("the endpoint host refused")
	fail := func(n int) error {
		if n >= 2 {
			return boom
		}
		return nil
	}
	r.failOpen.Store(&fail)
	r.store.onRestore = func(int) { r.moveS() }
	_, err := r.rewind(context.Background())
	r.assertPartial(err)
	if !errors.Is(err, boom) {
		t.Fatalf("want the staging failure as the cause, got %v", err)
	}
	if do, dc := r.endpointDelta(opens, closes); do != 1 || dc != 2 {
		t.Fatalf("endpoint opens=%d closes=%d, want 1 and 2", do, dc)
	}
	if closed, deleted, _ := r.probes.fork(1).state(); !closed || !deleted {
		t.Fatal("the abandoned first fork must be closed and deleted")
	}
	if r.svc.ServingTarget(r.session.ID).Active {
		t.Fatal("nothing may be left serving")
	}
}
