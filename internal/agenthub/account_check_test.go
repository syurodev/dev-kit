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
