package agenthub

import "time"

// Accessors the external test package needs and production has no use for.
// In _test.go so they cannot be called from the app.

// RuntimeForTest returns a session's live runtime.
func (s *Service) RuntimeForTest(sessionID string) (SessionRuntime, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt, ok := s.runtimes[sessionID]
	return rt, ok
}

// SetPeerClockForTest replaces the clock the messaging guards read. Set it
// before any message is sent; the function itself must be safe to call from
// any goroutine.
func (s *Service) SetPeerClockForTest(now func() time.Time) {
	s.peers.mu.Lock()
	defer s.peers.mu.Unlock()
	s.peers.now = now
}

// The limits the external tests count against.
const (
	PeerPairPerHour = peerPairPerHour
	PeerPairHour    = peerPairHour
)
