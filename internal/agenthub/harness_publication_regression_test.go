package agenthub_test

import (
	"context"
	"testing"
	"time"

	"devkit/internal/agenthub"
)

func TestRewindDurableCommitAndRuntimePublicationExcludeClose(t *testing.T) {
	e, session, promptID, observer := newHarnessRewindEnv(t)
	closeDone := make(chan error, 1)
	observer.afterCommit = func(agenthub.RewindSpec) {
		// The SQL transaction has committed, but the fork is not published yet.
		// Close must not slip into this gap and leave the fork resurrected.
		go func() { closeDone <- e.svc.CloseSession(context.Background(), session.ID) }()
		select {
		case err := <-closeDone:
			t.Errorf("Close overtook rewind publication: %v", err)
			closeDone <- err
		case <-time.After(100 * time.Millisecond):
		}
	}
	preview, err := e.svc.PlanRewind(context.Background(), agenthub.RewindRequest{
		SessionID: session.ID, PromptMessageID: promptID, Mode: agenthub.RewindConversation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.ApplyRewind(context.Background(), preview.Token, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not settle after rewind publication")
	}
	row, err := agenthub.NewSQLStore(e.db).GetSession(context.Background(), session.ID)
	if err != nil || row.Status != agenthub.SessionStatusClosed {
		t.Fatalf("rewind lost the Close: %+v %v", row, err)
	}
	ref, _, found := e.durable(session.ID)
	mem, _, ok := e.svc.HarnessApplied(session.ID)
	if !found || !ok || !ref.Equal(mem) {
		t.Fatal("Close lost the transactional rewind reference")
	}
}
