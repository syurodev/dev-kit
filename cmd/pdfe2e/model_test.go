package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// A scripted "model": it calls the named tools in order, then answers. Both
// providers' mocks share this; only the wire format differs.

type toolCall struct {
	// Code, when set, makes the "model" run this JavaScript in Codex's exec tool
	// instead of calling Tool directly (Codex reaches MCP tools only that way).
	// Tool is matched against the end of the tool's name in the request, so the
	// same script works for mcp__devkit__pdf_viewPages and however a vendor
	// spells it.
	Tool string
	Args map[string]any
	Code string
}

// resultBlock is one piece of a tool result as the model was sent it.
type resultBlock struct {
	Kind string // text, image
	Text string
	MIME string
	Data []byte // decoded image bytes
}

// seenResult is a tool result as it arrived in a request to the model.
type seenResult struct {
	CallID  string
	Tool    string
	IsError bool
	Blocks  []resultBlock
	Raw     string // the wire JSON of the result, for diagnosis
}

func (r seenResult) text() string {
	var parts []string
	for _, b := range r.Blocks {
		if b.Kind == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func (r seenResult) images() []resultBlock {
	var out []resultBlock
	for _, b := range r.Blocks {
		if b.Kind == "image" {
			out = append(out, b)
		}
	}
	return out
}

// modelServer records what the runtime sends and plays the script.
type modelServer struct {
	t      *testing.T
	server *httptest.Server

	mu       sync.Mutex
	script   []toolCall
	final    string
	requests []string        // every request body, raw
	system   string          // the system / instructions text of the latest main request
	prompts  []string        // user text blocks of the latest main request
	toolList []string        // tool names offered on the latest main request
	results  []seenResult    // every distinct tool result, in the order seen
	seen     map[string]bool // call ids already recorded
	calls    map[string]string
	mainReqs int
	baseline int // tool calls already in the conversation when the current script began; -1 until known
	nextID   int
}

func newModelServer(t *testing.T, handler func(m *modelServer) http.Handler) *modelServer {
	m := &modelServer{t: t, seen: map[string]bool{}, calls: map[string]string{}}
	m.server = httptest.NewServer(handler(m))
	t.Cleanup(m.server.Close)
	return m
}

func (m *modelServer) setScript(final string, calls ...toolCall) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.script, m.final = calls, final
	m.baseline = -1
}

func (m *modelServer) URL() string { return m.server.URL }

func (m *modelServer) record(body string) {
	m.mu.Lock()
	m.requests = append(m.requests, body)
	m.mu.Unlock()
}

func (m *modelServer) snapshotResults() []seenResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]seenResult(nil), m.results...)
}

func (m *modelServer) resultFor(tool string) (seenResult, bool) {
	for _, r := range m.snapshotResults() {
		if strings.HasSuffix(r.Tool, tool) {
			return r, true
		}
	}
	return seenResult{}, false
}

// next picks the step to play given how many tool calls the conversation holds.
func (m *modelServer) next(done int) (toolCall, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.baseline < 0 {
		m.baseline = done
	}
	if i := done - m.baseline; i < len(m.script) {
		return m.script[i], true
	}
	return toolCall{}, false
}

// newID is a call id no earlier turn used.
func (m *modelServer) newID(prefix string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	return fmt.Sprintf("%s_%d", prefix, m.nextID)
}

func (m *modelServer) finalText() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.final == "" {
		return "done"
	}
	return m.final
}

func (m *modelServer) note(system string, prompts, tools []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mainReqs++
	m.system, m.prompts, m.toolList = system, prompts, tools
}

func (m *modelServer) addResult(r seenResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.seen[r.CallID] {
		return
	}
	m.seen[r.CallID] = true
	r.Tool = m.calls[r.CallID]
	m.results = append(m.results, r)
}

func (m *modelServer) rememberCall(id, tool string) {
	m.mu.Lock()
	m.calls[id] = tool
	m.mu.Unlock()
}

func pick(tools []string, suffix string) string {
	for _, t := range tools {
		if strings.HasSuffix(t, suffix) {
			return t
		}
	}
	return ""
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func flush(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func sse(w http.ResponseWriter, event string, data any) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, mustJSON(data))
	flush(w)
}

func decodeB64(s string) []byte {
	b, _ := base64.StdEncoding.DecodeString(s)
	return b
}
