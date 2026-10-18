package agenthub

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"devkit/internal/storage"
)

// An agent that gave up retrying does not always say the notice is over. The
// turn ending must clear it — in the window and in the stored row — or a
// reload would find a turn that ended still "retrying".
func TestEndOfTurnClearsAnUnclearedRetryNotice(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	store := NewSQLStore(db)
	reg := NewRegistry()
	reg.Register(NewMockAdapter())
	var emitted []json.RawMessage
	svc := NewService(reg, store, func(_ string, update json.RawMessage) { emitted = append(emitted, update) })
	sess, err := svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	// Nothing showing: nothing to clear, no event.
	svc.endProviderRetry(ctx, sess.ID)
	if len(emitted) != 0 {
		t.Fatalf("emitted %d updates with no notice showing", len(emitted))
	}

	svc.handleUpdate(ctx, sess.ID, json.RawMessage(`{"sessionUpdate":"session_info_update","_meta":{"opencode/retry":{"attempt":11,"error":{"message":"Upstream request failed: Endpoint is unavailable.","status":503}}}}`))
	svc.endProviderRetry(ctx, sess.ID)
	if got := ParseSessionUpdate(emitted[len(emitted)-1]); got.Kind != UpdateProviderRetry || got.Retry != nil {
		t.Fatalf("last event = %+v, want a cleared notice", got)
	}
	// And once: a second end of turn has nothing left to clear.
	before := len(emitted)
	svc.endProviderRetry(ctx, sess.ID)
	if len(emitted) != before {
		t.Fatal("the notice was cleared twice")
	}

	row, found, err := store.FindTranscriptKind(ctx, sess.ID, UpdateProviderRetry)
	if err != nil || !found {
		t.Fatalf("row: %v %v", found, err)
	}
	var stored SessionUpdate
	if err := json.Unmarshal(row.Payload, &stored); err != nil || stored.Retry != nil {
		t.Fatalf("stored = %+v (%v), want the cleared notice", stored, err)
	}
}
