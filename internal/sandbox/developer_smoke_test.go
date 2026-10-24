//go:build darwin

package sandbox

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/creack/pty"
	"testing"
	"time"
)

func requireHostSeatbelt(t *testing.T) {
	t.Helper()
	out, err := exec.Command("/usr/bin/sandbox-exec", "-p", "(version 1)(allow default)", "/usr/bin/true").CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "sandbox_apply: Operation not permitted") || os.IsNotExist(err) {
			t.Skip("nested Seatbelt unavailable; run go test ./internal/sandbox -run Smoke -v from a host terminal")
		}
		t.Fatalf("Seatbelt probe: %v: %s", err, out)
	}
}

func TestSmokeSeatbeltDeveloperTools(t *testing.T) {
	if os.Getenv("DEVKIT_DEVELOPER_SMOKE_CHILD") == "1" {
		switch os.Getenv("DEVKIT_DEVELOPER_SMOKE_CASE") {
		case "unix":
			socket := filepath.Join(os.TempDir(), "smoke.sock")
			ln, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			accepted := make(chan error, 1)
			go func() {
				c, err := ln.Accept()
				if err == nil {
					c.Close()
				}
				accepted <- err
			}()
			c, err := net.Dial("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			c.Close()
			if err := <-accepted; err != nil {
				t.Fatal(err)
			}
		case "isolation":
			denied := func(action string, err error) {
				t.Helper()
				if !errors.Is(err, syscall.EPERM) && !errors.Is(err, syscall.EACCES) {
					t.Fatalf("%s: expected permission denied, got %v", action, err)
				}
			}
			otherFile := os.Getenv("DEVKIT_OTHER_TEMP_FILE")
			_, err := os.ReadFile(otherFile)
			denied("read other session", err)
			denied("write other session", os.WriteFile(filepath.Join(filepath.Dir(otherFile), "planted"), nil, 0o600))
			denied("hardlink other session", os.Link(otherFile, filepath.Join(os.TempDir(), "linked")))
			ownFile := filepath.Join(os.TempDir(), "rename-source")
			if err := os.WriteFile(ownFile, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			denied("rename into other session", os.Rename(ownFile, filepath.Join(filepath.Dir(otherFile), "renamed")))
			denied("plant session directory", os.Mkdir("/private/tmp/dk-zzzzzzzz", 0o700))
			hostPID, err := strconv.Atoi(os.Getenv("DEVKIT_HOST_PID"))
			if err != nil {
				t.Fatal(err)
			}
			denied("signal host process", syscall.Kill(hostPID, 0))
			tty, err := os.OpenFile(os.Getenv("DEVKIT_HOST_TTY"), os.O_WRONLY, 0)
			if err == nil {
				tty.Close()
				t.Fatal("opened host-created tty for writing")
			}
			denied("write host tty", err)
			ln, err := net.Listen("unix", os.Getenv("DEVKIT_OUTSIDE_SOCKET"))
			if err == nil {
				ln.Close()
				t.Fatal("bound Unix socket outside own temp")
			}
			denied("bind outside scratch", err)
		case "signal":
			cmd := exec.Command("/bin/sleep", "30")
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			_ = cmd.Wait()
		case "user":
			u, err := user.Current()
			if err != nil || u.Username == "" {
				t.Fatalf("user.Current: %+v %v", u, err)
			}
		}
		fmt.Println("DEVELOPER_OK")
		return
	}
	requireHostSeatbelt(t)
	dir, err := os.MkdirTemp("/private/tmp", "dk-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	other, err := os.MkdirTemp("/private/tmp", "dk-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(other)
	otherFile := filepath.Join(other, "secret")
	if err := os.WriteFile(otherFile, []byte("other session"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside, err := os.MkdirTemp("/private/tmp", "devkit-smoke-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outside)
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal("create host PTY fixture", err)
	}
	defer master.Close()
	defer slave.Close()
	env := map[string]string{"PATH": os.Getenv("PATH"), "HOME": hostHomeDir(), "TMPDIR": dir, "GOTMPDIR": dir}
	policy := Policy{WorkDir: dir, SessionTempDir: dir, Network: NetworkHost, PrivateHome: boolPtr(false), Env: env,
		ReadOnlyPaths: []string{exe, "/Library/Developer", "/opt/homebrew", "/usr/local"}}
	apps, _ := filepath.Glob("/Applications/Xcode*.app")
	policy.ReadOnlyPaths = append(policy.ReadOnlyPaths, apps...)
	if _, err := os.Stat("/private/var/db/xcode_select_link"); err == nil {
		policy.ReadOnlyPaths = append(policy.ReadOnlyPaths, "/private/var/db/xcode_select_link")
	}
	run := func(t *testing.T, cmd Cmd) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		proc, err := NewSeatbeltBackend().Start(ctx, policy, cmd)
		if err != nil {
			t.Fatal(err)
		}
		err = proc.Wait()
		stdout, stderr := proc.Output()
		t.Logf("sandbox stdout:\n%s", stdout)
		if err != nil || !strings.Contains(string(stdout), "DEVELOPER_OK") {
			t.Fatalf("%v\n%s\n%s", err, stdout, stderr)
		}
	}
	env["DEVKIT_OTHER_TEMP_FILE"] = otherFile
	env["DEVKIT_HOST_PID"] = strconv.Itoa(os.Getpid())
	env["DEVKIT_HOST_TTY"] = slave.Name()
	env["DEVKIT_OUTSIDE_SOCKET"] = filepath.Join(outside, "outside.sock")
	for _, name := range []string{"unix", "isolation", "signal", "user"} {
		t.Run(name, func(t *testing.T) {
			env["DEVKIT_DEVELOPER_SMOKE_CHILD"] = "1"
			env["DEVKIT_DEVELOPER_SMOKE_CASE"] = name
			run(t, Cmd{Path: exe, Args: []string{"-test.run=^TestSmokeSeatbeltDeveloperTools$"}})
		})
	}
	delete(env, "DEVKIT_DEVELOPER_SMOKE_CHILD")
	// Separate programs keep a process-info failure from hiding the PTY result.
	// Build and execute both inside the jail to exercise xcrun/clang discovery
	// and cgo's temp work tree, rather than just a host-built executable.
	for _, probe := range []struct {
		name string
		body string
	}{
		{
			name: "cgo-process-info",
			// Host process metadata and executable paths are readable in the
			// jail. Keep measuring them for visibility; the separate signal probe
			// checks the boundary that remains enforced.
			body: `
				 struct proc_bsdinfo info;
				 char path[PROC_PIDPATHINFO_MAXSIZE];
 errno = 0;
				 int info_count = proc_pidinfo(atoi(getenv("DEVKIT_HOST_PID")), PROC_PIDTBSDINFO, 0, &info, sizeof(info));
			 int info_errno = errno;
			 errno = 0;
			 int path_bytes = proc_pidpath(atoi(getenv("DEVKIT_HOST_PID")), path, sizeof(path));
			 int path_errno = errno;
				 printf("PROBE pidinfo_count=%d pidinfo_errno=%d pidpath_bytes=%d pidpath_errno=%d\n", info_count, info_errno, path_bytes, path_errno);
 fflush(stdout);
 return 0;
`,
		},
		{
			name: "cgo-and-posix_openpt",
			body: `
 int fd = posix_openpt(O_RDWR | O_NOCTTY);
 if (fd < 0) return 1;
 if (grantpt(fd) || unlockpt(fd)) { close(fd); return 2; }
 int slave = open(ptsname(fd), O_RDWR | O_NOCTTY);
 if (slave < 0) { close(fd); return 3; }
 close(slave); close(fd); return 0;
`,
		},
	} {
		t.Run(probe.name, func(t *testing.T) {
			source := `package main
/*
#include <stdlib.h>
#include <stdio.h>
#include <fcntl.h>
#include <unistd.h>
#include <libproc.h>
#include <errno.h>
int probe(void) {` + probe.body + `}
*/
import "C"
import (
 "fmt"
 "os"
)
func main() {
 if n := C.probe(); n != 0 {
  fmt.Fprintf(os.Stderr, "probe failed: code=%d\n", int(n))
  os.Exit(int(n))
 }
	 fmt.Println("DEVELOPER_OK")
}

`
			path := filepath.Join(dir, probe.name+".go")
			if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
				t.Fatal(err)
			}
			goBin, err := exec.LookPath("go")
			if err != nil {
				t.Fatal(err)
			}
			env["CGO_ENABLED"] = "1"
			env["GOCACHE"] = filepath.Join(dir, "cache")
			run(t, Cmd{Path: goBin, Args: []string{"run", path}})
		})
	}
}

// TestSmokeSeatbeltProcessInfoVariants isolates the two broad profile grants
// that could account for proc_pidinfo's observed host metadata. It is opt-in
// because it needs to run under a real host Seatbelt policy.
func TestSmokeSeatbeltProcessInfoVariants(t *testing.T) {
	if os.Getenv("DEVKIT_SANDBOX_VARIANTS") != "1" {
		t.Skip("set DEVKIT_SANDBOX_VARIANTS=1 on a macOS host to compare Seatbelt rules")
	}
	requireHostSeatbelt(t)
	dir, err := os.MkdirTemp("/private/tmp", "dk-procinfo-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	pid := os.Getpid()
	source := `package main
/*
#include <libproc.h>
#include <sys/sysctl.h>
#include <sys/proc.h>
#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
void probe(int pid) {
 struct proc_bsdinfo info;
 char path[PROC_PIDPATHINFO_MAXSIZE];
 int mib[4] = { CTL_KERN, KERN_PROC, KERN_PROC_PID, pid };
 struct kinfo_proc kp;
 size_t size = sizeof(kp);
 errno = 0;
 int info_count = proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &info, sizeof(info));
 int info_errno = errno;
 errno = 0;
 int path_count = proc_pidpath(pid, path, sizeof(path));
 int path_errno = errno;
 errno = 0;
 int sysctl_result = sysctl(mib, 4, &kp, &size, NULL, 0);
 int sysctl_errno = errno;
 printf("PROBE pidinfo_count=%d pidinfo_errno=%d pidpath_bytes=%d pidpath_errno=%d kern_proc_data=%d kern_proc_errno=%d\n",
  info_count, info_errno, path_count, path_errno, sysctl_result == 0 && size > 0, sysctl_errno);
}
*/
import "C"
import "os"
import "strconv"
func main() { pid, _ := strconv.Atoi(os.Getenv("DEVKIT_HOST_PID")); C.probe(C.int(pid)) }
`
	sourcePath := filepath.Join(dir, "probe.go")
	binaryPath := filepath.Join(dir, "probe")
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", binaryPath, sourcePath)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build cgo probe: %v\n%s", err, output)
	}
	policy := Policy{WorkDir: dir, SessionTempDir: dir, Network: NetworkHost, PrivateHome: boolPtr(false),
		Env: map[string]string{"HOME": hostHomeDir(), "TMPDIR": dir}}
	base := buildSeatbeltProfileWithUnixSockets(policy, true)
	variants := []struct{ name, profile string }{
		{"baseline", base},
		{"without-sysctl-read", strings.Replace(base, "(allow sysctl-read)\n", "", 1)},
		{"without-process-info", strings.Replace(base, "(allow process-info* (target same-sandbox))", "", 1)},
	}
	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			control, err := exec.Command("/usr/bin/sandbox-exec", "-p", variant.profile, "--", "/usr/bin/true").CombinedOutput()
			if err != nil {
				t.Fatalf("profile control did not start /usr/bin/true: %v: %s", err, control)
			}
			cmd := exec.Command("/usr/bin/sandbox-exec", "-p", variant.profile, "--", binaryPath)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "DEVKIT_HOST_PID="+strconv.Itoa(pid), "TMPDIR="+dir, "HOME="+hostHomeDir())
			output, err := cmd.CombinedOutput()
			line := strings.TrimSpace(string(output))
			if err != nil {
				if line == "" {
					line = "<no output>"
				} else if i := strings.IndexByte(line, '\n'); i >= 0 {
					line = line[:i]
				}
				t.Logf("VARIANT %s control=started probe-did-not-complete error=%v first_line=%q", variant.name, err, line)
				return
			}
			if !strings.Contains(line, "PROBE ") {
				t.Fatalf("probe produced no measurement under %s: %s", variant.name, output)
			}
			t.Logf("VARIANT %s control=started %s", variant.name, line)
		})
	}
}
