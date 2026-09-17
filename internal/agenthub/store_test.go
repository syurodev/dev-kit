package agenthub_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
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

// pageFixture seeds three turns — a user_message_chunk row opens each turn —
// so the page tests can pin both the window and the turn-boundary snap.
func pageFixture(t *testing.T) agenthub.Store {
	t.Helper()
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.InsertSession(ctx, sampleSession("s1")); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	kinds := []agenthub.UpdateKind{
		agenthub.UpdateUserMessageChunk,  // 1 turn 1 opens
		agenthub.UpdateAgentMessageChunk, // 2
		agenthub.UpdateToolCall,          // 3
		agenthub.UpdateUserMessageChunk,  // 4 turn 2 opens
		agenthub.UpdateAgentMessageChunk, // 5
		agenthub.UpdateToolCall,          // 6
		agenthub.UpdateUserMessageChunk,  // 7 turn 3 opens
		agenthub.UpdateAgentMessageChunk, // 8
		agenthub.UpdateAgentMessageChunk, // 9
	}
	for i, kind := range kinds {
		if err := store.AppendTranscript(ctx, agenthub.TranscriptEntry{
			ID:        fmt.Sprintf("t%d", i+1),
			SessionID: "s1",
			Seq:       int64(i + 1),
			Kind:      kind,
			Text:      "x",
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func seqs(entries []agenthub.TranscriptEntry) []int64 {
	out := make([]int64, len(entries))
	for i, e := range entries {
		out[i] = e.Seq
	}
	return out
}

func TestListTranscriptPageWindowsAndHasMore(t *testing.T) {
	store := pageFixture(t)
	ctx := context.Background()

	// Last four entries; the oldest (6) is mid-turn and older rows exist, so
	// the page snaps back to the turn start at 4. A window already beginning
	// on a turn start (seq 7) stays as-is — that boundary falls between turns.
	page, err := store.ListTranscriptPage(ctx, "s1", 0, 4)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := seqs(page.Entries), []int64{4, 5, 6, 7, 8, 9}; !slices.Equal(got, want) {
		t.Fatalf("page = %v, want %v", got, want)
	}
	if !page.HasMore {
		t.Fatal("hasMore = false, want true (seq 1-3 are older)")
	}

	// Walking backwards from the previous page's oldest seq: seq<4 is the
	// whole first turn, and nothing is older.
	page, err = store.ListTranscriptPage(ctx, "s1", 4, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := seqs(page.Entries), []int64{1, 2, 3}; !slices.Equal(got, want) {
		t.Fatalf("page = %v, want %v", got, want)
	}
	if page.HasMore {
		t.Fatal("hasMore = true, want false at the start of the transcript")
	}

	// A window that already begins on a turn start must not snap further back.
	page, err = store.ListTranscriptPage(ctx, "s1", 7, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := seqs(page.Entries), []int64{4, 5, 6}; !slices.Equal(got, want) {
		t.Fatalf("page = %v, want %v", got, want)
	}
	if !page.HasMore {
		t.Fatal("hasMore = false, want true (seq 1-3 are older)")
	}

	// A page larger than the transcript returns everything there is.
	page, err = store.ListTranscriptPage(ctx, "s1", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 9 || page.HasMore {
		t.Fatalf("full page len=%d hasMore=%v, want 9 and false", len(page.Entries), page.HasMore)
	}

	// Before the first row is empty, not an error.
	page, err = store.ListTranscriptPage(ctx, "s1", 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 0 || page.HasMore {
		t.Fatalf("page before seq 1 = %v hasMore=%v, want empty and false", seqs(page.Entries), page.HasMore)
	}

	// Unknown session reads empty, like ListTranscript.
	page, err = store.ListTranscriptPage(ctx, "no-such", 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 0 || page.HasMore {
		t.Fatalf("unknown session = %d entries hasMore=%v, want empty and false", len(page.Entries), page.HasMore)
	}
}

// A single turn longer than a page cannot be snapped to its start without
// unbounded growth. The snap is abandoned when its bound would cut the turn
// again — the skipped rows would be unreachable — so the page keeps its
// original mid-turn cut, visible, and hasMore still says what is above.
func TestListTranscriptPageSnapIsBounded(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.InsertSession(ctx, sampleSession("s1")); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	kinds := []agenthub.UpdateKind{agenthub.UpdateUserMessageChunk}
	for i := 0; i < 9; i++ {
		kinds = append(kinds, agenthub.UpdateAgentMessageChunk)
	}
	for i, kind := range kinds {
		if err := store.AppendTranscript(ctx, agenthub.TranscriptEntry{
			ID:        fmt.Sprintf("t%d", i+1),
			SessionID: "s1",
			Seq:       int64(i + 1),
			Kind:      kind,
			Text:      "x",
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}

	page, err := store.ListTranscriptPage(ctx, "s1", 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := seqs(page.Entries), []int64{8, 9, 10}; !slices.Equal(got, want) {
		t.Fatalf("page = %v, want the un-snapped window %v", got, want)
	}
	if !page.HasMore {
		t.Fatal("hasMore = false with rows still above")
	}

	// Walking back through the un-snappable turn must still reach every row.
	seen := map[int64]bool{}
	for _, e := range page.Entries {
		seen[e.Seq] = true
	}
	before := page.Entries[0].Seq
	for {
		page, err = store.ListTranscriptPage(ctx, "s1", before, 3)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Entries) == 0 {
			break
		}
		for _, e := range page.Entries {
			if seen[e.Seq] {
				t.Fatalf("seq %d served twice", e.Seq)
			}
			seen[e.Seq] = true
		}
		before = page.Entries[0].Seq
		if !page.HasMore {
			break
		}
	}
	for seq := int64(1); seq <= 10; seq++ {
		if !seen[seq] {
			t.Fatalf("seq %d unreachable by pagination", seq)
		}
	}
}

// Retention keyed on updated_at, which opening or reading a session never
// touches: a session left idle past the window lost its whole transcript while
// its row stayed resumable, and reopened as an empty conversation that still
// answered. Only closed sessions go now, each whole — row, turns, transcript —
// and never out from under a child that is kept. (Before that, a row-wise
// created_at cut sawed the head off long conversations.)
func TestPruneClosedSessionsKeepsEverySessionStillInUse(t *testing.T) {
	store := newTestStore(t)
	sql, ok := store.(*agenthub.SQLStore)
	if !ok {
		t.Fatalf("want *agenthub.SQLStore, got %T", store)
	}
	ctx := context.Background()
	old := time.Now().AddDate(0, 0, -120)
	recent := time.Now().AddDate(0, 0, -1)

	insert := func(id string, status agenthub.SessionStatus, updated time.Time, parent string) {
		t.Helper()
		s := sampleSession(id)
		s.Status = status
		s.CreatedAt = old
		s.UpdatedAt = updated
		if parent != "" {
			s.ParentSessionID = &parent
		}
		if err := sql.InsertSession(ctx, s); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			if err := sql.AppendTranscript(ctx, agenthub.TranscriptEntry{
				ID: fmt.Sprintf("%s-t%d", id, i), SessionID: id, Kind: agenthub.UpdateAgentMessageChunk,
				Text: "x", CreatedAt: old, UpdatedAt: old,
			}); err != nil {
				t.Fatal(err)
			}
		}
		if err := sql.InsertTurn(ctx, agenthub.TurnRecord{ID: id + "-turn", SessionID: id, CreatedAt: old}); err != nil {
			t.Fatal(err)
		}
	}
	// Idle for months, but still the user's: it resumes on the next prompt.
	insert("idle-old", agenthub.SessionStatusIdle, old, "")
	insert("closed-recent", agenthub.SessionStatusClosed, recent, "")
	insert("closed-old", agenthub.SessionStatusClosed, old, "")
	insert("closed-old-child", agenthub.SessionStatusClosed, old, "closed-old")
	insert("closed-parent-of-idle", agenthub.SessionStatusClosed, old, "")
	insert("idle-child", agenthub.SessionStatusIdle, old, "closed-parent-of-idle")

	removed, err := sql.PruneClosedSessions(ctx, time.Now().AddDate(0, 0, -90))
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("removed %d sessions, want closed-old and its closed child", removed)
	}

	for id, kept := range map[string]bool{
		"idle-old":              true,
		"closed-recent":         true,
		"closed-old":            false,
		"closed-old-child":      false,
		"closed-parent-of-idle": true,
		"idle-child":            true,
	} {
		_, getErr := sql.GetSession(ctx, id)
		rows, err := sql.ListTranscript(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		turns, err := sql.ListTurns(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if kept && (getErr != nil || len(rows) != 3 || len(turns) != 1) {
			t.Fatalf("%s: session err=%v, %d transcript rows, %d turns; want it kept whole", id, getErr, len(rows), len(turns))
		}
		if !kept && (!errors.Is(getErr, agenthub.ErrSessionNotFound) || len(rows) != 0 || len(turns) != 0) {
			t.Fatalf("%s: session err=%v, %d transcript rows, %d turns; want it gone whole", id, getErr, len(rows), len(turns))
		}
	}
}

func TestStoreNamesATurnByItsPromptAndForgetsItsCheckpoints(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := agenthub.NewSQLStore(db)
	ctx := context.Background()
	at := time.Now().UTC()
	if err := store.InsertSession(ctx, sampleSession("s1")); err != nil {
		t.Fatal(err)
	}

	for i, id := range []string{"p1", "p2"} {
		payload, _ := json.Marshal(agenthub.SessionUpdate{Kind: agenthub.UpdateUserMessageChunk, MessageID: id})
		if err := store.AppendTranscript(ctx, agenthub.TranscriptEntry{
			ID: fmt.Sprintf("row-%d", i), SessionID: "s1", Kind: agenthub.UpdateUserMessageChunk,
			Text: id, Payload: payload, CreatedAt: at, UpdatedAt: at,
		}); err != nil {
			t.Fatal(err)
		}
	}
	entry, ok, err := store.FindPromptEntry(ctx, "s1", "p2")
	if err != nil || !ok || entry.ID != "row-1" {
		t.Fatalf("FindPromptEntry(p2) = %+v, %v, %v; want row-1", entry, ok, err)
	}
	if _, ok, _ := store.FindPromptEntry(ctx, "s1", "nope"); ok {
		t.Fatal("found a prompt that was never sent")
	}

	if err := store.InsertTurn(ctx, agenthub.TurnRecord{ID: "t1", SessionID: "s1", PromptMessageID: "p2", CreatedAt: at}); err != nil {
		t.Fatal(err)
	}
	turns, err := store.ListTurns(ctx, "s1")
	if err != nil || len(turns) != 1 || turns[0].PromptMessageID != "p2" {
		t.Fatalf("turns = %+v, %v", turns, err)
	}

	if err := store.UpdateSessionStatus(ctx, "s1", agenthub.SessionStatusRunning, at); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateSessionACPID(ctx, "s1", "acp-forked", at); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSession(ctx, "s1")
	if err != nil || got.ACPSessionID != "acp-forked" || got.Status != agenthub.SessionStatusRunning {
		t.Fatalf("session = %+v, %v; want the new acp id and the status left alone", got, err)
	}
	if err := store.UpdateSessionACPID(ctx, "missing", "x", at); !errors.Is(err, agenthub.ErrSessionNotFound) {
		t.Fatalf("missing session: %v", err)
	}

	// A checkpoint row declares a foreign key onto the session; deleting the
	// session has to take it along or the delete fails.
	if _, err := db.ExecContext(ctx, `INSERT INTO agent_checkpoints (id, session_id, project_key, kind, status, created_at) VALUES ('c1', 's1', 'k', 'turn', 'done', ?)`, at.Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert checkpoint: %v", err)
	}
	if err := store.DeleteSessions(ctx, []string{"s1"}); err != nil {
		t.Fatalf("DeleteSessions: %v", err)
	}
	var left int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_checkpoints`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("checkpoints left = %d, %v", left, err)
	}
}
