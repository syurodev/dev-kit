package agenthub

import (
	"time"

	"devkit/internal/preferences"
)

// accountPolicyTTL is how long a policy read is reused. Every usage update and every
// refused turn asks whether a vendor is configured; that is a preference read on the
// one SQLite connection. A few seconds keeps it off the hot path, and a change made
// in Settings must be followed by InvalidateAccountPolicy (whoever saves the
// policy calls it) so the change still applies at the next turn; without that call
// a change shows up only when the TTL runs out.
const accountPolicyTTL = 3 * time.Second

// cachedAccountPolicy is AccountPolicy through a short cache. An error is never
// cached. What it returns is a copy (the Vendors map and every Order slice), so a
// caller may change it without touching what the next read gets. A read that
// started before InvalidateAccountPolicy is returned to its caller but not
// stored, so an invalidation can never be undone by a read that was already in
// flight.
func (s *Service) cachedAccountPolicy() (preferences.AgentAccountPolicy, error) {
	if s.AccountPolicy == nil {
		return preferences.AgentAccountPolicy{}, nil
	}
	now := s.policyNow()
	s.mu.Lock()
	if !s.policyAt.IsZero() && now.Sub(s.policyAt) < accountPolicyTTL {
		cached := clonePolicy(s.policyCached)
		s.mu.Unlock()
		return cached, nil
	}
	gen := s.policyGen
	s.mu.Unlock()
	policy, err := s.AccountPolicy() // outside the lock: a preference read hits the database
	if err != nil {
		return preferences.AgentAccountPolicy{}, err
	}
	s.mu.Lock()
	if s.policyGen == gen {
		s.policyCached, s.policyAt = clonePolicy(policy), now
	}
	s.mu.Unlock()
	return clonePolicy(policy), nil
}

// clonePolicy copies the parts of a policy a caller could change in place: the
// vendors map and each vendor's Order slice.
func clonePolicy(p preferences.AgentAccountPolicy) preferences.AgentAccountPolicy {
	if p.Vendors == nil {
		return p
	}
	vendors := make(map[string]preferences.AccountVendorPolicy, len(p.Vendors))
	for name, vp := range p.Vendors {
		vp.Order = append([]string(nil), vp.Order...)
		vendors[name] = vp
	}
	p.Vendors = vendors
	return p
}

// InvalidateAccountPolicy drops the cached read. It must be called by whoever
// saves the account policy, so the new policy applies to the very next question
// instead of after the TTL.
func (s *Service) InvalidateAccountPolicy() {
	s.mu.Lock()
	s.policyAt = time.Time{}
	s.policyGen++
	s.mu.Unlock()
}

func (s *Service) policyNow() time.Time {
	if s.policyClock != nil {
		return s.policyClock()
	}
	return time.Now()
}
