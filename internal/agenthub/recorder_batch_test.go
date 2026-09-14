package agenthub_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devkit/internal/agenthub"
)

// countingStore delegates to a real store and counts writes, so the batching
// tests assert how few SQLite statements a streamed message costs, not just
// that the final rows are right.
type countingStore struct {
	agenthub.Store
	appends atomic.Int64
	updates atomic.Int64
}

func (s *countingStore) AppendTranscript(ctx context.Context, e agenthub.TranscriptEntry) error {
	s.appends.Add(1)
	return s.Store.AppendTranscript(ctx, e)
}

func (s *countingStore) UpdateTranscript(ctx context.Context, e agenthub.TranscriptEntry) error {
	s.updates.Add(1)
	return s.Store.UpdateTranscript(ctx, e)
}

func chunkUpdate(messageID, text string) agenthub.SessionUpdate {
	return agenthub.SessionUpdate{
		Kind:      agenthub.UpdateAgentMessageChunk,
		Content:   &agenthub.ContentBlock{Type: "text", Text: text},
		MessageID: messageID,
	}
}

// A streamed message used to cost one SELECT of the whole last row plus one
// UPDATE rewriting the whole accumulated text, per token. The batched tail
// costs one INSERT at message start and one UPDATE per flush.
func TestRecorderBatchesChunkWritesUntilFlush(t *testing.T) {
	store := &countingStore{Store: newTestStore(t)}
	ctx := context.Background()
	sid := seedSession(t, store)
	rec := agenthub.NewRecorder(store)

	for i := 0; i < 100; i++ {
		if err := rec.Record(ctx, sid, chunkUpdate("m1", "token ")); err != nil {
			t.Fatal(err)
		}
	}
	if got := store.appends.Load(); got != 1 {
		t.Fatalf("appends = %d, want 1 (the row appears when the message starts)", got)
	}
	if got := store.updates.Load(); got != 0 {
		t.Fatalf("updates = %d, want 0 before any flush", got)
	}

	// The stored row holds the first chunk only; the tail is memory.
	rows, err := store.ListTranscript(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Text != "token " {
		t.Fatalf("row before flush = %+v, want the first chunk only", rows)
	}

	if err := rec.Flush(ctx, sid); err != nil {
		t.Fatal(err)
	}
	if got := store.updates.Load(); got != 1 {
		t.Fatalf("updates = %d, want 1 for the whole flushed tail", got)
	}
	rows, err = store.ListTranscript(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Text != strings.Repeat("token ", 100) {
		t.Fatalf("row after flush = %d rows, text len %d, want one full message", len(rows), len(rows[0].Text))
	}
}

// A new kind or messageId starts a new row, which is also the moment the
// previous tail must land — seq order is insert order, and a delayed tail
// would reorder the transcript on reload.
func TestRecorderFlushesTheTailWhenTheMessageChanges(t *testing.T) {
	store := &countingStore{Store: newTestStore(t)}
	ctx := context.Background()
	sid := seedSession(t, store)
	rec := agenthub.NewRecorder(store)

	for i := 0; i < 3; i++ {
		if err := rec.Record(ctx, sid, chunkUpdate("m1", "a")); err != nil {
			t.Fatal(err)
		}
	}
	if err := rec.Record(ctx, sid, chunkUpdate("m2", "b")); err != nil {
		t.Fatal(err)
	}
	if got := store.updates.Load(); got != 1 {
		t.Fatalf("updates = %d, want the m1 tail flushed when m2 starts", got)
	}

	rows, err := store.ListTranscript(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Text != "aaa" || rows[1].Text != "b" {
		t.Fatalf("rows = %+v, want m1 fully written before m2's row", rows)
	}
}

func TestRecorderBatchesToolCallUpdatesUntilFlush(t *testing.T) {
	store := &countingStore{Store: newTestStore(t)}
	ctx := context.Background()
	sid := seedSession(t, store)
	rec := agenthub.NewRecorder(store)

	if err := rec.Record(ctx, sid, agenthub.SessionUpdate{
		Kind:     agenthub.UpdateToolCall,
		ToolCall: &agenthub.ToolCall{ID: "c1", Title: "Edit file", Kind: "edit", Status: "pending"},
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := rec.Record(ctx, sid, agenthub.SessionUpdate{
			Kind:     agenthub.UpdateToolCallUpdate,
			ToolCall: &agenthub.ToolCall{ID: "c1", Status: "in_progress"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if got := store.updates.Load(); got != 0 {
		t.Fatalf("updates = %d, want 0 before flush", got)
	}

	if err := rec.Record(ctx, sid, agenthub.SessionUpdate{
		Kind:     agenthub.UpdateToolCallUpdate,
		ToolCall: &agenthub.ToolCall{ID: "c1", Status: "completed", Title: "Edit file"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := rec.Flush(ctx, sid); err != nil {
		t.Fatal(err)
	}
	if got := store.updates.Load(); got != 1 {
		t.Fatalf("updates = %d, want 1 merged write", got)
	}

	entry, ok, err := store.FindTranscriptToolCall(ctx, sid, "c1")
	if err != nil || !ok {
		t.Fatalf("find tool call: ok=%v err=%v", ok, err)
	}
	var stored agenthub.SessionUpdate
	if err := json.Unmarshal(entry.Payload, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.ToolCall == nil || stored.ToolCall.Status != "completed" || stored.ToolCall.Title != "Edit file" {
		t.Fatalf("merged call = %+v, want completed with the title kept", stored.ToolCall)
	}
}

// The cadence flush: a tail left alone must reach SQLite on the timer, without
// anyone calling Flush.
func TestRecorderFlushesOnTheCadence(t *testing.T) {
	store := &countingStore{Store: newTestStore(t)}
	ctx := context.Background()
	sid := seedSession(t, store)
	rec := agenthub.NewRecorder(store)
	rec.FlushInterval = 5 * time.Millisecond

	for i := 0; i < 10; i++ {
		if err := rec.Record(ctx, sid, chunkUpdate("m1", "x")); err != nil {
			t.Fatal(err)
		}
	}
	// Wait for the end state, not for the first write: under -race a timer
	// can fire between two Records and land only part of the tail, and the
	// next cadence brings the rest.
	var rows []agenthub.TranscriptEntry
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		if rows, err = store.ListTranscript(ctx, sid); err != nil {
			t.Fatal(err)
		}
		if len(rows) == 1 && rows[0].Text == "xxxxxxxxxx" {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("row after cadence = %+v, want the full tail", rows)
}

// flakyStore fails the next n transcript updates, the way a briefly locked
// database does.
type flakyStore struct {
	agenthub.Store
	failUpdates atomic.Int64
}

func (s *flakyStore) UpdateTranscript(ctx context.Context, e agenthub.TranscriptEntry) error {
	if s.failUpdates.Add(-1) >= 0 {
		return errors.New("database is locked")
	}
	return s.Store.UpdateTranscript(ctx, e)
}

// A failed flush used to mark the tail clean anyway, so the retry had nothing
// to write — and a new message starting behind a failed flush returned early
// and dropped its own first chunk.
func TestRecorderRetriesATailWhoseWriteFailed(t *testing.T) {
	store := &flakyStore{Store: newTestStore(t)}
	ctx := context.Background()
	sid := seedSession(t, store)
	rec := agenthub.NewRecorder(store)
	rec.FlushInterval = 0

	for _, token := range []string{"a", "b"} {
		if err := rec.Record(ctx, sid, chunkUpdate("m1", token)); err != nil {
			t.Fatal(err)
		}
	}
	store.failUpdates.Store(1)
	if err := rec.Flush(ctx, sid); err == nil {
		t.Fatal("flush against a failing store reported success")
	}
	if err := rec.Flush(ctx, sid); err != nil {
		t.Fatalf("retry: %v", err)
	}
	rows, err := store.ListTranscript(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Text != "ab" {
		t.Fatalf("rows after retry = %+v, want the whole tail", rows)
	}

	if err := rec.Record(ctx, sid, chunkUpdate("m1", "c")); err != nil {
		t.Fatal(err)
	}
	store.failUpdates.Store(1)
	// The error is reported, but the chunk that triggered the flush is kept.
	_ = rec.Record(ctx, sid, chunkUpdate("m2", "x"))
	if err := rec.Record(ctx, sid, chunkUpdate("m2", "y")); err != nil {
		t.Fatal(err)
	}
	if err := rec.Flush(ctx, sid); err != nil {
		t.Fatal(err)
	}
	rows, err = store.ListTranscript(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Text != "abc" || rows[1].Text != "xy" {
		t.Fatalf("rows = %+v, want m1 %q and m2 %q", rows, "abc", "xy")
	}
}

// FlushAll is the quit path: every session's buffered tail and tool state
// lands without waiting for a cadence, and nothing writes after it — the
// database closes right behind it.
func TestRecorderFlushAllLandsEverySessionAndStops(t *testing.T) {
	store := &countingStore{Store: newTestStore(t)}
	ctx := context.Background()
	chat := seedSession(t, store)
	tools := seedSession(t, store)
	rec := agenthub.NewRecorder(store)
	rec.FlushInterval = time.Hour // only FlushAll can land anything

	for _, token := range []string{"hello ", "world"} {
		if err := rec.Record(ctx, chat, chunkUpdate("m1", token)); err != nil {
			t.Fatal(err)
		}
	}
	for _, status := range []string{"in_progress", "completed"} {
		if err := rec.Record(ctx, tools, agenthub.SessionUpdate{
			Kind:     agenthub.UpdateToolCallUpdate,
			ToolCall: &agenthub.ToolCall{ID: "c1", Title: "Run", Status: status},
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := rec.FlushAll(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := store.ListTranscript(ctx, chat)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Text != "hello world" {
		t.Fatalf("chat rows = %+v, want the whole streamed text", rows)
	}
	entry, ok, err := store.FindTranscriptToolCall(ctx, tools, "c1")
	if err != nil || !ok {
		t.Fatalf("find tool call: ok=%v err=%v", ok, err)
	}
	var stored agenthub.SessionUpdate
	if err := json.Unmarshal(entry.Payload, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.ToolCall == nil || stored.ToolCall.Status != "completed" {
		t.Fatalf("tool call = %+v, want the buffered completed status", stored.ToolCall)
	}

	appends, updates := store.appends.Load(), store.updates.Load()
	rec.FlushInterval = time.Millisecond
	if err := rec.Record(ctx, chat, chunkUpdate("m1", " again")); err != nil {
		t.Fatal(err)
	}
	if err := rec.Record(ctx, chat, chunkUpdate("m2", "late")); err != nil {
		t.Fatal(err)
	}
	// A cadence timer's whole callback is this Flush, so calling it here does
	// synchronously what any timer armed after FlushAll would do when it fired.
	if err := rec.Flush(ctx, chat); err != nil {
		t.Fatal(err)
	}
	if store.appends.Load() != appends || store.updates.Load() != updates {
		t.Fatalf("writes after FlushAll: appends %d→%d, updates %d→%d",
			appends, store.appends.Load(), updates, store.updates.Load())
	}
}

// Records, cadence timers and explicit Flushes all run concurrently in the
// live app (update callback, AfterFunc, turn end). The recorder must hold up
// under that; run with -race.
func TestRecorderConcurrentRecordsAndFlushes(t *testing.T) {
	store := &countingStore{Store: newTestStore(t)}
	ctx := context.Background()
	sid := seedSession(t, store)
	rec := agenthub.NewRecorder(store)
	rec.FlushInterval = time.Millisecond

	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				_ = rec.Record(ctx, sid, chunkUpdate("m1", "x"))
			}
		}(g)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = rec.Flush(ctx, sid)
			time.Sleep(time.Millisecond)
		}
	}()
	wg.Wait()

	if err := rec.Flush(ctx, sid); err != nil {
		t.Fatal(err)
	}
	rows, err := store.ListTranscript(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Text != strings.Repeat("x", 800) {
		t.Fatalf("rows = %d, text len = %d, want one row of all 800 tokens", len(rows), len(rows[0].Text))
	}
}
