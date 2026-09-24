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
