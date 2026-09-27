package bridge

import (
	"errors"
	"fmt"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/agentruntime"
	"devkit/internal/app"
	"devkit/internal/apperrors"
)

func TestPublicErrorMapsAgentHubUnavailable(t *testing.T) {
	err := publicError("builtin.agenthub", errAgentHubUnavailable)
	if err == nil || err.Error() != "internal_error: Agent Hub is not available." {
		t.Fatalf("public error = %v", err)
	}
}

func TestPublicErrorMapsAgentHubSentinels(t *testing.T) {
	cases := []struct {
		in   error
		want string
	}{
		{agenthub.ErrSessionNotFound, "validation_failed: Agent session not found."},
		{agenthub.ErrSessionBusy, "validation_failed: Agent session is busy."},
		{agenthub.ErrDepthLimitExceeded, "validation_failed: Agent session depth limit exceeded."},
		{agenthub.ErrCycleDetected, "validation_failed: Agent session cycle detected."},
		{agenthub.ErrVendorUnknown, "validation_failed: Unknown agent vendor."},
		{agenthub.ErrPermissionDenied, "permission_denied: Permission denied."},
		{agenthub.ErrCancelled, "operation_cancelled: Operation cancelled."},
		{agenthub.ErrProcessFailed, "connection_failed: Agent process failed."},
		{agenthub.ErrAgentAuthRequired, "agent_auth_required: The agent isn't signed in, or the sign-in expired."},
		{&agenthub.VendorError{Agent: agenthub.AgentTypeClaude, Err: fmt.Errorf("%w: session/new: acp: rpc error -32000: Authentication required", agenthub.ErrAgentAuthRequired)},
			"agent_auth_required: Claude isn't signed in, or the sign-in expired. Sign in to Claude, or set ANTHROPIC_API_KEY."},
		{&agenthub.VendorError{Agent: agenthub.AgentTypeCodex, Err: fmt.Errorf("%w: %w: Codex refused the API key", agenthub.ErrAgentAuthRequired, agenthub.ErrAgentAPIKeyUnusable)},
			"agent_api_key_unusable: Codex can't use the API key chosen in Settings → Agents → Codex: it is gone from Accounts or was refused. Choose another, or go back to the ChatGPT sign-in."},
		{fmt.Errorf("invoke: %w", &agenthub.VendorError{Agent: agenthub.AgentTypeCodex, Err: fmt.Errorf("%w: Codex is not signed in", agenthub.ErrAgentAuthRequired)}),
			"agent_auth_required: Codex isn't signed in, or the sign-in expired. Sign in under Settings → Agents → Codex, or check the API key chosen there."},
		// A usage limit says so, in the vendor's own words — the only ones that
		// say when it lifts — rather than "connection_failed: Agent process failed."
		{fmt.Errorf("invoke: %w", &agenthub.VendorError{Agent: agenthub.AgentTypeCodex,
			Err:     fmt.Errorf("%w: You've hit your usage limit.", agenthub.ErrAgentUsageLimit),
			Message: "You've hit your usage limit. Upgrade to Pro or try again at 3:10 PM."}),
			"agent_usage_limit: Codex usage limit reached. You've hit your usage limit. Upgrade to Pro or try again at 3:10 PM."},
		{agenthub.ErrAgentUsageLimit, "agent_usage_limit: Usage limit reached."},
		{&agenthub.VendorError{Agent: agenthub.AgentTypeCodex,
			Err:     fmt.Errorf("%w: the conversation no longer fits: too long", agenthub.ErrTurnFailed),
			Message: "too long"},
			"agent_turn_failed: Codex couldn't finish the turn. too long"},
		{agenthub.ErrAuthMethodUnknown, "validation_failed: That sign-in method isn't offered by this agent."},
		{agenthub.ErrSandboxUnavailable, "connection_failed: Agent sandbox is unavailable."},
		{agenthub.ErrUnavailableInServerMode, "validation_failed: Agent Hub needs the desktop app. In server mode an agent could answer its own permission prompts."},
		{agenthub.ErrUnavailable, "internal_error: Agent Hub is not available."},
	}
	for _, tc := range cases {
		got := publicError("builtin.agenthub", tc.in)
		if got == nil || got.Error() != tc.want {
			t.Fatalf("%v → %v, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPublicErrorMapsAgentRuntimeErrors(t *testing.T) {
	notInstalledSession := &agenthub.VendorError{Agent: agenthub.AgentTypeClaude, Err: fmt.Errorf("%w: %w", agenthub.ErrAgentNotInstalled, agentruntime.ErrNotInstalled)}
	codexNotInstalled := &agenthub.VendorError{Agent: agenthub.AgentTypeCodex, Err: agenthub.ErrAgentNotInstalled}
	missingNode := fmt.Errorf("%w: %w", agenthub.ErrProcessFailed,
		&agentruntime.Error{Kind: agentruntime.ErrRequirementNotMet, Reason: "Node.js 20.1.0 is older than 22"})
	cases := []struct {
		in   error
		want string
	}{
		{notInstalledSession, "agent_not_installed: Claude Agent is not installed. Install it in Settings → Agents."},
		{codexNotInstalled, "agent_not_installed: Codex is not installed. Install it in Settings → Agents."},
		{agenthub.ErrAgentNotInstalled, "agent_not_installed: The agent is not installed. Install it in Settings → Agents."},
		{missingNode, "runtime_requirement: A requirement for the agent is not met: Node.js 20.1.0 is older than 22"},
		{&agentruntime.Error{Kind: agentruntime.ErrInstallFailed, Reason: "npm install failed: 404 Not Found"},
			"install_failed: Installing the agent failed: npm install failed: 404 Not Found"},
		{agentruntime.ErrInstallAlreadyRunning, "validation_failed: An install is already running for this agent."},
		{agentruntime.ErrVersionInUse, "validation_failed: That version is in use. Switch to another version before removing it."},
		{agentruntime.ErrNotInstalled, "validation_failed: That agent version is not installed."},
		{agentruntime.ErrUnknownAgent, "validation_failed: Unknown agent."},
		{errAgentRuntimesUnavailable, "internal_error: Agent runtimes are not available."},
	}
	for _, tc := range cases {
		got := publicError("builtin.agenthub", tc.in)
		if got == nil || got.Error() != tc.want {
			t.Fatalf("%v → %v, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAgentRuntimeStatusThroughTheGateway(t *testing.T) {
	container := app.NewContainer()
	container.AgentRuntimes = agentruntime.New(t.TempDir())
	svc := NewAppService(container, nil)

	st, err := svc.AgentRuntimeStatus(agentruntime.ClaudeACP)
	if err != nil {
		t.Fatal(err)
	}
	if st.Agent != agentruntime.ClaudeACP || st.TestedVersion == "" || st.Current != nil {
		t.Fatalf("status = %+v", st)
	}
	if _, err := svc.AgentRuntimeActivate("../etc", "0.74.0"); err == nil ||
		err.Error() != "validation_failed: Unknown agent." {
		t.Fatalf("activate unknown agent: %v", err)
	}
}

func TestAgentHubIdentityErrorsMap(t *testing.T) {
	cases := map[error]string{
		agenthub.ErrNotDescendant:       codePermissionDenied,
		agenthub.ErrCwdOutsideParent:    codeValidationFailed,
		agenthub.ErrInvalidCwd:          codeValidationFailed,
		agenthub.ErrInvalidConfigOption: codeValidationFailed,
		agenthub.ErrInvalidPrompt:       codeValidationFailed,
		agenthub.ErrCallerMismatch:      codeValidationFailed,
		agenthub.ErrProxyNotFound:       codeConnectionFailed,
	}
	for in, want := range cases {
		got := publicError("builtin.agenthub", in)
		var appErr *apperrors.AppError
		if !errors.As(got, &appErr) || appErr.Code != want {
			t.Fatalf("%v → %v, want code %s", in, got, want)
		}
	}
}
