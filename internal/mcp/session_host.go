package mcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"devkit/internal/acp"
	"devkit/internal/agenthub"
	"devkit/internal/identity"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ErrSocketPathTooLong: sun_path is 104 bytes on darwin; a longer path fails
// at bind time with a confusing EINVAL, so refuse it up front.
var ErrSocketPathTooLong = errors.New("mcp: agent socket path exceeds the sun_path limit")

const maxSunPath = 103

// AgentsDir is where per-session sockets live: next to the shared socket.
func AgentsDir(sharedSocketPath string) string {
	return filepath.Join(filepath.Dir(sharedSocketPath), "agents")
}

// socketName shortens the session UUID so the full path fits sun_path even
// under a long ~/Library/Application Support prefix.
func socketName(sessionID string) string {
	id := strings.ReplaceAll(sessionID, "-", "")
	if len(id) > 16 {
		id = id[:16]
	}
	return id + ".sock"
}

// SessionHost opens one identity-bound MCP socket per Agent Hub session. It
// implements agenthub.MCPEndpoints.
type SessionHost struct {
	ctx       context.Context
	server    *Server
	dir       string
	proxyPath string // absolute devkit-mcp; "" = not found → Open fails closed

	mu        sync.Mutex
	listeners map[string]net.Listener
}

var _ agenthub.MCPEndpoints = (*SessionHost)(nil)

// NewSessionHost returns a host serving sockets under dir until ctx is done.
func NewSessionHost(ctx context.Context, server *Server, dir, proxyPath string) *SessionHost {
	return &SessionHost{ctx: ctx, server: server, dir: dir, proxyPath: proxyPath, listeners: map[string]net.Listener{}}
}

// Open creates the socket for sessionID and returns what the sandbox and the
// ACP session/new need. Spawning an agent that cannot reach DevKit is worse
// than not spawning it, so a missing proxy fails here, before the spawn.
func (h *SessionHost) Open(sessionID string) (agenthub.MCPEndpoint, error) {
	if h.proxyPath == "" {
		return agenthub.MCPEndpoint{}, agenthub.ErrProxyNotFound
	}
	if err := os.MkdirAll(h.dir, 0o700); err != nil {
		return agenthub.MCPEndpoint{}, err
	}
	path := filepath.Join(h.dir, socketName(sessionID))
	if len(path) > maxSunPath {
		return agenthub.MCPEndpoint{}, fmt.Errorf("%w: %s", ErrSocketPathTooLong, path)
	}
	if conn, err := net.DialTimeout("unix", path, 250*time.Millisecond); err == nil {
		conn.Close()
		return agenthub.MCPEndpoint{}, fmt.Errorf("mcp: agent socket %s is already in use", path)
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return agenthub.MCPEndpoint{}, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		_ = os.Remove(path)
		return agenthub.MCPEndpoint{}, err
	}

	ms := h.server.sdkServerFor(identity.Agent(sessionID))
	h.mu.Lock()
	h.listeners[sessionID] = ln
	h.mu.Unlock()
	go h.serve(ln, ms)

	closeFn := func() {
		h.mu.Lock()
		delete(h.listeners, sessionID)
		h.mu.Unlock()
		_ = ln.Close()
		_ = os.Remove(path)
	}
	return agenthub.MCPEndpoint{
		SocketPath: path,
		Server: acp.McpServerStdio{
			Name:    "devkit",
			Command: h.proxyPath,
			Args:    []string{"--socket", path},
			Env:     []acp.EnvVariable{},
		},
		Close: closeFn,
	}, nil
}

func (h *SessionHost) serve(ln net.Listener, ms *sdk.Server) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			_ = ms.Run(h.ctx, newRWTransport(c))
		}(conn)
	}
}

// Close closes every open agent socket (app shutdown).
func (h *SessionHost) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, ln := range h.listeners {
		_ = ln.Close()
		_ = os.Remove(filepath.Join(h.dir, socketName(id)))
		delete(h.listeners, id)
	}
}

// SweepStaleSockets removes agent sockets nobody answers on — leftovers of a
// DevKit that exited without cleanup. Sockets that accept are another live
// instance's and are left alone.
func SweepStaleSockets(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sock" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if conn, err := net.DialTimeout("unix", path, 250*time.Millisecond); err == nil {
			conn.Close()
			continue
		}
		_ = os.Remove(path)
	}
}
