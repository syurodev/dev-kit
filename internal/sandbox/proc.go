package sandbox

import (
	"io"
	"os/exec"
)

// Proc wraps a started exec.Cmd.
type Proc struct {
	cmd *exec.Cmd
}

// NewProc returns a Proc for an already-started command.
func NewProc(cmd *exec.Cmd) *Proc {
	return &Proc{cmd: cmd}
}

func (p *Proc) Wait() error {
	return p.cmd.Wait()
}

func (p *Proc) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

func (p *Proc) PID() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p *Proc) Stdin() io.Reader {
	return p.cmd.Stdin
}

func (p *Proc) Stdout() io.Writer {
	return p.cmd.Stdout
}

func (p *Proc) Stderr() io.Writer {
	return p.cmd.Stderr
}
