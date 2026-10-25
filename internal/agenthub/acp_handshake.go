package agenthub

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"devkit/internal/acp"
	"devkit/internal/sandbox"
)

// acpHandshake is the part of opening an ACP vendor session that Claude and
// opencode do identically: wire the launched process to a runtime, bound the
// handshake by the process's own life, report a failed call as what actually
// happened, and ask for a new or reloaded session. What each vendor sends
// (client capabilities, session _meta, extra directories) and what it keeps of
// the answer stay in its adapter.
type acpHandshake struct {
	vendor AgentType
	rt     *acpRuntime
	conn   *acp.Conn
	proc   *sandbox.Proc
	// ctx ends with the caller, with handshakeTimeout, or when the process
	// dies, whichever comes first.
	ctx             context.Context
	cancelHandshake context.CancelFunc
	cancelServe     context.CancelFunc
}

// startACPHandshake connects rt to the launched process and starts its serve
// loop. release runs once the process is gone and reaped. The caller defers
// end and must not use rt.conn, rt.cancelServe or rt.serveDone before this.
//
// The process outlives ctx on purpose: lifetime belongs to the runtime. Only
// the handshake calls honour ctx.
func startACPHandshake(ctx context.Context, vendor AgentType, rt *acpRuntime, launched *LaunchedProcess, onUnknownMethod func(method string, params json.RawMessage), release func()) *acpHandshake {
	proc := launched.Proc
	conn := acp.NewConn(&stdioPipe{r: launched.Stdout, w: launched.Stdin}, &acpConnHandler{rt: rt, onUnknownMethod: onUnknownMethod})
	rt.conn = conn

	serveCtx, cancelServe := context.WithCancel(context.Background())
	rt.cancelServe = cancelServe
	rt.serveDone = make(chan struct{})
	go func() {
		defer close(rt.serveDone)
		serveErr := conn.Serve(serveCtx)
		_ = conn.Close()
		_ = proc.Kill()
		_ = proc.Wait()
		release()
		rt.noteServeExit(serveErr)
	}()

	// The handshake must not be able to outlive the process it is talking to.
	//
	// acp.Conn.Call blocks in a select on ctx.Done() and the response channel.
	// The caller's ctx has no deadline here (the process is deliberately
	// detached from the request that started it), so a vendor that dies without
	// closing stdout left every call parked forever: no error, no log, nothing
	// for the UI to render but a spinner.
	//
	// serveDone closes when conn.Serve returns, which is what process death and
	// pipe EOF both look like from here, so it is the real signal. The timeout
	// is only a backstop for a process that is alive but never answers.
	hsCtx, cancelHandshake := context.WithTimeout(ctx, handshakeTimeout)
	go func() {
		select {
		case <-rt.serveDone:
			cancelHandshake()
		case <-hsCtx.Done():
		}
	}()
	return &acpHandshake{vendor: vendor, rt: rt, conn: conn, proc: proc, ctx: hsCtx, cancelHandshake: cancelHandshake, cancelServe: cancelServe}
}

// end releases the handshake's context. Deferred by the caller.
func (h *acpHandshake) end() { h.cancelHandshake() }

// abort stops the process and waits for its serve loop. The runtime is not
// usable afterwards.
func (h *acpHandshake) abort() {
	h.cancelServe()
	_ = h.conn.Close()
	_ = h.proc.Kill()
	<-h.rt.serveDone
}

// fail is the error to return for a failed call at stage, judged before the
// process is stopped (stopping it makes every failure read as "exited during
// handshake", including an error the agent answered with while still running),
// and then stops the process.
func (h *acpHandshake) fail(stage string, err error) error {
	herr := h.explain(stage, err)
	h.abort()
	return herr
}

// explain reports what actually went wrong: a cancelled handshake reads as a
// context error, which says nothing about the process having exited.
func (h *acpHandshake) explain(stage string, err error) error {
	// A signed-out agent answers session/new or session/load with an error
	// and stays alive; that is a sign-in to offer, not a crash.
	if isAuthRequired(err) {
		return vendorError(h.vendor, fmt.Errorf("%w: %s: %v", ErrAgentAuthRequired, stage, err))
	}
	// A grace period rather than a peek. A vendor that died mid-handshake
	// fails this call two ways at once — the write hits a closed pipe, and
	// the serve loop notices EOF — and which arrives first is a race the
	// scheduler decides. Checking serveDone instantaneously lost that race
	// often enough to report "write |1: broken pipe", which tells the user
	// nothing about their agent binary having exited. The two are the same
	// event microseconds apart, so waiting briefly for the one that can
	// explain itself costs nothing on a path that has already failed.
	select {
	case <-h.rt.serveDone:
		reason := stderrReason(h.proc)
		logStderrTail(string(h.vendor)+" "+stage, h.proc)
		return fmt.Errorf("%w: %s: agent process exited during handshake%s", ErrProcessFailed, stage, reason)
	case <-time.After(serveExitGrace):
		// The process is still alive: this is handshakeTimeout firing on a
		// vendor that started but never answered, or a failure that is
		// genuinely not about the process at all. Whatever it wrote to
		// stderr before going silent — a keyring/D-Bus error, a missing
		// path inside the jail — is otherwise lost the moment abort kills it,
		// and "starting" forever with no further log line is exactly what
		// that loss looks like from the outside.
		logStderrTail(string(h.vendor)+" "+stage+" (still running, "+err.Error()+")", h.proc)
		return fmt.Errorf("%w: %s: %v", ErrProcessFailed, stage, err)
	}
}

// initialize sends ACP initialize and records the extensions the agent
// advertises in its answer. On error the process is already stopped.
func (h *acpHandshake) initialize(clientCapabilities map[string]any) (acp.InitializeResult, error) {
	var res acp.InitializeResult
	if err := h.conn.Call(h.ctx, "initialize", acp.InitializeRequest{
		ProtocolVersion:    1,
		ClientCapabilities: clientCapabilities,
		ClientInfo:         map[string]any{"name": "devkit", "version": "0.0.0"},
	}, &res); err != nil {
		return res, h.fail("initialize", err)
	}
	h.rt.steering, h.rt.goalControl = parseAgentExtensions(res.Meta)
	return res, nil
}

// acpMCPServers is the session's stdio MCP servers as session/new and
// session/load carry them. Non-nil even with none: mcpServers is a required
// field.
func acpMCPServers(servers []acp.McpServerStdio) []any {
	out := make([]any, 0, len(servers))
	for _, srv := range servers {
		if srv.Env == nil {
			srv.Env = []acp.EnvVariable{} // schema requires env; nil would serialise as null
		}
		out = append(out, srv)
	}
	return out
}

// acpSessionRequest is what a vendor adds to the shared session call.
type acpSessionRequest struct {
	// Resume is the vendor's session id to reopen; empty asks for a new one.
	Resume string
	// LoadMethod is the call that reopens it; empty is session/load.
	LoadMethod string
	Cwd        string
	McpServers []any
	// Meta and AdditionalDirectories are sent as given; either may be empty.
	Meta                  json.RawMessage
	AdditionalDirectories []string
}

// acpSession is the agent's answer to session/new or session/load.
type acpSession struct {
	ID      string
	Modes   *acp.SessionModeState
	Options json.RawMessage
}

// session asks for a new session, or reopens req.Resume. The id is ours to
// begin with, so a vendor that echoes a different one has resumed something
// else; better to fail than to attach a session to the wrong conversation. On
// error the process is already stopped.
func (h *acpHandshake) session(req acpSessionRequest) (acpSession, error) {
	if req.Resume != "" {
		method := req.LoadMethod
		if method == "" {
			method = "session/load"
		}
		var res acp.SessionLoadResult
		if err := h.conn.Call(h.ctx, method, acp.SessionLoadRequest{
			SessionID:             req.Resume,
			Cwd:                   req.Cwd,
			McpServers:            req.McpServers,
			AdditionalDirectories: req.AdditionalDirectories,
			Meta:                  req.Meta,
		}, &res); err != nil {
			// Judged before the process is stopped: see fail.
			return acpSession{}, h.fail(method, err)
		}
		if res.SessionID != "" && res.SessionID != req.Resume {
			h.abort()
			return acpSession{}, fmt.Errorf("%w: %s returned %q, asked for %q", ErrProcessFailed, method, res.SessionID, req.Resume)
		}
		return acpSession{ID: req.Resume, Modes: res.Modes, Options: res.ConfigOptions}, nil
	}

	var res acp.SessionNewResult
	if err := h.conn.Call(h.ctx, "session/new", acp.SessionNewRequest{
		Cwd:                   req.Cwd,
		McpServers:            req.McpServers,
		AdditionalDirectories: req.AdditionalDirectories,
		Meta:                  req.Meta,
	}, &res); err != nil {
		return acpSession{}, h.fail("session/new", err)
	}
	if res.SessionID == "" {
		h.abort()
		return acpSession{}, fmt.Errorf("%w: empty sessionId from session/new", ErrProcessFailed)
	}
	return acpSession{ID: res.SessionID, Modes: res.Modes, Options: res.ConfigOptions}, nil
}

// probeACP starts a serve loop on a launched process, sends ACP initialize and
// stops the process. No session is created. The process exiting ends the wait
// at once; probeTimeout is only for one that runs and never answers.
func probeACP(ctx context.Context, vendor AgentType, launched *LaunchedProcess, clientCapabilities map[string]any) (acp.InitializeResult, error) {
	proc := launched.Proc
	conn := acp.NewConn(&stdioPipe{r: launched.Stdout, w: launched.Stdin}, &acpConnHandler{rt: &acpRuntime{hooks: acpRuntimeHooks{Type: vendor}}})
	serveCtx, cancelServe := context.WithCancel(context.Background())
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = conn.Serve(serveCtx)
	}()
	defer func() {
		cancelServe()
		_ = conn.Close()
		_ = proc.Kill()
		<-serveDone
		_ = proc.Wait()
	}()

	callCtx, cancelCall := context.WithTimeout(ctx, probeTimeout)
	defer cancelCall()
	go func() {
		select {
		case <-serveDone:
			cancelCall()
		case <-callCtx.Done():
		}
	}()

	var res acp.InitializeResult
	err := conn.Call(callCtx, "initialize", acp.InitializeRequest{
		ProtocolVersion:    1,
		ClientCapabilities: clientCapabilities,
		ClientInfo:         map[string]any{"name": "devkit", "version": "0.0.0"},
	}, &res)
	if err == nil {
		return res, nil
	}
	if ctx.Err() != nil {
		return acp.InitializeResult{}, ctx.Err()
	}
	select {
	case <-serveDone:
		return acp.InitializeResult{}, fmt.Errorf("%w: the agent exited before answering initialize%s", ErrProcessFailed, stderrReason(proc))
	default:
		return acp.InitializeResult{}, fmt.Errorf("%w: initialize: %v", ErrProcessFailed, err)
	}
}
