# Terminal Primitive + CodeGraph Local Terminal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the shared [Terminal](base-doc `content/projects/dev-kit/terminal.mdx`) primitive (`internal/terminal`: `PTY` interface + session `Registry`, currently "Design only" with zero runtime) **and** its first real consumer — [CodeGraph § Terminal cục bộ](base-doc `content/projects/dev-kit/codegraph.mdx:301-334`), a local shell at the project root for `mvn`/`./gradlew`/running the app.

**Why build the primitive and its first consumer together:** the primitive alone (an interface and a registry nobody registers anything with) is inert and untestable in any way that proves it actually works — a `PTY` interface with no real implementation exercising it risks getting the contract wrong in ways only real usage would surface. `2026-08-19-codegraph-file-editing.md` established the same reasoning for `Service.projectRoot`. SSH's remote `PTY` implementation (the primitive's second consumer, per the design doc) is a separate, later plan — this one only builds the **local** implementation (`creack/pty`) CodeGraph needs.

**Architecture:** `internal/terminal.Registry` maps `sessionID → PTY`, shared by every consumer; `PTY` is a 4-method interface (`Read`/`Write`/`Resize`/`Close`) both a local shell and a future remote SSH session will implement without the Registry knowing which. Backend-only — this plan does **not** wire `xterm.js` or any frontend component (matching the "defer the UI, ship a working backend" split used for every prior CodeGraph phase); `main.go`'s `Events.Emit` wiring is stubbed with a plain Go callback and a clear TODO for whoever writes the frontend Terminal component. **No MCP tool surface** — per the design doc's own explicit non-goal: terminal is human-only, an agent already has `ssh.runCommand`-style one-shot execution (bounded, policy-tagged) and a full interactive session is deliberately not offered to agents.

**Capability model, as specified:** creating a session (`CodeGraphStartTerminal`) is gated by the owning module's own capability (`codegraph:terminal`, scope `project:active`) — same as every other module-specific creation action. Once created, `TerminalWrite`/`TerminalResize`/`TerminalClose` are **shared** bridge methods (not codegraph-specific) gated by one capability, `terminal:io`, scope `session:<id>` — routed through the session's **owning subject** (looked up from the Registry), not a hardcoded module name, since these 3 methods will also serve SSH's future remote sessions from the same bridge file.

**Tech Stack:** `github.com/creack/pty` (new dependency — verified against its real API before designing: `pty.StartWithSize(cmd *exec.Cmd, ws *pty.Winsize) (*os.File, error)`, `pty.Setsize(t *os.File, ws *pty.Winsize) error`, `pty.Winsize{Rows, Cols uint16}`). `github.com/google/uuid` for session IDs — **already** a dependency (`internal/agenthub` already uses `uuid.NewString()` for its own session IDs; reused here for consistency, not re-invented).

---

## File Structure

Create:
- `internal/terminal/terminal.go` — `PTY` interface, `Registry`.
- `internal/terminal/terminal_test.go` — `Registry` tests against a `fakePTY` test double.
- `internal/terminal/local.go` — `NewLocalPTY` (`creack/pty`-backed).
- `internal/terminal/local_test.go` — real shell integration test.
- `internal/bridge/terminal.go` — shared `TerminalWrite`/`TerminalResize`/`TerminalClose`.
- `internal/bridge/terminal_test.go` — tests for them.

Modify:
- `go.mod`/`go.sum` — add `github.com/creack/pty`.
- `internal/codegraph/service.go` — `projectRoot` (if not already added by an earlier plan), `SetTerminalRegistry`, `StartTerminal`.
- `internal/codegraph/service_test.go` — tests for `StartTerminal`.
- `internal/app/container.go` — `Terminal *terminal.Registry` field, 2 new capability grants.
- `main.go` — construct the real `Registry`, wire it into the container and `Service`, `defer container.Terminal.CloseAll()`.
- `internal/bridge/codegraph.go` — `CodeGraphStartTerminal`.
- `internal/bridge/codegraph_test.go` — test for it.

---

## Task 1: `Service.projectRoot` (skip if already present)

**Files:**
- Modify: `internal/codegraph/service.go`

- [ ] **Step 1: Check whether this already exists**

```bash
grep -n "projectRoot" internal/codegraph/service.go
```

If an earlier plan in this sequence (`2026-08-19-codegraph-file-editing.md` or `2026-08-19-codegraph-auto-reindex.md`) has already added `projectRoot string` to `Service`, `s.projectRoot = projectPath` inside `OpenProject`, and `activeProject() (Store, string, error)`, **skip this entire task**.

- [ ] **Step 2: If missing, add it**

In `internal/codegraph/service.go`:

```go
type Service struct {
	mu          sync.RWMutex
	store       Store
	analyzer    Analyzer
	active      bool
	projectRoot string
}
```

```go
func (s *Service) OpenProject(ctx context.Context, projectPath string) error {
	result, err := s.analyzer.Analyze(ctx, projectPath)
	if err != nil {
		return fmt.Errorf("codegraph: analyze %q: %w", projectPath, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.ReplaceIndex(ctx, result); err != nil {
		s.active = false
		return fmt.Errorf("codegraph: replace index: %w", err)
	}
	s.active = true
	s.projectRoot = projectPath
	return nil
}
```

```go
func (s *Service) activeProject() (Store, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.active {
		return nil, "", ErrNoActiveProject
	}
	return s.store, s.projectRoot, nil
}

func (s *Service) activeStore() (Store, error) {
	store, _, err := s.activeProject()
	return store, err
}
```

- [ ] **Step 3: Build and test**

```bash
go build ./...
go test ./internal/codegraph/... -race
```

- [ ] **Step 4: Commit (only if Step 2 was needed)**

```bash
git add internal/codegraph/service.go
git commit -m "feat(codegraph): Service tracks the active project's root path"
```

---

## Task 2: `internal/terminal` — `PTY` interface and `Registry`

**Files:**
- Create: `internal/terminal/terminal.go`
- Create: `internal/terminal/terminal_test.go`

- [ ] **Step 1: Write the failing tests**

```go
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
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/terminal/... -v
```

Expected: FAIL — compile error, the package doesn't exist yet.

- [ ] **Step 3: Implement**

```go
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
}

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
```

Add `"github.com/google/uuid"` to `go.mod` if it's somehow not already there (it should be — `internal/agenthub/service.go` already imports it):

```bash
grep -n "google/uuid" go.mod
```

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/terminal/... -v -race
```

Expected: all 6 pass.

- [ ] **Step 5: Commit**

```bash
git add internal/terminal/terminal.go internal/terminal/terminal_test.go
git commit -m "feat(terminal): PTY interface and session Registry"
```

---

## Task 3: `NewLocalPTY` (`creack/pty`)

**Files:**
- Create: `internal/terminal/local.go`
- Create: `internal/terminal/local_test.go`

- [ ] **Step 1: Add the dependency**

```bash
go get github.com/creack/pty
go build ./...
```

- [ ] **Step 2: Write the failing test**

```go
// internal/terminal/local_test.go
package terminal

import (
	"strings"
	"testing"
	"time"
)

func TestLocalPTYRunsShellInWorkDir(t *testing.T) {
	dir := t.TempDir()
	pty, err := NewLocalPTY("/bin/sh", dir, 24, 80)
	if err != nil {
		t.Fatalf("NewLocalPTY: %v", err)
	}
	defer pty.Close()

	if _, err := pty.Write([]byte("pwd\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var output strings.Builder
	buf := make([]byte, 4096)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		n, err := pty.Read(buf)
		if n > 0 {
			output.Write(buf[:n])
			if strings.Contains(output.String(), dir) {
				return // found the echoed `pwd` output — success
			}
		}
		if err != nil {
			t.Fatalf("Read: %v (output so far: %q)", err, output.String())
		}
	}
	t.Fatalf("did not see cwd %q in shell output within 5s, got: %q", dir, output.String())
}

func TestLocalPTYResize(t *testing.T) {
	dir := t.TempDir()
	pty, err := NewLocalPTY("/bin/sh", dir, 24, 80)
	if err != nil {
		t.Fatalf("NewLocalPTY: %v", err)
	}
	defer pty.Close()

	if err := pty.Resize(40, 120); err != nil {
		t.Fatalf("Resize: %v", err)
	}
}

func TestLocalPTYCloseKillsProcess(t *testing.T) {
	dir := t.TempDir()
	pty, err := NewLocalPTY("/bin/sh", dir, 24, 80)
	if err != nil {
		t.Fatalf("NewLocalPTY: %v", err)
	}
	if err := pty.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// A second Close (defer + explicit) must not panic — os.File.Close on an
	// already-closed file and Process.Kill on an already-dead process both
	// return an error, not a panic; NewLocalPTY's Close must tolerate that.
	_ = pty.Close()
}
```

- [ ] **Step 3: Run to confirm failure**

```bash
go test ./internal/terminal/... -run TestLocalPTY -v
```

Expected: FAIL — compile error, `NewLocalPTY` doesn't exist yet.

- [ ] **Step 4: Implement**

```go
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
```

- [ ] **Step 5: Run the tests**

```bash
go test ./internal/terminal/... -run TestLocalPTY -v
```

Expected: all 3 pass. (`TestLocalPTYRunsShellInWorkDir` spawns a real `/bin/sh` — if this hangs or fails in your environment, confirm `/bin/sh` exists and is a real POSIX shell; every Unix CI runner and dev machine has it, this is not expected to be flaky, but it is the one test in this plan that depends on the host OS having a real shell binary.)

- [ ] **Step 6: Full package regression**

```bash
go test ./internal/terminal/... -race -count=1
```

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/terminal/local.go internal/terminal/local_test.go
git commit -m "feat(terminal): NewLocalPTY (creack/pty)"
```

---

## Task 4: `Service.StartTerminal`

**Files:**
- Modify: `internal/codegraph/service.go`
- Modify: `internal/codegraph/service_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/codegraph/service_test.go`:

```go
func TestServiceStartTerminalUsesProjectRootAsCwd(t *testing.T) {
	dir := t.TempDir()
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	reg := terminal.NewRegistry(nil)
	svc.SetTerminalRegistry(reg)
	if err := svc.OpenProject(context.Background(), dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	sessionID, err := svc.StartTerminal(24, 80)
	if err != nil {
		t.Fatalf("StartTerminal: %v", err)
	}
	defer reg.Close(sessionID)

	owner, ok := reg.Owner(sessionID)
	if !ok || owner != "builtin.codegraph" {
		t.Fatalf("Owner = %q, %v", owner, ok)
	}
}

func TestServiceStartTerminalBeforeOpenProjectFails(t *testing.T) {
	svc := NewService(nilStore{}, fakeAnalyzer{})
	svc.SetTerminalRegistry(terminal.NewRegistry(nil))
	if _, err := svc.StartTerminal(24, 80); !errors.Is(err, ErrNoActiveProject) {
		t.Fatalf("expected ErrNoActiveProject, got %v", err)
	}
}

func TestServiceStartTerminalWithoutRegistryFails(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	if err := svc.OpenProject(context.Background(), t.TempDir()); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if _, err := svc.StartTerminal(24, 80); !errors.Is(err, ErrTerminalNotConfigured) {
		t.Fatalf("expected ErrTerminalNotConfigured, got %v", err)
	}
}
```

Add `"devkit/internal/terminal"` to `internal/codegraph/service_test.go`'s imports.

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/codegraph/... -run TestServiceStartTerminal -v
```

Expected: FAIL — compile error, `SetTerminalRegistry`/`StartTerminal`/`ErrTerminalNotConfigured` don't exist yet.

- [ ] **Step 3: Implement**

In `internal/codegraph/codegraph.go`, add to the `var (...)` error block:

```go
	// ErrTerminalNotConfigured is returned by StartTerminal when no
	// *terminal.Registry has been wired in via SetTerminalRegistry — the
	// container always wires a real one (see main.go), this only fires for
	// an ephemeral/test Service that never called SetTerminalRegistry.
	ErrTerminalNotConfigured = errors.New("codegraph: terminal registry not configured")
```

In `internal/codegraph/service.go`, add a field to `Service`:

```go
type Service struct {
	mu          sync.RWMutex
	store       Store
	analyzer    Analyzer
	active      bool
	projectRoot string

	terminalRegistry *terminal.Registry
}
```

```go
// SetTerminalRegistry wires the shared Terminal primitive's Registry into
// this Service — called once from container construction (see main.go),
// separately from NewService so NewService's signature (used by every
// existing test in this package) doesn't need to change.
func (s *Service) SetTerminalRegistry(reg *terminal.Registry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.terminalRegistry = reg
}

// StartTerminal opens a local shell ($SHELL, falling back to /bin/sh) at
// the active project's root and registers it with the shared Terminal
// Registry, returning its session ID. Matches the design doc's own
// sequence: Service itself creates the PTY and registers it — the bridge
// layer (Task 6) only handles the capability check and delegates here.
func (s *Service) StartTerminal(rows, cols int) (string, error) {
	_, root, err := s.activeProject()
	if err != nil {
		return "", err
	}
	s.mu.RLock()
	reg := s.terminalRegistry
	s.mu.RUnlock()
	if reg == nil {
		return "", ErrTerminalNotConfigured
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	pty, err := terminal.NewLocalPTY(shell, root, rows, cols)
	if err != nil {
		return "", fmt.Errorf("codegraph: start terminal: %w", err)
	}
	return reg.Register("builtin.codegraph", pty), nil
}
```

Add `"os"` and `"devkit/internal/terminal"` to `internal/codegraph/service.go`'s imports (`"os"` may already be present from an earlier plan's `SetProjectPath` `MkdirAll` change — check first).

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/codegraph/... -run TestServiceStartTerminal -v
```

Expected: all 3 pass.

- [ ] **Step 5: Full package regression**

```bash
go test ./internal/codegraph/... -race
```

- [ ] **Step 6: Commit**

```bash
git add internal/codegraph/codegraph.go internal/codegraph/service.go internal/codegraph/service_test.go
git commit -m "feat(codegraph): Service.StartTerminal"
```

---

## Task 5: Wire `Container.Terminal` and capability grants

**Files:**
- Modify: `internal/app/container.go`
- Modify: `main.go`

- [ ] **Step 1: Add the field**

In `internal/app/container.go`, add to `Container` (near `CodeGraph *codegraph.Service`):

```go
	// Terminal is nil in ephemeral/test containers built via
	// NewContainer/NewContainerWithStores — main.go constructs the real one
	// (it needs a Wails-aware output callback container.go doesn't have
	// access to) and wires it into CodeGraph via SetTerminalRegistry.
	Terminal *terminal.Registry
```

Add `"devkit/internal/terminal"` to `internal/app/container.go`'s imports.

- [ ] **Step 2: Add the 2 capability grants**

In `internal/app/container.go`, add next to the existing `builtin.codegraph` grants:

```go
		{SubjectID: "builtin.codegraph", Capability: "codegraph:terminal", Scope: "project:active"},
		{SubjectID: "builtin.codegraph", Capability: "terminal:io", Scope: "session:*"},
```

- [ ] **Step 3: Construct the real Registry in `main.go`**

In `main.go`, after `container := app.NewContainerWithStores(...)` and before `container.CodeGraph`'s first use (alongside the existing `container.Preferences = ...`/`container.ConfigDir = ...` post-construction wiring):

```go
	terminalRegistry := terminal.NewRegistry(func(sessionID string, chunk []byte) {
		// TODO(frontend Terminal component): wire to the real Wails event —
		// verify the exact Events.Emit call shape against @wailsio/runtime's
		// current API when that plan is written, same "verify, don't guess"
		// discipline used for every other Wails API touched in this
		// sequence. Until then this silently drops output — StartTerminal/
		// Write/Resize/Close all work end-to-end without it (proven by this
		// plan's own tests), only the live output stream needs the frontend.
	})
	container.Terminal = terminalRegistry
	container.CodeGraph.SetTerminalRegistry(terminalRegistry)
	defer terminalRegistry.CloseAll()
```

Add `"devkit/internal/terminal"` to `main.go`'s imports.

- [ ] **Step 4: Build**

```bash
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add internal/app/container.go main.go
git commit -m "feat(app): wire the Terminal registry into the container and CodeGraph"
```

---

## Task 6: Bridge methods

**Files:**
- Create: `internal/bridge/terminal.go`
- Create: `internal/bridge/terminal_test.go`
- Modify: `internal/bridge/codegraph.go`
- Modify: `internal/bridge/codegraph_test.go`

- [ ] **Step 1: Write the failing tests**

```go
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
	s := newTestService(t)
	reg := terminal.NewRegistry(nil)
	s.container.Terminal = reg
	pty := newFakePTY()
	defer pty.Close()
	id := reg.Register("builtin.codegraph", pty)

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
}

func TestTerminalWriteUnknownSessionFails(t *testing.T) {
	s := newTestService(t)
	s.container.Terminal = terminal.NewRegistry(nil)
	if err := s.TerminalWrite("nonexistent", nil); !errors.Is(err, terminal.ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}
```

Add to `internal/bridge/codegraph_test.go`:

```go
func TestCodeGraphStartTerminal(t *testing.T) {
	s := newTestService(t)
	reg := terminal.NewRegistry(nil)
	s.container.Terminal = reg
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{result: codegraph.AnalysisResult{}})
	s.container.CodeGraph.SetTerminalRegistry(reg)
	if err := s.container.CodeGraph.SetProjectPath(t.TempDir() + "/index.db"); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := s.container.CodeGraph.OpenProject(context.Background(), t.TempDir()); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	sessionID, err := s.CodeGraphStartTerminal(24, 80)
	if err != nil {
		t.Fatalf("CodeGraphStartTerminal: %v", err)
	}
	defer reg.Close(sessionID)

	owner, ok := reg.Owner(sessionID)
	if !ok || owner != "builtin.codegraph" {
		t.Fatalf("Owner = %q, %v", owner, ok)
	}
}
```

Add `"devkit/internal/terminal"` to `internal/bridge/codegraph_test.go`'s imports. (This test spawns a real local shell via `StartTerminal`'s `NewLocalPTY` call — the one integration-style test in this task, proving the whole chain from bridge method down to a real PTY actually works, not just the mocked ownership-routing the other tests check.)

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/bridge/... -run "TestTerminal|TestCodeGraphStartTerminal" -v
```

Expected: FAIL — compile error, `TerminalWrite`/`TerminalResize`/`TerminalClose`/`CodeGraphStartTerminal` don't exist yet.

- [ ] **Step 3: Implement the shared terminal bridge methods**

```go
// internal/bridge/terminal.go
package bridge

import (
	"context"

	"devkit/internal/terminal"
)

func (s *AppService) TerminalWrite(sessionID string, data []byte) error {
	owner, ok := s.container.Terminal.Owner(sessionID)
	if !ok {
		return terminal.ErrSessionNotFound
	}
	_, err := s.call(context.Background(), owner, "terminal:io", "session:"+sessionID, map[string]any{
		"action":     "terminal_write",
		"session_id": sessionID,
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.Terminal.Write(sessionID, data)
	})
	return err
}

func (s *AppService) TerminalResize(sessionID string, rows, cols int) error {
	owner, ok := s.container.Terminal.Owner(sessionID)
	if !ok {
		return terminal.ErrSessionNotFound
	}
	_, err := s.call(context.Background(), owner, "terminal:io", "session:"+sessionID, map[string]any{
		"action":     "terminal_resize",
		"session_id": sessionID,
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.Terminal.Resize(sessionID, rows, cols)
	})
	return err
}

func (s *AppService) TerminalClose(sessionID string) error {
	owner, ok := s.container.Terminal.Owner(sessionID)
	if !ok {
		return terminal.ErrSessionNotFound
	}
	_, err := s.call(context.Background(), owner, "terminal:io", "session:"+sessionID, map[string]any{
		"action":     "terminal_close",
		"session_id": sessionID,
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.Terminal.Close(sessionID)
	})
	return err
}
```

- [ ] **Step 4: Implement `CodeGraphStartTerminal`**

In `internal/bridge/codegraph.go`, add:

```go
func (s *AppService) CodeGraphStartTerminal(rows, cols int) (string, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:terminal", "project:active", map[string]any{
		"action": "start_terminal",
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.StartTerminal(rows, cols)
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}
```

- [ ] **Step 5: Run the tests**

```bash
go test ./internal/bridge/... -run "TestTerminal|TestCodeGraph" -v
```

Expected: all pass, including every pre-existing `TestCodeGraph*` test.

- [ ] **Step 6: Full repo regression**

```bash
go test ./internal/... ./cmd/... -count=1
go build ./...
```

- [ ] **Step 7: Commit**

```bash
git add internal/bridge/terminal.go internal/bridge/terminal_test.go internal/bridge/codegraph.go internal/bridge/codegraph_test.go
git commit -m "feat(terminal): bridge methods — CodeGraphStartTerminal, shared Write/Resize/Close"
```

---

## Task 7: Full regression

**Files:** none (verification only)

- [ ] **Step 1: Standard check**

```bash
go test ./internal/... ./cmd/... -count=1
go build ./...
```

- [ ] **Step 2: Race**

```bash
go test ./internal/... ./cmd/... -race -count=1
```

Expected: everything passes.

---

## What this plan does NOT do

**No frontend `xterm.js` component, no `Events.Emit` wiring** — the output-callback path is stubbed with a documented TODO (Task 5, Step 3); `StartTerminal`/`TerminalWrite`/`TerminalResize`/`TerminalClose` all work end-to-end without it (proven by Task 6's tests), only the *live* output stream needs a frontend to actually display anything. **No SSH remote `PTY` implementation** — this plan only builds `NewLocalPTY`; a remote one (wrapping an `x/crypto/ssh` `Session`) is a separate future plan, SSH's own consumer-side work per the design doc. **No MCP tool surface** — deliberately, per the design doc's explicit non-goal (human-only interactive terminal; an agent already has one-shot `ssh.runCommand`). **No tmux-style multiplexing, no scrollback persistence across app restarts** — both explicitly out of scope per the design doc's own non-goals list.
