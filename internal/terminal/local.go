// internal/terminal/local.go
package terminal

import (
	"errors"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

// NewLocalPTY starts shellPath as a child process with a pseudo-terminal,
// cwd set to workDir, sized rows x cols. Used by CodeGraph's local terminal
// (cwd = the active project's root) — a future remote implementation for
// SSH wraps an x/crypto/ssh Session instead, satisfying the same PTY
// interface without either consumer knowing the difference.
func NewLocalPTY(shellPath, workDir string, rows, cols int) (PTY, error) {
	return startLocal(exec.Command(shellPath), workDir, rows, cols)
}

// NewLocalCommandPTY runs one program instead of a shell — path with args, and
// exactly env (nil inherits the app's). Unsandboxed, like the user typing it:
// Agent Hub's sign-in uses it for the agent's own login command, which needs a
// browser, a localhost callback and the keychain.
func NewLocalCommandPTY(path string, args, env []string, workDir string, rows, cols int) (*localPTY, error) {
	cmd := exec.Command(path, args...)
	cmd.Env = env
	return startLocal(cmd, workDir, rows, cols)
}

func startLocal(cmd *exec.Cmd, workDir string, rows, cols int) (*localPTY, error) {
	cmd.Dir = workDir
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		return nil, err
	}
	p := &localPTY{f: f, cmd: cmd, done: make(chan struct{})}
	go func() {
		p.code = exitCode(cmd.Wait())
		close(p.done)
	}()
	return p, nil
}

// exitCode is the process's exit status, or -1 when it has none (killed by a
// signal, or Wait itself failed).
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

type localPTY struct {
	f    *os.File
	cmd  *exec.Cmd
	done chan struct{}
	code int
}

// Wait blocks until the process exits and returns its exit code.
func (p *localPTY) Wait() int {
	<-p.done
	return p.code
}

func (p *localPTY) Read(b []byte) (int, error)  { return p.f.Read(b) }
func (p *localPTY) Write(b []byte) (int, error) { return p.f.Write(b) }

func (p *localPTY) Resize(h, w int) error {
	return pty.Setsize(p.f, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)})
}

func (p *localPTY) Close() error {
	err := p.f.Close()
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	return err
}
