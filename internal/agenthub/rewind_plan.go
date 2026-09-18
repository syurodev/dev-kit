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

// planRewindCut works out both answers, reading only what it needs.
//
// Three bounded queries, not the whole transcript: a session with months of
// history must not pay for all of it because a dialog was opened — which is the
// reason the transcript is paged everywhere else, and this used to be the one
// place that forgot.
func planRewindCut(ctx context.Context, store TranscriptStore, sessionID, promptMessageID string) (rewindCut, error) {
	prompt, found, err := store.FindPromptEntry(ctx, sessionID, promptMessageID)
	if err != nil {
		return rewindCut{}, err
	}
	if !found {
		return rewindCut{}, ErrTranscriptEntryNotFound
	}
	cut := rewindCut{CutSeq: prompt.Seq, PromptText: prompt.Text}

	// The branch point: the newest thing the agent said on the main thread
	// before the named prompt. The query does the filtering, so the limit
	// counts candidates rather than rows.
	candidates, err := store.ForkCandidates(ctx, sessionID, prompt.Seq, forkCandidateLimit)
	if err != nil {
		return rewindCut{}, err
	}
	branchSeq := int64(0)
	for _, entry := range candidates {
		id := payloadMessageID(entry)
		if id == "" {
			continue
		}
		if cut.ForkMessageID == "" {
			cut.ForkMessageID = id
			branchSeq = entry.Seq
			continue
		}
		cut.Fallbacks = append(cut.Fallbacks, id)
	}

	// The cut moves back to the first prompt after the branch point. Between
	// the two sit the turns that produced nothing the vendor can branch at, and
	// a transcript that kept them would show prompts the forked session has
	// never heard. With no branch point at all the cut is the session's first
	// prompt: there is nothing to go back to but the beginning.
	from := int64(0)
	if cut.ForkMessageID != "" {
		from = branchSeq + 1
	}
	dropped, err := store.PromptsFrom(ctx, sessionID, from)
	if err != nil {
		return rewindCut{}, err
	}
	if len(dropped) == 0 {
		// The named prompt is below the branch point but the query found
		// nothing, which can only mean the row moved under us.
		return rewindCut{}, ErrTranscriptEntryNotFound
	}
	cut.CutSeq = dropped[0].Seq
	for _, entry := range dropped {
		id := payloadMessageID(entry)
		if id == "" {
			continue
		}
		cut.DroppedPrompts = append(cut.DroppedPrompts, id)
		if id != promptMessageID {
			cut.ExtraTurnsDropped++
		}
	}
	return cut, nil
}

// payloadMessageID reads the vendor's own message id off a transcript row. It
// lives in the payload rather than in a column because it is the vendor's, and
// only some rows carry one.
//
// The queries filter on it too. That is not the same job: SQL filtering is what
// makes a LIMIT count candidates instead of rows, and this is what keeps a
// store that does not filter from handing back a row with nothing to branch at.
func payloadMessageID(entry TranscriptEntry) string {
	if len(entry.Payload) == 0 {
		return ""
	}
	var body struct {
		MessageID string `json:"messageId"`
	}
	if err := json.Unmarshal(entry.Payload, &body); err != nil {
		return ""
	}
	return body.MessageID
}
