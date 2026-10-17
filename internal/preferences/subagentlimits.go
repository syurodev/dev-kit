package preferences

import (
	"errors"
	"fmt"
	"sync"
)

// Subagent limits are the person's cap on how many subagents a vendor's agent
// may run at once. They are local, UI-owned configuration: an agent cannot read
// or change them, and a limit carries no permission — it only makes a session
// able to do less.
//
// Only Codex has a control (measured on 0.163.0, docs in AGENTS.md "Codex
// subagent limits"): its thread cap bounds the whole tree, so subagents that
// spawn subagents draw from the same count. Codex's agents.max_depth is
// accepted and ignored by its app-server, so there is deliberately no depth
// setting here.
const (
	// MaxCodexSubagentsCap is the highest value Settings may store. Codex takes
	// any number; every subagent is a full model session, so a typo of 100
	// should be refused rather than obeyed.
	MaxCodexSubagentsCap = 16
)

var (
	// ErrInvalidSubagentLimits rejects a value the store cannot hold.
	ErrInvalidSubagentLimits = errors.New("preferences: invalid subagent limits")
	// ErrSubagentLimitsConflict: the limits changed since the caller read them.
	ErrSubagentLimitsConflict = errors.New("subagent limits revision conflict")
)

// CodexSubagentLimits is what the person capped for Codex. A nil field is
// unset: DevKit writes nothing and Codex keeps its own default.
type CodexSubagentLimits struct {
	// MaxSubagents is how many subagents may run at the same time, counting
	// those a subagent started. 0 means none (every spawn is refused).
	MaxSubagents *int `json:"maxSubagents,omitempty"`
}

// AgentSubagentLimits is the whole preference, versioned like
// AgentModelProfiles so a stale Settings form cannot overwrite a newer value.
type AgentSubagentLimits struct {
	Revision int64               `json:"revision"`
	Codex    CodexSubagentLimits `json:"codex"`
}

func (l AgentSubagentLimits) clone() AgentSubagentLimits {
	out := l
	if l.Codex.MaxSubagents != nil {
		v := *l.Codex.MaxSubagents
		out.Codex.MaxSubagents = &v
	}
	return out
}

// NormalizeSubagentLimits refuses what cannot be stored: a negative count or
// one above MaxCodexSubagentsCap.
func NormalizeSubagentLimits(in CodexSubagentLimits) (CodexSubagentLimits, error) {
	if in.MaxSubagents == nil {
		return CodexSubagentLimits{}, nil
	}
	n := *in.MaxSubagents
	if n < 0 {
		return CodexSubagentLimits{}, fmt.Errorf("%w: the number of subagents cannot be negative", ErrInvalidSubagentLimits)
	}
	if n > MaxCodexSubagentsCap {
		return CodexSubagentLimits{}, fmt.Errorf("%w: at most %d subagents (each one is a full model session)", ErrInvalidSubagentLimits, MaxCodexSubagentsCap)
	}
	return CodexSubagentLimits{MaxSubagents: &n}, nil
}

var subagentLimitsMu sync.Mutex

// AgentSubagentLimits reads the limits. Unset is the zero value.
func (s *Service) AgentSubagentLimits() (AgentSubagentLimits, error) {
	value, _, err := s.repo.GetAgentSubagentLimits()
	if err != nil {
		return AgentSubagentLimits{}, err
	}
	return value.clone(), nil
}

// SetAgentSubagentLimits replaces the limits when the caller's revision is the
// stored one, and returns what was stored.
func (s *Service) SetAgentSubagentLimits(value AgentSubagentLimits) (AgentSubagentLimits, error) {
	subagentLimitsMu.Lock()
	defer subagentLimitsMu.Unlock()
	current, _, err := s.repo.GetAgentSubagentLimits()
	if err != nil {
		return AgentSubagentLimits{}, err
	}
	if current.Revision != value.Revision {
		return AgentSubagentLimits{}, ErrSubagentLimitsConflict
	}
	codex, err := NormalizeSubagentLimits(value.Codex)
	if err != nil {
		return AgentSubagentLimits{}, err
	}
	next := AgentSubagentLimits{Revision: current.Revision + 1, Codex: codex}
	if err := s.repo.SetAgentSubagentLimits(next); err != nil {
		return AgentSubagentLimits{}, err
	}
	return next.clone(), nil
}
