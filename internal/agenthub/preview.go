package agenthub

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// previewMaxRunes bounds the sidebar's second line. Runes, not bytes: a
// Vietnamese sentence is two or three bytes a letter.
const previewMaxRunes = 140

// PreviewFromMessage is the first sentence of an agent message, on one line.
// A sentence ends at . ! ? or … followed by whitespace or the end, so
// "v1.2.3" inside a word does not cut it, nor does a list number ("1."), a
// single letter or a common abbreviation ("e.g.", "Dr."). Markdown is read,
// not shown: list/quote/heading markers and emphasis/code marks are dropped,
// and a heading line is a sentence of its own.
func PreviewFromMessage(text string) string {
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		heading := headingMarker.MatchString(line)
		line = leadingMarker.ReplaceAllString(line, "")
		line = strings.Join(strings.Fields(inlineMarks.Replace(line)), " ")
		if line == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(line)
		if heading {
			b.WriteByte('\n') // a heading ends the sentence
		}
	}
	runes := []rune(strings.TrimSpace(b.String()))
	if len(runes) == 0 {
		return ""
	}
	start := 0 // first rune of the token being read
	for i, r := range runes {
		switch {
		case r == '\n':
			return clipPreview(strings.TrimSpace(string(runes[:i])))
		case unicode.IsSpace(r):
			start = i + 1
		case r == '.' || r == '!' || r == '?' || r == '…':
			if i != len(runes)-1 && !unicode.IsSpace(runes[i+1]) {
				continue
			}
			if r == '.' && notASentenceEnd(string(runes[start:i+1])) {
				continue
			}
			return clipPreview(strings.TrimSpace(string(runes[:i+1])))
		}
	}
	return clipPreview(strings.ReplaceAll(string(runes), "\n", " "))
}

var (
	headingMarker = regexp.MustCompile(`^\s*#{1,6}\s+`)
	leadingMarker = regexp.MustCompile(`^\s*(#{1,6}\s+|[-*+]\s+|\d+[.)]\s+|>\s+)`)
	inlineMarks   = strings.NewReplacer("**", "", "__", "", "`", "")
	abbreviations = map[string]bool{
		"e.g.": true, "i.e.": true, "etc.": true, "vs.": true, "dr.": true,
		"mr.": true, "mrs.": true, "ms.": true, "approx.": true, "no.": true,
	}
)

// notASentenceEnd says whether a token ending in a dot is a list number, one
// letter or an abbreviation rather than the end of a sentence.
func notASentenceEnd(token string) bool {
	token = strings.TrimLeft(token, `("'[`)
	word := strings.TrimSuffix(token, ".")
	if word != "" && strings.Trim(word, "0123456789") == "" {
		return true
	}
	if utf8.RuneCountInString(word) == 1 && unicode.IsLetter([]rune(word)[0]) {
		return true
	}
	return abbreviations[strings.ToLower(token)]
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
