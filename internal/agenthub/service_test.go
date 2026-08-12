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

func newTestService(t *testing.T) *agenthub.Service {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	reg := agenthub.NewRegistry()
	reg.Register(agenthub.NewMockAdapter())
	return agenthub.NewService(reg, agenthub.NewSQLStore(db), func(string, json.RawMessage) {})
}

func TestCreateAndListSession(t *testing.T) {
	svc := newTestService(t)
	s, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != agenthub.SessionStatusIdle {
		t.Fatalf("status=%s", s.Status)
	}
	if s.ID == "" {
		t.Fatal("expected session id")
	}
	if s.ACPSessionID == "" {
		t.Fatal("expected acp session id")
	}
	if s.Depth != 0 {
		t.Fatalf("depth=%d want 0", s.Depth)
	}
	list, err := svc.ListSessions(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
}

func TestCreateUnknownVendor(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.CreateSession(context.Background(), agenthub.CreateInput{AgentType: "nope", Cwd: t.TempDir()})
	if !errors.Is(err, agenthub.ErrVendorUnknown) {
		t.Fatalf("got %v", err)
	}
}

func TestCreateChildIncrementsDepth(t *testing.T) {
	svc := newTestService(t)
	parent, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	child, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType:       agenthub.AgentTypeMock,
		Cwd:             t.TempDir(),
		ParentSessionID: parent.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if child.Depth != 1 || child.ParentSessionID == nil || *child.ParentSessionID != parent.ID {
		t.Fatalf("%+v", child)
	}
}
