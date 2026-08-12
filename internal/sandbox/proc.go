package sandbox

import (
	"bytes"
	"io"
	"os/exec"
)

// Proc wraps a started exec.Cmd.
type Proc struct {
	cmd       *exec.Cmd
	stdoutBuf *bytes.Buffer
	stderrBuf *bytes.Buffer
}

// NewProc returns a Proc for an already-started command.
func NewProc(cmd *exec.Cmd) *Proc {
	return &Proc{cmd: cmd}
}

// NewProcWithBuffers captures stdout and stderr on cmd for later inspection.
func NewProcWithBuffers(cmd *exec.Cmd, stdout, stderr *bytes.Buffer) *Proc {
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return &Proc{cmd: cmd, stdoutBuf: stdout, stderrBuf: stderr}
}

// Output returns copies of captured stdout and stderr, when buffers were set.
func (p *Proc) Output() (stdout, stderr []byte) {
	if p.stdoutBuf != nil {
		stdout = append([]byte(nil), p.stdoutBuf.Bytes()...)
	}
	if p.stderrBuf != nil {
		stderr = append([]byte(nil), p.stderrBuf.Bytes()...)
	}
	return stdout, stderr
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
