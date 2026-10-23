package agenthub_test

import (
	"context"
	"sync"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/preferences"
)

// eventCounts collects what the hooks fired; the service calls them from the
// goroutine that ran the turn, but a test reads them after it returned.
type eventCounts struct {
	mu       sync.Mutex
	switched [][4]string
	changed  int
}

func (e *eventCounts) attach(svc *agenthub.Service) {
	svc.OnAccountSwitched = func(sessionID, from, to, reason string) {
		e.mu.Lock()
		e.switched = append(e.switched, [4]string{sessionID, from, to, reason})
		e.mu.Unlock()
	}
	svc.OnAccountsChanged = func() {
		e.mu.Lock()
		e.changed++
		e.mu.Unlock()
	}
}

func (e *eventCounts) snapshot() ([][4]string, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([][4]string(nil), e.switched...), e.changed
}

func TestAMoveFiresTheSwitchedHookAndAChangeEvent(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.fillBuiltin(t, 95)
	var ev eventCounts
	ev.attach(rig.svc)
	sess := startSession(t, rig)
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	switched, changed := ev.snapshot()
	if len(switched) != 1 || switched[0][0] != sess.ID || switched[0][1] != agenthub.BuiltinAccountID(agenthub.AgentTypeMock) || switched[0][2] != "work" || switched[0][3] == "" {
		t.Fatalf("switched = %v, want one move from the built-in account to work with its reason", switched)
	}
	if changed == 0 {
		t.Error("a move changes what the Accounts page shows: OnAccountsChanged must fire")
	}
}

func TestAnUndoneMoveDoesNotFireTheSwitchedHook(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	rig.vendor.failHome.Store(true) // the added account's home never opens
	rig.fillBuiltin(t, 95)
	var ev eventCounts
	ev.attach(rig.svc)
	sess := startSession(t, rig)
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if rig.vendor.homeOpens.Load() == 0 {
		t.Fatal("test premise: the move to the added account must have been tried")
	}
	if switched, _ := ev.snapshot(); len(switched) != 0 {
		t.Errorf("the move was undone: OnAccountSwitched fired %d times", len(switched))
	}
}

func TestObservedUsageTellsThePageAtMostOnceEveryTwoSeconds(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	var ev eventCounts
	ev.attach(rig.svc)
	sess := startSession(t, rig)
	for i := 1; i <= 5; i++ {
		rig.svc.ObserveUsageForTest(context.Background(), sess.ID, []agenthub.LimitWindow{{Label: "5-hour", UsedPercent: float64(i)}})
	}
	if _, changed := ev.snapshot(); changed != 1 {
		t.Fatalf("events = %d, want 1 for five quick readings (the page refetches the list on each event)", changed)
	}
}
