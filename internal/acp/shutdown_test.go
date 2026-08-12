package acp_test

import (
	"context"
	"testing"
	"time"

	"devkit/internal/acp"
)

func TestShutdownLadderKillsStubbornChild(t *testing.T) {
	ctx := context.Background()
	p, err := acp.StartProcess(ctx, acp.ProcessConfig{ // test-only helper
		Command:    "sleep",
		Args:       []string{"30"},
		CancelWait: 50 * time.Millisecond,
		TermWait:   50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- p.Wait() }()
	if err := acp.Shutdown(ctx, p, acp.ShutdownConfig{
		CancelWait: 50 * time.Millisecond,
		TermWait:   50 * time.Millisecond,
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("process did not exit")
	}
}
