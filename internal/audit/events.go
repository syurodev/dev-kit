package audit

import "sync"

type Outcome string

const (
	OutcomeAllowed Outcome = "allowed"
	OutcomeDenied  Outcome = "denied"
)

type Event struct {
	SubjectID  string
	Capability string
	Scope      string
	Outcome    Outcome
	Metadata   map[string]any
}

type Sink interface {
	Record(event Event)
}

// maxMemoryEvents bounds the retained in-memory audit trail: it exists for
// diagnostics, not compliance, and must not grow without limit for the
// process lifetime (every capability call appends).
const maxMemoryEvents = 10000

type MemorySink struct {
	mu     sync.RWMutex
	events []Event
}

func NewMemorySink() *MemorySink {
	return &MemorySink{}
}

func (s *MemorySink) Record(event Event) {
	event.Metadata = RedactMap(event.Metadata)

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.events) >= maxMemoryEvents {
		// Drop the oldest quarter instead of one-by-one trimming per event.
		drop := len(s.events) / 4
		if drop < 1 {
			drop = 1
		}
		kept := make([]Event, 0, maxMemoryEvents)
		kept = append(kept, s.events[drop:]...)
		s.events = kept
	}
	s.events = append(s.events, event)
}

func (s *MemorySink) Events() []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()

	events := make([]Event, len(s.events))
	copy(events, s.events)
	for i := range events {
		events[i].Metadata = RedactMap(events[i].Metadata)
	}
	return events
}
