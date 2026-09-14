package agenthub

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"devkit/internal/storage"
)

// brokenTailStore fails every transcript update while broken is set — a full
// disk, or a row that is already gone.
type brokenTailStore struct {
	Store
	broken atomic.Bool
}

func (s *brokenTailStore) UpdateTranscript(ctx context.Context, e TranscriptEntry) error {
	if s.broken.Load() {
		return errors.New("database or disk is full")
	}
	return s.Store.UpdateTranscript(ctx, e)
}

// serviceWithUnflushedTail opens a mock session whose transcript holds a tail
// that cannot be written any more.
func serviceWithUnflushedTail(t *testing.T) (*Service, *brokenTailStore, string, *[]error) {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	store := &brokenTailStore{Store: NewSQLStore(db)}
	reg := NewRegistry()
	reg.Register(NewMockAdapter())
	svc := NewService(reg, store, nil)
	var reported []error
	svc.OnRecordError = func(_ string, err error) { reported = append(reported, err) }

	sess, err := svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"a", "b"} {
		chunk := SessionUpdate{Kind: UpdateAgentMessageChunk, Content: &ContentBlock{Type: "text", Text: text}}
		if err := svc.recorder.Record(ctx, sess.ID, chunk); err != nil {
			t.Fatal(err)
		}
	}
	store.broken.Store(true)
	return svc, store, sess.ID, &reported
}

// A tail that would not write used to stop CloseSession after the runtime was
// gone but before the row said closed.
func TestCloseSessionClosesAlthoughTheTranscriptWillNotFlush(t *testing.T) {
	ctx := context.Background()
	svc, store, id, reported := serviceWithUnflushedTail(t)

	if err := svc.CloseSession(ctx, id); err != nil {
		t.Fatalf("close: %v", err)
	}
	sess, err := store.GetSession(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Status != SessionStatusClosed {
		t.Fatalf("status = %s, want closed", sess.Status)
	}
	if len(*reported) == 0 {
		t.Fatal("the failed flush was not reported")
	}
}

// Deleting is how a user frees the disk that is full; it must not need the
// disk to take a write first.
func TestDeleteSessionDeletesAlthoughTheTranscriptWillNotFlush(t *testing.T) {
	ctx := context.Background()
	svc, store, id, reported := serviceWithUnflushedTail(t)

	if err := svc.DeleteSession(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.GetSession(ctx, id); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("session after delete: err = %v, want ErrSessionNotFound", err)
	}
	if len(*reported) == 0 {
		t.Fatal("the failed flush was not reported")
	}
	svc.recorder.mu.Lock()
	_, held := svc.recorder.sessions[id]
	svc.recorder.mu.Unlock()
	if held {
		t.Fatal("recorder still holds write state for a deleted session")
	}
}
