package agenthub_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"devkit/internal/agenthub"
)

// Once the cut has committed, what is left (putting the session's mode back on the
// fork and deleting the source conversation) is owed whether or not the caller is still
// there. It runs on the host's own bounded context, so a caller that cancels right
// after the commit does not silently lose the re-application.
func TestACallerThatCancelsAfterTheCutStillGetsTheModeBackAndTheSourceDeleted(t *testing.T) {
	r, _ := newFilesRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	update := agenthub.ParseSessionUpdate(json.RawMessage(`{"sessionUpdate":"current_mode_update","currentModeId":"plan","availableModes":[{"id":"plan","name":"Plan"}]}`))
	payload, err := json.Marshal(update)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.store.AppendTranscript(context.Background(), agenthub.TranscriptEntry{
		ID: "mode-row", SessionID: r.session.ID, Kind: update.Kind, Payload: payload,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	// The cut commits, and the caller goes away before the host does what it owes after it.
	r.store.cutFn = func(real func() error) error {
		if err := real(); err != nil {
			return err
		}
		cancel()
		return nil
	}

	if _, err := r.rewind(ctx); err != nil {
		t.Fatalf("a committed rewind is a success whatever the caller does next: %v", err)
	}
	if r.messages() != 1 || !r.dropped() {
		t.Fatal("the rewind must have cut the conversation and restored the files")
	}
	fork := r.probes.fork(1)
	fork.mu.Lock()
	defer fork.mu.Unlock()
	if len(fork.modes) != 1 || fork.modes[0] != "plan" || fork.modeCtxErrs[0] != nil {
		t.Fatalf("the mode must be put back on the fork with a live context: modes=%v ctxErrs=%v", fork.modes, fork.modeCtxErrs)
	}
	if len(fork.deletedSessions) != 1 || fork.deleteCtxErrs[0] != nil {
		t.Fatalf("the source session must be deleted with a live context: sessions=%v ctxErrs=%v", fork.deletedSessions, fork.deleteCtxErrs)
	}
}
