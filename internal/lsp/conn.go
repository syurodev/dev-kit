// internal/lsp/conn.go
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf16"
)

// A client for a language server spoken over a subprocess's stdio.
//
// It exists because an index is a pile of questions a compiler already knows
// the answers to, and LSP is the only stable protocol for asking them. What it
// is NOT is an editor client: there is no document synchronisation beyond
// opening a file, no diagnostics, no completion. An indexer opens everything
// once and asks structural questions.
//
// Calls are safe to make concurrently and are matched by id, so a caller can
// keep several in flight. That is not a detail — the questions are per file
// and per symbol, there are tens of thousands of them, and a round trip costs
// more than the answer does.

var (
	// ErrClosed is returned by a call made after the server has gone.
	ErrClosed = errors.New("lsp: connection closed")
)

// ServerError is a JSON-RPC error the server answered with. Kept typed so a
// caller can tell "the server said no" from "the server is gone".
type ServerError struct {
	Method  string
	Code    int
	Message string
}

func (e *ServerError) Error() string {
	return fmt.Sprintf("lsp: %s: %s (code %d)", e.Method, e.Message, e.Code)
}

// Conn is one live connection to a language server.
type Conn struct {
	cmd *exec.Cmd
	// kill cancels the command's context, which kills the server's whole
	// process group. It is Close's backstop for a server that ignores
	// shutdown, and it is why the command is started with a context of its
	// own rather than the caller's: the connection outlives any one request.
	kill   context.CancelFunc
	stdin  io.WriteCloser
	stdout io.ReadCloser

	writeMu sync.Mutex
	nextID  atomic.Int64

	mu      sync.Mutex
	pending map[int64]chan rpcResponse
	closed  bool
	// exited carries why the reader stopped, so a call that loses the race
	// with a crashing server reports the crash rather than a bare EOF.
	exited chan struct{}
	stderr *tailBuffer
}

type rpcResponse struct {
	result json.RawMessage
	err    *ServerError
}

// Start launches a language server and returns a connection to it. dir is the
// working directory; argv[0] is the program.
func Start(argv []string, dir string, env []string) (*Conn, error) {
	if len(argv) == 0 {
		return nil, errors.New("lsp: no server command")
	}
	ctx, kill := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		kill()
		return nil, fmt.Errorf("lsp: stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		kill()
		return nil, fmt.Errorf("lsp: stdout: %w", err)
	}
	// Only the tail, and only for an error message: a language server logs
	// freely and none of it is ours to keep.
	stderr := newTailBuffer(32 << 10)
	cmd.Stderr = stderr
	// A server that ignores shutdown must not keep the parent waiting on a
	// pipe forever; killing the group means no grandchild holds it open.
	cmd.WaitDelay = 5 * time.Second
	killProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		kill()
		return nil, fmt.Errorf("lsp: start %s: %w", argv[0], err)
	}

	c := &Conn{
		cmd:     cmd,
		kill:    kill,
		stdin:   stdin,
		stdout:  stdout,
		pending: map[int64]chan rpcResponse{},
		exited:  make(chan struct{}),
		stderr:  stderr,
	}
	go c.read()
	return c, nil
}

// Call sends a request and decodes the result into out, which may be nil when
// the caller does not care. It returns when the server answers, ctx is done,
// or the server dies.
func (c *Conn) Call(ctx context.Context, method string, params, out any) error {
	id := c.nextID.Add(1)
	reply := make(chan rpcResponse, 1)

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClosed
	}
	c.pending[id] = reply
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	message := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		message["params"] = params
	}
	if err := c.write(message); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.exited:
		return c.exitError(method)
	case response := <-reply:
		if response.err != nil {
			return response.err
		}
		if out == nil || len(response.result) == 0 {
			return nil
		}
		if err := json.Unmarshal(response.result, out); err != nil {
			return fmt.Errorf("lsp: decode %s result: %w", method, err)
		}
		return nil
	}
}

// Notify sends a notification, which by definition has no reply.
func (c *Conn) Notify(method string, params any) error {
	message := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		message["params"] = params
	}
	return c.write(message)
}

func (c *Conn) write(message any) error {
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("lsp: encode request: %w", err)
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return ErrClosed
	}
	if _, err := fmt.Fprintf(c.stdin, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
		return fmt.Errorf("lsp: write: %w", err)
	}
	return nil
}

func (c *Conn) exitError(method string) error {
	if tail := strings.TrimSpace(c.stderr.String()); tail != "" {
		return fmt.Errorf("lsp: %s: server exited (%s)", method, tail)
	}
	return fmt.Errorf("lsp: %s: %w", method, ErrClosed)
}

// read is the single goroutine that owns the server's output. Everything
// arriving is one of three things: an answer to one of our requests, a request
// FROM the server, or a notification.
func (c *Conn) read() {
	defer func() {
		c.mu.Lock()
		c.closed = true
		pending := c.pending
		c.pending = map[int64]chan rpcResponse{}
		c.mu.Unlock()
		for _, reply := range pending {
			close(reply)
		}
		close(c.exited)
	}()

	reader := bufio.NewReaderSize(c.stdout, 1<<20)
	for {
		payload, err := readMessage(reader)
		if err != nil {
			return
		}
		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(payload, &envelope); err != nil {
			continue
		}

		// A request from the server (registerCapability, configuration). It
		// blocks the server until answered, and a server blocked on us never
		// answers anything else — which looks exactly like a hang.
		if envelope.Method != "" {
			if len(envelope.ID) > 0 {
				_ = c.write(map[string]any{
					"jsonrpc": "2.0",
					"id":      json.RawMessage(envelope.ID),
					"result":  serverRequestReply(envelope.Method),
				})
			}
			continue
		}
		if len(envelope.ID) == 0 {
			continue
		}
		id, err := strconv.ParseInt(strings.Trim(string(envelope.ID), `"`), 10, 64)
		if err != nil {
			continue
		}

		c.mu.Lock()
		reply, waiting := c.pending[id]
		c.mu.Unlock()
		if !waiting {
			continue
		}
		response := rpcResponse{result: envelope.Result}
		if envelope.Error != nil {
			response.err = &ServerError{Code: envelope.Error.Code, Message: envelope.Error.Message}
		}
		select {
		case reply <- response:
		default:
		}
	}
}

// serverRequestReply is what we answer the server's own requests with.
//
// Nothing useful, deliberately: this client registers no dynamic capabilities
// and has no workspace configuration to offer. What matters is answering at
// all, and answering in the shape each request expects — a configuration
// request wants one entry per item, and a server handed a bare null for it can
// fail its own request handling.
func serverRequestReply(method string) any {
	switch method {
	case "workspace/configuration":
		return []any{map[string]any{}}
	default:
		return nil
	}
}

func readMessage(reader *bufio.Reader) ([]byte, error) {
	length := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			length, _ = strconv.Atoi(strings.TrimSpace(value))
		}
	}
	if length <= 0 {
		return nil, errors.New("lsp: message with no Content-Length")
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// Close asks the server to shut down and then makes sure it has.
//
// The polite sequence first, because a language server holds a program in
// memory and may have work in flight; the kill is the backstop for one that
// does not answer. Either way Close returns — a quit that waits on a wedged
// compiler is a hang the user cannot explain.
func (c *Conn) Close() error {
	c.mu.Lock()
	alreadyClosed := c.closed
	c.mu.Unlock()

	if !alreadyClosed {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = c.Call(ctx, "shutdown", nil, nil)
		cancel()
		_ = c.Notify("exit", nil)
	}
	_ = c.stdin.Close()

	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		c.kill()
		<-done
	}
	return nil
}

// utf16Offset converts a byte offset within a line to the character offset LSP
// counts in, which is UTF-16 code units. It is the difference between pointing
// at an import specifier and pointing somewhere else entirely on a line with a
// non-ASCII string earlier in it.
func utf16Offset(line string, byteOffset int) int {
	if byteOffset > len(line) {
		byteOffset = len(line)
	}
	return len(utf16.Encode([]rune(line[:byteOffset])))
}

// tailBuffer keeps the last max bytes written to it, for an error message.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func newTailBuffer(max int) *tailBuffer { return &tailBuffer{max: max} }

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := len(p)
	if len(p) >= t.max {
		t.buf = append(t.buf[:0], p[len(p)-t.max:]...)
		return n, nil
	}
	if over := len(t.buf) + len(p) - t.max; over > 0 {
		t.buf = append(t.buf[:0], t.buf[over:]...)
	}
	t.buf = append(t.buf, p...)
	return n, nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
