package agenthub_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"devkit/internal/acp"
	"devkit/internal/agenthub"
	"devkit/internal/identity"
	"devkit/internal/preferences"
	"devkit/internal/storage"
)

type fakeEndpoints struct{ opened, closed []string }

func (f *fakeEndpoints) Open(sessionID string) (agenthub.MCPEndpoint, error) {
	f.opened = append(f.opened, sessionID)
	return agenthub.MCPEndpoint{
		SocketPath: "/tmp/" + sessionID[:8] + ".sock",
		Server:     acp.McpServerStdio{Name: "devkit", Command: "/opt/devkit-mcp", Args: []string{"--socket", "/tmp/x.sock"}, Env: []acp.EnvVariable{}},
		Close:      func() { f.closed = append(f.closed, sessionID) },
	}, nil
}

func newIdentityService(t *testing.T) (*agenthub.Service, *agenthub.MockAdapter, *fakeEndpoints) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	mock := agenthub.NewMockAdapter()
	reg := agenthub.NewRegistry()
	reg.Register(mock)
	svc := agenthub.NewService(reg, agenthub.NewSQLStore(db), nil)
	eps := &fakeEndpoints{}
	svc.MCPEndpoints = eps
	return svc, mock, eps
}

func agentCtx(id string) context.Context {
	return identity.WithCaller(context.Background(), identity.Agent(id))
}

func uiCtx() context.Context { return identity.WithCaller(context.Background(), identity.UI()) }

func projectDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCreateSessionOpensEndpointAndPassesSpec(t *testing.T) {
	svc, mock, eps := newIdentityService(t)
	sess, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(eps.opened) != 1 || eps.opened[0] != sess.ID {
		t.Fatalf("endpoint opened for %v, want [%s]", eps.opened, sess.ID)
	}
	spec := mock.LastSpec()
	if len(spec.McpServers) != 1 || spec.McpServers[0].Name != "devkit" {
		t.Fatalf("spec.McpServers = %+v", spec.McpServers)
	}
	if len(spec.AllowUnixSockets) != 1 || len(spec.ReadOnlyPaths) != 1 || spec.ReadOnlyPaths[0] != "/opt/devkit-mcp" {
		t.Fatalf("spec = %+v", spec)
	}
	if err := svc.CloseSession(uiCtx(), sess.ID); err != nil {
		t.Fatal(err)
	}
	if len(eps.closed) != 1 {
		t.Fatalf("endpoint must close with the session, closed = %v", eps.closed)
	}
}

// SandboxExtras widens every session's OpenSpec, on top of whatever the MCP
// endpoint already grants — the user's opt-in escape from the default jail
// (docs/dev-kit/sandbox.mdx), not a replacement for it.
func TestCreateSessionAppliesSandboxExtras(t *testing.T) {
	svc, mock, _ := newIdentityService(t)
	svc.SandboxExtras = func() preferences.AgentSandboxSettings {
		return preferences.AgentSandboxSettings{
			DockerSocketPath:   "/var/run/docker.sock",
			ExtraReadOnlyPaths: []string{"/opt/shared-lib"},
		}
	}
	if _, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)}); err != nil {
		t.Fatal(err)
	}
	spec := mock.LastSpec()
	if !slices.Contains(spec.AllowUnixSockets, "/var/run/docker.sock") {
		t.Fatalf("spec.AllowUnixSockets = %v, want the Docker socket included", spec.AllowUnixSockets)
	}
	if !slices.Contains(spec.ReadOnlyPaths, "/opt/shared-lib") {
		t.Fatalf("spec.ReadOnlyPaths = %v, want the extra path included", spec.ReadOnlyPaths)
	}
	if got := spec.ExtraEnv["DOCKER_HOST"]; got != "unix:///var/run/docker.sock" {
		t.Fatalf("spec.ExtraEnv[DOCKER_HOST] = %q, want the configured socket as a unix:// URL", got)
	}
	// The MCP socket grant from newIdentityService's fakeEndpoints must still be
	// there too: SandboxExtras adds to the jail, it does not replace a session's
	// own grants.
	if len(spec.AllowUnixSockets) != 2 {
		t.Fatalf("spec.AllowUnixSockets = %v, want both the MCP socket and Docker", spec.AllowUnixSockets)
	}
}

// A build that never wires SandboxExtras (nil, the default) must leave the
// jail exactly as restrictive as it was before this setting existed.
func TestCreateSessionWithNoSandboxExtrasConfiguredIsUnchanged(t *testing.T) {
	svc, mock, _ := newIdentityService(t)
	if _, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)}); err != nil {
		t.Fatal(err)
	}
	spec := mock.LastSpec()
	if len(spec.AllowUnixSockets) != 1 || len(spec.ReadOnlyPaths) != 1 {
		t.Fatalf("spec = %+v, want only the MCP endpoint's own grants", spec)
	}
	if len(spec.ExtraEnv) != 0 {
		t.Fatalf("spec.ExtraEnv = %v, want none", spec.ExtraEnv)
	}
}

func TestAgentCallerCreatesChildUnderItself(t *testing.T) {
	svc, _, _ := newIdentityService(t)
	root := projectDir(t)
	parent, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: root})
	if err != nil {
		t.Fatal(err)
	}
	child, err := svc.CreateSession(agentCtx(parent.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock})
	if err != nil {
		t.Fatal(err)
	}
	if child.ParentSessionID == nil || *child.ParentSessionID != parent.ID || child.Depth != 1 {
		t.Fatalf("child = %+v", child)
	}
	if child.ProjectPath != root {
		t.Fatalf("empty cwd must inherit the parent's project, got %q", child.ProjectPath)
	}
	// Lying about the parent is rejected, not silently corrected.
	_, err = svc.CreateSession(agentCtx(parent.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, ParentSessionID: child.ID})
	if !errors.Is(err, agenthub.ErrCallerMismatch) {
		t.Fatalf("got %v, want ErrCallerMismatch", err)
	}
}

func TestDepthIsEnforcedFromIdentity(t *testing.T) {
	svc, _, _ := newIdentityService(t)
	cur, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < agenthub.MaxDepth; i++ {
		next, err := svc.CreateSession(agentCtx(cur.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock})
		if err != nil {
			t.Fatalf("depth %d: %v", i+1, err)
		}
		cur = next
	}
	_, err = svc.CreateSession(agentCtx(cur.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock})
	if !errors.Is(err, agenthub.ErrDepthLimitExceeded) {
		t.Fatalf("got %v, want ErrDepthLimitExceeded (no callerSessionId needed to enforce)", err)
	}
}

func TestAgentCwdMustStayInsideParent(t *testing.T) {
	svc, _, _ := newIdentityService(t)
	root := projectDir(t)
	inside := filepath.Join(root, "svc-a")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	parent, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateSession(agentCtx(parent.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: inside}); err != nil {
		t.Fatalf("subpath of parent must be allowed: %v", err)
	}
	_, err = svc.CreateSession(agentCtx(parent.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)})
	if !errors.Is(err, agenthub.ErrCwdOutsideParent) {
		t.Fatalf("got %v, want ErrCwdOutsideParent", err)
	}
}

func TestRootAndHomeAreNeverProjects(t *testing.T) {
	svc, _, _ := newIdentityService(t)
	home, _ := os.UserHomeDir()
	for _, cwd := range []string{"/", home} {
		_, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: cwd})
		if !errors.Is(err, agenthub.ErrInvalidCwd) {
			t.Fatalf("cwd %q: got %v, want ErrInvalidCwd", cwd, err)
		}
	}
}

func TestAgentOnlyReachesItsSubtree(t *testing.T) {
	svc, _, _ := newIdentityService(t)
	a, _ := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)})
	b, _ := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)})
	aChild, _ := svc.CreateSession(agentCtx(a.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock})

	if _, err := svc.GetStatus(agentCtx(a.ID), aChild.ID); err != nil {
		t.Fatalf("own child must be visible: %v", err)
	}
	if _, err := svc.GetStatus(agentCtx(a.ID), b.ID); !errors.Is(err, agenthub.ErrNotDescendant) {
		t.Fatalf("sibling tree: got %v, want ErrNotDescendant", err)
	}
	if err := svc.Cancel(agentCtx(a.ID), b.ID); !errors.Is(err, agenthub.ErrNotDescendant) {
		t.Fatalf("cancel sibling tree: got %v, want ErrNotDescendant", err)
	}
	if _, err := svc.Invoke(agentCtx(a.ID), agenthub.InvokeInput{SessionID: b.ID, Prompt: "hi"}); !errors.Is(err, agenthub.ErrNotDescendant) {
		t.Fatalf("invoke sibling tree: got %v, want ErrNotDescendant", err)
	}
	list, err := svc.ListSessions(agentCtx(a.ID))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("agent list = %d sessions, want 2 (self + child)", len(list))
	}
	if all, _ := svc.ListSessions(uiCtx()); len(all) != 3 {
		t.Fatalf("ui list = %d, want 3", len(all))
	}
}

func TestOnSessionClosedHookFires(t *testing.T) {
	svc, _, _ := newIdentityService(t)
	var closed []string
	svc.OnSessionClosed = func(id string) { closed = append(closed, id) }
	s, _ := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)})
	if err := svc.CloseSession(uiCtx(), s.ID); err != nil {
		t.Fatal(err)
	}
	if len(closed) != 1 || closed[0] != s.ID {
		t.Fatalf("hook got %v", closed)
	}
}
