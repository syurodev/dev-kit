package codegraph

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

func writeTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSameRelativePathInTwoRootsIsTwoFiles(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	writeTestFile(t, a, "src/config.ts", "export const a = 1\n")
	writeTestFile(t, b, "src/config.ts", "export const b = 2\n")

	ra, err := ReadFileAt(a, "src/config.ts")
	if err != nil {
		t.Fatal(err)
	}
	rb, err := ReadFileAt(b, "src/config.ts")
	if err != nil {
		t.Fatal(err)
	}
	if ra.Revision == rb.Revision || ra.Content == rb.Content {
		t.Fatalf("the two roots' files must be independent: %+v %+v", ra, rb)
	}
	if _, err := WriteFileAt(a, "src/config.ts", "export const a = 10\n", ra.Revision); err != nil {
		t.Fatal(err)
	}
	after, _ := ReadFileAt(b, "src/config.ts")
	if after.Content != rb.Content || after.Revision != rb.Revision {
		t.Fatal("saving in root A changed root B")
	}
}

func TestWriteFileAtRefusesAnotherRevision(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "main.go", "package main\n")
	read, err := ReadFileAt(root, "main.go")
	if err != nil {
		t.Fatal(err)
	}
	// Someone else edits the file after the buffer was loaded.
	writeTestFile(t, root, "main.go", "package other\n")

	_, err = WriteFileAt(root, "main.go", "package mine\n", read.Revision)
	if !errors.Is(err, ErrFileConflict) {
		t.Fatalf("err = %v, want ErrFileConflict", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "main.go"))
	if string(got) != "package other\n" {
		t.Fatalf("a refused save changed the file: %q", got)
	}

	// Overwriting knowingly is possible, and only by saying so.
	saved, err := WriteFileAt(root, "main.go", "package mine\n", AnyRevision)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Revision != RevisionOf([]byte("package mine\n")) {
		t.Fatalf("revision = %s", saved.Revision)
	}
}

func TestWriteFileAtNewFileAndDeletedFile(t *testing.T) {
	root := t.TempDir()
	if _, err := WriteFileAt(root, "new/dir/a.txt", "hello", AbsentRevision); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Creating again conflicts: the file is there now.
	if _, err := WriteFileAt(root, "new/dir/a.txt", "again", AbsentRevision); !errors.Is(err, ErrFileConflict) {
		t.Fatalf("second create err = %v, want conflict", err)
	}
	// A file deleted under a buffer is a conflict, not a silent re-create.
	read, _ := ReadFileAt(root, "new/dir/a.txt")
	if err := os.Remove(filepath.Join(root, "new/dir/a.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteFileAt(root, "new/dir/a.txt", "back", read.Revision); !errors.Is(err, ErrFileConflict) {
		t.Fatalf("save after delete err = %v, want conflict", err)
	}
	if _, err := os.Stat(filepath.Join(root, "new/dir/a.txt")); err == nil {
		t.Fatal("a refused save re-created the file")
	}
	// No revision named is the strict reading: it believes the file is new.
	if _, err := WriteFileAt(root, "new/dir/a.txt", "x", ""); err != nil {
		t.Fatalf("empty revision on a missing file: %v", err)
	}
}

func TestWriteFileAtSerialisesConcurrentSavesOfOneRevision(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "f.txt", "base")
	read, _ := ReadFileAt(root, "f.txt")

	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := WriteFileAt(root, "f.txt", "writer"+string(rune('a'+i)), read.Revision)
			if err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			} else if !errors.Is(err, ErrFileConflict) {
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d saves based on the same revision succeeded, want exactly 1", wins)
	}
}

func TestWriteFileAtKeepsPermissionsAndRefusesSymlinkEscape(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	writeTestFile(t, root, "run.sh", "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(root, "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	read, _ := ReadFileAt(root, "run.sh")
	if _, err := WriteFileAt(root, "run.sh", "#!/bin/sh\necho hi\n", read.Revision); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(root, "run.sh"))
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v, want 0755", info.Mode().Perm())
	}

	writeTestFile(t, outside, "secret.txt", "s")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, err := WriteFileAt(root, "link.txt", "x", AnyRevision); !errors.Is(err, ErrPathOutsideProject) {
		t.Fatalf("write through escaping symlink err = %v", err)
	}
	if _, err := ReadFileAt(root, "link.txt"); !errors.Is(err, ErrPathOutsideProject) {
		t.Fatalf("read through escaping symlink err = %v", err)
	}
}

func TestStatFileAtReportsDeletedBinaryAndRevision(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a.txt", "text")
	if err := os.WriteFile(filepath.Join(root, "b.bin"), []byte{0xff, 0xfe, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := StatFileAt(root, "a.txt")
	if err != nil || !st.Exists || st.Revision != RevisionOf([]byte("text")) || st.Unreadable != "" {
		t.Fatalf("stat text = %+v, %v", st, err)
	}
	bin, _ := StatFileAt(root, "b.bin")
	if !bin.Exists || bin.Unreadable != "binary" || bin.Revision == "" {
		t.Fatalf("stat binary = %+v", bin)
	}
	gone, err := StatFileAt(root, "missing.txt")
	if err != nil || gone.Exists {
		t.Fatalf("stat missing = %+v, %v", gone, err)
	}
	if _, err := ReadFileAt(root, "b.bin"); !errors.Is(err, ErrBinaryFile) {
		t.Fatalf("read binary err = %v", err)
	}
}

func TestListFilesAtSkipsIgnoredAndIsSorted(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "b.go", "")
	writeTestFile(t, root, "a/z.go", "")
	writeTestFile(t, root, "node_modules/x/index.js", "")
	writeTestFile(t, root, ".git/HEAD", "")

	entries, truncated, err := ListFilesAt(context.Background(), root)
	if err != nil || truncated {
		t.Fatalf("list = %v, truncated %v", err, truncated)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Path)
	}
	want := []string{"a", "a/z.go", "b.go"}
	if len(got) != len(want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entries = %v, want %v", got, want)
		}
	}
}

func TestGitInfoAtAndStatus(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "trunk")
	writeTestFile(t, root, "a.txt", "1")
	run("add", ".")
	run("commit", "-q", "-m", "init")
	writeTestFile(t, root, "a.txt", "2")
	writeTestFile(t, root, "new.txt", "n")

	info := GitInfoAt(context.Background(), root)
	if !info.IsRepo || info.Branch != "trunk" || info.Changed != 2 {
		t.Fatalf("info = %+v", info)
	}
	status, _ := GitStatusAt(context.Background(), root)
	if status["a.txt"] != "M" || status["new.txt"] != "A" {
		t.Fatalf("status = %v", status)
	}
	plain := GitInfoAt(context.Background(), t.TempDir())
	if plain.IsRepo || plain.Branch != "" {
		t.Fatalf("a plain folder is not a repo: %+v", plain)
	}
}
