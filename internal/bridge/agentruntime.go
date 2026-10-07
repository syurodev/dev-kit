package bridge

import (
	"context"
	"errors"

	"devkit/internal/agentruntime"
)

var errAgentRuntimesUnavailable = errors.New("agent runtimes are not available")

// agentRuntimeID keeps an agent id out of a capability scope and the audit log
// unless it looks like one. Registry ids are short lowercase slugs.
func agentRuntimeID(value string) string {
	if value == "" || len(value) > 64 {
		return invalidOpaqueID
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
			return invalidOpaqueID
		}
	}
	return value
}

// agentRuntimeCall runs one runtime operation through the capability gateway
// and answers with the agent's status afterwards, so the settings page
// re-renders from one reply instead of a second round trip.
func (s *AppService) agentRuntimeCall(
	ctx context.Context,
	capability, action, agent string,
	do func(context.Context, *agentruntime.Manager) error,
) (agentruntime.Status, error) {
	id := agentRuntimeID(agent)
	result, err := s.call(ctx, "builtin.agenthub", capability, "runtime:"+id, map[string]any{
		"action": action,
		"agent":  id,
	}, func(callCtx context.Context) (any, error) {
		manager := s.container.AgentRuntimes
		if manager == nil {
			return nil, errAgentRuntimesUnavailable
		}
		if do != nil {
			if err := do(callCtx, manager); err != nil {
				return nil, err
			}
		}
		return manager.Status(callCtx, agent)
	})
	if err != nil {
		return agentruntime.Status{}, err
	}
	return result.(agentruntime.Status), nil
}

// AgentRuntimeStatus reports an agent's installs, requirements and Claude
// binary choice. No network: the latest version comes from the cached registry.
func (s *AppService) AgentRuntimeStatus(agent string) (agentruntime.Status, error) {
	return s.agentRuntimeCall(context.Background(), "agenthub:read", "runtime_status", agent, nil)
}

// AgentRuntimeCheckLatest refreshes the vendor's update cache. Offline is not an
// error; the status keeps the cached latest, or none.
func (s *AppService) AgentRuntimeCheckLatest(ctx context.Context, agent string) (agentruntime.Status, error) {
	return s.agentRuntimeCall(ctx, "agenthub:read", "runtime_check_latest", agent,
		func(callCtx context.Context, m *agentruntime.Manager) error {
			return m.CheckLatest(callCtx, agent)
		})
}

// AgentRuntimeInstall installs a version (DevKit's tested one when version is
// empty) and activates it. npm's output arrives as agenthub:runtimeInstallOutput
// events while this runs; the promise settles when the install is done, and
// cancelling it kills npm and discards the staged tree.
func (s *AppService) AgentRuntimeInstall(ctx context.Context, agent, version string) (agentruntime.Status, error) {
	return s.agentRuntimeCall(ctx, "agenthub:execute", "runtime_install", agent,
		func(callCtx context.Context, m *agentruntime.Manager) error {
			return m.Install(callCtx, agent, version)
		})
}

// AgentRuntimeActivate switches to an installed version — a rollback when it
// is the previous one. Sessions already running keep their process.
func (s *AppService) AgentRuntimeActivate(agent, version string) (agentruntime.Status, error) {
	return s.agentRuntimeCall(context.Background(), "agenthub:execute", "runtime_activate", agent,
		func(_ context.Context, m *agentruntime.Manager) error {
			return m.Activate(agent, version)
		})
}

// AgentRuntimeRemove deletes an installed version other than the current one.
func (s *AppService) AgentRuntimeRemove(agent, version string) (agentruntime.Status, error) {
	return s.agentRuntimeCall(context.Background(), "agenthub:execute", "runtime_remove", agent,
		func(_ context.Context, m *agentruntime.Manager) error {
			return m.Remove(agent, version)
		})
}

// AgentRuntimeIsDowngrade reports whether activating version would move the
// agent to an older release than what is current now, so the UI can confirm
// before a rollback: Activate itself is a pointer swap with no compatibility
// check of its own (agentruntime.Manager.IsDowngrade).
func (s *AppService) AgentRuntimeIsDowngrade(agent, version string) (bool, error) {
	id := agentRuntimeID(agent)
	result, err := s.call(context.Background(), "builtin.agenthub", "agenthub:read", "runtime:"+id, map[string]any{
		"action":  "runtime_is_downgrade",
		"agent":   id,
		"version": version,
	}, func(callCtx context.Context) (any, error) {
		manager := s.container.AgentRuntimes
		if manager == nil {
			return nil, errAgentRuntimesUnavailable
		}
		return manager.IsDowngrade(agent, version)
	})
	if err != nil {
		return false, err
	}
	return result.(bool), nil
}

// AgentRuntimeSetClaudeBinary chooses the Claude Code the Claude agent runs:
// "bundled" with the adapter (default) or "machine", the user's own install.
func (s *AppService) AgentRuntimeSetClaudeBinary(source string) (agentruntime.Status, error) {
	return s.agentRuntimeCall(context.Background(), "agenthub:execute", "runtime_set_claude_binary", agentruntime.ClaudeACP,
		func(callCtx context.Context, m *agentruntime.Manager) error {
			return m.SetClaudeBinary(callCtx, source)
		})
}
