package agenthub

import (
	"context"
	"sync"
)

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
	n    int
	idle []chan struct{}
}

func newWriterAdmission() *writerAdmission {
	return &writerAdmission{slots: map[string]*admissionSlot{}}
}

func (a *writerAdmission) enter(sessionID string) func() {
	a.mu.Lock()
	slot := a.slots[sessionID]
	if slot == nil {
		slot = &admissionSlot{}
		a.slots[sessionID] = slot
	}
	slot.n++
	a.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			defer a.mu.Unlock()
			slot.n--
			if slot.n > 0 {
				return
			}
			for _, w := range slot.idle {
				close(w)
			}
			delete(a.slots, sessionID)
		})
	}
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

// enterWriterAdmission registers the caller and rechecks the fence while
// registered. The returned leave is idempotent.
func (s *Service) enterWriterAdmission(ctx context.Context, sessionID string) (func(), error) {
	if s == nil || s.admission == nil {
		return func() {}, s.authorizeManagedWriter(ctx, sessionID)
	}
	leave := s.admission.enter(sessionID)
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
