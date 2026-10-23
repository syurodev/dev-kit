package agenthub_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/preferences"
)

var (
	pickNow     = time.Unix(1_800_000_000, 0)
	pickBuiltin = agenthub.BuiltinAccountID(agenthub.AgentTypeClaude)
)

func pickAccount(id string, usage ...agenthub.LimitWindow) agenthub.AccountRecord {
	home := "/h/" + id
	if id == pickBuiltin {
		home = ""
	}
	return agenthub.AccountRecord{ID: id, Vendor: agenthub.AgentTypeClaude, Label: id, HomeDir: home, SignedIn: true, Usage: usage}
}

// pickWindow is a window that resets in six hours.
func pickWindow(label string, used float64) agenthub.LimitWindow {
	return agenthub.LimitWindow{Label: label, UsedPercent: used, ResetsAt: pickNow.Add(6 * time.Hour).Unix()}
}

func pickPolicy() preferences.AccountVendorPolicy {
	return preferences.AccountVendorPolicy{Enabled: true, ThresholdPercent: 90, UsageMaxAgeSeconds: 300, OnLimitRefusal: preferences.RefusalNone}
}

func TestPickAccount(t *testing.T) {
	exhausted := func(a agenthub.AccountRecord) agenthub.AccountRecord {
		a.ExhaustedUntil = pickNow.Add(time.Hour)
		return a
	}
	exhaustedFor := func(a agenthub.AccountRecord, d time.Duration) agenthub.AccountRecord {
		a.ExhaustedUntil = pickNow.Add(d)
		return a
	}
	signedOut := func(a agenthub.AccountRecord) agenthub.AccountRecord { a.SignedIn = false; return a }
	coolingFor := func(a agenthub.AccountRecord, d time.Duration) agenthub.AccountRecord {
		a.CooldownUntil = pickNow.Add(d)
		return a
	}
	unlabelled := func(a agenthub.AccountRecord) agenthub.AccountRecord { a.Label = ""; return a }

	weekly := pickPolicy()
	weekly.WeeklyThresholdPercent = 70
	orderCThenB := pickPolicy()
	orderCThenB.Order = []string{"c", "b"}
	orderC := pickPolicy()
	orderC.Order = []string{"c"}
	noThreshold := pickPolicy()
	noThreshold.ThresholdPercent = 0

	elapsed := agenthub.LimitWindow{Label: "5-hour", UsedPercent: 99, ResetsAt: pickNow.Add(-time.Minute).Unix()}
	noReset := agenthub.LimitWindow{Label: "5-hour", UsedPercent: 99}
	builtin := pickBuiltin

	type want struct {
		target     string
		switched   bool
		allLimited bool
		reason     string    // exact; "" means none
		retryAt    time.Time // only read when allLimited
	}
	cases := []struct {
		name       string
		policy     preferences.AccountVendorPolicy
		accounts   []agenthub.AccountRecord
		current    string
		switchedAt time.Time
		previous   string
		want       want
	}{
		// Early stays: Target is Current exactly as given.
		{
			name:     "disabled policy stays",
			policy:   preferences.AccountVendorPolicy{},
			accounts: []agenthub.AccountRecord{pickAccount(builtin, pickWindow("5-hour", 99)), pickAccount("b")},
			current:  builtin,
			want:     want{target: builtin},
		},
		{
			name:     "disabled policy keeps an empty current as given",
			policy:   preferences.AccountVendorPolicy{},
			accounts: []agenthub.AccountRecord{pickAccount(builtin, pickWindow("5-hour", 99)), pickAccount("b")},
			current:  "",
			want:     want{target: ""},
		},
		{
			name:     "one account stays",
			policy:   pickPolicy(),
			accounts: []agenthub.AccountRecord{pickAccount(builtin, pickWindow("5-hour", 99))},
			current:  builtin,
			want:     want{target: builtin},
		},
		{
			name:     "current not in the list stays and keeps its id as given",
			policy:   pickPolicy(),
			accounts: []agenthub.AccountRecord{pickAccount(builtin, pickWindow("5-hour", 99)), pickAccount("b")},
			current:  "ghost",
			want:     want{target: "ghost"},
		},

		// Thresholds and windows.
		{
			name:     "under threshold stays",
			policy:   pickPolicy(),
			accounts: []agenthub.AccountRecord{pickAccount(builtin, pickWindow("5-hour", 89)), pickAccount("b")},
			current:  builtin,
			want:     want{target: builtin},
		},
		{
			name:     "at threshold moves",
			policy:   pickPolicy(),
			accounts: []agenthub.AccountRecord{pickAccount(builtin, pickWindow("5-hour", 90)), pickAccount("b")},
			current:  builtin,
			want:     want{target: "b", switched: true, reason: "5-hour 90% ≥ 90%"},
		},
		{
			name:     "any window counts",
			policy:   pickPolicy(),
			accounts: []agenthub.AccountRecord{pickAccount(builtin, pickWindow("5-hour", 10), pickWindow("Weekly", 95)), pickAccount("b")},
			current:  builtin,
			want:     want{target: "b", switched: true, reason: "Weekly 95% ≥ 90%"},
		},
		{
			name:     "weekly threshold applies to a Weekly window",
			policy:   weekly,
			accounts: []agenthub.AccountRecord{pickAccount(builtin, pickWindow("5-hour", 80), pickWindow("Weekly", 75)), pickAccount("b")},
			current:  builtin,
			want:     want{target: "b", switched: true, reason: "Weekly 75% ≥ 70%"},
		},
		{
			name:     "weekly threshold applies to a 7-day window",
			policy:   weekly,
			accounts: []agenthub.AccountRecord{pickAccount(builtin, pickWindow("7-day", 75)), pickAccount("b")},
			current:  builtin,
			want:     want{target: "b", switched: true, reason: "7-day 75% ≥ 70%"},
		},
		{
			name:     "weekly threshold leaves the 5-hour window alone",
			policy:   weekly,
			accounts: []agenthub.AccountRecord{pickAccount(builtin, pickWindow("5-hour", 80), pickWindow("Weekly", 60)), pickAccount("b")},
			current:  builtin,
			want:     want{target: builtin},
		},
		{
			name:     "zero threshold falls back to 90: 89 stays",
			policy:   noThreshold,
			accounts: []agenthub.AccountRecord{pickAccount(builtin, pickWindow("5-hour", 89)), pickAccount("b")},
			current:  builtin,
			want:     want{target: builtin},
		},
		{
			name:     "zero threshold falls back to 90: 90 moves",
			policy:   noThreshold,
			accounts: []agenthub.AccountRecord{pickAccount(builtin, pickWindow("5-hour", 90)), pickAccount("b")},
			current:  builtin,
			want:     want{target: "b", switched: true, reason: "5-hour 90% ≥ 90%"},
		},
		{
			name:     "elapsed reset counts as reset",
			policy:   pickPolicy(),
			accounts: []agenthub.AccountRecord{pickAccount(builtin, elapsed), pickAccount("b")},
			current:  builtin,
			want:     want{target: builtin},
		},
		{
			name:     "window with no known reset stays live",
			policy:   pickPolicy(),
			accounts: []agenthub.AccountRecord{pickAccount(builtin, noReset), pickAccount("b")},
			current:  builtin,
			want:     want{target: "b", switched: true, reason: "5-hour 99% ≥ 90%"},
		},
		{
			name:     "a reset window does not hide a live one over the threshold",
			policy:   pickPolicy(),
			accounts: []agenthub.AccountRecord{pickAccount(builtin, elapsed, pickWindow("Weekly", 95)), pickAccount("b")},
			current:  builtin,
			want:     want{target: "b", switched: true, reason: "Weekly 95% ≥ 90%"},
		},

		// Choosing the target.
		{
			name:     "order decides the target",
			policy:   orderCThenB,
			accounts: []agenthub.AccountRecord{pickAccount(builtin, pickWindow("5-hour", 99)), pickAccount("b"), pickAccount("c")},
			current:  builtin,
			want:     want{target: "c", switched: true, reason: "5-hour 99% ≥ 90%"},
		},
		{
			name:     "a listed account comes before unlisted ones",
			policy:   orderC,
			accounts: []agenthub.AccountRecord{pickAccount(builtin, pickWindow("5-hour", 99)), pickAccount("u2"), pickAccount("u1"), pickAccount("c")},
			current:  builtin,
			want:     want{target: "c", switched: true, reason: "5-hour 99% ≥ 90%"},
		},
		{
			name:     "unlisted accounts keep input order after the listed ones",
			policy:   orderC,
			accounts: []agenthub.AccountRecord{pickAccount(builtin, pickWindow("5-hour", 99)), pickAccount("u2"), pickAccount("u1"), exhausted(pickAccount("c"))},
			current:  builtin,
			want:     want{target: "u2", switched: true, reason: "5-hour 99% ≥ 90%"},
		},
		{
			name:     "target with no usage reading is eligible",
			policy:   pickPolicy(),
			accounts: []agenthub.AccountRecord{pickAccount(builtin, pickWindow("5-hour", 99)), pickAccount("b")},
			current:  builtin,
			want:     want{target: "b", switched: true, reason: "5-hour 99% ≥ 90%"},
		},
		{
			name:     "target over threshold is skipped",
			policy:   pickPolicy(),
			accounts: []agenthub.AccountRecord{pickAccount(builtin, pickWindow("5-hour", 99)), pickAccount("b", pickWindow("5-hour", 95)), pickAccount("c", pickWindow("5-hour", 10))},
			current:  builtin,
			want:     want{target: "c", switched: true, reason: "5-hour 99% ≥ 90%"},
		},
		{
			name:     "signed-out target is skipped",
			policy:   pickPolicy(),
			accounts: []agenthub.AccountRecord{pickAccount(builtin, pickWindow("5-hour", 99)), signedOut(pickAccount("b")), pickAccount("c")},
			current:  builtin,
			want:     want{target: "c", switched: true, reason: "5-hour 99% ≥ 90%; skipped b (signed out)"},
		},
		{
			name:     "exhausted target is skipped",
			policy:   pickPolicy(),
			accounts: []agenthub.AccountRecord{pickAccount(builtin, pickWindow("5-hour", 99)), exhausted(pickAccount("b")), pickAccount("c")},
			current:  builtin,
			want:     want{target: "c", switched: true, reason: "5-hour 99% ≥ 90%"},
		},
		{
			name:     "empty current means the built-in",
			policy:   pickPolicy(),
			accounts: []agenthub.AccountRecord{pickAccount(builtin, pickWindow("5-hour", 99)), pickAccount("b")},
			current:  "",
			want:     want{target: "b", switched: true, reason: "5-hour 99% ≥ 90%"},
		},
		{
			name:     "exhausted current moves",
			policy:   pickPolicy(),
			accounts: []agenthub.AccountRecord{exhausted(pickAccount(builtin)), pickAccount("b")},
			current:  builtin,
			want:     want{target: "b", switched: true, reason: "usage limit reached"},
		},
		{
			name:     "signed-out current that is fine stays (nothing special-cases it)",
			policy:   pickPolicy(),
			accounts: []agenthub.AccountRecord{signedOut(pickAccount(builtin)), pickAccount("b")},
			current:  builtin,
			want:     want{target: builtin},
		},

		// The hold after a switch.
		{
			name:       "recent switch holds",
			policy:     pickPolicy(),
			accounts:   []agenthub.AccountRecord{pickAccount(builtin), pickAccount("b", pickWindow("5-hour", 95))},
			current:    "b",
			switchedAt: pickNow.Add(-time.Minute),
			previous:   builtin,
			want:       want{target: "b"},
		},
		{
			name:       "switch 5 minutes - 1 second ago still holds",
			policy:     pickPolicy(),
			accounts:   []agenthub.AccountRecord{pickAccount(builtin), pickAccount("b", pickWindow("5-hour", 95))},
			current:    "b",
			switchedAt: pickNow.Add(-5*time.Minute + time.Second),
			previous:   builtin,
			want:       want{target: "b"},
		},
		{
			name:       "switch exactly 5 minutes ago no longer holds",
			policy:     pickPolicy(),
			accounts:   []agenthub.AccountRecord{pickAccount(builtin), pickAccount("b", pickWindow("5-hour", 95))},
			current:    "b",
			switchedAt: pickNow.Add(-5 * time.Minute),
			previous:   builtin,
			want:       want{target: builtin, switched: true, reason: "5-hour 95% ≥ 90%"},
		},
		{
			name:       "recent switch does not hold against a real refusal",
			policy:     pickPolicy(),
			accounts:   []agenthub.AccountRecord{pickAccount(builtin), exhausted(pickAccount("b"))},
			current:    "b",
			switchedAt: pickNow.Add(-time.Minute),
			previous:   builtin,
			want:       want{target: builtin, switched: true, reason: "usage limit reached"},
		},

		// Returning to the account it left needs a margin under the threshold.
		{
			name:       "return to the built-in needs the margin (85 is refused)",
			policy:     pickPolicy(),
			accounts:   []agenthub.AccountRecord{pickAccount(builtin, pickWindow("5-hour", 85)), pickAccount("b", pickWindow("5-hour", 95))},
			current:    "b",
			switchedAt: pickNow.Add(-time.Hour),
			previous:   builtin,
			want:       want{target: "b", reason: "5-hour 95% ≥ 90%"},
		},
		{
			name:       "return to the built-in with the margin is fine (70)",
			policy:     pickPolicy(),
			accounts:   []agenthub.AccountRecord{pickAccount(builtin, pickWindow("5-hour", 70)), pickAccount("b", pickWindow("5-hour", 95))},
			current:    "b",
			switchedAt: pickNow.Add(-time.Hour),
			previous:   builtin,
			want:       want{target: builtin, switched: true, reason: "5-hour 95% ≥ 90%"},
		},
		{
			name:       "no previous account means no margin for the built-in (85 is fine)",
			policy:     pickPolicy(),
			accounts:   []agenthub.AccountRecord{pickAccount(builtin, pickWindow("5-hour", 85)), pickAccount("b", pickWindow("5-hour", 95))},
			current:    "b",
			switchedAt: pickNow.Add(-time.Hour),
			previous:   "",
			want:       want{target: builtin, switched: true, reason: "5-hour 95% ≥ 90%"},
		},
		{
			name:       "return needs threshold minus margin exactly: 80 against 90 is refused",
			policy:     pickPolicy(),
			accounts:   []agenthub.AccountRecord{pickAccount("a", pickWindow("5-hour", 80)), pickAccount("b", pickWindow("5-hour", 95))},
			current:    "b",
			switchedAt: pickNow.Add(-time.Hour),
			previous:   "a",
			want:       want{target: "b", reason: "5-hour 95% ≥ 90%"},
		},
		{
			name:       "return needs threshold minus margin exactly: 79 is accepted",
			policy:     pickPolicy(),
			accounts:   []agenthub.AccountRecord{pickAccount("a", pickWindow("5-hour", 79)), pickAccount("b", pickWindow("5-hour", 95))},
			current:    "b",
			switchedAt: pickNow.Add(-time.Hour),
			previous:   "a",
			want:       want{target: "a", switched: true, reason: "5-hour 95% ≥ 90%"},
		},
		{
			name:       "the margin applies to the weekly threshold: 60 against 70 is refused",
			policy:     weekly,
			accounts:   []agenthub.AccountRecord{pickAccount("a", pickWindow("Weekly", 60)), pickAccount("b", pickWindow("5-hour", 95))},
			current:    "b",
			switchedAt: pickNow.Add(-time.Hour),
			previous:   "a",
			want:       want{target: "b", reason: "5-hour 95% ≥ 90%"},
		},
		{
			name:       "the margin applies to the weekly threshold: 59 is accepted",
			policy:     weekly,
			accounts:   []agenthub.AccountRecord{pickAccount("a", pickWindow("Weekly", 59)), pickAccount("b", pickWindow("5-hour", 95))},
			current:    "b",
			switchedAt: pickNow.Add(-time.Hour),
			previous:   "a",
			want:       want{target: "a", switched: true, reason: "5-hour 95% ≥ 90%"},
		},

		// Nowhere to go.
		{
			name:     "nobody to go to stays and says why",
			policy:   pickPolicy(),
			accounts: []agenthub.AccountRecord{pickAccount(builtin, pickWindow("5-hour", 99)), pickAccount("b", pickWindow("5-hour", 97))},
			current:  builtin,
			want:     want{target: builtin, reason: "5-hour 99% ≥ 90%"},
		},
		{
			name:     "every account exhausted",
			policy:   pickPolicy(),
			accounts: []agenthub.AccountRecord{exhausted(pickAccount(builtin)), exhausted(pickAccount("b"))},
			current:  builtin,
			want:     want{target: builtin, allLimited: true, reason: "usage limit reached", retryAt: pickNow.Add(time.Hour)},
		},
		{
			name:     "a signed-out account that is not exhausted does not end the all-exhausted rule",
			policy:   pickPolicy(),
			accounts: []agenthub.AccountRecord{exhausted(pickAccount(builtin)), exhausted(pickAccount("b")), signedOut(pickAccount("c"))},
			current:  builtin,
			want:     want{target: builtin, allLimited: true, reason: "usage limit reached", retryAt: pickNow.Add(time.Hour)},
		},
		{
			name:   "a signed-out account is ignored for the retry time",
			policy: pickPolicy(),
			accounts: []agenthub.AccountRecord{
				exhausted(pickAccount(builtin)),
				exhaustedFor(pickAccount("b"), 2*time.Hour),
				exhaustedFor(signedOut(pickAccount("c")), 10*time.Minute),
			},
			current: builtin,
			want:    want{target: builtin, allLimited: true, reason: "usage limit reached", retryAt: pickNow.Add(time.Hour)},
		},

		// Cooldowns: a candidate is kept out while one runs, never the account
		// a session is already on.
		{
			name:     "a current account with a cooldown but otherwise fine stays",
			policy:   pickPolicy(),
			accounts: []agenthub.AccountRecord{coolingFor(pickAccount(builtin, pickWindow("5-hour", 10)), time.Minute), pickAccount("b")},
			current:  builtin,
			want:     want{target: builtin},
		},
		{
			name:     "a cooldown that ends exactly now makes the candidate eligible",
			policy:   pickPolicy(),
			accounts: []agenthub.AccountRecord{pickAccount(builtin, pickWindow("5-hour", 99)), coolingFor(pickAccount("b"), 0)},
			current:  builtin,
			want:     want{target: "b", switched: true, reason: "5-hour 99% ≥ 90%"},
		},
		{
			name:     "a cooldown in the past makes the candidate eligible",
			policy:   pickPolicy(),
			accounts: []agenthub.AccountRecord{pickAccount(builtin, pickWindow("5-hour", 99)), coolingFor(pickAccount("b"), -time.Minute)},
			current:  builtin,
			want:     want{target: "b", switched: true, reason: "5-hour 99% ≥ 90%"},
		},
		{
			name:   "a cooling alternative is never named, a signed-out one is",
			policy: pickPolicy(),
			accounts: []agenthub.AccountRecord{
				pickAccount(builtin, pickWindow("5-hour", 99)),
				signedOut(pickAccount("b")),
				coolingFor(pickAccount("c"), time.Minute),
				pickAccount("d"),
			},
			current: builtin,
			want:    want{target: "d", switched: true, reason: "5-hour 99% ≥ 90%; skipped b (signed out)"},
		},
		{
			name:   "a signed-out account with no label is named by its id",
			policy: pickPolicy(),
			accounts: []agenthub.AccountRecord{
				pickAccount(builtin, pickWindow("5-hour", 99)),
				unlabelled(signedOut(pickAccount("b"))),
				pickAccount("c"),
			},
			current: builtin,
			want:    want{target: "c", switched: true, reason: "5-hour 99% ≥ 90%; skipped b (signed out)"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := agenthub.PickAccount(agenthub.PickInput{
				Policy: tc.policy, Accounts: tc.accounts, Current: tc.current,
				Now: pickNow, SwitchedAt: tc.switchedAt, Previous: tc.previous,
			})
			if got.Target != tc.want.target || got.Switch != tc.want.switched || got.AllLimited != tc.want.allLimited {
				t.Fatalf("PickAccount = %+v, want target %q switch %v allLimited %v", got, tc.want.target, tc.want.switched, tc.want.allLimited)
			}
			if got.Reason != tc.want.reason {
				t.Errorf("Reason = %q, want %q", got.Reason, tc.want.reason)
			}
			if tc.want.allLimited && !got.RetryAt.Equal(tc.want.retryAt) {
				t.Errorf("RetryAt = %v, want %v", got.RetryAt, tc.want.retryAt)
			}
			if !tc.want.allLimited && !got.RetryAt.IsZero() {
				t.Errorf("RetryAt = %v, want zero when not all limited", got.RetryAt)
			}
		})
	}
}

func TestPickAccountAllLimitedNamesTheEarliestReturn(t *testing.T) {
	a, b := pickAccount(pickBuiltin), pickAccount("b")
	a.ExhaustedUntil = pickNow.Add(3 * time.Hour)
	b.ExhaustedUntil = pickNow.Add(time.Hour)
	got := agenthub.PickAccount(agenthub.PickInput{Policy: pickPolicy(), Accounts: []agenthub.AccountRecord{a, b}, Current: pickBuiltin, Now: pickNow})
	if !got.AllLimited || !got.RetryAt.Equal(pickNow.Add(time.Hour)) {
		t.Fatalf("got %+v, want AllLimited with RetryAt in one hour", got)
	}
}

// PickAccount orders a copy: the caller's slice, and the usage in it, stay as given.
func TestPickAccountDoesNotChangeItsInput(t *testing.T) {
	p := pickPolicy()
	p.Order = []string{"c", "b"}
	accounts := []agenthub.AccountRecord{
		pickAccount(pickBuiltin, pickWindow("5-hour", 99), pickWindow("Weekly", 40)),
		pickAccount("b", pickWindow("5-hour", 10)),
		pickAccount("c"),
	}
	before := make([]agenthub.AccountRecord, len(accounts))
	for i, a := range accounts {
		before[i] = a
		before[i].Usage = append([]agenthub.LimitWindow(nil), a.Usage...)
	}
	got := agenthub.PickAccount(agenthub.PickInput{Policy: p, Accounts: accounts, Current: pickBuiltin, Now: pickNow})
	if !got.Switch || got.Target != "c" {
		t.Fatalf("PickAccount = %+v, want a switch to c (the order must have been applied)", got)
	}
	if !reflect.DeepEqual(accounts, before) {
		t.Fatalf("PickAccount changed its input:\n got %+v\nwant %+v", accounts, before)
	}
}

func TestPickAccountSkipsAnAccountThatIsCoolingDown(t *testing.T) {
	cooling := pickAccount("b")
	cooling.CooldownUntil = pickNow.Add(time.Minute)
	got := agenthub.PickAccount(agenthub.PickInput{
		Policy:   pickPolicy(),
		Accounts: []agenthub.AccountRecord{pickAccount(pickBuiltin, pickWindow("5-hour", 99)), cooling, pickAccount("c")},
		Current:  pickBuiltin, Now: pickNow,
	})
	if !got.Switch || got.Target != "c" {
		t.Fatalf("got %+v, want a move to c (b is cooling down)", got)
	}
}

func TestPickAccountACooldownAloneIsNotAllLimited(t *testing.T) {
	exhausted := pickAccount(pickBuiltin)
	exhausted.ExhaustedUntil = pickNow.Add(time.Hour)
	cooling := pickAccount("b")
	cooling.CooldownUntil = pickNow.Add(time.Minute)
	got := agenthub.PickAccount(agenthub.PickInput{
		Policy: pickPolicy(), Accounts: []agenthub.AccountRecord{exhausted, cooling}, Current: pickBuiltin, Now: pickNow,
	})
	if got.AllLimited {
		t.Fatalf("got %+v: b has not reached a limit, so the turn must not be refused with \"every account reached its limit\"", got)
	}
	if got.Switch {
		t.Fatalf("got %+v: b is cooling down, it must not be chosen", got)
	}
	if got.Target != pickBuiltin || got.Reason != "usage limit reached" || !got.RetryAt.IsZero() {
		t.Fatalf("got %+v, want a stay on %q with reason %q and no RetryAt", got, pickBuiltin, "usage limit reached")
	}
}

func TestPickAccountNamesTheSignedOutAccountsItSkipped(t *testing.T) {
	out := pickAccount("b")
	out.SignedIn = false
	out.Label = "Work"
	got := agenthub.PickAccount(agenthub.PickInput{
		Policy:   pickPolicy(),
		Accounts: []agenthub.AccountRecord{pickAccount(pickBuiltin, pickWindow("5-hour", 99)), out, pickAccount("c")},
		Current:  pickBuiltin, Now: pickNow,
	})
	if !got.Switch || got.Target != "c" {
		t.Fatalf("got %+v, want a move to c", got)
	}
	if !strings.Contains(got.Reason, "Work") || !strings.Contains(got.Reason, "signed out") {
		t.Errorf("reason = %q, want it to say that Work was skipped because it is signed out", got.Reason)
	}
}
