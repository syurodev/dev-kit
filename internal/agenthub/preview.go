package agenthub

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// previewMaxRunes bounds the sidebar's second line. Runes, not bytes: a
// Vietnamese sentence is two or three bytes a letter.
const previewMaxRunes = 140

// PreviewFromMessage is the first sentence of an agent message, on one line.
// A sentence ends at . ! ? or … followed by whitespace or the end, so
// "v1.2.3" and "e.g." inside a word do not cut it.
func PreviewFromMessage(text string) string {
	folded := strings.Join(strings.Fields(text), " ")
	if folded == "" {
		return ""
	}
	runes := []rune(folded)
	for i, r := range runes {
		if r != '.' && r != '!' && r != '?' && r != '…' {
			continue
		}
		if i == len(runes)-1 || unicode.IsSpace(runes[i+1]) {
			return clipPreview(string(runes[:i+1]))
		}
	}
	return clipPreview(folded)
}

// PreviewFromError is why a turn failed, in the vendor's own words when it
// gave any (the only place that says when a limit lifts), else the error.
func PreviewFromError(err error) string {
	if err == nil {
		return ""
	}
	if msg := ErrorVendorMessage(err); msg != "" {
		return PreviewFromMessage(msg)
	}
	return PreviewFromMessage(err.Error())
}

func clipPreview(s string) string {
	if utf8.RuneCountInString(s) <= previewMaxRunes {
		return s
	}
	r := []rune(s)
	return strings.TrimRightFunc(string(r[:previewMaxRunes-1]), unicode.IsSpace) + "…"
}
