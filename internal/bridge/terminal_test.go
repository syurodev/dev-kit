// internal/bridge/terminal_test.go
package bridge

import (
	"errors"
	"io"
	"sync"
	"testing"

	"devkit/internal/terminal"
)

// fakePTY — a local copy of internal/terminal's own test double; Go test
// doubles aren't shared across package boundaries without exporting them,
// which isn't warranted for something this small.
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

func (f *fakePTY) Read(p []byte) (int, error) { <-f.closeCh; return 0, io.EOF }
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
func (f *fakePTY) Close() error { f.once.Do(func() { close(f.closeCh) }); return nil }

func TestTerminalWriteResizeCloseRouteThroughOwner(t *testing.T) {
	for _, owner := range []string{"builtin.codegraph", "builtin.ssh"} {
		t.Run(owner, func(t *testing.T) {
			s := newTestService(t)
			reg := terminal.NewRegistry(nil)
			s.container.Terminal = reg
			pty := newFakePTY()
			defer pty.Close()
			id := reg.Register(owner, pty)

			if err := s.TerminalWrite(id, []byte("ls\n")); err != nil {
				t.Fatalf("TerminalWrite: %v", err)
			}
			if err := s.TerminalResize(id, 40, 120); err != nil {
				t.Fatalf("TerminalResize: %v", err)
			}

			pty.mu.Lock()
			if len(pty.written) != 1 || string(pty.written[0]) != "ls\n" {
				t.Fatalf("written = %+v", pty.written)
			}
			if len(pty.resized) != 1 || pty.resized[0] != [2]int{40, 120} {
				t.Fatalf("resized = %+v", pty.resized)
			}
			pty.mu.Unlock()

			if err := s.TerminalClose(id); err != nil {
				t.Fatalf("TerminalClose: %v", err)
			}
			if _, ok := reg.Owner(id); ok {
				t.Fatal("expected session removed after TerminalClose")
			}
		})
	}
}

func TestTerminalWriteUnknownSessionFails(t *testing.T) {
	s := newTestService(t)
	s.container.Terminal = terminal.NewRegistry(nil)
	if err := s.TerminalWrite("nonexistent", nil); !errors.Is(err, terminal.ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}
