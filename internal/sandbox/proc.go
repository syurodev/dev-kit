package sandbox

import (
	"bytes"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// Proc wraps a started exec.Cmd.
type Proc struct {
	waitOnce  sync.Once
	waitErr   error
	exited    chan struct{} // closed once Wait has reaped the process
	exitedAt  atomic.Int64  // unix nanoseconds of the reap; 0 until then
	contained bool          // set by the backend that built the jail
	cmd       *exec.Cmd
	stdoutBuf *bytes.Buffer
	stderrBuf *bytes.Buffer
	stdinW    io.WriteCloser
	stdoutR   io.ReadCloser
}

// NewProc returns a Proc for an already-started command.
func NewProc(cmd *exec.Cmd) *Proc {
	return &Proc{cmd: cmd, exited: make(chan struct{})}
}

// NewProcWithBuffers captures stdout and stderr on cmd for later inspection.
func NewProcWithBuffers(cmd *exec.Cmd, stdout, stderr *bytes.Buffer) *Proc {
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return &Proc{cmd: cmd, exited: make(chan struct{}), stdoutBuf: stdout, stderrBuf: stderr}
}

// pipedWaitDelay bounds how long Wait keeps copying stderr once the process
// itself has exited. A command the agent left running in the background (a
// dev server) inherits stderr and holds the pipe open; without a bound, Wait
// — and whoever reaps the agent — blocked until it chose to exit.
const pipedWaitDelay = 2 * time.Second

// NewProcPiped wraps a command started with stdin/stdout pipes.
// stderr may be a buffer for capture; pass nil to leave stderr unset.
func NewProcPiped(cmd *exec.Cmd, stdin io.WriteCloser, stdout io.ReadCloser, stderr *bytes.Buffer) *Proc {
	if stderr != nil {
		cmd.Stderr = stderr
	}
	cmd.WaitDelay = pipedWaitDelay
	return &Proc{cmd: cmd, exited: make(chan struct{}), stdinW: stdin, stdoutR: stdout, stderrBuf: stderr}
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
	p.waitOnce.Do(func() {
		p.waitErr = p.cmd.Wait()
		p.exitedAt.Store(time.Now().UnixNano())
		if p.exited != nil {
			close(p.exited)
		}
	})
	return p.waitErr
}

// Exited is closed once Wait has returned, which means the kernel reported the
// supervisor process gone and it was reaped. A Kill call, a signal sent or an
// error from either is not that: only the reap is. The channel is nil for a
// Proc not built by this package, which never closes.
func (p *Proc) Exited() <-chan struct{} { return p.exited }

// ExitedAt is when Wait observed the exit; the zero Time before that.
func (p *Proc) ExitedAt() time.Time {
	n := p.exitedAt.Load()
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n)
}

// ContainsDescendants reports that the backend gives the guarantee below, and
// nothing weaker: when this process has been reaped, every process the jail
// started is gone too. bwrap with --unshare-pid has it, because the kernel ends
// every process of a PID namespace when its init exits and the supervisor waits
// for that init. A Seatbelt or Landlock jail, or no jail, does not: a descendant
// the agent started can outlive its parent.
func (p *Proc) ContainsDescendants() bool { return p.contained }

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

// StdinWriter returns the write end of the stdin pipe from StartPiped.
// Nil when the process was started with Start (buffered).
func (p *Proc) StdinWriter() io.WriteCloser {
	return p.stdinW
}

// StdoutReader returns the read end of the stdout pipe from StartPiped.
// Nil when the process was started with Start (buffered).
func (p *Proc) StdoutReader() io.ReadCloser {
	return p.stdoutR
}
