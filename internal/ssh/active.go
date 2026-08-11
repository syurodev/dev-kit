package ssh

// AgentActiveProfileID returns the MCP agent's sticky active SSH profile ID.
// Empty means no profile is selected. UI call paths keep passing an explicit id.
func (s *Service) AgentActiveProfileID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.agentActiveID
}

// SetAgentActiveProfile sets the MCP agent's sticky active SSH profile.
// An empty id clears the selection. Unknown ids fail via store.Get.
func (s *Service) SetAgentActiveProfile(id string) error {
	if id == "" {
		s.mu.Lock()
		s.agentActiveID = ""
		s.mu.Unlock()
		return nil
	}
	if _, err := s.store.Get(id); err != nil {
		return err
	}
	s.mu.Lock()
	s.agentActiveID = id
	s.mu.Unlock()
	return nil
}
