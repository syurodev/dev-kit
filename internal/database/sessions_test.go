package database

import (
	"context"
	"testing"
)

type fakeSession struct {
	closed, broken bool
}

func (f *fakeSession) Query(context.Context, string, ...any) (Rows, error) { return emptyRows{}, nil }
func (f *fakeSession) Broken() bool                                        { return f.broken }
func (f *fakeSession) Close()                                              { f.closed = true }

type fakeOpener struct{ opened []*fakeSession }

func (o *fakeOpener) OpenSession(context.Context) (Session, error) {
	s := &fakeSession{}
	o.opened = append(o.opened, s)
	return s, nil
}

func key(console string) sessionKey { return sessionKey{profileID: "p1", sessionID: console} }

// #48. The same console gets the same connection back.
func TestSessionIsKeptForItsConsole(t *testing.T) {
	r, opener := newSessions(), &fakeOpener{}
	first, _ := r.acquire(context.Background(), key("c1"), opener)
	r.release(key("c1"), first)
	second, _ := r.acquire(context.Background(), key("c1"), opener)
	r.release(key("c1"), second)
	if first != second || len(opener.opened) != 1 {
		t.Fatalf("opened %d sessions for one console", len(opener.opened))
	}
}

func TestNoConsoleMeansNoSession(t *testing.T) {
	r, opener := newSessions(), &fakeOpener{}
	if entry, _ := r.acquire(context.Background(), key(""), opener); entry != nil {
		t.Fatal("a statement from no console got a session")
	}
}

// A profile holds a bounded number; the least recently used idle one makes
// room, and a busy one is never taken from under its statement.
func TestSessionsAreBoundedPerProfile(t *testing.T) {
	r, opener := newSessions(), &fakeOpener{}
	for _, c := range []string{"c1", "c2", "c3"} {
		entry, _ := r.acquire(context.Background(), key(c), opener)
		r.release(key(c), entry)
	}
	busy, _ := r.acquire(context.Background(), key("c2"), opener)
	fourth, _ := r.acquire(context.Background(), key("c4"), opener)
	if fourth == nil || !opener.opened[0].closed {
		t.Fatal("the least recently used idle session did not make room")
	}
	if opener.opened[1].closed {
		t.Fatal("a busy session was closed to make room")
	}
	r.release(key("c4"), fourth)
	r.release(key("c2"), busy)

	// All three busy: no room, so the statement runs on the pool.
	for _, c := range []string{"c2", "c3", "c4"} {
		if entry, _ := r.acquire(context.Background(), key(c), opener); entry == nil {
			t.Fatalf("acquire %s", c)
		}
	}
	if entry, _ := r.acquire(context.Background(), key("c5"), opener); entry != nil {
		t.Fatal("a fourth session opened while three were busy")
	}
}

func TestBrokenSessionIsReplaced(t *testing.T) {
	r, opener := newSessions(), &fakeOpener{}
	entry, _ := r.acquire(context.Background(), key("c1"), opener)
	opener.opened[0].broken = true
	r.release(key("c1"), entry)
	if !opener.opened[0].closed {
		t.Fatal("a broken session was kept")
	}
	if next, _ := r.acquire(context.Background(), key("c1"), opener); next == nil || len(opener.opened) != 2 {
		t.Fatal("the console did not get a fresh session")
	}
}

// Closing a connection while a statement runs leaves that session to close
// when the statement ends, not under it.
func TestClosingWaitsForTheRunningStatement(t *testing.T) {
	r, opener := newSessions(), &fakeOpener{}
	idle, _ := r.acquire(context.Background(), key("c1"), opener)
	r.release(key("c1"), idle)
	running, _ := r.acquire(context.Background(), key("c2"), opener)

	r.close(func(k sessionKey) bool { return k.profileID == "p1" })
	if !opener.opened[0].closed {
		t.Fatal("the idle session was not closed")
	}
	if opener.opened[1].closed {
		t.Fatal("the running session was closed under its statement")
	}
	r.release(key("c2"), running)
	if !opener.opened[1].closed {
		t.Fatal("the running session was not closed when its statement ended")
	}
}
