package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/internal/identity"
)

func environmentFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func TestEnvironmentInventoryDoesNotLeakConfigurationOrSkillBodies(t *testing.T) {
	home, cwd, bin := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	environmentFile(t, filepath.Join(bin, "gh"), "executable not run", 0o700)
	environmentFile(t, filepath.Join(bin, "glab"), "not executable", 0o600)
	environmentFile(t, filepath.Join(home, ".codex", "skills", "review", "SKILL.md"), "---\nname: review\ndescription: Review code\n---\nSKILL_BODY_SECRET", 0o600)
	environmentFile(t, filepath.Join(home, ".codex", "config.toml"), "[mcp_servers.docs]\ncommand = 'DO_NOT_EXECUTE'\n[mcp_servers.docs.env]\nTOKEN = 'CONFIG_SECRET'\n", 0o600)
	environmentFile(t, filepath.Join(cwd, ".agents", "skills", "broken", "SKILL.md"), "BROKEN_BODY_SECRET", 0o600)
	shared := false
	report, err := (environmentSnapshot{Agent: AgentTypeCodex, Spec: OpenSpec{Cwd: cwd, PrivateHome: &shared}, Env: map[string]string{"PATH": bin, "TOKEN": "ENV_SECRET"}, Command: "/bin/cat"}).report(context.Background(), "next")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(report)
	for _, secret := range []string{"SKILL_BODY_SECRET", "CONFIG_SECRET", "ENV_SECRET", "BROKEN_BODY_SECRET", "DO_NOT_EXECUTE"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("report leaked %s", secret)
		}
	}
	if entry := findEnvironment(t, report, "tools", "gh"); entry.Status != "found" {
		t.Fatalf("gh = %+v", entry)
	}
	if entry := findEnvironment(t, report, "tools", "glab"); entry.Status != "missing" {
		t.Fatalf("non-executable glab = %+v", entry)
	}
	if entry := findEnvironment(t, report, "skills", "review"); entry.Status != "configured" {
		t.Fatal("a file must not be advertised as loaded")
	}
	if entry := findEnvironment(t, report, "mcp", "docs"); entry.Status != "configured" {
		t.Fatal("a server config must not be advertised as connected")
	}
	if entry := findEnvironment(t, report, "skills", "broken"); entry.Status != "unverified" || entry.Action == "" {
		t.Fatal("invalid skill needs a repair hint")
	}
}

func findEnvironment(t *testing.T, report EnvironmentReport, kind, name string) EnvironmentEntry {
	t.Helper()
	for _, entry := range report.Entries {
		if entry.Kind == kind && entry.Name == name {
			return entry
		}
	}
	t.Fatalf("missing %s %s", kind, name)
	return EnvironmentEntry{}
}

func TestEnvironmentInspectionDoesNotStartRuntimeOrWriteItsHome(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	home := filepath.Join(t.TempDir(), "not-created")
	reg := NewRegistry()
	reg.Register(NewCodexAdapter(CodexOptions{Command: "/bin/cat", Home: home}))
	svc := NewService(reg, nil, nil)
	report, err := svc.Environment(context.Background(), AgentTypeCodex, "")
	if err != nil || report.Scope != "next" {
		t.Fatalf("report = %+v, %v", report, err)
	}
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("inspection created or modified the managed home")
	}
	ctx := identity.WithCaller(context.Background(), identity.Agent("agent"))
	if _, err := svc.Environment(ctx, AgentTypeCodex, ""); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("agent caller = %v", err)
	}
}

func TestEnvironmentSnapshotRetainsLaunchPermissionsAndPATH(t *testing.T) {
	shared := false
	paths := []string{"/granted"}
	env := map[string]string{"PATH": "/original"}
	snapshot := captureEnvironment(AgentTypeClaude, OpenSpec{PrivateHome: &shared, ExtraReadWritePaths: paths}, env, "/bin/cat", "")
	shared = true
	paths[0] = "/not-granted"
	env["PATH"] = "/changed"
	if *snapshot.Spec.PrivateHome || snapshot.Spec.ExtraReadWritePaths[0] != "/granted" || snapshot.Env["PATH"] != "/original" {
		t.Fatal("running-session report changed with later settings")
	}
}

func TestDevelopmentPathFindsHomeToolsWithoutRunningShellStartup(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".bun", "bin")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := developmentPath("/explicit/bin:/usr/bin", home)
	if !strings.HasPrefix(path, "/explicit/bin:/usr/bin") || !strings.Contains(path, dir) {
		t.Fatalf("PATH = %s", path)
	}
	if got := developmentPath(path, home); got != path {
		t.Fatal("PATH completion must not add duplicates")
	}
}

func TestManagedRuntimePATHKeepsDeveloperToolBins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".bun", "bin")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	shared := false
	spec := OpenSpec{PrivateHome: &shared}
	l := launch{path: "/bin/cat", env: map[string]string{"PATH": "/managed/node/bin:/usr/bin"}}
	codex := codexEnv(l, spec, "/managed/codex")
	claude, _ := sandboxFor(t.TempDir(), l, spec)
	for _, env := range []map[string]string{codex, claude.Env} {
		if !strings.HasPrefix(env["PATH"], "/managed/node/bin:/usr/bin") || !strings.Contains(env["PATH"], dir) {
			t.Fatal("managed PATH overwrote developer tool discovery")
		}
	}
}
