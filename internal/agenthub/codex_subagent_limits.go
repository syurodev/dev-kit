package agenthub

import (
	"strconv"

	"devkit/internal/preferences"
)

// SubagentLimits is the person's cap on subagents for one session (Service.
// applySubagents fills it from Settings). Nil means unset: DevKit passes the
// vendor nothing and the vendor's own default applies.
type SubagentLimits struct {
	// MaxSubagents is how many subagents may run at once, counting those a
	// subagent started. 0 is none. Only Codex has a control for it.
	MaxSubagents *int
}

// codexSubagentLimitArgs turns the limits into `-c` flags for this process only
// — never config.toml, which every session shares and DevKit rewrites on every
// spawn. A running session keeps what it started with until it is reloaded.
//
// Measured on Codex 0.163.0's app-server (cmd/pdfe2e/subagent_limits_test.go):
//   - features.multi_agent_v2.max_concurrent_threads_per_session=M gives M
//     slots *including* the main agent, so M-1 subagents. It wins over the
//     agents.* key when both are set, and a person's own config.toml (the
//     shared-home import carries `features`) may set it, so DevKit sets it.
//   - agents.max_threads=N gives N subagents (N+1 slots). 0 stops Codex
//     starting at all ("must be at least 1"), so it is only written for N>0;
//     it is there so a Codex that lacks the v2 key still obeys.
//   - agents.max_depth is accepted and ignored: nesting is bounded only by the
//     slot count, which is why there is no depth setting.
func codexSubagentLimitArgs(limits SubagentLimits) []string {
	if limits.MaxSubagents == nil {
		return nil
	}
	n := *limits.MaxSubagents
	if n < 0 {
		n = 0
	}
	if n > preferences.MaxCodexSubagentsCap {
		n = preferences.MaxCodexSubagentsCap
	}
	args := []string{"-c", "features.multi_agent_v2.max_concurrent_threads_per_session=" + strconv.Itoa(n+1)}
	if n > 0 {
		args = append(args, "-c", "agents.max_threads="+strconv.Itoa(n))
	}
	return args
}

func (s *Service) subagentLimits() preferences.AgentSubagentLimits {
	if s.SubagentLimits == nil {
		return preferences.AgentSubagentLimits{}
	}
	limits, err := s.SubagentLimits()
	if err != nil {
		return preferences.AgentSubagentLimits{}
	}
	return limits
}

// applySubagentLimits copies the person's cap for this session's vendor into
// the spec. Claude has none: its subagents cannot start subagents, and DevKit
// passes no count limit.
func (s *Service) applySubagentLimits(spec *OpenSpec, vendor AgentType) {
	if vendor != AgentTypeCodex {
		return
	}
	if n := s.subagentLimits().Codex.MaxSubagents; n != nil {
		v := *n
		spec.SubagentLimits = SubagentLimits{MaxSubagents: &v}
	}
}
