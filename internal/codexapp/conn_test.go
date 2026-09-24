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
	// notification, when set, replaces the default notes-channel behaviour.
	notification func(ctx context.Context, method string, params json.RawMessage)
}

func newTestHandler() *testHandler { return &testHandler{notes: make(chan string, 16)} }

func (h *testHandler) Notification(ctx context.Context, method string, params json.RawMessage) {
	if h.notification != nil {
		h.notification(ctx, method, params)
		return
	}
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
	select {
	case got := <-h.notes:
		if got != MethodServerRequestResolved {
			t.Fatalf("notification = %q, want it passed on too", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serverRequest/resolved was not also passed on as a notification")
	}
	if resp := srv.read(t); resp["id"] != float64(0) {
		t.Fatalf("response = %v, want id 0 echoed", resp)
	}
}

func TestReusedServerRequestIDCancelsCurrentHandlerOnly(t *testing.T) {
	h := newTestHandler()
	calls := make(chan context.Context, 2)
	h.request = func(ctx context.Context, _ string, _ json.RawMessage) (json.RawMessage, *RPCError) {
		calls <- ctx
		<-ctx.Done()
		return nil, &RPCError{Code: -32800, Message: "cancelled"}
	}
	_, srv := newTestConn(t, h)

	srv.send(t, `{"id":"dup","method":"item/fileChange/requestApproval","params":{}}`)
	first := <-calls
	srv.send(t, `{"id":"dup","method":"item/fileChange/requestApproval","params":{}}`)
	second := <-calls

	select {
	case <-first.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the first handler for a reused id was not cancelled when the id was re-registered")
	}
	if second.Err() != nil {
		t.Fatal("the second (current) handler for a reused id was cancelled by its own registration")
	}

	srv.send(t, `{"method":"serverRequest/resolved","params":{"requestId":"dup"}}`)
	select {
	case <-second.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("serverRequest/resolved did not cancel the current handler for a reused id")
	}

	// Both handler goroutines return, and each writes its own response.
	for i := 0; i < 2; i++ {
		resp := srv.read(t)
		if resp["id"] != "dup" {
			t.Fatalf("response %d id = %v, want \"dup\"", i, resp["id"])
		}
	}
}

func TestServerRequestResolvedCancelsHandlerWithStringID(t *testing.T) {
	h := newTestHandler()
	cancelled := make(chan struct{})
	h.request = func(ctx context.Context, _ string, _ json.RawMessage) (json.RawMessage, *RPCError) {
		<-ctx.Done()
		close(cancelled)
		return nil, &RPCError{Code: -32800, Message: "cancelled"}
	}
	_, srv := newTestConn(t, h)

	srv.send(t, `{"id":"abc","method":"item/commandExecution/requestApproval","params":{}}`)
	srv.send(t, `{"method":"serverRequest/resolved","params":{"requestId":"abc"}}`)
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("serverRequest/resolved with a string id did not cancel the handler")
	}
	if resp := srv.read(t); resp["id"] != "abc" {
		t.Fatalf("response = %v, want id \"abc\" echoed", resp)
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

func TestCallAfterCloseReturnsErrClosed(t *testing.T) {
	// Serve is deliberately never started: done stays nil forever, which
	// pins the test to the window between Close (sets closed, closes rw)
	// and Serve noticing (sets done) that a real Call can land in too.
	client, server := net.Pipe()
	t.Cleanup(func() { _ = server.Close() })
	c := NewConn(client, newTestHandler())
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	errc := make(chan error, 1)
	go func() { errc <- c.Call(context.Background(), "turn/start", struct{}{}, nil) }()
	select {
	case err := <-errc:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("Call after Close err = %v, want ErrClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Call after Close did not return promptly")
	}
}

func TestCallContextCancelled(t *testing.T) {
	c, srv := newTestConn(t, newTestHandler())
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- c.Call(ctx, "turn/start", struct{}{}, nil) }()
	req := srv.read(t)
	cancel()

	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Call err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Call did not return after ctx cancellation")
	}

	// A late response for the cancelled call's id must be harmless: nothing
	// is listening for it any more.
	srv.send(t, fmt.Sprintf(`{"id":%v,"result":{}}`, req["id"]))

	// The connection must still work for a fresh Call.
	errc2 := make(chan error, 1)
	go func() { errc2 <- c.Call(context.Background(), "thread/start", struct{}{}, nil) }()
	req2 := srv.read(t)
	srv.send(t, fmt.Sprintf(`{"id":%v,"result":{}}`, req2["id"]))
	select {
	case err := <-errc2:
		if err != nil {
			t.Fatalf("Call after a late stale response err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Call after a late stale response did not complete")
	}
}

func TestCallFromNotificationHandlerIsRefused(t *testing.T) {
	h := newTestHandler()
	result := make(chan error, 1)
	var c *Conn
	h.notification = func(ctx context.Context, _ string, _ json.RawMessage) {
		// A real handler would deadlock here: the read loop that would
		// deliver the response is the very goroutine making this call.
		result <- c.Call(ctx, "thread/start", struct{}{}, nil)
	}
	var srv *fakeServer
	c, srv = newTestConn(t, h)

	srv.send(t, `{"method":"turn/started","params":{}}`)
	select {
	case err := <-result:
		if !errors.Is(err, ErrCallFromNotification) {
			t.Fatalf("Call from Notification err = %v, want ErrCallFromNotification", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Call from a Notification handler deadlocked instead of being refused")
	}
}

func TestServeStopsWhenCtxCancelled(t *testing.T) {
	h := newTestHandler()
	client, server := net.Pipe()
	t.Cleanup(func() { _ = server.Close() })
	c := NewConn(client, h)
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- c.Serve(ctx) }()
	t.Cleanup(func() { _ = c.Close() })

	// A Call started before cancellation, against a server that stays silent.
	errc := make(chan error, 1)
	go func() { errc <- c.Call(context.Background(), "turn/start", struct{}{}, nil) }()

	// Give Serve's read loop time to actually block in ReadMessage (the
	// server never writes), so cancelling ctx here exercises unblocking a
	// read already in flight, not merely winning a race against Serve's
	// very first ctx.Err() check.
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-served:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after its ctx was cancelled")
	}
	select {
	case err := <-errc:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("pending Call err = %v, want ErrClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the pending Call did not fail once Serve stopped")
	}
}

func TestServeWaitsForInFlightHandler(t *testing.T) {
	h := newTestHandler()
	started := make(chan struct{})
	release := make(chan struct{})
	h.request = func(context.Context, string, json.RawMessage) (json.RawMessage, *RPCError) {
		close(started)
		<-release
		return json.RawMessage(`{}`), nil
	}
	client, server := net.Pipe()
	c := NewConn(client, h)
	served := make(chan error, 1)
	go func() { served <- c.Serve(context.Background()) }()
	t.Cleanup(func() { _ = c.Close() })

	if _, err := server.Write([]byte(`{"id":1,"method":"item/fileChange/requestApproval","params":{}}` + "\n")); err != nil {
		t.Fatalf("server write: %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never started")
	}
	_ = server.Close() // EOF on the client's read side; the handler is still running.

	select {
	case <-served:
		t.Fatal("Serve returned before the in-flight handler did")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("Serve err = %v, want nil (clean EOF)", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return once the in-flight handler finished")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	c, _ := newTestConn(t, newTestHandler())
	if err := c.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
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
