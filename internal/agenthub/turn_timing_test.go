package agenthub_test

import (
	"bytes"
	"context"
	"log"
	"strings"
	"sync"
	"testing"

	"devkit/internal/agenthub"
)

// safeBuffer is a bytes.Buffer a runtime goroutine may write to while the test
// reads it.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// A turn says where its time went, in the order the phases happened, and the
// line it writes names the session and the kinds but never the prompt.
func TestTurnTimingMeasuresThePhasesInOrder(t *testing.T) {
	adapter := agenthub.NewMockAdapter()
	svc := newTestServiceWith(t, adapter)

	var (
		mu      sync.Mutex
		got     []agenthub.TurnTiming
		logs    safeBuffer
		restore = log.Writer()
	)
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(restore) })
	svc.OnTurnTiming = func(timing agenthub.TurnTiming) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, timing)
	}

	ctx := context.Background()
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// A string that must not reach the log: it is the prompt, and the timing
	// line is about phases, not about what the person said.
	const prompt = "PROMPT-MARKER-9f08e9-do-not-log-me"
	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: prompt}); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	timings := append([]agenthub.TurnTiming(nil), got...)
	mu.Unlock()
	if len(timings) != 1 {
		t.Fatalf("timings = %d, want exactly one per turn: %+v", len(timings), timings)
	}
	timing := timings[0]
	if timing.SessionID != sess.ID {
		t.Fatalf("session = %q, want %q", timing.SessionID, sess.ID)
	}
	if timing.Vendor != agenthub.AgentTypeMock {
		t.Fatalf("vendor = %q", timing.Vendor)
	}
	if !timing.FirstUpdateSeen || !timing.FirstVisibleSeen {
		t.Fatalf("the mock always sends an update: %+v", timing)
	}
	if timing.VisibleKind != agenthub.UpdateAgentMessageChunk {
		t.Fatalf("visible kind = %q, want %q", timing.VisibleKind, agenthub.UpdateAgentMessageChunk)
	}
	// The phases before the prompt are disjoint and sequential, and everything
	// after it is an offset from the same start, so these cannot cross.
	beforePrompt := timing.Authorize + timing.Claim + timing.CheckpointWait + timing.Runtime
	if beforePrompt > timing.PromptSent {
		t.Fatalf("phases before the prompt (%s) exceed it (%s): %+v", beforePrompt, timing.PromptSent, timing)
	}
	if timing.PromptSent > timing.FirstUpdate {
		t.Fatalf("first update (%s) arrived before the prompt was sent (%s)", timing.FirstUpdate, timing.PromptSent)
	}
	if timing.FirstUpdate > timing.FirstVisible {
		t.Fatalf("first visible (%s) before the first update (%s)", timing.FirstVisible, timing.FirstUpdate)
	}

	line := logs.String()
	if !strings.Contains(line, "agenthub: turn timing") {
		t.Fatalf("no timing line in the log: %q", line)
	}
	if !strings.Contains(line, "session="+sess.ID) {
		t.Fatalf("the line does not name the session: %q", line)
	}
	if !strings.Contains(line, "first_visible_kind=agent_message_chunk") {
		t.Fatalf("the line does not name the visible kind: %q", line)
	}
	if strings.Contains(line, prompt) {
		t.Fatalf("the line carries the prompt: %q", line)
	}
}

// A turn that never drew a row still says how far it got: the phases are
// logged at the end, and the phases it never reached read as -1 rather than as
// an instant update.
func TestTurnTimingFallsBackAtTurnEndWithoutAVisibleRow(t *testing.T) {
	adapter := agenthub.NewMockAdapter()
	svc := newTestServiceWith(t, adapter)

	var (
		mu      sync.Mutex
		got     []agenthub.TurnTiming
		logs    safeBuffer
		restore = log.Writer()
	)
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(restore) })
	svc.OnTurnTiming = func(timing agenthub.TurnTiming) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, timing)
	}

	ctx := context.Background()
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// The turn is refused before it ever runs: no update, no visible row.
	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "FAIL_TURN_PLAIN"}); err == nil {
		t.Fatal("want the refusal")
	}

	mu.Lock()
	timings := append([]agenthub.TurnTiming(nil), got...)
	mu.Unlock()
	if len(timings) != 1 {
		t.Fatalf("timings = %d, want one at the turn's end", len(timings))
	}
	if timings[0].FirstVisibleSeen || timings[0].FirstUpdateSeen {
		t.Fatalf("a refused turn drew nothing: %+v", timings[0])
	}
	line := logs.String()
	if !strings.Contains(line, "first_visible_ms=-1") || !strings.Contains(line, "first_visible_kind=-") {
		t.Fatalf("an unreached phase must say so: %q", line)
	}
}
