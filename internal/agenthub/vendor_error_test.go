package agenthub

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"devkit/internal/agentruntime"
	"devkit/internal/codexapp"
)

// The two errors a person fixes in Settings — not installed, not signed in —
// say which agent they are about, and stay the same errors underneath.
func TestVendorErrorsNameTheirAgent(t *testing.T) {
	notInstalled := func() (agentruntime.Resolved, error) {
		return agentruntime.Resolved{}, agentruntime.ErrNotInstalled
	}
	_, _, claudeNone := NewClaudeAdapter(ClaudeOptions{}).buildSandbox(OpenSpec{Cwd: t.TempDir()})
	_, _, claudeRuntime := NewClaudeAdapter(ClaudeOptions{Runtime: notInstalled}).buildSandbox(OpenSpec{Cwd: t.TempDir()})
	_, codexNone := NewCodexAdapter(CodexOptions{}).resolveCommand()
	_, codexRuntime := NewCodexAdapter(CodexOptions{Runtime: notInstalled}).resolveCommand()
	_, codexKey := NewCodexAdapter(CodexOptions{APIKey: func(context.Context) (string, error) {
		return "", errors.New("vault locked")
	}}).apiKey(context.Background())
	claudeSignedOut := NewClaudeAdapter(ClaudeOptions{Command: fakeAgentBinary(t), Runner: scriptedAgentRunner{replies: []string{
		`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1}}`,
		`{"jsonrpc":"2.0","id":2,"error":{"code":-32000,"message":"Authentication required"}}`,
	}}})
	_, claudeAuth := claudeSignedOut.Open(context.Background(), OpenSpec{Cwd: t.TempDir()})
	f := newCodexFixture(t, "signedout")
	_, codexAuth := f.adapter.Open(context.Background(), OpenSpec{Cwd: f.cwd})
	codexTurn := codexTurnError(&codexapp.TurnError{Message: "token expired", CodexErrorInfo: []byte(`"unauthorized"`)})

	cases := []struct {
		name     string
		err      error
		sentinel error
		agent    AgentType
	}{
		{"claude, nothing to run", claudeNone, ErrAgentNotInstalled, AgentTypeClaude},
		{"claude, runtime not installed", claudeRuntime, ErrAgentNotInstalled, AgentTypeClaude},
		{"claude, signed out", claudeAuth, ErrAgentAuthRequired, AgentTypeClaude},
		{"codex, nothing to run", codexNone, ErrAgentNotInstalled, AgentTypeCodex},
		{"codex, runtime not installed", codexRuntime, ErrAgentNotInstalled, AgentTypeCodex},
		{"codex, api key unreadable", codexKey, ErrAgentAuthRequired, AgentTypeCodex},
		{"codex, signed out", codexAuth, ErrAgentAuthRequired, AgentTypeCodex},
		{"codex, unauthorized turn", codexTurn, ErrAgentAuthRequired, AgentTypeCodex},
	}
	for _, tc := range cases {
		if !errors.Is(tc.err, tc.sentinel) {
			t.Errorf("%s: %v is not %v", tc.name, tc.err, tc.sentinel)
		}
		if got := ErrorVendor(tc.err); got != tc.agent {
			t.Errorf("%s: ErrorVendor = %q, want %q", tc.name, got, tc.agent)
		}
		// Wrapped again on the way up, it still says whose it is.
		if got := ErrorVendor(fmt.Errorf("invoke: %w", tc.err)); got != tc.agent {
			t.Errorf("%s: wrapped ErrorVendor = %q, want %q", tc.name, got, tc.agent)
		}
	}
	if got := ErrorVendor(ErrAgentAuthRequired); got != "" {
		t.Errorf("a bare sentinel names no agent, got %q", got)
	}
	if vendorError(AgentTypeCodex, nil) != nil {
		t.Error("vendorError(nil) must stay nil")
	}
	// The message is the error's own: naming the vendor adds nothing to it.
	plain := fmt.Errorf("%w: Codex is not signed in", ErrAgentAuthRequired)
	if got := vendorError(AgentTypeCodex, plain).Error(); got != plain.Error() {
		t.Errorf("Error() = %q, want %q", got, plain.Error())
	}
}
