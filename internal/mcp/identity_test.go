package mcp

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/identity"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func callVaultStatus(t *testing.T, ms *sdk.Server) {
	t.Helper()
	ctx := context.Background()
	st, ct := sdk.NewInMemoryTransports()
	ss, err := ms.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	if _, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "vault.status", Arguments: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
}

func TestSDKServerForStampsCaller(t *testing.T) {
	caller := &fakeCaller{}
	srv := NewServer(testDeps(caller, nil))
	callVaultStatus(t, srv.sdkServerFor(identity.Agent("11111111-2222-3333-4444-555555555555")))
	if caller.last.caller != "agent:11111111-2222-3333-4444-555555555555" {
		t.Fatalf("caller reaching the gateway = %q", caller.last.caller)
	}
}

func TestDefaultServerIsUserTerminal(t *testing.T) {
	caller := &fakeCaller{}
	srv := NewServer(testDeps(caller, nil))
	callVaultStatus(t, srv.MCP())
	if caller.last.caller != "user-terminal" {
		t.Fatalf("shared-socket caller = %q, want user-terminal", caller.last.caller)
	}
}

func TestSessionHostOpensIdentityBoundSocket(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "agents")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	caller := &fakeCaller{}
	srv := NewServer(testDeps(caller, nil))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host := NewSessionHost(ctx, srv, dir, "/usr/bin/true")

	const sid = "11111111-2222-3333-4444-555555555555"
	ep, err := host.Open(sid)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(ep.SocketPath) != "1111111122223333.sock" {
		t.Fatalf("socket name = %s", filepath.Base(ep.SocketPath))
	}
	if ep.Server.Command != "/usr/bin/true" || len(ep.Server.Args) != 2 || ep.Server.Args[1] != ep.SocketPath || ep.Server.Env == nil {
		t.Fatalf("server = %+v", ep.Server)
	}
	if info, err := os.Stat(ep.SocketPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket stat: %v mode=%v", err, info)
	}

	conn, err := net.Dial("unix", ep.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil)
	cs, err := client.Connect(ctx, newRWTransport(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "vault.status", Arguments: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	cs.Close()
	if caller.last.caller != "agent:"+sid {
		t.Fatalf("caller = %q", caller.last.caller)
	}

	ep.Close()
	if _, err := os.Stat(ep.SocketPath); !os.IsNotExist(err) {
		t.Fatalf("socket must be removed on Close, stat err = %v", err)
	}
}

func TestSessionHostWithoutProxyFailsClosed(t *testing.T) {
	host := NewSessionHost(context.Background(), NewServer(testDeps(&fakeCaller{}, nil)), t.TempDir(), "")
	if _, err := host.Open("11111111-2222-3333-4444-555555555555"); err != agenthub.ErrProxyNotFound {
		t.Fatalf("got %v, want ErrProxyNotFound", err)
	}
}

func TestSweepStaleSocketsRemovesDeadOnes(t *testing.T) {
	dir, _ := os.MkdirTemp("/tmp", "agents")
	defer os.RemoveAll(dir)
	dead := filepath.Join(dir, "dead.sock")
	ln, _ := net.Listen("unix", dead)
	ln.Close() // Close removes the file on Go; recreate a stale inode
	_ = os.WriteFile(dead, nil, 0o600)
	live := filepath.Join(dir, "live.sock")
	lln, _ := net.Listen("unix", live)
	defer lln.Close()
	go func() {
		for {
			c, err := lln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	SweepStaleSockets(dir)
	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Fatal("dead socket must be removed")
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatal("live socket must be kept")
	}
	_ = time.Second
}
