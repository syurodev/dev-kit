package agenthub_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"devkit/internal/agenthub"
)

// claudeUsage is a usage_update as Claude sends it: the context counter always,
// the rate-limit block only now and then.
func claudeUsage(used int, rateLimit string) []byte {
	if rateLimit == "" {
		return []byte(fmt.Sprintf(`{"sessionUpdate":"usage_update","used":%d,"size":200000}`, used))
	}
	return []byte(fmt.Sprintf(`{"sessionUpdate":"usage_update","used":%d,"size":200000,"_meta":{"_claude/rateLimit":%s}}`, used, rateLimit))
}

// fiveHourWindow is Claude's rate-limit block for the 5-hour window, resetting a day from now.
func fiveHourWindow(status string, utilization int) string {
	resetsAt := time.Now().Add(24 * time.Hour).Unix()
	if utilization == 0 {
		return fmt.Sprintf(`{"status":%q,"resetsAt":%d,"rateLimitType":"five_hour"}`, status, resetsAt)
	}
	return fmt.Sprintf(`{"status":%q,"resetsAt":%d,"rateLimitType":"five_hour","utilization":%d}`, status, resetsAt, utilization)
}

// storedUsage is the session's one stored usage row, as a reload reads it.
func storedUsage(t *testing.T, st agenthub.Store, sessionID string) *agenthub.Usage {
	t.Helper()
	rows, err := st.ListTranscript(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	var found *agenthub.Usage
	for _, row := range rows {
		if row.Kind != agenthub.UpdateUsage {
			continue
		}
		if found != nil {
			t.Fatal("more than one stored usage row")
		}
		var u agenthub.SessionUpdate
		if err := json.Unmarshal(row.Payload, &u); err != nil {
			t.Fatal(err)
		}
		found = u.Usage
	}
	if found == nil {
		t.Fatal("no stored usage row")
	}
	return found
}

// Claude puts its rate-limit windows on some usage_updates only. The stored row
// is replaced by every usage_update, so one without them used to wipe the
// windows the previous one carried: Plan limits showed and was gone after a reload.
func TestStoredUsageKeepsLimitsALaterUpdateDoesNotCarry(t *testing.T) {
	svc, st := newServiceForPreviewTest(t, agenthub.NewMockAdapter())
	ctx := context.Background()
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	svc.HandleUpdateForTest(ctx, sess.ID, claudeUsage(100, fiveHourWindow("allowed_warning", 82)))
	svc.HandleUpdateForTest(ctx, sess.ID, claudeUsage(24000, ""))

	got := storedUsage(t, st, sess.ID)
	if got.Used != 24000 {
		t.Fatalf("used = %d, want the newest update's context", got.Used)
	}
	if len(got.Limits) != 1 || got.Limits[0].Label != "5-hour" || got.Limits[0].UsedPercent != 82 {
		t.Fatalf("limits = %+v, want the 5-hour window kept", got.Limits)
	}
	if got.RateLimit == nil || got.RateLimit.LimitType != "five_hour" {
		t.Fatalf("rateLimit = %+v, want it kept", got.RateLimit)
	}
}

// claude-agent-acp streams a subagent's updates on the parent's connection and
// the runtime tags them with their origin. A subagent's usage_update names its
// own context window; stored as the session's it would replace the real one.
func TestAnOriginTaggedUsageUpdateDoesNotReplaceTheSessionsUsage(t *testing.T) {
	svc, st := newServiceForPreviewTest(t, agenthub.NewMockAdapter())
	ctx := context.Background()
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	svc.HandleUpdateForTest(ctx, sess.ID, []byte(`{"sessionUpdate":"usage_update","used":500,"size":1000000}`))
	svc.HandleUpdateForTest(ctx, sess.ID, []byte(`{"sessionUpdate":"usage_update","used":3,"size":200000,"_devkit":{"origin":"a0549d"}}`))

	if got := storedUsage(t, st, sess.ID); got.Used != 500 || got.Size != 1000000 {
		t.Fatalf("stored usage = %+v, want the session's own", got)
	}
}

// A rate-limit block with no utilization names the window and its verdict and
// has no reading: it must not turn a real 73% into 0%.
func TestStoredUsageKeepsThePercentARateLimitOnlyUpdateDoesNotCarry(t *testing.T) {
	svc, st := newServiceForPreviewTest(t, agenthub.NewMockAdapter())
	ctx := context.Background()
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	svc.HandleUpdateForTest(ctx, sess.ID, claudeUsage(100, fiveHourWindow("allowed", 73)))
	svc.HandleUpdateForTest(ctx, sess.ID, claudeUsage(200, fiveHourWindow("allowed_warning", 0)))

	got := storedUsage(t, st, sess.ID)
	if len(got.Limits) != 1 || got.Limits[0].UsedPercent != 73 || got.Limits[0].Status != "warning" || got.Limits[0].NoReading {
		t.Fatalf("limits = %+v, want 73%% kept with the new status", got.Limits)
	}
}

// A subagent's usage_update: its context numbers are its own, its plan windows
// are the session's (it spends the same plan).
func TestAnOriginTaggedUsageUpdateKeepsOnlyItsPlanWindows(t *testing.T) {
	svc, st := newServiceForPreviewTest(t, agenthub.NewMockAdapter())
	ctx := context.Background()
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	tagged := func(used int, rateLimit string) []byte {
		meta := ""
		if rateLimit != "" {
			meta = `,"_meta":{"_claude/rateLimit":` + rateLimit + `}`
		}
		return []byte(fmt.Sprintf(`{"sessionUpdate":"usage_update","used":%d,"size":200000%s,"_devkit":{"origin":"a0549d"}}`, used, meta))
	}

	// Before the session has any usage row: the windows are kept, no context.
	svc.HandleUpdateForTest(ctx, sess.ID, tagged(3, fiveHourWindow("allowed", 40)))
	if got := storedUsage(t, st, sess.ID); got.Used != 0 || got.Size != 0 || len(got.Limits) != 1 || got.Limits[0].UsedPercent != 40 {
		t.Fatalf("stored usage = %+v, want the windows alone", got)
	}

	svc.HandleUpdateForTest(ctx, sess.ID, []byte(`{"sessionUpdate":"usage_update","used":500,"size":1000000}`))
	svc.HandleUpdateForTest(ctx, sess.ID, tagged(9, fiveHourWindow("allowed_warning", 82)))
	svc.HandleUpdateForTest(ctx, sess.ID, tagged(11, ""))

	got := storedUsage(t, st, sess.ID)
	if got.Used != 500 || got.Size != 1000000 {
		t.Fatalf("stored usage = %+v, want the session's own context", got)
	}
	if len(got.Limits) != 1 || got.Limits[0].UsedPercent != 82 {
		t.Fatalf("limits = %+v, want the subagent's window kept", got.Limits)
	}
}

// A subagent's context-only usage, with nothing before it, leaves no row: a
// row of zeros would stand in for the session's usage.
func TestAnOriginTaggedContextOnlyUsageLeavesNoRow(t *testing.T) {
	svc, st := newServiceForPreviewTest(t, agenthub.NewMockAdapter())
	ctx := context.Background()
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	svc.HandleUpdateForTest(ctx, sess.ID, []byte(`{"sessionUpdate":"usage_update","used":3,"size":200000,"_devkit":{"origin":"a0549d"}}`))
	rows, err := st.ListTranscript(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Kind == agenthub.UpdateUsage {
			t.Fatalf("a usage row was stored: %s", row.Payload)
		}
	}
}
