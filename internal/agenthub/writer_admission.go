package agenthub

import (
	"context"
	"errors"
	"sync"
)

// errWriterAdmissionSealed is the cause carried by ErrManagedWriterBlocked while
// a release holds the session's admission barrier.
var errWriterAdmissionSealed = errors.New("writer admission sealed for release")

// writerAdmission is the host-owned barrier between "a managed session may
// start work" and "an owner paused it". A path that can start a runtime or send
// work registers here first and checks the managed-writer fence again while
// registered; a pause flips its durable state and then drains. An admission
// that rechecked before the flip is counted and drained; one that rechecks
// after is refused. So once DrainWriterAdmission returns, nothing that passed
// an earlier check can still start a runtime or send a prompt.
//
// Two limits are deliberate. A turn already sent is not unsent: the slot is
// released immediately before the runtime is handed the prompt, so a prompt in
// that last step counts as sent and stopping it is the cancellation policy's
// job. And nothing here holds a database transaction or waits on a vendor.
type writerAdmission struct {
	mu    sync.Mutex
	slots map[string]*admissionSlot
}

type admissionSlot struct {
	n      int
	sealed int
	idle   []chan struct{}
}

func newWriterAdmission() *writerAdmission {
	return &writerAdmission{slots: map[string]*admissionSlot{}}
}

func (a *writerAdmission) slot(sessionID string) *admissionSlot {
	slot := a.slots[sessionID]
	if slot == nil {
		slot = &admissionSlot{}
		a.slots[sessionID] = slot
	}
	return slot
}

func (a *writerAdmission) settle(sessionID string, slot *admissionSlot) {
	if slot.n == 0 {
		for _, w := range slot.idle {
			close(w)
		}
		slot.idle = nil
		if slot.sealed == 0 {
			delete(a.slots, sessionID)
		}
	}
}

// enter registers an admission, or reports false while the session is sealed.
// A sealed session refuses every new admission, including one nested inside an
// admission that registered before the seal.
func (a *writerAdmission) enter(sessionID string) (func(), bool) {
	a.mu.Lock()
	slot := a.slot(sessionID)
	if slot.sealed > 0 {
		a.settle(sessionID, slot)
		a.mu.Unlock()
		return nil, false
	}
	slot.n++
	a.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			defer a.mu.Unlock()
			slot.n--
			a.settle(sessionID, slot)
		})
	}, true
}

func (a *writerAdmission) drain(ctx context.Context, sessionID string) error {
	a.mu.Lock()
	slot := a.slots[sessionID]
	if slot == nil || slot.n == 0 {
		a.mu.Unlock()
		return nil
	}
	w := make(chan struct{})
	slot.idle = append(slot.idle, w)
	a.mu.Unlock()
	select {
	case <-w:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// seal refuses new admissions until the returned end is called, then waits for
// the ones already registered to leave. If the context ends first the seal is
// lifted again.
func (a *writerAdmission) seal(ctx context.Context, sessionID string) (func(), error) {
	a.mu.Lock()
	slot := a.slot(sessionID)
	slot.sealed++
	a.mu.Unlock()
	var once sync.Once
	end := func() {
		once.Do(func() {
			a.mu.Lock()
			defer a.mu.Unlock()
			slot.sealed--
			a.settle(sessionID, slot)
		})
	}
	if err := a.drain(ctx, sessionID); err != nil {
		end()
		return nil, err
	}
	return end, nil
}

// enterWriterAdmission registers the caller and rechecks the fence while
// registered. The returned leave is idempotent.
func (s *Service) enterWriterAdmission(ctx context.Context, sessionID string) (func(), error) {
	if s == nil || s.admission == nil {
		return func() {}, s.authorizeManagedWriter(ctx, sessionID)
	}
	leave, ok := s.admission.enter(sessionID)
	if !ok {
		return nil, managedWriterBlocked(errWriterAdmissionSealed)
	}
	if err := s.authorizeManagedWriter(ctx, sessionID); err != nil {
		leave()
		return nil, err
	}
	return leave, nil
}

// DrainWriterAdmission waits until every admission registered for the session
// has left. A pause calls it after its own durable state change, so a start or
// prompt that passed an earlier check has either been sent or been refused when
// it returns. It reports only the context's error; it never cancels a turn.
func (s *Service) DrainWriterAdmission(ctx context.Context, sessionID string) error {
	if s == nil || s.admission == nil {
		return nil
	}
	return s.admission.drain(ctx, sessionID)
}

// SealWriterAdmission is the release half of the barrier. Until end is called
// nothing may start a runtime, send a prompt or write files for the session:
// every Invoke, Steer, resume, rewind and restore is refused, including one that
// registered before the seal and only now reaches the point of starting. It
// returns once the admissions already registered have left. A host holds the seal
// across its stop proof AND the release commit, so a positive proof cannot be
// overtaken by a start that passed an earlier fence check.
func (s *Service) SealWriterAdmission(ctx context.Context, sessionID string) (func(), error) {
	if s == nil || s.admission == nil || sessionID == "" {
		return nil, ErrManagedWriterBlocked
	}
	return s.admission.seal(ctx, sessionID)
}
