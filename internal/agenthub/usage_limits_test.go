package agenthub

import (
	"reflect"
	"testing"
	"time"
)

func TestMergeUsageLimits(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	future := now.Unix() + 3600
	past := now.Unix() - 60
	five := LimitWindow{Label: "5-hour", UsedPercent: 40, ResetsAt: future, Status: "ok"}
	week := LimitWindow{Label: "Weekly", UsedPercent: 10, ResetsAt: future + 86400, Status: "ok"}
	warn := &RateLimit{Status: "allowed_warning", LimitType: "five_hour", ResetsAt: future, Utilization: 82}
	ctx := func(used int64, limits ...LimitWindow) Usage { return Usage{Used: used, Size: 100, Limits: limits} }

	cases := []struct {
		name string
		prev *Usage
		next Usage
		want Usage
	}{
		{"nothing before", nil, ctx(2), ctx(2)},
		{"empty before and after", &Usage{}, Usage{Used: 2}, Usage{Used: 2}},
		{"no limits after limits keeps them, context from the newest", &Usage{Used: 1, Size: 100, Limits: []LimitWindow{five}}, ctx(2),
			ctx(2, five)},
		{"new limits replace the window of the same label", &Usage{Limits: []LimitWindow{five, week}},
			ctx(2, LimitWindow{Label: "5-hour", UsedPercent: 55, ResetsAt: future, Status: "ok"}),
			ctx(2, LimitWindow{Label: "5-hour", UsedPercent: 55, ResetsAt: future, Status: "ok"}, week)},
		{"a window only the new update names is added", &Usage{Limits: []LimitWindow{five}}, ctx(2, week), ctx(2, five, week)},
		{"an expired window is dropped", &Usage{Limits: []LimitWindow{{Label: "5-hour", UsedPercent: 99, ResetsAt: past}, week}}, ctx(2), ctx(2, week)},
		{"an expired window is dropped when others arrive", &Usage{Limits: []LimitWindow{{Label: "5-hour", UsedPercent: 99, ResetsAt: past}}}, ctx(2, week), ctx(2, week)},
		{"a window without a reset time is kept", &Usage{Limits: []LimitWindow{{Label: "Spark", UsedPercent: 30}}}, ctx(2), ctx(2, LimitWindow{Label: "Spark", UsedPercent: 30})},
		{"rateLimit is kept while its window lasts", &Usage{RateLimit: warn}, ctx(2), Usage{Used: 2, Size: 100, RateLimit: warn}},
		{"rateLimit past its reset is dropped", &Usage{RateLimit: &RateLimit{Status: "allowed_warning", ResetsAt: past}}, ctx(2), ctx(2)},
		{"a rejected rateLimit is an event, not carried", &Usage{RateLimit: &RateLimit{Status: "rejected", ResetsAt: future}}, ctx(2), ctx(2)},
		{"a new rateLimit replaces the old", &Usage{RateLimit: warn}, Usage{Used: 2, RateLimit: &RateLimit{Status: "allowed"}}, Usage{Used: 2, RateLimit: &RateLimit{Status: "allowed"}}},
		{"tokens and cost come from the newest update only", &Usage{Amount: 1, Currency: "USD", Tokens: &TokenBreakdown{}, Limits: []LimitWindow{five}},
			Usage{Used: 2, Amount: 2}, Usage{Used: 2, Amount: 2, Limits: []LimitWindow{five}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := mergeUsage(c.prev, c.next, now)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got  %+v\nwant %+v", got, c.want)
			}
		})
	}
}

// The merge must not hand back the stored row's own slice: a later write into
// one would show in the other.
func TestMergeUsageDoesNotAliasThePrevious(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	prev := &Usage{Limits: []LimitWindow{{Label: "5-hour", UsedPercent: 40, ResetsAt: now.Unix() + 60}}}
	got := mergeUsage(prev, Usage{Used: 1}, now)
	got.Limits[0].UsedPercent = 99
	if prev.Limits[0].UsedPercent != 40 {
		t.Fatal("merged limits share storage with the previous usage")
	}
}
