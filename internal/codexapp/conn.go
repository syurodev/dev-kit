package codexapp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// ErrClosed is returned by every Call still waiting when the connection ended,
// and by every Call after that. The app-server dying mid-turn must fail the
// turn at once, not leave it waiting on a response that cannot come.
var ErrClosed = errors.New("codexapp: connection closed")

// ErrCallFromNotification is what Call returns when ctx is the one Handler
// was given for a Notification (or derives from it via context.WithValue /
// WithCancel / etc.): the read loop that delivers Call's response is the
// same goroutine currently blocked inside Notification, so waiting for that
// response would deadlock forever instead of erroring out.
var ErrCallFromNotification = errors.New("codexapp: Call from a Notification handler would deadlock the read loop")

// notificationKey marks a ctx as (derived from) the one passed to
// Handler.Notification, so Call can refuse to be reached through it. Only
// Notification's own ctx carries the marker — a handler that calls back into
// Conn from a goroutine of its own, with a ctx it built itself, is not
// caught, because that call does not run on the read loop and does not
// deadlock it.
type notificationKey struct{}

// Handler receives what the app-server sends on its own initiative.
type Handler interface {
	// Notification runs on the read loop, one at a time, in the order the
	// server sent them — so an item's tool_call reaches the transcript before
	// the approval that refers to it. It must not block on the Conn: calling
	// Call from here (even via ctx passed further down) would wait for a
	// response that this very goroutine is the one meant to deliver, so it
	// hangs forever — Call detects this ctx and returns
	// ErrCallFromNotification instead. Hand any such work to a goroutine of
	// its own, and pass it a ctx you build yourself rather than the one you
	// were given if it needs to call back into Conn.
	Notification(ctx context.Context, method string, params json.RawMessage)
	// Request answers a server→client request (an approval, mostly). It runs
	// on its own goroutine, so a person taking a minute to answer does not
	// hold every later notification in the pipe. ctx ends when the server
	// reports the request resolved (serverRequest/resolved), when the
	// connection closes, or when Serve's ctx does. Whatever it returns is
	// written back exactly once; a nil result is sent as {}.
	//
	// Respecting ctx is not optional. Serve does not return until every
	// Request goroutine has, so a handler that outlives ctx.Done() — waiting
	// on a person's answer without selecting on it — hangs Serve, and the
	// shutdown waiting on Serve, forever.
	Request(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, *RPCError)
}

type callResult struct {
	msg Message
	err error
}

// Conn is one app-server process's stdio.
type Conn struct {
	rw     io.ReadWriteCloser
	br     *bufio.Reader
	h      Handler
	closed atomic.Bool

	closeOnce sync.Once
	closeErr  error

	writeMu sync.Mutex
	nextID  atomic.Int64

	mu      sync.Mutex
	pending map[int64]chan callResult
	done    error // set once Serve has returned; wraps ErrClosed

	inflightMu sync.Mutex
	inflight   map[string]*inflightEntry

	// handlers tracks running Request handler goroutines, so Serve can wait
	// for all of them before returning: once Serve is back, nothing should
	// still be writing to rw on the caller's behalf.
	handlers sync.WaitGroup
}

// inflightEntry is a pointer so a handler goroutine's deferred cleanup can
// tell whether the map still holds *its* registration: the app-server is
// free to reuse a request id (a fresh request-response cycle counting from
// 0), and when it does, the old entry must be cancelled and the old
// goroutine's cleanup must not delete the new one from under it.
type inflightEntry struct {
	cancel context.CancelFunc
}

// NewConn wraps rw. Nothing is read until Serve runs.
func NewConn(rw io.ReadWriteCloser, h Handler) *Conn {
	return &Conn{
		rw:       rw,
		br:       bufio.NewReader(rw),
		h:        h,
		pending:  map[int64]chan callResult{},
		inflight: map[string]*inflightEntry{},
	}
}

// Serve reads until the stream ends, then fails every pending Call, cancels
// every running server request, and waits for every Request handler
// goroutine to return before coming back itself — so nothing is still
// writing to rw on the caller's behalf once Serve has returned. It returns
// nil on a clean end (EOF, or Close) and the read error otherwise.
//
// Cancelling ctx closes the connection (as Close would) to unblock a read
// that is already in flight, rather than having the read loop itself select
// on ctx: a goroutine blocked inside ReadMessage has no other way to give
// up. Close is idempotent, so the caller should still call it once Serve
// returns, regardless of how Serve ended.
func (c *Conn) Serve(ctx context.Context) error {
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	err := c.serve(ctx)
	c.shutdown(err)
	c.handlers.Wait()
	return err
}

func (c *Conn) serve(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		msg, err := ReadMessage(c.br)
		if err != nil {
			if errors.Is(err, io.EOF) || c.closed.Load() {
				return nil
			}
			return err
		}
		c.dispatch(ctx, msg)
	}
}

func (c *Conn) shutdown(cause error) {
	closed := ErrClosed
	if cause != nil {
		closed = fmt.Errorf("%w: %v", ErrClosed, cause)
	}
	c.mu.Lock()
	if c.done == nil {
		c.done = closed
	}
	pending := c.pending
	c.pending = map[int64]chan callResult{}
	c.mu.Unlock()
	for _, ch := range pending {
		ch <- callResult{err: closed} // buffered: never blocks
	}
	c.cancelAllInflight()
}

func (c *Conn) dispatch(ctx context.Context, msg Message) {
	switch {
	case msg.IsRequest():
		c.handleRequest(ctx, msg)
	case msg.IsNotification():
		if msg.Method == MethodServerRequestResolved {
			c.cancelResolved(msg.Params)
		}
		c.h.Notification(context.WithValue(ctx, notificationKey{}, true), msg.Method, msg.Params)
	case msg.IsResponse():
		c.complete(msg)
	}
}

func (c *Conn) handleRequest(ctx context.Context, msg Message) {
	key := canonicalID(msg.ID)
	reqCtx, cancel := context.WithCancel(ctx)
	entry := &inflightEntry{cancel: cancel}
	c.inflightMu.Lock()
	if old, ok := c.inflight[key]; ok {
		// The app-server reused an id before resolving the last request that
		// used it (its own ids restart at 0 per request-response cycle). The
		// stale handler must not keep running, and must not be the one that
		// a later serverRequest/resolved for this id reaches.
		old.cancel()
	}
	c.inflight[key] = entry
	c.inflightMu.Unlock()

	c.handlers.Add(1)
	go func() {
		defer c.handlers.Done()
		defer func() {
			c.inflightMu.Lock()
			// Only remove the entry if it is still ours: a later
			// registration for the same id already replaced and cancelled
			// us, and its own cleanup owns the delete.
			if c.inflight[key] == entry {
				delete(c.inflight, key)
			}
			c.inflightMu.Unlock()
			cancel()
		}()
		result, rpcErr := c.h.Request(reqCtx, msg.Method, msg.Params)
		// The id goes back byte for byte: the server's ids are its own, and
		// may be strings or numbers.
		resp := Message{ID: msg.ID}
		if rpcErr != nil {
			resp.Error = rpcErr
		} else {
			if len(result) == 0 {
				result = json.RawMessage(`{}`)
			}
			resp.Result = result
		}
		_ = c.write(resp)
	}()
}

func (c *Conn) cancelResolved(params json.RawMessage) {
	var p struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if json.Unmarshal(params, &p) != nil || len(p.RequestID) == 0 {
		return
	}
	c.inflightMu.Lock()
	entry := c.inflight[canonicalID(p.RequestID)]
	c.inflightMu.Unlock()
	if entry != nil {
		entry.cancel()
	}
}

func (c *Conn) cancelAllInflight() {
	c.inflightMu.Lock()
	defer c.inflightMu.Unlock()
	for _, entry := range c.inflight {
		entry.cancel()
	}
}

func (c *Conn) complete(msg Message) {
	var id int64
	if json.Unmarshal(msg.ID, &id) != nil {
		return // not one of ours: our ids are integers
	}
	c.mu.Lock()
	ch, ok := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if ok {
		ch <- callResult{msg: msg}
	}
}

// Call sends a request and waits for its response, ctx, or the end of the
// connection. nil params are sent as {} — several app-server methods reject a
// request with no params object at all. An error response comes back as
// *RPCError.
func (c *Conn) Call(ctx context.Context, method string, params, result any) error {
	if c.closed.Load() {
		return ErrClosed
	}
	if ctx.Value(notificationKey{}) != nil {
		return ErrCallFromNotification
	}
	raw := json.RawMessage(`{}`)
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		raw = b
	}
	id := c.nextID.Add(1)
	idRaw, _ := json.Marshal(id)
	ch := make(chan callResult, 1)

	c.mu.Lock()
	if c.done != nil {
		err := c.done
		c.mu.Unlock()
		return err
	}
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	if err := c.write(Message{ID: idRaw, Method: method, Params: raw}); err != nil {
		c.mu.Lock()
		done := c.done
		c.mu.Unlock()
		if done != nil {
			return done
		}
		if c.closed.Load() {
			return ErrClosed
		}
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return r.err
		}
		if r.msg.Error != nil {
			e := *r.msg.Error
			return &e
		}
		if result == nil || len(r.msg.Result) == 0 {
			return nil
		}
		return json.Unmarshal(r.msg.Result, result)
	}
}

// Notify sends a notification. nil params are omitted, as `initialized` is sent.
func (c *Conn) Notify(method string, params any) error {
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		raw = b
	}
	return c.write(Message{Method: method, Params: raw})
}

// Close cancels running server requests and closes the stream; Serve then
// returns and fails any Call still waiting. Idempotent: Serve calls it on
// every exit path via context.AfterFunc, and a caller that already knows
// Serve ended (or never called Serve at all) may also call it, so a second
// Close must not double-close rw (which some implementations error on) or
// re-cancel work that has already moved on.
func (c *Conn) Close() error {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		c.cancelAllInflight()
		c.closeErr = c.rw.Close()
	})
	return c.closeErr
}

func (c *Conn) write(m Message) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return WriteMessage(c.rw, m)
}

// canonicalID makes an id comparable across two encodings of one value — the
// id on a request and the requestId echoed in serverRequest/resolved are
// written separately, and `0` and `0.0` must meet.
func canonicalID(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(out)
}
