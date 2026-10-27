package agenthub_test

import (
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/preferences"
)

func strategyPolicy(strategy string) preferences.AccountVendorPolicy {
	p := pickPolicy()
	p.Strategy = strategy
	return p
}

// resets is a window that resets after d.
func resets(label string, used float64, d time.Duration) agenthub.LimitWindow {
	return agenthub.LimitWindow{Label: label, UsedPercent: used, ResetsAt: pickNow.Add(d).Unix()}
}

func pickFrom(p preferences.AccountVendorPolicy, current string, accounts ...agenthub.AccountRecord) agenthub.PickDecision {
	return agenthub.PickAccount(agenthub.PickInput{Policy: p, Accounts: accounts, Current: current, Now: pickNow})
}

func TestPickAccountStrategyChoosesAmongEligibleTargets(t *testing.T) {
	full := pickAccount("a", pickWindow("Weekly", 100))
	// b has the most room but resets last, c resets soonest, d is in between.
	b := pickAccount("b", resets("5-hour", 5, time.Hour), resets("Weekly", 10, 120*time.Hour))
	c := pickAccount("c", resets("5-hour", 60, time.Hour), resets("Weekly", 70, 5*time.Hour))
	d := pickAccount("d", resets("5-hour", 30, time.Hour), resets("Weekly", 40, 48*time.Hour))
	over := pickAccount("e", resets("Weekly", 95, time.Hour)) // over the threshold: never a target

	for _, tc := range []struct{ strategy, want string }{
		{"", "d"}, {preferences.StrategyOrder, "d"}, // the person's order: the first eligible
		{preferences.StrategyBest, "b"},         // the lowest highest-window
		{preferences.StrategyConsumeFirst, "c"}, // the weekly window that resets soonest
	} {
		got := pickFrom(strategyPolicy(tc.strategy), "a", full, over, d, c, b)
		if !got.Switch || got.Target != tc.want {
			t.Errorf("strategy %q: %+v, want a switch to %s", tc.strategy, got, tc.want)
		}
	}
}

func TestPickAccountStrategyTiesAndUnknownsFallBackToTheOrder(t *testing.T) {
	full := pickAccount("a", pickWindow("Weekly", 100))
	unknown := pickAccount("u") // no reading
	known := pickAccount("k", resets("Weekly", 50, 24*time.Hour))
	noReset := pickAccount("n", agenthub.LimitWindow{Label: "Weekly", UsedPercent: 20})

	// A reading beats no reading for best; the order breaks a tie.
	if got := pickFrom(strategyPolicy(preferences.StrategyBest), "a", full, unknown, known); got.Target != "k" {
		t.Errorf("best: %+v, want the account with a reading", got)
	}
	// Without a known weekly reset an account is not preferred by consumeFirst.
	if got := pickFrom(strategyPolicy(preferences.StrategyConsumeFirst), "a", full, noReset, known); got.Target != "k" {
		t.Errorf("consumeFirst: %+v, want the account with a known reset", got)
	}
	if got := pickFrom(strategyPolicy(preferences.StrategyConsumeFirst), "a", full, unknown, noReset); got.Target != "u" {
		t.Errorf("consumeFirst with nothing known must keep the order: %+v", got)
	}
	twin1 := pickAccount("t1", resets("Weekly", 40, 24*time.Hour))
	twin2 := pickAccount("t2", resets("Weekly", 40, 24*time.Hour))
	for _, s := range []string{preferences.StrategyBest, preferences.StrategyConsumeFirst} {
		if got := pickFrom(strategyPolicy(s), "a", full, twin2, twin1); got.Target != "t2" {
			t.Errorf("%s: a tie must keep the order, got %+v", s, got)
		}
	}
}

func TestPickAccountStrategyNeverMovesAHealthyAccount(t *testing.T) {
	healthy := pickAccount("a", resets("Weekly", 30, 100*time.Hour))
	better := pickAccount("b", resets("Weekly", 1, time.Hour))
	for _, s := range []string{preferences.StrategyBest, preferences.StrategyConsumeFirst} {
		if got := pickFrom(strategyPolicy(s), "a", healthy, better); got.Switch {
			t.Errorf("%s moved an account that is under its threshold: %+v", s, got)
		}
	}
}

func TestPickAccountThresholdPerAccount(t *testing.T) {
	policy := pickPolicy()
	policy.AccountThresholds = map[string]int{"a": 70, "b": 60}
	a := pickAccount("a", pickWindow("5-hour", 75))
	b := pickAccount("b", pickWindow("5-hour", 65))
	c := pickAccount("c", pickWindow("5-hour", 65))

	// a is over its own limit (75 ≥ 70) though under the vendor's 90.
	got := pickFrom(policy, "a", a, b, c)
	if !got.Switch || got.Target != "c" || got.Reason != "5-hour 75% ≥ 70%" {
		t.Errorf("per-account limit not used: %+v", got)
	}
	// With no entry the vendor's threshold applies, to the account and as a target.
	if got := pickFrom(pickPolicy(), "a", a, b); got.Switch {
		t.Errorf("without per-account limits 75%% is under 90%%: %+v", got)
	}
	// The weekly threshold still replaces the limit for weekly windows.
	weekly := pickPolicy()
	weekly.WeeklyThresholdPercent = 80
	weekly.AccountThresholds = map[string]int{"a": 60}
	w := pickAccount("a", pickWindow("Weekly", 70))
	if got := pickFrom(weekly, "a", w, c); got.Switch {
		t.Errorf("weekly threshold 80 must win over the account's 60 for a weekly window: %+v", got)
	}
}
