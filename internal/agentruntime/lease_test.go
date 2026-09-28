package agentruntime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLeasedRuntimeSurvivesActivationRemovalAndReinstall(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()
	if err := m.Install(ctx, ClaudeACP, "0.76.0"); err != nil {
		t.Fatal(err)
	}
	// Lease is obtained BEFORE spawn, with no caller-side acquire gap.
	rt, err := m.ResolveLeased(ClaudeACP)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Release()
	if err := m.Install(ctx, ClaudeACP, "0.78.0"); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove(ClaudeACP, "0.76.0"); !errors.Is(err, ErrVersionInUse) {
		t.Fatal(err)
	}
	if err := m.Install(ctx, ClaudeACP, "0.76.0"); !errors.Is(err, ErrVersionInUse) {
		t.Fatalf("reinstall: %v", err)
	}
	if _, err := os.Stat(rt.Args[0]); err != nil {
		t.Fatalf("leased entry disappeared: %v", err)
	}
	if m.current(t).Version != "0.78.0" {
		t.Fatal("failed reinstall changed current")
	}
	if len(leftovers(t, m.agentDir(ClaudeACP))) != 0 {
		t.Fatal("staging leaked")
	}
	rt.Release()
	if err := m.Install(ctx, ClaudeACP, "0.76.0"); err != nil {
		t.Fatal(err)
	}
}

func TestFailedResolveReleasesLease(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()
	if err := m.Install(ctx, ClaudeACP, "0.76.0"); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(m.agentDir(ClaudeACP), "0.76.0", filepath.FromSlash(agents[ClaudeACP].Entry)))
	if _, err := m.ResolveLeased(ClaudeACP); err == nil {
		t.Fatal("missing entry resolved")
	}
	if err := m.Install(ctx, ClaudeACP, "0.78.0"); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove(ClaudeACP, "0.76.0"); err != nil {
		t.Fatalf("failed resolution retained lease: %v", err)
	}
}

func TestTestedLockIsStagedBeforeNPM(t *testing.T) {
	m := newTestManager(t)
	old := m.runNPM
	m.runNPM = func(ctx context.Context, exe string, args, env []string, line func(string)) error {
		if args[0] != "--version" {
			if args[0] != "ci" {
				t.Fatalf("tested install must use ci: %v", args)
			}
			var lock struct {
				Packages map[string]struct{ Version, Integrity string }
			}
			if err := readJSON(filepath.Join(args[2], "package-lock.json"), &lock); err != nil {
				t.Fatal(err)
			}
			if lock.Packages["node_modules/@anthropic-ai/claude-agent-sdk"].Version != "0.3.270" {
				t.Fatal("SDK not pinned")
			}
			for name, p := range lock.Packages {
				if name != "" && p.Integrity == "" {
					t.Fatalf("no integrity: %s", name)
				}
			}
		}
		return old(ctx, exe, args, env, line)
	}
	if err := m.Install(context.Background(), ClaudeACP, ""); err != nil {
		t.Fatal(err)
	}
}
