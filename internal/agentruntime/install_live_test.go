package agentruntime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCodexInstallsAndRunsForReal installs the pinned @openai/codex with the
// managed Node/npm and runs the resolved binary. It is the check the
// fake tree in manager_test.go cannot make: that npm really lays the package
// out where NativeEntry looks.
func TestCodexInstallsAndRunsForReal(t *testing.T) {
	if os.Getenv("DEVKIT_AGENT_INSTALL_TEST") != "1" {
		t.Skip("set DEVKIT_AGENT_INSTALL_TEST=1 to install @openai/codex for real (network, ~300MB)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	m := New(filepath.Join(t.TempDir(), "runtimes"))
	m.OnOutput = func(_, line string) { t.Log(line) }
	if err := m.Install(ctx, CodexCLI, ""); err != nil {
		t.Fatal(err)
	}
	rt, err := m.Resolve(CodexCLI)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("resolved %s", rt.Command)

	cmd := exec.CommandContext(ctx, rt.Command, "--version")
	cmd.Env = append(os.Environ(), "PATH="+rt.Env["PATH"])
	out, err := cmd.CombinedOutput()
	want := "codex-cli " + agents[CodexCLI].TestedVersion
	if err != nil || !strings.Contains(string(out), want) {
		t.Fatalf("%s --version = %q, %v; want %q", rt.Command, out, err, want)
	}
}

// This exercises the actual download, checksum, extraction, npm ci and binary
// launch with no Node/npm/vendor CLI on PATH. It never authenticates or calls a model.
func TestManagedRuntimesWithoutGlobalNode(t *testing.T) {
	if os.Getenv("DEVKIT_AGENT_INSTALL_TEST") != "1" {
		t.Skip("opt in to runtime downloads with DEVKIT_AGENT_INSTALL_TEST=1")
	}
	t.Setenv("PATH", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	m := New(filepath.Join(t.TempDir(), "runtimes"))
	m.OnOutput = func(agent, line string) { t.Log(agent, line) }
	for _, agent := range []string{CodexCLI, ClaudeACP} {
		t.Run(agent, func(t *testing.T) {
			if err := m.Install(ctx, agent, ""); err != nil {
				t.Fatal(err)
			}
			rt, err := m.ResolveLeased(agent)
			if err != nil {
				t.Fatal(err)
			}
			defer rt.Release()
			if agent == ClaudeACP && !strings.HasPrefix(rt.Command, resolved(t, m.root)+string(filepath.Separator)) {
				t.Fatal("Claude uses external Node")
			}
			args := []string{"--version"}
			command := rt.Command
			if agent == ClaudeACP {
				var mf installManifest
				if err := readJSON(filepath.Join(m.agentDir(agent), rt.Version, "install.json"), &mf); err != nil {
					t.Fatal(err)
				}
				command = filepath.Join(m.agentDir(agent), rt.Version, filepath.FromSlash(mf.BundledClaude))
			}
			out, err := exec.CommandContext(ctx, command, args...).CombinedOutput()
			if err != nil {
				t.Fatalf("binary: %v %s", err, out)
			}
			t.Log(string(out))
		})
	}
}
