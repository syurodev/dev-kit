package agenthub_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/identity"
	"devkit/internal/preferences"
)

// A new session picks its account before anything is opened, by the policy a
// turn uses. These tests drive CreateSession against a vendor that can refuse
// an account's open, and read back which accounts were asked to open.

var errQuotaAtOpen = fmt.Errorf("%w: the plan's usage is spent", agenthub.ErrAgentUsageLimit)

func createSession(rig *accountRig, t *testing.T) (agenthub.AgentSession, error) {
	t.Helper()
	return rig.svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)})
}

// exhaustAccount marks an account's limit as reached until then.
func exhaustAccount(t *testing.T, rig *accountRig, id string, until time.Time) {
	t.Helper()
	if _, err := rig.book.List(t.Context(), agenthub.AgentTypeMock); err != nil { // creates the built-in row
		t.Fatal(err)
	}
	if err := rig.book.MarkExhausted(t.Context(), id, until); err != nil {
		t.Fatal(err)
	}
}

func openedHomes(rig *accountRig) []string {
	var homes []string
	for _, a := range rig.vendor.tried() {
		homes = append(homes, a.home)
	}
	return homes
}

func wantHomes(t *testing.T, rig *accountRig, want ...string) {
	t.Helper()
	got := openedHomes(rig)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("accounts asked to open = %q, want %q", got, want)
	}
}

func workGeneration(t *testing.T, rig *accountRig) string {
	t.Helper()
	acct, err := rig.book.Get(t.Context(), "work")
	if err != nil {
		t.Fatal(err)
	}
	if acct.UsageGeneration == "" {
		t.Fatal("the added account has no usage generation")
	}
	return acct.UsageGeneration
}

// A built-in account that is known to be out of quota is never asked to open.
func TestCreateSessionSkipsAKnownExhaustedBuiltin(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	exhaustAccount(t, rig, builtinMock, time.Now().Add(2*time.Hour))
	rig.vendor.refuse("", errors.New("the built-in account must not be opened"))

	sess, err := createSession(rig, t)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	wantHomes(t, rig, rig.home)
	if sess.AccountID != "work" {
		t.Errorf("response account = %q, want work", sess.AccountID)
	}
	if got := rowAccount(t, rig, sess.ID); got != "work" {
		t.Errorf("stored account = %q, want work", got)
	}
	if got, _ := rig.svc.SessionAccountCachedForTest(sess.ID); got != "work" {
		t.Errorf("cached account = %q, want work", got)
	}
	facts, ok := rig.svc.RuntimeUsageForTest(sess.ID)
	if !ok || facts.AccountID != "work" || facts.AccountGeneration != workGeneration(t, rig) {
		t.Errorf("usage facts = %+v (%v), want work at its current generation", facts, ok)
	}
	if got := rig.vendor.tried()[0]; got.generation != workGeneration(t, rig) {
		t.Errorf("the open carried generation %q, want the account's %q", got.generation, workGeneration(t, rig))
	}
	if n := len(rig.vendor.promptedHomes()); n != 0 {
		t.Errorf("%d runtimes were prompted by creation", n)
	}
}

// What makes the built-in account ineligible is the policy's own rule, not one
// special case: a reached window, a weekly threshold, a stale reset.
func TestCreateSessionFollowsThePolicyForTheBuiltin(t *testing.T) {
	soon := time.Now().Add(24 * time.Hour).Unix()
	past := time.Now().Add(-time.Hour).Unix()
	cases := []struct {
		name    string
		windows []agenthub.LimitWindow
		weekly  int
		want    []string
	}{
		{"window at the threshold", []agenthub.LimitWindow{{Label: "Session", UsedPercent: 92, ResetsAt: soon}}, 0, []string{"work"}},
		{"window reached", []agenthub.LimitWindow{{Label: "Session", UsedPercent: 100, ResetsAt: soon}}, 0, []string{"work"}},
		{"weekly threshold lower than the main one", []agenthub.LimitWindow{{Label: "Weekly", UsedPercent: 71, ResetsAt: soon}}, 70, []string{"work"}},
		{"high window already reset", []agenthub.LimitWindow{{Label: "Session", UsedPercent: 99, ResetsAt: past}}, 0, []string{""}},
		{"under the threshold stays put", []agenthub.LimitWindow{{Label: "Session", UsedPercent: 40, ResetsAt: soon}}, 0, []string{""}},
		{"no reading stays put", nil, 0, []string{""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := newAccountRig(t, preferences.RefusalNone)
			if tc.weekly > 0 {
				base := rig.svc.AccountPolicy
				rig.svc.AccountPolicy = func() (preferences.AgentAccountPolicy, error) {
					p, err := base()
					vp := p.Vendors[string(agenthub.AgentTypeMock)]
					vp.WeeklyThresholdPercent = tc.weekly
					p.Vendors[string(agenthub.AgentTypeMock)] = vp
					return p, err
				}
			}
			if _, err := rig.book.List(t.Context(), agenthub.AgentTypeMock); err != nil {
				t.Fatal(err)
			}
			if len(tc.windows) > 0 {
				if err := rig.book.SaveUsage(t.Context(), builtinMock, tc.windows, "observed", time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			sess, err := createSession(rig, t)
			if err != nil {
				t.Fatal(err)
			}
			want := tc.want[0]
			if want == "work" {
				want = rig.home
			}
			wantHomes(t, rig, want)
			if (want == "") != (sess.AccountID == "") {
				t.Errorf("session account = %q for a launch in home %q", sess.AccountID, want)
			}
		})
	}
}

// A built-in that has capacity keeps the new session even if another account has more.
func TestCreateSessionKeepsAHealthyBuiltinOverAnEmptierAccount(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.fillBuiltin(t, 60)
	sess, err := createSession(rig, t)
	if err != nil {
		t.Fatal(err)
	}
	wantHomes(t, rig, "")
	if sess.AccountID != "" {
		t.Errorf("account = %q, want the built-in account (empty)", sess.AccountID)
	}
	facts, _ := rig.svc.RuntimeUsageForTest(sess.ID)
	if facts.AccountID != builtinMock || facts.AccountGeneration != "builtin" {
		t.Errorf("usage facts = %+v, want the built-in account", facts)
	}
}

// With switching off nothing changes: the built-in account opens even when exhausted.
func TestCreateSessionWithSwitchingOffOpensTheBuiltin(t *testing.T) {
	for name, policy := range map[string]func() (preferences.AgentAccountPolicy, error){
		"disabled": func() (preferences.AgentAccountPolicy, error) {
			return preferences.AgentAccountPolicy{Vendors: map[string]preferences.AccountVendorPolicy{
				string(agenthub.AgentTypeMock): {Enabled: false},
			}}, nil
		},
		"no entry": func() (preferences.AgentAccountPolicy, error) { return preferences.AgentAccountPolicy{}, nil },
		"unreadable": func() (preferences.AgentAccountPolicy, error) {
			return preferences.AgentAccountPolicy{}, errors.New("preferences are unreadable")
		},
	} {
		t.Run(name, func(t *testing.T) {
			rig := newAccountRig(t, preferences.RefusalNone)
			rig.svc.AccountPolicy = policy
			exhaustAccount(t, rig, builtinMock, time.Now().Add(time.Hour))
			sess, err := createSession(rig, t)
			if err != nil {
				t.Fatal(err)
			}
			wantHomes(t, rig, "")
			if sess.AccountID != "" {
				t.Errorf("account = %q, want the built-in account", sess.AccountID)
			}
		})
	}
}

// A vendor with no account book behaves as before.
func TestCreateSessionWithoutAnAccountBook(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.svc.Accounts = nil
	if _, err := createSession(rig, t); err != nil {
		t.Fatal(err)
	}
	wantHomes(t, rig, "")
}

// A signed-out account is never opened for a new session. When the built-in
// account is the only signed-in one and it is out of quota, that is the
// all-limited answer a turn would give, with the built-in account's reset.
func TestCreateSessionNeverOpensASignedOutAccount(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	exhaustAccount(t, rig, builtinMock, time.Now().Add(time.Hour))
	if err := rig.book.SetSignedIn(t.Context(), "work", false); err != nil {
		t.Fatal(err)
	}
	_, err := createSession(rig, t)
	if !errors.Is(err, agenthub.ErrAllAccountsLimited) {
		t.Fatalf("error = %v, want the all-accounts-limited error for the one signed-in account", err)
	}
	wantHomes(t, rig)
	assertNothingLeft(t, rig)
}

// An alternative that is only cooling down is not an exhausted one: the limit
// error is not claimed, and the session starts where the policy leaves it.
func TestCreateSessionDoesNotClaimAllLimitedWhileAnAlternativeCoolsDown(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	exhaustAccount(t, rig, builtinMock, time.Now().Add(time.Hour))
	if err := rig.book.MarkCooldown(t.Context(), "work", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	sess, err := createSession(rig, t)
	if errors.Is(err, agenthub.ErrAllAccountsLimited) {
		t.Fatalf("error = %v: a cooling-down account has not reached a limit", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	wantHomes(t, rig, "")
	if sess.AccountID != "" {
		t.Errorf("account = %q, want the built-in account", sess.AccountID)
	}
}

// Every signed-in account is spent: the existing limit error, nothing opened.
func TestCreateSessionWhenEveryAccountIsExhausted(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	reset := time.Now().Add(3 * time.Hour)
	exhaustAccount(t, rig, builtinMock, reset)
	if err := rig.book.MarkExhausted(t.Context(), "work", reset.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_, err := createSession(rig, t)
	if !errors.Is(err, agenthub.ErrAllAccountsLimited) {
		t.Fatalf("error = %v, want the all-accounts-limited error", err)
	}
	if !strings.Contains(err.Error(), reset.Local().Format("15:04")) {
		t.Errorf("error %q does not carry the earliest reset", err)
	}
	wantHomes(t, rig)
	assertNothingLeft(t, rig)
}

// assertNothingLeft: no endpoint, reservation or session survives a creation that failed.
func assertNothingLeft(t *testing.T, rig *accountRig) {
	t.Helper()
	if n := rig.svc.EndpointCountForTest(); n != 0 {
		t.Errorf("%d endpoints are still open", n)
	}
	if n := rig.svc.OpeningAccountsForTest(); n != 0 {
		t.Errorf("%d accounts are still held for a session being created", n)
	}
	list, err := rig.svc.ListSessions(uiCtx())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("%d sessions were stored", len(list))
	}
}

// The built-in account refuses at open with a quota error nobody knew about:
// the next account opens, once, and nothing is prompted.
func TestCreateSessionFailsOverOnAQuotaRefusalAtOpen(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.vendor.refuse("", errQuotaAtOpen)

	sess, err := createSession(rig, t)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	wantHomes(t, rig, "", rig.home)
	if sess.AccountID != "work" {
		t.Errorf("account = %q, want work", sess.AccountID)
	}
	if exhaustedUntil(t, rig, builtinMock).Before(time.Now()) {
		t.Error("the refusing account was not marked as out of quota")
	}
	if got := exhaustedUntil(t, rig, "work"); got.After(time.Now()) {
		t.Errorf("the account that opened was marked exhausted until %v", got)
	}
	if n := rig.svc.EndpointCountForTest(); n != 1 {
		t.Errorf("%d endpoints are open, want the successful session's one", n)
	}
	if n := rig.svc.OpeningAccountsForTest(); n != 0 {
		t.Errorf("%d reservations remain after publication", n)
	}
	if len(rig.vendor.promptedHomes()) != 0 {
		t.Error("a prompt was sent while creating")
	}
	facts, _ := rig.svc.RuntimeUsageForTest(sess.ID)
	if facts.AccountID != "work" || facts.AccountGeneration != workGeneration(t, rig) {
		t.Errorf("usage facts = %+v, want work", facts)
	}
}

// Both accounts refuse: two attempts and the limit error; no third try.
func TestCreateSessionStopsAfterEveryAccountRefusedForQuota(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.vendor.refuse("", errQuotaAtOpen)
	rig.vendor.refuse(rig.home, errQuotaAtOpen)

	_, err := createSession(rig, t)
	if !errors.Is(err, agenthub.ErrAllAccountsLimited) {
		t.Fatalf("error = %v, want the all-accounts-limited error", err)
	}
	wantHomes(t, rig, "", rig.home)
	assertNothingLeft(t, rig)
}

// A quota refusal at open must not rotate onto an account that was added while
// the attempts ran: the candidate set is the one captured at the start.
func TestCreateSessionDoesNotTryAnAccountAddedMidwayOrTwice(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.vendor.refuse("", errQuotaAtOpen)
	rig.vendor.refuse(rig.home, errQuotaAtOpen)
	late := t.TempDir()
	var once atomic.Bool
	rig.vendor.onEnter = func() {
		if once.CompareAndSwap(false, true) {
			if err := rig.book.Put(context.Background(), agenthub.AccountRecord{ID: "late", Vendor: agenthub.AgentTypeMock, HomeDir: late, SignedIn: true}); err != nil {
				t.Error(err)
			}
		}
	}
	_, err := createSession(rig, t)
	if err == nil {
		t.Fatal("expected the creation to fail")
	}
	for _, h := range openedHomes(rig) {
		if h == late {
			t.Fatal("an account added during the attempts was opened")
		}
	}
	if len(openedHomes(rig)) > 2 {
		t.Fatalf("accounts asked to open = %q, want at most the two known at the start", openedHomes(rig))
	}
}

// Only a quota error found by type is a reason to try another account.
func TestCreateSessionDoesNotRotateOnOtherOpenFailures(t *testing.T) {
	cases := map[string]error{
		"generic process failure": errors.New("the process exited with status 1: too many requests, limit"),
		"sign-in required":        fmt.Errorf("%w: sign in", agenthub.ErrAgentAuthRequired),
		"home cannot be prepared": fmt.Errorf("%w: no space left", agenthub.ErrAccountHome),
		"process failed":          fmt.Errorf("%w: bad flag", agenthub.ErrProcessFailed),
	}
	for name, openErr := range cases {
		t.Run(name, func(t *testing.T) {
			rig := newAccountRig(t, preferences.RefusalNone)
			rig.vendor.refuse("", openErr)
			exhaustAccount(t, rig, builtinMock, time.Time{})
			_, err := createSession(rig, t)
			if !errors.Is(err, openErr) && err == nil {
				t.Fatal("expected the creation to fail")
			}
			wantHomes(t, rig, "")
			if until := exhaustedUntil(t, rig, builtinMock); until.After(time.Now()) {
				t.Errorf("the account was marked exhausted until %v for a failure that is not a quota", until)
			}
			assertNothingLeft(t, rig)
		})
	}
}

// A cancelled creation ends it; it does not blame or skip the account.
func TestCreateSessionCancelledAtOpenMarksNothing(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.vendor.refuse("", context.Canceled)
	exhaustAccount(t, rig, builtinMock, time.Time{})
	if _, err := createSession(rig, t); err == nil {
		t.Fatal("expected the creation to fail")
	}
	wantHomes(t, rig, "")
	if until := exhaustedUntil(t, rig, builtinMock); until.After(time.Now()) {
		t.Errorf("a cancelled open marked the account exhausted until %v", until)
	}
	assertNothingLeft(t, rig)
}

// Switching turned off between attempts ends the loop with the vendor's own error.
func TestCreateSessionStopsRotatingWhenTheSwitchIsTurnedOffMidway(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.vendor.refuse("", errQuotaAtOpen)
	rig.vendor.onEnter = func() {
		rig.svc.AccountPolicy = func() (preferences.AgentAccountPolicy, error) { return preferences.AgentAccountPolicy{}, nil }
		rig.svc.InvalidateAccountPolicy()
	}
	_, err := createSession(rig, t)
	if !errors.Is(err, agenthub.ErrAgentUsageLimit) {
		t.Fatalf("error = %v, want the vendor's usage limit", err)
	}
	wantHomes(t, rig, "")
	assertNothingLeft(t, rig)
}

// The account is held against removal from the moment it is chosen.
func TestCreateSessionHoldsItsAccountAgainstRemoval(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.svc.AccountsDir = t.TempDir()
	exhaustAccount(t, rig, builtinMock, time.Now().Add(time.Hour))
	started, release := rig.vendor.holdNextOpen()
	done := make(chan error, 1)
	go func() {
		_, err := createSession(rig, t)
		done <- err
	}()
	<-started
	if err := rig.svc.RemoveAccount(uiCtx(), "work"); !errors.Is(err, agenthub.ErrAccountInUse) {
		t.Errorf("RemoveAccount while the session opens = %v, want ErrAccountInUse", err)
	}
	release()
	if err := <-done; err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := rig.svc.RemoveAccount(uiCtx(), "work"); !errors.Is(err, agenthub.ErrAccountInUse) {
		t.Errorf("RemoveAccount with the session running on it = %v, want ErrAccountInUse", err)
	}
}

// A creation that fails lets go of its account.
func TestCreateSessionReleasesTheAccountWhenItFails(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.svc.AccountsDir = t.TempDir()
	exhaustAccount(t, rig, builtinMock, time.Now().Add(time.Hour))
	rig.vendor.refuse(rig.home, errors.New("the process would not start"))
	if _, err := createSession(rig, t); err == nil {
		t.Fatal("expected the creation to fail")
	}
	assertNothingLeft(t, rig)
	if err := rig.svc.RemoveAccount(uiCtx(), "work"); err != nil {
		t.Errorf("RemoveAccount after a failed creation: %v", err)
	}
}

// The chosen account is removed between the pick and the hold: the choice is
// made again among what is left, never filled from a missing row.
func TestCreateSessionWhenTheChosenAccountIsRemovedFirst(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.svc.AccountsDir = t.TempDir()
	addSpareAccount(t, rig)
	spare, err := rig.book.Get(t.Context(), "spare")
	if err != nil {
		t.Fatal(err)
	}
	exhaustAccount(t, rig, builtinMock, time.Now().Add(time.Hour))
	rig.svc.SetAccountPickedHookForTest(func(id string) {
		if id == "work" {
			if err := rig.svc.RemoveAccount(context.Background(), "work"); err != nil {
				t.Errorf("RemoveAccount: %v", err)
			}
		}
	})
	sess, err := createSession(rig, t)
	if err != nil {
		t.Fatal(err)
	}
	wantHomes(t, rig, spare.HomeDir)
	if sess.AccountID != "spare" {
		t.Errorf("account = %q, want spare", sess.AccountID)
	}
	facts, _ := rig.svc.RuntimeUsageForTest(sess.ID)
	if facts.AccountID != "spare" || facts.AccountGeneration != spare.UsageGeneration {
		t.Errorf("usage facts = %+v, want spare at generation %q", facts, spare.UsageGeneration)
	}
}

// The only added account is removed between the pick and the hold: the policy
// then sees one account, and the session is the built-in account's, recorded as such.
func TestCreateSessionWhenTheOnlyOtherAccountIsRemovedFirst(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.svc.AccountsDir = t.TempDir()
	exhaustAccount(t, rig, builtinMock, time.Now().Add(time.Hour))
	rig.svc.SetAccountPickedHookForTest(func(string) {
		if err := rig.svc.RemoveAccount(context.Background(), "work"); err != nil {
			t.Errorf("RemoveAccount: %v", err)
		}
	})
	sess, err := createSession(rig, t)
	if err != nil {
		t.Fatal(err)
	}
	wantHomes(t, rig, "")
	facts, _ := rig.svc.RuntimeUsageForTest(sess.ID)
	if sess.AccountID != "" || facts.AccountID != builtinMock || facts.AccountGeneration != "builtin" {
		t.Errorf("session account %q, usage facts %+v: want the built-in account throughout", sess.AccountID, facts)
	}
}

// Creation, a restart and a resume all agree on the account.
func TestCreatedSessionResumesOnTheSameAccount(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	exhaustAccount(t, rig, builtinMock, time.Now().Add(time.Hour))
	sess, err := createSession(rig, t)
	if err != nil {
		t.Fatal(err)
	}
	// The built-in account is usable again by now; the session stays where it ran.
	if err := rig.book.MarkExhausted(t.Context(), builtinMock, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	rig.restart(t)
	if err := rig.svc.ResumeSession(uiCtx(), sess.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := rig.mock.LastSpec().AccountHome; got != rig.home {
		t.Errorf("resumed in home %q, want %q", got, rig.home)
	}
	if got := rowAccount(t, rig, sess.ID); got != "work" {
		t.Errorf("stored account = %q", got)
	}
}

// Choosing a first account is not a move: no notice, no hook, no report row.
func TestCreateSessionOnAnotherAccountIsNotReportedAsAMove(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	var moves atomic.Int32
	rig.svc.OnAccountSwitched = func(string, string, string, string) { moves.Add(1) }
	exhaustAccount(t, rig, builtinMock, time.Now().Add(time.Hour))
	sess, err := createSession(rig, t)
	if err != nil {
		t.Fatal(err)
	}
	if moves.Load() != 0 {
		t.Error("OnAccountSwitched fired for a first selection")
	}
	for _, u := range rig.updates() {
		if strings.Contains(string(u), "accountSwitch") {
			t.Errorf("an account-switch notice was emitted: %s", u)
		}
	}
	if rig.svc.AccountSwitchKnownForTest(sess.ID) {
		t.Error("a switch was remembered for a session that never moved")
	}
}

// The model, effort and mode a creation asks for do not depend on the account.
func TestCreateSessionAppliesTheConfigOnTheChosenAccount(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	exhaustAccount(t, rig, builtinMock, time.Now().Add(time.Hour))
	sess, err := rig.svc.CreateSession(uiCtx(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t),
		InitialConfig: map[string]string{"model": "mock-fast"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sess.AccountID != "work" {
		t.Errorf("session = %+v", sess)
	}
}

// A built-in account pinned to a credential the person chose (Codex's API key)
// is not replaced by a subscription account: nothing is chosen among accounts,
// and a quota refusal at open is the vendor's own answer.
func TestCreateSessionKeepsAnExplicitlyChosenCredential(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.vendor.pinned.Store(true)
	exhaustAccount(t, rig, builtinMock, time.Now().Add(time.Hour))
	sess, err := createSession(rig, t)
	if err != nil {
		t.Fatal(err)
	}
	wantHomes(t, rig, "")
	if sess.AccountID != "" {
		t.Errorf("account = %q, want the built-in account", sess.AccountID)
	}

	rig.vendor.refuse("", errQuotaAtOpen)
	if _, err := createSession(rig, t); !errors.Is(err, agenthub.ErrAgentUsageLimit) {
		t.Fatalf("error = %v, want the vendor's usage limit", err)
	}
	wantHomes(t, rig, "", "")
}

// An account book that cannot be read stops an enabled creation: the built-in
// account is not launched blind.
type failingList struct{ agenthub.AccountBook }

func (failingList) List(context.Context, agenthub.AgentType) ([]agenthub.AccountRecord, error) {
	return nil, errors.New("the database is locked")
}

func TestCreateSessionFailsClosedWhenTheAccountsCannotBeRead(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.svc.Accounts = failingList{rig.book}
	_, err := createSession(rig, t)
	if !errors.Is(err, agenthub.ErrAccountsUnavailable) {
		t.Fatalf("error = %v, want ErrAccountsUnavailable", err)
	}
	if strings.Contains(err.Error(), "locked") {
		t.Errorf("error %q carries the database's words", err)
	}
	wantHomes(t, rig)
	assertNothingLeft(t, rig)
}

// A child is a fresh session: the parent's account is not inherited, and the
// same policy picks for it.
func TestFreshChildPicksItsOwnAccount(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	parent := startSession(t, rig)
	if err := rig.svc.BindSessionAccountForTest(t.Context(), parent.ID, "work"); err != nil {
		t.Fatal(err)
	}
	ctx := identity.WithCaller(t.Context(), identity.Agent(parent.ID))
	child, err := rig.svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock})
	if err != nil {
		t.Fatal(err)
	}
	if child.AccountID != "" {
		t.Errorf("child account = %q, want the built-in account the policy leaves a healthy session on", child.AccountID)
	}
	exhaustAccount(t, rig, builtinMock, time.Now().Add(time.Hour))
	second, err := rig.svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock})
	if err != nil {
		t.Fatal(err)
	}
	if second.AccountID != "work" {
		t.Errorf("second child account = %q, want work while the built-in account is out of quota", second.AccountID)
	}
}

// Each attempt of a creation builds its own endpoint and snapshot from the one
// pipeline: the refused attempt's endpoint is closed before the next opens, only
// the opened one is published, and its token alone adopts the session's usage.
func TestCreateSessionRetryKeepsTheHarnessPipelineCoherent(t *testing.T) {
	e := newLaunchEnv(t, policy)
	e.harness.set(func(s, ws, v string) (harness.Snapshot, error) {
		return snapshotFor(t, s, ws, v, func(in *harness.Inputs) {
			gitOffW(3)(in)
			in.Session = &harness.Override{Schema: 1, Revision: 5}
		}), nil
	})
	book := agenthub.NewSQLAccountStore(e.db)
	home := t.TempDir()
	if _, err := book.List(t.Context(), agenthub.AgentTypeMock); err != nil {
		t.Fatal(err)
	}
	if err := book.Put(t.Context(), agenthub.AccountRecord{ID: "work", Vendor: agenthub.AgentTypeMock, HomeDir: home, SignedIn: true}); err != nil {
		t.Fatal(err)
	}
	e.svc.Accounts = book
	e.svc.AccountPolicy = func() (preferences.AgentAccountPolicy, error) {
		return preferences.AgentAccountPolicy{Vendors: map[string]preferences.AccountVendorPolicy{
			string(agenthub.AgentTypeMock): {Enabled: true, ThresholdPercent: 90, Order: []string{builtinMock, "work"}},
		}}, nil
	}
	e.adapter.openErrFor = func(spec agenthub.OpenSpec) error {
		if spec.AccountHome == "" {
			return errQuotaAtOpen
		}
		return nil
	}

	s := e.mustCreate()

	if s.AccountID != "work" {
		t.Fatalf("account = %q, want work", s.AccountID)
	}
	if n := e.ev.count("endpoint.openWithPolicy"); n != 2 {
		t.Errorf("%d endpoints were opened, want one per attempt", n)
	}
	if n := e.ev.count("endpoint.close"); n != 1 {
		t.Errorf("%d endpoints were closed, want only the refused attempt's", n)
	}
	closed := e.ev.index("endpoint.close")
	second := e.ev.index("adapter.open")
	if closed < 0 || second < 0 || e.ev.all()[closed] != "endpoint.close" {
		t.Fatalf("events = %v", e.ev.all())
	}
	eps := e.svc.MCPEndpoints.(*policyEndpoints)
	if len(eps.policies) != 2 || eps.policies[0].Served("git.status") || eps.policies[1].Served("git.status") {
		t.Errorf("each endpoint must be staged with the snapshot's policy: %d policies", len(eps.policies))
	}
	specs := e.adapter.specs
	if len(specs) != 2 || specs[0].UsageToken == specs[1].UsageToken || specs[0].AccountHome != "" || specs[1].AccountHome != home {
		t.Fatalf("specs = %+v", specs)
	}
	if _, ok := e.svc.ToolUsageScope(s.ID, specs[0].UsageToken); ok {
		t.Error("the refused attempt's token adopts the session's usage")
	}
	scope, ok := e.svc.ToolUsageScope(s.ID, specs[1].UsageToken)
	if !ok || *scope.ProfileRevision != 3 || *scope.OverrideRevision != 5 {
		t.Errorf("scope = %+v %v", scope, ok)
	}
	ref, enforced, ok := e.svc.HarnessApplied(s.ID)
	if !ok || !enforced || *ref.Revisions.Workspace != 3 {
		t.Errorf("applied = %+v %v %v", ref, enforced, ok)
	}
}
