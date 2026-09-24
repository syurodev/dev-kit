# Codex trong Agent Hub — Kế hoạch 2a: `internal/codexapp` + runtime native

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Hai nền móng cho `CodexAdapter` (Kế hoạch 2b): một package kết nối stdio với `codex app-server` đã kiểm chứng trên dây thật, và `internal/agentruntime` biết cài, xác minh và resolve binary native của `@openai/codex`.

**Architecture:** `internal/codexapp` là package lá (không import gì trong module), cùng khuôn với `internal/acp` nhưng theo giao thức thật của app-server đo ở Giai đoạn 0: không có field `jsonrpc`, server request chạy ngoài read loop, `serverRequest/resolved` huỷ ctx của request đó, tiến trình chết thì mọi `Call` đang chờ nhận `ErrClosed`. Kiểu giao thức chỉ gồm phần vòng đời (initialize, thread, turn, account, model) và được kiểm bằng golden test so với `internal/agenthub/testdata/codex_session_capture.jsonl`. `internal/agentruntime` thêm `Agent.NativeEntry` (chạy thẳng binary, không node) và đổi `Verify` thành `Verifiers` theo từng agent — nếu không, Probe của Claude sẽ chạy trên binary Codex.

**Tech Stack:** Go 1.27, `net.Pipe` cho test kết nối, npm (chỉ khi cài), codex-cli 0.147.0.

**Spec:** `docs/superpowers/specs/2026-09-24-codex-agent-hub-design.md` §4.1, §4.2, §9 (Giai đoạn 2–3).
**Trước đó:** Kế hoạch 1 (xong). **Sau đó:** Kế hoạch 2b — `CodexAdapter` (dịch sự kiện, duyệt quyền, đăng nhập, resume), dùng API của kế hoạch này.

**Quy ước chung**

- Mọi lệnh chạy từ gốc repo `/home/ptvu3/Downloads/dev-kit-main`, nhánh `feat/codex-plan-1` (git cục bộ, **không remote, không push**).
- Commit kết thúc bằng `Co-Authored-By: <model đã viết code> <noreply@anthropic.com>`.
- Hết kế hoạch: controller chạy `.sync/export.sh`.

## File structure

| File | Trách nhiệm |
|---|---|
| `internal/codexapp/wire.go` (mới) | `Message`, `RPCError`, `ReadMessage`/`WriteMessage` — framing một dòng một JSON, không `jsonrpc` |
| `internal/codexapp/conn.go` (mới) | `Conn`, `Handler`, `ErrClosed` — Call/Notify/Serve/Close, server request ngoài read loop, huỷ theo `serverRequest/resolved` |
| `internal/codexapp/protocol.go` (mới) | Tên method + kiểu vòng đời: initialize, thread/start\|resume, turn/start\|interrupt, account/\*, model/list |
| `internal/codexapp/*_test.go` (mới) | Test đơn vị + golden so với capture thật |
| `internal/arch/arch_test.go` | Đăng ký `codexapp` là package lá |
| `internal/agentruntime/manager.go` | `Verifiers` theo agent; `inspectInstall`/`resolveAt` hỗ trợ `NativeEntry`; `firstExecutable` |
| `internal/agentruntime/agentruntime.go` | `Agent.NativeEntry`, hằng `CodexCLI`, định nghĩa agent `codex` |
| `internal/agentruntime/manager_test.go` | Test cho Verifiers + cài/resolve Codex |
| `internal/agentruntime/install_live_test.go` (mới) | Cài `@openai/codex` thật (bật bằng env) |
| `main.go`, `internal/agenthub/adapter_claude.go` | Nối `Verifiers`; sửa comment |

---

## Phần A — `internal/codexapp`

### Task 1: Framing (`wire.go`) + đăng ký package lá

**Files:**
- Create: `internal/codexapp/wire.go`
- Test: `internal/codexapp/wire_test.go`
- Modify: `internal/arch/arch_test.go` (thêm kiểm tra lá, ngay sau khối `notify`)

- [ ] **Step 1: Viết test hỏng** — tạo `internal/codexapp/wire_test.go`:

```go
package codexapp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestWriteMessageHasNoJSONRPCField(t *testing.T) {
	var buf bytes.Buffer
	err := WriteMessage(&buf, Message{ID: json.RawMessage("1"), Method: "initialize", Params: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := buf.String(), `{"id":1,"method":"initialize","params":{}}`+"\n"; got != want {
		t.Fatalf("wrote %q, want %q", got, want)
	}
}

func TestReadMessageKinds(t *testing.T) {
	// A server request with id 0 (the app-server numbers its own requests from
	// 0, overlapping the client's ids), a notification that carries a
	// "jsonrpc" field anyway (tolerated), a blank line, and a response.
	input := strings.Join([]string{
		`{"id":0,"method":"item/commandExecution/requestApproval","params":{}}`,
		`{"jsonrpc":"2.0","method":"turn/started","params":{}}`,
		``,
		`{"id":3,"result":{"turn":{"id":"t1"}}}`,
	}, "\n") + "\n"
	br := bufio.NewReader(strings.NewReader(input))

	req, err := ReadMessage(br)
	if err != nil || !req.IsRequest() || req.IsResponse() || string(req.ID) != "0" {
		t.Fatalf("request = %+v, err %v", req, err)
	}
	note, err := ReadMessage(br)
	if err != nil || !note.IsNotification() || note.Method != "turn/started" {
		t.Fatalf("notification = %+v, err %v", note, err)
	}
	resp, err := ReadMessage(br)
	if err != nil || !resp.IsResponse() || string(resp.ID) != "3" {
		t.Fatalf("response = %+v, err %v", resp, err)
	}
	if _, err := ReadMessage(br); !errors.Is(err, io.EOF) {
		t.Fatalf("after last line err = %v, want io.EOF", err)
	}
}

func TestReadMessageRejectsGarbage(t *testing.T) {
	br := bufio.NewReader(strings.NewReader("not json\n"))
	if _, err := ReadMessage(br); err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want a decode error", err)
	}
}

func TestRPCErrorMessage(t *testing.T) {
	e := &RPCError{Code: -32601, Message: "method not found"}
	if got := e.Error(); got != "codexapp: rpc error -32601: method not found" {
		t.Fatalf("Error() = %q", got)
	}
}
```

- [ ] **Step 2: Chạy, xác nhận hỏng**

Run: `go test ./internal/codexapp/ -count=1`
Expected: FAIL — `undefined: WriteMessage` (package chưa có file nguồn).

- [ ] **Step 3: Tạo `internal/codexapp/wire.go`**

```go
// Package codexapp speaks to `codex app-server` over stdio.
//
// It is the Codex counterpart of internal/acp, and deliberately a separate
// package: the app-server is not ACP. Measured on codex-cli 0.147.0
// (docs/superpowers/research/2026-09-24-agent-auth-in-sandbox.md):
//
//   - Messages have JSON-RPC's shape — id, method, params, result, error —
//     without its version field. Nothing sends "jsonrpc", so a reader that
//     required "2.0" would drop every message.
//   - The server numbers its own requests from 0, overlapping the client's
//     ids. A message is a server request because it has a method, never
//     because of its id.
//
// Nothing here imports another package in this module; internal/arch pins it
// as a leaf.
package codexapp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Message is one line on the app-server's stdio: a request, a response or a
// notification, told apart by which fields are present.
type Message struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

func (m Message) hasID() bool { return len(m.ID) > 0 && string(m.ID) != "null" }

// IsRequest reports a server→client request: it must be answered.
func (m Message) IsRequest() bool { return m.Method != "" && m.hasID() }

// IsNotification reports a message that expects no answer.
func (m Message) IsNotification() bool { return m.Method != "" && !m.hasID() }

// IsResponse reports the answer to one of our requests.
func (m Message) IsResponse() bool { return m.Method == "" && m.hasID() }

// RPCError is an error object on a response.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("codexapp: rpc error %d: %s", e.Code, e.Message)
}

// WriteMessage writes m as one line of JSON.
func WriteMessage(w io.Writer, m Message) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if bytes.IndexByte(data, '\n') >= 0 {
		return fmt.Errorf("codexapp: message must not contain a newline")
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

// ReadMessage reads the next message, skipping blank lines. A line that is not
// JSON is an error rather than skipped: the app-server writes only machine
// output on stdout, so garbage there means the stream is not what we think.
func ReadMessage(br *bufio.Reader) (Message, error) {
	for {
		line, err := br.ReadBytes('\n')
		if err != nil && (err != io.EOF || len(line) == 0) {
			return Message{}, err
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			if err == io.EOF {
				return Message{}, io.EOF
			}
			continue
		}
		var m Message
		if uerr := json.Unmarshal(line, &m); uerr != nil {
			return Message{}, fmt.Errorf("codexapp: undecodable message: %w", uerr)
		}
		return m, nil
	}
}
```

- [ ] **Step 4: Chạy, xác nhận qua**

Run: `go test ./internal/codexapp/ -count=1`
Expected: PASS.

- [ ] **Step 5: Đăng ký package lá** — trong `internal/arch/arch_test.go`, ngay sau khối:

```go
	for _, imp := range graph[pkg("notify")] {
		t.Errorf("layering violation: notify must not import %s", short(imp))
	}
```
thêm:
```go

	// codexapp is a leaf transport for `codex app-server`, like acp: the
	// adapter in agenthub owns the sandbox and the translation to ACP.
	for _, imp := range graph[pkg("codexapp")] {
		t.Errorf("layering violation: codexapp must not import %s", short(imp))
	}
```

Run: `go test ./internal/arch/ -count=1` → PASS.

- [ ] **Step 6: Commit**

```bash
gofmt -l internal/codexapp internal/arch
git add internal/codexapp/wire.go internal/codexapp/wire_test.go internal/arch/arch_test.go
git commit -m "feat(codexapp): framing for codex app-server stdio

One JSON object per line, JSON-RPC's shape without the version field,
as measured on codex-cli 0.147.0. Pinned as a leaf package.

Co-Authored-By: <model> <noreply@anthropic.com>"
```

### Task 2: Kết nối (`conn.go`)

**Files:**
- Create: `internal/codexapp/conn.go`
- Test: `internal/codexapp/conn_test.go`

- [ ] **Step 1: Viết test hỏng** — tạo `internal/codexapp/conn_test.go`:

```go
package codexapp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"testing"
	"time"
)

type testHandler struct {
	notes   chan string
	request func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, *RPCError)
}

func newTestHandler() *testHandler { return &testHandler{notes: make(chan string, 16)} }

func (h *testHandler) Notification(_ context.Context, method string, _ json.RawMessage) {
	h.notes <- method
}

func (h *testHandler) Request(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, *RPCError) {
	return h.request(ctx, method, params)
}

// fakeServer is the app-server's end of the pipe, driven by the test.
type fakeServer struct {
	conn net.Conn
	br   *bufio.Reader
}

func (s *fakeServer) send(t *testing.T, line string) {
	t.Helper()
	if _, err := s.conn.Write([]byte(line + "\n")); err != nil {
		t.Fatalf("server write: %v", err)
	}
}

func (s *fakeServer) read(t *testing.T) map[string]any {
	t.Helper()
	_ = s.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := s.br.ReadBytes('\n')
	if err != nil {
		t.Fatalf("server read: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		t.Fatalf("server got %q: %v", line, err)
	}
	return m
}

// expectSilence fails if the client writes anything within d.
func (s *fakeServer) expectSilence(t *testing.T, d time.Duration) {
	t.Helper()
	_ = s.conn.SetReadDeadline(time.Now().Add(d))
	if line, err := s.br.ReadBytes('\n'); err == nil {
		t.Fatalf("unexpected extra message %q", line)
	}
}

func newTestConn(t *testing.T, h Handler) (*Conn, *fakeServer) {
	t.Helper()
	client, server := net.Pipe()
	c := NewConn(client, h)
	done := make(chan struct{})
	go func() {
		_ = c.Serve(context.Background())
		close(done)
	}()
	t.Cleanup(func() {
		_ = c.Close()
		_ = server.Close()
		<-done
	})
	return c, &fakeServer{conn: server, br: bufio.NewReader(server)}
}

func TestCallMatchesResponsesOutOfOrder(t *testing.T) {
	c, srv := newTestConn(t, newTestHandler())
	results := make(chan string, 2)
	call := func(method string) {
		var r struct {
			V string `json:"v"`
		}
		if err := c.Call(context.Background(), method, struct{}{}, &r); err != nil {
			results <- method + "=error:" + err.Error()
			return
		}
		results <- method + "=" + r.V
	}

	go call("a")
	first := srv.read(t)
	go call("b")
	second := srv.read(t)
	if _, ok := first["jsonrpc"]; ok {
		t.Fatalf("request carries jsonrpc: %v", first)
	}
	if fmt.Sprint(first["params"]) != "map[]" {
		t.Fatalf("params = %v, want {} for struct{}{}", first["params"])
	}
	srv.send(t, fmt.Sprintf(`{"id":%v,"result":{"v":"B"}}`, second["id"]))
	srv.send(t, fmt.Sprintf(`{"id":%v,"result":{"v":"A"}}`, first["id"]))

	got := []string{<-results, <-results}
	sort.Strings(got)
	if got[0] != "a=A" || got[1] != "b=B" {
		t.Fatalf("results = %v", got)
	}
}

func TestCallReturnsRPCError(t *testing.T) {
	c, srv := newTestConn(t, newTestHandler())
	errc := make(chan error, 1)
	go func() { errc <- c.Call(context.Background(), "thread/start", struct{}{}, nil) }()
	req := srv.read(t)
	srv.send(t, fmt.Sprintf(`{"id":%v,"error":{"code":-32600,"message":"bad"}}`, req["id"]))

	var rpcErr *RPCError
	if err := <-errc; !errors.As(err, &rpcErr) || rpcErr.Code != -32600 {
		t.Fatalf("err = %v, want RPCError -32600", err)
	}
}

func TestNotificationsDeliveredInOrder(t *testing.T) {
	h := newTestHandler()
	_, srv := newTestConn(t, h)
	for _, m := range []string{"turn/started", "item/started", "item/completed"} {
		srv.send(t, `{"method":"`+m+`","params":{}}`)
	}
	for _, want := range []string{"turn/started", "item/started", "item/completed"} {
		select {
		case got := <-h.notes:
			if got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for %q", want)
		}
	}
}

func TestServerRequestRunsOffReadLoopAndIsAnsweredOnce(t *testing.T) {
	h := newTestHandler()
	release := make(chan struct{})
	h.request = func(context.Context, string, json.RawMessage) (json.RawMessage, *RPCError) {
		<-release
		return json.RawMessage(`{"decision":"accept"}`), nil
	}
	_, srv := newTestConn(t, h)

	srv.send(t, `{"id":"req-7","method":"item/fileChange/requestApproval","params":{}}`)
	srv.send(t, `{"method":"item/started","params":{}}`)
	select {
	case got := <-h.notes:
		if got != "item/started" {
			t.Fatalf("notification = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a notification was held behind a pending approval")
	}

	close(release)
	resp := srv.read(t)
	if resp["id"] != "req-7" || resp["error"] != nil {
		t.Fatalf("response = %v", resp)
	}
	if res, _ := resp["result"].(map[string]any); res["decision"] != "accept" {
		t.Fatalf("result = %v", resp["result"])
	}
	srv.expectSilence(t, 200*time.Millisecond)
}

func TestServerRequestEmptyResultIsAnObject(t *testing.T) {
	h := newTestHandler()
	h.request = func(context.Context, string, json.RawMessage) (json.RawMessage, *RPCError) { return nil, nil }
	_, srv := newTestConn(t, h)
	srv.send(t, `{"id":5,"method":"item/permissions/requestApproval","params":{}}`)
	resp := srv.read(t)
	if res, ok := resp["result"].(map[string]any); !ok || len(res) != 0 {
		t.Fatalf("result = %#v, want {}", resp["result"])
	}
}

func TestHandlerErrorBecomesErrorResponse(t *testing.T) {
	h := newTestHandler()
	h.request = func(context.Context, string, json.RawMessage) (json.RawMessage, *RPCError) {
		return nil, &RPCError{Code: -32601, Message: "unsupported"}
	}
	_, srv := newTestConn(t, h)
	srv.send(t, `{"id":1,"method":"attestation/generate","params":{}}`)
	resp := srv.read(t)
	e, _ := resp["error"].(map[string]any)
	if e == nil || e["code"] != float64(-32601) || resp["result"] != nil {
		t.Fatalf("response = %v", resp)
	}
}

func TestServerRequestResolvedCancelsHandler(t *testing.T) {
	h := newTestHandler()
	cancelled := make(chan struct{})
	h.request = func(ctx context.Context, _ string, _ json.RawMessage) (json.RawMessage, *RPCError) {
		<-ctx.Done()
		close(cancelled)
		return nil, &RPCError{Code: -32800, Message: "cancelled"}
	}
	_, srv := newTestConn(t, h)

	srv.send(t, `{"id":0,"method":"item/commandExecution/requestApproval","params":{}}`)
	srv.send(t, `{"method":"serverRequest/resolved","params":{"threadId":"t","requestId":0}}`)
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("serverRequest/resolved did not cancel the handler")
	}
	if got := <-h.notes; got != MethodServerRequestResolved {
		t.Fatalf("notification = %q, want it passed on too", got)
	}
	if resp := srv.read(t); resp["id"] != float64(0) {
		t.Fatalf("response = %v, want id 0 echoed", resp)
	}
}

func TestPendingCallsFailWhenServerExits(t *testing.T) {
	h := newTestHandler()
	handlerCtx := make(chan context.Context, 1)
	h.request = func(ctx context.Context, _ string, _ json.RawMessage) (json.RawMessage, *RPCError) {
		handlerCtx <- ctx
		<-ctx.Done()
		return nil, nil
	}
	c, srv := newTestConn(t, h)

	srv.send(t, `{"id":9,"method":"item/fileChange/requestApproval","params":{}}`)
	inflight := <-handlerCtx

	errc := make(chan error, 1)
	go func() { errc <- c.Call(context.Background(), "turn/start", struct{}{}, nil) }()
	srv.read(t)
	_ = srv.conn.Close() // the app-server died mid-turn

	select {
	case err := <-errc:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("pending Call err = %v, want ErrClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a pending Call outlived the connection")
	}
	select {
	case <-inflight.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("an in-flight server request outlived the connection")
	}
	if err := c.Call(context.Background(), "turn/start", struct{}{}, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("Call after close err = %v, want ErrClosed", err)
	}
}

func TestNotifyOmitsParamsWhenNil(t *testing.T) {
	c, srv := newTestConn(t, newTestHandler())
	errc := make(chan error, 1)
	go func() { errc <- c.Notify(MethodInitialized, nil) }()
	got := srv.read(t)
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	if got["method"] != MethodInitialized || got["id"] != nil {
		t.Fatalf("notification = %v", got)
	}
	if _, ok := got["params"]; ok {
		t.Fatalf("params present on %v", got)
	}
}
```

> `MethodInitialized` và `MethodServerRequestResolved` được định nghĩa ở Task 3. Để Task 2 tự biên dịch được, Step 3 dưới đây khai báo chúng tạm trong `conn.go`; Task 3 **chuyển** chúng sang `protocol.go` (xem Task 3 Step 3).

- [ ] **Step 2: Chạy, xác nhận hỏng**

Run: `go test ./internal/codexapp/ -count=1`
Expected: FAIL — `undefined: NewConn`, `Handler`, `ErrClosed`, …

- [ ] **Step 3: Tạo `internal/codexapp/conn.go`**

```go
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

// Declared here so this file stands alone; protocol.go holds the rest.
const (
	MethodInitialized           = "initialized"
	MethodServerRequestResolved = "serverRequest/resolved"
)

// ErrClosed is returned by every Call still waiting when the connection ended,
// and by every Call after that. The app-server dying mid-turn must fail the
// turn at once, not leave it waiting on a response that cannot come.
var ErrClosed = errors.New("codexapp: connection closed")

// Handler receives what the app-server sends on its own initiative.
type Handler interface {
	// Notification runs on the read loop, one at a time, in the order the
	// server sent them — so an item's tool_call reaches the transcript before
	// the approval that refers to it.
	Notification(ctx context.Context, method string, params json.RawMessage)
	// Request answers a server→client request (an approval, mostly). It runs
	// on its own goroutine, so a person taking a minute to answer does not
	// hold every later notification in the pipe. ctx ends when the server
	// reports the request resolved (serverRequest/resolved), when the
	// connection closes, or when Serve's ctx does. Whatever it returns is
	// written back exactly once; a nil result is sent as {}.
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

	writeMu sync.Mutex
	nextID  atomic.Int64

	mu      sync.Mutex
	pending map[int64]chan callResult
	done    error // set once Serve has returned; wraps ErrClosed

	inflightMu sync.Mutex
	inflight   map[string]context.CancelFunc
}

// NewConn wraps rw. Nothing is read until Serve runs.
func NewConn(rw io.ReadWriteCloser, h Handler) *Conn {
	return &Conn{
		rw:       rw,
		br:       bufio.NewReader(rw),
		h:        h,
		pending:  map[int64]chan callResult{},
		inflight: map[string]context.CancelFunc{},
	}
}

// Serve reads until the stream ends, then fails every pending Call and
// cancels every running server request. It returns nil on a clean end (EOF,
// or Close) and the read error otherwise.
func (c *Conn) Serve(ctx context.Context) error {
	err := c.serve(ctx)
	c.shutdown(err)
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
		c.h.Notification(ctx, msg.Method, msg.Params)
	case msg.IsResponse():
		c.complete(msg)
	}
}

func (c *Conn) handleRequest(ctx context.Context, msg Message) {
	key := canonicalID(msg.ID)
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
	cancel := c.inflight[canonicalID(p.RequestID)]
	c.inflightMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (c *Conn) cancelAllInflight() {
	c.inflightMu.Lock()
	defer c.inflightMu.Unlock()
	for _, cancel := range c.inflight {
		cancel()
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
// returns and fails any Call still waiting.
func (c *Conn) Close() error {
	c.closed.Store(true)
	c.cancelAllInflight()
	return c.rw.Close()
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
```

- [ ] **Step 4: Chạy, xác nhận qua** (kèm race detector — Conn là code đồng thời)

Run: `go test ./internal/codexapp/ -count=1 -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/codexapp
git add internal/codexapp/conn.go internal/codexapp/conn_test.go
git commit -m "feat(codexapp): Conn for codex app-server

Server requests run off the read loop and are answered exactly once;
serverRequest/resolved cancels the handler's ctx; a dead process fails
every pending Call with ErrClosed instead of leaving it waiting.

Co-Authored-By: <model> <noreply@anthropic.com>"
```

### Task 3: Kiểu giao thức vòng đời (`protocol.go`) + golden test

**Files:**
- Create: `internal/codexapp/protocol.go`
- Modify: `internal/codexapp/conn.go` (xoá khối `const` tạm ở đầu file — hai hằng chuyển sang `protocol.go`)
- Test: `internal/codexapp/protocol_test.go`

- [ ] **Step 1: Viết test hỏng** — tạo `internal/codexapp/protocol_test.go`. Test đọc capture thật của Kế hoạch 1 và so **params DevKit sẽ gửi** với những gì probe đã gửi (và Codex 0.147.0 đã nhận), rồi giải mã các response thật.

```go
package codexapp

import (
	"bufio"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

type captureLine struct {
	Dir     string  `json:"dir"`
	Session string  `json:"session"`
	Msg     Message `json:"msg"`
}

func loadCapture(t *testing.T) []captureLine {
	t.Helper()
	f, err := os.Open("../agenthub/testdata/codex_session_capture.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []captureLine
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 8<<20)
	for sc.Scan() {
		var l captureLine
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatal(err)
		}
		out = append(out, l)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// sent returns the params of the first request with method in session, and
// the result of its response.
func sent(t *testing.T, lines []captureLine, session, method string) (params, result json.RawMessage) {
	t.Helper()
	for i, l := range lines {
		if l.Session != session || l.Dir != "out" || l.Msg.Method != method {
			continue
		}
		for _, r := range lines[i+1:] {
			if r.Session == session && r.Dir == "in" && r.Msg.IsResponse() && string(r.Msg.ID) == string(l.Msg.ID) {
				return l.Msg.Params, r.Msg.Result
			}
		}
		return l.Msg.Params, nil
	}
	t.Fatalf("no %s request in session %s", method, session)
	return nil, nil
}

func sameJSON(t *testing.T, what string, got any, want json.RawMessage) {
	t.Helper()
	gotRaw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var g, w any
	_ = json.Unmarshal(gotRaw, &g)
	_ = json.Unmarshal(want, &w)
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("%s:\n got  %s\n want %s", what, gotRaw, want)
	}
}

func TestInitializeMatchesCapture(t *testing.T) {
	lines := loadCapture(t)
	params, result := sent(t, lines, "main", MethodInitialize)
	sameJSON(t, "initialize params", InitializeParams{
		ClientInfo:   ClientInfo{Name: "devkit-probe", Title: "DevKit probe", Version: "0"},
		Capabilities: &InitializeCapabilities{},
	}, params)

	var resp InitializeResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.CodexHome != "/probe/home" || resp.PlatformOS != "linux" || resp.UserAgent == "" {
		t.Fatalf("initialize response = %+v", resp)
	}
}

func TestThreadStartMatchesCapture(t *testing.T) {
	lines := loadCapture(t)
	params, result := sent(t, lines, "main", MethodThreadStart)
	sameJSON(t, "thread/start params", ThreadStartParams{
		Cwd: "/probe/repo", ApprovalPolicy: ApprovalUntrusted, ApprovalsReviewer: ReviewerUser,
	}, params)

	var resp ThreadStartResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Thread.ID == "" || resp.Model == "" {
		t.Fatalf("thread/start response = %+v", resp)
	}
}

func TestTurnStartMatchesCapture(t *testing.T) {
	lines := loadCapture(t)
	params, result := sent(t, lines, "main", MethodTurnStart)
	var captured struct {
		ThreadID string `json:"threadId"`
		Input    []struct {
			Text string `json:"text"`
		} `json:"input"`
	}
	if err := json.Unmarshal(params, &captured); err != nil {
		t.Fatal(err)
	}
	sandbox := ExternalSandbox(true)
	sameJSON(t, "turn/start params", TurnStartParams{
		ThreadID:          captured.ThreadID,
		Input:             []UserInput{TextInput(captured.Input[0].Text)},
		ApprovalPolicy:    ApprovalUntrusted,
		ApprovalsReviewer: ReviewerUser,
		SandboxPolicy:     &sandbox,
	}, params)

	var resp TurnStartResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Turn.ID == "" || resp.Turn.Status != TurnInProgress {
		t.Fatalf("turn/start response = %+v", resp)
	}
}

func TestTurnInterruptMatchesCapture(t *testing.T) {
	lines := loadCapture(t)
	params, _ := sent(t, lines, "interrupt", MethodTurnInterrupt)
	var captured TurnInterruptParams
	if err := json.Unmarshal(params, &captured); err != nil {
		t.Fatal(err)
	}
	if captured.ThreadID == "" || captured.TurnID == "" {
		t.Fatalf("captured interrupt = %+v", captured)
	}
	sameJSON(t, "turn/interrupt params", TurnInterruptParams{ThreadID: captured.ThreadID, TurnID: captured.TurnID}, params)
}

func TestAccountAndLoginShapes(t *testing.T) {
	// Response bodies measured in Phase 0 (research doc), not in the capture.
	var acct GetAccountResponse
	if err := json.Unmarshal([]byte(`{"account":null,"requiresOpenaiAuth":true}`), &acct); err != nil {
		t.Fatal(err)
	}
	if acct.Account != nil || !acct.RequiresOpenAIAuth {
		t.Fatalf("signed-out account = %+v", acct)
	}
	if err := json.Unmarshal([]byte(`{"account":{"type":"chatgpt","email":"user@example.com","planType":"plus"},"requiresOpenaiAuth":true}`), &acct); err != nil {
		t.Fatal(err)
	}
	if acct.Account == nil || acct.Account.Type != AccountChatGPT || acct.Account.PlanType == nil || *acct.Account.PlanType != "plus" {
		t.Fatalf("signed-in account = %+v", acct)
	}

	sameJSON(t, "browser login params", LoginStartParams{Type: LoginChatGPT}, json.RawMessage(`{"type":"chatgpt"}`))
	sameJSON(t, "api key login params", LoginStartParams{Type: LoginAPIKey, APIKey: "sk-test"}, json.RawMessage(`{"type":"apiKey","apiKey":"sk-test"}`))

	var login LoginStartResponse
	if err := json.Unmarshal([]byte(`{"type":"chatgptDeviceCode","loginId":"l1","verificationUrl":"https://auth.openai.com/codex/device","userCode":"ABCD-EFGH"}`), &login); err != nil {
		t.Fatal(err)
	}
	if login.LoginID != "l1" || login.VerificationURL == "" || login.UserCode != "ABCD-EFGH" {
		t.Fatalf("device login = %+v", login)
	}
	var done AccountLoginCompletedNotification
	if err := json.Unmarshal([]byte(`{"loginId":"l1","success":true,"error":null,"onboardingEntrypoint":null}`), &done); err != nil {
		t.Fatal(err)
	}
	if !done.Success || done.LoginID == nil || *done.LoginID != "l1" {
		t.Fatalf("login completed = %+v", done)
	}
}
```

- [ ] **Step 2: Chạy, xác nhận hỏng**

Run: `go test ./internal/codexapp/ -run 'Capture|Shapes' -count=1`
Expected: FAIL — `undefined: InitializeParams`, …

- [ ] **Step 3: Tạo `internal/codexapp/protocol.go`, và xoá khối const tạm ở đầu `conn.go`**

Trong `conn.go` xoá đúng khối này **và dòng trống ngay sau nó** (hai hằng giờ ở `protocol.go`; để lại dòng trống thì `gofmt -l` sẽ báo file):
```go
// Declared here so this file stands alone; protocol.go holds the rest.
const (
	MethodInitialized           = "initialized"
	MethodServerRequestResolved = "serverRequest/resolved"
)
```

Tạo `internal/codexapp/protocol.go`:

```go
package codexapp

import "encoding/json"

// The lifecycle half of the app-server protocol: what the adapter calls to
// start, drive and stop a session, and to sign in. Field names are the
// app-server's, from `codex app-server generate-ts` for the pinned version
// (0.147.0); protocol_test.go holds them against a real recorded session.
// Item and turn notifications — the translator's input — are added with the
// translator.

const (
	MethodInitialize            = "initialize"
	MethodInitialized           = "initialized"
	MethodThreadStart           = "thread/start"
	MethodThreadResume          = "thread/resume"
	MethodTurnStart             = "turn/start"
	MethodTurnInterrupt         = "turn/interrupt"
	MethodAccountRead           = "account/read"
	MethodAccountLoginStart     = "account/login/start"
	MethodAccountLoginCancel    = "account/login/cancel"
	MethodAccountLogout         = "account/logout"
	MethodAccountLoginCompleted = "account/login/completed"
	MethodAccountUpdated        = "account/updated"
	MethodModelList             = "model/list"
	MethodServerRequestResolved = "serverRequest/resolved"
)

// AskForApproval's string forms. The granular object form is not used.
const (
	ApprovalUntrusted = "untrusted"
	ApprovalOnRequest = "on-request"
	ApprovalNever     = "never"
)

// ReviewerUser routes every approval to the client. DevKit always sends it:
// the default is "user" today, and a project config can ask for
// "auto_review" — per-turn parameters win over that (Phase 0).
const ReviewerUser = "user"

// Turn statuses.
const (
	TurnInProgress  = "inProgress"
	TurnCompleted   = "completed"
	TurnInterrupted = "interrupted"
	TurnFailed      = "failed"
)

// Account and login types.
const (
	AccountChatGPT  = "chatgpt"
	AccountAPIKey   = "apiKey"
	LoginChatGPT    = "chatgpt"
	LoginDeviceCode = "chatgptDeviceCode"
	LoginAPIKey     = "apiKey"
)

type ClientInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

type InitializeCapabilities struct {
	ExperimentalAPI    bool `json:"experimentalApi"`
	RequestAttestation bool `json:"requestAttestation"`
}

type InitializeParams struct {
	ClientInfo   ClientInfo              `json:"clientInfo"`
	Capabilities *InitializeCapabilities `json:"capabilities"`
}

type InitializeResponse struct {
	UserAgent      string `json:"userAgent"`
	CodexHome      string `json:"codexHome"`
	PlatformFamily string `json:"platformFamily"`
	PlatformOS     string `json:"platformOs"`
}

// ThreadStartParams deliberately has no sandbox field: the app-server's is a
// SandboxMode (read-only / workspace-write / danger-full-access) with no
// external variant. externalSandbox is sent on every turn/start instead.
type ThreadStartParams struct {
	Cwd               string `json:"cwd,omitempty"`
	ApprovalPolicy    string `json:"approvalPolicy,omitempty"`
	ApprovalsReviewer string `json:"approvalsReviewer,omitempty"`
	Model             string `json:"model,omitempty"`
}

type ThreadResumeParams struct {
	ThreadID          string `json:"threadId"`
	Cwd               string `json:"cwd,omitempty"`
	ApprovalPolicy    string `json:"approvalPolicy,omitempty"`
	ApprovalsReviewer string `json:"approvalsReviewer,omitempty"`
	Model             string `json:"model,omitempty"`
}

type Thread struct {
	ID   string  `json:"id"`
	Name *string `json:"name"`
	Cwd  string  `json:"cwd"`
}

// ThreadStartResponse is also thread/resume's response: same fields.
type ThreadStartResponse struct {
	Thread          Thread  `json:"thread"`
	Model           string  `json:"model"`
	ReasoningEffort *string `json:"reasoningEffort"`
}

// UserInput is one element of turn/start's input, already encoded.
type UserInput = json.RawMessage

// TextInput is a text prompt. text_elements is required on the wire even when
// empty (measured).
func TextInput(text string) UserInput {
	b, _ := json.Marshal(struct {
		Type         string   `json:"type"`
		Text         string   `json:"text"`
		TextElements []string `json:"text_elements"`
	}{Type: "text", Text: text, TextElements: []string{}})
	return b
}

type SandboxPolicy struct {
	Type          string `json:"type"`
	NetworkAccess string `json:"networkAccess,omitempty"`
}

// ExternalSandbox tells Codex that something outside it — DevKit's jail —
// already confines the process, so it must not sandbox commands itself.
func ExternalSandbox(network bool) SandboxPolicy {
	access := "restricted"
	if network {
		access = "enabled"
	}
	return SandboxPolicy{Type: "externalSandbox", NetworkAccess: access}
}

type TurnStartParams struct {
	ThreadID          string         `json:"threadId"`
	Input             []UserInput    `json:"input"`
	ApprovalPolicy    string         `json:"approvalPolicy,omitempty"`
	ApprovalsReviewer string         `json:"approvalsReviewer,omitempty"`
	SandboxPolicy     *SandboxPolicy `json:"sandboxPolicy,omitempty"`
	Model             string         `json:"model,omitempty"`
	Effort            string         `json:"effort,omitempty"`
}

type TurnError struct {
	Message           string          `json:"message"`
	CodexErrorInfo    json.RawMessage `json:"codexErrorInfo"`
	AdditionalDetails *string         `json:"additionalDetails"`
}

type Turn struct {
	ID     string     `json:"id"`
	Status string     `json:"status"`
	Error  *TurnError `json:"error"`
}

type TurnStartResponse struct {
	Turn Turn `json:"turn"`
}

type TurnInterruptParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
}

type Account struct {
	Type     string  `json:"type"`
	Email    *string `json:"email"`
	PlanType *string `json:"planType"`
}

type GetAccountResponse struct {
	Account            *Account `json:"account"`
	RequiresOpenAIAuth bool     `json:"requiresOpenaiAuth"`
}

type LoginStartParams struct {
	Type   string `json:"type"`
	APIKey string `json:"apiKey,omitempty"`
}

type LoginStartResponse struct {
	Type            string `json:"type"`
	LoginID         string `json:"loginId,omitempty"`
	AuthURL         string `json:"authUrl,omitempty"`
	VerificationURL string `json:"verificationUrl,omitempty"`
	UserCode        string `json:"userCode,omitempty"`
}

type CancelLoginParams struct {
	LoginID string `json:"loginId"`
}

type AccountLoginCompletedNotification struct {
	LoginID *string `json:"loginId"`
	Success bool    `json:"success"`
	Error   *string `json:"error"`
}

type ModelListParams struct {
	Cursor *string `json:"cursor,omitempty"`
	Limit  *int    `json:"limit,omitempty"`
}

type Model struct {
	ID                     string `json:"id"`
	Model                  string `json:"model"`
	DisplayName            string `json:"displayName"`
	Description            string `json:"description"`
	Hidden                 bool   `json:"hidden"`
	IsDefault              bool   `json:"isDefault"`
	DefaultReasoningEffort string `json:"defaultReasoningEffort"`
}

type ModelListResponse struct {
	Data       []Model `json:"data"`
	NextCursor *string `json:"nextCursor"`
}

type ServerRequestResolvedNotification struct {
	ThreadID  string          `json:"threadId"`
	RequestID json.RawMessage `json:"requestId"`
}
```

- [ ] **Step 4: Chạy, xác nhận qua**

Run: `go test ./internal/codexapp/ -count=1 -race && go test ./internal/arch/ -count=1`
Expected: PASS cả hai.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/codexapp
git add internal/codexapp/protocol.go internal/codexapp/protocol_test.go internal/codexapp/conn.go
git commit -m "feat(codexapp): lifecycle protocol types, held to a real session

initialize, thread/start|resume, turn/start|interrupt, account/*,
model/list. Golden tests compare the params DevKit will send with what
codex-cli 0.147.0 accepted in the Phase 0 capture.

Co-Authored-By: <model> <noreply@anthropic.com>"
```

---

## Phần B — `internal/agentruntime`

### Task 4: `Verify` → `Verifiers` theo từng agent

**Vì sao:** `Manager.Verify` là một hàm dùng chung, và `main.go` gán `claudeAdapter.Probe`. Cài Codex sẽ chạy Probe ACP của Claude trên binary Codex và luôn thất bại.

**Files:**
- Modify: `internal/agentruntime/manager.go` (field ~dòng 39–42; Install ~dòng 267–279)
- Modify: `internal/agentruntime/agentruntime.go` (package doc, dòng cuối)
- Modify: `internal/agentruntime/manager_test.go` (`TestInstallVerifyFailureKeepsCurrent`)
- Modify: `main.go` (dòng `runtimes.Verify = claudeAdapter.Probe`)
- Modify: `internal/agenthub/adapter_claude.go` (comment của `Probe`)

- [ ] **Step 1: Sửa test cho API mới (hỏng vì chưa có `Verifiers`)** — trong `TestInstallVerifyFailureKeepsCurrent`, thay

```go
	tm.Verify = func(_ context.Context, rt Resolved) error {
		verified = rt
		return errors.New("initialize: agent process exited")
	}
```
bằng
```go
	tm.Verifiers = map[string]func(context.Context, Resolved) error{
		ClaudeACP: func(_ context.Context, rt Resolved) error {
			verified = rt
			return errors.New("initialize: agent process exited")
		},
	}
```

Run: `go test ./internal/agentruntime/ -count=1` → FAIL: `tm.Verifiers undefined`.

- [ ] **Step 2: Field** — trong `internal/agentruntime/manager.go` thay

```go
	// Verify proves a staged install runs before it is activated. agenthub
	// supplies a sandboxed ACP initialize under the same policy a real session
	// gets; nil skips the check, which only tests should do.
	Verify func(ctx context.Context, rt Resolved) error
```
bằng
```go
	// Verifiers prove a staged install runs before it is activated, one per
	// agent: agenthub supplies each vendor's own handshake under the jail a
	// real session gets. Per agent because a probe is vendor-specific — Claude's
	// ACP initialize run against the Codex binary would fail every install of
	// it. An agent with no verifier is checked statically only, and its
	// install.json says verified.handshake=false.
	Verifiers map[string]func(ctx context.Context, rt Resolved) error
```

- [ ] **Step 3: Install** — thay

```go
	if m.Verify != nil {
		rt, err := m.resolveAt(ctx, agent, staging, mf)
		if err != nil {
			return err
		}
		if err := m.Verify(ctx, rt); err != nil {
```
bằng
```go
	if verify := m.Verifiers[agentID]; verify != nil {
		rt, err := m.resolveAt(ctx, agent, staging, mf)
		if err != nil {
			return err
		}
		if err := verify(ctx, rt); err != nil {
```
(phần còn lại của khối giữ nguyên).

- [ ] **Step 4: Comment** — `internal/agentruntime/agentruntime.go`: `// install works, and agenthub hands that proof in as Manager.Verify.` → `// install works, and agenthub hands that proof in as Manager.Verifiers.`
`internal/agenthub/adapter_claude.go`: `// ACP initialize and stops it. It is agentruntime.Manager.Verify: the proof` → `// ACP initialize and stops it. It is agentruntime.Manager.Verifiers[ClaudeACP]: the proof`
(nếu dòng bị tràn, gói lại comment cho gọn — không đổi nội dung khác).

- [ ] **Step 5: `main.go`** — thay `	runtimes.Verify = claudeAdapter.Probe` bằng

```go
	runtimes.Verifiers = map[string]func(context.Context, agentruntime.Resolved) error{
		agentruntime.ClaudeACP: claudeAdapter.Probe,
	}
```

- [ ] **Step 6: Chạy**

```bash
go test ./internal/agentruntime/ ./internal/agenthub/ -count=1 && go build -tags server -o /dev/null . && go vet -tags server ./...
```
Expected: PASS, build/vet sạch.

- [ ] **Step 7: Commit**

```bash
gofmt -l internal/agentruntime internal/agenthub main.go
git add internal/agentruntime/manager.go internal/agentruntime/agentruntime.go internal/agentruntime/manager_test.go internal/agenthub/adapter_claude.go main.go
git commit -m "refactor(agentruntime): verifiers per agent

A single Verify ran Claude's ACP probe against whatever was installed;
the Codex binary would fail every install. Agents without a verifier
are checked statically and recorded as not handshake-verified.

Co-Authored-By: <model> <noreply@anthropic.com>"
```

### Task 5: `NativeEntry` + agent `codex`

**Vì sao thêm:** `@openai/codex` là binary native trong gói theo nền tảng (`node_modules/@openai/codex-<platform>/vendor/<triple>/bin/codex`, ~300 MB); `bin/codex.js` chỉ là launcher. DevKit chạy thẳng binary — không cần node lúc chạy, và không có node trong jail thì ít thứ phải cấp quyền hơn. Hai lỗi phải tránh: (1) `inspectInstall` hiện **bắt buộc** binary Claude đi kèm với mọi agent (vì `claudeSource` mặc định "bundled"); (2) `mf.Entry` bị gán lại bằng `agent.Entry` rỗng.

**Files:**
- Modify: `internal/agentruntime/agentruntime.go` (`Agent`, `agents`, hằng mới)
- Modify: `internal/agentruntime/manager.go` (`Install` gọi `inspectInstall`; thay toàn bộ `inspectInstall`; thêm `firstExecutable`; nhánh native trong `resolveAt`)
- Test: `internal/agentruntime/manager_test.go`

- [ ] **Step 1: Viết test hỏng** — thêm `"os/exec"` vào import của `manager_test.go`, rồi thêm vào cuối file:

```go
// writeFakeCodexInstall writes the tree `npm install @openai/codex` leaves,
// with the platform package hoisted or nested under @openai/codex (npm does
// either). The "binary" is a script that prints what the real one does for
// --version.
func writeFakeCodexInstall(t *testing.T, prefix, version string, nested, withBinary bool) {
	t.Helper()
	platform := "node_modules/@openai/codex-linux-x64"
	if nested {
		platform = "node_modules/@openai/codex/node_modules/@openai/codex-linux-x64"
	}
	files := map[string]string{
		"package-lock.json":                       "{}",
		"node_modules/@openai/codex/package.json": `{"version":"` + version + `"}`,
		"node_modules/@openai/codex/bin/codex.js": "",
	}
	if withBinary {
		files[platform+"/vendor/x86_64-unknown-linux-musl/bin/"+exeName("codex")] = "#!/bin/sh\necho codex-cli " + version + "\n"
	}
	for rel, body := range files {
		path := filepath.Join(prefix, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func codexTestManager(t *testing.T, nested, withBinary bool) *testManager {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake codex binary is a shell script")
	}
	tm := newTestManager(t)
	tm.npm = func(_ context.Context, prefix, spec string, _ func(string)) error {
		if !strings.HasPrefix(spec, "@openai/codex@") {
			t.Errorf("npm spec = %q, want @openai/codex@<version>", spec)
		}
		writeFakeCodexInstall(t, prefix, spec[strings.LastIndex(spec, "@")+1:], nested, withBinary)
		return nil
	}
	return tm
}

func TestInstallCodexRunsItsNativeBinary(t *testing.T) {
	tested := agents[CodexCLI].TestedVersion
	for _, nested := range []bool{false, true} {
		t.Run(fmt.Sprintf("nested=%v", nested), func(t *testing.T) {
			tm := codexTestManager(t, nested, true)
			claudeProbed := false
			tm.Verifiers = map[string]func(context.Context, Resolved) error{
				ClaudeACP: func(context.Context, Resolved) error { claudeProbed = true; return nil },
			}
			if err := tm.Install(context.Background(), CodexCLI, ""); err != nil {
				t.Fatal(err)
			}
			if claudeProbed {
				t.Fatal("Claude's probe ran against the Codex install")
			}

			rt, err := tm.Resolve(CodexCLI)
			if err != nil {
				t.Fatal(err)
			}
			versionDir := resolved(t, filepath.Join(tm.agentDir(CodexCLI), tested))
			if !strings.HasPrefix(rt.Command, versionDir+string(filepath.Separator)) || filepath.Base(rt.Command) != exeName("codex") {
				t.Fatalf("command = %q, want the codex binary under %s", rt.Command, versionDir)
			}
			if len(rt.Args) != 0 {
				t.Fatalf("args = %v, want none: the binary is the entry", rt.Args)
			}
			if !slices.Equal(rt.ReadOnlyPaths, []string{versionDir}) {
				t.Fatalf("ReadOnlyPaths = %v, want only the version directory (no node)", rt.ReadOnlyPaths)
			}
			if rt.Version != tested || rt.Env["PATH"] == "" {
				t.Fatalf("resolved = %+v", rt)
			}
			out, err := exec.Command(rt.Command, "--version").Output()
			if err != nil || !strings.Contains(string(out), "codex-cli "+tested) {
				t.Fatalf("running %s: %q, %v", rt.Command, out, err)
			}
		})
	}
}

func TestInstallCodexWithoutPlatformBinaryFails(t *testing.T) {
	tm := codexTestManager(t, false, false)
	err := tm.Install(context.Background(), CodexCLI, "")
	if !errors.Is(err, ErrInstallFailed) || !strings.Contains(err.Error(), "no codex executable") {
		t.Fatalf("err = %v, want ErrInstallFailed naming the missing executable", err)
	}
	if got := tm.readCurrent(CodexCLI); got.Version != "" {
		t.Fatalf("current = %+v after a failed install", got)
	}
}

func TestResolveRefusesEntryOutsideVersionDir(t *testing.T) {
	tm := codexTestManager(t, false, true)
	if err := tm.Install(context.Background(), CodexCLI, ""); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(tm.agentDir(CodexCLI), agents[CodexCLI].TestedVersion, "install.json")
	var mf map[string]any
	if err := readJSON(manifest, &mf); err != nil {
		t.Fatal(err)
	}
	mf["entry"] = "../../../bin/sh"
	if err := writeJSON(manifest, mf); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.Resolve(CodexCLI); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("Resolve with an escaping entry = %v, want ErrNotInstalled", err)
	}
}
```

- [ ] **Step 2: Chạy, xác nhận hỏng**

Run: `go test ./internal/agentruntime/ -run Codex -count=1`
Expected: FAIL — `undefined: CodexCLI`.

- [ ] **Step 3: `agentruntime.go`** — trong `type Agent struct`, ngay sau field `BundledClaude []string` thêm:

```go
	// NativeEntry lists globs, relative to a version directory, where npm puts
	// the agent's own executable. When set, the agent is that binary: Entry and
	// node are unused, and Resolved.Command is the match. npm picks the
	// platform package by os/cpu, so which one exists is decided at install —
	// the same reason BundledClaude is a list.
	NativeEntry []string
```

Ngay dưới `const ClaudeACP = "claude-acp"` thêm:

```go
// CodexCLI is OpenAI's Codex CLI, run as `codex app-server`. Not an ACP
// adapter and not in the ACP registry, so no "latest" is offered: the pinned
// TestedVersion is what installs.
const CodexCLI = "codex"
```

Trong map `agents`, sau mục `ClaudeACP: {...},` thêm:

```go
	CodexCLI: {
		ID:            CodexCLI,
		Package:       "@openai/codex",
		TestedVersion: "0.147.0",
		NativeEntry: []string{
			"node_modules/@openai/codex-*/vendor/*/bin/" + exeName("codex"),
			// npm nests the platform package when it cannot hoist it; the global
			// install on the machine this was measured on did exactly that.
			"node_modules/@openai/codex/node_modules/@openai/codex-*/vendor/*/bin/" + exeName("codex"),
		},
	},
```

- [ ] **Step 4: `manager.go` — lời gọi trong `Install`**: thay

```go
	mf, err := inspectInstall(agent, staging, version, m.claudeSource(agentID) == ClaudeBundled)
```
bằng
```go
	needBundledClaude := len(agent.BundledClaude) > 0 && m.claudeSource(agentID) == ClaudeBundled
	mf, err := inspectInstall(agent, staging, version, needBundledClaude)
```

- [ ] **Step 5: `manager.go` — thay toàn bộ hàm `inspectInstall`** (từ comment `// inspectInstall is the static half of verification:` tới hết hàm) bằng:

```go
// inspectInstall is the static half of verification: the files a launch needs
// are where they should be, and npm installed what was asked for.
func inspectInstall(agent Agent, dir, version string, needBundledClaude bool) (installManifest, error) {
	var mf installManifest
	entry := agent.Entry
	if len(agent.NativeEntry) > 0 {
		entry = firstExecutable(dir, agent.NativeEntry)
		if entry == "" {
			return mf, fail(ErrInstallFailed, "npm installed no "+agent.ID+" executable for this platform")
		}
	} else if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(entry))); err != nil {
		return mf, fail(ErrInstallFailed, "the adapter's entry script is missing")
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := readJSON(filepath.Join(dir, "node_modules", filepath.FromSlash(agent.Package), "package.json"), &pkg); err != nil {
		return mf, fail(ErrInstallFailed, "the adapter's package.json is unreadable")
	}
	if pkg.Version != version {
		return mf, fail(ErrInstallFailed, fmt.Sprintf("npm installed %s, not %s", pkg.Version, version))
	}
	if _, err := os.Stat(filepath.Join(dir, "package-lock.json")); err != nil {
		return mf, fail(ErrInstallFailed, "package-lock.json is missing")
	}
	if agent.SDKPackage != "" {
		var sdk struct {
			Version string `json:"version"`
		}
		_ = readJSON(filepath.Join(dir, "node_modules", filepath.FromSlash(agent.SDKPackage), "package.json"), &sdk)
		mf.SDKVersion = sdk.Version
	}

	mf.Agent, mf.Version, mf.Entry = agent.ID, version, entry
	mf.Package = agent.Package + "@" + version
	mf.BundledClaude = firstExecutable(dir, agent.BundledClaude)
	if mf.BundledClaude == "" && needBundledClaude {
		return mf, fail(ErrInstallFailed, "npm installed no Claude Code binary for this platform")
	}
	return mf, nil
}

// firstExecutable returns the first executable matching patterns — globs
// relative to dir — as a slash-separated path relative to dir, or "".
func firstExecutable(dir string, patterns []string) string {
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(filepath.Join(dir, filepath.FromSlash(pattern)))
		for _, match := range matches {
			if !isExecutable(match) {
				continue
			}
			if rel, err := filepath.Rel(dir, match); err == nil {
				return filepath.ToSlash(rel)
			}
		}
	}
	return ""
}
```

- [ ] **Step 6: `manager.go` — nhánh native trong `resolveAt`**: ngay sau khối

```go
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return Resolved{}, ErrNotInstalled
	}
```
chèn:
```go
	if len(agent.NativeEntry) > 0 {
		// The binary is the agent: no node, no entry script, nothing to grant
		// but the version directory. install.json names the binary; it is
		// DevKit's own file, but a path that leaves the version directory is
		// refused anyway rather than executed.
		rel := filepath.FromSlash(mf.Entry)
		if mf.Entry == "" || filepath.IsAbs(rel) || !filepath.IsLocal(rel) {
			return Resolved{}, ErrNotInstalled
		}
		exe := filepath.Join(dir, rel)
		if !isExecutable(exe) {
			return Resolved{}, ErrNotInstalled
		}
		return Resolved{
			Command:       exe,
			ReadOnlyPaths: []string{dir},
			Env:           map[string]string{"PATH": hostPath()},
			Version:       mf.Version,
		}, nil
	}
```

- [ ] **Step 7: Chạy**

```bash
go test ./internal/agentruntime/ -count=1 && go test ./internal/agenthub/ ./internal/bridge/ ./internal/arch/ -count=1 && go vet -tags server ./...
```
Expected: PASS toàn bộ (test Claude cũ không đổi, vẫn qua).

- [ ] **Step 8: Commit**

```bash
gofmt -l internal/agentruntime
git add internal/agentruntime/agentruntime.go internal/agentruntime/manager.go internal/agentruntime/manager_test.go
git commit -m "feat(agentruntime): native entries, and the codex agent

@openai/codex ships a native binary in a per-platform package; DevKit
runs it directly with only the version directory granted. The install
no longer demands a bundled Claude binary from agents that have none,
and a manifest entry that leaves the version directory is refused.

Co-Authored-By: <model> <noreply@anthropic.com>"
```

### Task 6: Cài `@openai/codex` thật (test bật bằng env)

**Files:**
- Create: `internal/agentruntime/install_live_test.go`

- [ ] **Step 1: Tạo test**

```go
package agentruntime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCodexInstallsAndRunsForReal installs the pinned @openai/codex with the
// machine's own node/npm and runs the resolved binary. It is the check the
// fake tree in manager_test.go cannot make: that npm really lays the package
// out where NativeEntry looks.
func TestCodexInstallsAndRunsForReal(t *testing.T) {
	if os.Getenv("DEVKIT_AGENT_INSTALL_TEST") != "1" {
		t.Skip("set DEVKIT_AGENT_INSTALL_TEST=1 to install @openai/codex for real (network, ~300MB)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	m := New(filepath.Join(t.TempDir(), "runtimes"))
	m.OnOutput = func(_, line string) { t.Log(line) }
	if err := m.Install(ctx, CodexCLI, ""); err != nil {
		t.Fatal(err)
	}
	rt, err := m.Resolve(CodexCLI)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("resolved %s", rt.Command)

	cmd := exec.CommandContext(ctx, rt.Command, "--version")
	cmd.Env = append(os.Environ(), "PATH="+rt.Env["PATH"])
	out, err := cmd.CombinedOutput()
	want := "codex-cli " + agents[CodexCLI].TestedVersion
	if err != nil || !strings.Contains(string(out), want) {
		t.Fatalf("%s --version = %q, %v; want %q", rt.Command, out, err, want)
	}
}
```

- [ ] **Step 2: Chạy ở chế độ bỏ qua** — `go test ./internal/agentruntime/ -run ForReal -count=1 -v` → `SKIP`.

- [ ] **Step 3: Chạy thật** — **controller hỏi người dùng trước** (tải khoảng 300 MB từ npm registry vào thư mục tạm; không dùng tài khoản hay quota nào):

```bash
DEVKIT_AGENT_INSTALL_TEST=1 go test ./internal/agentruntime/ -run ForReal -count=1 -v -timeout 15m
```
Expected: PASS, log có `resolved …/vendor/<triple>/bin/codex`. Nếu FAIL vì npm đặt binary ở chỗ khác: **dừng**, ghi đường dẫn thật và sửa glob `NativeEntry` (Task 5) trước khi đi tiếp.

- [ ] **Step 4: Commit**

```bash
git add internal/agentruntime/install_live_test.go
git commit -m "test(agentruntime): install @openai/codex for real behind an env flag

Co-Authored-By: <model> <noreply@anthropic.com>"
```

---

## Kết thúc

- [ ] Toàn bộ: `go test ./internal/codexapp/ ./internal/agentruntime/ ./internal/agenthub/ ./internal/bridge/ ./internal/mcp/ ./internal/arch/ -count=1 -race`, `go vet -tags server ./...`, `gofmt -l internal main.go`.
- [ ] Review tổng (opus) cho khoảng commit của kế hoạch này.
- [ ] `.sync/export.sh "codex plan 2a: codexapp + native runtime"` — báo người dùng đường dẫn zip.
- [ ] Viết Kế hoạch 2b (`CodexAdapter`) dùng `codexapp.Conn`, `codexapp` protocol types, `agentruntime.CodexCLI`/`Resolve`.
