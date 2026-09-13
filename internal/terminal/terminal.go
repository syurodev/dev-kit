// internal/terminal/terminal.go
package terminal

import (
	"errors"
	"sync"

	"github.com/google/uuid"
)

// ErrSessionNotFound is returned by every Registry method when sessionID
// isn't currently registered (never existed, or was already Close()'d).
var ErrSessionNotFound = errors.New("terminal: session not found")

// PTY is implemented by both a local shell (see local.go, creack/pty-backed)
// and a future remote SSH session — Registry and every bridge method
// operate on this interface without knowing which kind of session it is.
type PTY interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Resize(h, w int) error
	Close() error
}

type session struct {
	pty   PTY
	owner string
}

// Registry maps sessionID -> PTY, shared by every Terminal consumer. onOutput
// is called from each session's own background read goroutine for every
// chunk read from its PTY — wire this to the real Wails
// app.Event.Emit("terminal:output:<sessionId>", chunk) call once the
// frontend Terminal component exists to consume it; nil is valid (silently
// drops output, useful for tests that only exercise Write/Resize/Close).
type Registry struct {
	mu       sync.Mutex
	sessions map[string]*session
	onOutput func(sessionID string, chunk []byte)
	// OnExit reports a session's exit code when its process ends, for PTYs that
	// can tell (a local process: Wait() int). Set during startup wiring, before
	// the first Register. Driven by the process, not by the read loop: a child
	// that leaves the terminal open behind it must not hide the exit.
	OnExit func(sessionID string, code int)
}

// exitWaiter is a PTY that can report its process's exit code.
type exitWaiter interface{ Wait() int }

func NewRegistry(onOutput func(sessionID string, chunk []byte)) *Registry {
	return &Registry{sessions: make(map[string]*session), onOutput: onOutput}
}

// Register starts a background goroutine reading pty continuously and
// returns a new session ID. owner is the subject (e.g. "builtin.codegraph")
// that created this session — TerminalWrite/Resize/Close route their
// capability check through this owner rather than a hardcoded subject,
// since those bridge methods are shared across every Terminal consumer.
func (r *Registry) Register(owner string, pty PTY) string {
	id := uuid.NewString()
	r.mu.Lock()
	r.sessions[id] = &session{pty: pty, owner: owner}
	r.mu.Unlock()
	go r.readLoop(id, pty)
	if waiter, ok := pty.(exitWaiter); ok && r.OnExit != nil {
		go func() { r.OnExit(id, waiter.Wait()) }()
	}
	return id
}

func (r *Registry) readLoop(id string, pty PTY) {
	buf := make([]byte, 4096)
	for {
		n, err := pty.Read(buf)
		if n > 0 && r.onOutput != nil {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			r.onOutput(id, chunk)
		}
		if err != nil {
			r.mu.Lock()
			delete(r.sessions, id)
			r.mu.Unlock()
			return
		}
	}
}

// Owner returns the subject that created sessionID, for the bridge layer to
// route TerminalWrite/Resize/Close's capability check through.
func (r *Registry) Owner(sessionID string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[sessionID]
	if !ok {
		return "", false
	}
	return s.owner, true
}

func (r *Registry) Write(sessionID string, data []byte) error {
	r.mu.Lock()
	s, ok := r.sessions[sessionID]
	r.mu.Unlock()
	if !ok {
		return ErrSessionNotFound
	}
	_, err := s.pty.Write(data)
	return err
}

func (r *Registry) Resize(sessionID string, h, w int) error {
	r.mu.Lock()
	s, ok := r.sessions[sessionID]
	r.mu.Unlock()
	if !ok {
		return ErrSessionNotFound
	}
	return s.pty.Resize(h, w)
}

// Close kills the session's process immediately (no "keep running in
// background" concept — see the design doc's Lifecycle section) and removes
// it from the registry. Closing the underlying PTY unblocks readLoop's
// pending Read, which then exits on the resulting error and does its own
// (redundant, harmless — delete on a missing map key is a no-op) cleanup.
func (r *Registry) Close(sessionID string) error {
	r.mu.Lock()
	s, ok := r.sessions[sessionID]
	if ok {
		delete(r.sessions, sessionID)
	}
	r.mu.Unlock()
	if !ok {
		return ErrSessionNotFound
	}
	return s.pty.Close()
}

// CloseAll closes every open session — called once on app shutdown so no
// shell process outlives the app itself.
func (r *Registry) CloseAll() {
	r.mu.Lock()
	sessions := make([]*session, 0, len(r.sessions))
	for _, s := range r.sessions {
		sessions = append(sessions, s)
	}
	r.sessions = make(map[string]*session)
	r.mu.Unlock()
	for _, s := range sessions {
		s.pty.Close()
	}
}
