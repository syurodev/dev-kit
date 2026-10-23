package bridge

import (
	"errors"
	"fmt"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/agentruntime"
	"devkit/internal/app"
	"devkit/internal/apperrors"
	"devkit/internal/preferences"
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
		// The model picker's own failure: what could not be read, not a crash.
		{&agenthub.VendorError{Agent: agenthub.AgentTypeCodex, Err: fmt.Errorf("%w: %w", agenthub.ErrModelsUnavailable, agenthub.ErrProcessFailed)},
			"connection_failed: Codex didn't report which models it offers. Try Refresh models, or check that it is signed in."},
		{agenthub.ErrAuthMethodUnknown, "validation_failed: That sign-in method isn't offered by this agent."},
		{agenthub.ErrSandboxUnavailable, "connection_failed: Agent sandbox is unavailable."},
		{agenthub.ErrUnavailableInServerMode, "validation_failed: Agent Hub needs the desktop app. In server mode an agent could answer its own permission prompts."},
		{agenthub.ErrUnavailable, "internal_error: Agent Hub is not available."},
		// Vendor accounts (Settings → Agents → Accounts & limits). A home error wraps a
		// path and the OS's words: neither reaches the window.
		{fmt.Errorf("%w: every account of this vendor has reached its limit; the first one is available again at 15:04 on 2 Jan", agenthub.ErrAllAccountsLimited),
			"agent_usage_limit: Every account of this vendor has reached its limit; the first one is available again at 15:04 on 2 Jan"},
		{fmt.Errorf("invoke: %w", agenthub.ErrAccountNotFound), "validation_failed: That account no longer exists."},
		{fmt.Errorf("%w: a name is at most 40 characters", agenthub.ErrInvalidAccount), "validation_failed: Account: a name is at most 40 characters"},
		{fmt.Errorf("add: %w: a name is at most 40 characters", agenthub.ErrInvalidAccount), "validation_failed: Account: a name is at most 40 characters"},
		{fmt.Errorf("%w: create /home/someone/.local/share/devkit/agent-accounts/claude/x: %w", agenthub.ErrAccountHome, errors.New("permission denied")),
			"validation_failed: DevKit could not prepare a folder for that account. Nothing was changed."},
		{fmt.Errorf("%w: %w", agenthub.ErrInvalidAccount, fmt.Errorf("%w: /secret/path", agenthub.ErrAccountHome)),
			"validation_failed: DevKit could not prepare a folder for that account. Nothing was changed."},
		{agenthub.ErrAccountLabelTaken, "validation_failed: You already have an account with that name."},
		{agenthub.ErrAccountInUse, "validation_failed: A session is using that account. Close it first."},
		{fmt.Errorf("%w: the CLI said something with a path /x/y", agenthub.ErrUsageNotCheckable), "validation_failed: This account's usage cannot be read right now."},
		{fmt.Errorf("%w: the folder for added accounts is not set", agenthub.ErrAccountsUnavailable), "validation_failed: Accounts are not available."},
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

func TestInvalidPromptNamesTheRefusedAttachment(t *testing.T) {
	err := fmt.Errorf("%w: %s is not text or an image", agenthub.ErrInvalidPrompt, "report.docx")
	if got := invalidPromptMessage(err); got != "The prompt was not sent: report.docx is not text or an image." {
		t.Fatalf("message = %q", got)
	}
	// No detail to name: the generic sentence, as before.
	if got := invalidPromptMessage(agenthub.ErrInvalidPrompt); got != invalidPromptGeneric {
		t.Fatalf("message = %q", got)
	}
}

// The account switching rules are saved from Settings; their refusals say what to do
// and carry nothing but the validator's own sentence.
func TestPublicErrorMapsAccountPolicyRefusals(t *testing.T) {
	cases := []struct {
		in   error
		want string
	}{
		{preferences.ErrAccountPolicyConflict, "validation_failed: The account switching rules changed. Refresh and apply your change again."},
		{fmt.Errorf("%w: claude: the threshold must be between 50 and 100 percent", preferences.ErrInvalidAccountPolicy),
			"validation_failed: Account switching rules: claude: the threshold must be between 50 and 100 percent"},
		{fmt.Errorf("save: %w: claude: accept the terms warning before turning switching on", preferences.ErrInvalidAccountPolicy),
			"validation_failed: Account switching rules: claude: accept the terms warning before turning switching on"},
	}
	for _, tc := range cases {
		got := publicError("builtin.settings", tc.in)
		if got == nil || got.Error() != tc.want {
			t.Fatalf("%v → %v, want %q", tc.in, got, tc.want)
		}
	}
}
