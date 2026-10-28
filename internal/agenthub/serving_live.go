package agenthub

import (
	"context"
	"errors"
	"sync"

	"devkit/internal/harness"
)

// Live serving: how a committed change to a session override (S) reaches the
// endpoint that is serving the session, and how every opening path is fenced
// against it. The endpoint itself (admission, SDK discovery, the policy) is
// implemented by the MCP session host; Agent Hub owns the per-session
// coordinator and the pinned launch basis.
//
// Lock order, outermost first: the vendor-customization (V) validation boundary
// held by harnessconfig.Apply, then a session's coordinator (servingCoord.mu),
// then the host's locks (bindMu, mu, the endpoint's transition lock). Nothing
// that waits for a person, a vendor process or a prompt runs under the
// coordinator, and it is never held across a vendor Open/Load/Fork: an opening
// path reserves an epoch before it reads S and checks it again before it
// publishes. Short runtime-map updates take Service.mu inside the coordinator;
// never acquire the coordinator while holding Service.mu.

// EndpointTicket names one operation on an endpoint's admission. A later
// operation supersedes an earlier one, so an old worker's Restore or Install
// cannot undo or complete what a newer operation owns.
type EndpointTicket uint64

// EndpointState is a point-in-time observation of one endpoint incarnation.
type EndpointState struct {
	Incarnation string
	// Paused: admission is closed (quarantine). New calls are refused and the
	// tool list is not offered; calls already admitted finish.
	Paused bool
	Closed bool
	// Fingerprint is the fingerprint of the policy the endpoint holds.
	Fingerprint string
}

// InstallReport is the endpoint's account of an Install. NotificationScheduled
// means the SDK scheduled a list-changed notification; it is not an
// acknowledgement that any client refreshed.
type InstallReport struct {
	ToolsChanged          bool
	NotificationScheduled bool
}

// ServingEndpoint is the live-publication handle of one endpoint incarnation
// (MCPEndpoint.Live). Every method acts on that incarnation only.
type ServingEndpoint interface {
	Incarnation() string
	State() EndpointState
	// Quarantine closes admission and returns the ticket of this operation. It
	// works on an endpoint that is already paused (a reconciliation takes over).
	Quarantine() (EndpointTicket, error)
	// Restore reopens admission under the policy the endpoint held, only if t is
	// still the current operation.
	Restore(t EndpointTicket) error
	// Install makes the endpoint's registry, discovery and policy exactly eff and
	// reopens admission, only if t is still the current operation. On any failure
	// admission stays closed.
	Install(t EndpointTicket, eff harness.Effective, id harness.ServingIdentity) (InstallReport, error)
}

var (
	// ErrHarnessLaunchStale: the session's tool configuration changed while it was
	// starting, so the launch was discarded instead of published with guidance and
	// permissions written for the old one. Opening again resolves the new one.
	ErrHarnessLaunchStale = &harness.Error{Code: harness.CodeSnapshotUnavailable, Message: "The session's tool configuration changed while it was starting. Try again."}

	errServingNoEndpoint = errors.New("agenthub: no live serving endpoint")
)

// launchAttempts bounds the re-preparation of a launch whose configuration moved
// under it. There is no background retry.
const launchAttempts = 3

// servingCoord is one session's commit/publication coordinator. It serializes
// every writer of S, every endpoint replacement and the session's close, and
// holds the session's live endpoint.
type servingCoord struct {
	mu    sync.Mutex
	epoch uint64
	// lifecycle counts the session's Closes only. epoch moves for an S commit and for
	// a Close alike, so epoch alone cannot say which; a launch prepared under an older
	// lifecycle belongs to a session that has since been closed and never resumes.
	lifecycle    uint64
	rt           *liveServing
	closing      bool // a Close is in progress, including durable override cleanup
	closePending bool // a failed Close must be retried before opening again
}

// liveServing is the runtime record of the session's current endpoint: staged
// while it launches, then published. It never outlives the endpoint.
type liveServing struct {
	ep      ServingEndpoint
	basis   *harness.ServingBasis
	serving harness.ServingIdentity // what the endpoint last acknowledged
	// eff is the effective configuration serving describes: the launch's, then
	// each installed one. It never replaces the launch reference.
	eff       harness.Effective
	acked     bool
	published bool // the vendor runtime and durable launch reference were published
	// pending: a change is stored but not installed. Admission stays closed
	// until a reconciliation installs the latest durable S.
	pending bool
	// held: an operation of this coordinator has the endpoint paused.
	held   bool
	ticket EndpointTicket
}

func (s *Service) servingCoord(sessionID string) *servingCoord {
	s.servingMu.Lock()
	defer s.servingMu.Unlock()
	if s.servingCoords == nil {
		s.servingCoords = map[string]*servingCoord{}
	}
	c := s.servingCoords[sessionID]
	if c == nil {
		c = &servingCoord{}
		s.servingCoords[sessionID] = c
	}
	return c
}

// ServingView is a point-in-time observation of a session's live endpoint.
type ServingView struct {
	Active      bool
	Staged      bool
	Incarnation string
	// Epoch counts the session's S commits and lifecycle fences.
	Epoch   uint64
	Basis   *harness.ServingBasis
	Serving harness.ServingIdentity
	// Effective is the configuration Serving names (read-only).
	Effective harness.Effective
	// Quarantined: admission is closed (a publication is in flight or pending).
	Quarantined bool
	// Pending: a stored change awaits reconciliation.
	Pending bool
	// Acknowledged: the endpoint is open, serves exactly Serving and nothing is
	// pending. It is the only state that may be reported as live.
	Acknowledged bool
}

func (c *servingCoord) viewLocked() ServingView {
	v := ServingView{Epoch: c.epoch}
	rt := c.rt
	if rt == nil {
		return v
	}
	st := rt.ep.State()
	if st.Closed {
		return v
	}
	v.Active, v.Incarnation, v.Basis, v.Serving, v.Effective = true, rt.ep.Incarnation(), rt.basis, rt.serving, rt.eff
	v.Staged = !rt.published
	v.Quarantined = st.Paused || rt.pending
	v.Pending = rt.pending
	v.Acknowledged = rt.published && rt.acked && !rt.pending && !st.Paused && st.Fingerprint == rt.serving.Fingerprint
	return v
}

// ServingTarget reads the session's live endpoint without changing anything.
func (s *Service) ServingTarget(sessionID string) ServingView {
	c := s.servingCoord(sessionID)
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.viewLocked()
}

// ServingPublication is the outcome of installing a committed S.
type ServingPublication struct {
	Installed             bool
	Staged                bool // policy installed, but vendor runtime not published
	Pending               bool
	Serving               harness.ServingIdentity
	ToolsChanged          bool
	NotificationScheduled bool
}

// ServingTxn is a session's coordinator while it is held (CoordinateServing).
type ServingTxn struct {
	s  *Service
	c  *servingCoord
	id string
}

// CoordinateServing runs fn holding the session's coordinator. fn must not wait
// for a person, a vendor or another session's coordinator.
func (s *Service) CoordinateServing(ctx context.Context, sessionID string, fn func(*ServingTxn) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c := s.servingCoord(sessionID)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing || c.closePending {
		return ErrHarnessCleanupFailed
	}
	return fn(&ServingTxn{s: s, c: c, id: sessionID})
}

// Target is the observation under the coordinator.
func (t *ServingTxn) Target() ServingView { return t.c.viewLocked() }

// Quarantine closes the current endpoint's admission before a commit.
func (t *ServingTxn) Quarantine() error {
	rt := t.c.rt
	if rt == nil {
		return errServingNoEndpoint
	}
	ticket, err := rt.ep.Quarantine()
	if err != nil {
		return err
	}
	rt.ticket, rt.held = ticket, true
	return nil
}

// Restore reopens admission after a proven rollback, only for the operation that
// closed it. If the endpoint refuses (it was superseded or closed) the endpoint
// is left to its reconciliation.
func (t *ServingTxn) Restore() error {
	rt := t.c.rt
	if rt == nil || !rt.held {
		return nil
	}
	rt.held = false
	if err := rt.ep.Restore(rt.ticket); err != nil {
		rt.pending = true
		return err
	}
	return nil
}

// MarkPending records that S moved, or may have, without installing it: the
// epoch advances, any opening launch is made stale, and the endpoint stays
// paused until reconciled.
func (t *ServingTxn) MarkPending() {
	t.c.epoch++
	rt := t.c.rt
	if rt == nil {
		return
	}
	rt.pending = true
	if !rt.held {
		if ticket, err := rt.ep.Quarantine(); err == nil {
			rt.ticket, rt.held = ticket, true
		}
	}
}

// Publish installs the policy resolved from the pinned basis and the given
// durable S on the current endpoint, and acknowledges it. With no endpoint it
// only advances the epoch (so an opening launch is made stale).
func (t *ServingTxn) Publish(stamp harness.Stamp, durable *harness.Override) (ServingPublication, error) {
	t.c.epoch++
	rt := t.c.rt
	if rt == nil {
		return ServingPublication{}, nil
	}
	pub, err := t.s.installLocked(t.id, rt, stamp, durable)
	return pub, err
}

// installLocked resolves and installs the latest durable S on rt. The caller
// holds the coordinator. It never replays a captured candidate: the effective
// configuration is resolved here from the pinned basis and what it is given.
func (s *Service) installLocked(sessionID string, rt *liveServing, stamp harness.Stamp, durable *harness.Override) (ServingPublication, error) {
	pend := func(err error) (ServingPublication, error) {
		rt.pending = true
		return ServingPublication{Pending: true}, err
	}
	if stamp.Revision < rt.serving.Session.Revision {
		// An older S than the one already served never replaces it.
		return pend(errors.New("agenthub: stale serving stamp"))
	}
	var o *harness.Override
	if durable != nil {
		c := *durable
		c.Revision = stamp.Revision
		o = &c
	}
	eff, err := rt.basis.Resolve(o)
	if err != nil {
		return pend(err)
	}
	id := rt.basis.Identity(stamp, eff)
	if !rt.held {
		ticket, err := rt.ep.Quarantine()
		if err != nil {
			return pend(err)
		}
		rt.ticket, rt.held = ticket, true
	}
	rep, err := rt.ep.Install(rt.ticket, eff, id)
	if err != nil {
		// Admission stays closed: the endpoint's own contract on a failed Install.
		return pend(err)
	}
	rt.serving, rt.eff, rt.acked, rt.pending, rt.held = id, eff, true, false, false
	s.noteServedFacts(sessionID, rt)
	return ServingPublication{Installed: true, Staged: !rt.published, Serving: id, ToolsChanged: rep.ToolsChanged, NotificationScheduled: rep.NotificationScheduled}, nil
}

// ReconcileServing brings the session's endpoint to the latest durable S. It is
// bounded and idempotent: it reads S once under the coordinator, installs it, and
// makes no retry of its own. Concurrent attempts serialize and the later ones
// find nothing to do. It starts no runtime and sends no prompt; with no active
// endpoint there is nothing to reconcile (the next normal launch reads S).
func (s *Service) ReconcileServing(ctx context.Context, sessionID string, readS func(context.Context) (*harness.Override, harness.Stamp, error)) (ServingPublication, error) {
	c := s.servingCoord(sessionID)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing || c.closePending {
		return ServingPublication{}, ErrHarnessCleanupFailed
	}
	rt := c.rt
	if rt == nil || rt.ep.State().Closed {
		return ServingPublication{}, nil
	}
	durable, stamp, err := readS(ctx)
	if err != nil {
		return ServingPublication{Pending: true}, err
	}
	if v := c.viewLocked(); v.Acknowledged && v.Serving.Session == stamp {
		return ServingPublication{Installed: true, Serving: v.Serving}, nil
	}
	// A reconciliation supersedes whatever operation paused the endpoint.
	ticket, err := rt.ep.Quarantine()
	if err != nil {
		return ServingPublication{Pending: true}, err
	}
	rt.ticket, rt.held = ticket, true
	return s.installLocked(sessionID, rt, stamp, durable)
}

// servingReservation is an opening path's claim on the session's epoch.
type servingReservation struct {
	epoch     uint64
	lifecycle uint64
	set       bool
}

// reserveServing records the epoch before S is read for a launch.
func (s *Service) reserveServing(sessionID string) (servingReservation, error) {
	c := s.servingCoord(sessionID)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing || c.closePending {
		return servingReservation{}, ErrHarnessCleanupFailed
	}
	return servingReservation{epoch: c.epoch, lifecycle: c.lifecycle, set: true}, nil
}

// stageServing registers a launch's endpoint with the coordinator before the
// vendor can call it. It fails if S or the session's lifecycle moved since the
// reservation: the caller closes the endpoint and prepares again. From here on
// any S commit reaches this endpoint like a published one.
func (s *Service) stageServing(sessionID string, res servingReservation, ep ServingEndpoint, basis *harness.ServingBasis, launch harness.ServingIdentity, eff harness.Effective) error {
	c := s.servingCoord(sessionID)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing || c.closePending || (res.set && (c.epoch != res.epoch || c.lifecycle != res.lifecycle)) {
		return ErrHarnessLaunchStale
	}
	c.rt = &liveServing{ep: ep, basis: basis, serving: launch, eff: eff, acked: true}
	return nil
}

// publishServingLaunch serializes final validation, the durable launch write
// and runtime-map publication against S commits and Close. commit contains only
// local storage/map operations, never vendor calls, handlers or user callbacks.
func (s *Service) publishServingLaunch(sessionID string, l harnessLaunch, commit func() error) error {
	c := s.servingCoord(sessionID)
	c.mu.Lock()
	defer c.mu.Unlock()
	if reason := launchStaleLocked(c, l); reason != launchCurrent {
		return &launchStaleError{reason}
	}
	if err := commit(); err != nil {
		return err
	}
	if l.incarnation != "" {
		c.rt.published = true
		s.noteServedFacts(sessionID, c.rt)
	}
	return nil
}

// launchStaleReason says why a prepared launch can no longer be published, so a
// caller that has already done something it cannot take back (a rewind that
// restored files) can tell a change of S, which a fresh launch resolves, from a
// Close or a lost endpoint, which it does not.
//
// The reason is decided at the refused publication, under the coordinator, from
// every fact the coordinator holds. Exactly one holds: the order below is from the
// most final to the least, so S having moved never hides a Close or a lost endpoint.
type launchStaleReason int

const (
	launchCurrent launchStaleReason = iota
	// launchStaleClose: a Close is in progress, or a failed Close must be retried.
	launchStaleClose
	// launchStaleLifecycle: a Close has run since the reservation (and finished).
	launchStaleLifecycle
	// launchStaleEndpoint: the staged endpoint is no longer the session's: it was
	// closed, or another launch replaced it.
	launchStaleEndpoint
	// launchStaleEpoch: S moved since the reservation and nothing else did. It is
	// the only reason a fresh launch can resolve.
	launchStaleEpoch
)

// launchStaleError is ErrHarnessLaunchStale with its reason. It is ErrHarnessLaunchStale
// to errors.Is and errors.As, so every existing caller and the bridge mapping see
// the error they always did.
type launchStaleError struct{ reason launchStaleReason }

func (e *launchStaleError) Error() string { return ErrHarnessLaunchStale.Error() }
func (e *launchStaleError) Unwrap() error { return ErrHarnessLaunchStale }

// launchStaleLocked is publishServingLaunch's validation: a Close, the session's
// lifecycle, the staged endpoint, or S, in that order of finality.
func launchStaleLocked(c *servingCoord, l harnessLaunch) launchStaleReason {
	switch {
	case c.closing || c.closePending:
		return launchStaleClose
	case l.reservation.set && c.lifecycle != l.reservation.lifecycle:
		return launchStaleLifecycle
	case l.incarnation != "" && (c.rt == nil || c.rt.ep.Incarnation() != l.incarnation || c.rt.ep.State().Closed):
		return launchStaleEndpoint
	case l.reservation.set && c.epoch != l.reservation.epoch:
		return launchStaleEpoch
	}
	return launchCurrent
}

// checkServingLaunch asks, without publishing anything, whether the launch would
// still be accepted now. A caller about to do something it cannot take back
// before publication (a rewind restoring files) asks first, so the refusal
// comes before the effect. It is not a reservation: a commit that lands after
// this returns still makes publication refuse.
func (s *Service) checkServingLaunch(sessionID string, l harnessLaunch) error {
	c := s.servingCoord(sessionID)
	c.mu.Lock()
	defer c.mu.Unlock()
	if reason := launchStaleLocked(c, l); reason != launchCurrent {
		return &launchStaleError{reason}
	}
	return nil
}

// detachServing forgets the endpoint with the given incarnation, if it is still
// the session's current one. An old endpoint's close cannot detach a replacement.
func (s *Service) detachServing(sessionID, incarnation string) {
	c := s.servingCoord(sessionID)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rt != nil && c.rt.ep.Incarnation() == incarnation {
		c.rt = nil
	}
}

// dropServingCoord discards a deleted session's coordinator. It is called only
// after the rows are durably gone, so a failed delete keeps its fence.
func (s *Service) dropServingCoord(sessionID string) {
	s.servingMu.Lock()
	defer s.servingMu.Unlock()
	delete(s.servingCoords, sessionID)
}

// beginServingClose keeps opening and S commits fenced until the whole Close,
// including its durable S tombstone, has finished. Vendor shutdown and callbacks
// run without the coordinator lock. A failed cleanup stays fenced until retry.
func (s *Service) beginServingClose(sessionID string) (func(bool), error) {
	c := s.servingCoord(sessionID)
	c.mu.Lock()
	if c.closing {
		c.mu.Unlock()
		return nil, ErrSessionBusy
	}
	c.closing, c.closePending = true, true
	c.epoch++
	c.lifecycle++
	if c.rt != nil {
		_, _ = c.rt.ep.Quarantine()
	}
	c.rt = nil
	c.mu.Unlock()
	return func(success bool) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.closing = false
		c.closePending = !success
	}, nil
}
