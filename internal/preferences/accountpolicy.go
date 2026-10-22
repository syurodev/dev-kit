package preferences

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

// The person's rules for switching between several accounts of one vendor
// (Settings → Agents → Accounts & limits). Local, UI-owned: an agent cannot
// read or change it. It carries no credential and grants nothing — it only
// decides which of the person's own accounts a session runs as.

const (
	// RefusalNone leaves a turn the vendor refused for a real limit alone; the
	// next prompt starts on the next account.
	RefusalNone = "none"
	// RefusalResendIfNoProgress sends the same prompt once on the next account
	// when the refused turn had produced no output and no tool call.
	RefusalResendIfNoProgress = "resendIfNoProgress"
	// RefusalContinue sends one fixed "continue" message on the next account.
	RefusalContinue = "continue"

	DefaultAccountThresholdPercent = 90
	DefaultAccountUsageMaxAge      = 300 // seconds
	maxAccountOrder                = 32
)

var (
	ErrInvalidAccountPolicy  = errors.New("preferences: invalid account policy")
	ErrAccountPolicyConflict = errors.New("account policy revision conflict")
)

// AccountVendorPolicy is the policy for one vendor, keyed by its agent type.
type AccountVendorPolicy struct {
	Enabled bool `json:"enabled"`
	// ThresholdPercent: an account at or over this in any window is left (50–100).
	ThresholdPercent int `json:"thresholdPercent"`
	// WeeklyThresholdPercent replaces ThresholdPercent for weekly windows; 0 = same.
	WeeklyThresholdPercent int `json:"weeklyThresholdPercent,omitempty"`
	// UsageMaxAgeSeconds is how old a shared account's reading may be before a
	// turn start asks the vendor again (60–3600).
	UsageMaxAgeSeconds int `json:"usageMaxAgeSeconds"`
	// Order is the person's preference, first used first. Built-in ids may be listed.
	Order          []string `json:"order,omitempty"`
	OnLimitRefusal string   `json:"onLimitRefusal"`
}

// AgentAccountPolicy is the whole preference, versioned like AgentModelProfiles.
type AgentAccountPolicy struct {
	Revision int64 `json:"revision"`
	// RiskAcknowledgedAt is when the person accepted the terms warning; a vendor
	// cannot be enabled without it.
	RiskAcknowledgedAt string                         `json:"riskAcknowledgedAt,omitempty"`
	Vendors            map[string]AccountVendorPolicy `json:"vendors,omitempty"`
}

func (p AgentAccountPolicy) clone() AgentAccountPolicy {
	out := p
	if p.Vendors != nil {
		out.Vendors = make(map[string]AccountVendorPolicy, len(p.Vendors))
		for k, v := range p.Vendors {
			v.Order = append([]string(nil), v.Order...)
			out.Vendors[k] = v
		}
	}
	return out
}

// NormalizeAccountPolicy fills defaults and refuses what cannot be stored.
func NormalizeAccountPolicy(in AgentAccountPolicy) (AgentAccountPolicy, error) {
	out := in.clone()
	for name, v := range out.Vendors {
		if strings.TrimSpace(name) == "" || len(name) > 32 {
			return AgentAccountPolicy{}, fmt.Errorf("%w: a vendor name must be 1–32 characters", ErrInvalidAccountPolicy)
		}
		if v.ThresholdPercent == 0 {
			v.ThresholdPercent = DefaultAccountThresholdPercent
		}
		if v.UsageMaxAgeSeconds == 0 {
			v.UsageMaxAgeSeconds = DefaultAccountUsageMaxAge
		}
		if v.OnLimitRefusal == "" {
			v.OnLimitRefusal = RefusalNone
		}
		switch {
		case v.ThresholdPercent < 50 || v.ThresholdPercent > 100:
			return AgentAccountPolicy{}, fmt.Errorf("%w: the threshold must be between 50 and 100 percent", ErrInvalidAccountPolicy)
		case v.WeeklyThresholdPercent != 0 && (v.WeeklyThresholdPercent < 50 || v.WeeklyThresholdPercent > 100):
			return AgentAccountPolicy{}, fmt.Errorf("%w: the weekly threshold must be between 50 and 100 percent", ErrInvalidAccountPolicy)
		case v.UsageMaxAgeSeconds < 60 || v.UsageMaxAgeSeconds > 3600:
			return AgentAccountPolicy{}, fmt.Errorf("%w: how old a reading may be must be between 60 and 3600 seconds", ErrInvalidAccountPolicy)
		case v.OnLimitRefusal != RefusalNone && v.OnLimitRefusal != RefusalResendIfNoProgress && v.OnLimitRefusal != RefusalContinue:
			return AgentAccountPolicy{}, fmt.Errorf("%w: unknown choice for a turn refused at its limit", ErrInvalidAccountPolicy)
		case v.Enabled && strings.TrimSpace(out.RiskAcknowledgedAt) == "":
			return AgentAccountPolicy{}, fmt.Errorf("%w: accept the terms warning before turning switching on", ErrInvalidAccountPolicy)
		}
		seen := map[string]bool{}
		order := make([]string, 0, len(v.Order))
		for _, id := range v.Order {
			id = strings.TrimSpace(id)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			order = append(order, id)
		}
		if len(order) > maxAccountOrder {
			return AgentAccountPolicy{}, fmt.Errorf("%w: at most %d accounts in the order", ErrInvalidAccountPolicy, maxAccountOrder)
		}
		v.Order = order
		out.Vendors[name] = v
	}
	return out, nil
}

var accountPolicyMu sync.Mutex

// AgentAccountPolicy reads the policy. Unset is the zero value: nothing enabled.
func (s *Service) AgentAccountPolicy() (AgentAccountPolicy, error) {
	value, _, err := s.repo.GetAgentAccountPolicy()
	if err != nil {
		return AgentAccountPolicy{}, err
	}
	return value.clone(), nil
}

// SetAgentAccountPolicy replaces the policy when the caller's revision is the
// stored one, and returns what was stored.
func (s *Service) SetAgentAccountPolicy(value AgentAccountPolicy) (AgentAccountPolicy, error) {
	accountPolicyMu.Lock()
	defer accountPolicyMu.Unlock()
	current, _, err := s.repo.GetAgentAccountPolicy()
	if err != nil {
		return AgentAccountPolicy{}, err
	}
	if current.Revision != value.Revision {
		return AgentAccountPolicy{}, ErrAccountPolicyConflict
	}
	next, err := NormalizeAccountPolicy(value)
	if err != nil {
		return AgentAccountPolicy{}, err
	}
	next.Revision = current.Revision + 1
	if err := s.repo.SetAgentAccountPolicy(next); err != nil {
		return AgentAccountPolicy{}, err
	}
	return next.clone(), nil
}
