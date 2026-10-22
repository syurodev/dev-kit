package agenthub_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"devkit/internal/acp"
	"devkit/internal/agenthub"
	"devkit/internal/preferences"
)

const limitPrompt = "please FAIL_USAGE_LIMIT now" // the mock adapter's usage-limit marker

func ctxBackground() context.Context { return context.Background() }
func nowish() time.Time              { return time.Now() }

// sentPrompts is the text of every prompt the vendor was sent, with the home of
// the runtime that received it, in the order the runtimes were opened.
type sentPrompt struct{ home, text string }

func sentPrompts(rig *accountRig) []sentPrompt {
	rig.vendor.mu.Lock()
	defer rig.vendor.mu.Unlock()
	var out []sentPrompt
	for _, r := range rig.vendor.homes {
		p, ok := r.rt.(interface{ Prompts() [][]acp.ContentBlock })
		if !ok {
			continue
		}
		for _, blocks := range p.Prompts() {
			out = append(out, sentPrompt{home: r.home, text: agenthub.PromptText(blocks)})
		}
	}
	return out
}

func countSent(rig *accountRig, substr string) int {
	n := 0
	for _, p := range sentPrompts(rig) {
		if strings.Contains(p.text, substr) {
			n++
		}
	}
	return n
}

func exhaustedUntil(t *testing.T, rig *accountRig, id string) time.Time {
	t.Helper()
	acct, err := rig.book.Get(ctxBackground(), id)
	if err != nil {
		t.Fatal(err)
	}
	return acct.ExhaustedUntil
}

func addSpareAccount(t *testing.T, rig *accountRig) {
	t.Helper()
	if err := rig.book.Put(ctxBackground(), agenthub.AccountRecord{ID: "spare", Vendor: agenthub.AgentTypeMock, Label: "Spare", HomeDir: t.TempDir(), SignedIn: true}); err != nil {
		t.Fatal(err)
	}
}

var builtinMock = agenthub.BuiltinAccountID(agenthub.AgentTypeMock)

func TestRefusalMarksTheAccountAndNoneLeavesThePromptAlone(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	sess := startSession(t, rig)

	_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: limitPrompt})
	if !errors.Is(err, agenthub.ErrAgentUsageLimit) {
		t.Fatalf("error = %v, want the usage limit", err)
	}
	if got := rowAccount(t, rig, sess.ID); got != "" {
		t.Errorf("with `none` the refused turn must not move the session; account = %q", got)
	}
	if !exhaustedUntil(t, rig, builtinMock).After(nowish()) {
		t.Fatalf("the refused account must be marked exhausted, got %v", exhaustedUntil(t, rig, builtinMock))
	}
	if n := countSent(rig, "FAIL_USAGE_LIMIT"); n != 1 {
		t.Errorf("with `none` the prompt was sent %d times, want 1", n)
	}
	// The next prompt the person sends starts on the next account.
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "again"}); err != nil {
		t.Fatal(err)
	}
	if got := rowAccount(t, rig, sess.ID); got != "work" {
		t.Errorf("next turn account = %q, want work", got)
	}
}

func TestResendIfNoProgressSendsTheSamePromptOnceOnTheNextAccount(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalResendIfNoProgress)
	// A third account: a chain that followed its own follow-up would go on to it.
	addSpareAccount(t, rig)
	sess := startSession(t, rig)
	_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: limitPrompt})
	// The mock refuses the same prompt on the second account too: the chain ends there.
	if !errors.Is(err, agenthub.ErrAgentUsageLimit) {
		t.Fatalf("error = %v, want the usage limit from the resend", err)
	}
	if got := rowAccount(t, rig, sess.ID); got != "work" {
		t.Errorf("the resend must have run on work, and nothing after it; account = %q", got)
	}
	if got := rig.mock.LastSpec().AccountHome; got != rig.home {
		t.Errorf("the resend ran in %q, want %q", got, rig.home)
	}
	sent := sentPrompts(rig)
	var onWork, total int
	for _, p := range sent {
		if strings.Contains(p.text, "FAIL_USAGE_LIMIT") {
			total++
			if p.home == rig.home {
				onWork++
			}
		}
	}
	if total != 2 || onWork != 1 {
		t.Errorf("the prompt was sent %d times (%d on work), want twice: once refused, once resent on work; sent %v", total, onWork, sent)
	}
	if !exhaustedUntil(t, rig, "work").After(nowish()) {
		t.Error("the resend's refusal must mark work too")
	}
}

func TestResendIfNoProgressLeavesATurnThatMadeProgress(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalResendIfNoProgress)
	sess := startSession(t, rig)
	_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "please FAIL_USAGE_LIMIT_AFTER_OUTPUT"})
	if !errors.Is(err, agenthub.ErrAgentUsageLimit) {
		t.Fatalf("error = %v, want the original usage limit", err)
	}
	if n := countSent(rig, "FAIL_USAGE_LIMIT_AFTER_OUTPUT"); n != 1 {
		t.Errorf("a turn that already answered was sent %d times, want 1 (no resend)", n)
	}
	if got := rowAccount(t, rig, sess.ID); got != "" {
		t.Errorf("no follow-up, no move; account = %q", got)
	}
	if !exhaustedUntil(t, rig, builtinMock).After(nowish()) {
		t.Error("the refused account must still be marked")
	}
}

func TestResendIfNoProgressReadsOnlyTheRefusedTurn(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalResendIfNoProgress)
	sess := startSession(t, rig)
	// A turn that answered: its progress is its own, not the next turn's.
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: limitPrompt})
	if !errors.Is(err, agenthub.ErrAgentUsageLimit) {
		t.Fatalf("error = %v, want the usage limit from the resend", err)
	}
	if n := countSent(rig, "FAIL_USAGE_LIMIT"); n != 2 {
		t.Errorf("a refused turn with no output was sent %d times, want 2 (resent once)", n)
	}
	if got := rowAccount(t, rig, sess.ID); got != "work" {
		t.Errorf("account = %q, want work", got)
	}
}

func TestContinueSendsAFixedMessageOnTheNextAccount(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalContinue)
	sess := startSession(t, rig)
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: limitPrompt}); err != nil {
		t.Fatalf("the continue message succeeds on the next account, got %v", err)
	}
	if got := rowAccount(t, rig, sess.ID); got != "work" {
		t.Errorf("account = %q, want work", got)
	}
	if n := countSent(rig, "FAIL_USAGE_LIMIT"); n != 1 {
		t.Errorf("`continue` must not resend the prompt; it was sent %d times", n)
	}
	var continued bool
	for _, p := range sentPrompts(rig) {
		if p.home == rig.home && strings.Contains(p.text, "Continue where you left off") {
			continued = true
		}
	}
	if !continued {
		t.Errorf("the fixed message must go to work; sent %v", sentPrompts(rig))
	}
}

func TestNoFollowUpWhenThereIsNowhereToGo(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalContinue)
	sess := startSession(t, rig)
	// work is exhausted too, so nothing can take the continue message.
	_, _ = rig.book.List(ctxBackground(), agenthub.AgentTypeMock)
	if err := rig.book.MarkExhausted(ctxBackground(), "work", nowish().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: limitPrompt})
	if !errors.Is(err, agenthub.ErrAgentUsageLimit) || errors.Is(err, agenthub.ErrAllAccountsLimited) {
		t.Fatalf("error = %v, want the original usage limit (no follow-up, not a new error)", err)
	}
	if n := len(sentPrompts(rig)); n != 1 {
		t.Errorf("%d prompts were sent, want only the refused one", n)
	}
}

func TestRefusalFollowUpGivesUpQuietlyWhenTheSessionIsBusy(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalResendIfNoProgress)
	sess := startSession(t, rig)
	// Another turn (a queued peer message, say) takes the session between the
	// refused turn and its follow-up.
	var release func()
	rig.svc.SetFollowUpHookForTest(func(id string) { release = rig.svc.ClaimSessionForTest(id) })
	t.Cleanup(func() {
		if release != nil {
			release()
		}
	})
	_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: limitPrompt})
	if release == nil {
		t.Fatal("the follow-up was never attempted")
	}
	if !errors.Is(err, agenthub.ErrAgentUsageLimit) || errors.Is(err, agenthub.ErrSessionBusy) {
		t.Fatalf("error = %v, want the original usage limit, not busy", err)
	}
	if n := countSent(rig, "FAIL_USAGE_LIMIT"); n != 1 {
		t.Errorf("the prompt was sent %d times, want 1", n)
	}
}

func TestRefusalFollowUpKeepsTheOriginalErrorWhenEveryAccountIsLimitedByThen(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalContinue)
	sess := startSession(t, rig)
	// work reaches its limit after the follow-up was decided, before it starts.
	rig.svc.SetFollowUpHookForTest(func(string) {
		if err := rig.book.MarkExhausted(ctxBackground(), "work", nowish().Add(time.Hour)); err != nil {
			t.Error(err)
		}
	})
	_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: limitPrompt})
	if !errors.Is(err, agenthub.ErrAgentUsageLimit) || errors.Is(err, agenthub.ErrAllAccountsLimited) {
		t.Fatalf("error = %v, want the original usage limit", err)
	}
	if n := len(sentPrompts(rig)); n != 1 {
		t.Errorf("%d prompts were sent, want only the refused one", n)
	}
}

func TestRefusalFollowUpThatRanAndFailedReturnsItsOwnError(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalContinue)
	sess := startSession(t, rig)
	ctx, cancel := context.WithCancel(uiCtx())
	defer cancel()
	// The follow-up fails for a reason of its own: here, the caller went away.
	rig.svc.SetFollowUpHookForTest(func(string) { cancel() })
	_, err := rig.svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: limitPrompt})
	if !errors.Is(err, context.Canceled) || errors.Is(err, agenthub.ErrAgentUsageLimit) {
		t.Fatalf("error = %v, want the follow-up's own failure", err)
	}
}

func TestAccountLimitRateLimitedMarksWithTheCooldown(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	sess := startSession(t, rig)
	before := nowish()
	_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "please FAIL_RATE_LIMIT_429"})
	if !errors.Is(err, agenthub.ErrTurnFailed) {
		t.Fatalf("error = %v, want the refused turn", err)
	}
	got := exhaustedUntil(t, rig, builtinMock)
	if got.Before(before.Add(14*time.Minute)) || got.After(nowish().Add(16*time.Minute)) {
		t.Errorf("a 429 that names no reset keeps the account out for the 15-minute cooldown; until = %v (now %v)", got, nowish())
	}
}

func TestAccountLimitNonLimitFailuresMarkNothing(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalContinue)
	sess := startSession(t, rig)
	_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "please FAIL_PROVIDER_503"})
	if !errors.Is(err, agenthub.ErrTurnFailed) {
		t.Fatalf("error = %v, want the refused turn", err)
	}
	for _, kind := range []agenthub.StopKind{
		agenthub.StopProviderUnavailable, agenthub.StopProcessExited, agenthub.StopFailed,
		agenthub.StopAuthRequired, agenthub.StopCancelled, agenthub.StopTimeout,
	} {
		retry := nowish().Add(time.Hour)
		if err := rig.svc.MarkAccountLimitedForTest(ctxBackground(), sess.ID, agenthub.SessionStop{Kind: kind, RetryAfter: &retry}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{builtinMock, "work"} {
		if got := exhaustedUntil(t, rig, id); !got.IsZero() {
			t.Errorf("account %s marked until %v by a failure that is not a limit", id, got)
		}
	}
	if got := rowAccount(t, rig, sess.ID); got != "" {
		t.Errorf("no limit, no follow-up; account = %q", got)
	}
}

func TestAccountLimitMarksTheAccountTheRuntimeRunsAs(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	sess := startSession(t, rig)
	if err := rig.svc.ResumeSession(uiCtx(), sess.ID, true); err != nil { // runs as the built-in account
		t.Fatal(err)
	}
	// The row names work, but the process still runs as the built-in account.
	if err := rig.svc.BindSessionAccountForTest(ctxBackground(), sess.ID, "work"); err != nil {
		t.Fatal(err)
	}
	if err := rig.svc.MarkAccountLimitedForTest(ctxBackground(), sess.ID, agenthub.SessionStop{Kind: agenthub.StopUsageLimit}); err != nil {
		t.Fatal(err)
	}
	if !exhaustedUntil(t, rig, builtinMock).After(nowish()) {
		t.Error("the account the vendor refused (the runtime's) must be marked")
	}
	if got := exhaustedUntil(t, rig, "work"); !got.IsZero() {
		t.Errorf("work runs nothing and must not be marked; until = %v", got)
	}
}

func TestAccountLimitUntilComesFromTheBlockingWindowThenTheStopThenTheCooldown(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	sess := startSession(t, rig)
	ctx := ctxBackground()
	if _, err := rig.book.List(ctx, agenthub.AgentTypeMock); err != nil {
		t.Fatal(err)
	}
	now := nowish()
	short := now.Add(2 * time.Hour).Unix()
	codexPair := []agenthub.LimitWindow{
		{Label: "5h", UsedPercent: 100, ResetsAt: short, Status: "reached"},
		{Label: "Weekly", UsedPercent: 20, ResetsAt: now.Add(5 * 24 * time.Hour).Unix(), Status: "reached"},
	}
	if err := rig.book.SaveUsage(ctx, builtinMock, codexPair, "observed", now); err != nil {
		t.Fatal(err)
	}
	retry := now.Add(time.Hour)
	if err := rig.svc.MarkAccountLimitedForTest(ctx, sess.ID, agenthub.SessionStop{Kind: agenthub.StopUsageLimit, RetryAfter: &retry}); err != nil {
		t.Fatal(err)
	}
	if got := exhaustedUntil(t, rig, builtinMock).Unix(); got != short {
		t.Errorf("until = %v, want the full 5-hour window's reset %v", time.Unix(got, 0), time.Unix(short, 0))
	}

	// No usable window: the stop's own retry time.
	retryWork := now.Add(3 * time.Hour)
	if err := rig.svc.MoveSessionForTest(ctx, sess.ID, "work"); err != nil { // the row and the runtime's account
		t.Fatal(err)
	}
	if err := rig.svc.MarkAccountLimitedForTest(ctx, sess.ID, agenthub.SessionStop{Kind: agenthub.StopUsageLimit, RetryAfter: &retryWork}); err != nil {
		t.Fatal(err)
	}
	if got := exhaustedUntil(t, rig, "work").Unix(); got != retryWork.Unix() {
		t.Errorf("until = %v, want the stop's retry time %v", time.Unix(got, 0), retryWork)
	}

	// Neither (a retry time already past): the cooldown.
	addSpareAccount(t, rig)
	if err := rig.svc.MoveSessionForTest(ctx, sess.ID, "spare"); err != nil {
		t.Fatal(err)
	}
	past := now.Add(-time.Minute)
	before := nowish()
	if err := rig.svc.MarkAccountLimitedForTest(ctx, sess.ID, agenthub.SessionStop{Kind: agenthub.StopRateLimited, RetryAfter: &past}); err != nil {
		t.Fatal(err)
	}
	got := exhaustedUntil(t, rig, "spare")
	if got.Before(before.Add(14*time.Minute)) || got.After(nowish().Add(16*time.Minute)) {
		t.Errorf("until = %v, want about 15 minutes from now", got)
	}
}
