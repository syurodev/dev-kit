// internal/terminal/local.go
package terminal

import (
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
	cmd := exec.Command(shellPath)
	cmd.Dir = workDir
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		return nil, err
	}
	go func() { _ = cmd.Wait() }()
	return &localPTY{f: f, cmd: cmd}, nil
}

type localPTY struct {
	f   *os.File
	cmd *exec.Cmd
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
