package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateRejectsRelativeDenyWritePath(t *testing.T) {
	err := Policy{WorkDir: "/tmp/proj", DenyWritePaths: []string{"rules"}}.Validate()
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("Validate = %v, want ErrInvalidPolicy", err)
	}
}

func TestBwrapArgsDenyWriteLandsOnTopOfReadWriteParent(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "codex-home")
	rules := filepath.Join(home, "rules")
	if err := os.MkdirAll(rules, 0o755); err != nil {
		t.Fatal(err)
	}
	args := buildBwrapArgs(Policy{
		WorkDir:        filepath.Join(root, "proj"),
		ReadWritePaths: []string{home},
		DenyWritePaths: []string{rules},
		Env:            map[string]string{"HOME": filepath.Join(root, "home")},
		PrivateHome:    boolPtr(false),
	}, Cmd{Path: "/bin/sh"})
	rw, ro := bindIndex(args, "--bind", home), bindIndex(args, "--ro-bind", rules)
	if rw < 0 || ro < 0 {
		t.Fatalf("args %q lack the RW home bind or the RO rules bind", args)
	}
	if ro < rw {
		t.Fatalf("--ro-bind of rules at %d comes before the RW home at %d: rules would end up writable", ro, rw)
	}
}

func TestBwrapDenyWriteIsReadOnlyAtRuntime(t *testing.T) {
	backend := NewBwrapBackend()
	if err := backend.Available(context.Background()); err != nil {
		t.Skipf("bwrap unavailable: %v", err)
	}
	root := t.TempDir()
	home := filepath.Join(root, "codex-home")
	rules := filepath.Join(home, "rules")
	config := filepath.Join(home, "config.toml")
	for _, dir := range []string{rules, filepath.Join(root, "proj")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(config, []byte("# devkit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	script := strings.Join([]string{
		"printf ok > " + filepath.Join(home, "auth.json"),
		"! printf x > " + filepath.Join(rules, "default.rules") + " 2>/dev/null",
		"! printf x >> " + config + " 2>/dev/null",
		"! mv " + rules + " " + rules + ".old 2>/dev/null",
		"cat " + config + " >/dev/null",
	}, " && ")
	proc, err := backend.Start(ctx, Policy{
		WorkDir:        filepath.Join(root, "proj"),
		ReadWritePaths: []string{home},
		DenyWritePaths: []string{rules, config},
		Env:            map[string]string{"HOME": filepath.Join(root, "home")},
		PrivateHome:    boolPtr(false),
	}, Cmd{Path: "/bin/sh", Args: []string{"-c", script}})
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Wait(); err != nil {
		t.Fatalf("sandboxed shell: %v (home writable; rules, config.toml and the rules rename must not be)", err)
	}
	if got, _ := os.ReadFile(filepath.Join(home, "auth.json")); string(got) != "ok" {
		t.Fatalf("auth.json = %q, want the RW home to stay writable", got)
	}
	if _, err := os.Stat(filepath.Join(rules, "default.rules")); err == nil {
		t.Fatal("a rule file was written under a deny-write path")
	}
	if got, _ := os.ReadFile(config); string(got) != "# devkit\n" {
		t.Fatalf("config.toml = %q, want it untouched", got)
	}
}

func TestSeatbeltProfileDenyWritePaths(t *testing.T) {
	prof := buildSeatbeltProfile(Policy{
		WorkDir:        "/tmp/proj",
		ReadWritePaths: []string{"/Users/alice/Library/Application Support/DevKit/agent-homes/codex"},
		DenyWritePaths: []string{"/Users/alice/Library/Application Support/DevKit/agent-homes/codex/rules"},
		Env:            map[string]string{"HOME": "/Users/alice"},
	}.withDefaults())
	block := "(deny file-write*\n  (subpath \"/Users/alice/Library/Application Support/DevKit/agent-homes/codex/rules\")"
	at := strings.Index(prof, block)
	if at < 0 {
		t.Fatalf("expected a deny-write block for rules: %s", prof)
	}
	if strings.LastIndex(prof, "(allow ") > at {
		t.Fatalf("deny-write must come after every allow (the later rule wins): %s", prof)
	}
	want := `(deny file-write-unlink (require-all (vnode-type DIRECTORY) (literal "/Users/alice/Library/Application Support/DevKit/agent-homes/codex")))`
	if !strings.Contains(prof, want) {
		t.Fatalf("the deny-write path's parent must not be renamable: %s", prof)
	}
}
