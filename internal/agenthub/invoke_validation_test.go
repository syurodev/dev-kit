package agenthub_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/storage"
)

// A prompt the backend refuses never started a turn, so nothing of it may
// reach the transcript: the frontend puts a refused prompt back in the queue,
// and an echo already written showed it twice once it was sent again.
func TestInvokeRejectsInvalidInputBeforeTouchingTranscriptOrStatus(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	store := agenthub.NewSQLStore(db)
	var (
		mu      sync.Mutex
		emitted []json.RawMessage
	)
	reg := agenthub.NewRegistry()
	reg.Register(agenthub.NewMockAdapter())
	svc := agenthub.NewService(reg, store, func(_ string, update json.RawMessage) {
		mu.Lock()
		emitted = append(emitted, update)
		mu.Unlock()
	})
	ctx := context.Background()
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	// Error, not idle: a refused prompt that still went through running and
	// back would end on idle and look unchanged.
	row, err := store.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	row.Status = agenthub.SessionStatusError
	if err := store.UpdateSession(ctx, row); err != nil {
		t.Fatal(err)
	}

	cases := map[string]agenthub.InvokeInput{
		"attachment outside the project": {Prompt: "look at this", Attachments: []string{filepath.Join(t.TempDir(), "missing.txt")}},
		"unsupported image":              {Prompt: "look at this", Images: []agenthub.ImageAttachment{{Name: "a.bmp", MimeType: "image/bmp", Data: "AAAA"}}},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			in.SessionID = sess.ID
			if _, err := svc.Invoke(ctx, in); !errors.Is(err, agenthub.ErrInvalidPrompt) {
				t.Fatalf("err = %v, want ErrInvalidPrompt", err)
			}
			entries, err := store.ListTranscript(ctx, sess.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("transcript has %d entries, want none: %+v", len(entries), entries)
			}
			mu.Lock()
			n := len(emitted)
			mu.Unlock()
			if n != 0 {
				t.Fatalf("emitted %d updates, want none", n)
			}
			got, err := store.GetSession(ctx, sess.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != agenthub.SessionStatusError {
				t.Fatalf("status = %s, want it left at error", got.Status)
			}
		})
	}
}
