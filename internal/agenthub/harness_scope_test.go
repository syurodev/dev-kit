package agenthub_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/storage"
)

func TestHarnessScopeReadsTheSessionsWorkspaceAndVendor(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = storage.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO workspaces (id, name, root) VALUES ('w1', 'w1', '')"); err != nil {
		t.Fatal(err)
	}
	reg := agenthub.NewRegistry()
	reg.Register(agenthub.NewMockAdapter())
	svc := agenthub.NewService(reg, agenthub.NewSQLStore(db), func(string, json.RawMessage) {})
	svc.MatchingWorkspaces = func(string) ([]string, error) { return []string{"w1"}, nil }
	bound, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir(), WorkspaceID: "w1"})
	if err != nil {
		t.Fatal(err)
	}
	ws, vendor, err := svc.HarnessScope(ctx, bound.ID)
	if err != nil || ws != "w1" || vendor != agenthub.AgentTypeMock {
		t.Fatalf("scope = %q %q %v", ws, vendor, err)
	}
	for name, id := range map[string]string{"empty": "", "unknown": "nope"} {
		if _, _, err := svc.HarnessScope(ctx, id); !errors.Is(err, agenthub.ErrSessionScopeUnavailable) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A closed session has no scope a change could target.
	if err = svc.CloseSession(ctx, bound.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.HarnessScope(ctx, bound.ID); !errors.Is(err, agenthub.ErrSessionScopeUnavailable) {
		t.Errorf("closed: %v", err)
	}
	// A session with no workspace likewise.
	svc.MatchingWorkspaces = func(string) ([]string, error) { return nil, nil }
	loose, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.HarnessScope(ctx, loose.ID); !errors.Is(err, agenthub.ErrSessionScopeUnavailable) {
		t.Errorf("no workspace: %v", err)
	}
}
