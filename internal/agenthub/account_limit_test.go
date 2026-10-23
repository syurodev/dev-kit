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

// sentPrompt is the text of a prompt the vendor was sent, with the home of the
// runtime that received it.
type sentPrompt struct{ home, text string }

// sentPrompts is every prompt the vendor was sent, in the order the runtimes
// were opened.
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
	acct, err := rig.book.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return acct.ExhaustedUntil
}

func addSpareAccount(t *testing.T, rig *accountRig) {
	t.Helper()
	if err := rig.book.Put(t.Context(), agenthub.AccountRecord{ID: "spare", Vendor: agenthub.AgentTypeMock, Label: "Spare", HomeDir: t.TempDir(), SignedIn: true}); err != nil {
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
	if !exhaustedUntil(t, rig, builtinMock).After(time.Now()) {
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
	if !exhaustedUntil(t, rig, "work").After(time.Now()) {
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
	if !exhaustedUntil(t, rig, builtinMock).After(time.Now()) {
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
	if _, err := rig.book.List(t.Context(), agenthub.AgentTypeMock); err != nil {
		t.Fatal(err)
	}
	if err := rig.book.MarkExhausted(t.Context(), "work", time.Now().Add(time.Hour)); err != nil {
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
		if err := rig.book.MarkExhausted(t.Context(), "work", time.Now().Add(time.Hour)); err != nil {
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
	rig := newAccountRig(t, preferences.RefusalResendIfNoProgress)
	sess := startSession(t, rig)
	// Refused for its limit on the built-in account; the resend on work fails
	// at the vendor for a reason of its own.
	const prompt = "please FAIL_LIMIT_IN_BUILTIN_HOME"
	_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: prompt})
	if !errors.Is(err, agenthub.ErrTurnFailed) || errors.Is(err, agenthub.ErrAgentUsageLimit) {
		t.Fatalf("error = %v, want the follow-up's own failure, not the original limit", err)
	}
	var onWork int
	for _, p := range sentPrompts(rig) {
		if p.home == rig.home && strings.Contains(p.text, "FAIL_LIMIT_IN_BUILTIN_HOME") {
			onWork++
		}
	}
	if onWork != 1 {
		t.Errorf("the resend reached work %d times, want 1; sent %v", onWork, sentPrompts(rig))
	}
	if got := storedStop(t, rig, sess.ID); got == nil || got.Kind != agenthub.StopFailed {
		t.Errorf("stored stop = %+v, want the follow-up's own failure", got)
	}
}

func TestRefusalFollowUpWhoseCallerWentAwayReturnsThatError(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalContinue)
	sess := startSession(t, rig)
	ctx, cancel := context.WithCancel(uiCtx())
	defer cancel()
	// The follow-up fails before it reaches the vendor: the caller went away.
	rig.svc.SetFollowUpHookForTest(func(string) { cancel() })
	_, err := rig.svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: limitPrompt})
	if !errors.Is(err, context.Canceled) || errors.Is(err, agenthub.ErrAgentUsageLimit) {
		t.Fatalf("error = %v, want the follow-up's own failure", err)
	}
}

func TestAccountLimitRateLimitedMarksWithTheCooldown(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	sess := startSession(t, rig)
	before := time.Now()
	_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "please FAIL_RATE_LIMIT_429"})
	if !errors.Is(err, agenthub.ErrTurnFailed) {
		t.Fatalf("error = %v, want the refused turn", err)
	}
	got := exhaustedUntil(t, rig, builtinMock)
	if got.Before(before.Add(14*time.Minute)) || got.After(time.Now().Add(16*time.Minute)) {
		t.Errorf("a 429 that names no reset keeps the account out for the 15-minute cooldown; until = %v (now %v)", got, time.Now())
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
		retry := time.Now().Add(time.Hour)
		if err := rig.svc.MarkAccountLimitedForTest(t.Context(), sess.ID, agenthub.SessionStop{Kind: kind, RetryAfter: &retry}); err != nil {
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
	if err := rig.svc.BindSessionAccountForTest(t.Context(), sess.ID, "work"); err != nil {
		t.Fatal(err)
	}
	if err := rig.svc.MarkAccountLimitedForTest(t.Context(), sess.ID, agenthub.SessionStop{Kind: agenthub.StopUsageLimit}); err != nil {
		t.Fatal(err)
	}
	if !exhaustedUntil(t, rig, builtinMock).After(time.Now()) {
		t.Error("the account the vendor refused (the runtime's) must be marked")
	}
	if got := exhaustedUntil(t, rig, "work"); !got.IsZero() {
		t.Errorf("work runs nothing and must not be marked; until = %v", got)
	}
}

func TestAccountLimitUntilComesFromTheBlockingWindowThenTheStopThenTheCooldown(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	sess := startSession(t, rig)
	ctx := t.Context()
	if _, err := rig.book.List(ctx, agenthub.AgentTypeMock); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
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
	before := time.Now()
	if err := rig.svc.MarkAccountLimitedForTest(ctx, sess.ID, agenthub.SessionStop{Kind: agenthub.StopRateLimited, RetryAfter: &past}); err != nil {
		t.Fatal(err)
	}
	got := exhaustedUntil(t, rig, "spare")
	if got.Before(before.Add(14*time.Minute)) || got.After(time.Now().Add(16*time.Minute)) {
		t.Errorf("until = %v, want about 15 minutes from now", got)
	}
}

// plainFailPrompt makes the mock fail the turn with nothing in its words to say
// why: StopFromError reads it as `failed`, the way Claude's limit refusal
// reads, since DevKit does not know its wording.
const plainFailPrompt = "please FAIL_TURN_PLAIN"

func storedStop(t *testing.T, rig *accountRig, id string) *agenthub.SessionStop {
	t.Helper()
	sess, err := rig.store.GetSession(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return sess.Stop
}

// seeReachedWindow stores a reached window on the built-in account, observed at
// observedAt, the way observeAccountUsage stores Claude's rate-limit event: a
// "rejected" event marks the window reached even below 100%, and at 40% the
// turn's own pick stays on this account.
func seeReachedWindow(t *testing.T, rig *accountRig, resetsAt, observedAt time.Time) {
	t.Helper()
	if _, err := rig.book.List(t.Context(), agenthub.AgentTypeMock); err != nil {
		t.Fatal(err)
	}
	windows := []agenthub.LimitWindow{{Label: "5h", UsedPercent: 40, ResetsAt: resetsAt.Unix(), Status: "reached"}}
	if err := rig.book.SaveUsage(t.Context(), builtinMock, windows, "observed", observedAt); err != nil {
		t.Fatal(err)
	}
}

func TestReachedWindowMakesAPlainFailureAUsageLimit(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	sess := startSession(t, rig)
	reset := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	seeReachedWindow(t, rig, reset, time.Now())

	_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: plainFailPrompt})
	if !errors.Is(err, agenthub.ErrTurnFailed) {
		t.Fatalf("error = %v, want the vendor's own failure", err)
	}
	stop := storedStop(t, rig, sess.ID)
	if stop == nil || stop.Kind != agenthub.StopUsageLimit {
		t.Fatalf("stored stop = %+v, want usage_limit", stop)
	}
	if stop.Retryable || stop.RetryAfter == nil || !stop.RetryAfter.Equal(reset) {
		t.Errorf("stop = %+v, want not retryable before the window's reset %v", stop, reset)
	}
	if stop.Message != "The agent could not finish this turn." || stop.Note == "" {
		t.Errorf("the vendor's sentence and its note must be kept; stop = %+v", stop)
	}
	if got := exhaustedUntil(t, rig, builtinMock); !got.Equal(reset) {
		t.Errorf("the account must be out until the window's reset %v, got %v", reset, got)
	}
	if n := countSent(rig, "FAIL_TURN_PLAIN"); n != 1 {
		t.Errorf("with `none` the prompt was sent %d times, want 1", n)
	}
}

// A reached window on a vendor the person never configured explains nothing:
// the plain failure stays `failed`, and nothing follows from it (no mark on the
// account, no follow-up on another).
func TestReachedWindowIsIgnoredForAVendorThePersonDidNotConfigure(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalContinue)
	rig.svc.AccountPolicy = nil
	sess := startSession(t, rig)
	seeReachedWindow(t, rig, time.Now().Add(2*time.Hour), time.Now())

	_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: plainFailPrompt})
	if !errors.Is(err, agenthub.ErrTurnFailed) {
		t.Fatalf("error = %v, want the vendor's own failure", err)
	}
	stop := storedStop(t, rig, sess.ID)
	if stop == nil || stop.Kind != agenthub.StopFailed || !stop.Retryable || stop.RetryAfter != nil {
		t.Fatalf("stored stop = %+v, want a plain, retryable failure", stop)
	}
	if got := exhaustedUntil(t, rig, builtinMock); !got.IsZero() && got.After(time.Now()) {
		t.Errorf("the account was marked out until %v by a failure that is not a limit", got)
	}
	if n := countSent(rig, "FAIL_TURN_PLAIN"); n != 1 {
		t.Errorf("the prompt was sent %d times, want 1 (no follow-up)", n)
	}
	if got := rowAccount(t, rig, sess.ID); got != "" {
		t.Errorf("session account = %q, want it left on the built-in", got)
	}

	direct, err := rig.svc.UpgradeStopForTest(uiCtx(), sess.ID, agenthub.SessionStop{Kind: agenthub.StopFailed, Retryable: true})
	if err != nil {
		t.Fatal(err)
	}
	if direct.Kind != agenthub.StopFailed || !direct.Retryable || direct.RetryAfter != nil {
		t.Errorf("upgradeStopForReachedWindow = %+v, want the stop unchanged", direct)
	}
}

func TestReachedWindowRefusalIsResentOnTheNextAccount(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalResendIfNoProgress)
	sess := startSession(t, rig)
	seeReachedWindow(t, rig, time.Now().Add(2*time.Hour), time.Now())

	// The resend fails too (the mock fails this prompt everywhere), on work,
	// where no window is reached: that is its own failure.
	_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: plainFailPrompt})
	if !errors.Is(err, agenthub.ErrTurnFailed) {
		t.Fatalf("error = %v, want the resend's failure", err)
	}
	var total, onWork int
	for _, p := range sentPrompts(rig) {
		if strings.Contains(p.text, "FAIL_TURN_PLAIN") {
			total++
			if p.home == rig.home {
				onWork++
			}
		}
	}
	if total != 2 || onWork != 1 {
		t.Errorf("the prompt was sent %d times (%d on work), want once refused and once resent on work", total, onWork)
	}
	if got := rowAccount(t, rig, sess.ID); got != "work" {
		t.Errorf("account = %q, want work", got)
	}
	if got := storedStop(t, rig, sess.ID); got == nil || got.Kind != agenthub.StopFailed {
		t.Errorf("stored stop = %+v, want the resend's plain failure (work has no reached window)", got)
	}
	if got := exhaustedUntil(t, rig, "work"); !got.IsZero() {
		t.Errorf("work failed for no limit and must not be marked; until = %v", got)
	}
}

func TestReachedWindowRefusalContinuesOnTheNextAccount(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalContinue)
	sess := startSession(t, rig)
	seeReachedWindow(t, rig, time.Now().Add(2*time.Hour), time.Now())

	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: plainFailPrompt}); err != nil {
		t.Fatalf("the continue message succeeds on the next account, got %v", err)
	}
	if got := rowAccount(t, rig, sess.ID); got != "work" {
		t.Errorf("account = %q, want work", got)
	}
	var continued bool
	for _, p := range sentPrompts(rig) {
		if p.home == rig.home && strings.Contains(p.text, "Continue where you left off") {
			continued = true
		}
	}
	if !continued || countSent(rig, "FAIL_TURN_PLAIN") != 1 {
		t.Errorf("want the prompt once and the fixed message on work; sent %v", sentPrompts(rig))
	}
}

// A reached window that does not explain this turn's failure leaves it a plain
// failure: nothing marked, nothing followed.
func TestReachedWindowThatDoesNotExplainTheFailureChangesNothing(t *testing.T) {
	cases := map[string]struct{ resetsAt, observedAt time.Time }{
		"observed an hour ago":    {time.Now().Add(2 * time.Hour), time.Now().Add(-time.Hour)},
		"reset already past":      {time.Now().Add(-time.Minute), time.Now()},
		"observed 11 minutes ago": {time.Now().Add(2 * time.Hour), time.Now().Add(-11 * time.Minute)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rig := newAccountRig(t, preferences.RefusalContinue)
			sess := startSession(t, rig)
			seeReachedWindow(t, rig, c.resetsAt, c.observedAt)

			_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: plainFailPrompt})
			if !errors.Is(err, agenthub.ErrTurnFailed) {
				t.Fatalf("error = %v, want the plain failure", err)
			}
			if got := storedStop(t, rig, sess.ID); got == nil || got.Kind != agenthub.StopFailed {
				t.Errorf("stored stop = %+v, want failed", got)
			}
			if got := exhaustedUntil(t, rig, builtinMock); !got.IsZero() {
				t.Errorf("nothing must be marked; until = %v", got)
			}
			if n := len(sentPrompts(rig)); n != 1 {
				t.Errorf("%d prompts were sent, want only the failed one (no follow-up)", n)
			}
			if got := rowAccount(t, rig, sess.ID); got != "" {
				t.Errorf("no follow-up, no move; account = %q", got)
			}
		})
	}
}

func TestReachedWindowUpgradesOnlyAPlainFailure(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	sess := startSession(t, rig)
	reset := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	seeReachedWindow(t, rig, reset, time.Now())
	since := time.Now().UTC().Add(-time.Second)
	retry := time.Now().Add(30 * time.Minute).UTC()

	for _, kind := range []agenthub.StopKind{
		agenthub.StopCancelled, agenthub.StopTimeout, agenthub.StopAuthRequired, agenthub.StopProcessExited,
		agenthub.StopProviderUnavailable, agenthub.StopRateLimited, agenthub.StopUsageLimit, agenthub.StopInterruptedByRestart,
	} {
		in := agenthub.SessionStop{Kind: kind, Message: "said", Note: "note", Retryable: true, RetryAfter: &retry, Since: since, Attempts: 3}
		got, err := rig.svc.UpgradeStopForTest(t.Context(), sess.ID, in)
		if err != nil {
			t.Fatal(err)
		}
		if got.Kind != in.Kind || got.Retryable != in.Retryable || got.RetryAfter != in.RetryAfter {
			t.Errorf("%s was changed to %+v; only a plain failure is read against a reached window", kind, got)
		}
	}

	in := agenthub.SessionStop{Kind: agenthub.StopFailed, Message: "said", Note: "note", Retryable: true, Since: since, Attempts: 3}
	got, err := rig.svc.UpgradeStopForTest(t.Context(), sess.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != agenthub.StopUsageLimit || got.Retryable || got.RetryAfter == nil || !got.RetryAfter.Equal(reset) {
		t.Errorf("upgraded = %+v, want usage_limit, not retryable, until %v", got, reset)
	}
	if got.Message != in.Message || got.Note != in.Note || !got.Since.Equal(in.Since) || got.Attempts != in.Attempts {
		t.Errorf("upgraded = %+v, want the vendor's sentence, note, time and attempts kept from %+v", got, in)
	}
}

// The follow-up reads the stop of the turn Invoke just ran. A turn refused
// before it ran anything (here, an attachment) leaves an earlier turn's
// usage-limit stop in place, and that must not earn a follow-up.
func TestFollowUpReadsOnlyThisTurnsStop(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	sess := startSession(t, rig)
	if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: limitPrompt}); !errors.Is(err, agenthub.ErrAgentUsageLimit) {
		t.Fatalf("error = %v, want the usage limit", err)
	}
	if got := storedStop(t, rig, sess.ID); got == nil || got.Kind != agenthub.StopUsageLimit {
		t.Fatalf("stored stop = %+v, want usage_limit", got)
	}
	rig.refusal = preferences.RefusalContinue
	_, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "look", Attachments: []string{"/no/such/file.txt"}})
	if err == nil {
		t.Fatal("a missing attachment must refuse the turn")
	}
	if n := len(sentPrompts(rig)); n != 1 {
		t.Errorf("%d prompts were sent, want only the first turn's (no follow-up for a turn that never ran); sent %v", n, sentPrompts(rig))
	}
	if got := rowAccount(t, rig, sess.ID); got != "" {
		t.Errorf("no follow-up, no move; account = %q", got)
	}
}

func TestAccountLimitMarksOnlyAVendorThePersonConfigured(t *testing.T) {
	cases := map[string]func() (preferences.AgentAccountPolicy, error){
		"no policy": nil,
		"no entry for the vendor": func() (preferences.AgentAccountPolicy, error) {
			return preferences.AgentAccountPolicy{Vendors: map[string]preferences.AccountVendorPolicy{
				string(agenthub.AgentTypeCodex): {Enabled: true},
			}}, nil
		},
	}
	for name, policy := range cases {
		t.Run(name, func(t *testing.T) {
			rig := newAccountRig(t, preferences.RefusalNone)
			rig.svc.AccountPolicy = policy
			sess := startSession(t, rig)
			if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: limitPrompt}); !errors.Is(err, agenthub.ErrAgentUsageLimit) {
				t.Fatalf("error = %v, want the usage limit", err)
			}
			if _, err := rig.book.Get(t.Context(), builtinMock); !errors.Is(err, agenthub.ErrAccountNotFound) {
				t.Errorf("the built-in account's row = %v, want none created for a vendor the person did not configure", err)
			}
		})
	}

	t.Run("an entry, switching off", func(t *testing.T) {
		rig := newAccountRig(t, preferences.RefusalNone)
		rig.svc.AccountPolicy = func() (preferences.AgentAccountPolicy, error) {
			return preferences.AgentAccountPolicy{Vendors: map[string]preferences.AccountVendorPolicy{
				string(agenthub.AgentTypeMock): {Enabled: false},
			}}, nil
		}
		sess := startSession(t, rig)
		if _, err := rig.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: limitPrompt}); !errors.Is(err, agenthub.ErrAgentUsageLimit) {
			t.Fatalf("error = %v, want the usage limit", err)
		}
		if !exhaustedUntil(t, rig, builtinMock).After(time.Now()) {
			t.Error("a configured vendor's account is marked even while switching is off")
		}
	})
}
