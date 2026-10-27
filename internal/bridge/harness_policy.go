package bridge

import (
	"context"
)

// AgentHarnessPolicyState is the person's setting for the harness as the window
// sees it. Revision rises with every change; a save names the revision it read.
type AgentHarnessPolicyState struct {
	Revision int64 `json:"revision"`
	// AutoNarrowing lets an agent's narrowing result-budget edit be applied without
	// being asked. Off unless the person turned it on. It never approves a tool
	// call, an environment confirmation or a permission, and widening, mixed and
	// executable changes always ask.
	AutoNarrowing bool `json:"autoNarrowing"`
}

// AgentHarnessPolicyGet reads the setting. Window only: the check is made here in
// Go and no MCP tool reaches this method.
func (s *AppService) AgentHarnessPolicyGet(ctx context.Context) (AgentHarnessPolicyState, error) {
	result, err := s.call(ctx, "builtin.settings", "preferences:read", "agent_harness_policy", map[string]any{"action": "get_agent_harness_policy"}, func(callCtx context.Context) (any, error) {
		if err := uiOnly(callCtx); err != nil {
			return nil, err
		}
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		p, err := s.container.Preferences.AgentHarnessPolicy()
		if err != nil {
			return nil, err
		}
		return AgentHarnessPolicyState{Revision: p.Revision, AutoNarrowing: p.AutoNarrowing}, nil
	})
	if err != nil {
		return AgentHarnessPolicyState{}, err
	}
	return result.(AgentHarnessPolicyState), nil
}

// AgentHarnessAutoNarrowingSet turns the opt-in on or off when expectRevision is
// the stored revision; a stale revision is a conflict, not a write. Window only.
func (s *AppService) AgentHarnessAutoNarrowingSet(ctx context.Context, expectRevision int64, on bool) (AgentHarnessPolicyState, error) {
	result, err := s.call(ctx, "builtin.settings", "preferences:write", "agent_harness_policy", map[string]any{
		"action": "set_agent_harness_policy", "auto_narrowing": on,
	}, func(callCtx context.Context) (any, error) {
		if err := uiOnly(callCtx); err != nil {
			return nil, err
		}
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		p, err := s.container.Preferences.SetAgentHarnessAutoNarrowing(expectRevision, on)
		if err != nil {
			return nil, err
		}
		return AgentHarnessPolicyState{Revision: p.Revision, AutoNarrowing: p.AutoNarrowing}, nil
	})
	if err != nil {
		return AgentHarnessPolicyState{}, err
	}
	return result.(AgentHarnessPolicyState), nil
}
