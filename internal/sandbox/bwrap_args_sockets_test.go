//go:build linux

package sandbox

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestBwrapBindsAllowedUnixSocketAfterTmpfsHome(t *testing.T) {
	home := t.TempDir()
	sock := filepath.Join(home, ".config", "DevKit", "agents", "s.sock")
	if err := os.MkdirAll(filepath.Dir(sock), 0o700); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	args := buildBwrapArgs(Policy{WorkDir: t.TempDir(), Env: map[string]string{"HOME": home}, AllowUnixSockets: []string{sock}}, Cmd{Path: "/bin/true"})
	tmpfsAt, bindAt := -1, -1
	for i := range args {
		if args[i] == "--tmpfs" && i+1 < len(args) && args[i+1] == home {
			tmpfsAt = i
		}
		if args[i] == "--bind" && i+2 < len(args) && args[i+1] == sock && args[i+2] == sock {
			bindAt = i
		}
	}
	if tmpfsAt < 0 || bindAt < 0 || bindAt < tmpfsAt {
		t.Fatalf("socket bind must follow the home tmpfs: tmpfs@%d bind@%d in %v", tmpfsAt, bindAt, args)
	}
}
