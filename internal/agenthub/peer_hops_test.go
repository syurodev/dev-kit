package agenthub_test

import (
	"strings"
	"testing"

	"devkit/internal/agenthub"
)

// exchange sends n messages alternately, first -> second -> first ..., each
// answering the last, and returns the last result, whoever sent it. A refusal
// ends the run early.
func (r *peerRig) exchange(first, second agenthub.AgentSession, n int) (agenthub.SendMessageResult, string) {
	r.t.Helper()
	from, to := first, second
	replyTo := ""
	var last agenthub.SendMessageResult
	for i := 1; i <= n; i++ {
		r.hold(to)
		last = r.send(from, to, "round", replyTo)
		_ = r.svc.Cancel(uiCtx(), to.ID)
		r.waitIdle(to)
		if last.Delivery == agenthub.DeliveryRefused {
			return last, replyTo
		}
		replyTo = last.MessageID
		from, to = to, from
		r.svc.SetPromptQueue(from.ID, 0, false)
	}
	return last, replyTo
}

// The evidence: a child whose own count had reached the cap could not send its
// final report, and fell back to a task-board hand-off.
func TestAChildCanReportToItsParentWhateverItsCount(t *testing.T) {
	r := newPeerRig(t)
	x := r.child(r.a, "backend")

	// x -> a, a -> x, ... six messages; the last, a -> x, hands the child hop 6.
	last, replyTo := r.exchange(x, r.a, agenthub.MaxPeerHops)
	if last.Delivery == agenthub.DeliveryRefused || last.Hop != agenthub.MaxPeerHops {
		t.Fatalf("sixth message: %+v", last)
	}
	r.hold(r.a)
	defer func() { _ = r.svc.Cancel(uiCtx(), r.a.ID) }()
	res := r.send(x, r.a, "final report", replyTo)
	if res.Delivery == agenthub.DeliveryRefused {
		t.Fatalf("the child's report to its parent was refused: %+v", res)
	}
	if res.Hop <= agenthub.MaxPeerHops {
		t.Fatalf("test setup: the report is hop %d, not past the cap", res.Hop)
	}
}

// Ping-pong between a parent and a child still stops, on the parent's side.
func TestParentChildPingPongStopsOnTheParentSide(t *testing.T) {
	r := newPeerRig(t)
	x := r.child(r.a, "backend")
	last, _ := r.exchange(r.a, x, agenthub.MaxPeerHops+1)
	if last.Delivery != agenthub.DeliveryRefused || last.Code != agenthub.PeerLoop || !strings.Contains(last.Reason, "limit is 6") {
		t.Fatalf("seventh message of a parent-started exchange: %+v", last)
	}
}

// A message that starts an exchange (hop 1) resets the target's count, even if
// the target still carries a high one from earlier. Siblings show it: their cap
// is 4, so a stale count of 6 would refuse a reply the new exchange allows.
func TestAFreshMessageResetsTheTargetsCount(t *testing.T) {
	r := newPeerRig(t)
	x, y := r.child(r.a, "backend"), r.child(r.a, "frontend")

	// x ends this exchange holding hop 6 (a -> x is the sixth message).
	last, _ := r.exchange(x, r.a, agenthub.MaxPeerHops)
	if last.Delivery == agenthub.DeliveryRefused {
		t.Fatalf("setup: %+v", last)
	}
	// Still inside the decay window, x answering a sibling would be hop 7.
	if res := r.send(x, y, "stale", ""); res.Delivery != agenthub.DeliveryRefused || res.Code != agenthub.PeerLoop {
		t.Fatalf("with the old count x -> y: %+v", res)
	}
	// The person prompts the parent, which then starts a new exchange.
	if _, err := r.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: r.a.ID, Prompt: "carry on"}); err != nil {
		t.Fatal(err)
	}
	fresh := r.send(r.a, x, "new task", "")
	if fresh.Delivery == agenthub.DeliveryRefused || fresh.Hop != 1 {
		t.Fatalf("fresh parent message: %+v", fresh)
	}
	r.waitIdle(x)
	res := r.send(x, y, "question", "")
	if res.Delivery == agenthub.DeliveryRefused || res.Hop != 2 {
		t.Fatalf("x -> y after the fresh message: %+v, want hop 2", res)
	}
}
