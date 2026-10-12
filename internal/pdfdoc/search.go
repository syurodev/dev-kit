package pdfdoc

import (
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Search is by what a reader means, not by bytes: case and diacritics are
// ignored, so "xac thuc" finds "xác thực" and "DOANH NGHIEP" finds "Doanh
// nghiệp" — people type Vietnamese both ways — and whitespace, including the
// tabs between table cells and the line breaks, is one space.

// fold lowercases and strips diacritics, one rune out per rune in, so an index
// into the folded text is an index into the original. (đ has no decomposition;
// it is mapped by hand.)
func fold(text string) []rune {
	in := []rune(text)
	out := make([]rune, len(in))
	for i, r := range in {
		out[i] = foldRune(r)
	}
	return out
}

func foldRune(r rune) rune {
	switch r {
	case 'đ', 'Đ':
		return 'd'
	}
	if unicode.IsSpace(r) {
		return ' '
	}
	if r < 0x80 {
		return unicode.ToLower(r)
	}
	// NFD splits a precomposed letter into its base and marks; the base is the
	// first rune. A rune that is itself a mark stays (rare; keeps the 1:1).
	d := []rune(norm.NFD.String(string(r)))
	if len(d) > 0 && !unicode.Is(unicode.Mn, d[0]) {
		return unicode.ToLower(d[0])
	}
	return unicode.ToLower(r)
}

// collapse folds runs of spaces to one and returns, for every rune kept, its
// index in the original (so a hit can be quoted from the original text).
func collapse(folded []rune) (text []rune, index []int) {
	text = make([]rune, 0, len(folded))
	index = make([]int, 0, len(folded))
	space := true // drops leading spaces
	for i, r := range folded {
		if r == ' ' {
			if space {
				continue
			}
			space = true
		} else {
			space = false
		}
		text = append(text, r)
		index = append(index, i)
	}
	return text, index
}

// Hit is one place a query was found.
type Hit struct {
	Page    int    `json:"page"`
	Snippet string `json:"snippet"`
	// Match is "phrase" (the words together, in order) or "words" (all of them
	// on the page, not together — the snippet is around the first).
	Match string `json:"match"`
	// Zone is where on the page it was: body, or a chrome region when the
	// search included it.
	Zone Zone `json:"zone,omitempty"`
}

// searchPage finds a query on one page's text. words is the folded query split
// on spaces; phrase the same joined. At most maxPerPage phrase hits.
func searchPage(page int, text string, phrase string, words []string, maxPerPage int, radius int) []Hit {
	orig := []rune(text)
	folded, index := collapse(fold(text))
	hay := string(folded)
	var hits []Hit
	start := 0
	for len(hits) < maxPerPage {
		i := strings.Index(hay[start:], phrase)
		if i < 0 {
			break
		}
		at := start + i
		// byte offset → rune offset in the folded text
		r := len([]rune(hay[:at]))
		hits = append(hits, Hit{Page: page, Match: "phrase", Snippet: snippet(orig, index, r, len([]rune(phrase)), radius)})
		start = at + len(phrase)
	}
	if len(hits) > 0 || len(words) < 2 {
		return hits
	}
	// Not the phrase: the words, all within a short stretch of each other. A
	// page that merely contains each of them somewhere is not an answer.
	const window = 160
	hayRunes := []rune(hay)
	anchor := words[0]
	for from := 0; ; {
		i := strings.Index(hay[from:], anchor)
		if i < 0 {
			return nil
		}
		at := len([]rune(hay[:from+i]))
		lo, hi := at-window, at+window
		if lo < 0 {
			lo = 0
		}
		if hi > len(hayRunes) {
			hi = len(hayRunes)
		}
		near := string(hayRunes[lo:hi])
		all := true
		for _, w := range words[1:] {
			if !strings.Contains(near, w) {
				all = false
				break
			}
		}
		if all {
			return []Hit{{Page: page, Match: "words", Snippet: snippet(orig, index, at, len([]rune(anchor)), radius)}}
		}
		from += i + len(anchor)
	}
}

func snippet(orig []rune, index []int, at, length, radius int) string {
	if at >= len(index) {
		return ""
	}
	end := at + length
	if end > len(index) {
		end = len(index)
	}
	from := index[at] - radius
	if from < 0 {
		from = 0
	}
	last := index[end-1] + radius
	if last >= len(orig) {
		last = len(orig) - 1
	}
	s := strings.Join(strings.Fields(string(orig[from:last+1])), " ")
	if from > 0 {
		s = "…" + s
	}
	if last < len(orig)-1 {
		s += "…"
	}
	return s
}

// queryTerms folds a query: the whole phrase and its words.
func queryTerms(query string) (phrase string, words []string) {
	text, _ := collapse(fold(query))
	phrase = strings.TrimSpace(string(text))
	words = strings.Fields(phrase)
	return phrase, words
}

// sortHits orders hits by page, then by how well they matched.
func sortHits(hits []Hit) {
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Match != hits[j].Match {
			return hits[i].Match == "phrase"
		}
		return hits[i].Page < hits[j].Page
	})
}
