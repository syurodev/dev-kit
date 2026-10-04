package ssh

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"devkit/internal/terminal"
	gossh "golang.org/x/crypto/ssh"
)

// SetTerminalRegistry is startup wiring, shared with the local terminals.
func (s *Service) SetTerminalRegistry(reg *terminal.Registry) { s.terminalRegistry = reg }

// StartTerminal uses the same credential and host-key path as one-shot commands.
// The setup context ends at registration; closing the tab owns the shell lifetime.
func (s *Service) StartTerminal(ctx context.Context, profileID, passphrase string, rows, cols int) (string, error) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.terminalRegistry == nil {
		return "", errors.New("SSH terminal is not configured")
	}
	if rows < 1 || cols < 1 || rows > 1000 || cols > 1000 {
		return "", errors.New("invalid terminal size")
	}
	client, err := s.dial(ctx, profileID, passphrase)
	if err != nil {
		return "", err
	}
	// Session creation and PTY requests are network operations too: bound setup
	// even when the peer authenticated and then stopped responding.
	setupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	stopCancel := context.AfterFunc(setupCtx, func() { _ = client.Close() })
	defer stopCancel()
	session, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return "", err
	}
	p := &remotePTY{client: client, session: session, exitDone: make(chan struct{})}
	fail := func(err error) (string, error) { _ = p.Close(); return "", err }
	p.input, err = session.StdinPipe()
	if err != nil {
		return fail(err)
	}
	// A remote PTY normally merges stderr into stdout. Handle both streams for
	// servers that send extended data anyway, without losing or interleaving writes.
	reader, writer := io.Pipe()
	p.output, p.writer = reader, writer
	session.Stdout, session.Stderr = writer, writer
	if err = session.RequestPty("xterm-256color", rows, cols, gossh.TerminalModes{gossh.ECHO: 1, gossh.TTY_OP_ISPEED: 14400, gossh.TTY_OP_OSPEED: 14400}); err != nil {
		return fail(err)
	}
	if err = session.Shell(); err != nil {
		return fail(err)
	}
	if !stopCancel() || setupCtx.Err() != nil {
		return fail(context.Canceled)
	}
	s.mu.Lock()
	if s.terminals == nil {
		s.terminals = make(map[*remotePTY]string)
	}
	s.terminals[p] = profileID
	s.mu.Unlock()
	p.onClose = func() { s.mu.Lock(); delete(s.terminals, p); s.mu.Unlock() }
	go p.awaitExit()
	return s.terminalRegistry.Register("builtin.ssh", p), nil
}

type remotePTY struct {
	exitDone chan struct{}
	exitCode int
	client   *gossh.Client
	session  *gossh.Session
	input    io.WriteCloser
	output   *io.PipeReader
	writer   *io.PipeWriter
	once     sync.Once
	onClose  func()
}

func (p *remotePTY) Read(data []byte) (int, error)  { return p.output.Read(data) }
func (p *remotePTY) Write(data []byte) (int, error) { return p.input.Write(data) }
func (p *remotePTY) Resize(rows, cols int) error {
	if rows < 1 || cols < 1 || rows > 1000 || cols > 1000 {
		return errors.New("invalid terminal size")
	}
	return p.session.WindowChange(rows, cols)
}
func (p *remotePTY) Wait() int {
	<-p.exitDone
	return p.exitCode
}

func (p *remotePTY) awaitExit() {
	err := p.session.Wait()
	code := 0
	if err != nil {
		code = -1
		var exit *gossh.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitStatus()
		}
	}
	_ = p.Close()
	p.exitCode = code
	close(p.exitDone)
}
func (p *remotePTY) Close() error {
	p.once.Do(func() {
		if p.output != nil {
			_ = p.output.Close()
		}
		if p.writer != nil {
			_ = p.writer.Close()
		}
		_ = p.session.Close()
		_ = p.client.Close()
		if p.onClose != nil {
			p.onClose()
		}
	})
	return nil
}
func (s *Service) closeTerminalsForProfile(id string) {
	s.mu.Lock()
	var sessions []*remotePTY
	for p, owner := range s.terminals {
		if id == "" || owner == id {
			sessions = append(sessions, p)
		}
	}
	s.mu.Unlock()
	for _, p := range sessions {
		_ = p.Close()
	}
}
