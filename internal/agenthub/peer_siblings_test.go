package agenthub_test

import (
	"strings"
	"testing"

	"devkit/internal/agenthub"
)

func (r *peerRig) child(parent agenthub.AgentSession, title string) agenthub.AgentSession {
	r.t.Helper()
	sess, err := r.svc.CreateSession(agentCtx(parent.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: parent.ProjectPath, ParentSessionID: parent.ID, Title: &title})
	if err != nil {
		r.t.Fatal(err)
	}
	var view mockRuntimeView = r.mock.LastRuntime()
	r.rts[sess.ID] = &view
	return sess
}

func TestSiblingsMessageEachOtherAndTheEnvelopeSaysToReportToTheParent(t *testing.T) {
	r := newPeerRig(t)
	x, y := r.child(r.a, "backend"), r.child(r.a, "frontend")

	rel, _, found, err := r.svc.PeerRoute(agentCtx(x.ID), y.ID)
	if err != nil || !found || rel != agenthub.PeerSibling {
		t.Fatalf("route = %v %v %v", rel, found, err)
	}
	if res := r.send(x, y, "which field is the booking id?", ""); res.Delivery != agenthub.DeliveryStarted {
		t.Fatalf("sibling message: %+v", res)
	}
	r.waitIdle(y)
	sent := promptsText(r.rt(y).Prompts())
	for _, want := range []string{"sibling", "tell YOUR PARENT", "at most 4"} {
		if !strings.Contains(sent, want) {
			t.Errorf("the envelope lacks %q:\n%s", want, sent)
		}
	}
	// The parent and its children are still reachable both ways.
	if res := r.send(y, r.a, "done", ""); res.Delivery == agenthub.DeliveryRefused {
		t.Fatalf("to the parent: %+v", res)
	}
}

func TestAnExchangeBetweenSiblingsStopsAtFourMessages(t *testing.T) {
	r := newPeerRig(t)
	from, to := r.child(r.a, "backend"), r.child(r.a, "frontend")
	replyTo := ""
	for hop := 1; hop <= agenthub.MaxSiblingHops; hop++ {
		r.hold(to)
		res := r.send(from, to, "round", replyTo)
		if res.Delivery == agenthub.DeliveryRefused || res.Hop != hop {
			t.Fatalf("hop %d: %+v", hop, res)
		}
		replyTo = res.MessageID
		_ = r.svc.Cancel(uiCtx(), to.ID)
		r.waitIdle(to)
		from, to = to, from
		r.svc.SetPromptQueue(from.ID, 0, false)
	}
	over := r.send(from, to, "one more", replyTo)
	if over.Delivery != agenthub.DeliveryRefused || over.Code != agenthub.PeerLoop || !strings.Contains(over.Reason, "parent") || !strings.Contains(over.Reason, "limit is 4") {
		t.Fatalf("past the sibling cap: %+v", over)
	}
}

func TestChildrenOfDifferentParentsDoNotSeeEachOther(t *testing.T) {
	r := newPeerRig(t)
	x, z := r.child(r.a, "backend"), r.child(r.b, "someone else's child")

	res := r.send(x, z, "hello", "")
	if res.Delivery != agenthub.DeliveryRefused || res.Code != agenthub.PeerNotFound || !strings.Contains(res.Reason, "different parent") {
		t.Fatalf("to another parent's child: %+v", res)
	}
	peers, err := r.svc.ListPeers(agentCtx(x.ID))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]agenthub.PeerRelation{}
	for _, p := range peers {
		seen[p.SessionID] = p.Relation
	}
	if _, listed := seen[z.ID]; listed {
		t.Error("another parent's child is listed")
	}
	if seen[r.a.ID] != agenthub.PeerParent {
		t.Errorf("the parent is %q", seen[r.a.ID])
	}
	// A top-level session of the workspace is still reachable by a child.
	if seen[r.b.ID] != agenthub.PeerSameWorkspace {
		t.Errorf("a top-level session of the workspace is %q", seen[r.b.ID])
	}
}
