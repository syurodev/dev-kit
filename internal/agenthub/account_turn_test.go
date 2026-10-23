package agenthub_test

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/preferences"
)

func startSession(t *testing.T, rig *accountRig) agenthub.AgentSession {
	t.Helper()
	sess, err := rig.svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)})
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

func rowAccount(t *testing.T, rig *accountRig, id string) string {
	t.Helper()
	sess, err := rig.svc.GetStatus(uiCtx(), id)
	if err != nil {
		t.Fatal(err)
	}
	return sess.AccountID
}

func mustStatus(t *testing.T, rig *accountRig, id string) agenthub.SessionStatus {
	t.Helper()
	sess, err := rig.svc.GetStatus(uiCtx(), id)
	if err != nil {
		t.Fatal(err)
	}
	return sess.Status
}

// switchNotices are the emitted notices of a move that happened.
func switchNotices(rig *accountRig) []string { return noticesOf(rig, `"accountSwitch":`) }

// failedSwitchNotices are the emitted notices of a move that was undone.
func failedSwitchNotices(rig *accountRig) []string { return noticesOf(rig, `"accountSwitchFailed":`) }

func noticesOf(rig *accountRig, key string) []string {
	var out []string
	for _, u := range rig.updates() {
		if strings.Contains(string(u), key) {
			out = append(out, string(u))
		}
	}
	return out
}

func TestAccountTurnMovesToTheNextAccountOverTheThreshold(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	var recordErrs []error
	var recordMu sync.Mutex
	rig.svc.OnRecordError = func(_ string, err error) {
		recordMu.Lock()
		recordErrs = append(recordErrs, err)
		recordMu.Unlock()
	}
	rig.fillBuiltin(t, 95)
	sess := startSession(t, rig)

	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if got := rowAccount(t, rig, sess.ID); got != "work" {
		t.Fatalf("session account = %q, want work", got)
	}
	if got := rig.mock.LastSpec().AccountHome; got != rig.home {
		t.Errorf("the turn ran in %q, want the account home %q", got, rig.home)
	}
	if got, ok := rig.svc.SessionAccountCachedForTest(sess.ID); !ok || got != "work" {
		t.Errorf("cached account = %q (%v), want work", got, ok)
	}
	var noticed bool
	builtin := agenthub.BuiltinAccountID(agenthub.AgentTypeMock)
	for _, u := range switchNotices(rig) {
		if strings.Contains(u, "Weekly 95% ≥ 90%") && strings.Contains(u, `"from":"`+builtin+`"`) && strings.Contains(u, `"to":"work"`) {
			noticed = true
		}
	}
	if !noticed {
		t.Errorf("the move must leave a notice carrying its reason, from and to; got %v", switchNotices(rig))
	}

	// The notice is stored as an ordinary row, and gives the session no title.
	page, err := rig.svc.TranscriptPage(uiCtx(), sess.ID, 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	var stored bool
	for _, e := range page.Entries {
		if e.Kind == agenthub.UpdateSessionInfo && strings.Contains(string(e.Payload), "accountSwitch") {
			stored = true
		}
	}
	if !stored {
		t.Error("the notice must be kept in the transcript")
	}
	recordMu.Lock()
	defer recordMu.Unlock()
	if len(recordErrs) > 0 {
		t.Errorf("recording failed: %v", recordErrs)
	}
}

func TestAccountTurnStaysUnderTheThreshold(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.fillBuiltin(t, 40)
	sess := startSession(t, rig)
	opens := rig.opens.Load()
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if got := rowAccount(t, rig, sess.ID); got != "" {
		t.Errorf("session account = %q, want the built-in (empty)", got)
	}
	if n := switchNotices(rig); len(n) != 0 {
		t.Errorf("no move, no notice; got %v", n)
	}
	if got := rig.opens.Load(); got != opens {
		t.Errorf("the idle process was reopened (%d opens, want %d): a turn that stays must keep it", got, opens)
	}
}

func TestAccountTurnDoesNothingWithTheFeatureOff(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.fillBuiltin(t, 99)
	rig.svc.Accounts = nil
	sess := startSession(t, rig)
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if got := rowAccount(t, rig, sess.ID); got != "" {
		t.Errorf("session account = %q, want the built-in with the feature off", got)
	}
	if n := switchNotices(rig); len(n) != 0 {
		t.Errorf("feature off, no notice; got %v", n)
	}
}

func TestAccountTurnDoesNothingWhenTheVendorIsNotEnabled(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.fillBuiltin(t, 99)
	rig.svc.AccountPolicy = func() (preferences.AgentAccountPolicy, error) {
		return preferences.AgentAccountPolicy{Vendors: map[string]preferences.AccountVendorPolicy{
			string(agenthub.AgentTypeMock): {Enabled: false, ThresholdPercent: 90},
		}}, nil
	}
	sess := startSession(t, rig)
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if got := rowAccount(t, rig, sess.ID); got != "" {
		t.Errorf("session account = %q, want the built-in with the vendor's switching off", got)
	}
}

func TestAccountTurnStaysOnTheBuiltinWhenItIsTheOnlyAccount(t *testing.T) {
	rig := newAccountRigWith(t, preferences.RefusalNone, rigOptions{noWork: true})
	rig.fillBuiltin(t, 99)
	sess := startSession(t, rig)
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if got := rowAccount(t, rig, sess.ID); got != "" {
		t.Errorf("session account = %q, want the built-in (nowhere to go)", got)
	}
	if n := switchNotices(rig); len(n) != 0 {
		t.Errorf("no move, no notice; got %v", n)
	}
}

func TestAccountTurnIsRefusedWhenEveryAccountIsExhausted(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	ctx := context.Background()
	builtin := agenthub.BuiltinAccountID(agenthub.AgentTypeMock)
	// Not a no-op: listing creates the built-in account's row, which
	// MarkExhausted needs (an open does not create it).
	if _, err := rig.book.List(ctx, agenthub.AgentTypeMock); err != nil {
		t.Fatal(err)
	}
	sess := startSession(t, rig)
	first := time.Now().Add(time.Hour)
	for id, until := range map[string]time.Time{builtin: first.Add(time.Hour), "work": first} {
		if err := rig.book.MarkExhausted(ctx, id, until); err != nil {
			t.Fatal(err)
		}
	}
	_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"})
	if !errors.Is(err, agenthub.ErrAllAccountsLimited) {
		t.Fatalf("error = %v, want ErrAllAccountsLimited", err)
	}
	// The same layout as a stop's retry time (stop.go), and the earliest return.
	if when := first.Local().Format("15:04 on 2 Jan"); !strings.Contains(err.Error(), "available again at "+when) {
		t.Errorf("the refusal must say when the first account is back (%s); got %q", when, err)
	}
	if status := mustStatus(t, rig, sess.ID); status != agenthub.SessionStatusIdle {
		t.Errorf("status after the refusal = %s, want idle (nothing ran)", status)
	}
	page, err := rig.svc.TranscriptPage(uiCtx(), sess.ID, 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range page.Entries {
		if e.Kind == agenthub.UpdateUserMessageChunk {
			t.Errorf("a refused turn recorded the prompt: %s", e.Payload)
		}
	}
	// The claim was released: the next turn is refused for the same reason,
	// not as busy.
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); !errors.Is(err, agenthub.ErrAllAccountsLimited) {
		t.Fatalf("the turn after a refusal: %v, want ErrAllAccountsLimited again (not ErrSessionBusy)", err)
	}
}

// failingAccountStore refuses to write a session's account.
type failingAccountStore struct {
	agenthub.Store
}

func (failingAccountStore) UpdateSessionAccount(context.Context, string, string, time.Time) error {
	return errors.New("disk full")
}

func TestAccountTurnStaysWhereItWasWhenTheMoveCannotBeWritten(t *testing.T) {
	rig := newAccountRigWith(t, preferences.RefusalNone, rigOptions{
		store: func(s agenthub.Store) agenthub.Store { return failingAccountStore{s} },
	})
	rig.fillBuiltin(t, 95)
	sess := startSession(t, rig)
	opens := rig.opens.Load()

	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatalf("the turn must still run on its old account: %v", err)
	}
	if got := rowAccount(t, rig, sess.ID); got != "" {
		t.Errorf("session account = %q, want the built-in it was on", got)
	}
	if got := rig.opens.Load(); got != opens+1 {
		t.Errorf("opens = %d, want %d: the closed process is reopened once, on the old account", got, opens+1)
	}
	if got := rig.mock.LastSpec().AccountHome; got != "" {
		t.Errorf("reopened in %q, want the built-in account (no home)", got)
	}
	if got, ok := rig.svc.SessionAccountCachedForTest(sess.ID); !ok || got != agenthub.BuiltinAccountID(agenthub.AgentTypeMock) {
		t.Errorf("cached account = %q (%v), want the built-in", got, ok)
	}
	if n := switchNotices(rig); len(n) != 0 {
		t.Errorf("a move that did not happen must not be announced; got %v", n)
	}
	if status := mustStatus(t, rig, sess.ID); status != agenthub.SessionStatusIdle {
		t.Errorf("status = %s, want idle", status)
	}
}

// After a restart the window's PrepareSession resumes the selected session
// while the person's first prompt arrives. A turn that moves the session must
// not be handed the process that resume opened on the account it is leaving.
func TestAccountTurnWaitsForAResumeInFlightAndRunsOnTheNewAccount(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.fillBuiltin(t, 95)
	sess := startSession(t, rig)
	rig.restart(t) // no session has a process now
	moveWaits := make(chan struct{}, 1)
	rig.svc.SetWaitHookForTest(func(waiter, _ string) {
		if waiter != "move" {
			return
		}
		select {
		case moveWaits <- struct{}{}:
		default:
		}
	})
	opens := rig.opens.Load()
	started, release := rig.vendor.holdNextOpen()
	defer release()

	prepared := make(chan error, 1)
	go func() { prepared <- rig.svc.PrepareSession(uiCtx(), sess.ID) }()
	<-started // the resume is inside the vendor, opening on the built-in account
	invoked := make(chan error, 1)
	go func() {
		_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"})
		invoked <- err
	}()
	var invokeErr error
	invokeDone := false
	select {
	case <-moveWaits:
	case invokeErr = <-invoked:
		invokeDone = true
		t.Errorf("the turn ended (%v) while a resume on the old account was still opening", invokeErr)
	case <-time.After(10 * time.Second):
		t.Error("the move did not wait for the resume in flight")
	}
	release()
	if err := <-prepared; err != nil {
		t.Errorf("PrepareSession: %v", err)
	}
	if !invokeDone {
		invokeErr = <-invoked
	}
	if invokeErr != nil {
		t.Fatalf("Invoke: %v", invokeErr)
	}

	if got := rowAccount(t, rig, sess.ID); got != "work" {
		t.Errorf("session account = %q, want work", got)
	}
	if got := rig.vendor.promptedHomes(); len(got) != 1 || got[0] != rig.home {
		t.Errorf("the turn ran in %q, want only the work account's home %q", got, rig.home)
	}
	if got := rig.opens.Load() - opens; got != 2 {
		t.Errorf("opens = %d, want 2: the resume on the old account, then one on the new", got)
	}
	if got := rig.vendor.homeOpens.Load(); got != 1 {
		t.Errorf("opens in the work home = %d, want 1", got)
	}
	if got, ok := rig.svc.SessionAccountCachedForTest(sess.ID); !ok || got != "work" {
		t.Errorf("cached account = %q (%v), want work", got, ok)
	}
	if n := switchNotices(rig); len(n) != 1 {
		t.Errorf("want one switch notice, got %v", n)
	}
}

func TestAccountTurnStaysOnItsAccountWhenTheNewOneCannotOpen(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.fillBuiltin(t, 95)
	rig.vendor.failHome.Store(true) // the work account's sign-in has expired
	sess := startSession(t, rig)
	builtin := agenthub.BuiltinAccountID(agenthub.AgentTypeMock)

	before := time.Now()
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatalf("the turn must run on its old account: %v", err)
	}
	if got := rowAccount(t, rig, sess.ID); got != "" {
		t.Errorf("session account = %q, want the built-in it was on (empty, as it was written)", got)
	}
	if got := rig.mock.LastSpec().AccountHome; got != "" {
		t.Errorf("the turn ran in %q, want the built-in account (no home)", got)
	}
	if got := rig.vendor.promptedHomes(); len(got) != 1 || got[0] != "" {
		t.Errorf("prompted homes = %q, want only the built-in", got)
	}
	if got, ok := rig.svc.SessionAccountCachedForTest(sess.ID); !ok || got != builtin {
		t.Errorf("cached account = %q (%v), want the built-in", got, ok)
	}
	if n := switchNotices(rig); len(n) != 0 {
		t.Errorf("a move that was undone must not be announced as one; got %v", n)
	}
	failed := failedSwitchNotices(rig)
	if len(failed) != 1 || !strings.Contains(failed[0], `"from":"`+builtin+`"`) || !strings.Contains(failed[0], `"to":"work"`) ||
		!strings.Contains(failed[0], errAccountHomeBroken.Error()) {
		t.Errorf("want one could-not-switch notice naming from, to and why; got %v", failed)
	}
	work, err := rig.book.Get(context.Background(), "work")
	if err != nil {
		t.Fatal(err)
	}
	if !work.CooldownUntil.After(before.Add(10 * time.Minute)) {
		t.Errorf("work kept out until %v, want about 15 minutes from now", work.CooldownUntil)
	}
	if !work.ExhaustedUntil.IsZero() {
		t.Error("a failed reopen must not claim a limit")
	}
	if got := rig.vendor.homeOpens.Load(); got != 1 {
		t.Fatalf("opens tried in the work home = %d, want 1", got)
	}

	// Within the cooldown the next turn does not try the broken account again.
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "again"}); err != nil {
		t.Fatalf("second turn: %v", err)
	}
	if got := rig.vendor.homeOpens.Load(); got != 1 {
		t.Errorf("opens tried in the work home = %d after a second turn, want still 1", got)
	}
	if got := rowAccount(t, rig, sess.ID); got != "" {
		t.Errorf("session account after the second turn = %q, want the built-in", got)
	}
}

// A turn whose context ends while the target account opens did not find the
// account broken: the session goes back, the account is not put on the
// cooldown, nothing says the switch failed, and the next turn tries it again.
func TestAccountTurnEndedWhileTheNewOneOpensDoesNotBlameIt(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.fillBuiltin(t, 95)
	rig.vendor.ctxEnds.Store(true)
	sess := startSession(t, rig)
	builtin := agenthub.BuiltinAccountID(agenthub.AgentTypeMock)

	started, release := rig.vendor.holdNextOpen()
	defer release()
	ctx, cancel := context.WithCancel(uiCtx())
	defer cancel()
	invoked := make(chan error, 1)
	go func() {
		_, err := rig.svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"})
		invoked <- err
	}()
	select {
	case <-started: // the open in the work home is inside the vendor
	case err := <-invoked:
		t.Fatalf("the turn ended (%v) before the open in the new account began", err)
	case <-time.After(10 * time.Second):
		t.Fatal("the open in the new account did not begin")
	}
	cancel()
	release()
	select {
	case <-invoked: // cancelled; its outcome is not what this test is about
	case <-time.After(10 * time.Second):
		t.Fatal("the turn did not end")
	}

	if got := rowAccount(t, rig, sess.ID); got != "" {
		t.Errorf("session account = %q, want the built-in it was on (empty, as it was written)", got)
	}
	if got, ok := rig.svc.SessionAccountCachedForTest(sess.ID); !ok || got != builtin {
		t.Errorf("cached account = %q (%v), want the built-in", got, ok)
	}
	if n := switchNotices(rig); len(n) != 0 {
		t.Errorf("a move that did not stand must not be announced; got %v", n)
	}
	if n := failedSwitchNotices(rig); len(n) != 0 {
		t.Errorf("an ended turn is not a failed switch; got %v", n)
	}
	work, err := rig.book.Get(context.Background(), "work")
	if err != nil {
		t.Fatal(err)
	}
	if !work.ExhaustedUntil.IsZero() || !work.CooldownUntil.IsZero() {
		t.Errorf("work kept out (exhausted %v, cooldown %v), want neither: the account was not at fault", work.ExhaustedUntil, work.CooldownUntil)
	}
	if status := mustStatus(t, rig, sess.ID); status != agenthub.SessionStatusIdle {
		t.Errorf("status after the ended turn = %s, want idle", status)
	}

	// The account was not blamed, so a turn with a live context moves to it.
	rig.vendor.ctxEnds.Store(false)
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "again"}); err != nil {
		t.Fatalf("next turn: %v", err)
	}
	if got := rowAccount(t, rig, sess.ID); got != "work" {
		t.Errorf("session account after the next turn = %q, want work", got)
	}
	if got := rig.mock.LastSpec().AccountHome; got != rig.home {
		t.Errorf("the next turn ran in %q, want the account home %q", got, rig.home)
	}
	if n := switchNotices(rig); len(n) != 1 {
		t.Errorf("want one switch notice after the next turn, got %v", n)
	}
}

// accountWritesFailFrom writes a session's account until the n-th call, then
// refuses.
type accountWritesFailFrom struct {
	agenthub.Store
	calls *atomic.Int32
	n     int32
}

func (s accountWritesFailFrom) UpdateSessionAccount(ctx context.Context, id, account string, at time.Time) error {
	if s.calls.Add(1) >= s.n {
		return errors.New("disk full")
	}
	return s.Store.UpdateSessionAccount(ctx, id, account, at)
}

// syncWriter is a log destination a test can read while the service writes.
type syncWriter struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *syncWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// The new account will not open and the move back cannot be written either: the
// session stays on the new account (the row says so, and so does the
// transcript), the account is kept out, and the turn fails with the vendor's
// error rather than anything worse.
func TestAccountTurnWhoseMoveBackCannotBeWrittenStaysOnTheNewAccount(t *testing.T) {
	rig := newAccountRigWith(t, preferences.RefusalNone, rigOptions{
		store: func(s agenthub.Store) agenthub.Store { return accountWritesFailFrom{s, new(atomic.Int32), 2} },
	})
	rig.fillBuiltin(t, 95)
	rig.vendor.failHome.Store(true)
	sess := startSession(t, rig)
	logs := new(syncWriter)
	log.SetOutput(logs)
	defer log.SetOutput(os.Stderr)

	_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"})
	if !errors.Is(err, errAccountHomeBroken) {
		t.Fatalf("error = %v, want the vendor's own refusal", err)
	}
	if got := rowAccount(t, rig, sess.ID); got != "work" {
		t.Errorf("session account = %q, want work (the move back was not written)", got)
	}
	if !strings.Contains(logs.String(), "could not move back to account") {
		t.Errorf("the failed move back must be logged; log:\n%s", logs.String())
	}
	if n := switchNotices(rig); len(n) != 1 {
		t.Errorf("the move stands, so it is announced once; got %v", n)
	}
	if n := failedSwitchNotices(rig); len(n) != 0 {
		t.Errorf("the session did not go back, so no could-not-switch notice; got %v", n)
	}
	work, gerr := rig.book.Get(context.Background(), "work")
	if gerr != nil {
		t.Fatal(gerr)
	}
	if !work.CooldownUntil.After(time.Now()) {
		t.Errorf("work must be kept out after its failed open; until %v", work.CooldownUntil)
	}
	if !work.ExhaustedUntil.IsZero() {
		t.Error("a failed reopen must not claim a limit")
	}
	if status := mustStatus(t, rig, sess.ID); status != agenthub.SessionStatusIdle {
		t.Errorf("status = %s, want idle", status)
	}
}

// heldAccountWrite holds the first write of a session's account until release
// is closed, and tells when it is held and when the read armed after it has
// been answered.
type heldAccountWrite struct {
	agenthub.Store
	entered, release chan struct{}
	writes           *atomic.Int32
	readArmed        *atomic.Bool
	readDone         chan struct{}
}

func (s heldAccountWrite) UpdateSessionAccount(ctx context.Context, id, account string, at time.Time) error {
	if s.writes.Add(1) == 1 {
		close(s.entered)
		<-s.release
	}
	return s.Store.UpdateSessionAccount(ctx, id, account, at)
}

func (s heldAccountWrite) GetSession(ctx context.Context, id string) (agenthub.AgentSession, error) {
	sess, err := s.Store.GetSession(ctx, id)
	if s.readArmed.CompareAndSwap(true, false) {
		close(s.readDone)
	}
	return sess, err
}

// A settings call that read the session before a move, and reopens it once the
// move is done, must reopen it on the account the row names now: the move's
// barrier holds it back while the move runs, and the resume reads the account
// from the row, not from the caller's copy.
func TestAccountMoveSendsAStaleResumeToTheNewAccount(t *testing.T) {
	held := heldAccountWrite{
		entered: make(chan struct{}), release: make(chan struct{}), writes: new(atomic.Int32),
		readArmed: new(atomic.Bool), readDone: make(chan struct{}),
	}
	rig := newAccountRigWith(t, preferences.RefusalNone, rigOptions{
		store: func(s agenthub.Store) agenthub.Store { held.Store = s; return held },
	})
	sess := startSession(t, rig) // live, on the built-in account
	ctx := context.Background()

	resumeWaits := make(chan struct{}, 1)
	rig.svc.SetWaitHookForTest(func(waiter, _ string) {
		if waiter != "resume" {
			return
		}
		select {
		case resumeWaits <- struct{}{}:
		default:
		}
	})
	opening, releaseOpen := rig.vendor.holdNextOpen()
	defer releaseOpen()

	moved := make(chan error, 1)
	go func() { moved <- rig.svc.MoveSessionForTest(ctx, sess.ID, "work") }()
	<-held.entered // its process is closed, the row not yet written
	held.readArmed.Store(true)
	prepared := make(chan error, 1)
	go func() { prepared <- rig.svc.PrepareSession(uiCtx(), sess.ID) }()
	<-held.readDone // PrepareSession has read the row: still the built-in account
	select {
	case <-resumeWaits: // held back by the move's barrier
	case <-opening:
		t.Error("PrepareSession reopened the session while the move was still writing its account")
	}
	releaseOpen()
	close(held.release)
	if err := <-moved; err != nil {
		t.Fatal(err)
	}
	if err := <-prepared; err != nil {
		t.Fatal(err)
	}
	if got := rowAccount(t, rig, sess.ID); got != "work" {
		t.Errorf("session account = %q, want work", got)
	}
	if got := rig.mock.LastSpec().AccountHome; got != rig.home {
		t.Errorf("PrepareSession reopened the session in %q, want the work account's home %q", got, rig.home)
	}
	if got, ok := rig.svc.SessionAccountCachedForTest(sess.ID); !ok || got != "work" {
		t.Errorf("cached account = %q (%v), want work", got, ok)
	}
}
