package bridge

import (
	"fmt"
	"strings"
	"testing"

	"devkit/internal/agenthub"
)

// A model the vendor does not offer used to reach an agent as "Choose a valid
// agent setting." with nothing about which setting or what would work.
func TestAWrongModelTellsTheAgentWhatWouldWork(t *testing.T) {
	var detail = "opencode has no model \"GLM-5.3-Plash Free\". Closest: opencode/glm-5.3-flash-free (GLM-5.3 Flash Free). Pass one of those ids."
	err := fmt.Errorf("create: %w", &agenthub.ModelChoiceError{Detail: detail})

	got := publicError("builtin.agenthub", err)
	if got.Code != codeValidationFailed || !strings.Contains(got.Message, "opencode/glm-5.3-flash-free") {
		t.Fatalf("agent was told %q (%s)", got.Message, got.Code)
	}

	// Any other invalid setting keeps the generic sentence, and so does a
	// sentence that is not one bounded line.
	for name, err := range map[string]error{
		"generic":   agenthub.ErrInvalidConfigOption,
		"multiline": &agenthub.ModelChoiceError{Detail: "a\nb"},
		"too long":  &agenthub.ModelChoiceError{Detail: strings.Repeat("x", 2000)},
	} {
		if got := publicError("builtin.agenthub", err); got.Message != "Choose a valid agent setting." {
			t.Errorf("%s: %q", name, got.Message)
		}
	}
}
