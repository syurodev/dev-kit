package agenthub_test

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"devkit/internal/agenthub"
)

func parse(t *testing.T, raw string) agenthub.SessionUpdate {
	t.Helper()
	return agenthub.ParseSessionUpdate(json.RawMessage(raw))
}

func TestSessionInfoCarriesTheVendorsFailure(t *testing.T) {
	// DevKit asks for these (the air extension's sessionFailure capability), so
	// dropping them would be asking for something and throwing it away.
	update := parse(t, `{"sessionUpdate":"session_info_update","_meta":{"jetbrains":{"air":{"sessionFailure":{
		"id":"f1","revision":2,"category":"access","severity":"error","title":"Sign in to continue using Claude.",
		"details":"The session expired.","reason":"auth_required","actions":["login"]}}}}}`)

	if update.Failure == nil {
		t.Fatal("failure dropped")
	}
	if update.Failure.Title != "Sign in to continue using Claude." || update.Failure.Reason != "auth_required" {
		t.Fatalf("failure = %+v", update.Failure)
	}
	if len(update.Failure.Actions) != 1 || update.Failure.Actions[0] != "login" {
		t.Fatalf("actions = %v", update.Failure.Actions)
	}
	if update.Failure.Severity != "error" || update.Failure.Revision != 2 {
		t.Fatalf("failure = %+v", update.Failure)
	}
}

func TestSessionInfoTitleWithoutMetaIsStillATitle(t *testing.T) {
	update := parse(t, `{"sessionUpdate":"session_info_update","title":"Refund worker"}`)
	if update.Title != "Refund worker" || update.Failure != nil || update.Goal != nil || update.GoalCleared {
		t.Fatalf("update = %+v", update)
	}
}

func TestSessionInfoCarriesTheGoalAndItsClearing(t *testing.T) {
	set := parse(t, `{"sessionUpdate":"session_info_update","_meta":{"goal":{"objective":"green build","status":"active","iterations":3}}}`)
	if set.Goal == nil || set.Goal.Objective != "green build" || set.Goal.Iterations != 3 {
		t.Fatalf("goal = %+v", set.Goal)
	}
	if set.GoalCleared {
		t.Fatal("a set goal must not read as cleared")
	}

	// Cleared arrives as an explicit null, which nil alone cannot say: every
	// other session_info_update has no goal key at all.
	cleared := parse(t, `{"sessionUpdate":"session_info_update","_meta":{"goal":null}}`)
	if cleared.Goal != nil || !cleared.GoalCleared {
		t.Fatalf("goal = %+v cleared = %v", cleared.Goal, cleared.GoalCleared)
	}
}

func TestUsageCarriesTheSubscriptionsRateLimit(t *testing.T) {
	// The context window says nothing about the subscription: a turn can fit
	// and still be refused.
	update := parse(t, `{"sessionUpdate":"usage_update","used":10,"size":100,"_meta":{"_claude/rateLimit":{
		"status":"allowed_warning","resetsAt":1790000000,"rateLimitType":"five_hour","utilization":82}}}`)
	if update.Usage == nil || update.Usage.RateLimit == nil {
		t.Fatalf("usage = %+v", update.Usage)
	}
	limit := update.Usage.RateLimit
	if limit.Status != "allowed_warning" || limit.ResetsAt != 1790000000 || limit.LimitType != "five_hour" || limit.Utilization != 82 {
		t.Fatalf("rate limit = %+v", limit)
	}
}

func TestUsageWithoutARateLimitKeepsItsNumbers(t *testing.T) {
	update := parse(t, `{"sessionUpdate":"usage_update","used":10,"size":100}`)
	if update.Usage == nil || update.Usage.RateLimit != nil || update.Usage.Used != 10 {
		t.Fatalf("usage = %+v", update.Usage)
	}
}

func TestAccountSwitchNoticesAreTypedOnTheUpdate(t *testing.T) {
	sw := parse(t, `{"sessionUpdate":"session_info_update","_meta":{"devkit":{"accountSwitch":{"from":"claude-default","to":"claude-work","reason":"Weekly 95% ≥ 90%"}}}}`)
	if sw.Kind != agenthub.UpdateSessionInfo || sw.AccountNotice == nil {
		t.Fatalf("update = %+v, want a session_info_update with an account notice", sw)
	}
	if got := *sw.AccountNotice; got.Kind != "switch" || got.From != "claude-default" || got.To != "claude-work" || got.Reason != "Weekly 95% ≥ 90%" {
		t.Errorf("notice = %+v", got)
	}
	failed := parse(t, `{"sessionUpdate":"session_info_update","_meta":{"devkit":{"accountSwitchFailed":{"from":"a","to":"b","reason":"sign-in expired"}}}}`)
	if failed.AccountNotice == nil || failed.AccountNotice.Kind != "failed" {
		t.Errorf("notice = %+v, want kind failed", failed.AccountNotice)
	}
	if plain := parse(t, `{"sessionUpdate":"session_info_update","title":"x"}`); plain.AccountNotice != nil {
		t.Errorf("an ordinary info update has no notice: %+v", plain.AccountNotice)
	}
	if junk := parse(t, `{"sessionUpdate":"session_info_update","_meta":{"devkit":{"accountSwitch":"nope"}}}`); junk.AccountNotice != nil {
		t.Errorf("a malformed notice is no notice: %+v", junk.AccountNotice)
	}
	long := parse(t, `{"sessionUpdate":"session_info_update","_meta":{"devkit":{"accountSwitch":{"from":"a","to":"b","reason":"`+strings.Repeat("x", 600)+`"}}}}`)
	if long.AccountNotice == nil || utf8.RuneCountInString(long.AccountNotice.Reason) > 300 {
		t.Errorf("the reason is clipped to 300 runes: %+v", long.AccountNotice)
	}
}

// The parser must accept exactly what the service writes.
func TestAccountNoticeRoundTripsThroughTheRealWriter(t *testing.T) {
	cases := []struct {
		kind  string
		write json.RawMessage
	}{
		{"switch", agenthub.AccountSwitchUpdateForTest("claude-default", "claude-work", "Weekly 95% ≥ 90%")},
		{"failed", agenthub.AccountSwitchFailedUpdateForTest("claude-work", "codex-x", "sign-in expired")},
	}
	for _, c := range cases {
		got := agenthub.ParseSessionUpdate(c.write)
		if got.Kind != agenthub.UpdateSessionInfo || got.AccountNotice == nil {
			t.Fatalf("%s: parsed %+v from %s", c.kind, got, c.write)
		}
		if n := *got.AccountNotice; n.Kind != c.kind || n.From == "" || n.To == "" || n.Reason == "" {
			t.Errorf("%s: notice = %+v", c.kind, n)
		}
	}
	got := agenthub.ParseSessionUpdate(cases[0].write)
	if n := *got.AccountNotice; n.From != "claude-default" || n.To != "claude-work" || n.Reason != "Weekly 95% ≥ 90%" {
		t.Errorf("notice = %+v", n)
	}
}
