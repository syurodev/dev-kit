package sandbox

import (
	"os/exec"
	"testing"
	"time"
)

func startSh(t *testing.T, script string) *Proc {
	t.Helper()
	command, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("requires sh")
	}
	cmd := exec.Command(command, "-c", script)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return NewProc(cmd)
}

// Kill is a request. Only a Wait that returned says the process was reaped.
func TestProcExitIsObservedOnlyAfterWaitReturns(t *testing.T) {
	p := startSh(t, "sleep 30")
	select {
	case <-p.Exited():
		t.Fatal("exit reported before Wait ran")
	default:
	}
	if !p.ExitedAt().IsZero() {
		t.Fatal("ExitedAt set before Wait ran")
	}
	if err := p.Kill(); err != nil {
		t.Fatal(err)
	}
	// A kill that was sent but not yet reaped is still not an observed exit.
	select {
	case <-p.Exited():
		t.Fatal("Kill alone reported the exit")
	default:
	}
	_ = p.Wait()
	select {
	case <-p.Exited():
	case <-time.After(time.Second):
		t.Fatal("Exited not closed after Wait")
	}
	if p.ExitedAt().IsZero() {
		t.Fatal("ExitedAt not set after Wait")
	}
}

func TestProcConcurrentWaitCloseExitedOnce(t *testing.T) {
	p := startSh(t, "exit 0")
	done := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func() { _ = p.Wait(); done <- struct{}{} }()
	}
	for i := 0; i < 4; i++ {
		<-done
	}
	<-p.Exited()
}

// A plain Proc makes no containment promise; only a backend that builds a
// pid-namespace jail may.
func TestProcContainsDescendantsDefaultsFalse(t *testing.T) {
	p := startSh(t, "exit 0")
	defer p.Wait()
	if p.ContainsDescendants() {
		t.Fatal("a Proc built without a containing backend claims containment")
	}
}
