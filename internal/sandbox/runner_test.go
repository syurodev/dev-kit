package sandbox

import (
	"context"
	"os/exec"
	"runtime"
	"testing"
)

type fakeBackend struct {
	start func(ctx context.Context, p Policy, c Cmd) (*Proc, error)
}

func (f *fakeBackend) Name() string { return "fake" }

func (f *fakeBackend) Available(context.Context) error { return nil }

func (f *fakeBackend) Start(ctx context.Context, p Policy, c Cmd) (*Proc, error) {
	return f.start(ctx, p, c)
}

func TestRunnerStartUsesBackend(t *testing.T) {
	var saw Policy
	fake := &fakeBackend{start: func(ctx context.Context, p Policy, c Cmd) (*Proc, error) {
		saw = p
		cmd := exec.CommandContext(ctx, "true")
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return NewProc(cmd), nil
	}}
	r := NewRunner(fake)
	proc, err := r.Start(context.Background(), Policy{WorkDir: t.TempDir()}, Cmd{Path: "true"})
	if err != nil {
		t.Fatal(err)
	}
	defer proc.Kill()
	if saw.WorkDir == "" {
		t.Fatal("backend not called")
	}
}

func TestRunnerStartAppliesPolicyDefaults(t *testing.T) {
	var saw Policy
	fake := &fakeBackend{start: func(ctx context.Context, p Policy, c Cmd) (*Proc, error) {
		saw = p
		cmd := exec.CommandContext(ctx, "true")
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return NewProc(cmd), nil
	}}
	r := NewRunner(fake)
	proc, err := r.Start(context.Background(), Policy{WorkDir: t.TempDir()}, Cmd{Path: "true"})
	if err != nil {
		t.Fatal(err)
	}
	defer proc.Kill()
	if saw.Network != NetworkNone {
		t.Fatalf("network default = %q, want %q", saw.Network, NetworkNone)
	}
	if !saw.EffectivePrivateHome() {
		t.Fatal("private home should default to true")
	}
}

func TestDefaultRunnerAvailableOnCI(t *testing.T) {
	r := DefaultRunner()
	err := r.Available(context.Background())
	if runtime.GOOS == "linux" {
		if err != nil {
			t.Skip("bwrap missing:", err)
		}
	}
}

func TestRunnerInvalidPolicyFailsBeforeBackend(t *testing.T) {
	called := false
	fake := &fakeBackend{start: func(ctx context.Context, p Policy, c Cmd) (*Proc, error) {
		called = true
		return nil, nil
	}}
	r := NewRunner(fake)
	_, err := r.Start(context.Background(), Policy{WorkDir: "rel"}, Cmd{Path: "true"})
	if err == nil {
		t.Fatal("expected error")
	}
	if called {
		t.Fatal("backend called on invalid policy")
	}
}
