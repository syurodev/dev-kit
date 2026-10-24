package preferences

import (
	"errors"
	"testing"
)

func TestSandboxToolchainsAndRevision(t *testing.T) {
	svc := NewService(NewMemoryRepository())
	initial, err := svc.AgentSandbox()
	if err != nil || !initial.DeveloperToolchainsAllowed() {
		t.Fatalf("default: %+v %v", initial, err)
	}
	off := false
	initial.AllowDeveloperToolchains = &off
	if err := svc.SetAgentSandbox(initial); err != nil {
		t.Fatal(err)
	}
	stored, err := svc.AgentSandbox()
	if err != nil || stored.Revision != 1 || stored.DeveloperToolchainsAllowed() {
		t.Fatalf("stored: %+v %v", stored, err)
	}
	if err := svc.SetAgentSandbox(initial); !errors.Is(err, ErrSandboxConflict) {
		t.Fatalf("stale write: %v", err)
	}
	if StrictAgentSandbox().DeveloperToolchainsAllowed() {
		t.Fatal("corrupt settings must not re-enable toolchain reads")
	}
}
