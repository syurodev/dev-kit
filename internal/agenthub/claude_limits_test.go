package agenthub

import (
	"strings"
	"testing"
)

func TestClaudeUnifiedWindowsBecomePercentWindows(t *testing.T) {
	// The event captured from Claude Code 2.1.308.
	update := []byte(`{"sessionUpdate":"usage_update","used":10,"size":200000,"_meta":{"_claude/rateLimit":{
		"status":"allowed","resetsAt":1792653600,"rateLimitType":"five_hour","overageStatus":"rejected",
		"overageDisabledReason":"org_level_disabled_until","isUsingOverage":false,
		"unifiedWindows":{"five_hour":{"utilization":0.69,"resetsAt":1792653600},"seven_day":{"utilization":0.15,"resetsAt":1793080800}}}}}`)
	got := ParseSessionUpdate(update)
	if got.Usage == nil || len(got.Usage.Limits) != 2 {
		t.Fatalf("limits = %+v, want the two unified windows", got.Usage)
	}
	want := []LimitWindow{
		{Label: "5-hour", UsedPercent: 69, ResetsAt: 1792653600, Status: "ok"},
		{Label: "Weekly", UsedPercent: 15, ResetsAt: 1793080800, Status: "ok"},
	}
	for i, w := range want {
		if got.Usage.Limits[i] != w {
			t.Errorf("window %d = %+v, want %+v", i, got.Usage.Limits[i], w)
		}
	}
}

func TestClaudeUnifiedWindowStatusFollowsThePercent(t *testing.T) {
	for _, c := range []struct{ fraction, status string }{
		{"0.79", "ok"}, {"0.8", "warning"}, {"0.93", "warning"}, {"1", "reached"},
	} {
		update := []byte(`{"sessionUpdate":"usage_update","used":1,"size":2,"_meta":{"_claude/rateLimit":{"status":"allowed",
			"unifiedWindows":{"five_hour":{"utilization":` + c.fraction + `,"resetsAt":1792653600}}}}}`)
		got := ParseSessionUpdate(update)
		if got.Usage == nil || len(got.Usage.Limits) != 1 || got.Usage.Limits[0].Status != c.status {
			t.Errorf("utilization %s: limits = %+v, want status %q", c.fraction, got.Usage, c.status)
		}
	}
}

func TestClaudeUnifiedWindowsAreListedInAFixedOrder(t *testing.T) {
	update := []byte(`{"sessionUpdate":"usage_update","used":1,"size":2,"_meta":{"_claude/rateLimit":{"status":"allowed",
		"unifiedWindows":{"zeta_extra":{"utilization":0.1,"resetsAt":1},"seven_day":{"utilization":0.2,"resetsAt":2},"five_hour":{"utilization":0.3,"resetsAt":3},"alpha_extra":{"utilization":0.4,"resetsAt":4}}}}}`)
	got := ParseSessionUpdate(update)
	var labels []string
	if got.Usage != nil {
		for _, w := range got.Usage.Limits {
			labels = append(labels, w.Label)
		}
	}
	want := []string{"5-hour", "Weekly", "alpha extra", "zeta extra"}
	if strings.Join(labels, "|") != strings.Join(want, "|") {
		t.Fatalf("labels = %v, want %v", labels, want)
	}
}

// usageWithRateLimit is a usage_update whose rate_limit_info has the given
// status and type, a top-level utilization of 82 and the given windows.
func usageWithRateLimit(status, limitType, windows string) []byte {
	return []byte(`{"sessionUpdate":"usage_update","used":1,"size":2,"_meta":{"_claude/rateLimit":{
		"status":"` + status + `","resetsAt":1790000000,"rateLimitType":"` + limitType + `","utilization":82,"unifiedWindows":` + windows + `}}}`)
}

func TestClaudeRejectedStatusMarksTheNamedUnifiedWindowReached(t *testing.T) {
	got := ParseSessionUpdate(usageWithRateLimit("rejected", "five_hour",
		`{"five_hour":{"utilization":0.99,"resetsAt":1792653600},"seven_day":{"utilization":0.15,"resetsAt":1793080800}}`))
	if got.Usage == nil || len(got.Usage.Limits) != 2 {
		t.Fatalf("limits = %+v", got.Usage)
	}
	if w := got.Usage.Limits[0]; w.Label != "5-hour" || w.UsedPercent != 99 || w.Status != "reached" {
		t.Errorf("5-hour window = %+v, want 99%% reached", w)
	}
	if w := got.Usage.Limits[1]; w.Status != "ok" {
		t.Errorf("weekly window = %+v, want it left ok", w)
	}
}

func TestClaudeRejectedStatusAddsAWindowTheMapDoesNotList(t *testing.T) {
	got := ParseSessionUpdate(usageWithRateLimit("rejected", "seven_day_opus",
		`{"five_hour":{"utilization":0.3,"resetsAt":1792653600}}`))
	if got.Usage == nil || len(got.Usage.Limits) != 2 {
		t.Fatalf("limits = %+v, want the listed window and the rejected one", got.Usage)
	}
	want := LimitWindow{Label: "Weekly · Opus", UsedPercent: 82, ResetsAt: 1790000000, Status: "reached"}
	if got.Usage.Limits[1] != want {
		t.Errorf("added window = %+v, want %+v", got.Usage.Limits[1], want)
	}
	if got.Usage.Limits[0].Status != "ok" {
		t.Errorf("listed window = %+v, want it left ok", got.Usage.Limits[0])
	}
}

func TestClaudeAllowedWarningRaisesAnOkWindowToWarning(t *testing.T) {
	got := ParseSessionUpdate(usageWithRateLimit("allowed_warning", "five_hour",
		`{"five_hour":{"utilization":0.5,"resetsAt":1792653600}}`))
	if got.Usage == nil || len(got.Usage.Limits) != 1 || got.Usage.Limits[0].Status != "warning" || got.Usage.Limits[0].UsedPercent != 50 {
		t.Fatalf("limits = %+v, want the 50%% window raised to warning", got.Usage)
	}
	got = ParseSessionUpdate(usageWithRateLimit("allowed_warning", "five_hour",
		`{"five_hour":{"utilization":1.2,"resetsAt":1792653600}}`))
	if got.Usage.Limits[0].Status != "reached" {
		t.Errorf("a reached window must stay reached, got %+v", got.Usage.Limits[0])
	}
}

func TestClaudeAllowedStatusChangesNothing(t *testing.T) {
	got := ParseSessionUpdate(usageWithRateLimit("allowed", "five_hour",
		`{"five_hour":{"utilization":0.5,"resetsAt":1792653600}}`))
	want := LimitWindow{Label: "5-hour", UsedPercent: 50, ResetsAt: 1792653600, Status: "ok"}
	if got.Usage == nil || len(got.Usage.Limits) != 1 || got.Usage.Limits[0] != want {
		t.Fatalf("limits = %+v, want only %+v", got.Usage, want)
	}
}

func TestClaudeUnifiedUtilizationScale(t *testing.T) {
	for _, c := range []struct {
		utilization string
		pct         float64
		status      string
	}{
		{"82", 82, "warning"},   // already a percentage: not 8200
		{"1.2", 120, "reached"}, // overage is a fraction above 1
		{"1.5", 150, "reached"}, // the last fraction
		{"250", 100, "reached"}, // a percentage is clamped
		{"-0.2", 0, "ok"},       // never below zero
	} {
		got := ParseSessionUpdate(usageWithRateLimit("allowed", "five_hour",
			`{"five_hour":{"utilization":`+c.utilization+`,"resetsAt":1792653600}}`))
		if got.Usage == nil || len(got.Usage.Limits) != 1 || got.Usage.Limits[0].UsedPercent != c.pct || got.Usage.Limits[0].Status != c.status {
			t.Errorf("utilization %s: limits = %+v, want %v%% %s", c.utilization, got.Usage, c.pct, c.status)
		}
	}
}

func TestClaudeUnifiedWindowWithoutUtilizationIsSkipped(t *testing.T) {
	got := ParseSessionUpdate(usageWithRateLimit("allowed", "five_hour",
		`{"five_hour":{"resetsAt":1792653600},"seven_day":{"utilization":0.15,"resetsAt":1793080800}}`))
	if got.Usage == nil || len(got.Usage.Limits) != 1 || got.Usage.Limits[0].Label != "Weekly" {
		t.Fatalf("limits = %+v, want only the weekly window", got.Usage)
	}
}

func TestClaudeUnifiedWindowsToleratesAFractionalResetsAt(t *testing.T) {
	got := ParseSessionUpdate(usageWithRateLimit("allowed", "five_hour",
		`{"five_hour":{"utilization":0.69,"resetsAt":1792653600.5},"seven_day":{"utilization":0.15,"resetsAt":1793080800}}`))
	if got.Usage == nil || len(got.Usage.Limits) != 2 || got.Usage.Limits[0].ResetsAt != 1792653600 || got.Usage.Limits[1].UsedPercent != 15 {
		t.Fatalf("limits = %+v, want both windows with the fractional time truncated", got.Usage)
	}
}

func TestClaudeRateLimitWithoutUnifiedWindowsIsReadAsBefore(t *testing.T) {
	update := []byte(`{"sessionUpdate":"usage_update","used":10,"size":200000,"_meta":{"_claude/rateLimit":{"status":"allowed_warning","resetsAt":1790000000,"rateLimitType":"five_hour","utilization":82}}}`)
	got := ParseSessionUpdate(update)
	if got.Usage == nil || len(got.Usage.Limits) != 1 || got.Usage.Limits[0].UsedPercent != 82 {
		t.Fatalf("limits = %+v, want the old single window at 82", got.Usage)
	}
}

func TestClaudeUnifiedWindowsWinOverATopLevelUtilization(t *testing.T) {
	update := []byte(`{"sessionUpdate":"usage_update","used":10,"size":200000,"_meta":{"_claude/rateLimit":{
		"status":"allowed_warning","resetsAt":1790000000,"rateLimitType":"five_hour","utilization":82,
		"unifiedWindows":{"five_hour":{"utilization":0.69,"resetsAt":1792653600},"seven_day":{"utilization":0.15,"resetsAt":1793080800}}}}}`)
	got := ParseSessionUpdate(update)
	if got.Usage == nil || len(got.Usage.Limits) != 2 || got.Usage.Limits[0].UsedPercent != 69 || got.Usage.Limits[1].UsedPercent != 15 {
		t.Fatalf("limits = %+v, want the two unified windows (69, 15), not the top-level 82", got.Usage)
	}
}

func TestClaudeUnreadableUnifiedWindowsFallBackToTheOldReading(t *testing.T) {
	for _, c := range []struct{ name, windows string }{
		{"a string", `"nope"`},
		{"an array", `[1,2]`},
		{"null", `null`},
		{"empty", `{}`},
		{"bad window", `{"five_hour":"x"}`},
		{"bad utilization", `{"five_hour":{"utilization":"high"}}`},
		{"no utilization", `{"five_hour":{"resetsAt":1792653600}}`},
	} {
		name, windows := c.name, c.windows
		update := []byte(`{"sessionUpdate":"usage_update","used":10,"size":200000,"_meta":{"_claude/rateLimit":{
			"status":"allowed_warning","resetsAt":1790000000,"rateLimitType":"five_hour","utilization":82,"unifiedWindows":` + windows + `}}}`)
		got := ParseSessionUpdate(update) // must not panic
		if got.Usage == nil || len(got.Usage.Limits) != 1 || got.Usage.Limits[0].UsedPercent != 82 {
			t.Errorf("%s: limits = %+v, want the old single window at 82", name, got.Usage)
		}
	}
}

func TestClaudeOverageIsNotAReachedWindow(t *testing.T) {
	update := []byte(`{"sessionUpdate":"usage_update","used":1,"size":2,"_meta":{"_claude/rateLimit":{
		"status":"allowed","rateLimitType":"five_hour","isUsingOverage":true,"overageStatus":"allowed",
		"unifiedWindows":{"five_hour":{"utilization":1.2,"resetsAt":1792653600},"seven_day":{"utilization":0.4,"resetsAt":1793080800}}}}}`)
	got := ParseSessionUpdate(update)
	if got.Usage == nil || len(got.Usage.Limits) != 2 {
		t.Fatalf("limits = %+v", got.Usage)
	}
	if status := got.Usage.Limits[0].Status; status != "warning" {
		t.Errorf("a window past 100%% while the plan goes on through overage is %q, want warning (the account still answers)", status)
	}
}

func TestClaudeOverageThatIsRejectedStaysReached(t *testing.T) {
	update := []byte(`{"sessionUpdate":"usage_update","used":1,"size":2,"_meta":{"_claude/rateLimit":{
		"status":"rejected","rateLimitType":"five_hour","isUsingOverage":true,
		"unifiedWindows":{"five_hour":{"utilization":1.2,"resetsAt":1792653600}}}}}`)
	if got := ParseSessionUpdate(update); got.Usage == nil || got.Usage.Limits[0].Status != "reached" {
		t.Fatalf("limits = %+v, want the rejected window reached", got.Usage)
	}
}

// An event with no status at all is no rate-limit block (parseRateLimit), but it
// can still say the plan goes on through overage: that keeps a window past 100%
// a warning. Only "rejected" makes it reached.
func TestClaudeOverageWithNoStatusIsNotAReachedWindow(t *testing.T) {
	update := []byte(`{"sessionUpdate":"usage_update","used":1,"size":2,"_meta":{"_claude/rateLimit":{
		"rateLimitType":"five_hour","isUsingOverage":true,
		"unifiedWindows":{"five_hour":{"utilization":1.2,"resetsAt":1792653600}}}}}`)
	got := ParseSessionUpdate(update)
	if got.Usage == nil || len(got.Usage.Limits) != 1 {
		t.Fatalf("limits = %+v", got.Usage)
	}
	if status := got.Usage.Limits[0].Status; status != "warning" {
		t.Errorf("status = %q, want warning: overage with no status is not a refusal", status)
	}
	if got.Usage.RateLimit != nil {
		t.Errorf("RateLimit = %+v, want none: no status means no rate-limit block", got.Usage.RateLimit)
	}
}
