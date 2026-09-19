// internal/codegraph/analyzer_ts_scan.go
package codegraph

import "strings"

// A scanner over TypeScript source that knows just enough of the language to
// not be fooled by it: it steps over strings, templates and comments, and it
// tracks bracket depth.
//
// That is the whole job. It never parses a declaration — the language server
// has already said where every declaration is — so what is left is finding a
// matching bracket and splitting an argument list, two things a naive scan
// gets wrong the moment a route path contains a comma or a comment contains a
// parenthesis.

type tsScanner struct {
	text  string
	pos   int
	depth int
}

func newTSScanner(text string) *tsScanner { return &tsScanner{text: text} }

func (s *tsScanner) done() bool { return s.pos >= len(s.text) }

func (s *tsScanner) peek() byte {
	if s.done() {
		return 0
	}
	return s.text[s.pos]
}

// advance moves past exactly one meaningful position: one character, or a
// whole string, template or comment. Bracket depth is updated as it goes, so a
// caller testing depth before calling advance sees the depth OUTSIDE the
// bracket it is looking at.
func (s *tsScanner) advance() {
	if s.done() {
		return
	}
	switch c := s.text[s.pos]; {
	case c == '/' && s.pos+1 < len(s.text) && s.text[s.pos+1] == '/':
		if end := strings.IndexByte(s.text[s.pos:], '\n'); end >= 0 {
			s.pos += end
		} else {
			s.pos = len(s.text)
		}
	case c == '/' && s.pos+1 < len(s.text) && s.text[s.pos+1] == '*':
		if end := strings.Index(s.text[s.pos+2:], "*/"); end >= 0 {
			s.pos += end + 4
		} else {
			s.pos = len(s.text)
		}
	case c == '\'' || c == '"' || c == '`':
		s.skipString(c)
	case c == '(' || c == '[' || c == '{' || c == '<':
		s.depth++
		s.pos++
	case c == ')' || c == ']' || c == '}' || c == '>':
		// Clamped: a lone '>' is an operator as often as it is the end of a
		// type argument list, and a depth that can go negative turns every
		// later top-level test into a lie.
		if s.depth > 0 {
			s.depth--
		}
		s.pos++
	default:
		s.pos++
	}
}

func (s *tsScanner) skipString(quote byte) {
	s.pos++
	for s.pos < len(s.text) {
		switch s.text[s.pos] {
		case '\\':
			s.pos += 2
			continue
		case quote:
			s.pos++
			return
		case '\n':
			if quote != '`' {
				// An unterminated quoted string does not run past its line;
				// treating it as if it did would swallow the rest of the file.
				return
			}
		}
		s.pos++
	}
}

func (s *tsScanner) skipSpace() {
	for !s.done() {
		switch s.text[s.pos] {
		case ' ', '\t', '\r', '\n':
			s.pos++
		case '/':
			if s.pos+1 < len(s.text) && (s.text[s.pos+1] == '/' || s.text[s.pos+1] == '*') {
				s.advance()
				continue
			}
			return
		default:
			return
		}
	}
}

func (s *tsScanner) readIdentifier() string {
	start := s.pos
	for !s.done() {
		c := s.text[s.pos]
		letter := c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		digit := c >= '0' && c <= '9'
		if letter || (s.pos > start && digit) {
			s.pos++
			continue
		}
		break
	}
	return s.text[start:s.pos]
}

// readArgs consumes a parenthesised list and returns its top-level arguments
// as source text. It reports false for a list that never closes, which is what
// a file being edited looks like.
func (s *tsScanner) readArgs() ([]string, bool) {
	if s.done() || s.peek() != '(' {
		return nil, false
	}
	opened := s.depth
	s.advance() // past '('

	var args []string
	start := s.pos
	for !s.done() {
		// Inside the list the depth is one above where it started; the closing
		// parenthesis is the one seen AT that depth, since advance only drops
		// back out as it steps over it.
		if s.depth == opened+1 && s.peek() == ')' {
			if arg := strings.TrimSpace(s.text[start:s.pos]); arg != "" {
				args = append(args, arg)
			}
			s.advance()
			return args, true
		}
		if s.depth == opened+1 && s.peek() == ',' {
			args = append(args, strings.TrimSpace(s.text[start:s.pos]))
			s.advance()
			start = s.pos
			continue
		}
		s.advance()
	}
	return nil, false
}

// skipBracketed steps over a balanced pair, for the type parameter list that
// can sit between a generic function's name and its arguments.
func (s *tsScanner) skipBracketed(open, close byte) bool {
	if s.done() || s.peek() != open {
		return false
	}
	depth := 0
	for !s.done() {
		switch s.peek() {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				s.advance()
				return true
			}
		}
		s.advance()
	}
	return false
}
