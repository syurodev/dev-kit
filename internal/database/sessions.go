package database

import (
	"context"
	"sync"
	"time"
)

/*
Session is one connection kept for one console, so what a statement leaves on
its session is there for the next one (#48): USE, SET search_path, a variable,
a temporary table. Without it every Run could land on a different pooled
connection — and on MySQL, which kept no idle connections, always did.

A session is cancelled by asking the server to stop the statement, never by
hanging up, which would take the session with the statement.
*/
type Session interface {
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	// Broken reports a connection that can no longer be used, so the next run
	// opens a fresh session instead of failing on this one forever.
	Broken() bool
	Close()
}

// SessionOpener is optional on Conn, like Transactor.
type SessionOpener interface {
	OpenSession(ctx context.Context) (Session, error)
}

const (
	// sessionIdle closes a session nobody has run anything on for this long:
	// a held connection is a connection taken from the user's server.
	sessionIdle = 10 * time.Minute
	// maxSessionsPerProfile leaves the rest of a profile's pool of six for
	// table tabs and the catalogue. ponytail: a fixed number; per-profile
	// sizing is the upgrade if someone keeps more consoles open at once.
	maxSessionsPerProfile = 3
)

type sessionKey struct{ profileID, sessionID string }

type sessionEntry struct {
	session  Session
	busy     bool
	lastUsed time.Time
	idle     *time.Timer
}

type sessions struct {
	mu   sync.Mutex
	open map[sessionKey]*sessionEntry
}

func newSessions() *sessions {
	return &sessions{open: make(map[sessionKey]*sessionEntry)}
}

/*
acquire returns the session for key, opening one when there is none, and marks
it busy until release.

It returns nil — run on the pool, as before sessions — when the key names no
console, or when every session the profile may hold is running a statement.
Otherwise the least recently used idle session makes room.
*/
func (r *sessions) acquire(ctx context.Context, key sessionKey, opener SessionOpener) (*sessionEntry, error) {
	if key.sessionID == "" || opener == nil {
		return nil, nil
	}
	r.mu.Lock()
	if entry := r.open[key]; entry != nil {
		if entry.busy {
			r.mu.Unlock()
			return nil, nil
		}
		entry.busy = true
		entry.idle.Stop()
		r.mu.Unlock()
		return entry, nil
	}
	var evicted *sessionEntry
	count := 0
	var oldest sessionKey
	for k, e := range r.open {
		if k.profileID != key.profileID {
			continue
		}
		count++
		if !e.busy && (evicted == nil || e.lastUsed.Before(evicted.lastUsed)) {
			evicted, oldest = e, k
		}
	}
	if count >= maxSessionsPerProfile {
		if evicted == nil {
			r.mu.Unlock()
			return nil, nil
		}
		delete(r.open, oldest)
		evicted.idle.Stop()
	} else {
		evicted = nil
	}
	r.mu.Unlock()
	if evicted != nil {
		evicted.session.Close()
	}

	session, err := opener.OpenSession(ctx)
	if err != nil {
		return nil, err
	}
	entry := &sessionEntry{session: session, busy: true}
	entry.idle = time.AfterFunc(sessionIdle, func() { r.closeIdle(key, entry) })
	entry.idle.Stop()
	r.mu.Lock()
	if r.open[key] != nil {
		// Opened twice at once for one console, which one run per console
		// should prevent. The first one stays.
		r.mu.Unlock()
		session.Close()
		return nil, nil
	}
	r.open[key] = entry
	r.mu.Unlock()
	return entry, nil
}

// release ends a run. A broken session, or one closed while it was running,
// is closed now; any other starts its idle clock.
func (r *sessions) release(key sessionKey, entry *sessionEntry) {
	r.mu.Lock()
	if r.open[key] != entry || entry.session.Broken() {
		if r.open[key] == entry {
			delete(r.open, key)
		}
		r.mu.Unlock()
		entry.session.Close()
		return
	}
	entry.busy = false
	entry.lastUsed = time.Now()
	entry.idle.Reset(sessionIdle)
	r.mu.Unlock()
}

func (r *sessions) closeIdle(key sessionKey, entry *sessionEntry) {
	r.mu.Lock()
	if r.open[key] != entry || entry.busy {
		r.mu.Unlock()
		return
	}
	delete(r.open, key)
	r.mu.Unlock()
	entry.session.Close()
}

// close drops the sessions match selects. One that is running is closed by
// its release instead, when its statement ends.
func (r *sessions) close(match func(sessionKey) bool) {
	r.mu.Lock()
	var idle []*sessionEntry
	for k, e := range r.open {
		if !match(k) {
			continue
		}
		delete(r.open, k)
		e.idle.Stop()
		if !e.busy {
			idle = append(idle, e)
		}
	}
	r.mu.Unlock()
	for _, e := range idle {
		e.session.Close()
	}
}

// CloseSession closes one console's session, or every session of the profile
// when sessionID is empty — a console closed, a connection switched away from.
func (s *Service) CloseSession(id, sessionID string) {
	s.sessions.close(func(k sessionKey) bool {
		return k.profileID == id && (sessionID == "" || k.sessionID == sessionID)
	})
}

// closeConnection drops a profile's sessions and then its pool. In that order:
// pgxpool's Close waits for every connection it lent out.
func (s *Service) closeConnection(id string) {
	s.CloseSession(id, "")
	s.pools.Remove(id)
}
