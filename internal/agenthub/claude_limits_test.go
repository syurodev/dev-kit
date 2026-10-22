package agenthub

import "testing"

func TestClaudeUnifiedWindowsBecomePercentWindows(t *testing.T) {
	// The event captured from the real CLI in Phase 0.
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
	cases := map[string]string{"0.79": "ok", "0.8": "warning", "0.93": "warning", "1": "reached"}
	for fraction, status := range cases {
		update := []byte(`{"sessionUpdate":"usage_update","used":1,"size":2,"_meta":{"_claude/rateLimit":{"status":"allowed",
			"unifiedWindows":{"five_hour":{"utilization":` + fraction + `,"resetsAt":1792653600}}}}}`)
		got := ParseSessionUpdate(update)
		if got.Usage == nil || len(got.Usage.Limits) != 1 || got.Usage.Limits[0].Status != status {
			t.Errorf("utilization %s: limits = %+v, want status %q", fraction, got.Usage, status)
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
	if len(labels) != len(want) {
		t.Fatalf("labels = %v, want %v", labels, want)
	}
	for i := range want {
		if labels[i] != want[i] {
			t.Fatalf("labels = %v, want %v", labels, want)
		}
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
	for name, windows := range map[string]string{
		"a string":      `"nope"`,
		"an array":      `[1,2]`,
		"null":          `null`,
		"empty":         `{}`,
		"bad window":    `{"five_hour":"x"}`,
		"bad utilation": `{"five_hour":{"utilization":"high"}}`,
	} {
		update := []byte(`{"sessionUpdate":"usage_update","used":10,"size":200000,"_meta":{"_claude/rateLimit":{
			"status":"allowed_warning","resetsAt":1790000000,"rateLimitType":"five_hour","utilization":82,"unifiedWindows":` + windows + `}}}`)
		got := ParseSessionUpdate(update) // must not panic
		if got.Usage == nil || len(got.Usage.Limits) != 1 || got.Usage.Limits[0].UsedPercent != 82 {
			t.Errorf("%s: limits = %+v, want the old single window at 82", name, got.Usage)
		}
	}
}
