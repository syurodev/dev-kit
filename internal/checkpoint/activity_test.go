package checkpoint

import (
	"testing"
	"time"
)

func fixedClock(start time.Time) (*time.Time, func() time.Time) {
	now := start
	return &now, func() time.Time { return now }
}

// The point of the registry: a file nothing in a turn accounts for gets
// labelled "possibly another session" only when another session really was
// running at the time.
func TestOverlapFindsOnlyConcurrentSessions(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	clock, now := fixedClock(base)
	activity := &Activity{now: now}

	// Ended well before the window.
	earlier := activity.Begin("/proj", "s-earlier", KindTurn)
	*clock = base.Add(1 * time.Minute)
	earlier.End()

	// Runs across the window.
	*clock = base.Add(2 * time.Minute)
	during := activity.Begin("/proj", "s-during", KindTurn)

	// A different project, same time.
	other := activity.Begin("/other", "s-other", KindTurn)
	defer other.End()

	windowStart := base.Add(3 * time.Minute)
	*clock = base.Add(4 * time.Minute)
	windowEnd := *clock

	got := activity.Overlap("/proj", windowStart, windowEnd, "s-self")
	if len(got) != 1 || got[0].Session != "s-during" {
		t.Fatalf("overlap = %+v, want only s-during", got)
	}

	during.End()
	if got := activity.Overlap("/proj", windowStart, windowEnd); len(got) != 1 {
		t.Fatalf("overlap after End = %+v, want the span still remembered", got)
	}
	if got := activity.Overlap("/proj", windowStart, windowEnd, "s-during"); len(got) != 0 {
		t.Fatalf("overlap = %+v, want the excluded session left out", got)
	}
}

// A restore asks "is anyone writing in this project right now". A span that has
// not ended is the answer, however long ago it started.
func TestRunningInIgnoresFinishedSpans(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	clock, now := fixedClock(base)
	activity := &Activity{now: now}

	done := activity.Begin("/proj", "s-done", KindTurn)
	live := activity.Begin("/proj", "s-live", KindTurn)
	*clock = base.Add(time.Minute)
	done.End()

	got := activity.RunningIn("/proj")
	if len(got) != 1 || got[0].Session != "s-live" || got[0].Kind != KindTurn {
		t.Fatalf("running = %+v, want only s-live", got)
	}
	if got := activity.RunningIn("/proj", "s-live"); len(got) != 0 {
		t.Fatalf("running = %+v, want the caller's own span excluded", got)
	}
	live.End()
	if got := activity.RunningIn("/proj"); len(got) != 0 {
		t.Fatalf("running = %+v, want none", got)
	}
}

// Trimming must never forget a span that is still open: an agent can hold a
// turn for hours, and that is precisely the span a later question is about.
func TestTrimKeepsRunningSpansAndDropsOldOnes(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	clock, now := fixedClock(base)
	activity := &Activity{now: now}

	stale := activity.Begin("/proj", "s-stale", KindTurn)
	longRunning := activity.Begin("/proj", "s-long", KindTurn)
	*clock = base.Add(time.Minute)
	stale.End()

	*clock = base.Add(spanHistory + time.Hour)
	activity.Begin("/proj", "s-new", KindTurn).End()

	sessions := map[string]bool{}
	for _, span := range activity.Overlap("/proj", base, *clock) {
		sessions[span.Session] = true
	}
	if sessions["s-stale"] {
		t.Error("a span that ended beyond the history window was kept")
	}
	if !sessions["s-long"] {
		t.Error("a still-running span was trimmed")
	}
	longRunning.End()
}

// End on a nil handle is what lets every caller `defer h.End()` without asking
// whether checkpoints were on, and a second End must not move the timestamp.
func TestEndIsSafeAndIdempotent(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	clock, now := fixedClock(base)
	activity := &Activity{now: now}

	var absent *Handle
	absent.End()

	handle := activity.Begin("/proj", "s", KindRestore)
	*clock = base.Add(time.Minute)
	handle.End()
	first := handle.Span().End
	*clock = base.Add(time.Hour)
	handle.End()
	if handle.Span().End != first {
		t.Fatalf("End moved the timestamp: %v then %v", first, handle.Span().End)
	}
}
