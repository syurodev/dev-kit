package agenthub

import (
	"time"

	"devkit/internal/preferences"
)

// accountPolicyTTL is how long a policy read is reused. Every usage update and every
// refused turn asks whether a vendor is configured; that is a preference read on the
// one SQLite connection. A few seconds keeps it off the hot path, and a change made
// in Settings calls InvalidateAccountPolicy so it still applies at the next turn.
const accountPolicyTTL = 3 * time.Second

// cachedAccountPolicy is AccountPolicy through a short cache. An error is never
// cached. A read that started before InvalidateAccountPolicy is returned to its
// caller but not stored, so an invalidation can never be undone by a read that
// was already in flight.
func (s *Service) cachedAccountPolicy() (preferences.AgentAccountPolicy, error) {
	if s.AccountPolicy == nil {
		return preferences.AgentAccountPolicy{}, nil
	}
	now := s.policyNow()
	s.mu.Lock()
	if !s.policyAt.IsZero() && now.Sub(s.policyAt) < accountPolicyTTL {
		cached := s.policyCached
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
		s.policyCached, s.policyAt = policy, now
	}
	s.mu.Unlock()
	return policy, nil
}

// InvalidateAccountPolicy drops the cached read; the bridge calls it after a save.
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
