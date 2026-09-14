package agenthub

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

// failingTranscriptStore keeps rows in memory, fails every update while broken
// and answers ErrTranscriptEntryNotFound for rows marked missing. It counts
// update attempts per row, so a test can see what a write pass cost.
type failingTranscriptStore struct {
	TranscriptStore

	mu       sync.Mutex
	order    []string
	text     map[string]string
	broken   bool
	missing  map[string]bool
	attempts map[string]int
}

func newFailingTranscriptStore() *failingTranscriptStore {
	return &failingTranscriptStore{text: map[string]string{}, missing: map[string]bool{}, attempts: map[string]int{}}
}

func (s *failingTranscriptStore) AppendTranscript(_ context.Context, e TranscriptEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.order = append(s.order, e.ID)
	s.text[e.ID] = e.Text
	return nil
}

func (s *failingTranscriptStore) UpdateTranscript(_ context.Context, e TranscriptEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts[e.ID]++
	if s.missing[e.ID] {
		return ErrTranscriptEntryNotFound
	}
	if s.broken {
		return errors.New("database is locked")
	}
	s.text[e.ID] = e.Text
	return nil
}

func (s *failingTranscriptStore) setBroken(broken bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.broken = broken
}

func (s *failingTranscriptStore) updates() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.attempts {
		n += c
	}
	return n
}

func (s *failingTranscriptStore) texts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.order))
	for i, id := range s.order {
		out[i] = s.text[id]
	}
	return out
}

func textChunk(messageID, text string) SessionUpdate {
	return SessionUpdate{Kind: UpdateAgentMessageChunk, Content: &ContentBlock{Type: "text", Text: text}, MessageID: messageID}
}

// streamMessages records messages from..to-1, each "a" then "b", and returns
// every error the recorder reported.
func streamMessages(rec *Recorder, sid string, from, to int) []error {
	var errs []error
	for i := from; i < to; i++ {
		for _, text := range []string{"a", "b"} {
			if err := rec.Record(context.Background(), sid, textChunk(fmt.Sprintf("m%d", i), text)); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errs
}

// Every message that started behind a failed write left its tail to be
// retried, and every later Record retried all of them — under the recorder's
// lock, on the path the agent's updates arrive on, at up to busy_timeout each.
func TestRecorderCostUnderPersistentFailureDoesNotGrowWithTheBacklog(t *testing.T) {
	store := newFailingTranscriptStore()
	rec := NewRecorder(store)
	rec.FlushInterval = 0
	store.setBroken(true)
	streamMessages(rec, "s1", 0, 20)

	before := store.updates()
	_ = rec.Record(context.Background(), "s1", textChunk("m20", "a"))
	if cost := store.updates() - before; cost > 1 {
		t.Fatalf("one Record made %d store writes behind a backlog of 19, want at most 1", cost)
	}

	before = store.updates()
	if err := rec.Flush(context.Background(), "s1"); err == nil {
		t.Fatal("flush against a failing store reported success")
	}
	if cost := store.updates() - before; cost != 1 {
		t.Fatalf("one Flush made %d store writes, want it to stop at the first failure", cost)
	}
}

func TestRecorderCapsTheTextWaitingOnAFailingStore(t *testing.T) {
	store := newFailingTranscriptStore()
	rec := NewRecorder(store)
	rec.FlushInterval = 0
	store.setBroken(true)
	const over = 3
	// One more message than the cap plus over: the last one is the live tail.
	errs := streamMessages(rec, "s1", 0, maxUnflushedTails+over+1)

	rec.mu.Lock()
	held := len(rec.sessions["s1"].unflushed)
	rec.mu.Unlock()
	if held != maxUnflushedTails {
		t.Fatalf("holding %d failed tails, want the cap %d", held, maxUnflushedTails)
	}
	dropped := 0
	for _, err := range errs {
		if errors.Is(err, errUnflushedTailDropped) {
			dropped++
		}
	}
	if dropped != over {
		t.Fatalf("reported %d dropped tails, want %d", dropped, over)
	}

	store.setBroken(false)
	if err := rec.Flush(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	for i, text := range store.texts() {
		want := "ab"
		if i < over {
			want = "a" // the oldest tails' late text is what the cap gave up
		}
		if text != want {
			t.Fatalf("row %d = %q, want %q (all: %q)", i, text, want, store.texts())
		}
	}
}

// UpdateTranscript answers ErrTranscriptEntryNotFound when the row is gone. No
// retry can land that, and it used to be retried by every flush forever.
func TestRecorderDropsATailWhoseRowIsGone(t *testing.T) {
	ctx := context.Background()
	store := newFailingTranscriptStore()
	rec := NewRecorder(store)
	rec.FlushInterval = 0
	streamMessages(rec, "s1", 0, 1)
	gone := store.order[0]
	store.mu.Lock()
	store.missing[gone] = true
	store.mu.Unlock()

	// Retired behind a new message, then flushed twice.
	if err := rec.Record(ctx, "s1", textChunk("m1", "x")); !errors.Is(err, ErrTranscriptEntryNotFound) {
		t.Fatalf("record: err = %v, want the missing row reported", err)
	}
	for i := 0; i < 2; i++ {
		if err := rec.Flush(ctx, "s1"); err != nil {
			t.Fatalf("flush %d: %v", i, err)
		}
	}
	if n := store.attempts[gone]; n != 1 {
		t.Fatalf("wrote the missing row %d times, want once", n)
	}

	// The live tail's row going missing is the same.
	live := store.order[1]
	_ = rec.Record(ctx, "s1", textChunk("m1", "y"))
	store.mu.Lock()
	store.missing[live] = true
	before := store.attempts[live]
	store.mu.Unlock()
	if err := rec.Flush(ctx, "s1"); !errors.Is(err, ErrTranscriptEntryNotFound) {
		t.Fatalf("flush: err = %v, want the missing row reported", err)
	}
	if err := rec.Flush(ctx, "s1"); err != nil {
		t.Fatalf("second flush: %v", err)
	}
	if n := store.attempts[live] - before; n != 1 {
		t.Fatalf("wrote the missing live row %d times, want once", n)
	}
}

func TestRecorderLandsEveryTailInOrderOnceTheStoreRecovers(t *testing.T) {
	store := newFailingTranscriptStore()
	rec := NewRecorder(store)
	rec.FlushInterval = 0
	store.setBroken(true)
	streamMessages(rec, "s1", 0, 5)

	store.setBroken(false)
	if err := rec.Record(context.Background(), "s1", textChunk("m5", "a")); err != nil {
		t.Fatalf("record after recovery: %v", err)
	}
	if err := rec.Flush(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	want := []string{"ab", "ab", "ab", "ab", "ab", "a"}
	if got := store.texts(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("rows = %q, want %q", got, want)
	}
}
