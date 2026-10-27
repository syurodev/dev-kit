package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

// A link inside a granted folder is only inside it if its target is. These are the
// containment decisions gitFor makes, so a repository outside the workspace cannot
// be reached through a name that sits under a granted folder.
func TestScopeContainmentFollowsLinks(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(outside, "deep"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"out": outside, "in": filepath.Join(root, "pkg")} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	scope := gitScope{granted: []string{root}, folders: []WorkspaceFolder{{Name: "ws", Path: root}}}
	for name, tc := range map[string]struct {
		path string
		in   bool
	}{
		"the folder itself":                {root, true},
		"a plain subfolder":                {filepath.Join(root, "pkg"), true},
		"a link to a subfolder":            {filepath.Join(root, "in"), true},
		"a link out":                       {filepath.Join(root, "out"), false},
		"inside a link out":                {filepath.Join(root, "out", "deep"), false},
		"not yet existing inside a link":   {filepath.Join(root, "out", "new", "x"), false},
		"not yet existing inside a folder": {filepath.Join(root, "new", "x"), true},
		"a sibling sharing the prefix":     {root + "-v2", false},
		"an unrelated folder":              {outside, false},
	} {
		if got := scope.covers(tc.path); got != tc.in {
			t.Errorf("%s: covers(%s) = %v, want %v", name, tc.path, got, tc.in)
		}
		if got := scope.folderOf(tc.path) != ""; got != tc.in {
			t.Errorf("%s: folderOf(%s) found a folder = %v, want %v", name, tc.path, got, tc.in)
		}
	}
}
