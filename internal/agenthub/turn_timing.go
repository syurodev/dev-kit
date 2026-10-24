package agenthub

import (
	"log"
	"sync"
	"time"
)

// TurnTiming is where one turn's time went, from the moment invoke accepted it
// to the first row the transcript can draw. It is what separates "Starting"
// into the phases that can be fixed one at a time: waiting for the checkpoint,
// opening the agent, or the model's own time to first token.
//
// Durations are not comparable to each other in general. Authorize, Claim,
// CheckpointWait and Runtime happened one after another before the prompt, and
// PromptSent, FirstUpdate and FirstVisible are offsets from the turn's start,
// so Authorize+Claim+CheckpointWait+Runtime <= PromptSent <= FirstUpdate <=
// FirstVisible. A later task (P0.2) emits these to the UI; nothing here knows
// about a window.
type TurnTiming struct {
	SessionID string
	Vendor    AgentType

	// Authorize is the time from the turn's start to the session claim: the
	// session read, the descendant check, input validation and the document,
	// prompt and workspace-context preparation.
	Authorize time.Duration
	// Claim is how long the turn waited for the session's claim.
	Claim time.Duration
	// CheckpointWait is how long the opening snapshot blocked the prompt
	// (cp.Wait), not how long the snapshot itself ran.
	CheckpointWait time.Duration
	// Runtime is runtimeFor: reusing a live process, or resuming one.
	Runtime time.Duration
	// RuntimeResume is true when the process was opened rather than reused.
	RuntimeResume bool
	// PromptSent is the time from the turn's start until rt.Prompt was called.
	PromptSent time.Duration
	// FirstUpdate is the time until the vendor's first session/update, and
	// FirstUpdateSeen says whether one arrived at all.
	FirstUpdate     time.Duration
	FirstUpdateSeen bool
	// FirstVisible is the time until the first update the transcript draws as
	// work — what ends the "Starting" label. FirstVisibleSeen says whether one
	// arrived, because a zero Duration would otherwise read as an instant one.
	FirstVisible     time.Duration
	FirstVisibleSeen bool
	// VisibleKind is that update's kind; empty when none arrived.
	VisibleKind UpdateKind
}

// visibleTurnKind reports whether an update gives the turn something to draw,
// which is what ends "Starting": agent text (the answer, "Writing"), or a work
// row — the kinds in frontend modules/agenthub/turns.ts WORK_KINDS, which
// activityOf reads. Usage, session info, commands and retry notices are notes,
// not rows, and keep "Starting" up. Keep the two lists in step: a work kind
// the frontend draws but this does not count would report a first visible
// update later than the person's.
func visibleTurnKind(kind UpdateKind) bool {
	switch kind {
	case UpdateAgentMessageChunk, UpdateAgentThoughtChunk, UpdatePlan,
		UpdateToolCall, UpdateToolCallUpdate,
		UpdateSubagentSpawned, UpdateSubagentState,
		UpdateAsyncTaskSpawned, UpdateAsyncTaskProgress, UpdateAsyncTaskState:
		return true
	}
	return false
}

// turnTiming collects one turn's phases under a mutex because the turn's
// updates arrive on the runtime's goroutine while invoke is still setting the
// prompt up. The collector is finished once: at the first visible update (when
// "Starting" ends, which is the moment worth logging), or at the turn's end if
// the turn never drew a row.
type turnTiming struct {
	started   time.Time
	sessionID string
	vendor    AgentType

	mu             sync.Mutex
	authorize      time.Duration
	claim          time.Duration
	checkpointWait time.Duration
	runtime        time.Duration
	runtimeResume  bool
	promptSent     time.Duration
	firstUpdate    time.Duration
	firstUpdateOn  bool
	firstVisible   time.Duration
	firstVisibleOn bool
	visibleKind    UpdateKind
	finished       bool
}

func newTurnTiming(started time.Time, sessionID string, vendor AgentType) *turnTiming {
	return &turnTiming{started: started, sessionID: sessionID, vendor: vendor}
}

// elapsed is the time since the turn's start. It takes no lock: started never
// changes, and callers use it to stamp an update's arrival before parsing it.
func (t *turnTiming) elapsed() time.Duration { return time.Since(t.started) }

func (t *turnTiming) markAuthorize() {
	t.mu.Lock()
	t.authorize = t.elapsed()
	t.mu.Unlock()
}

func (t *turnTiming) addClaim(d time.Duration) {
	t.mu.Lock()
	t.claim = d
	t.mu.Unlock()
}

func (t *turnTiming) addCheckpointWait(d time.Duration) {
	t.mu.Lock()
	t.checkpointWait = d
	t.mu.Unlock()
}

func (t *turnTiming) addRuntime(d time.Duration, resumed bool) {
	t.mu.Lock()
	t.runtime = d
	t.runtimeResume = resumed
	t.mu.Unlock()
}

func (t *turnTiming) markPromptSent() {
	t.mu.Lock()
	t.promptSent = t.elapsed()
	t.mu.Unlock()
}

// observeUpdate records the first update overall and the first of a visible
// kind, both stamped with arrived (when the update reached the handler, before
// it was parsed or recorded). It reports whether this update was the first
// visible one, so the caller can finish the timing at that moment.
func (t *turnTiming) observeUpdate(kind UpdateKind, arrived time.Duration) (firstVisible bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.firstUpdateOn {
		t.firstUpdateOn = true
		t.firstUpdate = arrived
	}
	if !t.firstVisibleOn && visibleTurnKind(kind) {
		t.firstVisibleOn = true
		t.firstVisible = arrived
		t.visibleKind = kind
		return true
	}
	return false
}

// finish returns the turn's timings and whether this call was the one that
// finished them. Every later call is a no-op, so a turn logs exactly once.
func (t *turnTiming) finish() (TurnTiming, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finished {
		return TurnTiming{}, false
	}
	t.finished = true
	return TurnTiming{
		SessionID:        t.sessionID,
		Vendor:           t.vendor,
		Authorize:        t.authorize,
		Claim:            t.claim,
		CheckpointWait:   t.checkpointWait,
		Runtime:          t.runtime,
		RuntimeResume:    t.runtimeResume,
		PromptSent:       t.promptSent,
		FirstUpdate:      t.firstUpdate,
		FirstUpdateSeen:  t.firstUpdateOn,
		FirstVisible:     t.firstVisible,
		FirstVisibleSeen: t.firstVisibleOn,
		VisibleKind:      t.visibleKind,
	}, true
}

// finishTurnTiming emits a turn's timing once: the log line an operator reads,
// and OnTurnTiming for a caller that shows the phases. The line carries only
// ids, kinds and durations — never prompt text, attachment paths or tool
// payloads.
func (s *Service) finishTurnTiming(t *turnTiming) {
	timing, first := t.finish()
	if !first {
		return
	}
	log.Printf("agenthub: turn timing session=%s vendor=%s authorize_ms=%d claim_ms=%d checkpoint_wait_ms=%d runtime_ms=%d runtime=%s prompt_ms=%d first_update_ms=%d first_visible_ms=%d first_visible_kind=%s",
		timing.SessionID,
		timing.Vendor,
		timing.Authorize.Milliseconds(),
		timing.Claim.Milliseconds(),
		timing.CheckpointWait.Milliseconds(),
		timing.Runtime.Milliseconds(),
		runtimePhaseName(timing.RuntimeResume),
		timing.PromptSent.Milliseconds(),
		timingMS(timing.FirstUpdate, timing.FirstUpdateSeen),
		timingMS(timing.FirstVisible, timing.FirstVisibleSeen),
		visibleKindName(timing.VisibleKind),
	)
	if s.OnTurnTiming != nil {
		s.OnTurnTiming(timing)
	}
}

// timingMS is a phase's milliseconds, or -1 when the phase was not reached —
// the log's way of saying "no update" without it reading as an instant one.
func timingMS(d time.Duration, seen bool) int64 {
	if !seen {
		return -1
	}
	return d.Milliseconds()
}

func runtimePhaseName(resumed bool) string {
	if resumed {
		return "resume"
	}
	return "live"
}

func visibleKindName(kind UpdateKind) string {
	if kind == "" {
		return "-"
	}
	return string(kind)
}
