//go:build linux

package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

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
