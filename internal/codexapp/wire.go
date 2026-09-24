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
