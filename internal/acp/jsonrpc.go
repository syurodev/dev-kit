package acp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Request is a JSON-RPC 2.0 request (has id and method).
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response (has id and result or error).
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// Notification is a JSON-RPC 2.0 notification (method, no id).
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error keeps Data. The agent puts the reason there as often as in Message —
// claude-agent-acp answers an expired login with "Internal error" and
// {"errorKind":"authentication_failed"} — and an error that dropped it said
// only "Internal error" to everyone downstream.
func (e *RPCError) Error() string {
	if len(e.Data) == 0 {
		return fmt.Sprintf("acp: rpc error %d: %s", e.Code, e.Message)
	}
	data := string(e.Data)
	if len(data) > 300 {
		data = data[:300] + "…"
	}
	return fmt.Sprintf("acp: rpc error %d: %s (data %s)", e.Code, e.Message, data)
}

// Message is a decoded JSON-RPC 2.0 message (request, response, or notification).
// ID is nil when absent (notifications); otherwise the raw JSON bytes of the id.
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// WriteMessage encodes v as a single line of JSON-RPC 2.0 (no embedded newlines)
// and writes it followed by a newline.
func WriteMessage(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if bytes.Contains(data, []byte{'\n'}) {
		return fmt.Errorf("acp: message must not contain embedded newlines")
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

// ReadMessage reads one newline-delimited JSON-RPC 2.0 message from r.
func ReadMessage(r io.Reader) (Message, error) {
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}
	line, err := br.ReadBytes('\n')
	if err != nil && !(err == io.EOF && len(line) > 0) {
		return Message{}, err
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return Message{}, io.EOF
	}

	var msg Message
	if err := json.Unmarshal(line, &msg); err != nil {
		return Message{}, err
	}
	if msg.JSONRPC != "2.0" {
		return Message{}, fmt.Errorf("acp: unsupported jsonrpc version %q", msg.JSONRPC)
	}
	return msg, nil
}
