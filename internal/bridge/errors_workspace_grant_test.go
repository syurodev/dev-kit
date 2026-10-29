package bridge

import (
	"errors"
	"testing"

	"devkit/internal/apperrors"
)

// workspace.requestWrite names its codes through constants, which the source scan
// in TestEveryMCPErrorCodeIsPublishedOrRedactedOnPurpose does not match, so they
// are checked here: an agent that is refused, declined or cannot be answered has
// to read why, not "An unexpected error occurred."
func TestWorkspaceGrantCodesReachTheAgent(t *testing.T) {
	for _, code := range []string{"workspace.grantRefused", "workspace.grantDeclined", "workspace.grantUnavailable"} {
		if !keptCodes[code] {
			t.Errorf("%s is not in keptCodes", code)
		}
		public := publicErrorForCode("builtin.workspace", errors.New(code), &apperrors.AppError{Code: code, Message: "the reason", Recoverable: false})
		if public.Message != "the reason" {
			t.Errorf("%s reaches an agent as %q, want its own message", code, public.Message)
		}
	}
}
