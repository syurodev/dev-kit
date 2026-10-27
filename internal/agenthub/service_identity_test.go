package agenthub_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"devkit/internal/acp"
	"devkit/internal/agenthub"
	"devkit/internal/identity"
	"devkit/internal/preferences"
	"devkit/internal/storage"
)

type fakeEndpoints struct{ opened, closed, gateOwnedTools []string }

func (f *fakeEndpoints) Open(sessionID string) (agenthub.MCPEndpoint, error) {
	f.opened = append(f.opened, sessionID)
	return agenthub.MCPEndpoint{
		GateOwnedTools: f.gateOwnedTools,
		SocketPath:     "/tmp/" + sessionID[:8] + ".sock",
		Server:         acp.McpServerStdio{Name: "devkit", Command: "/opt/devkit-mcp", Args: []string{"--socket", "/tmp/x.sock"}, Env: []acp.EnvVariable{}},
		Close:          func() { f.closed = append(f.closed, sessionID) },
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
	eps.gateOwnedTools = []string{"db.query"}
	sess, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(eps.opened) != 1 || eps.opened[0] != sess.ID {
		t.Fatalf("endpoint opened for %v, want [%s]", eps.opened, sess.ID)
	}
	spec := mock.LastSpec()
	if !slices.Equal(spec.GateOwnedMCP["devkit"], eps.gateOwnedTools) {
		t.Fatal("endpoint ownership did not reach the runtime")
	}
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

// SandboxExtras shapes every session's OpenSpec, on top of whatever the MCP
// endpoint already grants (docs/dev-kit/sandbox.mdx), not in place of it.
func TestCreateSessionAppliesSandboxExtras(t *testing.T) {
	svc, mock, _ := newIdentityService(t)
	svc.DetectDocker = func() agenthub.DockerGrant { return agenthub.DockerGrant{} }
	svc.SandboxExtras = func() preferences.AgentSandboxSettings {
		return preferences.AgentSandboxSettings{
			DockerSocketPath:   "/nonexistent/docker.sock",
			ExtraReadOnlyPaths: []string{"/opt/shared-lib"},
		}
	}
	if _, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)}); err != nil {
		t.Fatal(err)
	}
	spec := mock.LastSpec()
	if !slices.Contains(spec.AllowUnixSockets, "/nonexistent/docker.sock") {
		t.Fatalf("spec.AllowUnixSockets = %v, want the Docker socket included", spec.AllowUnixSockets)
	}
	if !slices.Contains(spec.ReadOnlyPaths, "/opt/shared-lib") {
		t.Fatalf("spec.ReadOnlyPaths = %v, want the extra path included", spec.ReadOnlyPaths)
	}
	if got := spec.ExtraEnv["DOCKER_HOST"]; got != "unix:///nonexistent/docker.sock" {
		t.Fatalf("spec.ExtraEnv[DOCKER_HOST] = %q, want the configured socket as a unix:// URL", got)
	}
	// The MCP socket grant from newIdentityService's fakeEndpoints must still be
	// there too: SandboxExtras adds to the jail, it does not replace a session's
	// own grants.
	if len(spec.AllowUnixSockets) != 2 {
		t.Fatalf("spec.AllowUnixSockets = %v, want both the MCP socket and Docker", spec.AllowUnixSockets)
	}
	// Home is shared by default.
	if spec.PrivateHome == nil || *spec.PrivateHome {
		t.Fatalf("spec.PrivateHome = %v, want a pointer to false (ShareHome defaults on)", spec.PrivateHome)
	}
}

// With nothing configured, Docker is found and granted — socket, the CLI's
// app bundle, and its directory on PATH.
func TestCreateSessionGrantsDetectedDockerByDefault(t *testing.T) {
	svc, mock, _ := newIdentityService(t)
	svc.DetectDocker = func() agenthub.DockerGrant {
		return agenthub.DockerGrant{
			Socket:   "/Users/a/.orbstack/run/docker.sock",
			ReadOnly: []string{"/Applications/OrbStack.app"},
			PathDirs: []string{"/definitely/not/on/path"},
		}
	}
	svc.SandboxExtras = func() preferences.AgentSandboxSettings { return preferences.AgentSandboxSettings{} }
	if _, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)}); err != nil {
		t.Fatal(err)
	}
	spec := mock.LastSpec()
	if !slices.Contains(spec.AllowUnixSockets, "/Users/a/.orbstack/run/docker.sock") {
		t.Fatalf("spec.AllowUnixSockets = %v, want the detected socket", spec.AllowUnixSockets)
	}
	if got := spec.ExtraEnv["DOCKER_HOST"]; got != "unix:///Users/a/.orbstack/run/docker.sock" {
		t.Fatalf("DOCKER_HOST = %q", got)
	}
	if !slices.Contains(spec.ReadOnlyPaths, "/Applications/OrbStack.app") {
		t.Fatalf("spec.ReadOnlyPaths = %v, want the CLI's app bundle", spec.ReadOnlyPaths)
	}
	if !strings.HasSuffix(spec.ExtraEnv["PATH"], "/definitely/not/on/path") {
		t.Fatalf("PATH = %q, want the CLI's directory appended", spec.ExtraEnv["PATH"])
	}
}

// Both switches off: no Docker at all, and a private home.
func TestCreateSessionHonoursSandboxOptOuts(t *testing.T) {
	svc, mock, _ := newIdentityService(t)
	svc.DetectDocker = func() agenthub.DockerGrant {
		return agenthub.DockerGrant{Socket: "/var/run/docker.sock"}
	}
	svc.SandboxExtras = preferences.StrictAgentSandbox
	if _, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)}); err != nil {
		t.Fatal(err)
	}
	spec := mock.LastSpec()
	if len(spec.AllowUnixSockets) != 1 || spec.ExtraEnv["DOCKER_HOST"] != "" {
		t.Fatalf("spec = %+v, want no Docker grant", spec)
	}
	if spec.PrivateHome != nil {
		t.Fatalf("spec.PrivateHome = %v, want nil (the adapter's private default)", spec.PrivateHome)
	}
}

// DevKit's data directory is hidden from every session, shared home or not.
func TestCreateSessionHidesProtectedPaths(t *testing.T) {
	svc, mock, _ := newIdentityService(t)
	svc.ProtectedPaths = func() []string { return []string{"/home/a/.config/DevKit/devkit.db"} }
	if _, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)}); err != nil {
		t.Fatal(err)
	}
	if spec := mock.LastSpec(); !slices.Contains(spec.DenyReadPaths, "/home/a/.config/DevKit/devkit.db") {
		t.Fatalf("spec.DenyReadPaths = %v", spec.DenyReadPaths)
	}
}

// A build that never wires SandboxExtras (nil — tests) keeps the adapter's
// own jail: no Docker, private home.
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

func TestWorktreeInUseSeesOtherRootsAndResolvesSymlinkCwds(t *testing.T) {
	for _, tc := range []struct {
		name  string
		alias bool
	}{
		{name: "nested cwd"},
		{name: "symlink alias", alias: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _ := newIdentityService(t)
			worktree := filepath.Join(t.TempDir(), "checkout")
			if err := os.MkdirAll(filepath.Join(worktree, "nested"), 0o700); err != nil {
				t.Fatal(err)
			}
			cwd := filepath.Join(worktree, "nested")
			if tc.alias {
				alias := filepath.Join(t.TempDir(), "checkout-alias")
				if err := os.Symlink(worktree, alias); err != nil {
					t.Fatal(err)
				}
				cwd = filepath.Join(alias, "nested")
			}
			caller, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)})
			if err != nil {
				t.Fatal(err)
			}
			// This root session is not visible through caller's agent-scoped
			// ListSessions, but its cwd must still protect the checkout.
			other, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: cwd})
			if err != nil {
				t.Fatal(err)
			}
			if visible, err := svc.ListSessions(agentCtx(caller.ID)); err != nil || len(visible) != 1 {
				t.Fatalf("caller-scoped list = %d, %v; want only caller", len(visible), err)
			}
			inUse, err := svc.WorktreeInUse(agentCtx(caller.ID), worktree)
			if err != nil || !inUse {
				t.Fatalf("WorktreeInUse = %v, %v; want true for other root %s", inUse, err, other.ID)
			}
			if err := svc.CloseSession(uiCtx(), other.ID); err != nil {
				t.Fatal(err)
			}
			inUse, err = svc.WorktreeInUse(agentCtx(caller.ID), worktree)
			if err != nil || inUse {
				t.Fatalf("WorktreeInUse after close = %v, %v; want false", inUse, err)
			}
		})
	}
}

func TestWorktreeInUseHandlesMissingSessionDirectories(t *testing.T) {
	for _, tc := range []struct {
		name          string
		inside, alias bool
	}{
		{name: "unrelated deleted cwd"},
		{name: "deleted subdirectory inside checkout", inside: true},
		{name: "deleted subdirectory through existing alias", inside: true, alias: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _ := newIdentityService(t)
			worktree := projectDir(t)
			root := projectDir(t)
			if tc.inside {
				root = worktree
			}
			if tc.alias {
				link := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(root, link); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				root = link
			}
			cwd := filepath.Join(root, "nested", "gone")
			if err := os.MkdirAll(cwd, 0700); err != nil {
				t.Fatal(err)
			}
			sess, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: cwd})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.RemoveAll(filepath.Join(root, "nested")); err != nil {
				t.Fatal(err)
			}
			inUse, err := svc.WorktreeInUse(context.Background(), worktree)
			if err != nil || inUse != tc.inside {
				t.Fatalf("in use=%v err=%v; want %v", inUse, err, tc.inside)
			}
			if err := svc.CloseSession(uiCtx(), sess.ID); err != nil {
				t.Fatal(err)
			}
			if inUse, err := svc.WorktreeInUse(context.Background(), worktree); err != nil || inUse {
				t.Fatalf("closed session: in use=%v err=%v", inUse, err)
			}
		})
	}
}

func TestWorktreeInUseFailsClosedOnUnresolvableSessionAliases(t *testing.T) {
	for _, cycle := range []bool{false, true} {
		name := "dangling alias"
		if cycle {
			name = "symlink cycle"
		}
		t.Run(name, func(t *testing.T) {
			svc, _, _ := newIdentityService(t)
			worktree := projectDir(t)
			target := filepath.Join(worktree, "nested")
			if err := os.Mkdir(target, 0700); err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(t.TempDir(), "alias")
			if err := os.Symlink(target, alias); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if _, err := svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: alias}); err != nil {
				t.Fatal(err)
			}
			if cycle {
				if err := os.Remove(alias); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(alias, alias); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Remove(target); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.WorktreeInUse(context.Background(), worktree); err == nil {
				t.Fatal("an alias that cannot be resolved must not authorize removal")
			}
		})
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
