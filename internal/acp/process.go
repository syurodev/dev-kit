package acp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

// ProcessConfig configures StartProcess.
//
// TEST ONLY: StartProcess must not be called from the agenthub Claude adapter.
// Production agents are spawned via the sandbox runner; Hub cancel targets the
// sandbox supervisor PID (bwrap --die-with-parent + --unshare-pid).
type ProcessConfig struct {
	Command    string
	Args       []string
	Env        []string
	Dir        string
	CancelWait time.Duration // optional; unused by StartProcess (see ShutdownConfig)
	TermWait   time.Duration // optional; unused by StartProcess (see ShutdownConfig)
}

// Process is an unsandboxed subprocess wrapper for unit tests.
// It implements ProcessKiller. Do not use from production agent adapters.
type Process struct {
	cmd *exec.Cmd

	waitOnce sync.Once
	waitErr  error
	waitDone chan struct{}
}

// StartProcess starts an unsandboxed child process.
//
// TEST ONLY. Must not be called from the agenthub Claude adapter.
func StartProcess(ctx context.Context, cfg ProcessConfig) (*Process, error) {
	if cfg.Command == "" {
		return nil, fmt.Errorf("acp: StartProcess requires Command")
	}
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	if len(cfg.Env) > 0 {
		cmd.Env = cfg.Env
	}
	if cfg.Dir != "" {
		cmd.Dir = cfg.Dir
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &Process{
		cmd:      cmd,
		waitDone: make(chan struct{}),
	}, nil
}

// PID returns the OS process ID, or 0 if not started.
func (p *Process) PID() int {
	if p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

// Kill sends SIGKILL to the process.
func (p *Process) Kill() error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

// Signal delivers sig to the process (Unix: cmd.Process.Signal).
func (p *Process) Signal(sig os.Signal) error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Signal(sig)
}

// Wait blocks until the process exits. Safe for concurrent callers.
func (p *Process) Wait() error {
	p.waitOnce.Do(func() {
		p.waitErr = p.cmd.Wait()
		close(p.waitDone)
	})
	<-p.waitDone
	return p.waitErr
}

// Ensure Process implements ProcessKiller.
var _ ProcessKiller = (*Process)(nil)
