package bridge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"devkit/internal/harness"
	"devkit/internal/harnessconfig"
)

// The public tool-group controls answer with a small, fixed set of refusals. Each
// is published with its code and host sentence; none carries the cause behind it.
func TestEveryRefusalOfThePublicControlsIsAPublishedFixedSentence(t *testing.T) {
	refusals := []*harness.Error{
		harnessconfig.ErrDenied, harnessconfig.ErrScopeGone, harnessconfig.ErrFactsUnavailable, harnessconfig.ErrExecutableDenied,
		harnessconfig.ErrExecutableNotReady, harnessconfig.ErrNoChange, harnessconfig.ErrInvalid, harnessconfig.ErrStale,
		harnessconfig.ErrConflict, harnessconfig.ErrUnknownProposal, harnessconfig.ErrBusy, harnessconfig.ErrTooLargeToShow,
		harnessconfig.ErrCancelled, harnessconfig.ErrTooLateToCancel, harnessconfig.ErrNotConfirmed, harnessconfig.ErrConfirmUnavailable,
		harnessconfig.ErrLiveUnavailable, harnessconfig.ErrPublicationPending, harnessconfig.ErrStoredUnreadable,
		harnessconfig.ErrRepairUnavailable, harnessconfig.ErrRepairTooLarge,
	}
	for _, he := range refusals {
		t.Run(he.Code+"/"+he.Message[:12], func(t *testing.T) {
			if !keptCodes[he.Code] {
				t.Fatalf("code %q is not in keptCodes: it would be redacted at the gateway", he.Code)
			}
			want := he.Code + ": " + he.Message
			if got := publicError("builtin.harness", he); got == nil || got.Error() != want {
				t.Fatalf("public error = %v, want %q", got, want)
			}
			secret := "/home/me/.config/DevKit/devkit.db: SQLITE_BUSY token=abc"
			wrapped := fmt.Errorf("%s: %w", secret, he)
			if got := publicError("builtin.harness", wrapped); got == nil || strings.Contains(got.Error(), "SQLITE") || strings.Contains(got.Error(), "token") || got.Error() != want {
				t.Fatalf("a wrapped cause must not reach the sentence: %v", got)
			}
		})
	}
}

func TestOtherFailuresOfThePublicControlsStayRedactedOrCancelled(t *testing.T) {
	if got := publicError("builtin.harness", errors.New("sql: connection refused to postgres://u:p@host")); got == nil || strings.Contains(got.Error(), "postgres") {
		t.Fatalf("an unclassified cause must be redacted: %v", got)
	}
	if got := publicError("builtin.harness", fmt.Errorf("x: %w", context.Canceled)); got == nil || !strings.Contains(got.Error(), "cancelled") {
		t.Fatalf("a cancelled context is a cancellation: %v", got)
	}
	// A group the registry does not have is a published code of its own.
	unknown := &harness.Error{Code: harness.CodeGroupUnknown, Message: "One of the requested tool groups does not exist."}
	if got := publicError("builtin.harness", unknown); got == nil || got.Error() != harness.CodeGroupUnknown+": "+unknown.Message {
		t.Fatalf("group-unknown not published: %v", got)
	}
}
