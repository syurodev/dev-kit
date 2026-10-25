package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"devkit/internal/vaultitem"
)

// PermissionResolver mediates ACP session/request_permission against UI / interaction.
// nil means fail-closed deny (headless / no prompter).
type PermissionResolver func(ctx context.Context, sessionID string, params json.RawMessage) (json.RawMessage, error)

// deniedPermissionOutcome is the ACP reject response when permission is denied
// or times out (fail closed — never auto-approve).
var deniedPermissionOutcome = json.RawMessage(`{"outcome":{"outcome":"cancelled"}}`)

// PendingPermission is a permission prompt awaiting UI resolution.
type PendingPermission struct {
	ID                     string          `json:"id"`
	SessionID              string          `json:"sessionId"`
	Params                 json.RawMessage `json:"params"`
	ScopedApprovalEligible bool            `json:"scopedApprovalEligible,omitempty"`
	CreatedAt              time.Time       `json:"createdAt"`
	// ExpiresAt is when the gate closes on its own. It ships with the request
	// because the UI cannot infer it: a toast may mount long after the request
	// arrived — the rail was closed, the user was in another module, the lazy
	// chunk was still loading — and a countdown started at mount would promise
	// time that no longer exists.
	//
	// omitzero, not a plain field: a zero time.Time marshals as
	// "0001-01-01T00:00:00Z", which the UI read as a deadline in the year 1
	// and drew as "expired" — with the buttons greyed out on a request that
	// was still perfectly answerable.
	ExpiresAt time.Time `json:"expiresAt,omitzero"`
}

// Why a pending permission request is gone (PermissionClosed.Reason).
const (
	PermissionAnswered  = "answered"   // a person answered it (here or in another window)
	PermissionWithdrawn = "withdrawn"  // the agent took it back, or its process went away
	PermissionTurnEnded = "turn_ended" // the turn was stopped and every request of the session denied
	PermissionExpired   = "expired"    // the gate's deadline passed
)

// PermissionClosed says a pending permission request is gone and why, so a
// card still showing it can come down. Fired once per request.
type PermissionClosed struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Reason    string `json:"reason"`
}

// PermissionWaiter blocks ACP session/request_permission until Complete or timeout.
//
// Shared broker for vendor requests and tool gates. Gates translate their
// policy-owned options into the same request shape through ResolveGate; the
// gate still validates the returned decision. UI uses agenthub:permission
// and resolves one request ID through Complete.
type PermissionWaiter struct {
	notify func(PendingPermission)
	// Timeout must match Service.PermissionTimeout — it only stamps ExpiresAt
	// for the UI; the deadline that actually closes the gate is the context in
	// Service.mediatePermission. Zero, the default, is no deadline at all: the
	// request then carries no ExpiresAt and the card draws no countdown.
	Timeout time.Duration
	// OnClosed, when set before the waiter is used, is told once for every
	// request that stops waiting. It is called without the lock held and must
	// not block.
	OnClosed func(PermissionClosed)
	// PersistOpen runs before a request is published to the UI. A failure denies
	// the request without creating an answerable card.
	PersistOpen func(context.Context, PendingRequestRecord) error
	// PersistClose runs before a user answer is released to the agent. A failure
	// leaves the answer pending and cannot turn a failed write into approval.
	PersistClose func(context.Context, PendingRequestResolution) error
	// OnPersistenceError reports metadata durability failures without including
	// the permission params or any request payload. It must be nonblocking and
	// must not re-enter the waiter; some paths report while holding its lock.
	OnPersistenceError func(error)

	mu      sync.Mutex
	pending map[string]*permissionWait // by ID; independent gated calls may queue
}

type permissionWait struct {
	PendingPermission
	result         chan json.RawMessage
	finished       bool
	gate           bool
	ctx            context.Context
	persistedClose bool
	// opened is closed once notify has returned, i.e. the request's opening
	// has been published. A close that happens earlier waits for it, so for
	// one id the window never sees the close before the open.
	opened chan struct{}
}

// NewPermissionWaiter creates a waiter. notify may be nil (still waits for Complete).
func NewPermissionWaiter(notify func(PendingPermission)) *PermissionWaiter {
	return &PermissionWaiter{notify: notify, pending: map[string]*permissionWait{}}
}

// Resolve implements PermissionResolver for Service.ResolvePermission.
func (w *PermissionWaiter) Resolve(ctx context.Context, sessionID string, params json.RawMessage) (json.RawMessage, error) {
	return w.Wait(ctx, sessionID, params)
}

// Wait registers a pending prompt and blocks until Complete, ctx cancel, or deadline.
func (w *PermissionWaiter) Wait(ctx context.Context, sessionID string, params json.RawMessage) (json.RawMessage, error) {
	return w.wait(ctx, sessionID, params, false)
}

// ResolveGate queues independently correlated tool gates from the same session.
func (w *PermissionWaiter) ResolveGate(ctx context.Context, sessionID string, params json.RawMessage) (json.RawMessage, error) {
	return w.wait(ctx, sessionID, params, true)
}

func (w *PermissionWaiter) wait(ctx context.Context, sessionID string, params json.RawMessage, gate bool) (json.RawMessage, error) {
	if ctx.Err() != nil {
		return append(json.RawMessage(nil), deniedPermissionOutcome...), ErrPermissionDenied
	}
	id, err := vaultitem.NewID()
	if err != nil {
		return nil, err
	}
	resCh := make(chan json.RawMessage, 1)
	now := time.Now().UTC()
	p := PendingPermission{
		ID:        id,
		SessionID: sessionID,
		Params:    append(json.RawMessage(nil), params...),
		CreatedAt: now,
	}
	var eligibility struct {
		Origin         string `json:"origin"`
		Tool           string `json:"tool"`
		ProfileID      string `json:"profileId"`
		Env            string `json:"env"`
		Locked         bool   `json:"locked"`
		Fingerprint    string `json:"approvalFingerprint"`
		TargetRevision string `json:"targetRevision"`
		EnvRevision    string `json:"envRevision"`
	}
	if gate && json.Unmarshal(params, &eligibility) == nil {
		p.ScopedApprovalEligible = eligibility.Origin == "devkit_gate" && eligibility.Tool == "db.query" && eligibility.ProfileID != "" && eligibility.Env != "" && !eligibility.Locked && eligibility.Fingerprint != "" && eligibility.TargetRevision != "" && eligibility.EnvRevision != ""
	}
	// Zero ExpiresAt when nothing expires; the card reads that as "no clock".
	if timeout := w.timeout(); timeout > 0 {
		p.ExpiresAt = now.Add(timeout)
	}
	if deadline, ok := ctx.Deadline(); gate && ok && (p.ExpiresAt.IsZero() || deadline.Before(p.ExpiresAt)) {
		p.ExpiresAt = deadline
	}
	opened := make(chan struct{})
	w.mu.Lock()
	for _, other := range w.pending {
		if other.SessionID == sessionID && !(gate && other.gate) {
			// A session prompts synchronously; a second prompt from the same
			// session is a protocol violation, not a queue case.
			w.mu.Unlock()
			return append(json.RawMessage(nil), deniedPermissionOutcome...), ErrPermissionDenied
		}
	}
	if w.PersistOpen != nil {
		if err := w.PersistOpen(ctx, permissionRecord(p)); err != nil {
			w.mu.Unlock()
			w.reportPersistenceError(err)
			return append(json.RawMessage(nil), deniedPermissionOutcome...), ErrPermissionDenied
		}
	}
	if reason := permissionEndReason(ctx, p.ExpiresAt); reason != "" {
		if w.PersistClose != nil {
			if err := w.PersistClose(context.Background(), PendingRequestResolution{ID: id, Reason: reason, ResolvedAt: time.Now().UTC()}); err != nil {
				w.reportPersistenceError(err)
			}
		}
		w.mu.Unlock()
		return append(json.RawMessage(nil), deniedPermissionOutcome...), ErrPermissionDenied
	}
	w.pending[id] = &permissionWait{PendingPermission: p, result: resCh, gate: gate, ctx: ctx, opened: opened}
	w.mu.Unlock()

	// A request answered through Complete is closed here as "answered"; every
	// other exit below closes it first with its own reason, which makes this a no-op.
	defer w.close(id, PermissionAnswered)

	if w.notify != nil {
		w.notify(p)
	}
	close(opened)

	select {
	case <-ctx.Done():
		reason := PermissionWithdrawn
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			reason = PermissionExpired
		}
		w.close(id, reason)
		return append(json.RawMessage(nil), deniedPermissionOutcome...), ErrPermissionDenied
	case outcome := <-resCh:
		if ctx.Err() != nil {
			return append(json.RawMessage(nil), deniedPermissionOutcome...), ErrPermissionDenied
		}
		if isDeniedPermissionOutcome(outcome) {
			return outcome, ErrPermissionDenied
		}
		return outcome, nil
	}
}

// Complete resolves a pending permission prompt with an ACP outcome JSON body.
func (w *PermissionWaiter) Complete(id string, outcome json.RawMessage) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) == 0 {
		return ErrPermissionNotWaiting
	}
	p, ok := w.pending[id]
	if !ok || p.finished || p.ctx.Err() != nil || (!p.ExpiresAt.IsZero() && !time.Now().Before(p.ExpiresAt)) {
		return ErrPermissionNotWaiting
	}
	if w.PersistClose != nil {
		if err := w.PersistClose(context.Background(), PendingRequestResolution{ID: id, Reason: PermissionAnswered, ResolvedAt: time.Now().UTC()}); err != nil {
			w.reportPersistenceError(err)
			return err
		}
		p.persistedClose = true
	}
	if reason := permissionEndReason(p.ctx, p.ExpiresAt); reason != "" {
		if w.PersistClose != nil {
			err := w.PersistClose(context.Background(), PendingRequestResolution{ID: id, Reason: reason, ResolvedAt: time.Now().UTC()})
			w.reportPersistenceError(err)
			p.persistedClose = err == nil
		}
		return ErrPermissionNotWaiting
	}
	select {
	case p.result <- append(json.RawMessage(nil), outcome...):
		p.finished = true
		return nil
	default:
		return ErrPermissionNotWaiting
	}
}

// CancelSession answers every permission this session is waiting on with the
// denied outcome, and reports how many there were.
//
// Stopping a turn has to reach these. A pending request holds three things at
// once: the agent, blocked in session/request_permission; the card on screen,
// counting down; and the session's status. Killing the process alone left all
// three — the agent was gone, but its question stayed on screen with a live
// clock, and the only way out was to answer a request nobody was listening to.
//
// Denied rather than dropped: the outcome is what Wait already sends on a
// timeout, and it is the fail-closed answer. A tool call that was never
// approved must not run because the turn behind it was stopped.
func (w *PermissionWaiter) CancelSession(sessionID string) int {
	type goneRequest struct {
		wait   *permissionWait
		closed PermissionClosed
	}
	var gone []goneRequest
	defer func() {
		for _, g := range gone {
			w.announceClosed(g.wait, g.closed)
		}
	}()
	w.mu.Lock()
	defer w.mu.Unlock()
	cancelled := 0
	for id, p := range w.pending {
		if p.SessionID != sessionID || p.finished {
			continue
		}
		closed := PermissionClosed{ID: id, SessionID: p.SessionID, Reason: PermissionTurnEnded}
		if w.PersistClose != nil {
			persistErr := w.PersistClose(context.Background(), PendingRequestResolution{ID: id, Reason: closed.Reason, ResolvedAt: time.Now().UTC()})
			p.persistedClose = persistErr == nil
			w.reportPersistenceError(persistErr)
		}
		select {
		case p.result <- append(json.RawMessage(nil), deniedPermissionOutcome...):
			cancelled++
		default:
		}
		p.finished = true
		delete(w.pending, id)
		gone = append(gone, goneRequest{p, PermissionClosed{ID: id, SessionID: sessionID, Reason: PermissionTurnEnded}})
	}
	return cancelled
}

// PendingList returns every pending prompt, oldest first.
func (w *PermissionWaiter) PendingList() []PendingPermission {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]PendingPermission, 0, len(w.pending))
	for _, p := range w.pending {
		if p.finished || p.ctx.Err() != nil {
			continue
		}
		cp := p.PendingPermission
		cp.Params = append(json.RawMessage(nil), cp.Params...)
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// Pending returns the oldest pending prompt, or nil.
func (w *PermissionWaiter) Pending() *PendingPermission {
	list := w.PendingList()
	if len(list) == 0 {
		return nil
	}
	return &list[0]
}

func (w *PermissionWaiter) PendingByID(id string) *PendingPermission {
	w.mu.Lock()
	defer w.mu.Unlock()
	p := w.pending[id]
	if p == nil || p.finished || p.ctx.Err() != nil {
		return nil
	}
	cp := p.PendingPermission
	cp.Params = append(json.RawMessage(nil), cp.Params...)
	return &cp
}

// close takes a request out of the queue and announces it, once: only the call
// that finds it still there fires.
func (w *PermissionWaiter) close(id, reason string) {
	w.mu.Lock()
	p, ok := w.pending[id]
	if ok {
		if !p.persistedClose && w.PersistClose != nil {
			w.reportPersistenceError(w.PersistClose(context.Background(), PendingRequestResolution{ID: id, Reason: reason, ResolvedAt: time.Now().UTC()}))
		}
		p.finished = true
		delete(w.pending, id)
	}
	w.mu.Unlock()
	if ok {
		w.announceClosed(p, PermissionClosed{ID: id, SessionID: p.SessionID, Reason: reason})
	}
}

// announceClosed fires the close after the request's open was published. When
// the open is still being built (notify looks the session up) the close waits
// for it in its own goroutine, so a slow notify never blocks a canceller.
func (w *PermissionWaiter) announceClosed(p *permissionWait, c PermissionClosed) {
	select {
	case <-p.opened:
		w.fireClosed(c)
	default:
		go func() {
			<-p.opened
			w.fireClosed(c)
		}()
	}
}

func (w *PermissionWaiter) fireClosed(c PermissionClosed) {
	if w.OnClosed != nil {
		w.OnClosed(c)
	}
}

func (w *PermissionWaiter) timeout() time.Duration {
	return w.Timeout
}

// PermissionTimeoutOrDefault exposes the deadline the gate actually enforces so
// the waiter can stamp a matching ExpiresAt instead of duplicating the constant.
func (s *Service) PermissionTimeoutOrDefault() time.Duration {
	return s.permissionTimeout()
}

func (s *Service) permissionTimeout() time.Duration {
	return s.PermissionTimeout
}

// isDeniedPermissionOutcome reports cancelled/rejected/deny ACP outcomes.
// Malformed bodies fail closed (treated as deny).
func isDeniedPermissionOutcome(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	var wrap struct {
		Outcome struct {
			Outcome string `json:"outcome"`
		} `json:"outcome"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(wrap.Outcome.Outcome)) {
	case "selected":
		return false
	case "cancelled", "canceled", "rejected", "deny", "denied":
		return true
	default:
		// Unknown / empty outcome → fail closed.
		return true
	}
}

// HookDenyReason is what the PreToolUse hook says when it blocks a tool from
// reaching Claude Code's own credentials, and what a test asserts on to prove
// the hook ran at all.
//
// It lives here rather than in cmd/devkit-mcp so the two sides cannot drift:
// the hook is installed through claude-agent-acp's private
// _meta.claudeCode.options channel, and a silently reworded reason would make
// the integration test that guards that channel pass while guarding nothing.
const HookDenyReason = "DevKit blocks agent access to Claude Code's own credentials"

// mediatePermission fails closed: nil resolver, cancel, or deny → deny.
// Never auto-approves. Returns an ACP cancelled/rejected Result body together
// with ErrPermissionDenied so Conn can deliver Result while Invoke fails closed.
func (s *Service) mediatePermission(ctx context.Context, sessionID string, params json.RawMessage) (json.RawMessage, error) {
	return s.mediatePermissionWith(ctx, sessionID, params, s.ResolvePermission)
}

type permissionState struct {
	count    int
	previous SessionStatus
}

func (s *Service) mediatePermissionWith(ctx context.Context, sessionID string, params json.RawMessage, resolver PermissionResolver) (json.RawMessage, error) {
	if resolver == nil {
		return append(json.RawMessage(nil), deniedPermissionOutcome...), ErrPermissionDenied
	}

	s.permissionMu.Lock()
	sess, getErr := s.store.GetSession(ctx, sessionID)
	if getErr != nil || sess.Status == SessionStatusClosed {
		s.permissionMu.Unlock()
		return append(json.RawMessage(nil), deniedPermissionOutcome...), ErrPermissionDenied
	}
	if s.permissionStates == nil {
		s.permissionStates = map[string]*permissionState{}
	}
	state := s.permissionStates[sessionID]
	if state == nil {
		state = &permissionState{previous: sess.Status}
		s.permissionStates[sessionID] = state
	}
	state.count++
	err := s.setSessionStatus(ctx, sessionID, SessionStatusAwaitingPermission)
	s.permissionMu.Unlock()
	if err != nil {
		s.permissionMu.Lock()
		state.count--
		if state.count == 0 {
			delete(s.permissionStates, sessionID)
		}
		s.permissionMu.Unlock()
		return nil, err
	}

	// No deadline unless one is configured. A permission is a question put to
	// a person, and a person reading a 200-line diff — the card offers exactly
	// that — takes longer than any timer worth setting. The vendors' own apps
	// wait, and so does this: the turn ends when the user answers it, or when
	// they stop the session, which cancels ctx and denies.
	waitCtx := ctx
	if timeout := s.permissionTimeout(); timeout > 0 {
		var cancel context.CancelFunc
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	outcome, err := resolver(waitCtx, sessionID, params)

	// Answered, refused, dismissed or timed out, the turn goes on — the agent
	// is told and keeps working — so the session is running again. Left
	// awaiting_permission after a refusal, it said "waiting on you" for the
	// rest of the turn and the UI took down its running indicator while the
	// agent was still writing. Only a stopped turn is left alone: Invoke's
	// error path gives it its final status.
	s.permissionMu.Lock()
	state.count--
	if state.count == 0 {
		delete(s.permissionStates, sessionID)
		s.mu.Lock()
		_, turnActive := s.invoking[sessionID]
		s.mu.Unlock()
		if ctx.Err() == nil || turnActive || state.previous == SessionStatusIdle {
			// Gate expiry cancels the request, not the conversation. Restore
			// status with a recording context after that deadline has elapsed.
			restoreCtx := context.WithoutCancel(ctx)
			if current, getErr := s.store.GetSession(restoreCtx, sessionID); getErr == nil && current.Status == SessionStatusAwaitingPermission {
				if setErr := s.setSessionStatus(restoreCtx, sessionID, state.previous); setErr != nil {
					s.permissionMu.Unlock()
					return nil, setErr
				}
			}
		}
	}
	s.permissionMu.Unlock()

	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			err = ErrPermissionDenied
		}
		if !errors.Is(err, ErrPermissionDenied) {
			// Unexpected resolver failure — still fail closed for the turn.
			err = ErrPermissionDenied
		}
		if len(outcome) == 0 {
			outcome = append(json.RawMessage(nil), deniedPermissionOutcome...)
		}
		return outcome, err
	}

	if isDeniedPermissionOutcome(outcome) {
		if len(outcome) == 0 {
			outcome = append(json.RawMessage(nil), deniedPermissionOutcome...)
		}
		return outcome, ErrPermissionDenied
	}

	return outcome, nil
}

func permissionRecord(p PendingPermission) PendingRequestRecord {
	return PendingRequestRecord{ID: p.ID, SessionID: p.SessionID, Kind: PendingRequestPermission, CreatedAt: p.CreatedAt, ExpiresAt: p.ExpiresAt}
}

func (w *PermissionWaiter) reportPersistenceError(err error) {
	if err != nil && w.OnPersistenceError != nil {
		w.OnPersistenceError(err)
	}
}

func permissionEndReason(ctx context.Context, expiresAt time.Time) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || (!expiresAt.IsZero() && !time.Now().Before(expiresAt)) {
		return PermissionExpired
	}
	if ctx.Err() != nil {
		return PermissionWithdrawn
	}
	return ""
}
