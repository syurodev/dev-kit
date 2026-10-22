package agenthub_test

import (
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/preferences"
)

var pickNow = time.Unix(1_800_000_000, 0)

func acct(id string, usage ...agenthub.LimitWindow) agenthub.AccountRecord {
	home := "/h/" + id
	if id == "claude-default" {
		home = ""
	}
	return agenthub.AccountRecord{ID: id, Vendor: agenthub.AgentTypeClaude, Label: id, HomeDir: home, SignedIn: true, Usage: usage}
}

func win(label string, used float64) agenthub.LimitWindow {
	return agenthub.LimitWindow{Label: label, UsedPercent: used, ResetsAt: pickNow.Add(6 * time.Hour).Unix()}
}

func policy() preferences.AccountVendorPolicy {
	return preferences.AccountVendorPolicy{Enabled: true, ThresholdPercent: 90, UsageMaxAgeSeconds: 300, OnLimitRefusal: preferences.RefusalNone}
}

func TestPickAccount(t *testing.T) {
	exhausted := func(a agenthub.AccountRecord) agenthub.AccountRecord {
		a.ExhaustedUntil = pickNow.Add(time.Hour)
		return a
	}
	signedOut := func(a agenthub.AccountRecord) agenthub.AccountRecord { a.SignedIn = false; return a }
	weekly := policy()
	weekly.WeeklyThresholdPercent = 70

	cases := []struct {
		name       string
		policy     preferences.AccountVendorPolicy
		accounts   []agenthub.AccountRecord
		current    string
		switchedAt time.Time
		previous   string
		want       string // expected Target
		wantSwitch bool
		allLimited bool
	}{
		{"disabled stays", preferences.AccountVendorPolicy{}, []agenthub.AccountRecord{acct("claude-default", win("5-hour", 99)), acct("b")}, "claude-default", time.Time{}, "", "claude-default", false, false},
		{"one account stays", policy(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 99))}, "claude-default", time.Time{}, "", "claude-default", false, false},
		{"under threshold stays", policy(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 89)), acct("b")}, "claude-default", time.Time{}, "", "claude-default", false, false},
		{"at threshold moves", policy(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 90)), acct("b")}, "claude-default", time.Time{}, "", "b", true, false},
		{"any window counts", policy(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 10), win("Weekly", 95)), acct("b")}, "claude-default", time.Time{}, "", "b", true, false},
		{"weekly threshold applies to weekly only", weekly, []agenthub.AccountRecord{acct("claude-default", win("5-hour", 80), win("Weekly", 75)), acct("b")}, "claude-default", time.Time{}, "", "b", true, false},
		{"weekly threshold leaves the 5-hour window alone", weekly, []agenthub.AccountRecord{acct("claude-default", win("5-hour", 80), win("Weekly", 60)), acct("b")}, "claude-default", time.Time{}, "", "claude-default", false, false},
		{"elapsed reset counts as reset", policy(), []agenthub.AccountRecord{acct("claude-default", agenthub.LimitWindow{Label: "5-hour", UsedPercent: 99, ResetsAt: pickNow.Add(-time.Minute).Unix()}), acct("b")}, "claude-default", time.Time{}, "", "claude-default", false, false},
		{"order decides the target", func() preferences.AccountVendorPolicy { p := policy(); p.Order = []string{"c", "b"}; return p }(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 99)), acct("b"), acct("c")}, "claude-default", time.Time{}, "", "c", true, false},
		{"unknown usage is eligible", policy(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 99)), acct("b")}, "claude-default", time.Time{}, "", "b", true, false},
		{"target over threshold is skipped", policy(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 99)), acct("b", win("5-hour", 95)), acct("c", win("5-hour", 10))}, "claude-default", time.Time{}, "", "c", true, false},
		{"signed-out target is skipped", policy(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 99)), signedOut(acct("b")), acct("c")}, "claude-default", time.Time{}, "", "c", true, false},
		{"exhausted current moves", policy(), []agenthub.AccountRecord{exhausted(acct("claude-default")), acct("b")}, "claude-default", time.Time{}, "", "b", true, false},
		{"exhausted target is skipped", policy(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 99)), exhausted(acct("b")), acct("c")}, "claude-default", time.Time{}, "", "c", true, false},
		{"empty current means the built-in", policy(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 99)), acct("b")}, "", time.Time{}, "", "b", true, false},
		{"recent switch holds", policy(), []agenthub.AccountRecord{acct("claude-default"), acct("b", win("5-hour", 95))}, "b", pickNow.Add(-time.Minute), "claude-default", "b", false, false},
		{"recent switch does not hold against a real refusal", policy(), []agenthub.AccountRecord{acct("claude-default"), exhausted(acct("b"))}, "b", pickNow.Add(-time.Minute), "claude-default", "claude-default", true, false},
		{"returning needs a margin", policy(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 85)), acct("b", win("5-hour", 95))}, "b", pickNow.Add(-time.Hour), "claude-default", "b", false, false},
		{"returning with a margin is fine", policy(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 70)), acct("b", win("5-hour", 95))}, "b", pickNow.Add(-time.Hour), "claude-default", "claude-default", true, false},
		{"nobody to go to stays", policy(), []agenthub.AccountRecord{acct("claude-default", win("5-hour", 99)), acct("b", win("5-hour", 97))}, "claude-default", time.Time{}, "", "claude-default", false, false},
		{"every account exhausted", policy(), []agenthub.AccountRecord{exhausted(acct("claude-default")), exhausted(acct("b"))}, "claude-default", time.Time{}, "", "claude-default", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := agenthub.PickAccount(agenthub.PickInput{
				Policy: tc.policy, Accounts: tc.accounts, Current: tc.current,
				Now: pickNow, SwitchedAt: tc.switchedAt, Previous: tc.previous,
			})
			if got.Target != tc.want || got.Switch != tc.wantSwitch || got.AllLimited != tc.allLimited {
				t.Fatalf("PickAccount = %+v, want target %q switch %v allLimited %v", got, tc.want, tc.wantSwitch, tc.allLimited)
			}
			if tc.wantSwitch && got.Reason == "" {
				t.Errorf("a switch must carry its reason")
			}
		})
	}
}

func TestPickAccountAllLimitedNamesTheEarliestReturn(t *testing.T) {
	a, b := acct("claude-default"), acct("b")
	a.ExhaustedUntil = pickNow.Add(3 * time.Hour)
	b.ExhaustedUntil = pickNow.Add(time.Hour)
	got := agenthub.PickAccount(agenthub.PickInput{Policy: policy(), Accounts: []agenthub.AccountRecord{a, b}, Current: "claude-default", Now: pickNow})
	if !got.AllLimited || !got.RetryAt.Equal(pickNow.Add(time.Hour)) {
		t.Fatalf("got %+v, want AllLimited with RetryAt in one hour", got)
	}
}
