package mcp

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// SocketPath returns the Unix domain socket path for the DevKit MCP host:
// $UserConfigDir/DevKit/mcp.sock.
func SocketPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "DevKit", "mcp.sock"), nil
}

// ServeUnix listens on a Unix domain socket at path, chmods it to 0600, and
// serves one MCP session per accepted connection until ctx is cancelled.
// The DevKit config directory is created if needed.
func ServeUnix(ctx context.Context, path string, s *Server) error {
	if s == nil {
		return fmt.Errorf("mcp: nil server")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// Refuse to steal a live socket from another DevKit instance: a successful
	// probe means some process still owns it. Only a stale (dead) socket is
	// removed.
	if conn, dialErr := net.DialTimeout("unix", path, 250*time.Millisecond); dialErr == nil {
		conn.Close()
		return fmt.Errorf("mcp socket %s is already in use by another instance", path)
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return err
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
		_ = os.Remove(path)
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go func(c net.Conn) {
			defer c.Close()
			_ = s.MCP().Run(ctx, newRWTransport(c))
		}(conn)
	}
}

// rwTransport adapts an io.ReadWriteCloser to sdk.Transport, mirroring
// StdioTransport but for an arbitrary stream (e.g. a Unix socket connection).
// It uses the public IOTransport so we do not depend on unexported SDK helpers.
type rwTransport struct {
	rwc io.ReadWriteCloser
}

func newRWTransport(rwc io.ReadWriteCloser) *rwTransport {
	return &rwTransport{rwc: rwc}
}

// writeSide is an io.WriteCloser whose Close is a no-op so IOTransport only
// closes the underlying stream once (via Reader), matching StdioTransport's
// treatment of stdout.
type writeSide struct {
	io.Writer
}

func (writeSide) Close() error { return nil }

// Connect implements sdk.Transport.
func (t *rwTransport) Connect(ctx context.Context) (sdk.Connection, error) {
	return (&sdk.IOTransport{
		Reader: t.rwc,
		Writer: writeSide{t.rwc},
	}).Connect(ctx)
}
