// internal/codegraph/paths_test.go
package codegraph

import (
	"errors"
	"os"
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

func TestResolvePathAllowsFileInsideRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	got, err := resolvePath(root, "a.txt")
	if err != nil {
		t.Fatalf("resolvePath: %v", err)
	}
	want, _ := filepath.EvalSymlinks(filepath.Join(root, "a.txt"))
	if got != want {
		t.Fatalf("resolvePath = %q, want %q", got, want)
	}
}

func TestResolvePathAllowsNestedNewFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// "new.txt" does not exist yet — WriteFile's first save of a new file
	// must still resolve, since EvalSymlinks fails on a nonexistent path.
	got, err := resolvePath(root, "src/new.txt")
	if err != nil {
		t.Fatalf("resolvePath: %v", err)
	}
	wantDir, _ := filepath.EvalSymlinks(filepath.Join(root, "src"))
	if filepath.Dir(got) != wantDir || filepath.Base(got) != "new.txt" {
		t.Fatalf("resolvePath = %q, want dir %q base new.txt", got, wantDir)
	}
}

func TestResolvePathRejectsDotDotTraversal(t *testing.T) {
	root := t.TempDir()
	if _, err := resolvePath(root, "../../etc/passwd"); !errors.Is(err, ErrPathOutsideProject) {
		t.Fatalf("resolvePath = %v, want ErrPathOutsideProject", err)
	}
}

func TestResolvePathRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := resolvePath(root, "link.txt"); !errors.Is(err, ErrPathOutsideProject) {
		t.Fatalf("resolvePath = %v, want ErrPathOutsideProject", err)
	}
}

func TestResolvePathRejectsAbsolutePathEscape(t *testing.T) {
	root := t.TempDir()
	if _, err := resolvePath(root, "/etc/passwd"); !errors.Is(err, ErrPathOutsideProject) {
		t.Fatalf("resolvePath = %v, want ErrPathOutsideProject", err)
	}
}

func TestResolvePathRejectsDanglingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	// link.txt points to outside/nonexistent.txt which does not exist
	if err := os.Symlink(filepath.Join(outside, "nonexistent.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := resolvePath(root, "link.txt"); !errors.Is(err, ErrPathOutsideProject) {
		t.Fatalf("resolvePath = %v, want ErrPathOutsideProject", err)
	}
}

func TestResolvePathRejectsDanglingDirectorySymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	// dangling_dir points to outside/nonexistent_dir
	if err := os.Symlink(filepath.Join(outside, "nonexistent_dir"), filepath.Join(root, "dangling_dir")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := resolvePath(root, "dangling_dir/file.txt"); !errors.Is(err, ErrPathOutsideProject) {
		t.Fatalf("resolvePath = %v, want ErrPathOutsideProject", err)
	}
}
