//go:build darwin

package sandbox

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
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
			if _, err := os.ReadFile(os.Getenv("DEVKIT_OTHER_TEMP_FILE")); err == nil {
				t.Fatal("read another session's temp file")
			}
			ln, err := net.Listen("unix", os.Getenv("DEVKIT_OUTSIDE_SOCKET"))
			if err == nil {
				ln.Close()
				t.Fatal("bound Unix socket outside own temp")
			}
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
		if err != nil || !strings.Contains(string(stdout), "DEVELOPER_OK") {
			t.Fatalf("%v\n%s\n%s", err, stdout, stderr)
		}
	}
	env["DEVKIT_OTHER_TEMP_FILE"] = otherFile
	env["DEVKIT_OUTSIDE_SOCKET"] = filepath.Join(outside, "outside.sock")
	for _, name := range []string{"unix", "isolation", "signal", "user"} {
		t.Run(name, func(t *testing.T) {
			env["DEVKIT_DEVELOPER_SMOKE_CHILD"] = "1"
			env["DEVKIT_DEVELOPER_SMOKE_CASE"] = name
			run(t, Cmd{Path: exe, Args: []string{"-test.run=^TestSmokeSeatbeltDeveloperTools$"}})
		})
	}
	delete(env, "DEVKIT_DEVELOPER_SMOKE_CHILD")
	t.Run("cgo-and-posix_openpt", func(t *testing.T) {
		// Build and execute inside the jail: tests xcrun/clang discovery, cgo's
		// temp work tree and the PTY extension, not just a host-built executable.
		source := `package main
/*
#include <stdlib.h>
#include <fcntl.h>
#include <unistd.h>
int probe(void) {
 int fd = posix_openpt(O_RDWR | O_NOCTTY);
 if (fd < 0) return 1;
 if (grantpt(fd) || unlockpt(fd)) return 2;
 int slave = open(ptsname(fd), O_RDWR | O_NOCTTY);
 if (slave < 0) return 3;
 close(slave); close(fd); return 0;
}
*/
import "C"
import "fmt"
func main() { if n := C.probe(); n != 0 { panic(n) }; fmt.Println("DEVELOPER_OK") }
`
		path := filepath.Join(dir, "hello.go")
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
