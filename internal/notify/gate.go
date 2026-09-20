package notify

import "sync"

// Gate keeps one native notification thread per target. A later event updates
// that thread instead of creating another toast for every state change about
// the same thing.
//
// Target is a comparable struct, so it is the map key directly: no key format
// to invent, and no way for two modules to collide on an id.
type Gate struct {
	mu   sync.Mutex
	sent map[Target]struct{}
}

// Next reports whether the target already has a delivered notification and
// records it as delivered. A failed send can be put back with Reset.
func (g *Gate) Next(target Target) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sent == nil {
		g.sent = make(map[Target]struct{})
	}
	_, update := g.sent[target]
	g.sent[target] = struct{}{}
	return update
}

// Reset lets the next attempt open a new thread after a first send failed.
func (g *Gate) Reset(target Target) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.sent, target)
}
