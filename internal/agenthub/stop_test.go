package agenthub_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"devkit/internal/agenthub"
)

// failTurn runs a turn on the child that the vendor refuses, as the person's
// window would, and returns what Invoke said.
func (r *peerRig) failTurn(child agenthub.AgentSession, marker string) error {
	r.t.Helper()
	_, err := r.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: child.ID, Prompt: marker})
	if err == nil {
		r.t.Fatalf("%s: the turn completed", marker)
	}
	return err
}

// stopNotices are the DevKit notices about child that the parent was sent.
func (r *peerRig) stopNotices(parent, child agenthub.AgentSession) []agenthub.PeerMessageEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []agenthub.PeerMessageEvent
	for _, ev := range r.events {
		if ev.From.Kind == agenthub.MessageKindDevKit && ev.To == parent.ID && ev.From.SessionID == child.ID {
			out = append(out, ev)
		}
	}
	return out
}

func (r *peerRig) stopOf(s agenthub.AgentSession) *agenthub.SessionStop {
	r.t.Helper()
	got, err := r.svc.GetStatus(uiCtx(), s.ID)
	if err != nil {
		r.t.Fatal(err)
	}
	return got.Stop
}

// The evidence: a child whose provider returned 503 eleven times read as
// "status: error" to its parent, which resumed it again and again.
func TestAChildThatStopsOnAProviderErrorIsKnownToItsParent(t *testing.T) {
	r := newPeerRig(t)
	child := r.child(r.a, "backend")

	err := r.failTurn(child, "FAIL_PROVIDER_503")
	if !errors.Is(err, agenthub.ErrTurnFailed) {
		t.Fatalf("Invoke = %v", err)
	}

	// 1. The row says why, in the vocabulary an agent branches on.
	stop := r.stopOf(child)
	if stop == nil || stop.Kind != agenthub.StopProviderUnavailable || stop.Retryable || stop.Attempts != 11 || stop.Since.IsZero() ||
		stop.Message != "Upstream request failed: Endpoint is unavailable." || !strings.Contains(stop.Note, "not instructions") {
		t.Fatalf("stop = %+v", stop)
	}

	// 2. The parent is told once, as DevKit's notice and not the child's words.
	ev := r.waitEvent("the notice to the parent", func(ev agenthub.PeerMessageEvent) bool { return ev.From.Kind == agenthub.MessageKindDevKit })
	if ev.To != r.a.ID || ev.From.SessionID != child.ID || ev.Delivery != agenthub.DeliveryStarted {
		t.Fatalf("notice event = %+v", ev)
	}
	r.waitIdle(r.a)
	var seen []string
	for _, row := range r.userRows(r.a) {
		if row.Source != nil && row.Source.Kind == agenthub.MessageKindDevKit {
			seen = append(seen, row.Content.Text)
		}
	}
	if len(seen) != 1 {
		t.Fatalf("the parent's transcript holds %d notices, want 1", len(seen))
	}
	for _, part := range []string{"provider is unavailable", "provider_unavailable", "tried 11 times", "Endpoint is unavailable", "Retryable now: false", "do not loop"} {
		if !strings.Contains(seen[0], part) {
			t.Errorf("the notice lacks %q:\n%s", part, seen[0])
		}
	}
	prompts := r.rt(r.a).Prompts()
	if last := prompts[len(prompts)-1]; !strings.Contains(agenthub.PromptText(last), "NOT written by the person") {
		t.Errorf("the model was not told whose notice it is: %v", last)
	}

	// 3. The same failure on the parent's next try is not announced again.
	r.failTurn(child, "FAIL_PROVIDER_503")
	r.svc.WaitQuiet(context.Background())
	if n := len(r.stopNotices(r.a, child)); n != 1 {
		t.Fatalf("%d notices after a second identical failure, want 1", n)
	}

	// 4. A resume is refused with the reason, and goes through when forced.
	err = r.svc.ResumeChild(agentCtx(r.a.ID), child.ID, false)
	var refused *agenthub.ResumeRefusedError
	if !errors.Is(err, agenthub.ErrResumeRefused) || !errors.As(err, &refused) {
		t.Fatalf("resume = %v, want a refusal", err)
	}
	for _, part := range []string{"model provider is unavailable", "Endpoint is unavailable.", "third-party", "force=true"} {
		if !strings.Contains(refused.Sentence(), part) {
			t.Errorf("the refusal lacks %q: %s", part, refused.Sentence())
		}
	}
	if err := r.svc.ResumeChild(agentCtx(r.a.ID), child.ID, true); err != nil {
		t.Fatalf("forced resume: %v", err)
	}

	// 5. A new turn clears the stop; a failure after a success is news again.
	if _, err := r.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: child.ID, Prompt: "carry on"}); err != nil {
		t.Fatal(err)
	}
	if got := r.stopOf(child); got != nil {
		t.Fatalf("stop after a completed turn = %+v", got)
	}
	r.failTurn(child, "FAIL_PROVIDER_503")
	r.waitFor("a second notice", func() bool { return len(r.stopNotices(r.a, child)) == 2 })
}

func TestAUsageLimitStopCarriesItsResetAndBlocksResumeUntilThen(t *testing.T) {
	r := newPeerRig(t)
	child := r.child(r.a, "backend")
	if err := r.failTurn(child, "FAIL_USAGE_LIMIT"); !errors.Is(err, agenthub.ErrAgentUsageLimit) {
		t.Fatalf("Invoke = %v", err)
	}
	stop := r.stopOf(child)
	if stop == nil || stop.Kind != agenthub.StopUsageLimit || stop.Retryable || stop.RetryAfter == nil || !stop.RetryAfter.After(time.Now()) || stop.RetryAfter.After(time.Now().Add(25*time.Hour)) {
		t.Fatalf("stop = %+v", stop)
	}
	if err := r.svc.ResumeChild(agentCtx(r.a.ID), child.ID, false); !errors.Is(err, agenthub.ErrResumeRefused) {
		t.Fatalf("resume = %v", err)
	}
	notice := r.waitEvent("the notice", func(ev agenthub.PeerMessageEvent) bool { return ev.From.Kind == agenthub.MessageKindDevKit })
	if notice.Code != "" {
		t.Fatalf("notice = %+v", notice)
	}
}

// A busy parent gets the notice in its running turn, through the same delivery
// path as any message; the caps never see it.
func TestTheNoticeReachesABusyParentAndIsNotCountedAgainstTheCaps(t *testing.T) {
	r := newPeerRig(t)
	child := r.child(r.a, "backend")
	r.hold(r.a)
	defer func() { _ = r.svc.Cancel(uiCtx(), r.a.ID) }()

	// More failures of different kinds than the per-minute pair cap allows.
	markers := []string{"FAIL_PROVIDER_503", "FAIL_USAGE_LIMIT"}
	for i := 0; i < 8; i++ {
		r.failTurn(child, markers[i%2])
		want := i + 1
		r.waitFor("a notice", func() bool { return len(r.stopNotices(r.a, child)) == want })
	}
	for _, ev := range r.stopNotices(r.a, child) {
		if ev.Delivery != agenthub.DeliverySteered || ev.Code != "" {
			t.Fatalf("notice to a busy parent = %+v", ev)
		}
	}
}

func TestNoNoticeForACancelOrForTheParentsOwnCall(t *testing.T) {
	r := newPeerRig(t)
	child := r.child(r.a, "backend")

	// The parent invokes the child itself and waits: the error is its result.
	if _, err := r.svc.Invoke(agentCtx(r.a.ID), agenthub.InvokeInput{SessionID: child.ID, Prompt: "FAIL_PROVIDER_503", CallerSessionID: r.a.ID}); err == nil {
		t.Fatal("the turn completed")
	}
	// A cancel is recorded, not announced.
	r.hold(child)
	if err := r.svc.Cancel(agentCtx(r.a.ID), child.ID); err != nil {
		t.Fatal(err)
	}
	r.waitFor("the cancel to land", func() bool { stop := r.stopOf(child); return stop != nil && stop.Kind == agenthub.StopCancelled })
	r.svc.WaitQuiet(context.Background())
	if n := len(r.stopNotices(r.a, child)); n != 0 {
		t.Fatalf("%d notices, want none", n)
	}
	// A cancelled session is resumable: someone stopped it on purpose.
	if err := r.svc.ResumeChild(agentCtx(r.a.ID), child.ID, false); err != nil {
		t.Fatalf("resume after a cancel: %v", err)
	}
}

func TestStopFromErrorNamesTheKnownFailures(t *testing.T) {
	now := time.Date(2026, 10, 18, 20, 0, 0, 0, time.UTC)
	vendor := func(sentinel error, said string) error {
		return &agenthub.VendorError{Agent: agenthub.AgentTypeOpenCode, Err: fmt.Errorf("%w: x", sentinel), Message: said}
	}
	cases := []struct {
		name      string
		err       error
		timedOut  bool
		kind      agenthub.StopKind
		retryable bool
	}{
		{"opencode 503", vendor(agenthub.ErrProcessFailed, "Upstream request failed: Endpoint is unavailable. (503)"), false, agenthub.StopProviderUnavailable, false},
		{"turn failed 502", vendor(agenthub.ErrTurnFailed, "Bad Gateway"), false, agenthub.StopProviderUnavailable, false},
		{"rate limited", vendor(agenthub.ErrTurnFailed, "429 Too Many Requests"), false, agenthub.StopRateLimited, false},
		{"usage limit", vendor(agenthub.ErrAgentUsageLimit, "You've hit your usage limit."), false, agenthub.StopUsageLimit, false},
		{"sign-in", vendor(agenthub.ErrAgentAuthRequired, ""), false, agenthub.StopAuthRequired, false},
		{"process", fmt.Errorf("%w: exit status 1", agenthub.ErrProcessFailed), false, agenthub.StopProcessExited, true},
		{"full window", vendor(agenthub.ErrTurnFailed, "the conversation no longer fits the context window"), false, agenthub.StopFailed, true},
		{"cancel", agenthub.ErrCancelled, false, agenthub.StopCancelled, true},
		{"time limit", errors.New("deadline"), true, agenthub.StopTimeout, true},
	}
	for _, tc := range cases {
		got := agenthub.StopFromError(tc.err, now, tc.timedOut)
		if got.Kind != tc.kind || got.Retryable != tc.retryable || !got.Since.Equal(now) {
			t.Errorf("%s: %+v, want %s retryable=%v", tc.name, got, tc.kind, tc.retryable)
		}
	}
	// An adapter that knows more says so: opencode's scheduled next attempt.
	next := now.Add(10 * time.Minute)
	got := agenthub.StopFromError(agenthub.WithStopHint(vendor(agenthub.ErrTurnFailed, "x"), agenthub.StopHint{Kind: agenthub.StopRateLimited, Attempts: 3, RetryAfter: next}), now, false)
	if got.Kind != agenthub.StopRateLimited || got.Attempts != 3 || got.RetryAfter == nil || !got.RetryAfter.Equal(next) {
		t.Errorf("hinted: %+v", got)
	}
	// The vendor's sentence is one line and bounded, whatever it holds.
	long := "line one\n\x1b[31mline two\t" + strings.Repeat("x", 1000)
	got = agenthub.StopFromError(vendor(agenthub.ErrTurnFailed, long), now, false)
	if strings.ContainsAny(got.Message, "\n\x1b\t") || len([]rune(got.Message)) > 300 {
		t.Errorf("message not folded and clipped: %q", got.Message)
	}
}
