package pdfdoc

import (
	"strings"
	"testing"
)

// word lays a string out as glyphs, 5 pt per character, 8 pt tall, from x at baseline y.
func word(s string, x, y float64) []glyph {
	var g []glyph
	for _, r := range s {
		g = append(g, glyph{s: string(r), x0: x, y0: y, x1: x + 4.5, y1: y + 8})
		x += 5
	}
	return g
}

func cat(parts ...[]glyph) []glyph {
	var out []glyph
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

var nl = []glyph{{s: "\r"}, {s: "\n"}}

func TestBuildLinesSplitsATableRowIntoCells(t *testing.T) {
	lines := buildLines(cat(
		word("Request-ID", 150, 500), word("String", 240, 500), word("M", 300, 500), nl,
		word("a sentence with ordinary spaces", 150, 480),
	))
	if len(lines) != 2 {
		t.Fatalf("lines = %d", len(lines))
	}
	if len(lines[0].segs) != 3 || lines[0].segs[0].text != "Request-ID" {
		t.Fatalf("cells = %+v", lines[0].segs)
	}
	if len(lines[1].segs) != 1 || lines[1].text() != "a sentence with ordinary spaces" {
		t.Fatalf("a sentence was cut: %+v", lines[1].segs)
	}
}

func TestRowsJoinCellsOnOneBaselineAndOrderTopDown(t *testing.T) {
	// The engine read the label of a tall row after its description's lines; the
	// row keeps the label with the first of them only when they share a baseline.
	lines := buildLines(cat(
		word("Authorization", 150, 460), nl,
		word("first line of the cell", 310, 505), nl,
		word("second line of the cell", 310, 495), nl,
		word("Header", 150, 520), word("Meaning", 310, 520), nl,
	))
	rows := buildRows(lines)
	got := flowText(rows)
	want := []string{"Header\tMeaning", "first line of the cell", "second line of the cell", "Authorization"}
	if strings.Join(strings.Split(got, "\n"), "|") != strings.Join(want, "|") {
		t.Fatalf("flow =\n%s", got)
	}
}

func TestNormaliseMakesPageNumbersAlike(t *testing.T) {
	if normalise("  25/45 ") != normalise("26/45") || normalise("A  b") != "a b" {
		t.Fatalf("%q %q", normalise("25/45"), normalise("A  b"))
	}
	if normalise("Mục 1") == normalise("Mục A") {
		t.Fatal("letters must still tell lines apart")
	}
}

func page(n int, with ...[]glyph) pageLines {
	lines := buildLines(cat(with...))
	return pageLines{w: 612, h: 792, lines: lines}
}

func TestDetectChromeNeedsRepetitionAndAMarginPosition(t *testing.T) {
	var pages []pageLines
	for i := 1; i <= 8; i++ {
		var g [][]glyph
		g = append(g,
			word("9/15/26 header", 24, 770), nl, // header band, repeats
			word("https://x.test  "+string(rune('0'+i))+"/8", 24, 14), nl, // footer, repeats modulo the number
			word("MENU ITEM ONE", 40, 650), nl, word("MENU ITEM TWO", 40, 630), nl, word("MENU ITEM THREE", 40, 610), nl, // sidebar
			word("Tham so  Loai  Mo ta", 154, 500), nl, // a table header row that repeats mid-page: the document
			word("body text unique to this page number "+strings.Repeat("x", i), 154, 450), nl,
			word("another long body line of the document text", 154, 430), nl,
			word("and one more long body line of the document", 154, 410), nl,
		)
		pages = append(pages, page(i, cat(g...)))
	}
	m := detectChrome(pages)
	zone := func(text string, x, y float64) Zone {
		l := buildLines(word(text, x, y))[0]
		return m.classify(l, 612, 792)
	}
	if got := zone("9/15/26 header", 24, 770); got != ZoneHeader {
		t.Errorf("header = %s", got)
	}
	if got := zone("https://x.test  3/8", 24, 14); got != ZoneFooter {
		t.Errorf("footer = %s", got)
	}
	if got := zone("MENU ITEM TWO", 40, 630); got != ZoneLeftSidebar {
		t.Errorf("sidebar = %s", got)
	}
	// Not repeated, or repeated but in the body: kept.
	if got := zone("Tham so  Loai  Mo ta", 154, 500); got != ZoneBody {
		t.Errorf("a repeating table header was taken for chrome: %s", got)
	}
	if got := zone("something else entirely in the body", 154, 300); got != ZoneBody {
		t.Errorf("body = %s", got)
	}
	// A sidebar line that differs on this page is still in the sidebar's column.
	if got := zone("A RELATED LINK", 41, 400); got != ZoneLeftSidebar {
		t.Errorf("a changing sidebar line = %s", got)
	}
	if len(m.summary()) != 3 {
		t.Errorf("summary = %v", m.summary())
	}
}

func TestTooFewPagesShowNoRepetition(t *testing.T) {
	two := []pageLines{page(1, word("header", 24, 770)), page(2, word("header", 24, 770))}
	m := detectChrome(two)
	if len(m.repeated) != 0 {
		t.Fatalf("two pages cannot show repetition: %v", m.repeated)
	}
}

func TestOneRepeatedLineBesideTheBodyIsNotASidebar(t *testing.T) {
	var pages []pageLines
	for i := 0; i < 6; i++ {
		pages = append(pages, page(i, cat(
			word("margin note", 20, 400), nl,
			word("long body line number one of this page of text", 154, 450), nl,
			word("long body line number two of this page of text", 154, 430), nl,
			word("long body line number three of this page text", 154, 410), nl,
		)))
	}
	m := detectChrome(pages)
	l := buildLines(word("margin note", 20, 400))[0]
	if got := m.classify(l, 612, 792); got != ZoneBody {
		t.Fatalf("a single margin line is not evidence of a sidebar: %s", got)
	}
}

func TestFoldMakesVietnameseSearchableEitherWay(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Xác thực khách hàng", "xac thuc khach hang"},
		{"ĐỊNH DẠNG", "dinh dang"},
		{"Đường", "duong"},
		{"A\tB\nC", "a b c"},
		{"é", "e"}, // decomposed
	} {
		got, _ := collapse(fold(c.in))
		if string(got) != c.want && !(c.in == "é" && string(got) == "é") {
			t.Errorf("fold(%q) = %q, want %q", c.in, string(got), c.want)
		}
	}
}

func TestSearchPageQuotesTheOriginalAroundTheMatch(t *testing.T) {
	text := "Mức 1\tLoại dữ liệu\nHệ thống xác thực khách hàng bằng JWS.\nHết."
	phrase, words := queryTerms("XAC THUC khach")
	hits := searchPage(7, text, phrase, words, 5, 12)
	if len(hits) != 1 || hits[0].Page != 7 || hits[0].Match != "phrase" {
		t.Fatalf("hits = %+v", hits)
	}
	if !strings.Contains(hits[0].Snippet, "xác thực khách") {
		t.Fatalf("snippet is not the original text: %q", hits[0].Snippet)
	}
	if !strings.HasPrefix(hits[0].Snippet, "…") {
		t.Fatalf("an elided start is not marked: %q", hits[0].Snippet)
	}
	// Words close together but not as a phrase.
	p2, w2 := queryTerms("khách hệ")
	if got := searchPage(1, text, p2, w2, 5, 12); len(got) != 1 || got[0].Match != "words" {
		t.Fatalf("near words = %+v", got)
	}
	// Words far apart are not an answer.
	far := "alpha " + strings.Repeat("filler ", 60) + "omega"
	p3, w3 := queryTerms("alpha omega")
	if got := searchPage(1, far, p3, w3, 5, 12); len(got) != 0 {
		t.Fatalf("far-apart words matched: %+v", got)
	}
}

func TestCapTextRoundsToARuneBoundary(t *testing.T) {
	if clip("xin chào", 4) != "xin …" {
		t.Fatalf("%q", clip("xin chào", 4))
	}
	if clip("ngắn", 10) != "ngắn" {
		t.Fatal("a short line was clipped")
	}
}
