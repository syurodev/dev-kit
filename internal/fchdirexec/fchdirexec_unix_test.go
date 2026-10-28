//go:build unix

package fchdirexec_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"devkit/internal/fchdirexec"
)

// helper starts the program as the helper does: this binary under Arg0, with dir as fd 3.
func helper(t *testing.T, ctx context.Context, dir *os.File, program string, args ...string) *exec.Cmd {
	t.Helper()
	self, err := fchdirexec.Self()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, self)
	cmd.Args = append([]string{fchdirexec.Arg0, program}, args...)
	if dir != nil {
		cmd.ExtraFiles = []*os.File{dir}
	}
	return cmd
}

func openDir(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func sh(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	return path
}

func TestHelperStartsTheProgramInTheDescriptorsDirectory(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cmd := helper(t, context.Background(), openDir(t, dir), sh(t), "-c", "pwd -P")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != dir {
		t.Fatalf("cwd = %q, want %q", got, dir)
	}
}

// The directory is held by descriptor: renamed away while the process is not yet
// started, the helper still enters the directory and not what its name leads to now.
func TestHelperEntersTheDirectoryNotTheName(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	real, other := filepath.Join(root, "real"), filepath.Join(root, "other")
	for _, d := range []string{real, other} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	held := openDir(t, real)
	if err := os.Rename(real, real+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, real); err != nil {
		t.Fatal(err)
	}
	out, err := helper(t, context.Background(), held, sh(t), "-c", "pwd -P").Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != real+".moved" {
		t.Fatalf("cwd = %q, want the moved directory %q", got, real+".moved")
	}
}

func TestHelperPassesTheEnvironmentAndTheOtherDescriptorsThrough(t *testing.T) {
	dir := openDir(t, t.TempDir())
	extra := openDir(t, t.TempDir())
	cmd := helper(t, context.Background(), dir, sh(t), "-c", `echo "$GIT_DIR|$GIT_CONFIG_NOSYSTEM|$DEVKIT_SECRET|$GIT_WORK_TREE"; [ -d /proc/self/fd/4 ] && echo fd4 || echo nofd4`)
	cmd.ExtraFiles = append(cmd.ExtraFiles, extra)
	cmd.Env = append(os.Environ(), "GIT_DIR=/proc/self/fd/4", "GIT_CONFIG_NOSYSTEM=1", "DEVKIT_SECRET=s3", "GIT_WORK_TREE=.")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if lines[0] != "/proc/self/fd/4|1|s3|." {
		t.Fatalf("environment = %q", lines[0])
	}
	if runtime.GOOS == "linux" && (len(lines) < 2 || lines[1] != "fd4") {
		t.Fatalf("the second inherited descriptor did not reach the program: %q", lines)
	}
}

// The directory's own descriptor is closed before the program starts.
func TestHelperClosesDescriptorThree(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("reads /proc")
	}
	out, err := helper(t, context.Background(), openDir(t, t.TempDir()), sh(t), "-c",
		`if [ -e /proc/$$/fd/3 ]; then echo open; else echo closed; fi`).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != "closed" {
		t.Fatalf("fd 3 = %s in the program", got)
	}
}

func TestHelperExitCodes(t *testing.T) {
	dir := openDir(t, t.TempDir())
	var exit *exec.ExitError

	err := helper(t, context.Background(), dir, sh(t), "-c", "exit 7").Run()
	if !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("exit 7: %v", err)
	}
	if err := helper(t, context.Background(), dir, sh(t), "-c", "exit 0").Run(); err != nil {
		t.Fatalf("exit 0: %v", err)
	}
	var stderr bytes.Buffer
	cmd := helper(t, context.Background(), dir, "/nonexistent/program")
	cmd.Stderr = &stderr
	err = cmd.Run()
	if !errors.As(err, &exit) || exit.ExitCode() != fchdirexec.ExitExec || !strings.Contains(stderr.String(), "exec /nonexistent/program") {
		t.Fatalf("missing program: %v %q", err, stderr.String())
	}
	// No directory descriptor at all: the helper refuses and does not run the program.
	marker := filepath.Join(t.TempDir(), "ran")
	cmd = helper(t, context.Background(), nil, sh(t), "-c", "touch '"+marker+"'")
	stderr.Reset()
	cmd.Stderr = &stderr
	err = cmd.Run()
	if !errors.As(err, &exit) || exit.ExitCode() != fchdirexec.ExitFchdir || !strings.Contains(stderr.String(), "fchdir") {
		t.Fatalf("no descriptor: %v %q", err, stderr.String())
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the program ran although the directory could not be entered")
	}
}

// A descriptor that is not a directory is refused, not ignored.
func TestHelperRefusesADescriptorThatIsNotADirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var exit *exec.ExitError
	if err := helper(t, context.Background(), f, sh(t), "-c", "true").Run(); !errors.As(err, &exit) || exit.ExitCode() != fchdirexec.ExitFchdir {
		t.Fatalf("err = %v", err)
	}
}

// The program is the process the caller started: cancelling the context kills it, not a
// wrapper that leaves it running.
func TestHelperCancellationKillsTheProgram(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	cmd := helper(t, ctx, openDir(t, t.TempDir()), sh(t), "-c", "exec sleep 30")
	started := time.Now()
	err := cmd.Run()
	if err == nil {
		t.Fatal("a sleeping program finished")
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("cancellation took %v", elapsed)
	}
	pid := cmd.ProcessState.Pid()
	if err := syscallKill0(pid); err == nil {
		t.Fatalf("process %d is still there", pid)
	}
	// The sleep was the shell's child and is not the helper's concern; the process the
	// caller started (the shell, after exec) is gone, which is what the caller waits for.
}

func TestHelperCarriesLargeOutputAndInput(t *testing.T) {
	dir := openDir(t, t.TempDir())
	const size = 8 << 20
	out, err := helper(t, context.Background(), dir, sh(t), "-c", "head -c 8388608 /dev/zero").Output()
	if err != nil || len(out) != size {
		t.Fatalf("output = %d bytes, %v", len(out), err)
	}
	cmd := helper(t, context.Background(), dir, sh(t), "-c", "wc -c")
	cmd.Stdin = bytes.NewReader(make([]byte, size))
	counted, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(counted)) != "8388608" {
		t.Fatalf("input = %q, %v", counted, err)
	}
}

// With no program there is nothing to do and the helper says so.
func TestHelperWithNoProgram(t *testing.T) {
	self, err := fchdirexec.Self()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self)
	cmd.Args = []string{fchdirexec.Arg0}
	var exit *exec.ExitError
	if err := cmd.Run(); !errors.As(err, &exit) || exit.ExitCode() != fchdirexec.ExitExec {
		t.Fatalf("err = %v", err)
	}
}
