package pdfdoc

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// This file is the pure half of reading a page: turning glyph boxes into lines
// and rows, and deciding which lines are the page itself and which are what a
// browser printed around it. No PDF engine in here, so every rule is a test.
//
// Coordinates are PDF user space: points, origin bottom-left, y up.

// glyph is one character the engine found, in the order the engine read it.
type glyph struct {
	s              string
	x0, y0, x1, y1 float64 // left, bottom, right, top
}

// seg is a run of glyphs on one line with no real gap inside — a word, a
// sentence, one cell of a table row.
type seg struct {
	text           string
	x0, x1, bottom float64
	top            float64
}

// line is what the engine treats as one line of text: the segments on it,
// left to right.
type line struct {
	segs                []seg
	x0, x1, bottom, top float64
	chars               int // non-space characters
}

func (l line) text() string {
	parts := make([]string, len(l.segs))
	for i, s := range l.segs {
		parts[i] = strings.TrimSpace(s.text)
	}
	return strings.Join(parts, " ")
}

func (l line) centre() float64 { return (l.bottom + l.top) / 2 }
func (l line) height() float64 { return l.top - l.bottom }

func isBreak(s string) bool { return s == "\n" || s == "\r" || s == "\r\n" || s == " " }

func isSpaceGlyph(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// buildLines groups glyphs, in engine reading order, into lines of segments.
// The engine already breaks lines where the page does; a line is then cut into
// segments where the horizontal gap is more than a word space — which is where
// a table row's cells are.
func buildLines(glyphs []glyph) []line {
	var out []line
	var cur []glyph
	flush := func() {
		if l, ok := makeLine(cur); ok {
			out = append(out, l)
		}
		cur = nil
	}
	for _, g := range glyphs {
		if isBreak(g.s) {
			flush()
			continue
		}
		cur = append(cur, g)
	}
	flush()
	return out
}

func makeLine(glyphs []glyph) (line, bool) {
	var heights []float64
	for _, g := range glyphs {
		if !isSpaceGlyph(g.s) && g.y1 > g.y0 {
			heights = append(heights, g.y1-g.y0)
		}
	}
	if len(heights) == 0 {
		return line{}, false
	}
	sort.Float64s(heights)
	h := heights[len(heights)/2]
	splitGap := math.Max(2.2*h, 6)

	var l line
	var cur *seg
	prevRight := 0.0
	for _, g := range glyphs {
		if isSpaceGlyph(g.s) {
			if cur != nil && !strings.HasSuffix(cur.text, " ") {
				cur.text += " "
			}
			continue
		}
		if cur == nil || g.x0-prevRight > splitGap || g.x0 < prevRight-2*h {
			if cur != nil {
				l.segs = append(l.segs, *cur)
			}
			cur = &seg{x0: g.x0, x1: g.x1, bottom: g.y0, top: g.y1}
		}
		cur.text += g.s
		cur.x1 = math.Max(cur.x1, g.x1)
		cur.bottom = math.Min(cur.bottom, g.y0)
		cur.top = math.Max(cur.top, g.y1)
		prevRight = g.x1
		l.chars += len([]rune(g.s))
	}
	if cur != nil {
		l.segs = append(l.segs, *cur)
	}
	if len(l.segs) == 0 {
		return line{}, false
	}
	sort.SliceStable(l.segs, func(i, j int) bool { return l.segs[i].x0 < l.segs[j].x0 })
	l.x0, l.x1 = l.segs[0].x0, l.segs[0].x1
	l.bottom, l.top = l.segs[0].bottom, l.segs[0].top
	for _, s := range l.segs[1:] {
		l.x0 = math.Min(l.x0, s.x0)
		l.x1 = math.Max(l.x1, s.x1)
		l.bottom = math.Min(l.bottom, s.bottom)
		l.top = math.Max(l.top, s.top)
	}
	return l, true
}

// row is lines that sit side by side on the page: the cells of one table row
// whose text the engine read as separate lines, or a line and the note beside it.
type row struct {
	lines  []line
	centre float64
	x0, x1 float64
}

// buildRows sorts lines top to bottom and joins those on the same baseline that
// do not overlap horizontally.
func buildRows(lines []line) []row {
	ordered := append([]line(nil), lines...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if d := ordered[i].centre() - ordered[j].centre(); math.Abs(d) > 0.5*math.Min(ordered[i].height(), ordered[j].height()) {
			return d > 0
		}
		return ordered[i].x0 < ordered[j].x0
	})
	var rows []row
	for _, l := range ordered {
		joined := false
		for i := len(rows) - 1; i >= 0 && i >= len(rows)-3; i-- {
			r := &rows[i]
			if math.Abs(l.centre()-r.centre) > 0.5*math.Min(l.height(), rowHeight(*r)) {
				continue
			}
			if l.x0 < r.x1-1 && l.x1 > r.x0+1 && overlapsAny(r.lines, l) {
				continue
			}
			r.lines = append(r.lines, l)
			r.centre = (r.centre*float64(len(r.lines)-1) + l.centre()) / float64(len(r.lines))
			r.x0, r.x1 = math.Min(r.x0, l.x0), math.Max(r.x1, l.x1)
			joined = true
			break
		}
		if !joined {
			rows = append(rows, row{lines: []line{l}, centre: l.centre(), x0: l.x0, x1: l.x1})
		}
	}
	for i := range rows {
		sort.SliceStable(rows[i].lines, func(a, b int) bool { return rows[i].lines[a].x0 < rows[i].lines[b].x0 })
	}
	return rows
}

func rowHeight(r row) float64 {
	h := math.MaxFloat64
	for _, l := range r.lines {
		h = math.Min(h, l.height())
	}
	return h
}

func overlapsAny(lines []line, l line) bool {
	for _, o := range lines {
		if l.x0 < o.x1-1 && l.x1 > o.x0+1 {
			return true
		}
	}
	return false
}

// flowText writes rows in reading order: a row's cells are separated by a tab,
// so a table row stays one line the agent can split on.
func flowText(rows []row) string {
	var b strings.Builder
	for _, r := range rows {
		var cells []string
		for _, l := range r.lines {
			for _, s := range l.segs {
				if t := strings.TrimSpace(s.text); t != "" {
					cells = append(cells, t)
				}
			}
		}
		if len(cells) == 0 {
			continue
		}
		b.WriteString(strings.Join(cells, "\t"))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// layoutText writes rows on a character grid, columns where they sit on the
// page — the way a fixed-width printout of the page would, so a multi-column
// table keeps its columns. Wide gaps are capped so a sparse page does not
// become a wall of spaces.
func layoutText(rows []row) string {
	var widths []float64
	for _, r := range rows {
		for _, l := range r.lines {
			for _, s := range l.segs {
				if n := len([]rune(strings.TrimSpace(s.text))); n > 2 && s.x1 > s.x0 {
					widths = append(widths, (s.x1-s.x0)/float64(n))
				}
			}
		}
	}
	charW := 5.0
	if len(widths) > 0 {
		sort.Float64s(widths)
		charW = math.Max(widths[len(widths)/2], 2.5)
	}
	minX := math.MaxFloat64
	for _, r := range rows {
		minX = math.Min(minX, r.x0)
	}
	var b strings.Builder
	for _, r := range rows {
		var buf []rune
		for _, l := range r.lines {
			for _, s := range l.segs {
				text := strings.TrimSpace(s.text)
				if text == "" {
					continue
				}
				col := int(math.Round((s.x0 - minX) / charW))
				// A cell never lands on the one before it, and a long gap is
				// shortened: alignment matters, not the exact width.
				if len(buf) > 0 {
					if col < len(buf)+1 {
						col = len(buf) + 1
					}
					if col > len(buf)+9 {
						col = len(buf) + 9
					}
				}
				for len(buf) < col {
					buf = append(buf, ' ')
				}
				buf = append(buf, []rune(text)...)
			}
		}
		if len(buf) > 0 {
			b.WriteString(strings.TrimRight(string(buf), " "))
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
