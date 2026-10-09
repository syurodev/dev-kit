package wssearch

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxQueryBytes bounds a query. RE2 runs in time linear in the input, so a
// long pattern is not a denial of service, but nothing legitimate is this long.
const MaxQueryBytes = 1000

// ErrEmptyQuery is a search with nothing to look for.
var ErrEmptyQuery = errors.New("wssearch: empty query")

// InvalidQueryError is a query that cannot be compiled — a regex Go's RE2
// rejects. Its message is safe to show: it describes the pattern, which is
// what the person typed.
type InvalidQueryError struct{ Reason string }

func (e *InvalidQueryError) Error() string { return "wssearch: invalid query: " + e.Reason }

// matcher finds a query in one line of text. Matching is per line: a pattern
// never spans a newline, so `^` and `$` mean the line's ends.
type matcher struct {
	// lit is the fast path: a case-sensitive literal is found with
	// bytes.Index, which the runtime accelerates; everything else compiles to
	// a regexp. Exactly one of lit and re is set.
	lit       []byte
	re        *regexp.Regexp
	wholeWord bool
}

func newMatcher(o Options) (*matcher, error) {
	if o.Query == "" {
		return nil, ErrEmptyQuery
	}
	if len(o.Query) > MaxQueryBytes {
		return nil, &InvalidQueryError{Reason: fmt.Sprintf("the query is longer than %d bytes", MaxQueryBytes)}
	}
	m := &matcher{wholeWord: o.WholeWord}
	if !o.Regex && o.CaseSensitive {
		m.lit = []byte(o.Query)
		return m, nil
	}
	expr := o.Query
	if !o.Regex {
		expr = regexp.QuoteMeta(expr)
	}
	if !o.CaseSensitive {
		expr = "(?i)" + expr
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		reason := err.Error()
		// "error parsing regexp: " is Go's own prefix; the person typed a
		// pattern, not a Go program.
		reason = strings.TrimPrefix(reason, "error parsing regexp: ")
		return nil, &InvalidQueryError{Reason: reason}
	}
	m.re = re
	return m, nil
}

// couldMatch is a cheap whole-file test: false means no line of data can
// match, so the file need not be split into lines at all. Only a literal can
// say so; everything else answers true.
func (m *matcher) couldMatch(data []byte) bool {
	if m.lit != nil {
		return bytes.Contains(data, m.lit)
	}
	return true
}

// each calls fn with the byte range of every match in line, in order, until fn
// returns false. Empty matches (a regex like `a*`) are not matches.
//
// A regexp is asked for matches in batches, not all at once: a minified file's
// single multi-megabyte line searched for `e` has a million of them, and the
// caller wants a few hundred.
func (m *matcher) each(line []byte, fn func(start, end int) bool) {
	if m.lit != nil {
		for off := 0; off <= len(line); {
			i := bytes.Index(line[off:], m.lit)
			if i < 0 {
				return
			}
			start := off + i
			end := start + len(m.lit)
			if m.accepts(line, start, end) && !fn(start, end) {
				return
			}
			off = end
		}
		return
	}
	seen := 0
	for limit := 64; ; limit *= 8 {
		locs := m.re.FindAllIndex(line, limit)
		for _, loc := range locs[seen:] {
			if loc[0] == loc[1] {
				continue
			}
			if m.accepts(line, loc[0], loc[1]) && !fn(loc[0], loc[1]) {
				return
			}
		}
		seen = len(locs)
		if len(locs) < limit {
			return
		}
	}
}

func (m *matcher) accepts(line []byte, start, end int) bool {
	if !m.wholeWord {
		return true
	}
	return wholeWordAt(line, start, end)
}

// wholeWordAt reports whether line[start:end] stands alone as a word.
//
// A word character is a Unicode letter or digit or `_`. The boundary is only
// demanded where the match itself begins or ends with a word character: the
// query `.foo` is whole-word when it is not glued to more letters on its right,
// and nothing is required of what precedes the dot. That is the rule editors
// use, and it keeps `foo()` findable as a whole word.
func wholeWordAt(line []byte, start, end int) bool {
	first, _ := utf8.DecodeRune(line[start:end])
	if isWordRune(first) && start > 0 {
		prev, _ := utf8.DecodeLastRune(line[:start])
		if isWordRune(prev) {
			return false
		}
	}
	last, _ := utf8.DecodeLastRune(line[start:end])
	if isWordRune(last) && end < len(line) {
		next, _ := utf8.DecodeRune(line[end:])
		if isWordRune(next) {
			return false
		}
	}
	return true
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// utf16Cursor converts byte offsets in one line to UTF-16 code-unit offsets,
// which is what the editor (JavaScript strings, CodeMirror) counts in. It
// moves forward only, so converting every match of a long line is one pass
// over it rather than one per match. A byte that is not valid UTF-8 counts as
// one unit, like the replacement character it will be shown as.
type utf16Cursor struct {
	line []byte
	byte int
	unit int
}

func (c *utf16Cursor) at(offset int) int {
	if offset < c.byte {
		c.byte, c.unit = 0, 0
	}
	for c.byte < offset && c.byte < len(c.line) {
		r, size := utf8.DecodeRune(c.line[c.byte:])
		if r == utf8.RuneError && size <= 1 {
			size = 1
			c.unit++
		} else if r >= 0x10000 {
			c.unit += 2
		} else {
			c.unit++
		}
		c.byte += size
	}
	return c.unit
}

// utf16Len is the length of b in UTF-16 code units.
func utf16Len(b []byte) int {
	c := utf16Cursor{line: b}
	return c.at(len(b))
}

// Preview limits, in bytes. A preview is context for a hit, not the line: a
// minified bundle has megabyte lines, and one result must not carry one.
const (
	previewBefore = 80
	previewAfter  = 160
)

// previewOf returns the slice of line around [start,end) to show, with the byte
// offset it begins at. Leading indentation before the match is dropped so a
// deeply nested hit does not render as a row of spaces, and the cut never
// splits a UTF-8 sequence.
func previewOf(line []byte, start, end int) (text string, from int) {
	from = start - previewBefore
	if from < 0 {
		from = 0
	}
	to := end + previewAfter
	if to > len(line) {
		to = len(line)
	}
	for from > 0 && from < len(line) && !utf8.RuneStart(line[from]) {
		from--
	}
	for to < len(line) && !utf8.RuneStart(line[to]) {
		to++
	}
	if from < start && len(bytes.Trim(line[from:start], " \t")) == 0 {
		from = start
	}
	return strings.ToValidUTF8(string(line[from:to]), "�"), from
}
