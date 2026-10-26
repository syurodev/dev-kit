package agenthub

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// What a runtime can say about its own end. The host decides whether a writer
// has stopped from this and nothing else: a status of idle or closed, a Kill or
// Close call that returned, a context that ended and a lease that expired all
// describe a request or a clock, not a process.

// StopWitness is one runtime's host-only account of its process.
type StopWitness struct {
	// Exited is closed once the supervisor process was reaped (a Wait that
	// returned). It is never closed by a Kill, a signal or a timeout.
	Exited <-chan struct{}
	// Contained says the backend guarantees that, once the supervisor is
	// reaped, every process of the jail is gone too. A jail without that
	// guarantee can leave a descendant writing after its parent died.
	Contained bool
}

// RuntimeStopWitness is optional on a SessionRuntime. A runtime that does not
// implement it, or answers false, can never be proven stopped.
type RuntimeStopWitness interface {
	StopWitness() (StopWitness, bool)
}

// Reasons a WriterStopProof is not a positive one. They are bounded codes meant
// for a typed refusal or a diagnostic; none carries a path, pid or payload.
const (
	StopOK             = "ok"
	StopOpening        = "runtime_opening"
	StopLive           = "runtime_live"
	StopUnsupported    = "unsupported_runtime"
	StopOpenFailed     = "open_failed"
	StopExitUnobserved = "exit_unobserved"
	StopNoContainment  = "no_descendant_containment"
)

// writerStopWait bounds how long a proof waits for a closed runtime's process
// to be reaped. It waits on a channel, never on a vendor call.
const writerStopWait = 2 * time.Second

// WriterStopProof is what this process knows about every runtime it opened for
// one session during its own lifetime (its Epoch). It is evidence about this
// process only: whatever ran before the process started is outside it, so a
// caller must also require that its claim began inside the epoch.
type WriterStopProof struct {
	Epoch      string
	EpochStart time.Time
	CheckedAt  time.Time
	// Known is false when the answer cannot be given at all.
	Known bool
	// Stopped is true when every runtime opened for the session was reaped.
	Stopped bool
	// Isolated is true when, in addition, each jail contained its descendants.
	Isolated bool
	Reason   string
}

// Positive is the only combination that may release an ownership slot.
func (p WriterStopProof) Positive() bool { return p.Known && p.Stopped && p.Isolated }

type trackedRuntime struct {
	rt      SessionRuntime
	witness StopWitness
}

type sessionRuntimes struct {
	opening int
	// poison is sticky for the epoch: a runtime whose end can never be shown.
	poison string
	open   map[string]trackedRuntime
}

type runtimeTracker struct {
	epoch      string
	epochStart time.Time
	now        func() time.Time

	mu       sync.Mutex
	sessions map[string]*sessionRuntimes
}

func newRuntimeTracker() *runtimeTracker {
	return &runtimeTracker{epoch: uuid.NewString(), epochStart: time.Now().UTC(), now: time.Now, sessions: map[string]*sessionRuntimes{}}
}

func (t *runtimeTracker) record(id string) *sessionRuntimes {
	r := t.sessions[id]
	if r == nil {
		r = &sessionRuntimes{open: map[string]trackedRuntime{}}
		t.sessions[id] = r
	}
	return r
}

// openTracked runs one adapter open, Load or Fork for a session. The tracker
// counts it before the process can exist and settles it after: a runtime that
// comes back is remembered with its witness; one that does not leaves the
// session unprovable for the epoch, because the process a failed open may have
// started was reaped (or not) by the adapter and nobody reported which.
func (s *Service) openTracked(sessionID string, open func() (SessionRuntime, error)) (SessionRuntime, error) {
	t := s.stops
	if t == nil {
		return open()
	}
	t.mu.Lock()
	t.record(sessionID).opening++
	t.mu.Unlock()
	rt, err := open()
	t.mu.Lock()
	defer t.mu.Unlock()
	r := t.record(sessionID)
	r.opening--
	if err != nil || rt == nil {
		r.poison = StopOpenFailed
		return rt, err
	}
	witness, ok := StopWitness{}, false
	if w, supported := rt.(RuntimeStopWitness); supported {
		witness, ok = w.StopWitness()
	}
	if !ok || witness.Exited == nil {
		if r.poison == "" {
			r.poison = StopUnsupported
		}
		return rt, nil
	}
	r.open[uuid.NewString()] = trackedRuntime{rt: rt, witness: witness}
	return rt, nil
}

// WriterStopProof reports whether every runtime this process opened for the
// session has been reaped, and whether the jails contained their descendants.
// It starts, stops and signals nothing; closing the session is a separate act.
// A runtime that is still in the live map is never waited for.
func (s *Service) WriterStopProof(ctx context.Context, sessionID string) WriterStopProof {
	t := s.stops
	proof := WriterStopProof{CheckedAt: time.Now().UTC()}
	if t == nil || sessionID == "" {
		proof.Reason = StopUnsupported
		return proof
	}
	proof.Epoch, proof.EpochStart = t.epoch, t.epochStart

	t.mu.Lock()
	rec := t.sessions[sessionID]
	if rec == nil {
		t.mu.Unlock()
		// Nothing was opened for it in this epoch.
		proof.Known, proof.Stopped, proof.Isolated, proof.Reason = true, true, true, StopOK
		return proof
	}
	if rec.opening > 0 {
		t.mu.Unlock()
		proof.Known, proof.Reason = true, StopOpening
		return proof
	}
	poison := rec.poison
	pending := make(map[string]trackedRuntime, len(rec.open))
	for id, item := range rec.open {
		pending[id] = item
	}
	t.mu.Unlock()

	s.mu.Lock()
	live := s.runtimes[sessionID]
	s.mu.Unlock()
	for _, item := range pending {
		if live != nil && live == item.rt {
			proof.Known, proof.Reason = true, StopLive
			return proof
		}
	}

	wait := time.NewTimer(writerStopWait)
	defer wait.Stop()
	settled := make([]string, 0, len(pending))
	uncontained := false
	for id, item := range pending {
		select {
		case <-item.witness.Exited:
		default:
			select {
			case <-item.witness.Exited:
			case <-wait.C:
				proof.Reason = StopExitUnobserved
				return proof
			case <-ctx.Done():
				proof.Reason = StopExitUnobserved
				return proof
			}
		}
		settled = append(settled, id)
		uncontained = uncontained || !item.witness.Contained
	}

	t.mu.Lock()
	rec = t.record(sessionID)
	for _, id := range settled {
		delete(rec.open, id)
	}
	if uncontained && rec.poison == "" {
		rec.poison = StopNoContainment
	}
	poison = rec.poison
	opening, remaining := rec.opening, len(rec.open)
	t.mu.Unlock()

	switch {
	case opening > 0:
		proof.Known, proof.Reason = true, StopOpening
	case remaining > 0:
		// A runtime opened while this proof waited.
		proof.Known, proof.Reason = true, StopLive
	case poison == StopNoContainment:
		proof.Known, proof.Stopped, proof.Reason = true, true, StopNoContainment
	case poison != "":
		proof.Reason = poison
	default:
		proof.Known, proof.Stopped, proof.Isolated, proof.Reason = true, true, true, StopOK
	}
	return proof
}
