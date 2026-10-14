package agenthub

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPreviewFromMessage(t *testing.T) {
	cases := map[string]string{
		"":                                   "",
		"   \n ":                             "",
		"Done. I also ran the tests.":        "Done.",
		"Fixed it!\nNext I will refactor.":   "Fixed it!",
		"No full stop here":                  "No full stop here",
		"Line one\n\n  line two":             "Line one line two",
		"Đã sửa TTL. Tiếp theo là test.":     "Đã sửa TTL.",
		"See v1.2.3 for details. Then more.": "See v1.2.3 for details.",
		"1. First step. Second":              "First step.",
		"2) Second step. Third":              "Second step.",
		"- Fixed the TTL. Then tests":        "Fixed the TTL.",
		"> Quoted. More":                     "Quoted.",
		"## Summary\nAll good. More":         "Summary",
		"**Done.** Next":                     "Done.",
		"Use `go test`. Then":                "Use go test.",
		"e.g. this works. Next":              "e.g. this works.",
		"Dr. Smith did it. Yes":              "Dr. Smith did it.",
		"Step 1. Do it":                      "Step 1. Do it",
		"- a. b. Next":                       "a. b. Next",
	}
	for in, want := range cases {
		if got := PreviewFromMessage(in); got != want {
			t.Errorf("PreviewFromMessage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPreviewClipsToRunes(t *testing.T) {
	long := strings.Repeat("ữ", 300)
	got := PreviewFromMessage(long)
	if utf8.RuneCountInString(got) != previewMaxRunes {
		t.Fatalf("runes = %d", utf8.RuneCountInString(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("no ellipsis: %q", got)
	}
}

func TestPreviewFromError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"vendor message wins", &VendorError{Agent: AgentTypeCodex, Err: ErrAgentUsageLimit, Message: "You hit your usage limit. Try again at 14:00."}, "You hit your usage limit."},
		{"usage limit", &VendorError{Agent: AgentTypeCodex, Err: ErrAgentUsageLimit}, "Usage limit reached."},
		{"process failed", fmt.Errorf("%w: codex exited", ErrProcessFailed), "The agent stopped unexpectedly."},
		{"turn failed", fmt.Errorf("%w: context window", ErrTurnFailed), "The agent could not finish this turn."},
		{"auth", &VendorError{Agent: AgentTypeClaude, Err: ErrAgentAuthRequired}, "Sign in to continue."},
		{"api key before auth", &VendorError{Agent: AgentTypeCodex, Err: fmt.Errorf("%w: %w", ErrAgentAuthRequired, ErrAgentAPIKeyUnusable)}, "The chosen API key was refused."},
		{"anything else", errors.New("boom"), "The agent could not finish this turn."},
	}
	for _, tc := range cases {
		if got := PreviewFromError(tc.err); got != tc.want {
			t.Errorf("%s: PreviewFromError = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func agentText(id, text string) SessionUpdate {
	return SessionUpdate{Kind: UpdateAgentMessageChunk, MessageID: id, Content: &ContentBlock{Type: "text", Text: text}}
}

// Only the first sentence is ever shown, so a long message stops growing the
// buffer once it is past previewBufferBytes; a new message still starts over.
func TestLastAgentMessageIsBounded(t *testing.T) {
	var m lastAgentMessage
	chunk := strings.Repeat("x", 1000)
	for i := 0; i < 20; i++ {
		m.observe(agentText("m1", chunk))
	}
	if n := len(m.String()); n > previewBufferBytes+len(chunk) {
		t.Fatalf("buffer grew to %d bytes", n)
	}
	m.observe(agentText("m2", "Done."))
	if got := m.String(); got != "Done." {
		t.Fatalf("new message = %q", got)
	}
}

// Chunks with no message id belong to one message until something else of the
// turn comes between them; metadata such as usage does not split a message.
func TestLastAgentMessageWithoutIDs(t *testing.T) {
	var m lastAgentMessage
	m.observe(agentText("", "First."))
	m.observe(SessionUpdate{Kind: UpdateAgentThoughtChunk})
	m.observe(agentText("", "Second"))
	m.observe(SessionUpdate{Kind: UpdateUsage})
	m.observe(agentText("", " part."))
	m.observe(SessionUpdate{Kind: UpdateAgentMessageChunk, Origin: "sub", Content: &ContentBlock{Type: "text", Text: "Sub."}})
	if got := m.String(); got != "Second part." {
		t.Fatalf("got %q", got)
	}
}
