package pdfdoc

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
)

// Zone says what a line of a page is part of.
type Zone string

const (
	ZoneBody         Zone = "body"
	ZoneHeader       Zone = "header"
	ZoneFooter       Zone = "footer"
	ZoneLeftSidebar  Zone = "left-sidebar"
	ZoneRightSidebar Zone = "right-sidebar"
)

// A page printed from a website carries the site around the document: a
// running header and footer (date, title, URL, "25/45"), and often a sidebar of
// navigation that the browser repeats on every sheet. None of it is the
// document, and an agent that reads it as the body quotes menu items as law.
//
// The rule is the one a reader uses, made explicit: something that comes back
// on page after page, in the same place, and sits in the margin — the top or
// bottom band, or a column beside the body — is chrome. Repetition alone is
// not enough (a table's header row repeats on every page and is the document),
// and position alone is not enough (a margin note is the document); both are
// required. Where a sidebar was found, the other lines in the same column on
// pages where it changes are sidebar too.
//
// It is a heuristic, and says so: Info reports what it found and ReadText names
// what it left out, so the agent can look at the page image and disagree.

const (
	chromeMinSamples = 3    // fewer pages cannot show repetition
	chromeSampleMax  = 30   // pages looked at to find it
	chromeBand       = 0.09 // top / bottom fraction of the page that is header / footer
	chromeRepeatFrac = 0.4  // share of the sampled pages a line must come back on
)

// pageLines is one page's lines with the page's size.
type pageLines struct {
	w, h  float64
	lines []line
}

// chromeModel is what repetition found in a document.
type chromeModel struct {
	repeated  map[string]Zone // geometry-keyed repeating lines → what they are
	bodyLeft  float64
	bodyRight float64
	left      *band
	right     *band
	sampled   int
}

type band struct{ x0, x1 float64 }

// normalise is a line's identity for repetition: case folded, digits made
// alike (so "25/45" and "26/45" are one line), spaces collapsed.
func normalise(text string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.IsDigit(r):
			b.WriteRune('#')
			space = false
		case unicode.IsSpace(r):
			if !space {
				b.WriteRune(' ')
			}
			space = true
		default:
			b.WriteRune(r)
			space = false
		}
	}
	return strings.TrimSpace(b.String())
}

func chromeKey(l line, w, h float64, dx, dy int) string {
	return fmt.Sprintf("%s|%d|%d", normalise(l.text()), int(math.Round(l.x0/4))+dx, int(math.Round((h-l.top)/h*200))+dy)
}

// detectChrome finds the repeating margin lines of a document from a sample of
// its pages (pass at most chromeSampleMax, spread over the document).
func detectChrome(pages []pageLines) chromeModel {
	m := chromeModel{repeated: map[string]Zone{}, sampled: len(pages)}
	if len(pages) < chromeMinSamples {
		return m
	}
	need := int(math.Ceil(chromeRepeatFrac * float64(len(pages))))
	if need < chromeMinSamples {
		need = chromeMinSamples
	}

	// How many pages each placed line appears on (once per page).
	type seen struct {
		pages map[int]bool
		line  line
		w, h  float64
	}
	counts := map[string]*seen{}
	for pi, p := range pages {
		for _, l := range p.lines {
			if strings.TrimSpace(l.text()) == "" {
				continue
			}
			key := chromeKey(l, p.w, p.h, 0, 0)
			s := counts[key]
			if s == nil {
				s = &seen{pages: map[int]bool{}, line: l, w: p.w, h: p.h}
				counts[key] = s
			}
			s.pages[pi] = true
		}
	}
	repeating := map[string]bool{}
	for key, s := range counts {
		if len(s.pages) >= need {
			repeating[key] = true
		}
	}

	// The body column, from lines that do not repeat and are long enough to be
	// prose or a table's wide row.
	var lefts, rights []float64
	for key, s := range counts {
		if repeating[key] {
			continue
		}
		if s.line.chars >= 25 {
			lefts = append(lefts, s.line.x0)
			rights = append(rights, s.line.x1)
		}
	}
	if len(lefts) >= 3 {
		sort.Float64s(lefts)
		sort.Float64s(rights)
		m.bodyLeft = lefts[len(lefts)/10]
		m.bodyRight = rights[len(rights)-1-len(rights)/10]
	} else {
		m.bodyLeft, m.bodyRight = 0, math.MaxFloat64
	}

	var leftRail, rightRail []line
	for key := range repeating {
		s := counts[key]
		fromTop := (s.h - s.line.top) / s.h
		fromBottom := s.line.bottom / s.h
		var zone Zone
		switch {
		case fromTop <= chromeBand:
			zone = ZoneHeader
		case fromBottom <= chromeBand:
			zone = ZoneFooter
		case m.bodyLeft > 0 && s.line.x1 <= m.bodyLeft+2:
			zone = ZoneLeftSidebar
			leftRail = append(leftRail, s.line)
		case m.bodyRight < math.MaxFloat64 && s.line.x0 >= m.bodyRight-2:
			zone = ZoneRightSidebar
			rightRail = append(rightRail, s.line)
		default:
			continue
		}
		m.repeated[key] = zone
	}
	// A sidebar is a column: one stray repeated line beside the body is not
	// evidence of one.
	if len(leftRail) >= 3 {
		m.left = bandOf(leftRail)
	} else {
		for key, r := range m.repeated {
			if r == ZoneLeftSidebar {
				delete(m.repeated, key)
			}
		}
	}
	if len(rightRail) >= 3 {
		m.right = bandOf(rightRail)
	} else {
		for key, r := range m.repeated {
			if r == ZoneRightSidebar {
				delete(m.repeated, key)
			}
		}
	}
	return m
}

func bandOf(lines []line) *band {
	b := &band{x0: math.MaxFloat64, x1: 0}
	for _, l := range lines {
		b.x0 = math.Min(b.x0, l.x0)
		b.x1 = math.Max(b.x1, l.x1)
	}
	return b
}

// classify says what one line of a page is.
func (m chromeModel) classify(l line, w, h float64) Zone {
	if len(m.repeated) > 0 {
		for _, dx := range []int{0, -1, 1} {
			for _, dy := range []int{0, -1, 1} {
				if r, ok := m.repeated[chromeKey(l, w, h, dx, dy)]; ok {
					return r
				}
			}
		}
	}
	// In the column a sidebar was found in, but a line that differs from page to
	// page (a "related" list that scrolled): still the sidebar.
	if m.left != nil && l.x1 <= m.bodyLeft+2 && l.x0 >= m.left.x0-6 {
		return ZoneLeftSidebar
	}
	if m.right != nil && l.x0 >= m.bodyRight-2 && l.x1 <= m.right.x1+6 {
		return ZoneRightSidebar
	}
	return ZoneBody
}

// summary is the plain-language account of what was found, for Info.
func (m chromeModel) summary() []string {
	var out []string
	counts := map[Zone]int{}
	for _, r := range m.repeated {
		counts[r]++
	}
	if n := counts[ZoneHeader]; n > 0 {
		out = append(out, fmt.Sprintf("running header (%d line%s repeated on most pages)", n, plural(n)))
	}
	if n := counts[ZoneFooter]; n > 0 {
		out = append(out, fmt.Sprintf("running footer (%d line%s repeated on most pages)", n, plural(n)))
	}
	if m.left != nil {
		out = append(out, fmt.Sprintf("left sidebar (x %.0f–%.0f pt, %d repeated lines)", m.left.x0, m.left.x1, counts[ZoneLeftSidebar]))
	}
	if m.right != nil {
		out = append(out, fmt.Sprintf("right sidebar (x %.0f–%.0f pt, %d repeated lines)", m.right.x0, m.right.x1, counts[ZoneRightSidebar]))
	}
	return out
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
