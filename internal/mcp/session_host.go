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
	"devkit/internal/harness"
	"devkit/internal/identity"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/google/uuid"
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
// implements agenthub.MCPEndpoints, and agenthub.PolicyEndpoints for a session
// that must serve a prepared harness policy from its first call.
type SessionHost struct {
	ctx       context.Context
	server    *Server
	dir       string
	proxyPath string // absolute devkit-mcp; "" = not found → Open fails closed

	mu        sync.Mutex
	endpoints map[string]*sessionEndpoint

	// bindMu serializes everything that touches a session's socket path: an
	// open (probe, bind, chmod, publish in endpoints) and the cleanup of an
	// endpoint (listener closed, file removed, entry dropped). Without it an
	// old endpoint's cleanup can run between a replacement's bind and its
	// publication, find itself still registered and remove the new socket.
	// Lock order: bindMu, then mu. Nothing slow runs under it: a unix dial to a
	// closed path fails at once, bind and chmod are local.
	bindMu sync.Mutex
	// Hooks for tests; nil in production.
	bindHook, closeHook func()
}

var (
	_ agenthub.MCPEndpoints    = (*SessionHost)(nil)
	_ agenthub.PolicyEndpoints = (*SessionHost)(nil)
)

// NewSessionHost returns a host serving sockets under dir until ctx is done.
func NewSessionHost(ctx context.Context, server *Server, dir, proxyPath string) *SessionHost {
	return &SessionHost{ctx: ctx, server: server, dir: dir, proxyPath: proxyPath, endpoints: map[string]*sessionEndpoint{}}
}

// sessionEndpoint is one incarnation of a session's endpoint: its listener, its
// accepted connections, its token and its policy holder. Close is idempotent
// and touches only this incarnation, so a late or repeated Close of an old
// endpoint cannot remove a replacement's socket or entry.
type sessionEndpoint struct {
	host    *SessionHost
	id      string
	path    string
	ln      net.Listener
	serving *servingHolder // nil: today's unfiltered endpoint
	ctx     context.Context
	cancel  context.CancelFunc

	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	closed bool
	once   sync.Once
}

// Open creates the socket for sessionID and returns what the sandbox and the
// ACP session/new need. Spawning an agent that cannot reach DevKit is worse
// than not spawning it, so a missing proxy fails here, before the spawn.
func (h *SessionHost) Open(sessionID string) (agenthub.MCPEndpoint, error) {
	return h.open(sessionID, nil)
}

// OpenWithPolicy is Open with a prepared policy staged before the socket
// accepts anything: the endpoint lists only the tools the policy serves,
// refuses a registered tool it hides and cuts results to the policy's budgets.
// A policy resolved against a registry other than this server's is refused.
func (h *SessionHost) OpenWithPolicy(sessionID string, policy harness.Effective) (agenthub.MCPEndpoint, error) {
	inv, err := h.server.Inventory()
	if err != nil {
		return agenthub.MCPEndpoint{}, err
	}
	if err := checkPolicyCoversInventory(inv, policy); err != nil {
		return agenthub.MCPEndpoint{}, err
	}
	return h.open(sessionID, newServingHolder(policy))
}

func (h *SessionHost) open(sessionID string, serving *servingHolder) (agenthub.MCPEndpoint, error) {
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
	h.bindMu.Lock()
	unlocked := false
	unlock := func() {
		if !unlocked {
			unlocked = true
			h.bindMu.Unlock()
		}
	}
	defer unlock()
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

	token := uuid.NewString()
	caller := identity.Agent(sessionID)
	ms := h.server.sdkServerWith(&caller, token, serving)
	ctx, cancel := context.WithCancel(h.ctx)
	ep := &sessionEndpoint{host: h, id: sessionID, path: path, ln: ln, serving: serving, ctx: ctx, cancel: cancel, conns: map[net.Conn]struct{}{}}
	if h.bindHook != nil {
		h.bindHook()
	}
	h.mu.Lock()
	h.endpoints[sessionID] = ep
	h.mu.Unlock()
	unlock()
	go ep.serve(ms)

	gateOwned, preApproved := h.server.GateOwnedTools(), h.server.PreApprovedTools()
	if serving != nil {
		// Narrowing only: a tool the policy hides is neither pre-approved nor
		// host-gated, and nothing here adds a tool to either list.
		snap := serving.load()
		gateOwned, preApproved = servedOnly(gateOwned, snap), servedOnly(preApproved, snap)
	}
	return agenthub.MCPEndpoint{
		UsageToken: token, GateOwnedTools: gateOwned,
		PreApprovedTools: preApproved,
		SocketPath:       path,
		Server: acp.McpServerStdio{
			Name:    "devkit",
			Command: h.proxyPath,
			Args:    []string{"--socket", path},
			Env:     []acp.EnvVariable{},
		},
		Close: ep.close,
	}, nil
}

func servedOnly(names []string, snap *servingSnapshot) []string {
	if snap == nil || len(names) == 0 {
		return names
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if snap.eff.Served(n) {
			out = append(out, n)
		}
	}
	return out
}

func (e *sessionEndpoint) serve(ms *sdk.Server) {
	for {
		conn, err := e.ln.Accept()
		if err != nil {
			return
		}
		if !e.track(conn) {
			conn.Close()
			return
		}
		go func(c net.Conn) {
			defer func() { e.untrack(c); c.Close() }()
			_ = ms.Run(e.ctx, newRWTransport(c))
		}(conn)
	}
}

func (e *sessionEndpoint) track(c net.Conn) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return false
	}
	e.conns[c] = struct{}{}
	return true
}

func (e *sessionEndpoint) untrack(c net.Conn) {
	e.mu.Lock()
	delete(e.conns, c)
	e.mu.Unlock()
}

// close ends this incarnation: its policy answers nothing from now on, its
// accepted connections are closed (not only the listener), and its socket and
// registry entry are removed only if they are still this endpoint's.
func (e *sessionEndpoint) close() {
	e.once.Do(func() {
		if e.serving != nil {
			e.serving.close()
		}
		e.mu.Lock()
		e.closed = true
		conns := make([]net.Conn, 0, len(e.conns))
		for c := range e.conns {
			conns = append(conns, c)
		}
		e.mu.Unlock()
		e.cancel()
		_ = e.ln.Close()
		for _, c := range conns {
			_ = c.Close()
		}
		if e.host.closeHook != nil {
			e.host.closeHook()
		}
		// Under bindMu, so a replacement is either fully published (and this
		// endpoint no longer owns the entry) or has not bound yet.
		e.host.bindMu.Lock()
		defer e.host.bindMu.Unlock()
		e.host.mu.Lock()
		owns := e.host.endpoints[e.id] == e
		if owns {
			delete(e.host.endpoints, e.id)
		}
		e.host.mu.Unlock()
		if owns {
			_ = os.Remove(e.path)
		}
	})
}

// Close closes every open agent endpoint (app shutdown).
func (h *SessionHost) Close() {
	h.mu.Lock()
	eps := make([]*sessionEndpoint, 0, len(h.endpoints))
	for _, ep := range h.endpoints {
		eps = append(eps, ep)
	}
	h.mu.Unlock()
	for _, ep := range eps {
		ep.close()
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
