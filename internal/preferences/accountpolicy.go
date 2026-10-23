package preferences

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"
)

// Refusal choices for a turn the vendor refused at a real limit.
const (
	// RefusalNone leaves a turn the vendor refused for a real limit alone; the
	// next prompt starts on the next account.
	RefusalNone = "none"
	// RefusalResendIfNoProgress sends the same prompt once on the next account
	// when the refused turn had produced no output and no tool call.
	RefusalResendIfNoProgress = "resendIfNoProgress"
	// RefusalContinue sends one fixed "continue" message on the next account.
	RefusalContinue = "continue"
)

// Defaults and bounds of an account policy. A zero value in a stored policy
// means "use the default"; the bounds apply after the defaults are filled in.
const (
	// DefaultAccountThresholdPercent is the usage at which an account is left
	// when the person set none.
	DefaultAccountThresholdPercent = 90
	// DefaultAccountUsageMaxAgeSeconds is how old a shared account's reading
	// may be, when the person set none, before a turn start asks the vendor again.
	DefaultAccountUsageMaxAgeSeconds = 300

	minAccountThresholdPercent = 50
	maxAccountThresholdPercent = 100
	minAccountUsageMaxAge      = 60
	maxAccountUsageMaxAge      = 3600
	maxAccountVendorName       = 32
	maxAccountOrder            = 32
)

var (
	// ErrInvalidAccountPolicy rejects a policy the store cannot hold.
	ErrInvalidAccountPolicy = errors.New("preferences: invalid account policy")
	// ErrAccountPolicyConflict: the policy changed since the caller read it.
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

// AgentAccountPolicy is the person's rules for switching between several
// accounts of one vendor (Settings → Agents → Accounts & limits). It is local,
// UI-owned configuration: an agent cannot read or change it. It carries no
// credential and grants nothing — it only decides which of the person's own
// accounts a session runs as. The whole preference is versioned like
// AgentModelProfiles.
type AgentAccountPolicy struct {
	Revision int64 `json:"revision"`
	// RiskAcknowledgedAt is when the person accepted the terms warning; a vendor
	// cannot be enabled without it.
	RiskAcknowledgedAt string                         `json:"riskAcknowledgedAt,omitempty"`
	Vendors            map[string]AccountVendorPolicy `json:"vendors,omitempty"`
}

func thresholdInRange(v int) bool {
	return v >= minAccountThresholdPercent && v <= maxAccountThresholdPercent
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
		if n := utf8.RuneCountInString(name); strings.TrimSpace(name) == "" || n > maxAccountVendorName {
			return AgentAccountPolicy{}, fmt.Errorf("%w: a vendor name must be 1–%d characters", ErrInvalidAccountPolicy, maxAccountVendorName)
		}
		if v.ThresholdPercent == 0 {
			v.ThresholdPercent = DefaultAccountThresholdPercent
		}
		if v.UsageMaxAgeSeconds == 0 {
			v.UsageMaxAgeSeconds = DefaultAccountUsageMaxAgeSeconds
		}
		if v.OnLimitRefusal == "" {
			v.OnLimitRefusal = RefusalNone
		}
		switch {
		case !thresholdInRange(v.ThresholdPercent):
			return AgentAccountPolicy{}, fmt.Errorf("%w: %s: the threshold must be between %d and %d percent", ErrInvalidAccountPolicy, name, minAccountThresholdPercent, maxAccountThresholdPercent)
		case v.WeeklyThresholdPercent != 0 && !thresholdInRange(v.WeeklyThresholdPercent):
			return AgentAccountPolicy{}, fmt.Errorf("%w: %s: the weekly threshold must be between %d and %d percent", ErrInvalidAccountPolicy, name, minAccountThresholdPercent, maxAccountThresholdPercent)
		case v.UsageMaxAgeSeconds < minAccountUsageMaxAge || v.UsageMaxAgeSeconds > maxAccountUsageMaxAge:
			return AgentAccountPolicy{}, fmt.Errorf("%w: %s: how old a reading may be must be between %d and %d seconds", ErrInvalidAccountPolicy, name, minAccountUsageMaxAge, maxAccountUsageMaxAge)
		case v.OnLimitRefusal != RefusalNone && v.OnLimitRefusal != RefusalResendIfNoProgress && v.OnLimitRefusal != RefusalContinue:
			return AgentAccountPolicy{}, fmt.Errorf("%w: %s: unknown choice for a turn refused at its limit (use %s, %s or %s)", ErrInvalidAccountPolicy, name, RefusalNone, RefusalResendIfNoProgress, RefusalContinue)
		case v.Enabled && strings.TrimSpace(out.RiskAcknowledgedAt) == "":
			return AgentAccountPolicy{}, fmt.Errorf("%w: %s: accept the terms warning before turning switching on", ErrInvalidAccountPolicy, name)
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
			return AgentAccountPolicy{}, fmt.Errorf("%w: %s: at most %d accounts in the order", ErrInvalidAccountPolicy, name, maxAccountOrder)
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
