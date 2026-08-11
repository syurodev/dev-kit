package bridge

import (
	"errors"
	"testing"

	"devkit/internal/environments"
)

func TestPublicErrorMapsEnvTagInUse(t *testing.T) {
	err := publicError("builtin.environments", environments.ErrTagInUse)
	if err.Code != codeValidationFailed {
		t.Fatalf("code = %q, want %q", err.Code, codeValidationFailed)
	}
	if err.Message != environments.ErrTagInUse.Error() {
		t.Fatalf("message = %q, want %q", err.Message, environments.ErrTagInUse.Error())
	}
	if !errors.Is(environments.ErrTagInUse, environments.ErrTagInUse) {
		t.Fatal("sanity")
	}
}
