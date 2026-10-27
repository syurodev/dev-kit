package preferences

import "errors"

// AgentHarnessPolicy is the person's own setting for the harness: whether an
// agent's narrowing result-budget edit may be applied without being asked. It is
// owned by the window — nothing an agent can call reads it as an input or writes
// it — and a missing, unreadable or invalid stored value means off, which is the
// state in which every proposal asks.
//
// Revision starts at 0 (never set) and rises by one with every change of the
// value, so a proposal bound to a revision goes stale when the policy changes
// after it was prepared, including when it was switched off and on again (the
// boolean alone would look the same). AllowExecutable is deliberately not a
// field: executable proposals are not available and have no setting.
type AgentHarnessPolicy struct {
	Revision      int64 `json:"revision"`
	AutoNarrowing bool  `json:"autoNarrowing"`
}

// ErrHarnessPolicyConflict: the revision the caller saw is not the stored one.
// The caller reads the policy again and decides again; there is no last-write-wins.
var ErrHarnessPolicyConflict = errors.New("preferences: the harness policy changed; read it again")

// AgentHarnessPolicy reads the policy. Unset is the zero value: off, revision 0.
func (s *Service) AgentHarnessPolicy() (AgentHarnessPolicy, error) {
	value, _, err := s.repo.GetAgentHarnessPolicy()
	if err != nil {
		return AgentHarnessPolicy{}, err
	}
	return value, nil
}

// SetAgentHarnessAutoNarrowing sets the opt-in when expect is the stored revision,
// and returns what is stored. It takes the same lock as the harness's final
// recheck and commit (HoldAgentCustomization), so a change cannot land between
// "the policy is unchanged" and the write that relied on it, and it is never
// called while that lock is held. Setting the value it already has changes nothing
// and does not advance the revision. A persistence failure leaves the previous
// value in effect.
func (s *Service) SetAgentHarnessAutoNarrowing(expect int64, on bool) (AgentHarnessPolicy, error) {
	s.customizationMu.Lock()
	defer s.customizationMu.Unlock()
	current, _, err := s.repo.GetAgentHarnessPolicy()
	if err != nil {
		return AgentHarnessPolicy{}, err
	}
	if current.Revision != expect {
		return AgentHarnessPolicy{}, ErrHarnessPolicyConflict
	}
	if current.AutoNarrowing == on {
		return current, nil
	}
	next := AgentHarnessPolicy{Revision: current.Revision + 1, AutoNarrowing: on}
	if err := s.repo.SetAgentHarnessPolicy(next); err != nil {
		return AgentHarnessPolicy{}, err
	}
	return next, nil
}
