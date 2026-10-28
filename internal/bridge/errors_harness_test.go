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
		{"files restored, conversation not cut", &agenthub.RewindPartialError{Cause: agenthub.ErrHarnessLaunchStale, SafetyCheckpointID: "c1"},
			"validation_failed: The files were put back, but the conversation was not rewound. Rewind the same message again to finish, or undo the file restore from its safety checkpoint."},
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

// The cause of a partial rewind (a path, a store error) never reaches the sentence.
func TestAPartialRewindNeverCarriesItsCause(t *testing.T) {
	err := &agenthub.RewindPartialError{Cause: fmt.Errorf("/home/me/secret-path: database is locked"), SafetyCheckpointID: "c1"}
	got := publicError("builtin.agenthub", err)
	if got == nil || strings.Contains(got.Error(), "secret-path") || strings.Contains(got.Error(), "locked") || !strings.Contains(got.Error(), "files were put back") {
		t.Fatalf("public error = %v", got)
	}
}
