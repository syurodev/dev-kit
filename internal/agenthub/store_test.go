package agenthub_test

import (
	"context"
	"fmt"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/storage"
)

func newTestStore(t *testing.T) agenthub.Store {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return agenthub.NewSQLStore(db)
}

func sampleSession(id string) agenthub.AgentSession {
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	title := "hello"
	return agenthub.AgentSession{
		ID:           id,
		AgentType:    agenthub.AgentTypeMock,
		ProjectPath:  "/tmp/proj",
		Status:       agenthub.SessionStatusIdle,
		Depth:        0,
		Title:        &title,
		ACPSessionID: "acp-1",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func TestStoreInsertGetUpdateListSession(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	s := sampleSession("sess-1")
	if err := store.InsertSession(ctx, s); err != nil {
		t.Fatalf("InsertSession: %v", err)
	}

	got, err := store.GetSession(ctx, "sess-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.ID != s.ID || got.AgentType != s.AgentType || got.ProjectPath != s.ProjectPath {
		t.Fatalf("GetSession = %+v, want %+v", got, s)
	}
	if got.Status != s.Status || got.Depth != s.Depth || got.ACPSessionID != s.ACPSessionID {
		t.Fatalf("GetSession status/depth/acp = %+v", got)
	}
	if got.Title == nil || *got.Title != "hello" {
		t.Fatalf("Title = %v, want hello", got.Title)
	}
	if got.ParentSessionID != nil {
		t.Fatalf("ParentSessionID = %v, want nil", got.ParentSessionID)
	}
	if !got.CreatedAt.Equal(s.CreatedAt) || !got.UpdatedAt.Equal(s.UpdatedAt) {
		t.Fatalf("timestamps got %v/%v want %v/%v", got.CreatedAt, got.UpdatedAt, s.CreatedAt, s.UpdatedAt)
	}

	parent := "parent-1"
	newTitle := "updated"
	s.Status = agenthub.SessionStatusRunning
	s.Depth = 1
	s.ParentSessionID = &parent
	s.Title = &newTitle
	s.UpdatedAt = s.UpdatedAt.Add(time.Minute)
	if err := store.UpdateSession(ctx, s); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}

	got, err = store.GetSession(ctx, "sess-1")
	if err != nil {
		t.Fatalf("GetSession after update: %v", err)
	}
	if got.Status != agenthub.SessionStatusRunning || got.Depth != 1 {
		t.Fatalf("after update status/depth = %s/%d", got.Status, got.Depth)
	}
	if got.ParentSessionID == nil || *got.ParentSessionID != parent {
		t.Fatalf("ParentSessionID = %v, want %q", got.ParentSessionID, parent)
	}
	if got.Title == nil || *got.Title != "updated" {
		t.Fatalf("Title = %v, want updated", got.Title)
	}

	s2 := sampleSession("sess-2")
	if err := store.InsertSession(ctx, s2); err != nil {
		t.Fatalf("InsertSession sess-2: %v", err)
	}
	list, err := store.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListSessions len = %d, want 2", len(list))
	}
}

func TestStoreGetSessionNotFound(t *testing.T) {
	store := newTestStore(t)
	_, err := store.GetSession(context.Background(), "missing")
	if err != agenthub.ErrSessionNotFound {
		t.Fatalf("GetSession error = %v, want ErrSessionNotFound", err)
	}
}

func TestStoreUpdateSessionNotFound(t *testing.T) {
	store := newTestStore(t)
	err := store.UpdateSession(context.Background(), sampleSession("missing"))
	if err != agenthub.ErrSessionNotFound {
		t.Fatalf("UpdateSession error = %v, want ErrSessionNotFound", err)
	}
}

func TestStoreInsertListTurns(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.InsertSession(ctx, sampleSession("sess-1")); err != nil {
		t.Fatalf("InsertSession: %v", err)
	}

	now := time.Date(2026, 8, 12, 11, 0, 0, 0, time.UTC)
	usage := json.RawMessage(`{"cost_usd":0.01}`)
	meta := json.RawMessage(`{"vendor":"mock"}`)
	turn := agenthub.TurnRecord{
		ID:         "turn-1",
		SessionID:  "sess-1",
		StopReason: "end_turn",
		UsageJSON:  usage,
		RawMeta:    meta,
		CreatedAt:  now,
	}
	if err := store.InsertTurn(ctx, turn); err != nil {
		t.Fatalf("InsertTurn: %v", err)
	}

	turns, err := store.ListTurns(ctx, "sess-1")
	if err != nil {
		t.Fatalf("ListTurns: %v", err)
	}
	if len(turns) != 1 {
		t.Fatalf("ListTurns len = %d, want 1", len(turns))
	}
	got := turns[0]
	if got.ID != turn.ID || got.SessionID != turn.SessionID || got.StopReason != turn.StopReason {
		t.Fatalf("turn = %+v", got)
	}
	if string(got.UsageJSON) != string(usage) || string(got.RawMeta) != string(meta) {
		t.Fatalf("json fields usage=%s meta=%s", got.UsageJSON, got.RawMeta)
	}
	if !got.CreatedAt.Equal(now) {
		t.Fatalf("CreatedAt = %v, want %v", got.CreatedAt, now)
	}

	empty, err := store.ListTurns(ctx, "no-such-session")
	if err != nil {
		t.Fatalf("ListTurns missing session: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("ListTurns missing session len = %d, want 0", len(empty))
	}
}

// agent_transcript had no delete path of any kind: append-plus-upsert only, no
// session delete cascading into it, and one row per streamed chunk. The table
// was unbounded by construction, so this pins that it can now be bounded.
func TestPruneTranscriptsBeforeKeepsRecentRows(t *testing.T) {
	store := newTestStore(t)
	sql, ok := store.(*agenthub.SQLStore)
	if !ok {
		t.Fatalf("want *agenthub.SQLStore, got %T", store)
	}
	ctx := context.Background()

	if err := sql.InsertSession(ctx, sampleSession("s1")); err != nil {
		t.Fatal(err)
	}

	old := time.Now().AddDate(0, 0, -120)
	recent := time.Now()
	for i, at := range []time.Time{old, old, recent} {
		if err := sql.AppendTranscript(ctx, agenthub.TranscriptEntry{
			ID:        fmt.Sprintf("t%d", i+1),
			SessionID: "s1",
			Seq:       int64(i + 1),
			Kind:      agenthub.UpdateAgentMessageChunk,
			Text:      "x",
			CreatedAt: at,
			UpdatedAt: at,
		}); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := sql.PruneTranscriptsBefore(ctx, time.Now().AddDate(0, 0, -90))
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("want 2 rows pruned, got %d", removed)
	}

	rows, err := sql.ListTranscript(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row kept, got %d", len(rows))
	}
}
