package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"sync"
	"sync/atomic"
)

// Method names Conn routes by. Named because two of them are compared in more
// than one place, and a typo in a JSON-RPC method name fails silently: the
// message goes to UnknownMethod and the agent is refused.
const (
	MethodRequestPermission = "session/request_permission"
	MethodElicitationCreate = "elicitation/create"
	MethodSessionUpdate     = "session/update"
	MethodCancelRequest     = "$/cancel_request"
)

// Conn is a bidirectional JSON-RPC 2.0 connection over a single ReadWriteCloser
// (typically the client end of an ACP stdio transport).
type Conn struct {
	rw      io.ReadWriteCloser
	handler ClientHandler
	// elicit is handler, when it can ask a person something. Resolved once.
	elicit ElicitationHandler
	br     *bufio.Reader

	mu      sync.Mutex
	pending map[string]chan Message
	nextID  atomic.Int64
	closed  atomic.Bool
	writeMu sync.Mutex

	// inflight is every inbound request running off the read loop, keyed by
	// canonicalIDKey, so $/cancel_request can reach it.
	inflightMu sync.Mutex
	inflight   map[string]context.CancelFunc
}

// NewConn creates a Conn that owns a persistent bufio.Reader over rw.
func NewConn(rw io.ReadWriteCloser, h ClientHandler) *Conn {
	elicit, _ := h.(ElicitationHandler)
	return &Conn{
		rw:       rw,
		handler:  h,
		elicit:   elicit,
		br:       bufio.NewReader(rw),
		pending:  make(map[string]chan Message),
		inflight: make(map[string]context.CancelFunc),
	}
}

// Serve reads inbound messages until the connection closes or ctx is cancelled.
func (c *Conn) Serve(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		msg, err := c.readMessage()
		if err != nil {
			if c.closed.Load() || err == io.EOF {
				return nil
			}
			return err
		}
		c.dispatch(ctx, msg)
	}
}

func (c *Conn) readMessage() (Message, error) {
	return ReadMessage(c.br)
}

func (c *Conn) dispatch(ctx context.Context, msg Message) {
	hasMethod := msg.Method != ""
	idPresent := messageHasID(msg.ID)

	switch {
	case hasMethod && idPresent:
		c.handleRequest(ctx, msg)
	case hasMethod && !idPresent:
		c.handleNotification(ctx, msg)
	case idPresent:
		c.completePending(msg)
	}
}

func (c *Conn) handleRequest(ctx context.Context, msg Message) {
	if msg.Method == MethodElicitationCreate && c.elicit != nil {
		c.handleElicitation(ctx, msg)
		return
	}

	var (
		result json.RawMessage
		err    error
	)
	switch msg.Method {
	case MethodRequestPermission:
		result, err = c.handler.RequestPermission(ctx, msg.Params)
	default:
		result, err = c.handler.UnknownMethod(ctx, msg.Method, msg.Params)
	}
	c.respond(msg, result, err)
}

// handleElicitation answers elicitation/create off the read loop.
//
// Every other inbound message is handled on the loop, and has to be: the agent
// emits a question's tool_call update and waits for it before it sends
// elicitation/create, so dispatching updates in order is what puts the
// transcript row on screen ahead of the question. A permission blocking the
// loop is tolerable, because the turn really is stalled until it is answered.
// A question is not like that. The agent keeps working while it waits —
// subagents stream, background tasks report, the vendor lets the person type —
// and a question answered in a minute would hold every one of those updates in
// the pipe for that minute.
//
// The context derives from Serve's, so a session that closes or crashes
// cancels its open questions instead of leaking a goroutine per question.
func (c *Conn) handleElicitation(ctx context.Context, msg Message) {
	key := canonicalIDKey(msg.ID)
	reqCtx, cancel := context.WithCancel(ctx)

	c.inflightMu.Lock()
	c.inflight[key] = cancel
	c.inflightMu.Unlock()

	go func() {
		defer func() {
			c.inflightMu.Lock()
			delete(c.inflight, key)
			c.inflightMu.Unlock()
			cancel()
		}()
		result, err := c.elicit.Elicit(reqCtx, msg.Params)
		// Written even when cancelled. The agent sent $/cancel_request and has
		// already settled on its side, but its SDK keeps the request pending until
		// a response arrives; one that never comes is a leaked entry per
		// abandoned question.
		c.respond(msg, result, err)
	}()
}

// respond writes the response to an inbound request.
func (c *Conn) respond(msg Message, result json.RawMessage, err error) {
	var id any
	if uerr := json.Unmarshal(msg.ID, &id); uerr != nil {
		return
	}
	resp := Response{JSONRPC: "2.0", ID: id}
	if preferResult(msg.Method) && len(result) > 0 {
		resp.Result = result
	} else if err != nil {
		resp.Error = &RPCError{Code: -32000, Message: err.Error()}
	} else {
		resp.Result = result
	}
	_ = c.write(resp)
}

// preferResult is whether a handler's result body is the answer even when it
// also returned an error.
//
// For both, a refusal is an answer in the protocol's own vocabulary, not an RPC
// failure. A denied permission is {"outcome":{"outcome":"cancelled"}} — Invoke
// fails the turn closed internally while the agent gets a well-formed reply. A
// skipped question is {"action":"decline"}; sent as a JSON-RPC error instead,
// the vendor reads it as a crash presenting the question and aborts the tool.
func preferResult(method string) bool {
	return method == MethodRequestPermission || method == MethodElicitationCreate
}

func (c *Conn) handleNotification(ctx context.Context, msg Message) {
	switch msg.Method {
	case MethodSessionUpdate:
		_ = c.handler.SessionUpdate(ctx, msg.Params)
	case MethodCancelRequest:
		c.cancelInflight(msg.Params)
	default:
		_, _ = c.handler.UnknownMethod(ctx, msg.Method, msg.Params)
	}
}

// cancelInflight handles $/cancel_request: the agent gave up on a request it
// sent us. It used to fall through to UnknownMethod, which is harmless for a
// request that returns quickly and fatal for a question with no deadline — the
// card would stay open forever over an agent that stopped waiting.
//
// The params are {"requestId": …}; that is the only field the SDK sends
// (jsonrpc.js sendCancelRequest). A cancel for a request already answered is
// normal ordering, not news, and is dropped.
func (c *Conn) cancelInflight(params json.RawMessage) {
	var p struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if err := json.Unmarshal(params, &p); err != nil || !messageHasID(p.RequestID) {
		return
	}
	key := canonicalIDKey(p.RequestID)

	c.inflightMu.Lock()
	cancel, ok := c.inflight[key]
	c.inflightMu.Unlock()
	if ok {
		cancel()
	}
}

func (c *Conn) completePending(msg Message) {
	key := string(msg.ID)
	c.mu.Lock()
	ch, ok := c.pending[key]
	if ok {
		delete(c.pending, key)
	}
	c.mu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- msg:
	default:
	}
}

// Call sends a JSON-RPC request and waits for the matching response.
func (c *Conn) Call(ctx context.Context, method string, params any, result any) error {
	id := c.nextID.Add(1)
	idKey, err := json.Marshal(id)
	if err != nil {
		return err
	}

	var rawParams json.RawMessage
	if params != nil {
		rawParams, err = json.Marshal(params)
		if err != nil {
			return err
		}
	}

	ch := make(chan Message, 1)
	c.mu.Lock()
	c.pending[string(idKey)] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, string(idKey))
		c.mu.Unlock()
	}()

	req := Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  rawParams,
	}
	if err := c.write(req); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case msg := <-ch:
		if msg.Error != nil {
			// The error itself, so callers can errors.As it and read Data.
			rpcErr := *msg.Error
			return &rpcErr
		}
		if result == nil {
			return nil
		}
		if len(msg.Result) == 0 {
			return nil
		}
		return json.Unmarshal(msg.Result, result)
	}
}

// Notify sends a JSON-RPC notification (no response expected).
func (c *Conn) Notify(ctx context.Context, method string, params any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var rawParams json.RawMessage
	if params != nil {
		var err error
		rawParams, err = json.Marshal(params)
		if err != nil {
			return err
		}
	}
	return c.write(Notification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  rawParams,
	})
}

// Close closes the underlying ReadWriteCloser, and cancels every inbound
// request still running off the read loop: nobody can hear their answers now.
func (c *Conn) Close() error {
	c.closed.Store(true)
	c.inflightMu.Lock()
	for _, cancel := range c.inflight {
		cancel()
	}
	c.inflightMu.Unlock()
	return c.rw.Close()
}

func (c *Conn) write(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return WriteMessage(c.rw, v)
}

// canonicalIDKey is a request id as a map key that survives re-serialisation.
//
// The id on an inbound request and the one echoed back in $/cancel_request are
// two separate encodings of the same value; raw bytes can differ where the value
// does not (`7` and `7.0`, a space). Keying on the raw bytes would miss that
// match, and a missed cancel is a question that never closes. Outbound ids keep
// raw keys (completePending): those bytes are ours, written once.
func canonicalIDKey(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(out)
}

func messageHasID(id json.RawMessage) bool {
	if len(id) == 0 {
		return false
	}
	return string(id) != "null"
}
