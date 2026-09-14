package agenthub

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// TranscriptEntry is one persisted row of a session's conversation.
//
// Sessions and turns are operational records — they answer "what ran, what did
// it cost". The transcript answers "what was said", which is the only part a
// person needs back after a reload, and it is the part the hub used to lose
// entirely: session/update went straight to the UI event bus and nowhere else.
type TranscriptEntry struct {
	ID        string
	SessionID string
	Seq       int64
	Kind      UpdateKind
	// ToolCallID is set for tool-call entries. It is the merge key: ACP sends
	// tool_call once and then tool_call_update repeatedly for the same id, and
	// a transcript that appended each one would show the same call five times.
	ToolCallID string
	// Text holds the accumulated text of a chunk entry. Consecutive chunks of
	// the same kind coalesce into one row — chunks arrive per token, and one
	// row each would make the table grow faster than anything else in DevKit.
	Text string
	// Payload is the normalized SessionUpdate for non-chunk kinds.
	Payload   json.RawMessage
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TranscriptStore is the persistence half of the recorder. It is separate from
// Store only so tests can exercise coalescing without SQLite.
type TranscriptStore interface {
	AppendTranscript(ctx context.Context, e TranscriptEntry) error
	UpdateTranscript(ctx context.Context, e TranscriptEntry) error
	LastTranscriptEntry(ctx context.Context, sessionID string) (TranscriptEntry, bool, error)
	FindTranscriptToolCall(ctx context.Context, sessionID, toolCallID string) (TranscriptEntry, bool, error)
	FindTranscriptKind(ctx context.Context, sessionID string, kind UpdateKind) (TranscriptEntry, bool, error)
	ListTranscript(ctx context.Context, sessionID string) ([]TranscriptEntry, error)
	ListTranscriptPage(ctx context.Context, sessionID string, beforeSeq int64, limit int) (TranscriptPage, error)
}

// Recorder folds a stream of session/update notifications into transcript rows.
//
// Writes are batched, not write-through: a streamed message grows a tail held
// in memory and reaches SQLite on a cadence, when the message changes, when
// the turn ends (Invoke calls Flush), or when the session closes. Writing per
// token re-read the whole last row and rewrote the whole accumulated text for
// every chunk — O(len) per token, O(len²) per message; a 10k-token reply spent
// ~21s in SQLite (measured, BenchmarkRecordTenThousandTokens). The row itself
// is inserted when the message starts, so seq order is arrival order and a
// crash loses at most the tail since the last flush.
type Recorder struct {
	store TranscriptStore
	now   func() time.Time
	// FlushInterval is how long a dirty tail may sit in memory before a
	// background timer writes it out. Zero disables the timer; Flush itself
	// still fires at message change, turn end and close.
	FlushInterval time.Duration
	// OnWriteError reports a cadence flush that failed — the timer has no
	// caller to return to, but a lost tail must not be silent either.
	OnWriteError func(sessionID string, err error)

	mu       sync.Mutex
	sessions map[string]*recordSession
}

// recordSession is the per-session write state. One mutex on the Recorder
// guards the map and every session's state: the map itself needs it, updates
// for one session arrive from one goroutine, and the only cross-session
// contention is a flush writing while another session records — coarser than
// per-session locks and with nothing to win at this volume.
type recordSession struct {
	tail      *chunkTail
	tailDirty bool
	// tools holds only the calls with unflushed updates; a flushed call is
	// dropped and a late update re-reads the row, which bounds memory to the
	// in-flight calls instead of the session's whole history of big outputs.
	tools map[string]*toolTail
	timer *time.Timer
}

// chunkTail is the row a streaming message grows into. payload stays as
// inserted — the Text column is the mutable half, and rewriting the payload
// per token would be the same O(len²) in a different column.
type chunkTail struct {
	id        string
	kind      UpdateKind
	messageID string
	payload   json.RawMessage
	text      strings.Builder
	updatedAt time.Time
}

// toolTail is a tool call whose updates are still buffered. The call is merged
// in memory; lastRaw keeps the wire of the newest update so a flushed payload
// carries the same Raw the write-through path used to.
type toolTail struct {
	id        string
	kind      UpdateKind
	call      ToolCall
	lastRaw   json.RawMessage
	updatedAt time.Time
}

// DefaultFlushInterval is the cadence the service runs with: often enough that
// a reload mid-message is at most this far behind, rarely enough that a token
// stream costs one UPDATE per interval instead of one per token.
const DefaultFlushInterval = 250 * time.Millisecond

func NewRecorder(store TranscriptStore) *Recorder {
	return &Recorder{
		store:         store,
		now:           time.Now,
		sessions:      map[string]*recordSession{},
		FlushInterval: DefaultFlushInterval,
	}
}

// Flush writes out everything buffered for one session. Invoke calls it at
// turn end and CloseSession on close, so a normal exit never leaves a tail
// behind; the cadence timer calls it in between.
func (r *Recorder) Flush(ctx context.Context, sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.sessions[sessionID]
	if s == nil {
		return nil
	}
	return r.flushLocked(ctx, sessionID, s)
}

func (r *Recorder) flushLocked(ctx context.Context, sessionID string, s *recordSession) error {
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	var firstErr error
	if s.tail != nil && s.tailDirty {
		entry := TranscriptEntry{
			ID:        s.tail.id,
			SessionID: sessionID,
			Kind:      s.tail.kind,
			Text:      s.tail.text.String(),
			Payload:   s.tail.payload,
			UpdatedAt: r.now(),
		}
		if err := r.store.UpdateTranscript(ctx, entry); err != nil && firstErr == nil {
			firstErr = err
		}
		s.tailDirty = false
	}
	for id, tt := range s.tools {
		payload, err := json.Marshal(SessionUpdate{Kind: tt.kind, ToolCall: &tt.call, Raw: tt.lastRaw})
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		entry := TranscriptEntry{
			ID:         tt.id,
			SessionID:  sessionID,
			Kind:       tt.kind,
			ToolCallID: tt.call.ID,
			Payload:    payload,
			UpdatedAt:  r.now(),
		}
		if err := r.store.UpdateTranscript(ctx, entry); err != nil && firstErr == nil {
			firstErr = err
			continue
		}
		delete(s.tools, id)
	}
	return firstErr
}

// armTimerLocked schedules the cadence flush. One timer per session, armed on
// the first dirty write after a flush.
func (r *Recorder) armTimerLocked(sessionID string, s *recordSession) {
	if r.FlushInterval <= 0 || s.timer != nil {
		return
	}
	interval := r.FlushInterval
	s.timer = time.AfterFunc(interval, func() {
		if err := r.Flush(context.Background(), sessionID); err != nil && r.OnWriteError != nil {
			r.OnWriteError(sessionID, err)
		}
	})
}

func (r *Recorder) sessionLocked(sessionID string) *recordSession {
	s := r.sessions[sessionID]
	if s == nil {
		s = &recordSession{tools: map[string]*toolTail{}}
		r.sessions[sessionID] = s
	}
	return s
}

// Record persists one update. A kind the build does not model is still written:
// the row keeps Raw so nothing is silently lost, and the UI renders what it can.
func (r *Recorder) Record(ctx context.Context, sessionID string, update SessionUpdate) error {
	if r == nil || r.store == nil {
		return nil
	}
	switch {
	case update.IsChunk():
		return r.recordChunk(ctx, sessionID, update)
	case update.ToolCall != nil && update.ToolCall.ID != "":
		return r.recordToolCall(ctx, sessionID, update)
	case isSessionState(update.Kind):
		return r.recordSessionState(ctx, sessionID, update)
	default:
		return r.recordPlain(ctx, sessionID, update)
	}
}

// recordPlain flushes first on purpose: a plain row's seq must not overtake a
// still-buffered tail, or the transcript reorders itself on reload.
func (r *Recorder) recordPlain(ctx context.Context, sessionID string, update SessionUpdate) error {
	r.mu.Lock()
	s := r.sessionLocked(sessionID)
	if err := r.flushLocked(ctx, sessionID, s); err != nil {
		r.mu.Unlock()
		return err
	}
	if err := r.store.AppendTranscript(ctx, r.newEntry(sessionID, update)); err != nil {
		r.mu.Unlock()
		return err
	}
	retireTail(s)
	r.mu.Unlock()
	return nil
}

// retireTail drops the chunk tail after a non-chunk row has been appended
// behind it. The tail's row is no longer the session's last row, and folding a
// later chunk into it would move that text above rows the user already saw
// below it. The caller must have flushed first — the buffered text is real.
func retireTail(s *recordSession) {
	s.tail = nil
	s.tailDirty = false
}

// isSessionState reports whether a kind describes the session rather than the
// conversation. Usage, the command list and the current mode are each a single
// current value, not something that was said — the composer reads the latest
// usage, the command menu reads the latest list. Appending them would grow the
// transcript on every turn and force the UI to filter them back out. Measured:
// one failed session already produced two available_commands_update.
//
// config_option_update belongs here for the same reason and more strongly: each
// notification carries the entire set of options, and the agent republishes it
// on every model switch, effort change and plan-mode exit.
func isSessionState(kind UpdateKind) bool {
	switch kind {
	case UpdateUsage, UpdateAvailableCommands, UpdateCurrentMode, UpdateConfigOptions:
		return true
	default:
		return false
	}
}

// recordSessionState keeps exactly one row per session-level kind so a reload
// can restore the value without the transcript carrying its whole history.
func (r *Recorder) recordSessionState(ctx context.Context, sessionID string, update SessionUpdate) error {
	r.mu.Lock()
	s := r.sessionLocked(sessionID)
	if err := r.flushLocked(ctx, sessionID, s); err != nil {
		r.mu.Unlock()
		return err
	}
	prev, ok, err := r.store.FindTranscriptKind(ctx, sessionID, update.Kind)
	if err != nil {
		r.mu.Unlock()
		return err
	}
	if !ok {
		if err := r.store.AppendTranscript(ctx, r.newEntry(sessionID, update)); err != nil {
			r.mu.Unlock()
			return err
		}
		// The state row now sits below any chunk that follows; the tail's row
		// is no longer the last one.
		retireTail(s)
		r.mu.Unlock()
		return nil
	}
	payload, err := json.Marshal(update)
	if err != nil {
		r.mu.Unlock()
		return err
	}
	prev.Payload = payload
	prev.UpdatedAt = r.now()
	err = r.store.UpdateTranscript(ctx, prev)
	r.mu.Unlock()
	return err
}

func (r *Recorder) recordChunk(ctx context.Context, sessionID string, update SessionUpdate) error {
	text := update.Text()
	r.mu.Lock()
	s := r.sessionLocked(sessionID)
	if s.tail != nil && s.tail.kind == update.Kind && sameMessageID(s.tail.messageID, update.MessageID) {
		s.tail.text.WriteString(text)
		s.tail.updatedAt = r.now()
		s.tailDirty = true
		r.armTimerLocked(sessionID, s)
		r.mu.Unlock()
		return nil
	}
	// A new message starts: the old tail lands first, because its row already
	// holds an earlier seq and only its text is still growing in memory.
	if err := r.flushLocked(ctx, sessionID, s); err != nil {
		r.mu.Unlock()
		return err
	}
	entry := r.newEntry(sessionID, update)
	entry.Text = text
	if err := r.store.AppendTranscript(ctx, entry); err != nil {
		r.mu.Unlock()
		return err
	}
	tail := &chunkTail{
		id:        entry.ID,
		kind:      update.Kind,
		messageID: update.MessageID,
		payload:   entry.Payload,
		updatedAt: entry.UpdatedAt,
	}
	tail.text.WriteString(text)
	s.tail = tail
	s.tailDirty = true
	r.armTimerLocked(sessionID, s)
	r.mu.Unlock()
	return nil
}

func (r *Recorder) recordToolCall(ctx context.Context, sessionID string, update SessionUpdate) error {
	r.mu.Lock()
	s := r.sessionLocked(sessionID)
	if tt, ok := s.tools[update.ToolCall.ID]; ok {
		tt.call = *mergeToolCall(&tt.call, update.ToolCall)
		if len(update.Raw) > 0 {
			tt.lastRaw = update.Raw
		}
		tt.updatedAt = r.now()
		r.armTimerLocked(sessionID, s)
		r.mu.Unlock()
		return nil
	}
	if prev, ok, err := r.store.FindTranscriptToolCall(ctx, sessionID, update.ToolCall.ID); err != nil {
		r.mu.Unlock()
		return err
	} else if ok {
		// A call whose tail was already flushed: continue its row from the
		// stored state, the way every update used to be continued.
		var stored SessionUpdate
		if err := json.Unmarshal(prev.Payload, &stored); err != nil || stored.ToolCall == nil {
			// A row we cannot read back is worse than a row we overwrite: keep
			// the newest state rather than leaving the UI with a call stuck
			// in-flight.
			stored = update
		} else {
			stored.ToolCall = mergeToolCall(stored.ToolCall, update.ToolCall)
			stored.Raw = update.Raw
		}
		payload, err := json.Marshal(stored)
		if err != nil {
			r.mu.Unlock()
			return err
		}
		prev.Payload = payload
		prev.UpdatedAt = r.now()
		err = r.store.UpdateTranscript(ctx, prev)
		r.mu.Unlock()
		return err
	}
	// First sighting: flush the chunk tail, insert the row so seq order is
	// arrival order, and retire the tail — its row is no longer the session's
	// last, so a later chunk must not fold back into it. The updates that
	// follow this call are what gets batched.
	if err := r.flushLocked(ctx, sessionID, s); err != nil {
		r.mu.Unlock()
		return err
	}
	entry := r.newEntry(sessionID, update)
	entry.ToolCallID = update.ToolCall.ID
	if err := r.store.AppendTranscript(ctx, entry); err != nil {
		r.mu.Unlock()
		return err
	}
	retireTail(s)
	s.tools[update.ToolCall.ID] = &toolTail{
		id:        entry.ID,
		kind:      update.Kind,
		call:      *update.ToolCall,
		lastRaw:   update.Raw,
		updatedAt: entry.UpdatedAt,
	}
	r.armTimerLocked(sessionID, s)
	r.mu.Unlock()
	return nil
}

// sameMessageID keeps two assistant messages from folding into one bubble.
// Claude tags every chunk with a messageId (measured on claude-agent-acp
// 0.74); when the vendor sends none, kind alone is the best key available and
// the old behaviour stands.
func sameMessageID(tailMsgID, updateMsgID string) bool {
	if updateMsgID == "" {
		return true
	}
	if tailMsgID == "" {
		return true
	}
	return tailMsgID == updateMsgID
}

func (r *Recorder) newEntry(sessionID string, update SessionUpdate) TranscriptEntry {
	now := r.now()
	payload, err := json.Marshal(update)
	if err != nil {
		payload = nil
	}
	return TranscriptEntry{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Kind:      update.Kind,
		Payload:   payload,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// mergeToolCall applies a tool_call_update onto the state already recorded.
// ACP updates are partial by design — a status-only update carries no title,
// no content and no locations — so an empty field means "unchanged", never
// "cleared". Content and locations replace wholesale when present because the
// agent resends the full list, not a delta.
func mergeToolCall(prev, next *ToolCall) *ToolCall {
	if prev == nil {
		return next
	}
	if next == nil {
		return prev
	}
	merged := *prev
	if next.Title != "" {
		merged.Title = next.Title
	}
	if next.Kind != "" {
		merged.Kind = next.Kind
	}
	if next.Status != "" {
		merged.Status = next.Status
	}
	if len(next.Content) > 0 {
		merged.Content = next.Content
	}
	if len(next.Locations) > 0 {
		merged.Locations = next.Locations
	}
	if len(next.RawInput) > 0 {
		merged.RawInput = next.RawInput
	}
	if len(next.RawOutput) > 0 {
		merged.RawOutput = next.RawOutput
	}
	return &merged
}
