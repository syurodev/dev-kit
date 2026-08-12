package acp_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"devkit/internal/acp"
)

func TestWriteReadRequestRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	req := acp.Request{JSONRPC: "2.0", ID: 1, Method: "initialize", Params: json.RawMessage(`{"protocolVersion":1}`)}
	if err := acp.WriteMessage(&buf, req); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(buf.Bytes(), []byte("\n")) {
		t.Fatal("must be line-delimited")
	}
	msg, err := acp.ReadMessage(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Method != "initialize" || msg.ID == nil {
		t.Fatalf("got %+v", msg)
	}
}

func TestWriteNotificationHasNoID(t *testing.T) {
	var buf bytes.Buffer
	n := acp.Notification{JSONRPC: "2.0", Method: "session/cancel", Params: json.RawMessage(`{"sessionId":"s1"}`)}
	if err := acp.WriteMessage(&buf, n); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	var m map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(raw), &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["id"]; ok {
		t.Fatal("notification must omit id")
	}
}
