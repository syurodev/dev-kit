package preferences

import "errors"

// AgentIdleRelease is the person's setting for giving back the memory of idle
// agent sessions: when on, a session that has been idle for the number of
// minutes below has its process released (the session stays; the next prompt
// reopens it). A session with a parent is a sub-session and uses ChildMinutes.
//
// The policy is owned by the window: nothing an agent can call reads or writes
// it. Unset, and any stored value that is not valid, means the defaults below,
// which are on: the point of the setting is that nobody has to remember it.
type AgentIdleRelease struct {
	Enabled      bool `json:"enabled"`
	ChildMinutes int  `json:"childMinutes"`
	TopMinutes   int  `json:"topMinutes"`
}

const (
	DefaultIdleReleaseChildMinutes = 10
	DefaultIdleReleaseTopMinutes   = 30
	// MinIdleReleaseMinutes keeps the sweep (once a minute) from releasing a
	// session the person just left; MaxIdleReleaseMinutes is one day.
	MinIdleReleaseMinutes = 2
	MaxIdleReleaseMinutes = 24 * 60
)

// ErrIdleReleaseInvalid: a threshold is outside MinIdleReleaseMinutes..MaxIdleReleaseMinutes.
var ErrIdleReleaseInvalid = errors.New("preferences: idle release minutes must be between 2 and 1440")

// DefaultAgentIdleRelease is the policy before the person has chosen one.
func DefaultAgentIdleRelease() AgentIdleRelease {
	return AgentIdleRelease{Enabled: true, ChildMinutes: DefaultIdleReleaseChildMinutes, TopMinutes: DefaultIdleReleaseTopMinutes}
}

func (p AgentIdleRelease) valid() bool {
	in := func(m int) bool { return m >= MinIdleReleaseMinutes && m <= MaxIdleReleaseMinutes }
	return in(p.ChildMinutes) && in(p.TopMinutes)
}

// AgentIdleRelease reads the policy: the stored one, or the defaults when none
// is stored or the stored one is not valid.
func (s *Service) AgentIdleRelease() (AgentIdleRelease, error) {
	value, found, err := s.repo.GetAgentIdleRelease()
	if err != nil {
		return AgentIdleRelease{}, err
	}
	if !found || !value.valid() {
		return DefaultAgentIdleRelease(), nil
	}
	return value, nil
}

// SetAgentIdleRelease stores the policy and returns what is stored. A threshold
// outside the allowed range is refused and nothing changes.
func (s *Service) SetAgentIdleRelease(value AgentIdleRelease) (AgentIdleRelease, error) {
	if !value.valid() {
		return AgentIdleRelease{}, ErrIdleReleaseInvalid
	}
	if err := s.repo.SetAgentIdleRelease(value); err != nil {
		return AgentIdleRelease{}, err
	}
	return value, nil
}
