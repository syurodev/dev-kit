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
		{agenthub.ErrSandboxUnavailable, "connection_failed: Agent sandbox is unavailable."},
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
	notInstalledSession := fmt.Errorf("%w: %w", agenthub.ErrAgentNotInstalled, agentruntime.ErrNotInstalled)
	missingNode := fmt.Errorf("%w: %w", agenthub.ErrProcessFailed,
		&agentruntime.Error{Kind: agentruntime.ErrRequirementNotMet, Reason: "Node.js 20.1.0 is older than 22"})
	cases := []struct {
		in   error
		want string
	}{
		{notInstalledSession, "agent_not_installed: Claude Agent is not installed. Install it in Settings → Agents."},
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
		agenthub.ErrNotDescendant:    codePermissionDenied,
		agenthub.ErrCwdOutsideParent: codeValidationFailed,
		agenthub.ErrInvalidCwd:       codeValidationFailed,
		agenthub.ErrCallerMismatch:   codeValidationFailed,
		agenthub.ErrProxyNotFound:    codeConnectionFailed,
	}
	for in, want := range cases {
		got := publicError("builtin.agenthub", in)
		var appErr *apperrors.AppError
		if !errors.As(got, &appErr) || appErr.Code != want {
			t.Fatalf("%v → %v, want code %s", in, got, want)
		}
	}
}
