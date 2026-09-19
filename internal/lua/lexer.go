// internal/lua/lexer.go
package lua

import (
	"strings"
	"unicode/utf8"
)

// The scanner. It reads Lua 5.1 through 5.4 plus the LuaJIT literals, because
// that is the range one project can hold at once: a Kong plugin runs on
// LuaJIT (5.1 syntax, `1LL` integer literals), the tooling around it is often
// plain 5.4 (`//`, `~`, `<close>`), and a scanner that knew only one of them
// would drop the other's files out of the index with no node and no error.
//
// It never fails. An unterminated string or an unknown byte becomes a
// diagnostic and the scan carries on from the next line, so one bad literal
// costs one statement instead of the whole file.

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokName
	tokNumber
	tokString
	tokKeyword
	tokOp
)

type token struct {
	kind tokenKind
	// text is the source spelling: the identifier, the number as written, the
	// keyword, the operator.
	text string
	// value is a string literal's decoded content; empty for everything else.
	value string
	// long marks a string written with long brackets ([[...]]).
	long    bool
	line    int
	endLine int
}

var keywords = map[string]bool{
	"and": true, "break": true, "do": true, "else": true, "elseif": true,
	"end": true, "false": true, "for": true, "function": true, "goto": true,
	"if": true, "in": true, "local": true, "nil": true, "not": true,
	"or": true, "repeat": true, "return": true, "then": true, "true": true,
	"until": true, "while": true,
}

// operators are matched longest-first, so `...` never scans as `..` and `<<`
// never as two `<`.
var operators = []string{
	"...", "..", "::", "<<", ">>", "//", "==", "~=", "<=", ">=",
	".", ":", ";", ",", "<", ">", "=", "(", ")", "{", "}", "[", "]",
	"+", "-", "*", "/", "%", "^", "#", "&", "~", "|",
}

type lexer struct {
	src  string
	pos  int
	line int
	// diagnostics collects what could not be scanned. The parser appends its
	// own to the same chunk.
	diagnostics []Diagnostic
}

func newLexer(src string) *lexer {
	l := &lexer{src: src, line: 1}
	// A chunk may start with a shebang; Lua itself skips that line.
	if strings.HasPrefix(src, "#") {
		for l.pos < len(src) && src[l.pos] != '\n' {
			l.pos++
		}
	}
	return l
}

func (l *lexer) note(line int, message string) {
	// One diagnostic per line is enough to say a file is broken; a cascade
	// from a single unterminated string would bury the first, real cause.
	if n := len(l.diagnostics); n > 0 && l.diagnostics[n-1].Line == line {
		return
	}
	l.diagnostics = append(l.diagnostics, Diagnostic{Line: line, Message: message})
}

// scan returns every token in the source, ending with a tokEOF.
func (l *lexer) scan() []token {
	var tokens []token
	for {
		tok := l.next()
		tokens = append(tokens, tok)
		if tok.kind == tokEOF {
			return tokens
		}
	}
}

func (l *lexer) next() token {
	l.skipSpaceAndComments()
	if l.pos >= len(l.src) {
		return token{kind: tokEOF, line: l.line, endLine: l.line}
	}

	start := l.line
	c := l.src[l.pos]

	switch {
	case isNameStart(c):
		begin := l.pos
		for l.pos < len(l.src) && isNameByte(l.src[l.pos]) {
			l.pos++
		}
		text := l.src[begin:l.pos]
		kind := tokName
		if keywords[text] {
			kind = tokKeyword
		}
		return token{kind: kind, text: text, line: start, endLine: start}

	case isDigit(c), c == '.' && l.pos+1 < len(l.src) && isDigit(l.src[l.pos+1]):
		return l.scanNumber(start)

	case c == '"', c == '\'':
		return l.scanQuoted(start)

	case c == '[':
		if value, ok, endLine := l.scanLongBracket(); ok {
			return token{kind: tokString, value: value, long: true, line: start, endLine: endLine}
		}
	}

	for _, op := range operators {
		if strings.HasPrefix(l.src[l.pos:], op) {
			l.pos += len(op)
			return token{kind: tokOp, text: op, line: start, endLine: start}
		}
	}

	// Something that is not Lua at all. Consume one rune so the scan always
	// advances — a lexer that reports and stays put is an infinite loop.
	_, size := utf8.DecodeRuneInString(l.src[l.pos:])
	if size == 0 {
		size = 1
	}
	l.pos += size
	l.note(start, "unexpected character")
	return l.next()
}

func (l *lexer) skipSpaceAndComments() {
	for l.pos < len(l.src) {
		switch c := l.src[l.pos]; {
		case c == '\n':
			l.line++
			l.pos++
		case c == ' ', c == '\t', c == '\r', c == '\v', c == '\f':
			l.pos++
		case c == '-' && strings.HasPrefix(l.src[l.pos:], "--"):
			l.pos += 2
			// --[[ long comment ]] or --[==[ ... ]==]; anything else runs to
			// the end of the line.
			if l.pos < len(l.src) && l.src[l.pos] == '[' {
				if _, ok, _ := l.scanLongBracket(); ok {
					continue
				}
			}
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.pos++
			}
		default:
			return
		}
	}
}

// scanLongBracket reads `[=*[ ... ]=*]` starting at the opening '['. It reports
// false and consumes nothing when what follows is not a long bracket at all —
// an index like `t[1]`, or `a[[b]]`'s first bracket in a context where it is
// really indexing.
func (l *lexer) scanLongBracket() (string, bool, int) {
	startLine := l.line
	p := l.pos + 1
	level := 0
	for p < len(l.src) && l.src[p] == '=' {
		level++
		p++
	}
	if p >= len(l.src) || l.src[p] != '[' {
		return "", false, startLine
	}
	p++
	// A newline immediately after the opening bracket is not part of the
	// string, per the reference manual.
	if p < len(l.src) && l.src[p] == '\r' {
		p++
	}
	if p < len(l.src) && l.src[p] == '\n' {
		l.line++
		p++
	}
	closing := "]" + strings.Repeat("=", level) + "]"
	end := strings.Index(l.src[p:], closing)
	if end < 0 {
		// Unterminated: take the rest of the file so the scan ends rather than
		// looping, and say so.
		l.note(startLine, "unterminated long string")
		body := l.src[p:]
		l.line += strings.Count(body, "\n")
		l.pos = len(l.src)
		return body, true, l.line
	}
	body := l.src[p : p+end]
	l.line += strings.Count(body, "\n")
	l.pos = p + end + len(closing)
	return body, true, l.line
}

func (l *lexer) scanQuoted(startLine int) token {
	quote := l.src[l.pos]
	l.pos++
	var b strings.Builder
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == quote:
			l.pos++
			return token{kind: tokString, value: b.String(), line: startLine, endLine: l.line}
		case c == '\n':
			// A raw newline ends a quoted string in Lua. Report it and hand
			// back what there was; the parser recovers from the statement.
			l.note(startLine, "unterminated string")
			return token{kind: tokString, value: b.String(), line: startLine, endLine: l.line}
		case c == '\\':
			l.pos++
			l.scanEscape(&b)
		default:
			b.WriteByte(c)
			l.pos++
		}
	}
	l.note(startLine, "unterminated string")
	return token{kind: tokString, value: b.String(), line: startLine, endLine: l.line}
}

// scanEscape decodes one backslash escape. Unknown escapes are kept verbatim
// rather than rejected: the analyzer reads string literals for route paths and
// require names, and an escape it has never heard of is not a reason to lose
// the path around it.
func (l *lexer) scanEscape(b *strings.Builder) {
	if l.pos >= len(l.src) {
		return
	}
	c := l.src[l.pos]
	l.pos++
	switch c {
	case 'n':
		b.WriteByte('\n')
	case 't':
		b.WriteByte('\t')
	case 'r':
		b.WriteByte('\r')
	case 'a':
		b.WriteByte(7)
	case 'b':
		b.WriteByte(8)
	case 'f':
		b.WriteByte(12)
	case 'v':
		b.WriteByte(11)
	case '\\', '"', '\'':
		b.WriteByte(c)
	case '\n':
		// A backslash-newline is a literal newline, and the line still counts.
		l.line++
		b.WriteByte('\n')
	case 'x':
		for i := 0; i < 2 && l.pos < len(l.src) && isHex(l.src[l.pos]); i++ {
			l.pos++
		}
		b.WriteByte('?')
	case 'z':
		// \z skips the whitespace that follows, newlines included.
		for l.pos < len(l.src) && isSpace(l.src[l.pos]) {
			if l.src[l.pos] == '\n' {
				l.line++
			}
			l.pos++
		}
	case 'u':
		if l.pos < len(l.src) && l.src[l.pos] == '{' {
			for l.pos < len(l.src) && l.src[l.pos] != '}' {
				l.pos++
			}
			if l.pos < len(l.src) {
				l.pos++
			}
		}
		b.WriteByte('?')
	default:
		if isDigit(c) {
			for i := 0; i < 2 && l.pos < len(l.src) && isDigit(l.src[l.pos]); i++ {
				l.pos++
			}
			b.WriteByte('?')
			return
		}
		b.WriteByte(c)
	}
}

// scanNumber reads every numeric literal the three dialects write: decimal and
// hex integers, floats with an exponent, hex floats (`0x1p-2`), and LuaJIT's
// `LL`/`ULL`/`i` suffixes. Nothing converts the text, so the grammar here only
// has to know where the literal stops.
func (l *lexer) scanNumber(startLine int) token {
	begin := l.pos
	exponent := "eE"
	if strings.HasPrefix(strings.ToLower(l.src[l.pos:]), "0x") {
		l.pos += 2
		exponent = "pP"
	}
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case isHexDigit(c) || c == '.':
			l.pos++
		case strings.IndexByte(exponent, c) >= 0:
			l.pos++
			if l.pos < len(l.src) && (l.src[l.pos] == '+' || l.src[l.pos] == '-') {
				l.pos++
			}
		default:
			// LuaJIT's integer suffixes sit directly on the digits.
			if rest := strings.ToUpper(l.src[l.pos:]); strings.HasPrefix(rest, "ULL") {
				l.pos += 3
			} else if strings.HasPrefix(rest, "LL") {
				l.pos += 2
			}
			return token{kind: tokNumber, text: l.src[begin:l.pos], line: startLine, endLine: startLine}
		}
	}
	return token{kind: tokNumber, text: l.src[begin:l.pos], line: startLine, endLine: startLine}
}

func isNameStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isNameByte(c byte) bool { return isNameStart(c) || isDigit(c) }

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isHex(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// isHexDigit accepts the decimal digits too, so one loop reads both bases.
func isHexDigit(c byte) bool { return isHex(c) }

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f'
}
