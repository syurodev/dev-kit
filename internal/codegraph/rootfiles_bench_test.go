package codegraph

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// BenchmarkExplorerFolder is what the Files tab waits for per folder: the file
// listing and, in a repository, branch + change count. 10k files, 1% modified.
func BenchmarkExplorerFolder(b *testing.B) {
	root := b.TempDir()
	for i := 0; i < 10000; i++ {
		p := filepath.Join(root, fmt.Sprintf("src/pkg%d/file%d.go", i%50, i))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("package x\n"), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	hasGit := true
	if _, err := exec.LookPath("git"); err != nil {
		hasGit = false
	} else {
		for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "x"}} {
			if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
				b.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
		for i := 0; i < 100; i++ {
			_ = os.WriteFile(filepath.Join(root, fmt.Sprintf("src/pkg%d/file%d.go", i%50, i)), []byte("package y\n"), 0o644)
		}
	}
	b.Run("list", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, _, err := ListFilesAt(context.Background(), root); err != nil {
				b.Fatal(err)
			}
		}
	})
	if hasGit {
		b.Run("gitInfo", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				GitInfoAt(context.Background(), root)
			}
		})
		b.Run("gitStatus", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := GitStatusAt(context.Background(), root); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
