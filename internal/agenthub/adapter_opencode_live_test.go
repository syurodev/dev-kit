package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"devkit/internal/acp"
	"devkit/internal/agentruntime"
	"devkit/internal/sandbox"

	"github.com/google/uuid"
)

// The tests in this file run the real opencode under the real jail against a
// scripted model: what they establish is what the unit tests cannot — that the
// configuration DevKit writes is the configuration opencode obeys. They are the
// conformance check for an opencode that DevKit did not pin: run them against a
// new release before trusting it.
//
//	DEVKIT_OPENCODE_COMMAND=/abs/path/to/opencode go test ./internal/agenthub/ -run OpenCodeLive -v
//
// The binary is the native one, e.g. node_modules/@opencode/cli-darwin-arm64/bin/opencode.

// scriptedModel is an OpenAI-compatible server that answers by keyword in the
// last user message: SHELL:<cmd> and WRITE:<path>|<text> call that tool,
// SLOW streams for a while, anything else is a plain answer.
type scriptedModel struct {
	srv *httptest.Server

	mu       sync.Mutex
	systems  []string // the system message of every request that carried tools
	requests int
}

func newScriptedModel(t *testing.T) *scriptedModel {
	t.Helper()
	m := &scriptedModel{}
	m.srv = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *scriptedModel) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		_, _ = w.Write([]byte(`{"data":[{"id":"mock-1","object":"model"}]}`))
		return
	}
	var body struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		Tools []json.RawMessage `json:"tools"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	m.mu.Lock()
	m.requests++
	if len(body.Tools) > 0 && len(body.Messages) > 0 {
		m.systems = append(m.systems, string(body.Messages[0].Content))
	}
	m.mu.Unlock()

	w.Header().Set("content-type", "text/event-stream")
	flusher := w.(http.Flusher)
	send := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}
	chunk := func(delta map[string]any, finish any) map[string]any {
		return map[string]any{"id": "c1", "object": "chat.completion.chunk", "model": "mock-1",
			"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}}}
	}
	last, lastRole := "", ""
	if n := len(body.Messages); n > 0 {
		last, lastRole = string(body.Messages[n-1].Content), body.Messages[n-1].Role
	}
	call := func(name string, args map[string]any) {
		arguments, _ := json.Marshal(args)
		send(chunk(map[string]any{"role": "assistant", "tool_calls": []map[string]any{{"index": 0, "id": "call_" + name, "type": "function",
			"function": map[string]any{"name": name, "arguments": string(arguments)}}}}, nil))
		send(chunk(map[string]any{}, "tool_calls"))
	}
	field := func(key string) (string, bool) {
		_, rest, ok := strings.Cut(last, key+":")
		if !ok {
			return "", false
		}
		rest = strings.TrimSpace(rest)
		rest = strings.TrimSuffix(rest, `"`)
		rest, _, _ = strings.Cut(rest, `\n`)
		return rest, true
	}
	switch {
	case len(body.Tools) > 0 && lastRole == "user" && strings.Contains(last, "SHELL:"):
		cmd, _ := field("SHELL")
		call("shell", map[string]any{"command": cmd, "description": "run it"})
	case len(body.Tools) > 0 && lastRole == "user" && strings.Contains(last, "WRITE:"):
		spec, _ := field("WRITE")
		path, text, _ := strings.Cut(spec, "|")
		call("write", map[string]any{"path": path, "content": text})
	default:
		parts := []string{"mock ", "answer ", "done"}
		if strings.Contains(last, "SLOW") {
			parts = nil
			for i := 0; i < 20; i++ {
				parts = append(parts, fmt.Sprintf("slow %d ", i))
			}
		}
		for _, p := range parts {
			if strings.Contains(last, "SLOW") {
				time.Sleep(400 * time.Millisecond)
			}
			send(chunk(map[string]any{"role": "assistant", "content": p}, nil))
		}
		send(chunk(map[string]any{}, "stop"))
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func (m *scriptedModel) firstSystem() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.systems) == 0 {
		return ""
	}
	return m.systems[0]
}

// openCodeForReal is a real opencode under the real jail, with a scripted
// provider written into DevKit's opencode home (where a person's own providers
// would go), or a skip.
func openCodeForReal(t *testing.T) (*OpenCodeAdapter, *scriptedModel, string) {
	t.Helper()
	bin := os.Getenv("DEVKIT_OPENCODE_COMMAND")
	if bin == "" {
		t.Skip("set DEVKIT_OPENCODE_COMMAND to a native opencode binary to run it in the real jail")
	}
	if err := sandbox.DefaultRunner().Available(context.Background()); err != nil {
		t.Skipf("sandbox unavailable: %v", err)
	}
	model := newScriptedModel(t)
	// A short path: the sandbox and the unix sockets under it have length limits.
	home, err := os.MkdirTemp("", "dkoc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	cfgDir := filepath.Join(home, "config", "opencode")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	provider := fmt.Sprintf(`{"providers":{"mock":{"name":"Mock","package":"@opencode/ai/providers/openai-compatible",
		"settings":{"baseURL":%q,"apiKey":"test"},"models":{"mock-1":{"name":"Mock 1"}}}}}`, model.srv.URL+"/v1")
	if err := os.WriteFile(filepath.Join(cfgDir, "opencode.jsonc"), []byte(provider), 0o600); err != nil {
		t.Fatal(err)
	}
	return NewOpenCodeAdapter(OpenCodeOptions{Command: bin, Home: home}), model, home
}

// liveSession opens a session on the scripted model and records what DevKit is
// asked and told.
type liveSession struct {
	rt    SessionRuntime
	mu    sync.Mutex
	asked []json.RawMessage
	// answer is what the "person" replies to a permission question.
	answer string
	tools  []string
}

func openLive(t *testing.T, a *OpenCodeAdapter, spec OpenSpec) *liveSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	rt, err := a.Open(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close(context.Background()) })
	s := &liveSession{rt: rt, answer: "once"}
	rt.SetHandlers(RuntimeHandlers{
		OnPermission: func(_ context.Context, params json.RawMessage) (json.RawMessage, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.asked = append(s.asked, append(json.RawMessage(nil), params...))
			return openCodeSelected(s.answer), nil
		},
		OnUpdate: func(u json.RawMessage, _ json.RawMessage) {
			var head struct {
				Kind string `json:"sessionUpdate"`
				Name string `json:"title"`
			}
			if json.Unmarshal(u, &head) == nil && head.Kind == "tool_call" {
				s.mu.Lock()
				s.tools = append(s.tools, head.Name)
				s.mu.Unlock()
			}
		},
	})
	if err := rt.(RuntimeConfigurator).SetConfigOption(ctx, "model", "mock/mock-1"); err != nil {
		t.Fatalf("choosing the scripted model: %v", err)
	}
	return s
}

func (s *liveSession) prompt(t *testing.T, text string) PromptOutcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := s.rt.Prompt(ctx, []acp.ContentBlock{{Type: "text", Text: text}}, nil)
	if err != nil {
		t.Fatalf("prompt %q: %v", text, err)
	}
	return out
}

func (s *liveSession) questions() []json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]json.RawMessage(nil), s.asked...)
}

func (s *liveSession) forget() {
	s.mu.Lock()
	s.asked = nil
	s.mu.Unlock()
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

func TestOpenCodeLiveInstructionsReachTheModel(t *testing.T) {
	a, model, _ := openCodeForReal(t)
	s := openLive(t, a, OpenSpec{Cwd: t.TempDir(), Brief: "LIVE-BRIEF-MARKER"})
	s.prompt(t, "hello")
	system := model.firstSystem()
	if !strings.Contains(system, "LIVE-BRIEF-MARKER") || !strings.Contains(system, "tools.devkit.") {
		t.Fatalf("DevKit's prompt did not reach the model's system message (config `instructions`); got %d bytes:\n%.600s", len(system), system)
	}
}

func TestOpenCodeLiveAskModeAsksAndRespectsTheAnswer(t *testing.T) {
	a, _, _ := openCodeForReal(t)
	cwd := t.TempDir()
	s := openLive(t, a, OpenSpec{Cwd: cwd})

	s.prompt(t, "SHELL: printf yes > allowed.txt")
	asked := s.questions()
	if len(asked) != 1 {
		t.Fatalf("a command in Ask mode raised %d questions, want 1 (the config `permissions` is ignored if 0)", len(asked))
	}
	if strings.Contains(string(asked[0]), "allow_always") {
		t.Fatalf("the person was offered \"always\": %s", asked[0])
	}
	if !exists(filepath.Join(cwd, "allowed.txt")) {
		t.Fatal("an allowed command did not run")
	}

	s.forget()
	s.answer = "reject"
	out := s.prompt(t, "SHELL: printf no > refused.txt")
	if len(s.questions()) != 1 {
		t.Fatalf("second command raised %d questions, want 1 (a saved \"always\" would make it 0)", len(s.questions()))
	}
	if exists(filepath.Join(cwd, "refused.txt")) {
		t.Fatal("a refused command ran: the answer does not stop it")
	}
	if out.StopReason != "cancelled" {
		t.Logf("stop reason after a refusal = %q (measured as \"cancelled\" on 2.0.32)", out.StopReason)
	}
}

func TestOpenCodeLiveModesChangeWhoAnswers(t *testing.T) {
	a, _, _ := openCodeForReal(t)
	cwd := t.TempDir()
	s := openLive(t, a, OpenSpec{Cwd: cwd})
	configurator := s.rt.(RuntimeConfigurator)

	// Auto: edits go through, commands are still put to the person.
	if err := configurator.SetMode(context.Background(), openCodeModeAuto); err != nil {
		t.Fatal(err)
	}
	s.prompt(t, "WRITE:"+filepath.Join(cwd, "auto.txt")+"|written")
	if len(s.questions()) != 0 || !exists(filepath.Join(cwd, "auto.txt")) {
		t.Fatalf("Auto: questions=%d, file written=%v; want an edit without a question", len(s.questions()), exists(filepath.Join(cwd, "auto.txt")))
	}
	s.prompt(t, "SHELL: printf auto > auto-shell.txt")
	if len(s.questions()) != 1 {
		t.Fatalf("Auto: a command raised %d questions, want 1", len(s.questions()))
	}

	// Full auto: nothing is asked.
	s.forget()
	if err := configurator.SetMode(context.Background(), openCodeModeFull); err != nil {
		t.Fatal(err)
	}
	s.prompt(t, "SHELL: printf full > full.txt")
	if len(s.questions()) != 0 || !exists(filepath.Join(cwd, "full.txt")) {
		t.Fatalf("Full auto: questions=%d, ran=%v; want it to run unasked", len(s.questions()), exists(filepath.Join(cwd, "full.txt")))
	}
}

func TestOpenCodeLiveCancelEndsTheTurnAndKeepsTheProcess(t *testing.T) {
	a, _, _ := openCodeForReal(t)
	s := openLive(t, a, OpenSpec{Cwd: t.TempDir()})

	done := make(chan PromptOutcome, 1)
	go func() {
		out, _ := s.rt.Prompt(context.Background(), []acp.ContentBlock{{Type: "text", Text: "SLOW please"}}, nil)
		done <- out
	}()
	time.Sleep(2 * time.Second)
	started := time.Now()
	if err := s.rt.Cancel(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case out := <-done:
		if out.StopReason != "cancelled" {
			t.Fatalf("stop reason = %q, want cancelled", out.StopReason)
		}
		t.Logf("cancelled in %s", time.Since(started))
	case <-time.After(20 * time.Second):
		t.Fatal("the turn did not end after Cancel")
	}
	if dead, err := s.rt.(RuntimeLiveness).Dead(); dead {
		t.Fatalf("Cancel killed the process (%v): opencode ends the turn itself", err)
	}
	if out := s.prompt(t, "again"); out.StopReason != "end_turn" {
		t.Fatalf("the session after a cancel: stop reason %q", out.StopReason)
	}
}

func TestOpenCodeLiveSessionSurvivesItsProcess(t *testing.T) {
	a, _, _ := openCodeForReal(t)
	cwd := t.TempDir()
	first := openLive(t, a, OpenSpec{Cwd: cwd})
	first.prompt(t, "remember this")
	id := first.rt.ACPSessionID()
	if err := first.rt.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	rt, err := a.Load(ctx, OpenSpec{Cwd: cwd}, id)
	if err != nil {
		t.Fatalf("reopening %s: %v", id, err)
	}
	defer rt.Close(context.Background())
	if rt.ACPSessionID() != id {
		t.Fatalf("reopened %q, want %q", rt.ACPSessionID(), id)
	}
	if err := rt.(RuntimeConfigurator).SetConfigOption(ctx, "model", "mock/mock-1"); err != nil {
		t.Fatal(err)
	}
	out, err := rt.Prompt(ctx, []acp.ContentBlock{{Type: "text", Text: "and again"}}, nil)
	if err != nil || out.StopReason != "end_turn" {
		t.Fatalf("a reopened session: %+v, %v", out, err)
	}
}

func TestOpenCodeLiveCatalogListsTheProviderModel(t *testing.T) {
	a, _, _ := openCodeForReal(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	// The shared-home spec is the one Settings builds by default for a person who
	// shares their real $HOME; it inherits the host TMPDIR, which the jail does
	// not let opencode read.
	shared := false
	for name, spec := range map[string]OpenSpec{"private home": {}, "shared home": {PrivateHome: &shared}} {
		options, err := a.ConfigCatalog(ctx, spec)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		found := false
		for _, o := range options[0].Options {
			found = found || o.Value == "mock/mock-1"
		}
		if !found {
			t.Fatalf("%s: the catalog does not list the provider the home's config adds: %+v", name, options)
		}
	}
}

func TestOpenCodeLiveProbeAndSignIn(t *testing.T) {
	a, _, _ := openCodeForReal(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	methods, err := a.AuthMethods(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(methods) != 1 || methods[0].Args[len(methods[0].Args)-1] != "--login" {
		t.Fatalf("auth methods = %+v, want the one terminal login", methods)
	}
}

// withSessionTemp gives spec the private scratch directory every Agent Hub
// session has on macOS. A shared-home spec without one inherits the host's
// TMPDIR, which sits under /var/folders — outside what the jail grants, so
// opencode cannot create its temp directory there.
func withSessionTemp(t *testing.T, spec OpenSpec) OpenSpec {
	t.Helper()
	spec.SessionID = uuid.NewString()
	if err := prepareSessionTemp(&spec, true); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { removeSessionTemp(spec.SessionID) })
	return spec
}

// Sharing the real $HOME is Agent Hub's default. opencode looks in ~/.claude and
// ~/.agents when it opens a session, so a host that has them is the case worth
// running.
func TestOpenCodeLiveSharedHomeSessionWorks(t *testing.T) {
	a, _, _ := openCodeForReal(t)
	shared := false
	cwd := t.TempDir()
	s := openLive(t, a, withSessionTemp(t, OpenSpec{Cwd: cwd, PrivateHome: &shared}))
	s.prompt(t, "SHELL: printf shared > shared.txt")
	if len(s.questions()) != 1 || !exists(filepath.Join(cwd, "shared.txt")) {
		t.Fatalf("shared home: %d questions, ran=%v", len(s.questions()), exists(filepath.Join(cwd, "shared.txt")))
	}
}

// The whole path a person takes: DevKit asks npm for the latest release,
// installs it, proves it starts under the jail (Probe) and resolves it for a
// session. Needs the network and a Node; it installs ~150 MB into a temp dir.
//
//	DEVKIT_OPENCODE_INSTALL_LIVE=1 go test ./internal/agenthub/ -run OpenCodeInstallLive -v
func TestOpenCodeInstallLive(t *testing.T) {
	if os.Getenv("DEVKIT_OPENCODE_INSTALL_LIVE") == "" {
		t.Skip("set DEVKIT_OPENCODE_INSTALL_LIVE=1 to install opencode from npm")
	}
	if err := sandbox.DefaultRunner().Available(context.Background()); err != nil {
		t.Skipf("sandbox unavailable: %v", err)
	}
	home, err := os.MkdirTemp("", "dkoc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	runtimes := agentruntime.New(filepath.Join(home, "runtimes"))
	adapter := NewOpenCodeAdapter(OpenCodeOptions{
		Home:    filepath.Join(home, "agent-homes", "opencode"),
		Runtime: func() (agentruntime.Resolved, error) { return runtimes.ResolveLeased(agentruntime.OpenCodeCLI) },
	})
	runtimes.Verifiers = map[string]func(context.Context, agentruntime.Resolved) error{agentruntime.OpenCodeCLI: adapter.Probe}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	if err := runtimes.Install(ctx, agentruntime.OpenCodeCLI, ""); err != nil {
		t.Fatalf("install: %v", err)
	}
	st, err := runtimes.Status(ctx, agentruntime.OpenCodeCLI)
	if err != nil || st.Current == nil {
		t.Fatalf("status = %+v, %v", st, err)
	}
	t.Logf("installed opencode %s (%d bytes on disk)", st.Current.Version, st.DiskBytes)
	if st.TestedVersion != "" {
		t.Errorf("TestedVersion = %q, want none for an unpinned agent", st.TestedVersion)
	}
	if methods, err := adapter.AuthMethods(ctx); err != nil || len(methods) == 0 {
		t.Fatalf("auth methods of the installed runtime: %v, %v", methods, err)
	}
}

// A real project lives under /Users (or /home), not in a temp directory, and
// opencode resolves every directory above it when it opens a session. Where the
// jail let it only stat those directories, session/new failed with
// `EPERM: lstat '/Users'` for every project — which no test caught, because
// every test used a temp directory under /var. Both home modes, since the
// ancestors are granted differently.
func TestOpenCodeLiveProjectUnderHome(t *testing.T) {
	a, _, _ := openCodeForReal(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	for name, private := range map[string]bool{"shared home": false, "private home": true} {
		t.Run(name, func(t *testing.T) {
			project, err := os.MkdirTemp(home, ".dkoc-project-")
			if err != nil {
				t.Skipf("cannot make a project under %s: %v", home, err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(project) })
			priv := private
			if private && runtime.GOOS == "darwin" {
				// Said up front instead (openCodeNeedsSharedHome): Seatbelt cannot let
				// opencode walk through a hidden home without listing all of it.
				_, err := a.Open(context.Background(), OpenSpec{Cwd: project, PrivateHome: &priv})
				if !errors.Is(err, ErrProcessFailed) || !strings.Contains(err.Error(), "Share home folder") {
					t.Fatalf("a private-home session inside the home: err = %v, want the explained refusal", err)
				}
				return
			}
			s := openLive(t, a, withSessionTemp(t, OpenSpec{Cwd: project, PrivateHome: &priv}))
			s.prompt(t, "SHELL: printf here > from-home.txt")
			if !exists(filepath.Join(project, "from-home.txt")) {
				t.Fatalf("%s: the session opened but the command did not run", name)
			}
		})
	}
}
