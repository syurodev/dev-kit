package agenthub

import (
	"context"
	"log"
	"time"
)

// reopenAfterGrant is the end-of-turn half of a folder grant. The grant is already
// stored; what is left is the session's process, whose jail was built before the
// folder existed. If the session was marked, release the process (the half of
// CloseSession that tears it down) when it is safe: the next prompt, peer message
// or settings call reopens it from the stored row, as after a restart. turnEnded is
// the instant the turn ended: a process handed to a caller after it is left alone.
//
// It is one attempt. A session that is not safe to release keeps its process, the
// mark is spent, and the folder applies at the next reopen (idle release, a
// restart). A failed release never undoes the stored grant.
func (s *Service) reopenAfterGrant(ctx context.Context, id string, turnEnded time.Time) {
	s.mu.Lock()
	marked := s.reopenForGrant[id]
	delete(s.reopenForGrant, id)
	s.mu.Unlock()
	if !marked {
		return
	}
	sess, err := s.store.GetSession(ctx, id)
	if err != nil {
		return
	}
	if !s.releasableNow(ctx, sess) {
		log.Printf("agenthub: session %s keeps its process after a folder grant; the folder applies after the next reopen", id)
		return
	}
	s.releaseRuntimeSince(ctx, sess, time.Now(), 0, turnEnded)
}

// releasableNow is the idle-release safety checks without the clock: the session is
// idle or in error, no child is running or waiting, a process exists and is not being
// reopened, no vendor task would die with it, nothing is waiting on it (a question,
// a permission, a peer message on the way), and the vendor can load it again.
//
// These checks are optimisations and a first screen: they run without the lock a
// new caller takes. The guard is the locked check in releaseRuntimeSince.
func (s *Service) releasableNow(ctx context.Context, sess AgentSession) bool {
	if sess.Status != SessionStatusIdle && sess.Status != SessionStatusError {
		return false
	}
	sessions, err := s.store.ListSessions(ctx)
	if err != nil {
		return false
	}
	for _, other := range sessions {
		if other.ParentSessionID != nil && *other.ParentSessionID == sess.ID &&
			(other.Status == SessionStatusRunning || other.Status == SessionStatusAwaitingPermission) {
			return false
		}
	}
	s.mu.Lock()
	live := s.runtimes[sess.ID] != nil && s.resuming[sess.ID] == nil
	s.mu.Unlock()
	// Optimisation only: releaseRuntimeSince checks bgwork again under the lock.
	if !live || s.bgwork.running(sess.ID) {
		return false
	}
	blocked, ok := s.idleBlockedSessions(ctx)
	if !ok || blocked[sess.ID] {
		return false
	}
	return s.reopenable(ctx, sess)
}
