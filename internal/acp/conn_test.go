package acp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"devkit/internal/acp"
)

type stubHandler struct {
	permCalls  int
	permParams json.RawMessage
	permResult json.RawMessage
	permErr    error

	updateCalls  int
	updateParams json.RawMessage
	updateErr    error

	unknownCalls  int
	unknownMethod string
	unknownParams json.RawMessage
	unknownResult json.RawMessage
	unknownErr    error
}

func (h *stubHandler) RequestPermission(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	h.permCalls++
	h.permParams = append(json.RawMessage(nil), params...)
	if h.permErr != nil {
		return nil, h.permErr
	}
	if h.permResult != nil {
		return h.permResult, nil
	}
	return json.RawMessage(`{"outcome":{"outcome":"selected","optionId":"allow"}}`), nil
}

func (h *stubHandler) SessionUpdate(ctx context.Context, params json.RawMessage) error {
	h.updateCalls++
	h.updateParams = append(json.RawMessage(nil), params...)
	return h.updateErr
}

func (h *stubHandler) UnknownMethod(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	h.unknownCalls++
	h.unknownMethod = method
	h.unknownParams = append(json.RawMessage(nil), params...)
	if h.unknownErr != nil {
		return nil, h.unknownErr
	}
	if h.unknownResult != nil {
		return h.unknownResult, nil
	}
	return nil, nil
}

func decodeID(id json.RawMessage) (any, error) {
	var v any
	if err := json.Unmarshal(id, &v); err != nil {
		return nil, err
	}
	return v, nil
}

func TestConnCallRoundTrip(t *testing.T) {
	client, agent := net.Pipe()
	defer client.Close()
	defer agent.Close()

	h := &stubHandler{}
	c := acp.NewConn(client, h)
	go c.Serve(context.Background())

	errCh := make(chan error, 1)
	go func() {
		msg, err := acp.ReadMessage(agent)
		if err != nil {
			errCh <- err
			return
		}
		id, err := decodeID(msg.ID)
		if err != nil {
			errCh <- err
			return
		}
		errCh <- acp.WriteMessage(agent, acp.Response{
			JSONRPC: "2.0",
			ID:      id,
			Result:  json.RawMessage(`{"ok":true}`),
		})
	}()

	var result map[string]any
	if err := c.Call(context.Background(), "initialize", map[string]any{"protocolVersion": 1}, &result); err != nil {
		t.Fatal(err)
	}
	if result["ok"] != true {
		t.Fatalf("%v", result)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

func TestConnInboundRequestPermission(t *testing.T) {
	client, agent := net.Pipe()
	defer client.Close()
	defer agent.Close()

	h := &stubHandler{
		permResult: json.RawMessage(`{"outcome":{"outcome":"selected","optionId":"allow"}}`),
	}
	c := acp.NewConn(client, h)
	serveDone := make(chan error, 1)
	go func() { serveDone <- c.Serve(context.Background()) }()

	params := json.RawMessage(`{"sessionId":"s1","toolCall":{"toolCallId":"tc1"}}`)
	if err := acp.WriteMessage(agent, acp.Request{
		JSONRPC: "2.0",
		ID:      42,
		Method:  "session/request_permission",
		Params:  params,
	}); err != nil {
		t.Fatal(err)
	}

	msg, err := acp.ReadMessage(agent)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Method != "" {
		t.Fatalf("expected response, got method %q", msg.Method)
	}
	var id any
	if err := json.Unmarshal(msg.ID, &id); err != nil {
		t.Fatal(err)
	}
	if id != float64(42) {
		t.Fatalf("id = %v", id)
	}
	if msg.Error != nil {
		t.Fatalf("unexpected error: %+v", msg.Error)
	}
	var result map[string]any
	if err := json.Unmarshal(msg.Result, &result); err != nil {
		t.Fatal(err)
	}
	outcome, _ := result["outcome"].(map[string]any)
	if outcome["optionId"] != "allow" {
		t.Fatalf("result = %v", result)
	}
	if h.permCalls != 1 {
		t.Fatalf("permCalls = %d", h.permCalls)
	}

	_ = c.Close()
	<-serveDone
}

func TestConnRequestPermissionDenyPrefersResult(t *testing.T) {
	client, agent := net.Pipe()
	defer client.Close()
	defer agent.Close()

	// Handler returns cancelled Result AND an error (Invoke fail-closed signal).
	// Wire response must prefer Result so the agent gets a proper ACP outcome.
	c := acp.NewConn(client, &denyBothHandler{
		result: json.RawMessage(`{"outcome":{"outcome":"cancelled"}}`),
		err:    errPermissionDeniedStub,
	})
	serveDone := make(chan error, 1)
	go func() { serveDone <- c.Serve(context.Background()) }()

	if err := acp.WriteMessage(agent, acp.Request{
		JSONRPC: "2.0",
		ID:      7,
		Method:  "session/request_permission",
		Params:  json.RawMessage(`{"sessionId":"s1"}`),
	}); err != nil {
		t.Fatal(err)
	}

	msg, err := acp.ReadMessage(agent)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Error != nil {
		t.Fatalf("expected Result preferred over RPC error, got error %+v", msg.Error)
	}
	if len(msg.Result) == 0 {
		t.Fatal("expected cancelled Result body")
	}
	var result map[string]any
	if err := json.Unmarshal(msg.Result, &result); err != nil {
		t.Fatal(err)
	}
	outcome, _ := result["outcome"].(map[string]any)
	if outcome["outcome"] != "cancelled" {
		t.Fatalf("result = %v", result)
	}

	_ = c.Close()
	<-serveDone
}

type denyBothHandler struct {
	result json.RawMessage
	err    error
}

func (h *denyBothHandler) RequestPermission(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	return h.result, h.err
}

func (h *denyBothHandler) SessionUpdate(ctx context.Context, params json.RawMessage) error {
	return nil
}

func (h *denyBothHandler) UnknownMethod(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	return nil, nil
}

type errStub string

func (e errStub) Error() string { return string(e) }

var errPermissionDeniedStub errStub = "permission denied"

func TestConnInboundSessionUpdate(t *testing.T) {
	client, agent := net.Pipe()
	defer client.Close()
	defer agent.Close()

	h := &stubHandler{}
	c := acp.NewConn(client, h)
	go c.Serve(context.Background())

	params := json.RawMessage(`{"sessionId":"s1","update":{"sessionUpdate":"agent_message_chunk"}}`)
	if err := acp.WriteMessage(agent, acp.Notification{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params:  params,
	}); err != nil {
		t.Fatal(err)
	}

	// Drive the serve loop with a Call so we know the notification was processed
	// before asserting (agent responds after the notification is already queued).
	errCh := make(chan error, 1)
	go func() {
		msg, err := acp.ReadMessage(agent)
		if err != nil {
			errCh <- err
			return
		}
		id, err := decodeID(msg.ID)
		if err != nil {
			errCh <- err
			return
		}
		errCh <- acp.WriteMessage(agent, acp.Response{
			JSONRPC: "2.0",
			ID:      id,
			Result:  json.RawMessage(`{}`),
		})
	}()

	var result map[string]any
	if err := c.Call(context.Background(), "ping", nil, &result); err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if h.updateCalls != 1 {
		t.Fatalf("updateCalls = %d", h.updateCalls)
	}
}

// chanHandler is a ClientHandler whose calls are observable from the test
// goroutine without a data race: each call is reported on a channel, and the
// update and elicitation handlers can be held open until the test says go.
type chanHandler struct {
	updates chan json.RawMessage
	// holdUpdate, when set, blocks SessionUpdate until it is closed.
	holdUpdate chan struct{}

	unknown chan string
}

func newChanHandler() *chanHandler {
	return &chanHandler{
		updates: make(chan json.RawMessage, 16),
		unknown: make(chan string, 16),
	}
}

func (h *chanHandler) RequestPermission(context.Context, json.RawMessage) (json.RawMessage, error) {
	return nil, nil
}

func (h *chanHandler) SessionUpdate(_ context.Context, params json.RawMessage) error {
	h.updates <- append(json.RawMessage(nil), params...)
	if h.holdUpdate != nil {
		<-h.holdUpdate
	}
	return nil
}

func (h *chanHandler) UnknownMethod(_ context.Context, method string, _ json.RawMessage) (json.RawMessage, error) {
	h.unknown <- method
	return nil, errUnsupported
}

var errUnsupported = errors.New("unsupported")

// elicitingHandler adds Elicit, so Conn routes elicitation/create to it.
type elicitingHandler struct {
	*chanHandler
	elicit func(ctx context.Context, params json.RawMessage) (json.RawMessage, error)
}

func (h *elicitingHandler) Elicit(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	return h.elicit(ctx, params)
}

// agentWrite sends one raw JSON-RPC line from the agent end of the pipe.
func agentWrite(t *testing.T, agent net.Conn, line string) {
	t.Helper()
	if _, err := agent.Write([]byte(line + "\n")); err != nil {
		t.Fatalf("agent write: %v", err)
	}
}

func readResponse(t *testing.T, agent net.Conn) acp.Message {
	t.Helper()
	type read struct {
		msg acp.Message
		err error
	}
	got := make(chan read, 1)
	go func() {
		msg, err := acp.ReadMessage(agent)
		got <- read{msg, err}
	}()
	select {
	case r := <-got:
		if r.err != nil {
			t.Fatalf("read response: %v", r.err)
		}
		return r.msg
	case <-time.After(2 * time.Second):
		t.Fatal("no response written")
		return acp.Message{}
	}
}

func serve(t *testing.T, h acp.ClientHandler) (net.Conn, context.CancelFunc) {
	t.Helper()
	client, agent := net.Pipe()
	c := acp.NewConn(client, h)
	ctx, cancel := context.WithCancel(context.Background())
	go c.Serve(ctx)
	t.Cleanup(func() {
		cancel()
		client.Close()
		agent.Close()
	})
	return agent, cancel
}

// TestElicitationDoesNotBlockTheReadLoop is the reason elicitation/create runs
// off the loop. A question can stay open for minutes, and the agent keeps
// working while it waits — subagents stream, background tasks report. Handled on
// the loop like a permission, every one of those updates would sit in the pipe
// until the person answered.
func TestElicitationDoesNotBlockTheReadLoop(t *testing.T) {
	release := make(chan struct{})
	h := &elicitingHandler{
		chanHandler: newChanHandler(),
		elicit: func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
			<-release
			return json.RawMessage(`{"action":"accept","content":{"question_0":"yes"}}`), nil
		},
	}
	agent, _ := serve(t, h)

	agentWrite(t, agent, `{"jsonrpc":"2.0","id":1,"method":"elicitation/create","params":{"mode":"form","sessionId":"s","message":"?"}}`)
	// From a goroutine: net.Pipe is unbuffered, so if the loop is stuck in the
	// question these writes block too, and the failure has to come from the
	// timeout below — with its message — rather than from the test hanging.
	go func() {
		for i := 0; i < 3; i++ {
			_, _ = agent.Write([]byte(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s"}}` + "\n"))
		}
	}()

	for i := 0; i < 3; i++ {
		select {
		case <-h.updates:
		case <-time.After(2 * time.Second):
			t.Fatalf("update %d never arrived while the question was open — the read loop is blocked", i+1)
		}
	}

	close(release)
	resp := readResponse(t, agent)
	if string(resp.ID) != "1" {
		t.Fatalf("response id = %s, want 1", resp.ID)
	}
	if !strings.Contains(string(resp.Result), `"accept"`) || resp.Error != nil {
		t.Fatalf("response = result %s, error %v", resp.Result, resp.Error)
	}
}

// TestSessionUpdateStillBlocksTheReadLoop pins the converse. Only
// elicitation/create leaves the loop. The agent emits a question's tool_call
// update and waits for it before asking, so handling updates in order on the
// loop is what puts the transcript row ahead of the card. Making notifications
// async "for consistency" would quietly invert that.
func TestSessionUpdateStillBlocksTheReadLoop(t *testing.T) {
	h := newChanHandler()
	h.holdUpdate = make(chan struct{})
	agent, _ := serve(t, h)

	// net.Pipe is unbuffered: each write returns only once the loop has read it.
	agentWrite(t, agent, `{"jsonrpc":"2.0","method":"session/update","params":{"n":1}}`)
	<-h.updates // the first is inside the handler, held

	second := make(chan struct{})
	go func() {
		// Not agentWrite: t.Fatalf may only be called from the test goroutine.
		_, _ = agent.Write([]byte(`{"jsonrpc":"2.0","method":"session/update","params":{"n":2}}` + "\n"))
		close(second)
	}()

	select {
	case <-second:
		t.Fatal("the loop read the next message while an update handler was still running")
	case <-time.After(100 * time.Millisecond):
	}

	close(h.holdUpdate)
	select {
	case <-h.updates:
	case <-time.After(2 * time.Second):
		t.Fatal("the second update never arrived after the first returned")
	}
}

// TestCancelRequestCancelsTheElicitation covers the one way the agent can take
// a question back. With no deadline on a question, a cancel that did not reach
// the handler is a card that never closes.
func TestCancelRequestCancelsTheElicitation(t *testing.T) {
	started := make(chan struct{})
	h := &elicitingHandler{
		chanHandler: newChanHandler(),
		elicit: func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
			close(started)
			<-ctx.Done()
			return json.RawMessage(`{"action":"cancel"}`), nil
		},
	}
	agent, _ := serve(t, h)

	agentWrite(t, agent, `{"jsonrpc":"2.0","id":7,"method":"elicitation/create","params":{"mode":"form","sessionId":"s","message":"?"}}`)
	<-started
	// The same id, encoded differently. Keying on the raw bytes would miss it.
	agentWrite(t, agent, `{"jsonrpc":"2.0","method":"$/cancel_request","params":{"requestId":7.0}}`)

	resp := readResponse(t, agent)
	if string(resp.ID) != "7" {
		t.Fatalf("response id = %s, want 7", resp.ID)
	}
	// Still answered: the SDK keeps a request it cancelled pending until a
	// response arrives.
	if !strings.Contains(string(resp.Result), `"cancel"`) {
		t.Fatalf("result = %s, want the cancel action", resp.Result)
	}

	select {
	case m := <-h.unknown:
		t.Fatalf("%s reached UnknownMethod; it is a method Conn understands now", m)
	default:
	}
}

// TestElicitationPrefersResultOverError: a skipped question is an answer. As a
// JSON-RPC error the vendor reads it as a failure to present the question.
func TestElicitationPrefersResultOverError(t *testing.T) {
	h := &elicitingHandler{
		chanHandler: newChanHandler(),
		elicit: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"action":"decline"}`), errors.New("declined")
		},
	}
	agent, _ := serve(t, h)

	agentWrite(t, agent, `{"jsonrpc":"2.0","id":3,"method":"elicitation/create","params":{"mode":"form","sessionId":"s","message":"?"}}`)
	resp := readResponse(t, agent)
	if resp.Error != nil || !strings.Contains(string(resp.Result), `"decline"`) {
		t.Fatalf("result %s, error %v; want the decline body and no error", resp.Result, resp.Error)
	}
}

// TestElicitationWithoutAHandlerIsRefused: a client that cannot ask says so,
// through UnknownMethod, rather than leaving the agent waiting.
func TestElicitationWithoutAHandlerIsRefused(t *testing.T) {
	h := newChanHandler()
	agent, _ := serve(t, h)

	agentWrite(t, agent, `{"jsonrpc":"2.0","id":4,"method":"elicitation/create","params":{}}`)
	resp := readResponse(t, agent)
	if resp.Error == nil {
		t.Fatalf("result %s; a client without Elicit must refuse", resp.Result)
	}
	if m := <-h.unknown; m != acp.MethodElicitationCreate {
		t.Fatalf("UnknownMethod got %q", m)
	}
}

// TestCloseCancelsOpenElicitations: nobody can hear an answer after Close, so
// the question must not keep a goroutine waiting for one.
func TestCloseCancelsOpenElicitations(t *testing.T) {
	started := make(chan struct{})
	ended := make(chan struct{})
	h := &elicitingHandler{
		chanHandler: newChanHandler(),
		elicit: func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
			close(started)
			<-ctx.Done()
			close(ended)
			return json.RawMessage(`{"action":"cancel"}`), nil
		},
	}
	client, agent := net.Pipe()
	defer agent.Close()
	c := acp.NewConn(client, h)
	go c.Serve(context.Background()) // not cancelled: Close alone must do it

	go func() {
		_, _ = agent.Write([]byte(`{"jsonrpc":"2.0","id":9,"method":"elicitation/create","params":{"mode":"form","sessionId":"s","message":"?"}}` + "\n"))
	}()
	<-started
	_ = c.Close()

	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("Close left the question's handler waiting")
	}
}
