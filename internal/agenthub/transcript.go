package agenthub

import (
	"context"
	"encoding/json"
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
// It is safe for the single-goroutine update callback the runtime uses; it does
// no locking of its own because ACP delivers one notification at a time per
// session.
type Recorder struct {
	store TranscriptStore
	now   func() time.Time
}

func NewRecorder(store TranscriptStore) *Recorder {
	return &Recorder{store: store, now: time.Now}
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
		return r.store.AppendTranscript(ctx, r.newEntry(sessionID, update))
	}
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
	prev, ok, err := r.store.FindTranscriptKind(ctx, sessionID, update.Kind)
	if err != nil {
		return err
	}
	if !ok {
		return r.store.AppendTranscript(ctx, r.newEntry(sessionID, update))
	}
	payload, err := json.Marshal(update)
	if err != nil {
		return err
	}
	prev.Payload = payload
	prev.UpdatedAt = r.now()
	return r.store.UpdateTranscript(ctx, prev)
}

func (r *Recorder) recordChunk(ctx context.Context, sessionID string, update SessionUpdate) error {
	last, ok, err := r.store.LastTranscriptEntry(ctx, sessionID)
	if err != nil {
		return err
	}
	if ok && last.Kind == update.Kind && sameMessage(last, update) {
		last.Text += update.Text()
		last.UpdatedAt = r.now()
		return r.store.UpdateTranscript(ctx, last)
	}
	entry := r.newEntry(sessionID, update)
	entry.Text = update.Text()
	return r.store.AppendTranscript(ctx, entry)
}

func (r *Recorder) recordToolCall(ctx context.Context, sessionID string, update SessionUpdate) error {
	prev, ok, err := r.store.FindTranscriptToolCall(ctx, sessionID, update.ToolCall.ID)
	if err != nil {
		return err
	}
	if !ok {
		entry := r.newEntry(sessionID, update)
		entry.ToolCallID = update.ToolCall.ID
		return r.store.AppendTranscript(ctx, entry)
	}

	var stored SessionUpdate
	if err := json.Unmarshal(prev.Payload, &stored); err != nil || stored.ToolCall == nil {
		// A row we cannot read back is worse than a row we overwrite: keep the
		// newest state rather than leaving the UI with a call stuck in-flight.
		stored = update
	} else {
		stored.ToolCall = mergeToolCall(stored.ToolCall, update.ToolCall)
		stored.Raw = update.Raw
	}
	payload, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	prev.Payload = payload
	prev.UpdatedAt = r.now()
	return r.store.UpdateTranscript(ctx, prev)
}

// sameMessage keeps two consecutive assistant messages from folding into one
// bubble. Claude tags every chunk with a messageId (measured on
// claude-agent-acp 0.74); when the vendor sends none, kind alone is the best
// key available and the old behaviour stands.
func sameMessage(entry TranscriptEntry, update SessionUpdate) bool {
	if update.MessageID == "" {
		return true
	}
	var stored SessionUpdate
	if err := json.Unmarshal(entry.Payload, &stored); err != nil || stored.MessageID == "" {
		return true
	}
	return stored.MessageID == update.MessageID
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
