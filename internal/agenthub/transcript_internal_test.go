package agenthub

import (
	"context"
	"testing"
	"time"
)

// memStore is the least of a Store CloseSession needs; the recorder's writes
// succeed and go nowhere.
type memStore struct{ Store }

func (memStore) GetSession(_ context.Context, id string) (AgentSession, error) {
	return AgentSession{ID: id}, nil
}
func (memStore) UpdateSession(context.Context, AgentSession) error { return nil }
func (memStore) UpdateSessionStatus(context.Context, string, SessionStatus, time.Time) error {
	return nil
}
func (memStore) AppendTranscript(context.Context, TranscriptEntry) error { return nil }
func (memStore) UpdateTranscript(context.Context, TranscriptEntry) error { return nil }

// The recorder keyed its state by session and never let go, so a hub open for
// weeks held an entry for every session it had ever recorded.
func TestCloseSessionForgetsTheRecorderState(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewRegistry(), memStore{}, nil)

	chunk := SessionUpdate{Kind: UpdateAgentMessageChunk, Content: &ContentBlock{Type: "text", Text: "hi"}}
	if err := svc.recorder.Record(ctx, "s1", chunk); err != nil {
		t.Fatal(err)
	}
	svc.recorder.mu.Lock()
	_, held := svc.recorder.sessions["s1"]
	svc.recorder.mu.Unlock()
	if !held {
		t.Fatal("precondition: recorder holds no state for the session it just recorded")
	}

	if err := svc.CloseSession(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	svc.recorder.mu.Lock()
	_, held = svc.recorder.sessions["s1"]
	svc.recorder.mu.Unlock()
	if held {
		t.Fatal("recorder still holds state for a closed session")
	}
}
