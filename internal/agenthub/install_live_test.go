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
// machine's own node/npm and runs the resolved binary. It is the check the
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
