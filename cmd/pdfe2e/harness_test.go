package main

// End-to-end check that a PDF attached in Agent Hub is readable by the real
// vendor runtimes, and that page images reach the model — not by reading the
// code, by running it.
//
// What is real: DevKit's Service, document library, PDF engine, MCP server and
// per-session socket, the devkit-mcp stdio proxy, the sandbox (bubblewrap), the
// Claude adapter driving the real claude-agent-acp + Claude Code, and the Codex
// adapter driving the real `codex app-server`. What is not: the model. Each
// runtime is pointed at a local HTTP server that speaks its provider's wire
// protocol, scripts which tool the "model" calls, and records every request it
// is sent. What the model would have seen is then read off those requests.
//
// Skipped unless the runtimes are installed (see the env vars in each test):
//
//	npm install @agentclientprotocol/claude-agent-acp@0.78.0   # into DEVKIT_PDF_E2E_CLAUDE
//	npm install @openai/codex@0.147.0                          # into DEVKIT_PDF_E2E_CODEX
//	DEVKIT_PDF_E2E_CLAUDE=… DEVKIT_PDF_E2E_CODEX=… go test ./cmd/pdfe2e/ -v -timeout 20m

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/capabilities"
	"devkit/internal/mcp"
	"devkit/internal/pdfdoc"
	"devkit/internal/sandbox"
	"devkit/internal/storage"
)

// allowAll is the capability gateway with the policy question removed: what is
// under test is the PDF path, not the grants (those have their own tests).
type allowAll struct {
	mu    sync.Mutex
	calls []string
}

func (a *allowAll) Call(ctx context.Context, subject, capability, scope string, _ map[string]any, h capabilities.Handler) (any, error) {
	a.mu.Lock()
	a.calls = append(a.calls, subject+" "+capability+" "+scope)
	a.mu.Unlock()
	return h(ctx)
}

// stack is DevKit's Agent Hub, wired the way main.go wires it.
type stack struct {
	t       *testing.T
	svc     *agenthub.Service
	lib     *agenthub.DocumentLibrary
	store   *agenthub.SQLStore
	engine  *pdfdoc.Engine
	gateway *allowAll
	work    string // the project folder sessions work in
	data    string

	emitMu  sync.Mutex
	emitted []json.RawMessage // live session/update events, as the UI would have received them
}

var (
	proxyOnce sync.Once
	proxyPath string
	proxyErr  error
)

// devkitMCPProxy builds cmd/devkit-mcp once: the stdio proxy every vendor spawns.
func devkitMCPProxy(t *testing.T) string {
	proxyOnce.Do(func() {
		dir, err := os.MkdirTemp("", "devkit-mcp-proxy")
		if err != nil {
			proxyErr = err
			return
		}
		proxyPath = filepath.Join(dir, "devkit-mcp")
		cmd := exec.Command("go", "build", "-o", proxyPath, "devkit/cmd/devkit-mcp")
		if out, err := cmd.CombinedOutput(); err != nil {
			proxyErr = &buildError{string(out), err}
		}
	})
	if proxyErr != nil {
		t.Fatalf("build devkit-mcp: %v", proxyErr)
	}
	return proxyPath
}

type buildError struct {
	out string
	err error
}

func (e *buildError) Error() string { return e.err.Error() + ": " + e.out }

// shortDir makes a directory under /tmp: unix socket paths are limited to ~104
// bytes, and t.TempDir() under a deep scratch path overflows that.
func shortDir(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// newStack wires a Service over a fresh database with the given adapters.
func newStack(t *testing.T, adapters ...agenthub.VendorAdapter) *stack {
	t.Helper()
	data := shortDir(t, "pdfe2e")
	db, err := storage.Open(filepath.Join(data, "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	store := agenthub.NewSQLStore(db)
	engine := pdfdoc.New(pdfdoc.Options{CacheDir: filepath.Join(data, "pdf-engine")})
	t.Cleanup(engine.Close)
	lib := agenthub.NewDocumentLibrary(store, filepath.Join(data, "agent-documents"), engine)

	reg := agenthub.NewRegistry()
	for _, a := range adapters {
		reg.Register(a)
	}
	st := &stack{t: t}
	svc := agenthub.NewService(reg, store, func(_ string, update json.RawMessage) {
		st.emitMu.Lock()
		st.emitted = append(st.emitted, append(json.RawMessage(nil), update...))
		st.emitMu.Unlock()
	})
	svc.Documents = lib

	gateway := &allowAll{}
	server := mcp.NewServer(mcp.Deps{Caller: gateway, PDFDocuments: lib})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	host := mcp.NewSessionHost(ctx, server, filepath.Join(data, "agents"), devkitMCPProxy(t))
	t.Cleanup(host.Close)
	svc.MCPEndpoints = host

	work := shortDir(t, "pdfe2e-work")
	st.svc, st.lib, st.store, st.engine, st.gateway, st.work, st.data = svc, lib, store, engine, gateway, work, data
	return st
}

// liveBytesWith is the size of the largest live event whose JSON contains marker.
func (s *stack) liveBytesWith(marker string) int {
	s.emitMu.Lock()
	defer s.emitMu.Unlock()
	biggest := 0
	for _, e := range s.emitted {
		if len(e) > biggest && strings.Contains(string(e), marker) {
			biggest = len(e)
		}
	}
	return biggest
}

// storedBytesWith is the size of the largest stored transcript row whose JSON
// contains marker.
func (s *stack) storedBytesWith(sessionID, marker string) int {
	s.t.Helper()
	page, err := s.svc.TranscriptPage(context.Background(), sessionID, 0, 0)
	if err != nil {
		s.t.Fatal(err)
	}
	biggest := 0
	for _, e := range page.Entries {
		if len(e.Payload) > biggest && strings.Contains(string(e.Payload), marker) {
			biggest = len(e.Payload)
		}
	}
	return biggest
}

func (s *stack) newSession(agent agenthub.AgentType) agenthub.AgentSession {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	sess, err := s.svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agent, Cwd: s.work})
	if err != nil {
		s.t.Fatalf("create %s session: %v", agent, err)
	}
	s.t.Cleanup(func() { _ = s.svc.CloseSession(context.Background(), sess.ID) })
	return sess
}

func (s *stack) copyPDF(name, fixture string) string {
	s.t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "pdfdoc", "testdata", fixture))
	if err != nil {
		s.t.Fatal(err)
	}
	path := filepath.Join(s.work, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		s.t.Fatal(err)
	}
	return path
}

func (s *stack) invoke(sess agenthub.AgentSession, prompt string, attachments ...string) agenthub.InvokeResult {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	res, err := s.svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: prompt, Attachments: attachments})
	if err != nil {
		s.t.Fatalf("invoke: %v", err)
	}
	return res
}

// envRunner runs vendors in the real sandbox with extra environment: the
// provider's base URL and a dummy key, which point the runtime at the mock.
type envRunner struct {
	inner *sandbox.Runner
	env   map[string]string
	args  []string // appended to the vendor's command line (Codex: -c key=value)
}

func (r envRunner) StartPiped(ctx context.Context, p sandbox.Policy, c sandbox.Cmd) (*sandbox.Proc, error) {
	env := map[string]string{}
	for k, v := range p.Env {
		env[k] = v
	}
	for k, v := range r.env {
		env[k] = v
	}
	p.Env = env
	c.Args = append(append([]string(nil), c.Args...), r.args...)
	return r.inner.StartPiped(ctx, p, c)
}

func requireDir(t *testing.T, env string) string {
	t.Helper()
	dir := os.Getenv(env)
	if dir == "" {
		t.Skipf("set %s to run this (see harness_test.go)", env)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("%s=%s: %v", env, dir, err)
	}
	return dir
}

func contains(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

func (a *allowAll) CallOperation(ctx context.Context, r capabilities.CallRequest) (any, error) {
	return a.Call(ctx, r.SubjectID, r.Capability, r.Scope, r.Metadata, r.Handler)
}
