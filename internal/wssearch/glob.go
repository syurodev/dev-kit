package wssearch

import (
	"path"
	"strings"
)

// Include / exclude patterns.
//
// A pattern is matched against a file's path relative to its folder root,
// "/"-separated, case-sensitively (the folders are on filesystems of every
// kind, and a pattern that matched differently on each would make the same
// search return different files).
//
//	*.ts          no "/": any path segment matches — a file or a directory
//	              called x.ts, at any depth (gitignore's rule)
//	node_modules  same: also excludes everything below a directory of that name
//	src/**/*.go   has "/": anchored at the folder root
//	/docs         leading "/": anchored (same as docs/)
//	build/        trailing "/": the directory and everything below it
//	**            any number of segments, including none
//	*.{ts,tsx}    one level of braces expands to alternatives
//
// A path matches an anchored pattern when the pattern matches the whole path or
// any of its parent directories, so "src/gen" excludes src/gen/a/b.go.

type pattern struct {
	anchored bool
	segs     []string
}

// compilePatterns turns user patterns into matchers. Blank ones are dropped.
func compilePatterns(raw []string) []pattern {
	var out []pattern
	for _, r := range raw {
		for _, expanded := range expandBraces(strings.TrimSpace(r)) {
			p := strings.TrimSpace(expanded)
			p = strings.ReplaceAll(p, "\\", "/")
			if p == "" {
				continue
			}
			anchored := strings.HasPrefix(p, "/")
			p = strings.TrimPrefix(p, "./")
			p = strings.Trim(p, "/")
			if p == "" {
				continue
			}
			if strings.Contains(p, "/") {
				anchored = true
			}
			out = append(out, pattern{anchored: anchored, segs: strings.Split(p, "/")})
		}
	}
	return out
}

// expandBraces expands the first {a,b} group, recursively. Unbalanced braces
// are left alone as literal text.
func expandBraces(p string) []string {
	open := strings.IndexByte(p, '{')
	if open < 0 {
		return []string{p}
	}
	depth, closeAt := 0, -1
	for i := open; i < len(p); i++ {
		switch p[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				closeAt = i
			}
		}
		if closeAt >= 0 {
			break
		}
	}
	if closeAt < 0 {
		return []string{p}
	}
	var alts []string
	depth, start := 0, open+1
	for i := open + 1; i < closeAt; i++ {
		switch p[i] {
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				alts = append(alts, p[start:i])
				start = i + 1
			}
		}
	}
	alts = append(alts, p[start:closeAt])
	var out []string
	for _, alt := range alts {
		out = append(out, expandBraces(p[:open]+alt+p[closeAt+1:])...)
	}
	return out
}

func (p pattern) matches(segs []string) bool {
	if !p.anchored {
		// One segment pattern: any segment of the path.
		for _, s := range segs {
			if ok, _ := path.Match(p.segs[0], s); ok {
				return true
			}
		}
		return false
	}
	for end := 1; end <= len(segs); end++ {
		if matchSegments(p.segs, segs[:end]) {
			return true
		}
	}
	return false
}

func matchSegments(pat, segs []string) bool {
	if len(pat) == 0 {
		return len(segs) == 0
	}
	if pat[0] == "**" {
		for skip := 0; skip <= len(segs); skip++ {
			if matchSegments(pat[1:], segs[skip:]) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 {
		return false
	}
	if ok, _ := path.Match(pat[0], segs[0]); !ok {
		return false
	}
	return matchSegments(pat[1:], segs[1:])
}

// pathFilter decides which files are searched.
type pathFilter struct {
	include []pattern
	exclude []pattern
}

func newPathFilter(include, exclude []string) pathFilter {
	return pathFilter{include: compilePatterns(include), exclude: compilePatterns(exclude)}
}

func (f pathFilter) empty() bool { return len(f.include) == 0 && len(f.exclude) == 0 }

// allows reports whether the file at rel (slash-separated) is searched:
// excluded paths never are, and with include patterns only matching ones are.
func (f pathFilter) allows(rel string) bool {
	segs := strings.Split(rel, "/")
	for _, p := range f.exclude {
		if p.matches(segs) {
			return false
		}
	}
	if len(f.include) == 0 {
		return true
	}
	for _, p := range f.include {
		if p.matches(segs) {
			return true
		}
	}
	return false
}
