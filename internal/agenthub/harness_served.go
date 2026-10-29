package agenthub

// ServedTools is the DevKit tool surface the session's enforcing endpoint last
// acknowledged, sorted, as a copy. live is false when no enforcing endpoint is
// running for the session (none opened, a legacy endpoint that cannot enforce,
// one that is closed or never acknowledged a surface); the tools are then nil.
//
// It is a point-in-time observation, not a promise: a live change can follow
// the read. It reads what the endpoint serves, never what the launch offered or
// what the saved configuration resolves to now, so a group revoked after launch
// is not reported. Whether the endpoint is paused or has a change pending is
// HarnessStatus.Serving's to say; this read adds no vendor logic.
func (s *Service) ServedTools(sessionID string) (tools []string, live bool) {
	if sessionID == "" {
		return nil, false
	}
	served := s.servingServed(sessionID)
	if served == nil {
		return nil, false
	}
	return append([]string(nil), served.Tools...), true
}
