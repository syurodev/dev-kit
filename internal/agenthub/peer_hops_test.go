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

// The evidence for per-pair counting: a parent heard from three children, and
// its next message to a child was refused as "step 7".
func TestAParentThatHeardFromItsChildrenCanMessageEachOfThem(t *testing.T) {
	r := newPeerRig(t)
	kids := []agenthub.AgentSession{r.child(r.a, "backend"), r.child(r.a, "frontend"), r.child(r.a, "infra")}
	r.hold(r.a)
	defer func() { _ = r.svc.Cancel(uiCtx(), r.a.ID) }()
	// Each child reports several times, the way a long task does: nobody
	// answers, so the pair's step does not move.
	for _, kid := range kids {
		for i := 0; i < 3; i++ {
			res := r.send(kid, r.a, "progress", "")
			if res.Delivery == agenthub.DeliveryRefused || res.Hop != 1 {
				t.Fatalf("%s report %d: %+v, want hop 1", kid.ID, i+1, res)
			}
		}
	}
	for _, kid := range kids {
		r.hold(kid)
		res := r.send(r.a, kid, "carry on", "")
		_ = r.svc.Cancel(uiCtx(), kid.ID)
		r.waitIdle(kid)
		if res.Delivery == agenthub.DeliveryRefused || res.Hop != 2 {
			t.Fatalf("parent -> %s: %+v, want hop 2 (an answer to that child's reports only)", kid.ID, res)
		}
	}
}

// A third session's traffic with either side neither deepens nor ends the
// exchange between a pair — so it cannot be used to slip past the cap either.
func TestAThirdSessionDoesNotChangeAPairsCount(t *testing.T) {
	r := newPeerRig(t)
	x, y := r.child(r.a, "backend"), r.child(r.a, "frontend")
	from, to := r.a, x
	replyTo := ""
	for hop := 1; hop <= agenthub.MaxPeerHops; hop++ {
		r.hold(to)
		res := r.send(from, to, "round", replyTo)
		if res.Delivery == agenthub.DeliveryRefused || res.Hop != hop {
			t.Fatalf("hop %d: %+v", hop, res)
		}
		replyTo = res.MessageID
		// Each side also talks to a third session in between.
		if other := r.send(from, y, "noise", ""); other.Delivery == agenthub.DeliveryRefused || other.Hop != 1 {
			t.Fatalf("noise %d: %+v", hop, other)
		}
		r.waitIdle(y)
		_ = r.svc.Cancel(uiCtx(), to.ID)
		r.waitIdle(to)
		from, to = to, from
		r.svc.SetPromptQueue(from.ID, 0, false)
	}
	over := r.send(from, to, "one more", replyTo)
	if over.Delivery != agenthub.DeliveryRefused || over.Code != agenthub.PeerLoop {
		t.Fatalf("seventh message with noise in between: %+v", over)
	}
}

// Two pairs on one parent are two exchanges: a deep ping-pong with one child
// leaves the parent's first message to another at step 1.
func TestADeepExchangeWithOneChildDoesNotCountAgainstAnother(t *testing.T) {
	r := newPeerRig(t)
	x, y := r.child(r.a, "backend"), r.child(r.a, "frontend")
	last, _ := r.exchange(x, r.a, agenthub.MaxPeerHops) // the sixth message is the parent's
	if last.Delivery == agenthub.DeliveryRefused {
		t.Fatalf("setup: %+v", last)
	}
	r.hold(y)
	defer func() { _ = r.svc.Cancel(uiCtx(), y.ID) }()
	res := r.send(r.a, y, "your turn", "")
	if res.Delivery == agenthub.DeliveryRefused || res.Hop != 1 {
		t.Fatalf("parent -> y after six messages with x: %+v, want hop 1", res)
	}
	// And the report that ends x's exchange does not touch y's pair.
	rep := r.send(x, r.a, "final report", "")
	if rep.Delivery == agenthub.DeliveryRefused || rep.Hop <= agenthub.MaxPeerHops {
		t.Fatalf("x's report: %+v", rep)
	}
	if again := r.send(r.a, y, "and again", ""); again.Delivery == agenthub.DeliveryRefused || again.Hop != 1 {
		t.Fatalf("parent -> y after x's report: %+v, want hop 1", again)
	}
}

// The person speaking to a session ends every exchange it was in.
func TestThePersonsPromptEndsEveryPairOfTheSession(t *testing.T) {
	r := newPeerRig(t)
	x, y := r.child(r.a, "backend"), r.child(r.a, "frontend")
	for _, kid := range []agenthub.AgentSession{x, y} {
		r.send(kid, r.a, "hello", "")
	}
	if res := r.send(r.a, x, "hi", ""); res.Hop != 2 {
		t.Fatalf("setup: parent -> x hop %d, want 2", res.Hop)
	}
	r.waitIdle(x)
	if _, err := r.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: r.a.ID, Prompt: "carry on"}); err != nil {
		t.Fatal(err)
	}
	r.waitIdle(r.a)
	for _, kid := range []agenthub.AgentSession{x, y} {
		if res := r.send(kid, r.a, "after the prompt", ""); res.Hop != 1 {
			t.Fatalf("%s -> parent after the person's prompt: hop %d, want 1", kid.ID, res.Hop)
		}
	}
}
