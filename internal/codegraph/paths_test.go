// internal/codegraph/paths_test.go
package codegraph

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectDBPathIsStableForSamePath(t *testing.T) {
	a := ProjectDBPath("/home/user/.config", "/Users/dev/my-project")
	b := ProjectDBPath("/home/user/.config", "/Users/dev/my-project")
	if a != b {
		t.Fatalf("ProjectDBPath not stable: %q != %q", a, b)
	}
}

func TestProjectDBPathDiffersForDifferentProjects(t *testing.T) {
	a := ProjectDBPath("/home/user/.config", "/Users/dev/project-a")
	b := ProjectDBPath("/home/user/.config", "/Users/dev/project-b")
	if a == b {
		t.Fatalf("ProjectDBPath collided for different projects: %q", a)
	}
}

func TestProjectDBPathUnderConfigDevKitCodegraph(t *testing.T) {
	got := ProjectDBPath("/home/user/.config", "/Users/dev/my-project")
	want := filepath.Join("/home/user/.config", "DevKit", "codegraph")
	if !strings.HasPrefix(got, want+string(filepath.Separator)) {
		t.Fatalf("ProjectDBPath = %q, want prefix %q", got, want)
	}
	if !strings.HasSuffix(got, ".db") {
		t.Fatalf("ProjectDBPath = %q, want .db suffix", got)
	}
}
