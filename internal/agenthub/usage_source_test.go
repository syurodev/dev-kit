package agenthub_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/identity"
	"devkit/internal/storage"
)

// newUsageDB opens a migrated database holding workspaces w1 and w2: a session's
// workspace_id is a foreign key, so the rows must exist before a session names them.
func newUsageDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"w1", "w2"} {
		if _, err := db.Exec("INSERT INTO workspaces (id, name, root) VALUES (?, ?, '')", id, id); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func usageRowsFixture(t *testing.T) (agenthub.Store, time.Time) {
	t.Helper()
	store := agenthub.NewSQLStore(newUsageDB(t))
	ctx := context.Background()
	at := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

	claude := sampleSession("s-claude")
	claude.AgentType = agenthub.AgentTypeClaude
	claude.WorkspaceID = "w1"
	mock := sampleSession("s-mock")
	mock.WorkspaceID = "w2"
	for _, s := range []agenthub.AgentSession{claude, mock} {
		if err := store.InsertSession(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	raw := json.RawMessage(`{"quota":{"token_count":{"totalTokens":900,"inputTokens":4,"cachedInputTokens":800,"cachedWriteTokens":60,"outputTokens":36}}}`)
	turns := []agenthub.TurnRecord{
		{ID: "t1", SessionID: "s-claude", UsageJSON: json.RawMessage(`{"InputTokens":0,"OutputTokens":0}`), RawMeta: raw, CreatedAt: at},
		{ID: "t2", SessionID: "s-mock", UsageJSON: json.RawMessage(`{"InputTokens":1,"OutputTokens":1}`), CreatedAt: at},
	}
	for _, turn := range turns {
		if err := store.InsertTurn(ctx, turn); err != nil {
			t.Fatal(err)
		}
	}
	return store, at
}

func TestTurnUsageRowsListsOnlyTheVendorsTurnsWithTheirWorkspace(t *testing.T) {
	store, at := usageRowsFixture(t)
	source, ok := store.(agenthub.TurnUsageSource)
	if !ok {
		t.Fatal("the SQL store must be a TurnUsageSource")
	}
	rows, err := source.TurnUsageRows(context.Background(), agenthub.AgentTypeClaude)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].TurnID != "t1" || rows[0].SessionID != "s-claude" || rows[0].WorkspaceID != "w1" || !rows[0].CreatedAt.Equal(at) {
		t.Fatalf("rows = %+v", rows)
	}
	if len(rows[0].RawMeta) == 0 || len(rows[0].UsageJSON) == 0 {
		t.Errorf("the row must carry both stored parts: %+v", rows[0])
	}
}

func newUsageService(t *testing.T) *agenthub.Service {
	t.Helper()
	store := agenthub.NewSQLStore(newUsageDB(t))
	ctx := context.Background()
	s := sampleSession("s-claude")
	s.AgentType = agenthub.AgentTypeClaude
	s.WorkspaceID = "w1"
	if err := store.InsertSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{"quota":{"token_count":{"totalTokens":900,"inputTokens":4,"cachedInputTokens":800,"cachedWriteTokens":60,"outputTokens":36}}}`)
	if err := store.InsertTurn(ctx, agenthub.TurnRecord{ID: "t1", SessionID: "s-claude", UsageJSON: json.RawMessage(`{}`), RawMeta: raw, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	reg := agenthub.NewRegistry()
	reg.Register(agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{}))
	return agenthub.NewService(reg, store, nil)
}

func TestServiceUsageReportReadsTheStoredTurns(t *testing.T) {
	svc := newUsageService(t)
	rep, err := svc.UsageReport(context.Background(), agenthub.AgentTypeClaude)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Totals.Turns != 1 || rep.Totals.Total != 900 || rep.Totals.CacheRead != 800 || len(rep.ByWorkspace) != 1 || rep.ByWorkspace[0].Key != "w1" {
		t.Errorf("report = %+v", rep)
	}
}

func TestServiceUsageReportIsTheWindowsAndNeedsAKnownVendor(t *testing.T) {
	svc := newUsageService(t)
	agent := identity.WithCaller(context.Background(), identity.Agent("11111111-1111-1111-1111-111111111111"))
	if _, err := svc.UsageReport(agent, agenthub.AgentTypeClaude); !errors.Is(err, agenthub.ErrPermissionDenied) {
		t.Errorf("an agent caller got %v, want ErrPermissionDenied", err)
	}
	if _, err := svc.UsageReport(identity.WithCaller(context.Background(), identity.UI()), agenthub.AgentTypeClaude); err != nil {
		t.Errorf("the window got %v", err)
	}
	if _, err := svc.UsageReport(context.Background(), agenthub.AgentType("nope")); !errors.Is(err, agenthub.ErrVendorUnknown) {
		t.Errorf("unknown vendor got %v, want ErrVendorUnknown", err)
	}
}
