package agenthub

import (
	"context"
	"devkit/internal/agentruntime"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func leaseSignal() (func(), <-chan struct{}) {
	done := make(chan struct{})
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }, done
}
func assertReleased(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("runtime lease was not released")
	}
}
func assertHeld(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
		t.Fatal("runtime lease released before process exit")
	default:
	}
}
func leasedCodex(t *testing.T, scenario string) (codexFixture, <-chan struct{}) {
	t.Helper()
	f := newCodexFixture(t, scenario)
	release, done := leaseSignal()
	command := f.adapter.opts.Command
	f.adapter.opts.Command = ""
	f.adapter.opts.Runtime = func() (agentruntime.Resolved, error) {
		return agentruntime.Resolved{Command: command, Version: "0.147.0", Release: release}, nil
	}
	return f, done
}
func TestCodexLeaseLivesUntilSessionExit(t *testing.T) {
	f, done := leasedCodex(t, "turn")
	rt := f.open(t)
	assertHeld(t, done)
	rt.Close(context.Background())
	assertReleased(t, done)
}
func TestCodexSignInHoldsRuntimeLease(t *testing.T) {
	f, done := leasedCodex(t, "loginWait")
	flow, err := f.adapter.StartBrowserSignIn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertHeld(t, done)
	if err := f.adapter.CancelSignIn(context.Background(), flow.LoginID); err != nil {
		t.Fatal(err)
	}
	assertReleased(t, done)
}
func TestCodexFailedOpenReleasesRuntimeLease(t *testing.T) {
	for _, scenario := range []string{"signedout", "dieAtInitialize"} {
		t.Run(scenario, func(t *testing.T) {
			f, done := leasedCodex(t, scenario)
			if _, err := f.adapter.Open(context.Background(), OpenSpec{Cwd: f.cwd}); err == nil {
				t.Fatal("expected failure")
			}
			assertReleased(t, done)
		})
	}
	f, done := leasedCodex(t, "turn")
	f.adapter.opts.APIKey = func(context.Context) (string, error) { return "", errors.New("unusable key") }
	if _, err := f.adapter.Open(context.Background(), OpenSpec{Cwd: f.cwd}); err == nil {
		t.Fatal("expected key failure")
	}
	assertReleased(t, done)
}
func TestCodexAccountProbeReleasesRuntimeLease(t *testing.T) {
	f, done := leasedCodex(t, "turn")
	if _, err := f.adapter.SignInStatus(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertReleased(t, done)
}
func TestClaudeFailedHandshakeReleasesRuntimeLease(t *testing.T) {
	release, done := leaseSignal()
	a := NewClaudeAdapter(ClaudeOptions{Runtime: func() (agentruntime.Resolved, error) {
		return agentruntime.Resolved{Command: filepath.Join(t.TempDir(), "claude"), Release: release}, nil
	}, Runner: deadVendorRunner{}})
	if _, err := a.Open(context.Background(), OpenSpec{Cwd: t.TempDir()}); err == nil {
		t.Fatal("expected failure")
	}
	assertReleased(t, done)
}
