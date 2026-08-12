package acp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// ProcessKiller is the minimal process control surface used by Shutdown.
// Production Hub cancel targets the sandbox supervisor PID (bwrap
// --die-with-parent + --unshare-pid); this interface also covers unsandboxed
// test helpers. Real ACP agents often ignore stdin EOF, so OS signals are
// required for reliable teardown.
type ProcessKiller interface {
	PID() int
	Kill() error
	Wait() error
	Signal(os.Signal) error
}

// ShutdownConfig controls how long Shutdown waits at each rung of the ladder.
type ShutdownConfig struct {
	CancelWait time.Duration // default 5s — wait after ACP session/cancel
	TermWait   time.Duration // default 5s — wait after SIGTERM
}

const defaultShutdownWait = 5 * time.Second

// Shutdown assumes the caller already best-effort sent ACP session/cancel via Conn.
// Ladder:
//  1. wait CancelWait for voluntary exit
//  2. Signal(SIGTERM)
//  3. wait TermWait
//  4. Kill
func Shutdown(ctx context.Context, p ProcessKiller, cfg ShutdownConfig) error {
	cancelWait := cfg.CancelWait
	if cancelWait == 0 {
		cancelWait = defaultShutdownWait
	}
	termWait := cfg.TermWait
	if termWait == 0 {
		termWait = defaultShutdownWait
	}

	done := make(chan error, 1)
	go func() { done <- p.Wait() }()

	if err, ok := waitOrTimeout(ctx, done, cancelWait); ok {
		return normalizeWaitErr(err)
	}

	_ = p.Signal(syscall.SIGTERM)

	if err, ok := waitOrTimeout(ctx, done, termWait); ok {
		return normalizeWaitErr(err)
	}

	_ = p.Kill()

	select {
	case err := <-done:
		return normalizeWaitErr(err)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func waitOrTimeout(ctx context.Context, done <-chan error, d time.Duration) (error, bool) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case err := <-done:
		return err, true
	case <-ctx.Done():
		// Escalate the ladder even if the caller's context is cancelled.
		return nil, false
	case <-timer.C:
		return nil, false
	}
}

func normalizeWaitErr(err error) error {
	if err == nil {
		return nil
	}
	// ExitError is expected when we SIGTERM/Kill a still-running process.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return nil
	}
	return err
}
