package agenthub

import (
	"crypto/sha256"
	"encoding/hex"
)

// workspaceContextRefreshTurns is how many unchanged turns may pass before the
// block is sent again anyway. A vendor that compacts its conversation can
// summarise the block away, and an agent that no longer sees it would carry on
// without the workspace's rules. Re-sending costs one block per this many
// turns instead of one per turn.
const workspaceContextRefreshTurns = 20

// workspaceContextSent is what the session's agent last received.
type workspaceContextSent struct {
	fingerprint string
	// skipped counts the turns since, which went out without the block.
	skipped int
}

func workspaceContextFingerprint(block string) string {
	sum := sha256.Sum256([]byte(block))
	return hex.EncodeToString(sum[:])
}

// workspaceContextDue reports whether this turn must carry the block: the agent
// has never received one, what it received differs from the block now, or it
// has been a while. Unchanged text is already in the agent's conversation.
func (s *Service) workspaceContextDue(sessionID, fingerprint string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sent, ok := s.workspaceSent[sessionID]
	return !ok || sent.fingerprint != fingerprint || sent.skipped >= workspaceContextRefreshTurns
}

// workspaceContextDelivered records a turn that went through. A turn that
// failed records nothing, so the next one carries the block again: the agent
// may never have seen it.
func (s *Service) workspaceContextDelivered(sessionID, fingerprint string, carried bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if carried {
		if s.workspaceSent == nil {
			s.workspaceSent = make(map[string]workspaceContextSent)
		}
		s.workspaceSent[sessionID] = workspaceContextSent{fingerprint: fingerprint}
		return
	}
	// Only a session still on record counts a skipped turn: one forgotten
	// mid-turn (closed, resumed) must send the block next time.
	if sent, ok := s.workspaceSent[sessionID]; ok {
		sent.skipped++
		s.workspaceSent[sessionID] = sent
	}
}

// forgetWorkspaceContextLocked drops what a session's agent is known to have
// received. Called, with s.mu held, wherever the process holding the
// conversation goes away or is replaced: a resumed vendor session loads its
// history, but what that history still contains is the vendor's to say.
func (s *Service) forgetWorkspaceContextLocked(sessionID string) {
	delete(s.workspaceSent, sessionID)
}
