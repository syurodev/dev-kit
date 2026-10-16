package agenthub

import (
	"testing"

	"devkit/internal/codexapp"
)

func tokenUsageParams(window int64) map[string]any {
	return map[string]any{
		"threadId": "th", "turnId": "tu",
		"tokenUsage": map[string]any{
			"modelContextWindow": window,
			"total":              map[string]any{"totalTokens": 900, "inputTokens": 700, "cachedInputTokens": 500, "outputTokens": 200, "reasoningOutputTokens": 50},
			"last":               map[string]any{"totalTokens": 300, "inputTokens": 250, "cachedInputTokens": 200, "outputTokens": 50, "reasoningOutputTokens": 10},
		},
	}
}

func TestCodexTokenUsageCarriesTheSplit(t *testing.T) {
	got := translateOne(t, newCodexTranslator(), codexapp.MethodTokenUsageUpdated, tokenUsageParams(400000))
	if len(got) != 1 || got[0].Usage == nil {
		t.Fatalf("got %+v", got)
	}
	u := got[0].Usage
	if u.Used != 300 || u.Size != 400000 {
		t.Fatalf("meter = %d of %d, want the last request against the window", u.Used, u.Size)
	}
	if u.Tokens == nil || u.Tokens.Session.Total != 900 || u.Tokens.Session.Cached != 500 || u.Tokens.LastRequest.Reasoning != 10 {
		t.Fatalf("tokens = %+v", u.Tokens)
	}
}

// The plan's windows arrive on their own notification, and a usage_update
// replaces the last whole: sending the windows alone would blank the meter.
func TestCodexRateLimitsRideWithTheLastUsage(t *testing.T) {
	tr := newCodexTranslator()
	translateOne(t, tr, codexapp.MethodTokenUsageUpdated, tokenUsageParams(400000))
	got := translateOne(t, tr, codexapp.MethodRateLimitsUpdated, map[string]any{
		"rateLimits": map[string]any{
			"primary":   map[string]any{"usedPercent": 42, "windowDurationMins": 300, "resetsAt": 1_800_000_000},
			"secondary": map[string]any{"usedPercent": 91, "windowDurationMins": 10080},
		},
	})
	if len(got) != 1 || got[0].Usage == nil {
		t.Fatalf("got %+v", got)
	}
	u := got[0].Usage
	if u.Used != 300 || u.Size != 400000 || u.Tokens == nil {
		t.Fatalf("a rate-limit update dropped the usage it rides with: %+v", u)
	}
	if len(u.Limits) != 2 {
		t.Fatalf("limits = %+v", u.Limits)
	}
	if first := u.Limits[0]; first.Label != "5-hour" || first.UsedPercent != 42 || first.ResetsAt != 1_800_000_000 || first.Status != "ok" {
		t.Fatalf("first window = %+v", first)
	}
	if second := u.Limits[1]; second.Label != "Weekly" || second.Status != "warning" {
		t.Fatalf("second window = %+v", second)
	}
}

func TestCodexLimitReachedAndModelScopedNames(t *testing.T) {
	name, reached := "spark", "primary"
	limits := codexLimitWindows(codexapp.RateLimitSnapshot{
		LimitName:            &name,
		RateLimitReachedType: &reached,
		Primary:              &codexapp.RateLimitWindow{UsedPercent: 100},
	})
	if len(limits) != 1 || limits[0].Label != "spark · Plan" || limits[0].Status != "reached" {
		t.Fatalf("limits = %+v", limits)
	}
	for mins, want := range map[int64]string{300: "5-hour", 10080: "Weekly", 1440: "1-day", 120: "2-hour", 45: "45-minute", 0: "Plan"} {
		if got := codexWindowLabel(mins); got != want {
			t.Errorf("codexWindowLabel(%d) = %q, want %q", mins, got, want)
		}
	}
}

func TestCodexMcpStartupBuildsTheWholeList(t *testing.T) {
	tr := newCodexTranslator()
	send := func(name, status string, extra map[string]any) []SessionUpdate {
		p := map[string]any{"name": name, "status": status, "threadId": nil}
		for k, v := range extra {
			p[k] = v
		}
		return translateOne(t, tr, codexapp.MethodMcpStartupStatus, p)
	}
	send("devkit", "starting", nil)
	send("github", "starting", nil)
	got := send("devkit", "ready", nil)
	got = append(got, send("github", "failed", map[string]any{"error": "401", "failureReason": "reauthenticationRequired"})...)
	last := got[len(got)-1]
	if last.Kind != UpdateConnectors || len(last.Connectors) != 2 {
		t.Fatalf("got %+v", last)
	}
	if c := last.Connectors[0]; c.Name != "devkit" || c.State != "ready" {
		t.Fatalf("first = %+v, want the server's order kept and its state updated", c)
	}
	if c := last.Connectors[1]; c.Name != "github" || c.State != "failed" || c.Error != "401" || !c.NeedsLogin {
		t.Fatalf("second = %+v", c)
	}
}

func TestClaudeRateLimitIsReadAsAWindowToo(t *testing.T) {
	got := ParseSessionUpdate([]byte(`{"sessionUpdate":"usage_update","used":10,"size":200000,"_meta":{"_claude/rateLimit":{"status":"allowed_warning","resetsAt":1800000000,"rateLimitType":"seven_day_opus","utilization":83}}}`))
	if got.Usage == nil || len(got.Usage.Limits) != 1 {
		t.Fatalf("got %+v", got.Usage)
	}
	if l := got.Usage.Limits[0]; l.Label != "Weekly · Opus" || l.UsedPercent != 83 || l.Status != "warning" {
		t.Fatalf("limit = %+v", l)
	}
	if got.Usage.Tokens != nil {
		t.Fatal("Claude reports no token split; the summary must not be handed an empty one")
	}
}

// The servers start with the thread, before the session has a handler, so the
// runtime says what the translator kept once one attaches.
func TestCodexConnectorsStartedBeforeAnyHandlerAreAnnounced(t *testing.T) {
	tr := newCodexTranslator()
	if tr.connectorsUpdate() != nil {
		t.Fatal("nothing has reported, so there is nothing to announce")
	}
	translateOne(t, tr, codexapp.MethodMcpStartupStatus, map[string]any{"name": "devkit", "status": "ready"})
	got := ParseSessionUpdate(tr.connectorsUpdate())
	if got.Kind != UpdateConnectors || len(got.Connectors) != 1 || got.Connectors[0].State != "ready" {
		t.Fatalf("got %+v", got)
	}
}
