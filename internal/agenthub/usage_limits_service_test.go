package agenthub_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

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

const fiveHourWindow = `{"status":"allowed_warning","resetsAt":1900000000,"rateLimitType":"five_hour","utilization":82}`

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

	svc.HandleUpdateForTest(ctx, sess.ID, claudeUsage(100, fiveHourWindow))
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
