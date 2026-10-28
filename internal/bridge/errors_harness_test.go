package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/apperrors"
	"devkit/internal/harness"
	"devkit/internal/vault"
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
		{"stale session override", harness.ErrStaleSessionOverride, "harness.snapshotUnavailable: " + harness.ErrStaleSessionOverride.Message},
		{"launch went stale", agenthub.ErrHarnessLaunchStale, "harness.snapshotUnavailable: " + agenthub.ErrHarnessLaunchStale.Message},
		{"not enforceable", agenthub.ErrHarnessNotEnforceable, "harness.notEnforceable: " + agenthub.ErrHarnessNotEnforceable.Message},
		{"new session required", agenthub.ErrHarnessNewSessionRequired, "harness.newSessionRequired: " + agenthub.ErrHarnessNewSessionRequired.Message},
		{"wrapped", fmt.Errorf("create: %w", agenthub.ErrHarnessUnavailable), "harness.snapshotUnavailable: " + agenthub.ErrHarnessUnavailable.Message},
		{"files restored, conversation not cut", &agenthub.RewindPartialError{Cause: agenthub.ErrHarnessLaunchStale, SafetyCheckpointID: "c1"},
			"agenthub.rewindFilesRestored: The files were put back, but the conversation was not rewound. Rewinding the same message again can finish it. The files' earlier state is kept as a safety checkpoint."},
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

// What the window receives for a rewind that put the files back is decided by
// the public funnel, not by errors.Is in agenthub: a cause that is a cancelled
// request, a vault lock or a harness refusal must not turn "the files changed"
// into "Operation cancelled." or a sentence that says nothing changed.
func TestAPartialRewindIsNeverReportedAsTheCauseBehindIt(t *testing.T) {
	causes := map[string]error{
		"cancelled caller":  context.Canceled,
		"deadline":          context.DeadlineExceeded,
		"wrapped cancel":    fmt.Errorf("fork: %w", context.Canceled),
		"vault locked":      vault.ErrLocked,
		"closed session":    agenthub.ErrSessionClosed,
		"permission denied": agenthub.ErrPermissionDenied,
		"launch gate":       agenthub.ErrHarnessUnavailable,
		"stale launch":      agenthub.ErrHarnessLaunchStale,
		"agent missing":     agenthub.ErrAgentNotInstalled,
		"anything else":     errors.New("/home/me/secret-path: database is locked"),
	}
	for name, cause := range causes {
		t.Run(name, func(t *testing.T) {
			got := publicError("builtin.agenthub", &agenthub.RewindPartialError{Cause: cause, SafetyCheckpointID: "c1"})
			if got == nil || got.Code != codeRewindFilesRestored {
				t.Fatalf("public error = %v, want the %s refusal", got, codeRewindFilesRestored)
			}
			if !strings.Contains(got.Message, "files were put back") || !strings.Contains(got.Message, "was not rewound") {
				t.Fatalf("the sentence must say the files changed and the conversation did not: %q", got.Message)
			}
			if strings.Contains(got.Error(), "secret-path") || strings.Contains(got.Error(), "locked") {
				t.Fatalf("a cause leaked: %v", got)
			}
		})
	}
}

// The same outcomes through the real gateway call the window's bound method uses, and
// serialized as Wails hands them to the frontend: the three public fields only, with the
// published code, a fixed sentence and no cause.
func TestAnUnfinishedRewindReachesTheWindowAsItsOwnPublishedCode(t *testing.T) {
	svc := newBridgeWithAgentHub(t)
	secret := errors.New("/home/me/.config/DevKit/secret-path: database is locked")
	cases := map[string]struct {
		err  error
		code string
		keep string // a phrase the sentence must carry
	}{
		"partial, cause cancelled": {&agenthub.RewindPartialError{Cause: context.Canceled, SafetyCheckpointID: "c1"}, codeRewindFilesRestored, "was not rewound"},
		"partial, cause a store":   {&agenthub.RewindPartialError{Cause: secret, SafetyCheckpointID: "c1"}, codeRewindFilesRestored, "was not rewound"},
		"unproven":                 {&agenthub.RewindUnknownError{Cause: secret, SafetyCheckpointID: "c1"}, codeRewindOutcomeUnknown, "could not confirm"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := svc.call(context.Background(), "builtin.agenthub", "agenthub:execute", "restore:token",
				map[string]any{"action": "apply_rewind"},
				func(context.Context) (any, error) { return nil, c.err })
			var public *apperrors.AppError
			if !errors.As(err, &public) {
				t.Fatalf("the window receives an AppError, got %T %v", err, err)
			}
			wire, merr := json.Marshal(public)
			if merr != nil {
				t.Fatal(merr)
			}
			var got map[string]any
			if err := json.Unmarshal(wire, &got); err != nil {
				t.Fatal(err)
			}
			if got["code"] != c.code || got["recoverable"] != true || len(got) != 3 {
				t.Fatalf("wire payload = %s, want code %s, recoverable and nothing else", wire, c.code)
			}
			if message, _ := got["message"].(string); !strings.Contains(message, c.keep) || strings.Contains(message, "secret-path") || strings.Contains(message, "locked") {
				t.Fatalf("wire message = %q", message)
			}
		})
	}
}

// A conversation-only rewind restored no file: its unproven commit says only what is unknown.
func TestAnUnprovenConversationOnlyRewindClaimsNoFileChange(t *testing.T) {
	got := publicError("builtin.agenthub", fmt.Errorf("%w: /home/me/secret-path", agenthub.ErrRewindCommitUnknown))
	if got == nil || got.Code != codeRewindOutcomeUnknown || strings.Contains(got.Message, "put back") || strings.Contains(got.Error(), "secret-path") {
		t.Fatalf("public error = %v", got)
	}
}

// A rewind whose outcome could not be proven says so, and is not the partial
// sentence: "the conversation was not rewound" would be a claim nobody checked.
func TestAnUnprovenRewindOutcomeIsNotReportedAsPartialOrSuccess(t *testing.T) {
	err := &agenthub.RewindUnknownError{Cause: fmt.Errorf("%w: /home/me/secret-path", context.Canceled), SafetyCheckpointID: "c1"}
	got := publicError("builtin.agenthub", err)
	if got == nil || got.Code != codeRewindOutcomeUnknown {
		t.Fatalf("public error = %v, want the %s refusal", got, codeRewindOutcomeUnknown)
	}
	if strings.Contains(got.Message, "was not rewound") || strings.Contains(got.Error(), "secret-path") {
		t.Fatalf("an unproven outcome must not claim either result or leak its cause: %v", got)
	}
	if errors.Is(err, agenthub.ErrRewindFilesRestored) {
		t.Fatal("an unproven outcome is not the proven partial one")
	}
	// The published codes survive a second pass through the funnel.
	if again := publicError("builtin.agenthub", got); again == nil || again.Code != got.Code || again.Message != got.Message {
		t.Fatalf("a mapped rewind code was redacted on a second pass: %v", again)
	}
}
