package agenthub

import (
	"context"
	"log"
	"sync"
	"time"
)

// Idle release gives back the memory of a session nobody is using.
//
// Every open session holds a vendor process, its MCP proxy and whatever the
// agent started inside the jail (build daemons, language servers). Sub-sessions
// that finished their task keep all of that for as long as DevKit runs. Releasing
// the runtime of an idle session is the half of CloseSession that tears the
// process down, without ending the conversation: the row stays idle, the
// vendor's own session id stays, and the next prompt, peer message or settings
// call reopens it through resumeRuntime exactly as after a restart.
//
// What it must never do, and CloseSession does: change the status, fire
// OnSessionClosed, or call HarnessLifecycle.SessionClosed (which deletes the
// session's override S). The session is parked, not closed.

// IdlePolicy is when an idle session's process is released. A zero duration or
// Enabled == false releases nothing.
type IdlePolicy struct {
	Enabled bool
	// ChildAfter applies to a session that has a parent, TopAfter to the others.
	// A child usually runs one task and is finished, so it is released sooner.
	ChildAfter time.Duration
	TopAfter   time.Duration
}

const (
	// DefaultIdleChildAfter and DefaultIdleTopAfter are the policy when the
	// person has not chosen one.
	DefaultIdleChildAfter = 10 * time.Minute
	DefaultIdleTopAfter   = 30 * time.Minute
	// IdleReleaseInterval is how often StartIdleRelease sweeps.
	IdleReleaseInterval = time.Minute
	// idleReleaseCloseWait bounds one teardown. It runs without the caller's
	// cancellation so that DevKit shutting down does not leave a half-closed
	// process, and without an unbounded wait on a vendor that does not exit.
	idleReleaseCloseWait = 20 * time.Second
)

// markUsedLocked records that the session's runtime was just used. mu is held.
func (s *Service) markUsedLocked(id string) {
	if s.lastUsed == nil {
		s.lastUsed = make(map[string]time.Time)
	}
	s.lastUsed[id] = time.Now()
}

// StartIdleRelease sweeps every interval (IdleReleaseInterval when <= 0) until ctx
// ends. policy is read on every sweep, so a change in Settings applies at once.
func (s *Service) StartIdleRelease(ctx context.Context, policy func() IdlePolicy, interval time.Duration) {
	if interval <= 0 {
		interval = IdleReleaseInterval
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				p := policy()
				if !p.Enabled {
					continue
				}
				if n := s.ReleaseIdle(ctx, now, p); n > 0 {
					log.Printf("agenthub: released %d idle session runtime(s)", n)
				}
			}
		}
	}()
}

type idleCandidate struct {
	sess  AgentSession
	after time.Duration
}

// ReleaseIdle is one sweep: it releases the runtime of every session that is
// idle past its threshold and safe to reopen, and returns how many it released.
// now is a parameter so a test does not have to wait. A session whose process
// still has a vendor task (bgwork.go) is kept: releasing the process would kill it.
func (s *Service) ReleaseIdle(ctx context.Context, now time.Time, p IdlePolicy) int {
	if !p.Enabled || p.ChildAfter <= 0 || p.TopAfter <= 0 {
		return 0
	}
	sessions, err := s.store.ListSessions(ctx)
	if err != nil {
		log.Printf("agenthub: idle release: list sessions: %v", err)
		return 0
	}
	// A parent keeps its process while a child still works: the child's result
	// is about to wake it.
	activeChild := map[string]bool{}
	for _, sess := range sessions {
		if sess.ParentSessionID != nil && (sess.Status == SessionStatusRunning || sess.Status == SessionStatusAwaitingPermission) {
			activeChild[*sess.ParentSessionID] = true
		}
	}

	var candidates []idleCandidate
	s.mu.Lock()
	for _, sess := range sessions {
		if sess.Status != SessionStatusIdle && sess.Status != SessionStatusError {
			continue
		}
		// The bgwork check here is an optimisation: releaseRuntime repeats it under
		// the lock, and that one is the guard.
		if s.runtimes[sess.ID] == nil || s.resuming[sess.ID] != nil || activeChild[sess.ID] || s.bgwork.running(sess.ID) {
			continue
		}
		after := p.TopAfter
		if sess.ParentSessionID != nil {
			after = p.ChildAfter
		}
		last := sess.UpdatedAt
		if used := s.lastUsed[sess.ID]; used.After(last) {
			last = used
		}
		if now.Sub(last) < after {
			continue
		}
		candidates = append(candidates, idleCandidate{sess: sess, after: after})
	}
	s.mu.Unlock()
	if len(candidates) == 0 {
		return 0
	}

	blocked, ok := s.idleBlockedSessions(ctx)
	if !ok {
		return 0 // cannot tell what is pending: fail closed, release nothing
	}
	released := 0
	for _, c := range candidates {
		if ctx.Err() != nil {
			break
		}
		if blocked[c.sess.ID] || !s.reopenable(ctx, c.sess) {
			continue
		}
		if s.releaseRuntime(ctx, c.sess, now, c.after) {
			released++
		}
	}
	return released
}

// reopenable reports whether the next use of the session can bring it back. A
// session the vendor cannot load and that has a conversation would be gone for
// good: resumeRuntime answers ErrUnavailable.
func (s *Service) reopenable(ctx context.Context, sess AgentSession) bool {
	adapter, ok := s.reg.Get(sess.AgentType)
	if !ok {
		return false
	}
	if s.neverPrompted(ctx, sess) {
		return true // reopened with Open: nothing exists on the vendor's side to lose
	}
	_, loader := adapter.(SessionLoader)
	return loader && sess.ACPSessionID != ""
}

// idleBlockedSessions names the sessions with a question or permission waiting,
// or an accepted peer message not yet delivered. ok is false when a store that
// should know could not answer.
func (s *Service) idleBlockedSessions(ctx context.Context) (map[string]bool, bool) {
	blocked := map[string]bool{}
	if store, has := s.store.(PendingRequestStore); has {
		summaries, err := store.ListPendingRequestSummaries(ctx)
		if err != nil {
			log.Printf("agenthub: idle release: pending requests: %v", err)
			return nil, false
		}
		for _, sum := range summaries {
			blocked[sum.SessionID] = true
		}
	}
	if store, has := s.store.(PeerOutboxStore); has {
		rows, err := store.ListPeerOutbox(ctx)
		if err != nil {
			log.Printf("agenthub: idle release: peer outbox: %v", err)
			return nil, false
		}
		for _, row := range rows {
			if row.State == "queued" || row.State == "dispatching" {
				blocked[row.To] = true
			}
		}
	}
	return blocked, true
}

// releaseRuntime tears one session's process down and leaves the session itself
// alone. It reports whether it did.
//
// The runtime leaves the maps and a resume barrier goes up in the same critical
// section as the last check, so a caller that arrives afterwards finds no
// runtime, waits for the barrier, and reopens once the old process is gone (two
// processes holding one vendor thread is an error for Codex). A caller that
// arrived first stamped lastUsed under the same lock and the check refuses.
func (s *Service) releaseRuntime(ctx context.Context, sess AgentSession, now time.Time, after time.Duration) bool {
	return s.releaseRuntimeSince(ctx, sess, now, after, time.Time{})
}

// releaseRuntimeSince is releaseRuntime with a floor on use: a runtime handed to a
// caller after since is not released. The zero time means no floor. The end-of-turn
// reopen after a folder grant passes the instant its turn ended, because it has no
// idle threshold to protect a process that the next turn has just taken.
//
// A session whose turn claim is held (s.invoking) is never released: its turn is
// running on this process, or about to. This check, under the lock, is the guard;
// the checks callers make before it (the sweep's candidate loop, releasableNow) are
// optimisations that save work, not the guard.
func (s *Service) releaseRuntimeSince(ctx context.Context, sess AgentSession, now time.Time, after time.Duration, since time.Time) bool {
	id := sess.ID
	call := &resumeCall{done: make(chan struct{})}

	s.mu.Lock()
	rt := s.runtimes[id]
	_, claimed := s.invoking[id]
	if rt == nil || s.resuming[id] != nil || claimed || now.Sub(s.lastUsed[id]) < after ||
		(!since.IsZero() && s.lastUsed[id].After(since)) || s.bgwork.running(id) {
		s.mu.Unlock()
		return false
	}
	usage, hadUsage := s.runtimeUsage[id]
	account, hadAccount := s.sessionAccount[id]
	used := s.lastUsed[id]
	closeEndpoint := s.endpoints[id]
	delete(s.runtimes, id)
	delete(s.runtimeUsage, id)
	delete(s.lastUsed, id)
	delete(s.endpoints, id)
	delete(s.sessionAccount, id)
	s.forgetWorkspaceContextLocked(id)
	s.resuming[id] = call
	s.mu.Unlock()

	finish := func() {
		s.mu.Lock()
		delete(s.resuming, id)
		s.mu.Unlock()
		close(call.done)
	}

	// The harness's close fence: it quarantines the endpoint this process was
	// served from and moves the serving state on, which is what a later reopen
	// relies on. A close already in progress means someone else owns the teardown.
	endClose, err := s.beginServingClose(id)
	if err != nil {
		s.mu.Lock()
		s.runtimes[id] = rt
		if hadUsage {
			s.runtimeUsage[id] = usage
		}
		if hadAccount {
			s.sessionAccount[id] = account
		}
		if closeEndpoint != nil {
			s.endpoints[id] = closeEndpoint
		}
		s.lastUsed[id] = used
		s.mu.Unlock()
		finish()
		return false
	}

	hostCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), idleReleaseCloseWait)
	defer cancel()
	if closeEndpoint != nil {
		closeEndpoint = sync.OnceFunc(closeEndpoint)
	}
	// The conversation is complete on disk before the process goes: nothing
	// flushes for a session with no runtime. A failed write is reported, not fatal.
	if err := s.recorder.Flush(hostCtx, id); err != nil && s.OnRecordError != nil {
		s.OnRecordError(id, err)
	}
	if err := rt.Close(hostCtx); err != nil {
		log.Printf("agenthub: idle release: close session %s: %v", id, err)
	}
	if closeEndpoint != nil {
		closeEndpoint()
	}
	endClose(true)
	finish()
	log.Printf("agenthub: released idle runtime session=%s vendor=%s idle=%ds", id, sess.AgentType, int(now.Sub(used).Seconds()))
	return true
}
