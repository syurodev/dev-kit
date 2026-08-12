package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// Conn is a bidirectional JSON-RPC 2.0 connection over a single ReadWriteCloser
// (typically the client end of an ACP stdio transport).
type Conn struct {
	rw      io.ReadWriteCloser
	handler ClientHandler
	br      *bufio.Reader

	mu      sync.Mutex
	pending map[string]chan Message
	nextID  atomic.Int64
	closed  atomic.Bool
	writeMu sync.Mutex
}

// NewConn creates a Conn that owns a persistent bufio.Reader over rw.
func NewConn(rw io.ReadWriteCloser, h ClientHandler) *Conn {
	return &Conn{
		rw:      rw,
		handler: h,
		br:      bufio.NewReader(rw),
		pending: make(map[string]chan Message),
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
	var (
		result json.RawMessage
		err    error
	)
	switch msg.Method {
	case "session/request_permission":
		result, err = c.handler.RequestPermission(ctx, msg.Params)
	default:
		result, err = c.handler.UnknownMethod(ctx, msg.Method, msg.Params)
	}

	var id any
	if uerr := json.Unmarshal(msg.ID, &id); uerr != nil {
		return
	}
	resp := Response{JSONRPC: "2.0", ID: id}
	// For session/request_permission, prefer a Result body when present even if
	// the handler also returns an error (Invoke fail-closed + ACP cancelled Result).
	if msg.Method == "session/request_permission" && len(result) > 0 {
		resp.Result = result
	} else if err != nil {
		resp.Error = &RPCError{Code: -32000, Message: err.Error()}
	} else {
		resp.Result = result
	}
	_ = c.write(resp)
}

func (c *Conn) handleNotification(ctx context.Context, msg Message) {
	switch msg.Method {
	case "session/update":
		_ = c.handler.SessionUpdate(ctx, msg.Params)
	default:
		_, _ = c.handler.UnknownMethod(ctx, msg.Method, msg.Params)
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
			return fmt.Errorf("acp: rpc error %d: %s", msg.Error.Code, msg.Error.Message)
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

// Close closes the underlying ReadWriteCloser.
func (c *Conn) Close() error {
	c.closed.Store(true)
	return c.rw.Close()
}

func (c *Conn) write(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return WriteMessage(c.rw, v)
}

func messageHasID(id json.RawMessage) bool {
	if len(id) == 0 {
		return false
	}
	return string(id) != "null"
}
