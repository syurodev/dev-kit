// internal/terminal/terminal_test.go
package terminal

import (
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// fakePTY blocks Read until Close is called, simulating a live process
// (unlike a PTY that returns immediately from Read, which would make the
// Registry's read-loop delete the session before a test can exercise it).
type fakePTY struct {
	mu      sync.Mutex
	written [][]byte
	resized [][2]int
	closeCh chan struct{}
	once    sync.Once
}

func newFakePTY() *fakePTY {
	return &fakePTY{closeCh: make(chan struct{})}
}

func (f *fakePTY) Read(p []byte) (int, error) {
	<-f.closeCh
	return 0, io.EOF
}

func (f *fakePTY) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.written = append(f.written, append([]byte{}, p...))
	return len(p), nil
}

func (f *fakePTY) Resize(h, w int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resized = append(f.resized, [2]int{h, w})
	return nil
}

func (f *fakePTY) Close() error {
	f.once.Do(func() { close(f.closeCh) })
	return nil
}

func TestRegistryRegisterAndOwner(t *testing.T) {
	reg := NewRegistry(nil)
	pty := newFakePTY()
	defer pty.Close()

	id := reg.Register("builtin.codegraph", pty)
	if id == "" {
		t.Fatal("expected a non-empty session ID")
	}
	owner, ok := reg.Owner(id)
	if !ok || owner != "builtin.codegraph" {
		t.Fatalf("Owner = %q, %v", owner, ok)
	}
}

func TestRegistryWriteAndResize(t *testing.T) {
	reg := NewRegistry(nil)
	pty := newFakePTY()
	defer pty.Close()
	id := reg.Register("builtin.codegraph", pty)

	if err := reg.Write(id, []byte("echo hi\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := reg.Resize(id, 40, 120); err != nil {
		t.Fatalf("Resize: %v", err)
	}

	pty.mu.Lock()
	defer pty.mu.Unlock()
	if len(pty.written) != 1 || string(pty.written[0]) != "echo hi\n" {
		t.Fatalf("written = %+v", pty.written)
	}
	if len(pty.resized) != 1 || pty.resized[0] != [2]int{40, 120} {
		t.Fatalf("resized = %+v", pty.resized)
	}
}

func TestRegistryOperationsOnUnknownSessionFail(t *testing.T) {
	reg := NewRegistry(nil)
	if _, ok := reg.Owner("nonexistent"); ok {
		t.Fatal("expected Owner to report not-found")
	}
	if err := reg.Write("nonexistent", nil); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("Write: %v", err)
	}
	if err := reg.Resize("nonexistent", 1, 1); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("Resize: %v", err)
	}
	if err := reg.Close("nonexistent"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("Close: %v", err)
	}
}

func TestRegistryCloseRemovesSession(t *testing.T) {
	reg := NewRegistry(nil)
	pty := newFakePTY()
	id := reg.Register("builtin.codegraph", pty)

	if err := reg.Close(id); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, ok := reg.Owner(id); ok {
		t.Fatal("expected session to be gone after Close")
	}
	// Closing an already-closed fakePTY must not panic (fakePTY.once guards
	// this) — Registry.Close itself must also tolerate being called twice.
	if err := reg.Close(id); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("second Close = %v, want ErrSessionNotFound", err)
	}
}

func TestRegistryOnOutputReceivesChunks(t *testing.T) {
	type chunk struct {
		sessionID string
		data      []byte
	}
	received := make(chan chunk, 1)
	reg := NewRegistry(func(sessionID string, data []byte) {
		received <- chunk{sessionID, data}
	})
	// fakePTY.Read blocks on closeCh, so it never emits output on its own —
	// this test needs a pty whose Read returns data once, then blocks, to
	// exercise the read-loop's onOutput path without needing a real process.
	rp := &readOnceThenBlockPTY{data: []byte("hello"), closeCh: make(chan struct{})}
	defer rp.Close()
	id := reg.Register("builtin.codegraph", rp)

	select {
	case c := <-received:
		if c.sessionID != id || string(c.data) != "hello" {
			t.Fatalf("unexpected chunk: %+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("onOutput was never called")
	}
}

// readOnceThenBlockPTY returns data once from Read, then blocks like fakePTY.
type readOnceThenBlockPTY struct {
	mu      sync.Mutex
	data    []byte
	sent    bool
	closeCh chan struct{}
	once    sync.Once
}

func (p *readOnceThenBlockPTY) Read(b []byte) (int, error) {
	p.mu.Lock()
	if !p.sent {
		p.sent = true
		n := copy(b, p.data)
		p.mu.Unlock()
		return n, nil
	}
	p.mu.Unlock()
	<-p.closeCh
	return 0, io.EOF
}
func (p *readOnceThenBlockPTY) Write(b []byte) (int, error) { return len(b), nil }
func (p *readOnceThenBlockPTY) Resize(h, w int) error       { return nil }
func (p *readOnceThenBlockPTY) Close() error {
	p.once.Do(func() { close(p.closeCh) })
	return nil
}
