package sandbox_test

import (
	"errors"
	"testing"

	"devkit/internal/sandbox"
)

func TestPolicyRequiresAbsoluteWorkDir(t *testing.T) {
	err := sandbox.Policy{WorkDir: "rel"}.Validate()
	if !errors.Is(err, sandbox.ErrInvalidPolicy) {
		t.Fatalf("got %v", err)
	}
}

func TestPolicyRejectsEmptyCommand(t *testing.T) {
	err := sandbox.Policy{WorkDir: t.TempDir()}.ValidateCmd(sandbox.Cmd{Path: ""})
	if !errors.Is(err, sandbox.ErrInvalidPolicy) {
		t.Fatalf("got %v", err)
	}
}

func TestDefaultSensitivePathsNormalized(t *testing.T) {
	p := sandbox.Policy{WorkDir: t.TempDir()}
	paths := p.EffectiveDenyRead() // includes ~/.ssh etc expanded
	if len(paths) == 0 {
		t.Fatal("expected default denies")
	}
}
