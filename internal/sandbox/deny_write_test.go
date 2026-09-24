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

// A deny-write path the policy cannot enforce the same way on both backends
// is refused rather than half-applied: bwrap skips what does not exist (the
// agent could then create it), a symlink or an unclean spelling slips past
// Seatbelt's path match, and a writable grant inside one is writable on bwrap
// but not on Seatbelt.
func TestValidateRefusesDenyWritePathsTheBackendsWouldDisagreeOn(t *testing.T) {
	root := t.TempDir()
	rules := filepath.Join(root, "codex", "rules")
	for _, dir := range []string{rules, filepath.Join(root, "proj")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(root, "codex", "linked")
	if err := os.Symlink(rules, link); err != nil {
		t.Fatal(err)
	}
	base := Policy{WorkDir: filepath.Join(root, "proj")}
	cases := map[string]Policy{
		"missing":      {DenyWritePaths: []string{filepath.Join(root, "codex", "AGENTS.override.md")}},
		"symlink":      {DenyWritePaths: []string{link}},
		"unclean":      {DenyWritePaths: []string{filepath.Join(root, "codex") + "/skills/../rules"}},
		"grant inside": {DenyWritePaths: []string{rules}, ReadWritePaths: []string{filepath.Join(rules, "sub")}},
		"workdir same": {DenyWritePaths: []string{rules}, WorkDir: rules},
	}
	for name, p := range cases {
		if p.WorkDir == "" {
			p.WorkDir = base.WorkDir
		}
		if err := p.Validate(); !errors.Is(err, ErrInvalidPolicy) {
			t.Errorf("%s: Validate = %v, want ErrInvalidPolicy", name, err)
		}
	}
	ok := Policy{WorkDir: base.WorkDir, ReadWritePaths: []string{filepath.Join(root, "codex")}, DenyWritePaths: []string{rules}}
	if err := ok.Validate(); err != nil {
		t.Fatalf("a deny-write leaf inside a RW grant: Validate = %v, want nil", err)
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

// codexHomeLayout builds a CODEX_HOME-shaped tree several directories below
// a fake home, the way Agent Hub lays it out under DevKit's data directory,
// and returns the policy a session would get for it.
func codexHomeLayout(t *testing.T, privateHome bool) (Policy, map[string]string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	codex := filepath.Join(home, ".config", "DevKit", "agent-homes", "codex")
	paths := map[string]string{
		"root":   root,
		"home":   home,
		"codex":  codex,
		"rules":  filepath.Join(codex, "rules"),
		"config": filepath.Join(codex, "config.toml"),
		"agents": filepath.Join(codex, "AGENTS.md"),
	}
	for _, dir := range []string{paths["rules"], filepath.Join(root, "proj")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{paths["config"], paths["agents"]} {
		if err := os.WriteFile(file, []byte("# devkit\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return Policy{
		WorkDir:        filepath.Join(root, "proj"),
		ReadWritePaths: []string{codex},
		DenyWritePaths: []string{paths["rules"], paths["config"], paths["agents"]},
		Env:            map[string]string{"HOME": home},
		PrivateHome:    boolPtr(privateHome),
	}, paths
}

func runJailedShell(t *testing.T, runner *Runner, p Policy, script string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	proc, err := runner.Start(ctx, p, Cmd{Path: "/bin/sh", Args: []string{"-c", script}})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	return proc.Wait()
}

// TestDenyWriteHoldsAtRuntime runs the ways an agent would try to get a
// writable policy file past the real backend — bwrap on Linux, sandbox-exec
// on macOS. Renaming a directory above the protected path is the one that
// matters: the protected mount or rule moves with it, and the agent builds a
// fresh, writable tree at the old path. Every ancestor is tried, from the
// directory that holds the whole layout down to CODEX_HOME itself, each on a
// fresh layout so one that gets through cannot hide the next.
func TestDenyWriteHoldsAtRuntime(t *testing.T) {
	runner := DefaultRunner()
	if err := runner.Available(context.Background()); err != nil {
		t.Skipf("sandbox backend unavailable: %v", err)
	}
	for _, private := range []bool{false, true} {
		t.Run(map[bool]string{false: "shared-home", true: "private-home"}[private], func(t *testing.T) {
			p, paths := codexHomeLayout(t, private)
			if err := runJailedShell(t, runner, p, "printf ok > "+filepath.Join(paths["codex"], "auth.json")+" && cat "+paths["config"]+" >/dev/null"); err != nil {
				t.Fatalf("CODEX_HOME must stay writable and config.toml readable: %v", err)
			}
			attacks := map[string]func(map[string]string) string{
				"append config.toml": func(m map[string]string) string { return "printf x >> " + m["config"] },
				"replace config.toml": func(m map[string]string) string {
					return "printf x > " + m["codex"] + "/c.new && mv -f " + m["codex"] + "/c.new " + m["config"]
				},
				"write a rule":    func(m map[string]string) string { return "printf x > " + filepath.Join(m["rules"], "default.rules") },
				"write AGENTS.md": func(m map[string]string) string { return "printf x >> " + m["agents"] },
				"rename rules":    func(m map[string]string) string { return "mv " + m["rules"] + " " + m["rules"] + ".old" },
			}
			rel, _ := filepath.Rel(paths["root"], paths["codex"])
			for up := rel; ; up = filepath.Dir(up) {
				attacks["rename "+up] = func(m map[string]string) string {
					dir := filepath.Join(m["root"], up)
					return "mv " + dir + " " + dir + ".x"
				}
				if up == "." {
					break
				}
			}
			for name, attack := range attacks {
				t.Run(name, func(t *testing.T) {
					p, paths := codexHomeLayout(t, private)
					if err := runJailedShell(t, runner, p, attack(paths)+" 2>/dev/null"); err == nil {
						t.Errorf("succeeded inside the jail, want it refused")
					}
					if got, _ := os.ReadFile(paths["config"]); string(got) != "# devkit\n" {
						t.Errorf("config.toml = %q on the host, want it untouched", got)
					}
					if _, err := os.Stat(filepath.Join(paths["rules"], "default.rules")); err == nil {
						t.Error("a rule file reached the host")
					}
				})
			}
		})
	}
}
