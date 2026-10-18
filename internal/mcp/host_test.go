package mcp

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSocketPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := SocketPath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "mcp.sock" {
		t.Fatalf("SocketPath base = %q, want mcp.sock", filepath.Base(path))
	}
	if filepath.Base(filepath.Dir(path)) != "DevKit" {
		t.Fatalf("SocketPath dir = %q, want .../DevKit", filepath.Dir(path))
	}
}

func testSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "mcp-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "mcp.sock")
}

func TestServeUnixListenAcceptClose(t *testing.T) {
	sock := testSocketPath(t)

	caller := &fakeCaller{}
	server := NewServer(testDeps(caller, nil))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- ServeUnix(ctx, sock, server)
	}()

	deadline := time.Now().Add(2 * time.Second)
	var conn net.Conn
	for {
		var dialErr error
		conn, dialErr = net.DialTimeout("unix", sock, 50*time.Millisecond)
		if dialErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial unix: %v", dialErr)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// ServeUnix listens and then chmods, so a dial that lands between the two
	// sees the socket as the process umask made it. The directory above it is
	// 0700, so nobody else could use it in that gap; the contract is the mode
	// once ServeUnix has finished starting, which the poll waits for.
	var mode os.FileMode
	for {
		info, err := os.Stat(sock)
		if err != nil {
			t.Fatal(err)
		}
		mode = info.Mode().Perm()
		if mode&0o777 == 0o600 || time.Now().After(deadline.Add(2*time.Second)) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if mode&0o777 != 0o600 {
		t.Fatalf("socket mode = %o, want 0600", mode)
	}

	_ = conn.Close()
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("ServeUnix: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ServeUnix did not return after cancel")
	}
}

func TestServeUnixMCPInitializeAndListTools(t *testing.T) {
	sock := testSocketPath(t)

	caller := &fakeCaller{}
	server := NewServer(testDeps(caller, nil))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = ServeUnix(ctx, sock, server) }()

	deadline := time.Now().Add(2 * time.Second)
	for {
		c, err := net.DialTimeout("unix", sock, 50*time.Millisecond)
		if err == nil {
			_ = c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("socket not ready: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	client := sdk.NewClient(&sdk.Implementation{Name: "host-test", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, newRWTransport(conn), nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools.Tools) == 0 {
		t.Fatal("ListTools returned no tools")
	}
	var foundStatus bool
	for _, tool := range tools.Tools {
		if tool.Name == "vault.status" {
			foundStatus = true
			break
		}
	}
	if !foundStatus {
		t.Fatalf("expected vault.status in tools, got %#v", tools.Tools)
	}
}
