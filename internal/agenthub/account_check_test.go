package agenthub_test

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/identity"
	"devkit/internal/preferences"
)

func windows(pct float64) []agenthub.LimitWindow {
	return []agenthub.LimitWindow{{Label: "Weekly", UsedPercent: pct, ResetsAt: time.Now().Add(24 * time.Hour).Unix(), Status: "ok"}}
}

func checkRig(t *testing.T, answer func(context.Context, agenthub.OpenSpec) ([]agenthub.LimitWindow, error)) (*accountRig, *usageProbe) {
	t.Helper()
	probe := &usageProbe{answer: answer}
	rig := newAccountRigWith(t, preferences.RefusalNone, rigOptions{probe: probe})
	return rig, probe
}

func answerWith(pct float64) func(context.Context, agenthub.OpenSpec) ([]agenthub.LimitWindow, error) {
	return func(context.Context, agenthub.OpenSpec) ([]agenthub.LimitWindow, error) { return windows(pct), nil }
}

func TestRefreshAccountUsageStoresTheCheckedReadingWholesale(t *testing.T) {
	rig, probe := checkRig(t, answerWith(42))
	ctx := uiCtx()
	if err := rig.book.SaveUsage(context.Background(), "work", []agenthub.LimitWindow{{Label: "5-hour", UsedPercent: 90}}, "observed", time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	changed := 0
	rig.svc.OnAccountsChanged = func() { changed++ }
	before := time.Now()
	got, err := rig.svc.RefreshAccountUsage(ctx, "work")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Usage) != 1 || got.Usage[0].UsedPercent != 42 || got.UsageSource != "checked" {
		t.Fatalf("account = %+v, want exactly the checked window (the observed 5-hour one is not in the new reading)", got)
	}
	if got.ObservedAt.Before(before.Add(-time.Second)) {
		t.Errorf("observed_at = %v, want the check time (after %v)", got.ObservedAt, before)
	}
	if probe.calls.Load() != 1 || changed != 1 {
		t.Errorf("checks = %d, events = %d, want 1 and 1", probe.calls.Load(), changed)
	}
	if home := probe.specs[0].AccountHome; home != rig.home {
		t.Errorf("the probe ran with home %q, want the account's %q", home, rig.home)
	}
}

func TestACheckedReadingLetsTheNextIdenticalObservedOneThrough(t *testing.T) {
	rig, _ := checkRig(t, answerWith(42))
	rig.svc.SetUsageSeenForTest("work")
	if _, err := rig.svc.RefreshAccountUsage(uiCtx(), "work"); err != nil {
		t.Fatal(err)
	}
	if rig.svc.UsageSeenKnownForTest("work") {
		t.Error("a checked reading replaced what was stored: the last observed one must be forgotten, or an identical later one is skipped")
	}
}

func TestRefreshAccountUsageSaysWhenUsageCannotBeRead(t *testing.T) {
	rig, _ := checkRig(t, func(context.Context, agenthub.OpenSpec) ([]agenthub.LimitWindow, error) {
		return nil, agenthub.ErrUsageNotCheckable
	})
	if err := rig.book.SaveUsage(context.Background(), "work", windows(30), "observed", time.Now()); err != nil {
		t.Fatal(err)
	}
	changed := 0
	rig.svc.OnAccountsChanged = func() { changed++ }
	if _, err := rig.svc.RefreshAccountUsage(uiCtx(), "work"); !errors.Is(err, agenthub.ErrUsageNotCheckable) {
		t.Fatalf("error = %v, want ErrUsageNotCheckable", err)
	}
	got, _ := rig.book.Get(context.Background(), "work")
	if len(got.Usage) != 1 || got.Usage[0].UsedPercent != 30 || got.UsageSource != "observed" {
		t.Errorf("a failed check must keep the old reading: %+v", got)
	}
	if changed != 0 {
		t.Errorf("a failed check changed nothing, but %d events fired", changed)
	}
}

func TestOnlyTwoChecksRunAtOnceAndAWaitingOneThatGivesUpHoldsNobodyBack(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 4)
	rig, probe := checkRig(t, func(ctx context.Context, _ agenthub.OpenSpec) ([]agenthub.LimitWindow, error) {
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return windows(10), nil
	})
	if _, err := rig.book.List(context.Background(), agenthub.AgentTypeMock); err != nil { // creates the built-in row
		t.Fatal(err)
	}
	if err := rig.book.Put(context.Background(), agenthub.AccountRecord{ID: "other", Vendor: agenthub.AgentTypeMock, Label: "Other", HomeDir: t.TempDir(), SignedIn: true}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, id := range []string{"work", agenthub.BuiltinAccountID(agenthub.AgentTypeMock)} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := rig.svc.RefreshAccountUsage(uiCtx(), id); err != nil {
				t.Error(err)
			}
		}()
	}
	<-started
	<-started
	// Both slots are taken: a third account waits for one, and this caller gives up.
	ctx, cancel := context.WithTimeout(uiCtx(), 300*time.Millisecond)
	defer cancel()
	if _, err := rig.svc.RefreshAccountUsage(ctx, "other"); err == nil {
		t.Error("a third check ran while two others held the slots")
	}
	if probe.calls.Load() != 2 {
		t.Errorf("checks = %d, want 2 (the cap)", probe.calls.Load())
	}
	close(release)
	wg.Wait()
	// Nothing was asked of the vendor for "other": the floor must not hold it back.
	if _, err := rig.svc.RefreshAccountUsage(uiCtx(), "other"); err != nil || probe.calls.Load() != 3 {
		t.Fatalf("a refresh after the one that gave up: checks = %d, err = %v, want 3", probe.calls.Load(), err)
	}
}

func TestRefreshAccountUsageIsForTheWindowOnly(t *testing.T) {
	rig, probe := checkRig(t, answerWith(1))
	for name, ctx := range map[string]context.Context{
		"an agent":     agentCtx("11111111-1111-1111-1111-111111111111"),
		"the terminal": identity.WithCaller(context.Background(), identity.UserTerminal()),
	} {
		if _, err := rig.svc.RefreshAccountUsage(ctx, "work"); !errors.Is(err, agenthub.ErrPermissionDenied) {
			t.Fatalf("%s: %v, want ErrPermissionDenied", name, err)
		}
	}
	if probe.calls.Load() != 0 {
		t.Error("an agent must not make DevKit start a vendor process")
	}
}

func TestTwoRefreshesOfOneAccountAreOneCheck(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	joined := make(chan struct{}, 2)
	rig, probe := checkRig(t, func(ctx context.Context, _ agenthub.OpenSpec) ([]agenthub.LimitWindow, error) {
		started <- struct{}{}
		<-release
		return windows(10), nil
	})
	rig.svc.SetCheckWaitHookForTest(func(string) { joined <- struct{}{} })
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := rig.svc.RefreshAccountUsage(uiCtx(), "work"); err != nil {
				t.Error(err)
			}
		}()
	}
	<-started
	<-joined // the other caller reached the flight and is waiting on it
	close(release)
	wg.Wait()
	if probe.calls.Load() != 1 {
		t.Fatalf("checks = %d, want 1 (single flight per account)", probe.calls.Load())
	}
}

func TestARefreshInsideTheFloorAsksNothing(t *testing.T) {
	rig, probe := checkRig(t, answerWith(10))
	now := time.Unix(1_800_000_000, 0)
	rig.svc.SetCheckClockForTest(func() time.Time { return now })
	if _, err := rig.svc.RefreshAccountUsage(uiCtx(), "work"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(5 * time.Second)
	if _, err := rig.svc.RefreshAccountUsage(uiCtx(), "work"); err != nil {
		t.Fatal(err)
	}
	if probe.calls.Load() != 1 {
		t.Fatalf("checks = %d, want 1: a second press within 10 s must not start another process", probe.calls.Load())
	}
	now = now.Add(11 * time.Second)
	if _, err := rig.svc.RefreshAccountUsage(uiCtx(), "work"); err != nil || probe.calls.Load() != 2 {
		t.Fatalf("after the floor: checks = %d, err = %v, want 2", probe.calls.Load(), err)
	}
}

func markShared(t *testing.T, rig *accountRig, id string) {
	t.Helper()
	acct, err := rig.book.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	acct.Shared = true
	if err := rig.book.Put(context.Background(), acct); err != nil {
		t.Fatal(err)
	}
}

func TestATurnChecksASharedAccountWhoseReadingIsOldAndActsOnTheAnswer(t *testing.T) {
	rig, probe := checkRig(t, answerWith(95))
	if _, err := rig.book.List(context.Background(), agenthub.AgentTypeMock); err != nil {
		t.Fatal(err)
	}
	markShared(t, rig, agenthub.BuiltinAccountID(agenthub.AgentTypeMock)) // a team login: never read, so stale
	sess := startSession(t, rig)
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if probe.calls.Load() == 0 {
		t.Fatal("the shared account's reading is older than the policy allows: it must be checked before the pick")
	}
	if got := rowAccount(t, rig, sess.ID); got != "work" {
		t.Errorf("the checked 95%% must move the turn to work; account = %q", got)
	}
}

func TestATurnDoesNotCheckAnUnsharedOrFreshOrSignedOutAccount(t *testing.T) {
	rig, probe := checkRig(t, answerWith(95))
	sess := startSession(t, rig)
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if probe.calls.Load() != 0 {
		t.Fatalf("an account nobody else uses is read from its own sessions: checks = %d", probe.calls.Load())
	}
	markShared(t, rig, agenthub.BuiltinAccountID(agenthub.AgentTypeMock))
	rig.fillBuiltin(t, 10) // a reading made just now
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "again"}); err != nil {
		t.Fatal(err)
	}
	if probe.calls.Load() != 0 {
		t.Fatalf("a fresh reading needs no check: checks = %d", probe.calls.Load())
	}
}

func TestATurnDoesNotCheckASignedOutSharedAccount(t *testing.T) {
	rig, probe := checkRig(t, answerWith(95))
	sess := startSession(t, rig)
	if _, err := rig.book.List(context.Background(), agenthub.AgentTypeMock); err != nil { // creates the built-in row
		t.Fatal(err)
	}
	markShared(t, rig, agenthub.BuiltinAccountID(agenthub.AgentTypeMock))
	if err := rig.book.SetSignedIn(context.Background(), agenthub.BuiltinAccountID(agenthub.AgentTypeMock), false); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if probe.calls.Load() != 0 {
		t.Fatalf("a signed-out account is not probed before a turn: checks = %d", probe.calls.Load())
	}
}

func TestATurnDoesNotWaitLongForACheck(t *testing.T) {
	restore := agenthub.SetTurnCheckTimeoutForTest(50 * time.Millisecond)
	defer restore()
	rig, _ := checkRig(t, func(ctx context.Context, _ agenthub.OpenSpec) ([]agenthub.LimitWindow, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if _, err := rig.book.List(context.Background(), agenthub.AgentTypeMock); err != nil {
		t.Fatal(err)
	}
	markShared(t, rig, agenthub.BuiltinAccountID(agenthub.AgentTypeMock))
	sess := startSession(t, rig)
	done := make(chan error, 1)
	go func() {
		_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a check that timed out must not fail the turn: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the turn waited for a check that never answers")
	}
	got, _ := rig.book.Get(context.Background(), agenthub.BuiltinAccountID(agenthub.AgentTypeMock))
	if len(got.Usage) != 0 {
		t.Errorf("a check that timed out stored %+v", got.Usage)
	}
}

type fakeRateRuntime struct {
	agenthub.SessionRuntime
	windows []agenthub.LimitWindow
}

func (f fakeRateRuntime) ReadRateLimits(context.Context) ([]agenthub.LimitWindow, error) {
	return f.windows, nil
}

func TestARunningAccountIsReadThroughItsOwnSessionNotASecondProcess(t *testing.T) {
	rig, probe := checkRig(t, answerWith(1))
	rig.svc.SetRuntimeForTest("sess-running", "work", fakeRateRuntime{windows: windows(77)})
	got, err := rig.svc.RefreshAccountUsage(uiCtx(), "work")
	if err != nil {
		t.Fatal(err)
	}
	if probe.calls.Load() != 0 {
		t.Error("a session already runs on this home: a second process could refresh its tokens at once")
	}
	if len(got.Usage) != 1 || got.Usage[0].UsedPercent != 77 {
		t.Errorf("usage = %+v, want the running session's reading", got.Usage)
	}
}

// claudeProbe is a vendor typed claude (which has a shared conversation store) whose
// usage check records the spec it was handed.
type claudeProbe struct {
	*agenthub.MockAdapter
	probe *usageProbe
}

func (claudeProbe) Type() agenthub.AgentType { return agenthub.AgentTypeClaude }

func (c claudeProbe) CheckUsage(ctx context.Context, spec agenthub.OpenSpec) ([]agenthub.LimitWindow, error) {
	return c.probe.check(ctx, spec)
}

func TestTheProbeRunsUnderTheJailASessionWouldGetIt(t *testing.T) {
	rig := newAccountRigWith(t, preferences.RefusalNone, rigOptions{})
	probe := &usageProbe{answer: answerWith(20)}
	rig.reg.Register(claudeProbe{agenthub.NewMockAdapter(), probe})
	home := filepath.Join(t.TempDir(), "claude-work")
	sharedFrom := t.TempDir()
	protected := []string{filepath.Join(t.TempDir(), "agent-accounts"), filepath.Join(t.TempDir(), "vault.db")}
	rig.svc.AccountSharedHome = func(agenthub.AgentType) string { return sharedFrom }
	rig.svc.ProtectedPaths = func() []string { return protected }
	rig.svc.SandboxExtras = func() preferences.AgentSandboxSettings { return preferences.AgentSandboxSettings{} } // home shared by default
	if err := rig.book.Put(context.Background(), agenthub.AccountRecord{ID: "claude-work", Vendor: agenthub.AgentTypeClaude, Label: "Work", HomeDir: home, SignedIn: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.book.List(context.Background(), agenthub.AgentTypeClaude); err != nil { // creates the built-in row
		t.Fatal(err)
	}
	if _, err := rig.svc.RefreshAccountUsage(uiCtx(), "claude-work"); err != nil {
		t.Fatal(err)
	}
	spec := probe.specs[0]
	if spec.AccountHome != home {
		t.Errorf("AccountHome = %q, want %q", spec.AccountHome, home)
	}
	if want := agenthub.SharedStorePaths(agenthub.AgentTypeClaude, sharedFrom); len(want) == 0 || !slices.Equal(spec.AccountSharedPaths, want) {
		t.Errorf("AccountSharedPaths = %v, want %v", spec.AccountSharedPaths, want)
	}
	for _, p := range protected {
		if !slices.Contains(spec.DenyReadPaths, p) {
			t.Errorf("DenyReadPaths = %v, missing %q: an account's CLI must not read other accounts' homes", spec.DenyReadPaths, p)
		}
	}
	if spec.PrivateHome == nil || *spec.PrivateHome {
		t.Errorf("PrivateHome = %v, want false (the person shares the home)", spec.PrivateHome)
	}

	// The built-in account: no home, still hidden data directory; and a private home.
	off := false
	rig.svc.SandboxExtras = func() preferences.AgentSandboxSettings { return preferences.AgentSandboxSettings{ShareHome: &off} }
	rig.svc.SetCheckClockForTest(func() time.Time { return time.Now().Add(time.Hour) }) // past the floor
	if _, err := rig.svc.RefreshAccountUsage(uiCtx(), agenthub.BuiltinAccountID(agenthub.AgentTypeClaude)); err != nil {
		t.Fatal(err)
	}
	spec = probe.specs[1]
	if spec.AccountHome != "" || len(spec.AccountSharedPaths) != 0 {
		t.Errorf("the built-in account's probe carries home %q / shared %v", spec.AccountHome, spec.AccountSharedPaths)
	}
	if !slices.Equal(spec.DenyReadPaths, protected) {
		t.Errorf("DenyReadPaths = %v, want %v", spec.DenyReadPaths, protected)
	}
	if spec.PrivateHome != nil {
		t.Errorf("PrivateHome = %v, want nil (private home is the adapter's default)", *spec.PrivateHome)
	}
}

// blockedRig is a rig with "work" and the built-in account, and two probes held (on
// "work" and the built-in account) so both slots are taken. release lets them go.
type blockedRig struct {
	*accountRig
	probe   *usageProbe
	release chan struct{}
	wg      *sync.WaitGroup
}

func slotsTakenRig(t *testing.T) *blockedRig {
	t.Helper()
	release := make(chan struct{})
	started := make(chan struct{}, 8)
	rig, probe := checkRig(t, func(ctx context.Context, _ agenthub.OpenSpec) ([]agenthub.LimitWindow, error) {
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return windows(10), nil
	})
	if _, err := rig.book.List(context.Background(), agenthub.AgentTypeMock); err != nil { // creates the built-in row
		t.Fatal(err)
	}
	if err := rig.book.Put(context.Background(), agenthub.AccountRecord{ID: "other", Vendor: agenthub.AgentTypeMock, Label: "Other", HomeDir: t.TempDir(), SignedIn: true}); err != nil {
		t.Fatal(err)
	}
	b := &blockedRig{accountRig: rig, probe: probe, release: release, wg: new(sync.WaitGroup)}
	for _, id := range []string{"work", agenthub.BuiltinAccountID(agenthub.AgentTypeMock)} {
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			if _, err := rig.svc.RefreshAccountUsage(uiCtx(), id); err != nil {
				t.Error(err)
			}
		}()
	}
	<-started
	<-started
	return b
}

func TestAJoinerWithALiveContextIsNotHandedTheFirstCallersCancellation(t *testing.T) {
	b := slotsTakenRig(t)
	flightUp := make(chan string, 8)
	joined := make(chan struct{}, 8)
	b.svc.SetCheckFlightHookForTest(func(id string) { flightUp <- id })
	b.svc.SetCheckWaitHookForTest(func(string) { joined <- struct{}{} })

	// The first caller registers its flight and waits for a slot; it then gives up.
	ctxA, cancelA := context.WithCancel(uiCtx())
	errA := make(chan error, 1)
	go func() {
		_, err := b.svc.RefreshAccountUsage(ctxA, "other")
		errA <- err
	}()
	<-flightUp
	type result struct {
		acct agenthub.AccountRecord
		err  error
	}
	resB := make(chan result, 1)
	go func() {
		acct, err := b.svc.RefreshAccountUsage(uiCtx(), "other")
		resB <- result{acct, err}
	}()
	<-joined // B waits on A's flight
	cancelA()
	if err := <-errA; !errors.Is(err, context.Canceled) {
		t.Fatalf("the first caller's error = %v, want its own cancellation", err)
	}
	// B, whose own context is live, does its own check once a slot frees.
	<-flightUp
	close(b.release)
	got := <-resB
	if got.err != nil {
		t.Fatalf("the joiner was handed another caller's error: %v", got.err)
	}
	if len(got.acct.Usage) != 1 || got.acct.UsageSource != "checked" {
		t.Errorf("the joiner's account = %+v, want a real check result", got.acct)
	}
	b.wg.Wait()
	if b.probe.calls.Load() != 3 {
		t.Errorf("checks = %d, want 3 (two holders and the joiner's own)", b.probe.calls.Load())
	}
}

func TestAJoinerWithALiveContextIsNotHandedAFirstCallersDeadline(t *testing.T) {
	started := make(chan struct{}, 2)
	expire := make(chan struct{})
	rig, probe := checkRig(t, func(context.Context, agenthub.OpenSpec) ([]agenthub.LimitWindow, error) {
		started <- struct{}{}
		<-expire
		return nil, context.DeadlineExceeded // the first caller's short box ran out mid-probe
	})
	joined := make(chan struct{}, 2)
	rig.svc.SetCheckWaitHookForTest(func(string) { joined <- struct{}{} })
	errA := make(chan error, 1)
	go func() {
		_, err := rig.svc.RefreshAccountUsage(uiCtx(), "work")
		errA <- err
	}()
	<-started
	errB := make(chan error, 1)
	go func() {
		_, err := rig.svc.RefreshAccountUsage(uiCtx(), "work")
		errB <- err
	}()
	<-joined
	close(expire)
	if err := <-errA; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the first caller's error = %v, want its deadline", err)
	}
	// The vendor was asked a moment ago, so the floor answers the joiner: nothing
	// is asked again and nothing is reported as its own failure.
	if err := <-errB; err != nil {
		t.Fatalf("the joiner got %v, want no error (the floor applies again)", err)
	}
	if probe.calls.Load() != 1 {
		t.Errorf("checks = %d, want 1", probe.calls.Load())
	}
}

func TestAJoinerWhoseOwnContextEndedGetsItsOwnError(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	rig, _ := checkRig(t, func(context.Context, agenthub.OpenSpec) ([]agenthub.LimitWindow, error) {
		started <- struct{}{}
		<-release
		return windows(1), nil
	})
	joined := make(chan struct{}, 1)
	rig.svc.SetCheckWaitHookForTest(func(string) { joined <- struct{}{} })
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := rig.svc.RefreshAccountUsage(uiCtx(), "work"); err != nil {
			t.Error(err)
		}
	}()
	<-started
	ctx, cancel := context.WithCancel(uiCtx())
	errB := make(chan error, 1)
	go func() {
		_, err := rig.svc.RefreshAccountUsage(ctx, "work")
		errB <- err
	}()
	<-joined
	cancel()
	if err := <-errB; !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want the joiner's own cancellation", err)
	}
	close(release)
	<-done
}

func TestAWedgedVendorCallIsCutAtTheCheckTimeoutAndTheNextRefreshStartsFresh(t *testing.T) {
	restore := agenthub.SetUsageCheckTimeoutForTest(50 * time.Millisecond)
	defer restore()
	var calls int
	rig, probe := checkRig(t, func(ctx context.Context, _ agenthub.OpenSpec) ([]agenthub.LimitWindow, error) {
		calls++
		if calls == 1 {
			<-ctx.Done() // wedged: only the context ends it
			return nil, ctx.Err()
		}
		return windows(33), nil
	})
	now := time.Unix(1_800_000_000, 0)
	rig.svc.SetCheckClockForTest(func() time.Time { return now })
	done := make(chan error, 1)
	go func() {
		_, err := rig.svc.RefreshAccountUsage(uiCtx(), "work") // the caller's context has no deadline
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error = %v, want the check's timeout", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a wedged vendor call held the refresh for ever")
	}
	got, _ := rig.book.Get(context.Background(), "work")
	if len(got.Usage) != 0 {
		t.Errorf("a timed-out check stored %+v", got.Usage)
	}
	now = now.Add(manualGapForTest)
	acct, err := rig.svc.RefreshAccountUsage(uiCtx(), "work")
	if err != nil {
		t.Fatalf("the refresh after the floor: %v (a dead flight must not be joined)", err)
	}
	if probe.calls.Load() != 2 || len(acct.Usage) != 1 || acct.Usage[0].UsedPercent != 33 {
		t.Errorf("checks = %d, usage = %+v, want a fresh second check", probe.calls.Load(), acct.Usage)
	}
}

const manualGapForTest = 11 * time.Second

func TestAPanickingVendorCallIsAnErrorToTheOnesWaitingOnIt(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	rig, _ := checkRig(t, func(context.Context, agenthub.OpenSpec) ([]agenthub.LimitWindow, error) {
		started <- struct{}{}
		<-release
		panic("vendor bug")
	})
	joined := make(chan struct{}, 1)
	rig.svc.SetCheckWaitHookForTest(func(string) { joined <- struct{}{} })
	panicked := make(chan any, 1)
	go func() {
		defer func() { panicked <- recover() }()
		_, _ = rig.svc.RefreshAccountUsage(uiCtx(), "work")
	}()
	<-started
	errB := make(chan error, 1)
	go func() {
		_, err := rig.svc.RefreshAccountUsage(uiCtx(), "work")
		errB <- err
	}()
	<-joined
	close(release)
	if p := <-panicked; p == nil {
		t.Error("the panic must propagate, not be swallowed")
	}
	if err := <-errB; err == nil {
		t.Fatal("a check that panicked was handed to its joiner as a success")
	}
}

func TestACancelledContextNeverStartsACheckAndLeavesTheFloorAlone(t *testing.T) {
	rig, probe := checkRig(t, answerWith(10))
	now := time.Unix(1_800_000_000, 0)
	rig.svc.SetCheckClockForTest(func() time.Time { return now })
	for i := 0; i < 30; i++ { // a free slot and a dead context are both ready: it must never pick the slot
		ctx, cancel := context.WithCancel(uiCtx())
		cancel()
		asked, err := rig.svc.CheckUsageForTest(ctx, "work", time.Hour)
		if !errors.Is(err, context.Canceled) || asked {
			t.Fatalf("round %d: asked = %v, err = %v, want not asked and Canceled", i, asked, err)
		}
	}
	if probe.calls.Load() != 0 {
		t.Fatalf("checks = %d, want 0 for a context that was already done", probe.calls.Load())
	}
	if asked, err := rig.svc.CheckUsageForTest(uiCtx(), "work", time.Hour); err != nil || !asked {
		t.Fatalf("the next check: asked = %v, err = %v; the floor must not have been burned", asked, err)
	}
}

func TestACheckThatAnswersAtTheEndOfItsBoxStillSavesTheAnswer(t *testing.T) {
	ctx, cancel := context.WithCancel(uiCtx())
	// The vendor answers; the caller's context ends in the same instant.
	rig2, _ := checkRig(t, func(context.Context, agenthub.OpenSpec) ([]agenthub.LimitWindow, error) {
		cancel()
		return windows(61), nil
	})
	asked, err := rig2.svc.CheckUsageForTest(ctx, "work", 0)
	if err != nil || !asked {
		t.Fatalf("asked = %v, err = %v, want the answer to be kept", asked, err)
	}
	got, _ := rig2.book.Get(context.Background(), "work")
	if len(got.Usage) != 1 || got.Usage[0].UsedPercent != 61 {
		t.Errorf("stored %+v, want the 61%% reading even though the caller's context ended", got.Usage)
	}
}

func TestAnEmptyAnswerIsNotCheckableAndKeepsTheGoodReading(t *testing.T) {
	for name, answer := range map[string][]agenthub.LimitWindow{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			rig, _ := checkRig(t, func(context.Context, agenthub.OpenSpec) ([]agenthub.LimitWindow, error) { return answer, nil })
			if err := rig.book.SaveUsage(context.Background(), "work", windows(30), "observed", time.Now()); err != nil {
				t.Fatal(err)
			}
			changed := 0
			rig.svc.OnAccountsChanged = func() { changed++ }
			if _, err := rig.svc.RefreshAccountUsage(uiCtx(), "work"); !errors.Is(err, agenthub.ErrUsageNotCheckable) {
				t.Fatalf("error = %v, want ErrUsageNotCheckable", err)
			}
			got, _ := rig.book.Get(context.Background(), "work")
			if len(got.Usage) != 1 || got.Usage[0].UsedPercent != 30 || got.UsageSource != "observed" || changed != 0 {
				t.Errorf("a stub answer wiped the reading: %+v (events %d)", got, changed)
			}
		})
	}
}

func TestRemovingAnAccountForgetsItsCheckFloor(t *testing.T) {
	rig, probe := checkRig(t, answerWith(10))
	wireManage(rig.svc, t.TempDir(), t.TempDir())
	now := time.Unix(1_800_000_000, 0)
	rig.svc.SetCheckClockForTest(func() time.Time { return now })
	ctx := uiCtx()
	acct, err := rig.svc.AddAccount(ctx, agenthub.AgentTypeMock, "Gone")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rig.svc.RefreshAccountUsage(ctx, acct.ID); err != nil || probe.calls.Load() != 1 {
		t.Fatalf("first check: checks = %d, err = %v", probe.calls.Load(), err)
	}
	if err := rig.svc.RemoveAccount(ctx, acct.ID); err != nil {
		t.Fatal(err)
	}
	again, err := rig.svc.AddAccount(ctx, agenthub.AgentTypeMock, "Gone")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != acct.ID {
		t.Fatalf("the re-added account has id %q, want the same %q", again.ID, acct.ID)
	}
	if _, err := rig.svc.RefreshAccountUsage(ctx, again.ID); err != nil || probe.calls.Load() != 2 {
		t.Fatalf("a re-added account was held to its predecessor's floor: checks = %d, err = %v", probe.calls.Load(), err)
	}
}

func TestATurnChecksTheTargetWhenItIsTheOneWithAnOldSharedReading(t *testing.T) {
	rig, probe := checkRig(t, answerWith(10))
	rig.fillBuiltin(t, 95) // the session's own account: fresh, not shared, over the threshold
	markShared(t, rig, "work")
	sess := startSession(t, rig)
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if probe.calls.Load() != 1 || probe.specs[0].AccountHome != rig.home {
		t.Fatalf("checks = %d, specs = %+v, want exactly the target (work) checked", probe.calls.Load(), probe.specs)
	}
	if got := rowAccount(t, rig, sess.ID); got != "work" {
		t.Errorf("account = %q, want the turn moved to work", got)
	}
}

func TestATurnDoesNotProbeWhenThereIsNoOtherAccountToSwitchTo(t *testing.T) {
	probe := &usageProbe{answer: answerWith(95)}
	rig := newAccountRigWith(t, preferences.RefusalNone, rigOptions{noWork: true, probe: probe})
	if _, err := rig.book.List(context.Background(), agenthub.AgentTypeMock); err != nil {
		t.Fatal(err)
	}
	markShared(t, rig, agenthub.BuiltinAccountID(agenthub.AgentTypeMock))
	sess := startSession(t, rig)
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if n := probe.calls.Load(); n != 0 {
		t.Fatalf("checks = %d: with one account there is nothing to switch to, so nothing to ask", n)
	}
}

func TestTheTurnChecksHonourTheUsageMaxAgeOfThePolicy(t *testing.T) {
	for name, tc := range map[string]struct {
		age  time.Duration
		want int32
	}{"older than the policy": {90 * time.Second, 1}, "within the policy": {30 * time.Second, 0}} {
		t.Run(name, func(t *testing.T) {
			rig, probe := checkRig(t, answerWith(10))
			rig.svc.AccountPolicy = func() (preferences.AgentAccountPolicy, error) {
				return preferences.AgentAccountPolicy{Vendors: map[string]preferences.AccountVendorPolicy{
					string(agenthub.AgentTypeMock): {
						Enabled: true, ThresholdPercent: 90, UsageMaxAgeSeconds: 60,
						Order: []string{agenthub.BuiltinAccountID(agenthub.AgentTypeMock), "work"},
					},
				}}, nil
			}
			rig.fillBuiltin(t, 10)
			markShared(t, rig, agenthub.BuiltinAccountID(agenthub.AgentTypeMock))
			w := agenthub.LimitWindow{Label: "Weekly", UsedPercent: 10, ResetsAt: time.Now().Add(24 * time.Hour).Unix()}
			if err := rig.book.SaveUsage(context.Background(), agenthub.BuiltinAccountID(agenthub.AgentTypeMock), []agenthub.LimitWindow{w}, "observed", time.Now().Add(-tc.age)); err != nil {
				t.Fatal(err)
			}
			sess := startSession(t, rig)
			if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
				t.Fatal(err)
			}
			if probe.calls.Load() != tc.want {
				t.Errorf("a reading %v old under a 60 s policy: checks = %d, want %d", tc.age, probe.calls.Load(), tc.want)
			}
		})
	}
}

func TestOneTimeBoxCoversBothPreTurnChecks(t *testing.T) {
	restore := agenthub.SetTurnCheckTimeoutForTest(300 * time.Millisecond)
	defer restore()
	var mu sync.Mutex
	var deadlines []time.Time
	rig, _ := checkRig(t, func(ctx context.Context, _ agenthub.OpenSpec) ([]agenthub.LimitWindow, error) {
		d, ok := ctx.Deadline()
		if !ok {
			t.Error("a pre-turn check has no deadline")
		}
		mu.Lock()
		deadlines = append(deadlines, d)
		mu.Unlock()
		return windows(95), nil // over the threshold: the pick then names work, which is stale too
	})
	rig.fillBuiltin(t, 10)
	markShared(t, rig, agenthub.BuiltinAccountID(agenthub.AgentTypeMock))
	markShared(t, rig, "work")
	if err := rig.book.SaveUsage(context.Background(), agenthub.BuiltinAccountID(agenthub.AgentTypeMock), windows(10), "observed", time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	sess := startSession(t, rig)
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if len(deadlines) != 2 {
		t.Fatalf("checks = %d, want 2 (own account, then the target)", len(deadlines))
	}
	// The probe's context also carries the check's own 30 s cap, so the earlier
	// deadline is the turn's box: both checks share it.
	if d := deadlines[1].Sub(deadlines[0]); d < 0 || d > 100*time.Millisecond {
		t.Errorf("the two checks' deadlines differ by %v: they must share one box", d)
	}
}
