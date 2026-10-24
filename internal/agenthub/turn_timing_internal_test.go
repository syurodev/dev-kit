package agenthub

import "testing"

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
