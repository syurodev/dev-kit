package agenthub_test

import (
	"context"
	"errors"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/preferences"
)

// The built-in account is assumed signed in until a turn says otherwise, so the
// first turn of a person with no built-in login (only added accounts) is refused
// for sign-in. That refusal marks the account signed out, and the turn must then
// run once on the account that is signed in, instead of ending in an error the
// person has to retry by hand.

const (
	builtinAuthPrompt = "please FAIL_AUTH_IN_BUILTIN_HOME now"
	addedAuthPrompt   = "please FAIL_AUTH_IN_ADDED_HOME now"
)

func TestATurnRefusedForSignInRunsOnceOnASignedInAccount(t *testing.T) {
	// `none` is the person's choice for limits; a sign-in refusal ran nothing, so
	// it is not what that choice is about.
	for _, refusal := range []string{preferences.RefusalNone, preferences.RefusalResendIfNoProgress, preferences.RefusalContinue} {
		t.Run(refusal, func(t *testing.T) {
			rig := newAccountRig(t, refusal)
			sess := startSession(t, rig)
			if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: builtinAuthPrompt}); err != nil {
				t.Fatalf("the turn must run on the signed-in account, got %v", err)
			}
			if got := rowAccount(t, rig, sess.ID); got != "work" {
				t.Errorf("session account = %q, want it moved to work", got)
			}
			if signedIn(t, rig, builtinMock) {
				t.Error("the built-in account that refused the turn must be marked signed out")
			}
			var onBuiltin, onWork int
			for _, p := range sentPrompts(rig) {
				switch p.home {
				case "":
					onBuiltin++
				case rig.home:
					onWork++
				}
			}
			if onBuiltin != 1 || onWork != 1 {
				t.Errorf("prompt sent %d times on the built-in account and %d on work, want once each: %v", onBuiltin, onWork, sentPrompts(rig))
			}
		})
	}
}

func TestATurnRefusedForSignInOnAnAddedAccountRunsOnTheBuiltInOne(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	sess := startSession(t, rig)
	if err := rig.svc.BindSessionAccountForTest(context.Background(), sess.ID, "work"); err != nil {
		t.Fatal(err)
	}
	if err := rig.svc.ResumeSession(uiCtx(), sess.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: addedAuthPrompt}); err != nil {
		t.Fatalf("the turn must run on the built-in account, got %v", err)
	}
	if got := rowAccount(t, rig, sess.ID); got != builtinMock {
		t.Errorf("session account = %q, want it moved to the built-in account %q", got, builtinMock)
	}
	if signedIn(t, rig, "work") {
		t.Error("the added account that refused the turn must be marked signed out")
	}
}

func TestASignInRefusalWithNoOtherSignedInAccountKeepsTheError(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	sess := startSession(t, rig)
	if _, err := rig.book.List(context.Background(), agenthub.AgentTypeMock); err != nil {
		t.Fatal(err)
	}
	if err := rig.book.SetSignedIn(context.Background(), "work", false); err != nil {
		t.Fatal(err)
	}
	_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: builtinAuthPrompt})
	if !errors.Is(err, agenthub.ErrAgentAuthRequired) {
		t.Fatalf("error = %v, want the sign-in refusal", err)
	}
	if n := len(sentPrompts(rig)); n != 1 {
		t.Errorf("%d prompts were sent, want only the refused one", n)
	}
}

// One follow-up, never a walk through every account: a second sign-in refusal
// means the person hears about it.
func TestASignInRefusalIsFollowedOnlyOnce(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	addSpareAccount(t, rig)
	sess := startSession(t, rig)
	_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "please FAIL_AUTH_REQUIRED now"})
	if !errors.Is(err, agenthub.ErrAgentAuthRequired) {
		t.Fatalf("error = %v, want the sign-in refusal of the follow-up", err)
	}
	if n := countSent(rig, "FAIL_AUTH_REQUIRED"); n != 2 {
		t.Errorf("the prompt was sent %d times, want twice (refused, then one follow-up)", n)
	}
}
