package preferences

import (
	"errors"
	"testing"
)

func TestAccountPolicyDefaultsAreFilledIn(t *testing.T) {
	got, err := NormalizeAccountPolicy(AgentAccountPolicy{Vendors: map[string]AccountVendorPolicy{"claude": {}}})
	if err != nil {
		t.Fatal(err)
	}
	v := got.Vendors["claude"]
	if v.ThresholdPercent != 90 || v.UsageMaxAgeSeconds != 300 || v.OnLimitRefusal != RefusalNone {
		t.Errorf("defaults = %+v, want 90 / 300 / none", v)
	}
}

func TestAccountPolicyRefusesWhatCannotBeStored(t *testing.T) {
	cases := map[string]AccountVendorPolicy{
		"threshold too low":   {ThresholdPercent: 10},
		"threshold over 100":  {ThresholdPercent: 101},
		"weekly out of range": {ThresholdPercent: 90, WeeklyThresholdPercent: 30},
		"max age too small":   {ThresholdPercent: 90, UsageMaxAgeSeconds: 5},
		"unknown refusal":     {ThresholdPercent: 90, OnLimitRefusal: "always"},
	}
	for name, v := range cases {
		_, err := NormalizeAccountPolicy(AgentAccountPolicy{Vendors: map[string]AccountVendorPolicy{"claude": v}})
		if !errors.Is(err, ErrInvalidAccountPolicy) {
			t.Errorf("%s: error = %v, want ErrInvalidAccountPolicy", name, err)
		}
	}
}

func TestAccountPolicyNeedsTheRiskAcknowledgementToEnable(t *testing.T) {
	_, err := NormalizeAccountPolicy(AgentAccountPolicy{Vendors: map[string]AccountVendorPolicy{"claude": {Enabled: true}}})
	if !errors.Is(err, ErrInvalidAccountPolicy) {
		t.Fatalf("enabling without the acknowledgement: error = %v, want ErrInvalidAccountPolicy", err)
	}
	_, err = NormalizeAccountPolicy(AgentAccountPolicy{RiskAcknowledgedAt: "2026-10-22T10:00:00Z", Vendors: map[string]AccountVendorPolicy{"claude": {Enabled: true}}})
	if err != nil {
		t.Fatalf("enabling with it: %v", err)
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
}
