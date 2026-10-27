package preferences

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func oneVendor(v AccountVendorPolicy) AgentAccountPolicy {
	return AgentAccountPolicy{Vendors: map[string]AccountVendorPolicy{"claude": v}}
}

func distinctIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("acct-%02d", i)
	}
	return ids
}

func TestAccountPolicyDefaultsAreFilledIn(t *testing.T) {
	got, err := NormalizeAccountPolicy(oneVendor(AccountVendorPolicy{}))
	if err != nil {
		t.Fatal(err)
	}
	v := got.Vendors["claude"]
	if v.ThresholdPercent != DefaultAccountThresholdPercent ||
		v.UsageMaxAgeSeconds != DefaultAccountUsageMaxAgeSeconds ||
		v.OnLimitRefusal != RefusalNone {
		t.Errorf("defaults = %+v, want %d / %d / %s", v, DefaultAccountThresholdPercent, DefaultAccountUsageMaxAgeSeconds, RefusalNone)
	}
}

func TestAccountPolicyBounds(t *testing.T) {
	cases := []struct {
		name string
		v    AccountVendorPolicy
		ok   bool
	}{
		{"threshold 49", AccountVendorPolicy{ThresholdPercent: 49}, false},
		{"threshold 50", AccountVendorPolicy{ThresholdPercent: 50}, true},
		{"threshold 100", AccountVendorPolicy{ThresholdPercent: 100}, true},
		{"threshold 101", AccountVendorPolicy{ThresholdPercent: 101}, false},
		{"threshold negative", AccountVendorPolicy{ThresholdPercent: -1}, false},
		{"weekly 0 means same", AccountVendorPolicy{WeeklyThresholdPercent: 0}, true},
		{"weekly 49", AccountVendorPolicy{WeeklyThresholdPercent: 49}, false},
		{"weekly 50", AccountVendorPolicy{WeeklyThresholdPercent: 50}, true},
		{"weekly 100", AccountVendorPolicy{WeeklyThresholdPercent: 100}, true},
		{"weekly 101", AccountVendorPolicy{WeeklyThresholdPercent: 101}, false},
		{"max age 59", AccountVendorPolicy{UsageMaxAgeSeconds: 59}, false},
		{"max age 60", AccountVendorPolicy{UsageMaxAgeSeconds: 60}, true},
		{"max age 3600", AccountVendorPolicy{UsageMaxAgeSeconds: 3600}, true},
		{"max age 3601", AccountVendorPolicy{UsageMaxAgeSeconds: 3601}, false},
		{"unknown refusal", AccountVendorPolicy{OnLimitRefusal: "always"}, false},
		{"refusal resend", AccountVendorPolicy{OnLimitRefusal: RefusalResendIfNoProgress}, true},
		{"refusal continue", AccountVendorPolicy{OnLimitRefusal: RefusalContinue}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NormalizeAccountPolicy(oneVendor(tc.v))
			if tc.ok {
				if err != nil {
					t.Fatalf("error = %v, want none", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidAccountPolicy) {
				t.Fatalf("error = %v, want ErrInvalidAccountPolicy", err)
			}
			if !strings.Contains(err.Error(), "claude") {
				t.Errorf("error %q does not name the vendor", err)
			}
		})
	}
}

func TestAccountPolicyUnknownRefusalListsTheChoices(t *testing.T) {
	_, err := NormalizeAccountPolicy(oneVendor(AccountVendorPolicy{OnLimitRefusal: "always"}))
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{RefusalNone, RefusalResendIfNoProgress, RefusalContinue} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not list %q", err, want)
		}
	}
}

func TestAccountPolicyVendorName(t *testing.T) {
	cases := []struct {
		name   string
		vendor string
		ok     bool
	}{
		{"empty", "", false},
		{"whitespace only", " \t ", false},
		{"33 characters", strings.Repeat("a", 33), false},
		{"32 characters", strings.Repeat("a", 32), true},
		{"32 multibyte characters", strings.Repeat("é", 32), true},
		{"33 multibyte characters", strings.Repeat("é", 33), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NormalizeAccountPolicy(AgentAccountPolicy{Vendors: map[string]AccountVendorPolicy{tc.vendor: {}}})
			if tc.ok && err != nil {
				t.Fatalf("error = %v, want none", err)
			}
			if !tc.ok && !errors.Is(err, ErrInvalidAccountPolicy) {
				t.Fatalf("error = %v, want ErrInvalidAccountPolicy", err)
			}
		})
	}
}

func TestAccountPolicyNeedsTheRiskAcknowledgementToEnable(t *testing.T) {
	const at = "2026-10-22T10:00:00Z"
	cases := []struct {
		name string
		ack  string
		on   bool
		ok   bool
	}{
		{"enabled without it", "", true, false},
		{"enabled with whitespace only", "  \t\n", true, false},
		{"enabled with it", at, true, true},
		{"disabled needs none", "", false, true},
		{"disabled with whitespace only", "   ", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := oneVendor(AccountVendorPolicy{Enabled: tc.on})
			in.RiskAcknowledgedAt = tc.ack
			_, err := NormalizeAccountPolicy(in)
			if tc.ok && err != nil {
				t.Fatalf("error = %v, want none", err)
			}
			if !tc.ok && !errors.Is(err, ErrInvalidAccountPolicy) {
				t.Fatalf("error = %v, want ErrInvalidAccountPolicy", err)
			}
		})
	}
}

func TestAccountPolicyOrderIsTrimmedAndDeduplicated(t *testing.T) {
	got, err := NormalizeAccountPolicy(AgentAccountPolicy{Vendors: map[string]AccountVendorPolicy{
		"codex": {Order: []string{" work ", "work", "", "home"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if o := got.Vendors["codex"].Order; len(o) != 2 || o[0] != "work" || o[1] != "home" {
		t.Errorf("order = %v, want [work home]", o)
	}
}

func TestAccountPolicyOrderCapAppliesAfterTrimAndDedupe(t *testing.T) {
	t.Run("32 distinct accepted", func(t *testing.T) {
		got, err := NormalizeAccountPolicy(oneVendor(AccountVendorPolicy{Order: distinctIDs(maxAccountOrder)}))
		if err != nil {
			t.Fatal(err)
		}
		if n := len(got.Vendors["claude"].Order); n != maxAccountOrder {
			t.Errorf("order length = %d, want %d", n, maxAccountOrder)
		}
	})
	t.Run("33 distinct refused", func(t *testing.T) {
		_, err := NormalizeAccountPolicy(oneVendor(AccountVendorPolicy{Order: distinctIDs(maxAccountOrder + 1)}))
		if !errors.Is(err, ErrInvalidAccountPolicy) {
			t.Fatalf("error = %v, want ErrInvalidAccountPolicy", err)
		}
	})
	t.Run("33 raw entries that collapse to 32 accepted", func(t *testing.T) {
		raw := distinctIDs(maxAccountOrder)
		raw = append(raw, " "+raw[0]+" ") // a duplicate once trimmed
		raw = append(raw, "  ")           // blank
		if len(raw) <= maxAccountOrder+1 {
			t.Fatalf("setup: %d raw entries, want more than %d", len(raw), maxAccountOrder+1)
		}
		got, err := NormalizeAccountPolicy(oneVendor(AccountVendorPolicy{Order: raw}))
		if err != nil {
			t.Fatal(err)
		}
		if n := len(got.Vendors["claude"].Order); n != maxAccountOrder {
			t.Errorf("order length = %d, want %d", n, maxAccountOrder)
		}
	})
}

func TestAccountPolicySetUsesRevisionCompareAndSwap(t *testing.T) {
	svc := NewService(NewMemoryRepository())
	first, err := svc.SetAgentAccountPolicy(AgentAccountPolicy{Revision: 0})
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != 1 {
		t.Fatalf("revision = %d, want 1", first.Revision)
	}
	if _, err := svc.SetAgentAccountPolicy(AgentAccountPolicy{Revision: 0}); !errors.Is(err, ErrAccountPolicyConflict) {
		t.Fatalf("stale revision: error = %v, want ErrAccountPolicyConflict", err)
	}
	got, err := svc.AgentAccountPolicy()
	if err != nil || got.Revision != 1 {
		t.Fatalf("read back = %+v, %v", got, err)
	}
	second, err := svc.SetAgentAccountPolicy(AgentAccountPolicy{Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision != 2 {
		t.Fatalf("revision = %d, want 2", second.Revision)
	}
	got, err = svc.AgentAccountPolicy()
	if err != nil || got.Revision != 2 {
		t.Fatalf("read back after second set = %+v, %v", got, err)
	}
}

func TestAccountPolicySetStoresTheNormalizedValue(t *testing.T) {
	svc := NewService(NewMemoryRepository())
	returned, err := svc.SetAgentAccountPolicy(oneVendor(AccountVendorPolicy{Order: []string{" work ", "work", "", "home"}}))
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.AgentAccountPolicy()
	if err != nil {
		t.Fatal(err)
	}
	v := got.Vendors["claude"]
	if v.ThresholdPercent != DefaultAccountThresholdPercent ||
		v.UsageMaxAgeSeconds != DefaultAccountUsageMaxAgeSeconds ||
		v.OnLimitRefusal != RefusalNone {
		t.Errorf("stored defaults = %+v", v)
	}
	if len(v.Order) != 2 || v.Order[0] != "work" || v.Order[1] != "home" {
		t.Errorf("stored order = %v, want [work home]", v.Order)
	}
	if r := returned.Vendors["claude"]; r.ThresholdPercent != v.ThresholdPercent || len(r.Order) != len(v.Order) {
		t.Errorf("returned %+v differs from stored %+v", r, v)
	}
}

func TestAccountPolicyReadsDoNotAlias(t *testing.T) {
	svc := NewService(NewMemoryRepository())
	if _, err := svc.SetAgentAccountPolicy(oneVendor(AccountVendorPolicy{Order: []string{"work", "home"}})); err != nil {
		t.Fatal(err)
	}
	first, err := svc.AgentAccountPolicy()
	if err != nil {
		t.Fatal(err)
	}
	first.Vendors["claude"].Order[0] = "changed"
	first.Vendors["other"] = AccountVendorPolicy{}
	delete(first.Vendors, "claude")

	again, err := svc.AgentAccountPolicy()
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Vendors) != 1 {
		t.Fatalf("vendors = %v, want only claude", again.Vendors)
	}
	if o := again.Vendors["claude"].Order; len(o) != 2 || o[0] != "work" || o[1] != "home" {
		t.Errorf("order = %v, want [work home]", o)
	}
}

func TestAccountPolicyStrategyDefaultsAndRefusesUnknown(t *testing.T) {
	got, err := NormalizeAccountPolicy(oneVendor(AccountVendorPolicy{}))
	if err != nil {
		t.Fatal(err)
	}
	if s := got.Vendors["claude"].Strategy; s != StrategyOrder {
		t.Errorf("strategy = %q, want %q", s, StrategyOrder)
	}
	for _, s := range []string{StrategyOrder, StrategyBest, StrategyConsumeFirst} {
		if _, err := NormalizeAccountPolicy(oneVendor(AccountVendorPolicy{Strategy: s})); err != nil {
			t.Errorf("%s refused: %v", s, err)
		}
	}
	_, err = NormalizeAccountPolicy(oneVendor(AccountVendorPolicy{Strategy: "random"}))
	if !errors.Is(err, ErrInvalidAccountPolicy) || !strings.Contains(err.Error(), StrategyConsumeFirst) {
		t.Errorf("unknown strategy: err = %v, want a refusal that lists the choices", err)
	}
}

func TestAccountPolicyAccountThresholds(t *testing.T) {
	got, err := NormalizeAccountPolicy(oneVendor(AccountVendorPolicy{AccountThresholds: map[string]int{" work ": 70, "": 60}}))
	if err != nil {
		t.Fatal(err)
	}
	if l := got.Vendors["claude"].AccountThresholds; len(l) != 1 || l["work"] != 70 {
		t.Errorf("limits = %v, want only work=70 (ids trimmed, empty id dropped)", l)
	}
	for _, bad := range []int{0, 49, 101} {
		_, err := NormalizeAccountPolicy(oneVendor(AccountVendorPolicy{AccountThresholds: map[string]int{"work": bad}}))
		if !errors.Is(err, ErrInvalidAccountPolicy) {
			t.Errorf("limit %d: err = %v, want a refusal", bad, err)
		}
	}
	many := map[string]int{}
	for _, id := range distinctIDs(maxAccountThresholds + 1) {
		many[id] = 80
	}
	if _, err := NormalizeAccountPolicy(oneVendor(AccountVendorPolicy{AccountThresholds: many})); !errors.Is(err, ErrInvalidAccountPolicy) {
		t.Errorf("too many limits: err = %v", err)
	}
	empty, err := NormalizeAccountPolicy(oneVendor(AccountVendorPolicy{AccountThresholds: map[string]int{}}))
	if err != nil || empty.Vendors["claude"].AccountThresholds != nil {
		t.Errorf("an empty map must normalise to none: %v %v", empty.Vendors["claude"].AccountThresholds, err)
	}
}

func TestAccountPolicyReadsDoNotAliasTheLimits(t *testing.T) {
	svc := NewService(NewMemoryRepository())
	if _, err := svc.SetAgentAccountPolicy(oneVendor(AccountVendorPolicy{AccountThresholds: map[string]int{"work": 70}})); err != nil {
		t.Fatal(err)
	}
	first, _ := svc.AgentAccountPolicy()
	first.Vendors["claude"].AccountThresholds["work"] = 99
	again, _ := svc.AgentAccountPolicy()
	if got := again.Vendors["claude"].AccountThresholds["work"]; got != 70 {
		t.Errorf("limit = %d, want 70: a caller's edit reached the store", got)
	}
}
