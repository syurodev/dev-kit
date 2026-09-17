package checkpoint

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeScript puts an executable shell script on disk and returns its path.
func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script stubs need a POSIX shell")
	}
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// The macOS stub at /usr/bin/git is not git: with no Command Line Tools behind
// it, running it opens an installer dialog. A user who opened an agent session
// must not get that dialog, so the stub is judged by what is behind it and
// never executed.
func TestShimIsNeverRunWithoutCommandLineTools(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "it-ran")
	shim := writeScript(t, dir, "git", "touch "+marker+"\necho 'git version 2.50.0'\n")

	t.Cleanup(swap(&gitGOOS, "darwin"))
	t.Cleanup(swap(&gitShimPath, shim))
	t.Cleanup(swapSlice(&gitToolchainPaths, []string{filepath.Join(dir, "no-such-toolchain", "git")}))
	t.Cleanup(swapSlice(&gitSearchDirs, []string{filepath.Join(dir, "empty")}))
	t.Cleanup(swapFunc(&gitLookPath, func(string) (string, error) { return "", exec.ErrNotFound }))

	got := Locate(context.Background())
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the shim was executed")
	}
	if got.OK() {
		t.Fatalf("availability = %+v, want unavailable", got)
	}
	if !strings.Contains(got.Reason, "xcode-select") {
		t.Fatalf("reason = %q, want the xcode-select fix", got.Reason)
	}
}

// With the tools installed the stub is a real git and must be used, otherwise a
// stock Mac with no Homebrew has no checkpoints at all.
func TestShimIsUsedWhenCommandLineToolsArePresent(t *testing.T) {
	dir := t.TempDir()
	shim := writeScript(t, dir, "git", "echo 'git version 2.39.5 (Apple Git-154)'\n")
	toolchain := writeScript(t, filepath.Join(dir, "toolchain"), "git", "echo real\n")

	t.Cleanup(swap(&gitGOOS, "darwin"))
	t.Cleanup(swap(&gitShimPath, shim))
	t.Cleanup(swapSlice(&gitToolchainPaths, []string{toolchain}))
	t.Cleanup(swapSlice(&gitSearchDirs, []string{filepath.Join(dir, "empty")}))
	t.Cleanup(swapFunc(&gitLookPath, func(string) (string, error) { return "", exec.ErrNotFound }))

	got := Locate(context.Background())
	if got.Path != shim {
		t.Fatalf("path = %q, want %q (reason %q)", got.Path, shim, got.Reason)
	}
	if got.Version != "2.39.5" {
		t.Fatalf("version = %q, want 2.39.5 parsed out of Apple's banner", got.Version)
	}
}

// A git below the floor is a different message from no git at all: one is
// "upgrade", the other is "install".
func TestTooOldGitSaysSo(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, filepath.Join(dir, "bin"), "git", "echo 'git version 2.10.0'\n")

	t.Cleanup(swap(&gitGOOS, "linux"))
	t.Cleanup(swapSlice(&gitSearchDirs, []string{filepath.Join(dir, "bin")}))
	t.Cleanup(swapFunc(&gitLookPath, func(string) (string, error) { return "", exec.ErrNotFound }))

	got := Locate(context.Background())
	if got.OK() {
		t.Fatalf("availability = %+v, want unavailable", got)
	}
	if !strings.Contains(got.Reason, "2.10.0") || !strings.Contains(got.Reason, MinGitVersion) {
		t.Fatalf("reason = %q, want both versions named", got.Reason)
	}
}

func TestCompareGitVersionsIgnoresVendorBanners(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2.39.5 (Apple Git-154)", "2.26", 1},
		{"2.26.0", "2.26", 0},
		{"2.9.9", "2.26", -1},
		{"10.0.0", "9.99.99", 1},
	}
	for _, tc := range cases {
		if got := compareGitVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func swap[T any](target *T, value T) func() {
	previous := *target
	*target = value
	return func() { *target = previous }
}

func swapSlice(target *[]string, value []string) func() { return swap(target, value) }

func swapFunc(target *func(string) (string, error), value func(string) (string, error)) func() {
	return swap(target, value)
}
