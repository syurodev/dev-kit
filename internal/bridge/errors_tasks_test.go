package bridge

import (
	"testing"

	"devkit/internal/tasks"
)

// A board with a verify command but no workspace binding this machine can
// resolve must reach the UI as something a person can act on, not as the
// generic "unexpected error" every unclassified cause collapses into.
func TestPublicErrorMapsBoardUnbound(t *testing.T) {
	err := publicError("builtin.tasks", tasks.ErrBoardUnbound)
	if err.Code != codeValidationFailed {
		t.Fatalf("code = %q, want %q", err.Code, codeValidationFailed)
	}
	if err.Message == "" || err.Message == "An unexpected error occurred." {
		t.Fatalf("message = %q, want a readable explanation", err.Message)
	}
}
