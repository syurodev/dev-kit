// internal/agenthub/rewind_plan.go
package agenthub

import (
	"context"
	"encoding/json"
)

// Where to cut, and where to branch.
//
// These are two different questions with two different answers, and conflating
// them is what makes a rewind lose messages or keep ones it should not.
//
//	the branch point  a message id the VENDOR knows, so it can copy its own
//	                  history up to there
//	the cut           a row number DEVKIT knows, so the transcript matches
//
// The branch point is the last thing the agent said on the main thread before
// the turn being rewound. The cut is the first prompt after it — which may be
// earlier than the turn the user named, because a turn that failed or was
// cancelled leaves a prompt with no reply, and the vendor has no message to
// branch at inside it. That turn goes too, and the dialog says so rather than
// letting the user find out afterwards.

// forkCandidateLimit bounds how far back a refused branch point is retried.
// The vendor decides what it can fork at; measurement (spike S1) says the last
// assistant message works, so this is for the case it does not.
const forkCandidateLimit = 5

// rewindCut is where a rewind lands.
type rewindCut struct {
	// ForkMessageID is the vendor message to branch at. Empty means there is
	// nothing before this turn — the rewind opens a fresh session instead.
	ForkMessageID string
	// Fallbacks are earlier branch points to try if the vendor refuses the
	// first, newest first.
	Fallbacks []string
	// CutSeq is the first transcript row to drop.
	CutSeq int64
	// DroppedPrompts are the prompts being cut, newest last. The first is the
	// one the user named; anything before it is a turn with no reply that has
	// to go too.
	DroppedPrompts []string
	// PromptText is the text of the turn the user named, for the composer when
	// they asked to edit and re-run.
	PromptText string
	// ExtraTurnsDropped counts prompts cut beyond the one the user named.
	ExtraTurnsDropped int
}

// planRewindCut reads a session's transcript and works out both answers.
//
// The whole transcript rather than a window: the branch point can be any
// distance back when several turns in a row failed, and a session's rows are
// what a reload already reads.
func planRewindCut(ctx context.Context, store TranscriptStore, sessionID, promptMessageID string) (rewindCut, error) {
	entries, err := store.ListTranscript(ctx, sessionID)
	if err != nil {
		return rewindCut{}, err
	}

	requested := -1
	for i, entry := range entries {
		if entry.Kind != UpdateUserMessageChunk {
			continue
		}
		if payloadMessageID(entry) == promptMessageID {
			requested = i
			break
		}
	}
	if requested < 0 {
		return rewindCut{}, ErrTranscriptEntryNotFound
	}

	cut := rewindCut{CutSeq: entries[requested].Seq, PromptText: entries[requested].Text}

	// The branch point: the newest thing the agent said on the main thread
	// before the named prompt.
	var candidates []string
	branchAt := -1
	for i := requested - 1; i >= 0 && len(candidates) < forkCandidateLimit; i-- {
		entry := entries[i]
		if entry.Kind != UpdateAgentMessageChunk {
			continue
		}
		id, origin := payloadMessageID(entry), payloadOrigin(entry)
		// A subagent's message is not a branch point: the vendor keeps it in
		// the subagent's own session (spike S4), so asking the parent to fork
		// at it names a message that session has never had.
		if id == "" || origin != "" {
			continue
		}
		if branchAt < 0 {
			branchAt = i
		}
		candidates = append(candidates, id)
	}
	if len(candidates) > 0 {
		cut.ForkMessageID = candidates[0]
		cut.Fallbacks = candidates[1:]
	}

	// The cut moves back to the first prompt after the branch point. Between
	// the two sit the turns that produced nothing the vendor can branch at, and
	// a transcript that kept them would show prompts the forked session has
	// never heard.
	start := 0
	if branchAt >= 0 {
		start = branchAt + 1
	}
	firstCut := requested
	for i := start; i <= requested; i++ {
		if entries[i].Kind == UpdateUserMessageChunk {
			firstCut = i
			break
		}
	}
	cut.CutSeq = entries[firstCut].Seq

	// Everything from the cut down goes, so every prompt at or below it is
	// dropped — the named one, the reply-less turns above it, and every turn
	// after it.
	for i := firstCut; i < len(entries); i++ {
		if entries[i].Kind != UpdateUserMessageChunk {
			continue
		}
		if id := payloadMessageID(entries[i]); id != "" {
			cut.DroppedPrompts = append(cut.DroppedPrompts, id)
			if id != promptMessageID {
				cut.ExtraTurnsDropped++
			}
		}
	}
	return cut, nil
}

// payloadMessageID and payloadOrigin read the vendor's own fields off a
// transcript row. They live in the payload rather than in columns because they
// are the vendor's, and only some rows carry them.
func payloadMessageID(entry TranscriptEntry) string { return payloadField(entry).MessageID }
func payloadOrigin(entry TranscriptEntry) string    { return payloadField(entry).Origin }

type rowFields struct {
	MessageID string `json:"messageId"`
	Origin    string `json:"origin"`
}

func payloadField(entry TranscriptEntry) rowFields {
	if len(entry.Payload) == 0 {
		return rowFields{}
	}
	var body rowFields
	if err := json.Unmarshal(entry.Payload, &body); err != nil {
		return rowFields{}
	}
	return body
}
