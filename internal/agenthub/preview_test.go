package agenthub

import (
	"errors"
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
	vendor := &VendorError{Agent: AgentTypeCodex, Err: ErrAgentUsageLimit, Message: "You hit your usage limit. Try again at 14:00."}
	if got := PreviewFromError(vendor); got != "You hit your usage limit." {
		t.Fatalf("vendor: %q", got)
	}
	if got := PreviewFromError(errors.New("boom")); got != "boom" {
		t.Fatalf("plain: %q", got)
	}
	if got := PreviewFromError(nil); got != "" {
		t.Fatalf("nil: %q", got)
	}
}
