package agenthub

import (
	"testing"
	"time"
)

// The turn's updates arrive on the runtime's goroutine while invoke is still
// setting the prompt up, so "was this update after the prompt?" cannot be
// answered by looking at the collector's state when the update is processed:
// an update handed over before markPromptSent can reach observeUpdate after it.
// The arrival stamp is what decides, so PromptSent <= FirstUpdate holds even
// for that interleaving — otherwise the vendor is credited with the turn's own
// setup time.
func TestTurnTimingIgnoresAnUpdateReceivedBeforeThePrompt(t *testing.T) {
	timing := newTurnTiming(time.Now())

	// The runtime hands an update over…
	arrived := timing.elapsed()
	time.Sleep(2 * time.Millisecond)
	// …the prompt goes out while the handler is still working on it…
	timing.markPromptSent()
	time.Sleep(2 * time.Millisecond)
	// …and only now does the handler report what the update was.
	if timing.observeUpdate(UpdateAgentMessageChunk, arrived) {
		t.Fatal("an update that arrived before the prompt ended the turn")
	}

	got, first := timing.finish()
	if !first {
		t.Fatal("first finish did not answer")
	}
	if got.FirstUpdateSeen || got.FirstVisibleSeen {
		t.Fatalf("an update stamped before the prompt was counted: first_update_seen=%v first_visible_seen=%v",
			got.FirstUpdateSeen, got.FirstVisibleSeen)
	}
}

// The same collector still counts the first update that really did arrive after
// the prompt, so the guard above drops only what came too early.
func TestTurnTimingCountsAnUpdateThatArrivesAfterThePrompt(t *testing.T) {
	timing := newTurnTiming(time.Now())
	timing.markPromptSent()
	arrived := timing.elapsed()
	time.Sleep(2 * time.Millisecond)
	if !timing.observeUpdate(UpdateToolCall, arrived) {
		t.Fatal("the first drawable update after the prompt did not end the turn")
	}
	got, _ := timing.finish()
	if !got.FirstUpdateSeen || !got.FirstVisibleSeen {
		t.Fatalf("update after the prompt was not counted: %+v", got)
	}
	if got.FirstUpdate != arrived || got.FirstVisible != arrived {
		t.Fatalf("first update %v / first visible %v, want both %v", got.FirstUpdate, got.FirstVisible, arrived)
	}
	if got.FirstUpdate < got.PromptSent {
		t.Fatalf("first update %v is before the prompt %v", got.FirstUpdate, got.PromptSent)
	}
	if got.VisibleKind != UpdateToolCall {
		t.Fatalf("visible kind %q, want %q", got.VisibleKind, UpdateToolCall)
	}
}

// The turn's "first visible" phase is only honest if it means the same thing
// the transcript means by "Starting" ending: agent text, or a work row. The
// work kinds are the frontend's (modules/agenthub/turns.ts WORK_KINDS); this
// test is the seam that keeps them from drifting apart, because a work kind
// missing here reports a first visible update later than the person saw.
func TestVisibleTurnKindMatchesWhatTheTranscriptDraws(t *testing.T) {
	visible := []UpdateKind{
		// Agent text: the answer, "Writing".
		UpdateAgentMessageChunk,
		// turns.ts WORK_KINDS.
		UpdateAgentThoughtChunk,
		UpdateToolCall,
		UpdateToolCallUpdate,
		UpdateSubagentSpawned,
		UpdateSubagentState,
		UpdateAsyncTaskSpawned,
		UpdateAsyncTaskProgress,
		UpdateAsyncTaskState,
		UpdatePlan,
	}
	for _, kind := range visible {
		if !visibleTurnKind(kind) {
			t.Errorf("%q draws a row, so it ends Starting", kind)
		}
	}
	// Notes: a turn whose only update is one of these still reads "Starting",
	// which is why the timing logs first_visible_ms=-1 for it.
	notes := []UpdateKind{
		UpdateUserMessageChunk,
		UpdateAvailableCommands,
		UpdateCurrentMode,
		UpdateUsage,
		UpdateSessionInfo,
		UpdateConfigOptions,
		UpdateConnectors,
		UpdateProviderRetry,
		UpdateUnknown,
	}
	for _, kind := range notes {
		if visibleTurnKind(kind) {
			t.Errorf("%q is a note, not a row", kind)
		}
	}
}
