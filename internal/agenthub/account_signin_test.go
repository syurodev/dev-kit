package agenthub_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/preferences"
)

func TestAccountSignInCommandPreparesTheHomeAndAsksTheVendorForItsCommand(t *testing.T) {
	rig := newAccountRigWith(t, preferences.RefusalNone, rigOptions{signIn: &signInFake{}})
	fresh := filepath.Join(t.TempDir(), "fresh")
	if err := rig.book.Put(context.Background(), agenthub.AccountRecord{ID: "fresh", Vendor: agenthub.AgentTypeMock, Label: "Fresh", HomeDir: fresh}); err != nil {
		t.Fatal(err)
	}
	cmd, err := rig.svc.AccountSignInCommand(uiCtx(), "work", "login-method")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Label != "fake login" || rig.signIn.home != rig.home {
		t.Fatalf("cmd = %+v, the vendor was asked for home %q (want %q)", cmd, rig.signIn.home, rig.home)
	}
	if _, err := rig.svc.AccountSignInCommand(uiCtx(), "fresh", "login-method"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(fresh); err != nil || !info.IsDir() {
		t.Errorf("the home must exist before the login writes into it: %v", err)
	}
}

func TestAccountSignInCommandRefusesTheBuiltInAnAgentAndAnUnsupportedVendor(t *testing.T) {
	rig := newAccountRigWith(t, preferences.RefusalNone, rigOptions{signIn: &signInFake{}})
	if _, err := rig.book.List(context.Background(), agenthub.AgentTypeMock); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.svc.AccountSignInCommand(uiCtx(), agenthub.BuiltinAccountID(agenthub.AgentTypeMock), "login-method"); !errors.Is(err, agenthub.ErrInvalidAccount) {
		t.Errorf("built-in: %v, want ErrInvalidAccount (it signs in from Runtimes)", err)
	}
	if _, err := rig.svc.AccountSignInCommand(agentCtx("11111111-1111-1111-1111-111111111111"), "work", "login-method"); !errors.Is(err, agenthub.ErrPermissionDenied) {
		t.Errorf("agent: %v, want ErrPermissionDenied", err)
	}
	plain := newAccountRig(t, preferences.RefusalNone) // its vendor has no sign-in support
	if _, err := plain.svc.AccountSignInCommand(uiCtx(), "work", "login-method"); !errors.Is(err, agenthub.ErrAuthMethodUnknown) {
		t.Errorf("vendor without terminal sign-in: %v, want ErrAuthMethodUnknown", err)
	}
	if _, err := rig.svc.AccountSignInCommand(uiCtx(), "work", "carrier-pigeon"); !errors.Is(err, agenthub.ErrAuthMethodUnknown) {
		t.Errorf("unknown method: %v, want ErrAuthMethodUnknown", err)
	}
	if _, err := rig.svc.AccountSignInCommand(uiCtx(), "nobody", "login-method"); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Errorf("unknown account: %v, want ErrAccountNotFound", err)
	}
}

func TestStartAccountSignInUsesTheAccountsHomeAndTheProbeJail(t *testing.T) {
	rig := newAccountRigWith(t, preferences.RefusalNone, rigOptions{signIn: &signInFake{}})
	hidden := filepath.Join(t.TempDir(), "agent-accounts", "mock", "other")
	rig.svc.ProtectedPaths = func() []string { return []string{hidden} }
	flow, err := rig.svc.StartAccountSignIn(uiCtx(), "work", "browser")
	if err != nil || flow.LoginID != "login-1" {
		t.Fatalf("flow = %+v, %v", flow, err)
	}
	if rig.signIn.startedHome != rig.home || rig.signIn.startedAccount != "work" {
		t.Errorf("sign-in started with home %q account %q", rig.signIn.startedHome, rig.signIn.startedAccount)
	}
	if !slices.Contains(rig.signIn.startedSpec.DenyReadPaths, hidden) {
		t.Errorf("deny-read = %v, want DevKit's data directory hidden from the login process", rig.signIn.startedSpec.DenyReadPaths)
	}
	if _, err := rig.svc.StartAccountSignIn(uiCtx(), "work", "deviceCode"); err != nil {
		t.Errorf("device code: %v", err)
	}
	if _, err := rig.svc.StartAccountSignIn(uiCtx(), "work", "carrier-pigeon"); !errors.Is(err, agenthub.ErrAuthMethodUnknown) {
		t.Errorf("unknown method: %v, want ErrAuthMethodUnknown", err)
	}
}

func TestStartAccountSignInRefusesTheBuiltInAndNonWindowCallers(t *testing.T) {
	fake := &signInFake{}
	rig := newAccountRigWith(t, preferences.RefusalNone, rigOptions{signIn: fake})
	if _, err := rig.book.List(context.Background(), agenthub.AgentTypeMock); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.svc.StartAccountSignIn(uiCtx(), agenthub.BuiltinAccountID(agenthub.AgentTypeMock), "browser"); !errors.Is(err, agenthub.ErrInvalidAccount) {
		t.Errorf("built-in: %v, want ErrInvalidAccount", err)
	}
	if _, err := rig.svc.StartAccountSignIn(agentCtx("11111111-1111-1111-1111-111111111111"), "work", "browser"); !errors.Is(err, agenthub.ErrPermissionDenied) {
		t.Errorf("agent: %v, want ErrPermissionDenied", err)
	}
	if fake.startedAccount != "" {
		t.Errorf("a refused call started a sign-in for %q", fake.startedAccount)
	}
}

func TestRefreshAccountStatusRecordsWhatTheVendorSays(t *testing.T) {
	fake := &signInFake{state: agenthub.SignInState{SignedIn: true}}
	rig := newAccountRigWith(t, preferences.RefusalNone, rigOptions{signIn: fake})
	hidden := filepath.Join(t.TempDir(), "agent-accounts")
	rig.svc.ProtectedPaths = func() []string { return []string{hidden} }
	changed := 0
	rig.svc.OnAccountsChanged = func() { changed++ }
	acct, err := rig.svc.RefreshAccountStatus(uiCtx(), "work")
	if err != nil || !acct.SignedIn {
		t.Fatalf("account = %+v, %v", acct, err)
	}
	if changed != 0 {
		t.Errorf("nothing changed (work was already signed in): %d events", changed)
	}
	fake.state = agenthub.SignInState{SignedIn: false}
	acct, err = rig.svc.RefreshAccountStatus(uiCtx(), "work")
	if err != nil || acct.SignedIn || changed != 1 {
		t.Fatalf("after the vendor says signed out: %+v, %v, events %d", acct, err, changed)
	}
	if fake.checkedHome != rig.home {
		t.Errorf("the status was read from %q, want the account's home", fake.checkedHome)
	}
	if !slices.Contains(fake.checkedSpec.DenyReadPaths, hidden) {
		t.Errorf("the status probe ran with deny-read %v, want the probe jail (data directory hidden)", fake.checkedSpec.DenyReadPaths)
	}
	// And back: the way out of "signed out" is the vendor saying so.
	fake.state = agenthub.SignInState{SignedIn: true}
	acct, err = rig.svc.RefreshAccountStatus(uiCtx(), "work")
	if err != nil || !acct.SignedIn || changed != 2 {
		t.Fatalf("after the vendor says signed in: %+v, %v, events %d", acct, err, changed)
	}
}

// The vendor call can take long; the row can change under it (a turn marking the
// account signed out, a sign-in marking it in). The answer is written against the row
// as it is when the vendor has answered, not as it was when the call began.
func TestRefreshAccountStatusComparesWithTheRowAsItIsWhenTheVendorAnswers(t *testing.T) {
	for _, vendorSays := range []bool{true, false} {
		fake := &signInFake{state: agenthub.SignInState{SignedIn: vendorSays}}
		rig := newAccountRigWith(t, preferences.RefusalNone, rigOptions{signIn: fake})
		if err := rig.book.SetSignedIn(context.Background(), "work", vendorSays); err != nil {
			t.Fatal(err)
		}
		fake.onCheck = func() { // the row moves the other way while the vendor is asked
			if err := rig.book.SetSignedIn(context.Background(), "work", !vendorSays); err != nil {
				t.Error(err)
			}
		}
		changed := 0
		rig.svc.OnAccountsChanged = func() { changed++ }
		acct, err := rig.svc.RefreshAccountStatus(uiCtx(), "work")
		if err != nil || acct.SignedIn != vendorSays {
			t.Fatalf("vendor says %v: returned %+v, %v", vendorSays, acct, err)
		}
		if got := signedIn(t, rig, "work"); got != vendorSays {
			t.Errorf("vendor says %v: the row is %v, the answer the person asked for was lost", vendorSays, got)
		}
		if changed != 1 {
			t.Errorf("vendor says %v: %d events, want 1 (the row really changed)", vendorSays, changed)
		}
	}
}

func TestRefreshAccountStatusSaysSoWhenTheAccountWasRemovedMeanwhile(t *testing.T) {
	fake := &signInFake{state: agenthub.SignInState{SignedIn: false}}
	rig := newAccountRigWith(t, preferences.RefusalNone, rigOptions{signIn: fake})
	fake.onCheck = func() {
		if err := rig.book.Delete(context.Background(), "work"); err != nil {
			t.Error(err)
		}
	}
	if _, err := rig.svc.RefreshAccountStatus(uiCtx(), "work"); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Errorf("err = %v, want ErrAccountNotFound", err)
	}
}

func TestRefreshAccountStatusAnswersNothingWhenTheVendorCannotSay(t *testing.T) {
	for _, from := range []bool{true, false} {
		for _, cause := range []error{agenthub.ErrProcessFailed, agenthub.ErrAuthMethodUnknown, context.DeadlineExceeded} {
			fake := &signInFake{checkErr: cause, state: agenthub.SignInState{SignedIn: !from}}
			rig := newAccountRigWith(t, preferences.RefusalNone, rigOptions{signIn: fake})
			if err := rig.book.SetSignedIn(context.Background(), "work", from); err != nil {
				t.Fatal(err)
			}
			changed := 0
			rig.svc.OnAccountsChanged = func() { changed++ }
			if _, err := rig.svc.RefreshAccountStatus(uiCtx(), "work"); !errors.Is(err, cause) {
				t.Errorf("signed in %v, %v: err = %v, want it passed on", from, cause, err)
			}
			if got := signedIn(t, rig, "work"); got != from || changed != 0 {
				t.Errorf("signed in %v, %v: row now %v with %d events; an unknown status must change nothing", from, cause, got, changed)
			}
		}
	}
}

func TestRefreshAccountStatusReadsTheBuiltInAccountToo(t *testing.T) {
	builtin := agenthub.BuiltinAccountID(agenthub.AgentTypeMock)
	fake := &signInFake{state: agenthub.SignInState{SignedIn: true}}
	rig := newAccountRigWith(t, preferences.RefusalNone, rigOptions{signIn: fake})
	// No row yet: the first read creates it, like the list does.
	acct, err := rig.svc.RefreshAccountStatus(uiCtx(), builtin)
	if err != nil || !acct.SignedIn || !acct.Builtin() {
		t.Fatalf("account = %+v, %v", acct, err)
	}
	if fake.checkedHome != "" {
		t.Errorf("the built-in status was read from home %q, want the vendor's own", fake.checkedHome)
	}
	if err := rig.book.SetSignedIn(context.Background(), builtin, false); err != nil {
		t.Fatal(err)
	}
	if acct, err = rig.svc.RefreshAccountStatus(uiCtx(), builtin); err != nil || !acct.SignedIn {
		t.Fatalf("after a signed-out mark the refresh must say signed in again: %+v, %v", acct, err)
	}
	if _, err := rig.svc.RefreshAccountStatus(uiCtx(), "mock-nobody"); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Errorf("unknown account: %v, want ErrAccountNotFound", err)
	}
	if _, err := rig.svc.RefreshAccountStatus(agentCtx("11111111-1111-1111-1111-111111111111"), "work"); !errors.Is(err, agenthub.ErrPermissionDenied) {
		t.Errorf("agent: %v, want ErrPermissionDenied", err)
	}
}

func TestAfterAccountSignInRunsTheStatusCheckWithoutAWindowCaller(t *testing.T) {
	fake := &signInFake{state: agenthub.SignInState{SignedIn: true}}
	rig := newAccountRigWith(t, preferences.RefusalNone, rigOptions{signIn: fake})
	if err := rig.book.SetSignedIn(context.Background(), "work", false); err != nil {
		t.Fatal(err)
	}
	changed := 0
	rig.svc.OnAccountsChanged = func() { changed++ }
	rig.svc.AfterAccountSignIn("work")
	if got, _ := rig.book.Get(context.Background(), "work"); !got.SignedIn {
		t.Error("a finished sign-in must leave the account marked signed in")
	}
	if changed != 1 {
		t.Errorf("events = %d, want 1", changed)
	}
	// Quiet for what it cannot do.
	rig.svc.AfterAccountSignIn("nobody")
	fake.checkErr = agenthub.ErrProcessFailed
	rig.svc.AfterAccountSignIn("work")
	if !signedIn(t, rig, "work") {
		t.Error("an unreadable status after a sign-in must not sign the account out")
	}
}

// A turn that completed is the vendor accepting the login: the way back from the
// signed-out mark a dead sign-in left, with nobody having to press Refresh.
func TestACompletedTurnMarksASignedOutAccountSignedInAgain(t *testing.T) {
	ctx := context.Background()
	builtin := agenthub.BuiltinAccountID(agenthub.AgentTypeMock)
	for _, id := range []string{builtin, "work"} {
		t.Run(id, func(t *testing.T) {
			rig := newAccountRig(t, preferences.RefusalNone)
			// Switching off, so the session stays where it is and the turn runs as the account.
			base := rig.svc.AccountPolicy
			rig.svc.AccountPolicy = func() (preferences.AgentAccountPolicy, error) {
				p, err := base()
				v := p.Vendors[string(agenthub.AgentTypeMock)]
				v.Enabled = false
				p.Vendors[string(agenthub.AgentTypeMock)] = v
				return p, err
			}
			sess := startSession(t, rig)
			if id != builtin {
				if err := rig.svc.BindSessionAccountForTest(ctx, sess.ID, id); err != nil {
					t.Fatal(err)
				}
				if err := rig.svc.ResumeSession(uiCtx(), sess.ID, true); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := rig.book.List(ctx, agenthub.AgentTypeMock); err != nil {
				t.Fatal(err)
			}
			if err := rig.book.SetSignedIn(ctx, id, false); err != nil {
				t.Fatal(err)
			}
			changed := 0
			rig.svc.OnAccountsChanged = func() { changed++ }
			if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
				t.Fatal(err)
			}
			if !signedIn(t, rig, id) {
				t.Errorf("a completed turn on %s must set it signed in again", id)
			}
			if changed != 1 {
				t.Errorf("events = %d, want 1 (the mark changed)", changed)
			}
			if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "again"}); err != nil {
				t.Fatal(err)
			}
			if changed != 1 {
				t.Errorf("events = %d after a second turn, want still 1 (nothing changed)", changed)
			}
			// A refusal marks it out again: the two directions do not fight.
			if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "FAIL_AUTH_REQUIRED"}); err == nil {
				t.Fatal("the turn must fail")
			}
			if signedIn(t, rig, id) {
				t.Error("an auth-required stop must still mark the account signed out")
			}
		})
	}
}

func TestACompletedTurnLeavesAVendorNobodyConfiguredAlone(t *testing.T) {
	ctx := context.Background()
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.svc.AccountPolicy = nil
	sess := startSession(t, rig)
	if _, err := rig.book.List(ctx, agenthub.AgentTypeMock); err != nil {
		t.Fatal(err)
	}
	builtin := agenthub.BuiltinAccountID(agenthub.AgentTypeMock)
	if err := rig.book.SetSignedIn(ctx, builtin, false); err != nil {
		t.Fatal(err)
	}
	changed := 0
	rig.svc.OnAccountsChanged = func() { changed++ }
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if signedIn(t, rig, builtin) || changed != 0 {
		t.Errorf("nothing configured: the row must stay as it was (signed in %v, %d events)", signedIn(t, rig, builtin), changed)
	}
}
