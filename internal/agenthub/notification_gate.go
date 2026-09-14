package agenthub

import "sync"

// NotificationGate keeps one native notification thread per session. A later
// event updates that thread instead of creating another toast for every state
// change in the same turn.
type NotificationGate struct {
	mu   sync.Mutex
	sent map[string]struct{}
}

// Next reports whether the session already has a delivered notification and
// records it as delivered. A failed send can be put back with Reset.
func (g *NotificationGate) Next(sessionID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sent == nil {
		g.sent = make(map[string]struct{})
	}
	_, update := g.sent[sessionID]
	g.sent[sessionID] = struct{}{}
	return update
}

// Reset lets the next attempt use SendNotification after a first send failed.
func (g *NotificationGate) Reset(sessionID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.sent, sessionID)
}
