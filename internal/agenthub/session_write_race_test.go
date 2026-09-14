package agenthub_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/storage"
)

// raceStore lands a second writer's change inside the first writer's
// read-then-write window, deterministically: afterGet runs once right after a
// GetSession has read its snapshot, beforeUpdate once right before a whole-row
// UpdateSession writes one. A writer that no longer has such a window never
// fires the hook, and the test lands the change itself afterwards — the claim
// under test is only that both changes survive.
type raceStore struct {
	agenthub.Store
	mu           sync.Mutex
	afterGet     func()
	beforeUpdate func(agenthub.AgentSession) bool
}

func (s *raceStore) GetSession(ctx context.Context, id string) (agenthub.AgentSession, error) {
	sess, err := s.Store.GetSession(ctx, id)
	s.mu.Lock()
	hook := s.afterGet
	s.afterGet = nil
	s.mu.Unlock()
	if hook != nil {
		hook()
	}
	return sess, err
}

func (s *raceStore) UpdateSession(ctx context.Context, sess agenthub.AgentSession) error {
	s.mu.Lock()
	hook := s.beforeUpdate
	if hook != nil && hook(sess) {
		s.beforeUpdate = nil
	}
	s.mu.Unlock()
	return s.Store.UpdateSession(ctx, sess)
}

func newRaceService(t *testing.T) (*agenthub.Service, agenthub.Store, *raceStore) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	inner := agenthub.NewSQLStore(db)
	race := &raceStore{Store: inner}
	reg := agenthub.NewRegistry()
	reg.Register(agenthub.NewMockAdapter())
	return agenthub.NewService(reg, race, func(string, json.RawMessage) {}), inner, race
}

// writeRow is the other writer: a plain read-modify-write straight on the
// store, outside the service under test.
func writeRow(t *testing.T, store agenthub.Store, id string, change func(*agenthub.AgentSession)) {
	t.Helper()
	sess, err := store.GetSession(context.Background(), id)
	if err != nil {
		t.Error(err)
		return
	}
	change(&sess)
	sess.UpdatedAt = time.Now().UTC()
	if err := store.UpdateSession(context.Background(), sess); err != nil {
		t.Error(err)
	}
}

func TestRenameSessionKeepsAStatusChangeThatLandsDuringIt(t *testing.T) {
	svc, inner, race := newRaceService(t)
	ctx := context.Background()
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	fired := false
	turnStarts := func() {
		fired = true
		writeRow(t, inner, sess.ID, func(s *agenthub.AgentSession) { s.Status = agenthub.SessionStatusRunning })
	}
	race.afterGet = turnStarts

	if err := svc.RenameSession(ctx, sess.ID, "Renamed"); err != nil {
		t.Fatal(err)
	}
	if !fired {
		race.afterGet = nil
		turnStarts()
	}

	got, err := inner.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title == nil || *got.Title != "Renamed" {
		t.Fatalf("title = %v, want Renamed", got.Title)
	}
	if got.Status != agenthub.SessionStatusRunning {
		t.Fatalf("status = %s, want running: the rename wrote back the status it had read", got.Status)
	}
}

func TestSessionStatusWritesKeepARenameThatLandsDuringThem(t *testing.T) {
	svc, inner, race := newRaceService(t)
	ctx := context.Background()
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	fired := false
	userRenames := func() {
		fired = true
		writeRow(t, inner, sess.ID, func(s *agenthub.AgentSession) {
			title := "Renamed mid-turn"
			s.Title = &title
		})
	}
	race.beforeUpdate = func(s agenthub.AgentSession) bool {
		if s.Status != agenthub.SessionStatusRunning {
			return false
		}
		userRenames()
		return true
	}

	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	race.mu.Lock()
	race.beforeUpdate = nil
	race.mu.Unlock()
	if !fired {
		userRenames()
	}

	got, err := inner.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title == nil || *got.Title != "Renamed mid-turn" {
		t.Fatalf("title = %v, want the rename: a status write put back the title it had read", got.Title)
	}
	if got.Status != agenthub.SessionStatusIdle {
		t.Fatalf("status = %s, want idle", got.Status)
	}
}
