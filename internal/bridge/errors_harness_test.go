package bridge

import (
	"fmt"
	"strings"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
)

// A launch, reload or close can be refused by the harness. Those refusals reach a
// window through the agent-hub error funnel: each must say what happened in the
// fixed host sentence and never carry the cause behind it.
func TestHarnessLaunchRefusalsAreSurvivingPublicErrors(t *testing.T) {
	cases := []struct {
		name string
		in   error
		want string
	}{
		{"unresolved configuration", agenthub.ErrHarnessUnavailable, "harness.snapshotUnavailable: " + agenthub.ErrHarnessUnavailable.Message},
		{"launch went stale", agenthub.ErrHarnessLaunchStale, "harness.snapshotUnavailable: " + agenthub.ErrHarnessLaunchStale.Message},
		{"not enforceable", agenthub.ErrHarnessNotEnforceable, "harness.notEnforceable: " + agenthub.ErrHarnessNotEnforceable.Message},
		{"new session required", agenthub.ErrHarnessNewSessionRequired, "harness.newSessionRequired: " + agenthub.ErrHarnessNewSessionRequired.Message},
		{"wrapped", fmt.Errorf("create: %w", agenthub.ErrHarnessUnavailable), "harness.snapshotUnavailable: " + agenthub.ErrHarnessUnavailable.Message},
		{"cleanup failed", agenthub.ErrHarnessCleanupFailed, "validation_failed: The session was closed, but its tool override could not be removed. Close it again to retry."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := publicError("builtin.agenthub", c.in)
			if got == nil || got.Error() != c.want {
				t.Fatalf("public error = %v, want %q", got, c.want)
			}
		})
	}
}

// Anything else the harness might raise stays redacted, and a cause wrapped
// around a harness error never reaches the sentence.
func TestAHarnessRefusalNeverCarriesItsCause(t *testing.T) {
	secret := "/home/me/.config/DevKit/secret-path token=abc"
	wrapped := fmt.Errorf("%s: %w", secret, agenthub.ErrHarnessUnavailable)
	if got := publicError("builtin.agenthub", wrapped); strings.Contains(got.Error(), "secret-path") || strings.Contains(got.Error(), "token") {
		t.Fatalf("a cause leaked: %v", got)
	}
	// A harness code that is not a launch refusal is not published by this path.
	other := &harness.Error{Code: harness.CodeProfileConflict, Message: "The workspace profile changed. Propose it again."}
	if got := publicError("builtin.agenthub", other); strings.Contains(got.Error(), harness.CodeProfileConflict) {
		t.Fatalf("a proposal-only code leaked through the launch funnel: %v", got)
	}
}
