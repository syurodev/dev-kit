package agenthub_test

import (
	"context"
	"encoding/json"
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
	deadline := time.Now().Add(2 * time.Second)
	for store.updates.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if got := store.updates.Load(); got == 0 {
		t.Fatal("cadence flush never wrote the tail")
	}
	rows, err := store.ListTranscript(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Text != "xxxxxxxxxx" {
		t.Fatalf("row after cadence = %+v, want the full tail", rows)
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
