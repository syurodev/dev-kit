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
	s.events = append(s.events, event)
}

func (s *MemorySink) Events() []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()

	events := make([]Event, len(s.events))
	copy(events, s.events)
	return events
}
