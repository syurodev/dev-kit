package acp_test

import (
	"context"
	"encoding/json"
	"net"
	"testing"

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
