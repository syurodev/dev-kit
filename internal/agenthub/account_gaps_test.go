package agenthub_test

import (
	"context"
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
	if _, err := rig.book.Get(context.Background(), agenthub.BuiltinAccountID(agenthub.AgentTypeMock)); err == nil {
		t.Error("nothing configured: no account row may be created or changed")
	}
}
