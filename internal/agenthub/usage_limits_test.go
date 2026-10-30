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
		// A rateLimit-only event names the window and its verdict, no reading.
		{"a window without a reading keeps the percent it had and takes status and reset", &Usage{Limits: []LimitWindow{{Label: "5-hour", UsedPercent: 73, ResetsAt: future, Status: "ok"}}},
			ctx(2, LimitWindow{Label: "5-hour", ResetsAt: future + 60, Status: "warning", NoReading: true}),
			ctx(2, LimitWindow{Label: "5-hour", UsedPercent: 73, ResetsAt: future + 60, Status: "warning"})},
		{"a window without a reading and nothing before it stays as the vendor gave it", &Usage{Limits: []LimitWindow{week}},
			ctx(2, LimitWindow{Label: "5-hour", ResetsAt: future, Status: "ok", NoReading: true}),
			ctx(2, week, LimitWindow{Label: "5-hour", ResetsAt: future, Status: "ok", NoReading: true})},
		{"a window without a reading stays without one over a previous one without", &Usage{Limits: []LimitWindow{{Label: "5-hour", ResetsAt: future, NoReading: true}}},
			ctx(2, LimitWindow{Label: "5-hour", ResetsAt: future, Status: "warning", NoReading: true}),
			ctx(2, LimitWindow{Label: "5-hour", ResetsAt: future, Status: "warning", NoReading: true})},
		// Nothing says when a window with no reset time stops being true: it is
		// carried while the update says nothing about windows, not past one that does.
		{"a window with no reset time goes once an update brings other windows", &Usage{Limits: []LimitWindow{{Label: "Plan", UsedPercent: 30}, five}}, ctx(2, week), ctx(2, five, week)},
		{"a window with no reset time stays while the update brings none", &Usage{Limits: []LimitWindow{{Label: "Plan", UsedPercent: 30}}}, ctx(2), ctx(2, LimitWindow{Label: "Plan", UsedPercent: 30})},
		{"a rateLimit with no reset time is not carried past an update with windows", &Usage{RateLimit: &RateLimit{Status: "allowed"}}, ctx(2, week), ctx(2, week)},
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

func TestMergeSubagentUsage(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	future := now.Unix() + 3600
	five := LimitWindow{Label: "5-hour", UsedPercent: 40, ResetsAt: future, Status: "ok"}
	own := &Usage{Used: 500, Size: 1_000_000, Amount: 1.5, Currency: "USD", Tokens: &TokenBreakdown{}, Limits: []LimitWindow{five}}
	warn := &RateLimit{Status: "allowed_warning", LimitType: "five_hour", ResetsAt: future}

	t.Run("its windows join the session's and its context numbers are not taken", func(t *testing.T) {
		week := LimitWindow{Label: "Weekly", UsedPercent: 10, ResetsAt: future + 86400}
		got, ok := mergeSubagentUsage(own, Usage{Used: 3, Size: 200_000, Amount: 9, Limits: []LimitWindow{{Label: "5-hour", UsedPercent: 60, ResetsAt: future, Status: "ok"}, week}, RateLimit: warn}, now)
		want := Usage{Used: 500, Size: 1_000_000, Amount: 1.5, Currency: "USD", Tokens: &TokenBreakdown{},
			Limits: []LimitWindow{{Label: "5-hour", UsedPercent: 60, ResetsAt: future, Status: "ok"}, week}, RateLimit: warn}
		if !ok || !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v %+v\nwant %+v", ok, got, want)
		}
	})
	t.Run("an update with no windows changes nothing", func(t *testing.T) {
		if _, ok := mergeSubagentUsage(own, Usage{Used: 3, Size: 200_000}, now); ok {
			t.Fatal("a subagent's context-only usage was applied")
		}
	})
	t.Run("with no usage before it holds the windows alone", func(t *testing.T) {
		got, ok := mergeSubagentUsage(nil, Usage{Used: 3, Size: 200_000, Limits: []LimitWindow{five}}, now)
		if !ok || !reflect.DeepEqual(got, Usage{Limits: []LimitWindow{five}}) {
			t.Fatalf("got %v %+v", ok, got)
		}
	})
}

// Claude's rateLimit block with no utilization says which window it is about
// and its verdict; the window derived from it has no reading of its own.
func TestRateLimitWindowWithoutUtilizationHasNoReading(t *testing.T) {
	derive := func(utilization string) LimitWindow {
		raw := `{"sessionUpdate":"usage_update","used":1,"size":2,"_meta":{"_claude/rateLimit":{"status":"allowed","rateLimitType":"five_hour","resetsAt":1900000000` + utilization + `}}}`
		return ParseSessionUpdate([]byte(raw)).Usage.Limits[0]
	}
	if w := derive(""); !w.NoReading || w.Label != "5-hour" {
		t.Fatalf("window = %+v, want no reading", w)
	}
	if w := derive(`,"utilization":73`); w.NoReading || w.UsedPercent != 73 {
		t.Fatalf("window = %+v, want the reading", w)
	}
}
