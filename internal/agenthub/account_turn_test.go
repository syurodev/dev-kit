package agenthub_test

import (
	"context"
	"errors"
	"strings"
	"sync"
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

// switchNotices are the emitted account-switch notices.
func switchNotices(rig *accountRig) []string {
	var out []string
	for _, u := range rig.updates() {
		if strings.Contains(string(u), "accountSwitch") {
			out = append(out, string(u))
		}
	}
	return out
}

func TestTurnMovesToTheNextAccountOverTheThreshold(t *testing.T) {
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

func TestTurnStaysUnderTheThreshold(t *testing.T) {
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

func TestTurnDoesNothingWithTheFeatureOff(t *testing.T) {
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

func TestTurnDoesNothingWhenTheVendorIsNotEnabled(t *testing.T) {
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

func TestTurnStaysOnTheBuiltinWhenItIsTheOnlyAccount(t *testing.T) {
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

func TestTurnIsRefusedWhenEveryAccountIsExhausted(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	ctx := context.Background()
	if _, err := rig.book.List(ctx, agenthub.AgentTypeMock); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(time.Hour)
	for _, id := range []string{agenthub.BuiltinAccountID(agenthub.AgentTypeMock), "work"} {
		if err := rig.book.MarkExhausted(ctx, id, until); err != nil {
			t.Fatal(err)
		}
	}
	sess := startSession(t, rig)
	_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"})
	if !errors.Is(err, agenthub.ErrAllAccountsLimited) {
		t.Fatalf("error = %v, want ErrAllAccountsLimited", err)
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
	// The claim was released: once an account is back, the next turn runs.
	rig.svc.Accounts = nil
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatalf("the turn after a refusal: %v", err)
	}
}

// failingAccountStore refuses to write a session's account.
type failingAccountStore struct {
	agenthub.Store
}

func (failingAccountStore) UpdateSessionAccount(context.Context, string, string, time.Time) error {
	return errors.New("disk full")
}

func TestTurnStaysWhereItWasWhenTheMoveCannotBeWritten(t *testing.T) {
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
