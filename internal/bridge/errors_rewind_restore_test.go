package bridge

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"devkit/internal/agenthub"
)

// A restore that stopped part-way keeps the partial code, but its sentence must not say
// the files were put back, and its cause (a path, an OS error) never reaches it.
func TestAPartialRestoreHasItsOwnSentenceUnderTheSameCode(t *testing.T) {
	secret := fmt.Errorf("remove /home/me/secret-path/a.txt: permission denied")
	incomplete := publicError("builtin.agenthub", &agenthub.RewindPartialError{Cause: secret, SafetyCheckpointID: "c1", RestoreIncomplete: true})
	whole := publicError("builtin.agenthub", &agenthub.RewindPartialError{Cause: secret, SafetyCheckpointID: "c1"})
	if incomplete == nil || whole == nil || incomplete.Code != codeRewindFilesRestored || whole.Code != codeRewindFilesRestored {
		t.Fatalf("both are the published partial code: %v / %v", incomplete, whole)
	}
	if incomplete.Message == whole.Message {
		t.Fatal("a restore that did not finish must not use the sentence of one that did")
	}
	if strings.Contains(incomplete.Message, "were put back,") || !strings.Contains(incomplete.Message, "partly put back") ||
		!strings.Contains(incomplete.Message, "was not rewound") || !strings.Contains(incomplete.Message, "safety checkpoint") {
		t.Fatalf("sentence = %q", incomplete.Message)
	}
	if strings.Contains(incomplete.Error(), "secret-path") || strings.Contains(incomplete.Error(), "permission") {
		t.Fatalf("a cause leaked: %v", incomplete)
	}
	// Never the unknown outcome, and it still wins over a cancelled cause.
	cancelled := publicError("builtin.agenthub", &agenthub.RewindPartialError{Cause: context.Canceled, SafetyCheckpointID: "c1", RestoreIncomplete: true})
	if cancelled == nil || cancelled.Code != codeRewindFilesRestored {
		t.Fatalf("a cancelled cause must not replace the outcome: %v", cancelled)
	}
	// And it survives a second pass through the funnel.
	if again := publicError("builtin.agenthub", incomplete); again == nil || again.Code != incomplete.Code || again.Message != incomplete.Message {
		t.Fatalf("the partial restore sentence was redacted on a second pass: %v", again)
	}
}
