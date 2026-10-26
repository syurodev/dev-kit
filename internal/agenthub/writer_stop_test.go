package agenthub_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devkit/internal/acp"
	"devkit/internal/agenthub"
	"devkit/internal/storage"
)

// witnessAdapter wraps the mock so a test controls what each runtime says about
// its own process: whether it can say anything, when it is reaped, and whether
// its jail contains descendants.
type witnessAdapter struct {
	*agenthub.MockAdapter
	mu        sync.Mutex
	supported bool
	contained bool
	openErr   error
	opens     int
	runtimes  []*witnessRuntime
}

type witnessRuntime struct {
	agenthub.SessionRuntime
	supported bool
	contained bool
	exited    chan struct{}
	prompts   atomic.Int32
}

func (r *witnessRuntime) StopWitness() (agenthub.StopWitness, bool) {
	if !r.supported {
		return agenthub.StopWitness{}, false
	}
	return agenthub.StopWitness{Exited: r.exited, Contained: r.contained}, true
}

func (r *witnessRuntime) Prompt(ctx context.Context, p []acp.ContentBlock, meta json.RawMessage) (agenthub.PromptOutcome, error) {
	r.prompts.Add(1)
	return r.SessionRuntime.Prompt(ctx, p, meta)
}

func (a *witnessAdapter) Open(ctx context.Context, spec agenthub.OpenSpec) (agenthub.SessionRuntime, error) {
	a.mu.Lock()
	a.opens++
	err := a.openErr
	supported, contained := a.supported, a.contained
	a.mu.Unlock()
	if err != nil {
		return nil, err
	}
	inner, err := a.MockAdapter.Open(ctx, spec)
	if err != nil {
		return nil, err
	}
	rt := &witnessRuntime{SessionRuntime: inner, supported: supported, contained: contained, exited: make(chan struct{})}
	a.mu.Lock()
	a.runtimes = append(a.runtimes, rt)
	a.mu.Unlock()
	return rt, nil
}

// Load reopens a conversation the way a resume does, so a session that has been
// prompted can be closed and reopened.
func (a *witnessAdapter) Load(ctx context.Context, spec agenthub.OpenSpec, _ string) (agenthub.SessionRuntime, error) {
	return a.Open(ctx, spec)
}

func (a *witnessAdapter) last() *witnessRuntime {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.runtimes[len(a.runtimes)-1]
}

func newWitnessService(t *testing.T, supported, contained bool) (*agenthub.Service, *witnessAdapter) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	adapter := &witnessAdapter{MockAdapter: agenthub.NewMockAdapter(), supported: supported, contained: contained}
	reg := agenthub.NewRegistry()
	reg.Register(adapter)
	return agenthub.NewService(reg, agenthub.NewSQLStore(db), func(string, json.RawMessage) {}), adapter
}

func newMockSession(t *testing.T, svc *agenthub.Service) agenthub.AgentSession {
	t.Helper()
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

func TestWriterStopProofNeedsARuntimeThatCanWitnessItsOwnEnd(t *testing.T) {
	svc, _ := newWitnessService(t, false, false)
	sess := newMockSession(t, svc)
	if err := svc.CloseSession(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	// Closed, idle, gone from the live map: none of it is evidence.
	proof := svc.WriterStopProof(context.Background(), sess.ID)
	if proof.Known || proof.Positive() || proof.Reason != agenthub.StopUnsupported {
		t.Fatalf("proof for a runtime without a witness = %+v", proof)
	}
}

func TestWriterStopProofIsPositiveOnlyAfterTheProcessWasReaped(t *testing.T) {
	svc, adapter := newWitnessService(t, true, true)
	sess := newMockSession(t, svc)
	ctx := context.Background()

	if p := svc.WriterStopProof(ctx, sess.ID); p.Positive() || p.Reason != agenthub.StopLive || !p.Known {
		t.Fatalf("live runtime = %+v", p)
	}
	if err := svc.CloseSession(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	// Close returned, the runtime left the map, and the process has not been seen
	// to exit: the proof waits on the exit and gives up when its context ends.
	short, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if p := svc.WriterStopProof(short, sess.ID); p.Known || p.Positive() || p.Reason != agenthub.StopExitUnobserved {
		t.Fatalf("closed but not reaped = %+v", p)
	}
	close(adapter.last().exited)
	p := svc.WriterStopProof(ctx, sess.ID)
	if !p.Positive() || p.Reason != agenthub.StopOK || p.Epoch == "" || p.EpochStart.IsZero() || p.CheckedAt.Before(p.EpochStart) {
		t.Fatalf("reaped and contained = %+v", p)
	}
}

func TestWriterStopProofWithoutContainmentIsNeverIsolated(t *testing.T) {
	svc, adapter := newWitnessService(t, true, false)
	sess := newMockSession(t, svc)
	if err := svc.CloseSession(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	close(adapter.last().exited)
	p := svc.WriterStopProof(context.Background(), sess.ID)
	if p.Positive() || !p.Known || !p.Stopped || p.Isolated || p.Reason != agenthub.StopNoContainment {
		t.Fatalf("supervisor reaped, descendants not contained = %+v", p)
	}
	// Sticky for the epoch: a later contained runtime does not erase it.
	adapter.mu.Lock()
	adapter.contained = true
	adapter.mu.Unlock()
	if _, err := svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "again"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.CloseSession(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	close(adapter.last().exited)
	if p := svc.WriterStopProof(context.Background(), sess.ID); p.Positive() {
		t.Fatalf("an uncontained runtime was forgotten: %+v", p)
	}
}

// Every incarnation counts: reopening the session must not let the first
// runtime's exit stand in for the second one.
func TestWriterStopProofCoversEveryIncarnation(t *testing.T) {
	svc, adapter := newWitnessService(t, true, true)
	sess := newMockSession(t, svc)
	ctx := context.Background()
	if err := svc.CloseSession(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	first := adapter.last()
	close(first.exited)
	if !svc.WriterStopProof(ctx, sess.ID).Positive() {
		t.Fatal("first incarnation should be provable")
	}
	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "reopen"}); err != nil {
		t.Fatal(err)
	}
	second := adapter.last()
	if second == first {
		t.Fatal("session did not get a new runtime")
	}
	if p := svc.WriterStopProof(ctx, sess.ID); p.Positive() || p.Reason != agenthub.StopLive {
		t.Fatalf("evidence from the first runtime made the second one stopped: %+v", p)
	}
	if err := svc.CloseSession(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	short, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if p := svc.WriterStopProof(short, sess.ID); p.Positive() {
		t.Fatalf("second runtime unreaped but proof positive: %+v", p)
	}
	close(second.exited)
	if !svc.WriterStopProof(ctx, sess.ID).Positive() {
		t.Fatal("both reaped should be provable")
	}
}

// A failed open may have started a process nobody reaped on the record.
func TestWriterStopProofStaysUnknownAfterAFailedOpen(t *testing.T) {
	svc, adapter := newWitnessService(t, true, true)
	sess := newMockSession(t, svc)
	ctx := context.Background()
	if err := svc.CloseSession(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	close(adapter.last().exited)
	adapter.mu.Lock()
	adapter.openErr = errors.New("spawn failed")
	adapter.mu.Unlock()
	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "x"}); err == nil {
		t.Fatal("open failure was not reported")
	}
	if p := svc.WriterStopProof(ctx, sess.ID); p.Known || p.Positive() || p.Reason != agenthub.StopOpenFailed {
		t.Fatalf("failed open = %+v", p)
	}
}

// A new process has no record of what the last one left behind, and the proof
// says so by carrying its own epoch.
func TestWriterStopProofIsScopedToTheProcessEpoch(t *testing.T) {
	a, _ := newWitnessService(t, true, true)
	b, _ := newWitnessService(t, true, true)
	pa, pb := a.WriterStopProof(context.Background(), "s"), b.WriterStopProof(context.Background(), "s")
	if pa.Epoch == "" || pa.Epoch == pb.Epoch {
		t.Fatalf("epochs = %q / %q", pa.Epoch, pb.Epoch)
	}
	if pa.Reason != agenthub.StopOK {
		t.Fatalf("a session never opened in this epoch = %+v", pa)
	}
	if got := a.WriterStopProof(context.Background(), ""); got.Known {
		t.Fatalf("empty session id was provable: %+v", got)
	}
}

// guardSteps lets a test decide the outcome of each successive fence check.
type guardSteps struct {
	mu    sync.Mutex
	calls int
	deny  func(call int) error
	hold  func(call int)
}

func (g *guardSteps) AuthorizeCurrentWriter(context.Context, string) error {
	g.mu.Lock()
	g.calls++
	n := g.calls
	deny, hold := g.deny, g.hold
	g.mu.Unlock()
	if hold != nil {
		hold(n)
	}
	if deny != nil {
		return deny(n)
	}
	return nil
}

// A pause that wins after the first check still stops the prompt: the fence is
// asked again right before the prompt leaves.
func TestInvokeRechecksTheFenceBeforeThePromptLeaves(t *testing.T) {
	svc, adapter := newWitnessService(t, true, true)
	sess := newMockSession(t, svc)
	paused := errors.New("group paused")
	var flipped atomic.Bool
	svc.SetManagedWriterGuard(&guardSteps{deny: func(int) error {
		if flipped.Load() {
			return paused
		}
		return nil
	}, hold: func(call int) {
		// Between the admission check (2) and the last one (3), the pause lands.
		if call == 3 {
			flipped.Store(true)
		}
	}})
	_, err := svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "must not be sent"})
	if !errors.Is(err, agenthub.ErrManagedWriterBlocked) || !errors.Is(err, paused) {
		t.Fatalf("Invoke = %v", err)
	}
	if n := adapter.last().prompts.Load(); n != 0 {
		t.Fatalf("a prompt reached the runtime after the pause: %d", n)
	}
}

// Pause wins before a closed session is reopened: no runtime is started on a
// check that passed earlier.
func TestInvokeAdmissionStopsARuntimeStartAfterPause(t *testing.T) {
	svc, adapter := newWitnessService(t, true, true)
	sess := newMockSession(t, svc)
	ctx := context.Background()
	if err := svc.CloseSession(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	opensBefore := func() int { adapter.mu.Lock(); defer adapter.mu.Unlock(); return adapter.opens }()
	var paused atomic.Bool
	svc.SetManagedWriterGuard(&guardSteps{deny: func(int) error {
		if paused.Load() {
			return errors.New("paused")
		}
		return nil
	}, hold: func(call int) {
		if call == 2 { // the admission check, after the top-of-Invoke one passed
			paused.Store(true)
		}
	}})
	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "x"}); !errors.Is(err, agenthub.ErrManagedWriterBlocked) {
		t.Fatalf("Invoke = %v", err)
	}
	adapter.mu.Lock()
	opens := adapter.opens
	adapter.mu.Unlock()
	if opens != opensBefore {
		t.Fatalf("a runtime was started after the pause won (%d -> %d opens)", opensBefore, opens)
	}
}

// DrainWriterAdmission returns only once an admission that passed the earlier
// check has either sent or been refused, and it never waits on the turn itself.
func TestDrainWriterAdmissionWaitsForAnAdmissionInFlight(t *testing.T) {
	svc, _ := newWitnessService(t, true, true)
	sess := newMockSession(t, svc)
	reached, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	svc.SetManagedWriterGuard(&guardSteps{hold: func(call int) {
		if call == 2 { // inside the admission
			once.Do(func() { close(reached) })
			<-release
		}
	}})
	done := make(chan error, 1)
	go func() {
		_, err := svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "x"})
		done <- err
	}()
	<-reached

	short, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := svc.DrainWriterAdmission(short, sess.ID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("drain returned while an admission was in flight: %v", err)
	}
	drained := make(chan error, 1)
	go func() { drained <- svc.DrainWriterAdmission(context.Background(), sess.ID) }()
	close(release)
	if err := <-drained; err != nil {
		t.Fatalf("drain = %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Invoke = %v", err)
	}
	if err := svc.DrainWriterAdmission(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.DrainWriterAdmission(context.Background(), "other-session"); err != nil {
		t.Fatalf("drain of an idle session = %v", err)
	}
}

// The seal outranks a start that passed an earlier check. An Invoke that
// registered before the seal and only then reaches the point of starting a
// runtime is refused there, so a positive stop proof cannot be overtaken.
func TestSealedAdmissionRefusesAStartThatRegisteredBeforeTheSeal(t *testing.T) {
	svc, adapter := newWitnessService(t, true, true)
	sess := newMockSession(t, svc)
	ctx := context.Background()
	if err := svc.CloseSession(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	close(adapter.last().exited)
	opensBefore := func() int { adapter.mu.Lock(); defer adapter.mu.Unlock(); return adapter.opens }

	reached, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	svc.SetManagedWriterGuard(&guardSteps{hold: func(call int) {
		if call == 2 { // Invoke is registered and has passed nothing past the barrier yet
			once.Do(func() { close(reached) })
			<-release
		}
	}})
	invoked := make(chan error, 1)
	go func() {
		_, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "reopen"})
		invoked <- err
	}()
	<-reached

	type sealed struct {
		end func()
		err error
	}
	sealing := make(chan sealed, 1)
	go func() {
		end, err := svc.SealWriterAdmission(ctx, sess.ID)
		sealing <- sealed{end, err}
	}()
	// The seal is waiting for the registered Invoke; it must not return yet.
	select {
	case <-sealing:
		t.Fatal("seal returned while an admission was still registered")
	case <-time.After(40 * time.Millisecond):
	}
	close(release)
	if err := <-invoked; !errors.Is(err, agenthub.ErrManagedWriterBlocked) {
		t.Fatalf("Invoke after the seal = %v, want managed writer blocked", err)
	}
	s := <-sealing
	if s.err != nil {
		t.Fatal(s.err)
	}
	if got := opensBefore(); got != 1 {
		t.Fatalf("a runtime was started under the seal (%d opens)", got)
	}
	if p := svc.WriterStopProof(ctx, sess.ID); !p.Positive() {
		t.Fatalf("proof under the seal = %+v", p)
	}
	// Every other way in is refused too, with the guard allowing it.
	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "x"}); !errors.Is(err, agenthub.ErrManagedWriterBlocked) {
		t.Fatalf("Invoke under the seal = %v", err)
	}
	if err := svc.ResumeSession(ctx, sess.ID, false); !errors.Is(err, agenthub.ErrManagedWriterBlocked) {
		t.Fatalf("ResumeSession under the seal = %v", err)
	}
	if _, err := svc.Steer(ctx, sess.ID, "x"); !errors.Is(err, agenthub.ErrManagedWriterBlocked) {
		t.Fatalf("Steer under the seal = %v", err)
	}
	if got := opensBefore(); got != 1 {
		t.Fatalf("a runtime was started under the seal (%d opens)", got)
	}
	s.end()
	s.end() // idempotent
	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "after"}); err != nil {
		t.Fatalf("Invoke after the seal ended = %v", err)
	}
}

func TestSealWriterAdmissionGivesUpWithItsContext(t *testing.T) {
	svc, _ := newWitnessService(t, true, true)
	sess := newMockSession(t, svc)
	reached, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	svc.SetManagedWriterGuard(&guardSteps{hold: func(call int) {
		if call == 2 {
			once.Do(func() { close(reached) })
			<-release
		}
	}})
	done := make(chan error, 1)
	go func() {
		_, err := svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "x"})
		done <- err
	}()
	<-reached
	short, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if end, err := svc.SealWriterAdmission(short, sess.ID); err == nil {
		end()
		t.Fatal("seal returned although an admission never left")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Invoke after a failed seal = %v (a failed seal must not stay in force)", err)
	}
}

// The closed runtimes of a session are not kept alive by the proof table, even
// when nobody ever asks for a proof.
func TestClosedRuntimesAreNotRetainedByTheProofTable(t *testing.T) {
	svc, adapter := newWitnessService(t, true, true)
	sess := newMockSession(t, svc)
	ctx := context.Background()
	collected := make(chan struct{}, 64)
	for i := 0; i < 20; i++ {
		rt := adapter.last()
		runtime.SetFinalizer(rt, func(*witnessRuntime) { collected <- struct{}{} })
		if err := svc.CloseSession(ctx, sess.ID); err != nil {
			t.Fatal(err)
		}
		close(rt.exited)
		adapter.mu.Lock()
		adapter.runtimes = adapter.runtimes[:0] // the test's own references
		adapter.mu.Unlock()
		if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "reopen"}); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(collected) < 15 && time.Now().Before(deadline) {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}
	if len(collected) < 15 {
		t.Fatalf("only %d of 20 closed runtimes were collected: the proof table retains them", len(collected))
	}
	if n := svc.TrackedRuntimeCount(sess.ID); n > 1 {
		t.Fatalf("tracked runtimes = %d, want only the live one", n)
	}
}
