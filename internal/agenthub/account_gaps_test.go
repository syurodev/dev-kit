package agenthub_test

import (
	"context"
	"errors"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/preferences"
)

func TestASignInThatDiedMarksTheAccountSignedOut(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	sess := startSession(t, rig)
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "FAIL_AUTH_REQUIRED"}); err == nil {
		t.Fatal("the turn must fail")
	}
	got, err := rig.book.Get(context.Background(), agenthub.BuiltinAccountID(agenthub.AgentTypeMock))
	if err != nil {
		t.Fatal(err)
	}
	if got.SignedIn {
		t.Error("an auth-required stop must mark the account the process ran as signed out")
	}
}

func TestASignInThatDiedIsIgnoredForAVendorNobodyConfigured(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.svc.AccountPolicy = nil
	sess := startSession(t, rig)
	_, _ = rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "FAIL_AUTH_REQUIRED"})
	if _, err := rig.book.Get(context.Background(), agenthub.BuiltinAccountID(agenthub.AgentTypeMock)); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Errorf("nothing configured: no account row may be created or changed, Get err = %v, want ErrAccountNotFound", err)
	}
}

// signedIn reads whether the book says an account is signed in.
func signedIn(t *testing.T, rig *accountRig, id string) bool {
	t.Helper()
	got, err := rig.book.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return got.SignedIn
}

// Only a sign-in that died is one account's problem. A missing CLI or a bad API
// key is the vendor's, whichever account the session runs as.
func TestOnlyADeadSignInMarksTheAccountSignedOut(t *testing.T) {
	builtin := agenthub.BuiltinAccountID(agenthub.AgentTypeMock)
	for _, tc := range []struct {
		name string
		err  error
		want bool // signed in afterwards
	}{
		{"sign-in required", agenthub.ErrAgentAuthRequired, false},
		{"the CLI is not installed", agenthub.ErrAgentNotInstalled, true},
		{"the chosen API key is unusable", agenthub.ErrAgentAPIKeyUnusable, true},
		{"a provider outage", errors.New("503 unavailable"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newAccountRig(t, preferences.RefusalNone)
			sess := startSession(t, rig)
			if _, err := rig.book.List(context.Background(), agenthub.AgentTypeMock); err != nil { // creates the built-in row
				t.Fatal(err)
			}
			if err := rig.svc.RecordStopForTest(context.Background(), sess.ID, tc.err); err != nil {
				t.Fatal(err)
			}
			if got := signedIn(t, rig, builtin); got != tc.want {
				t.Errorf("SignedIn = %v after %v, want %v", got, tc.err, tc.want)
			}
		})
	}
}

// The account marked is the one the process ran as, an added one included.
func TestASignInThatDiedOnAnAddedAccountMarksThatAccountOnly(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	sess := startSession(t, rig)
	if err := rig.svc.BindSessionAccountForTest(context.Background(), sess.ID, "work"); err != nil {
		t.Fatal(err)
	}
	if err := rig.svc.ResumeSession(uiCtx(), sess.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "FAIL_AUTH_REQUIRED"}); err == nil {
		t.Fatal("the turn must fail")
	}
	if signedIn(t, rig, "work") {
		t.Error("the added account the process ran as must be signed out")
	}
	if !signedIn(t, rig, agenthub.BuiltinAccountID(agenthub.AgentTypeMock)) {
		t.Error("the built-in account must be left alone")
	}
}

// End to end: once an account is signed out, a session on it moves to the other
// at its next turn, and a session elsewhere never moves onto the signed-out one.
func TestASignedOutAccountIsLeftAndSkippedThroughTheService(t *testing.T) {
	ctx := context.Background()
	builtin := agenthub.BuiltinAccountID(agenthub.AgentTypeMock)

	t.Run("the session on it moves off", func(t *testing.T) {
		rig := newAccountRig(t, preferences.RefusalNone)
		sess := startSession(t, rig)
		if err := rig.svc.BindSessionAccountForTest(ctx, sess.ID, "work"); err != nil {
			t.Fatal(err)
		}
		if err := rig.svc.ResumeSession(uiCtx(), sess.ID, true); err != nil {
			t.Fatal(err)
		}
		if err := rig.book.SetSignedIn(ctx, "work", false); err != nil {
			t.Fatal(err)
		}
		if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
			t.Fatal(err)
		}
		if got := rowAccount(t, rig, sess.ID); got != builtin {
			t.Errorf("session account = %q, want it moved to %q (its own is signed out)", got, builtin)
		}
	})

	t.Run("a limited session does not move onto it", func(t *testing.T) {
		rig := newAccountRig(t, preferences.RefusalNone)
		if err := rig.book.SetSignedIn(ctx, "work", false); err != nil {
			t.Fatal(err)
		}
		sess := startSession(t, rig)
		rig.fillBuiltin(t, 99)
		if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
			t.Fatalf("with nowhere to go the turn runs where it is: %v", err)
		}
		if got := rowAccount(t, rig, sess.ID); got == "work" {
			t.Errorf("session moved onto the signed-out account %q", got)
		}
	})
}
