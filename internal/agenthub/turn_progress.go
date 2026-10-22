package agenthub

// A vendor's refusal error carries no progress, so DevKit watches the update
// stream; the flag is cleared when the next turn starts. Whether a turn refused
// for a real limit may be resent depends on it (account_limit.go).

// noteTurnProgress marks the session's running turn as having produced output
// or a tool call, which is what makes a refusal after it unsafe to resend. The
// echo of the prompt and a usage report are not progress.
func (s *Service) noteTurnProgress(sessionID string, kind UpdateKind) {
	switch kind {
	case UpdateAgentMessageChunk, UpdateAgentThoughtChunk, UpdateToolCall:
		s.mu.Lock()
		s.turnProgress[sessionID] = true
		s.mu.Unlock()
	}
}

func (s *Service) turnHadProgress(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turnProgress[sessionID]
}

// resetTurnProgress starts a turn with no progress; invoke calls it once the
// session is claimed.
func (s *Service) resetTurnProgress(sessionID string) {
	s.mu.Lock()
	delete(s.turnProgress, sessionID)
	s.mu.Unlock()
}
