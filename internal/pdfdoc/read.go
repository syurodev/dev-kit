package pdfdoc

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	pdfium "github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/enums"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
)

// objectScanCap bounds how many page objects are looked at to count images; a
// vector-heavy page can hold tens of thousands and an image is found long before.
const objectScanCap = 600

// readPage reads one page (0-based) into the cache entry, or returns what is
// already there.
func (e *Engine) readPage(p pdfium.Pdfium, doc references.FPDF_DOCUMENT, entry *docEntry, index int) (*pageInfo, error) {
	if cached := entry.page(index); cached != nil {
		return cached, nil
	}
	ref := pageRef(doc, index)
	w, err := p.FPDF_GetPageWidthF(&requests.FPDF_GetPageWidthF{Page: ref})
	if err != nil {
		return nil, fmt.Errorf("%w: page %d: %v", ErrCorrupt, index+1, err)
	}
	h, err := p.FPDF_GetPageHeightF(&requests.FPDF_GetPageHeightF{Page: ref})
	if err != nil {
		return nil, fmt.Errorf("%w: page %d: %v", ErrCorrupt, index+1, err)
	}
	info := &pageInfo{w: float64(w.PageWidth), h: float64(h.PageHeight)}

	text, err := p.GetPageTextStructured(&requests.GetPageTextStructured{
		Page: ref,
		Mode: requests.GetPageTextStructuredModeChars,
	})
	if err == nil {
		glyphs := make([]glyph, 0, len(text.Chars))
		for _, c := range text.Chars {
			glyphs = append(glyphs, glyph{
				s:  c.Text,
				x0: c.PointPosition.Left, y0: c.PointPosition.Bottom,
				x1: c.PointPosition.Right, y1: c.PointPosition.Top,
			})
		}
		info.lines = buildLines(glyphs)
		for _, l := range info.lines {
			info.chars += l.chars
		}
	}
	// A page whose text cannot be read is a page without text, not a failed
	// document: the image is still there and the agent is told so.

	if n, err := p.FPDFPage_CountObjects(&requests.FPDFPage_CountObjects{Page: ref}); err == nil {
		limit := n.Count
		if limit > objectScanCap {
			limit = objectScanCap
		}
		for i := 0; i < limit && info.images < 3; i++ {
			obj, err := p.FPDFPage_GetObject(&requests.FPDFPage_GetObject{Page: ref, Index: i})
			if err != nil {
				break
			}
			if t, err := p.FPDFPageObj_GetType(&requests.FPDFPageObj_GetType{PageObject: obj.PageObject}); err == nil && t.Type == enums.FPDF_PAGEOBJ_IMAGE {
				info.images++
			}
		}
	}
	entry.put(index, info)
	return info, nil
}

// chromeFor returns the document's chrome model, building it from a sample of
// pages the first time.
func (e *Engine) chromeFor(ctx context.Context, p pdfium.Pdfium, doc references.FPDF_DOCUMENT, entry *docEntry) (*chromeModel, error) {
	entry.mu.Lock()
	if entry.chrome != nil {
		m := entry.chrome
		entry.mu.Unlock()
		return m, nil
	}
	entry.mu.Unlock()

	var sample []pageLines
	for _, i := range sampleIndexes(entry.pages, chromeSampleMax) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pi, err := e.readPage(p, doc, entry, i)
		if err != nil {
			return nil, err
		}
		if pi.hasText() {
			sample = append(sample, pageLines{w: pi.w, h: pi.h, lines: pi.lines})
		}
	}
	m := detectChrome(sample)
	entry.mu.Lock()
	entry.chrome = &m
	entry.mu.Unlock()
	return &m, nil
}

// sampleIndexes picks up to n page indexes spread over [0, count), always
// including the first and the last.
func sampleIndexes(count, n int) []int {
	if count <= n {
		out := make([]int, count)
		for i := range out {
			out[i] = i
		}
		return out
	}
	out := make([]int, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, i*(count-1)/(n-1))
	}
	return out
}

// TextLayer says how much of a document has text to read.
type TextLayer struct {
	// State is "full" (every page), "partial", or "none" (a scan, or only images).
	State string `json:"state"`
	// PagesWithoutText lists pages with no text layer (capped; Count is exact).
	PagesWithoutText []int `json:"pagesWithoutText,omitempty"`
	Count            int   `json:"pagesWithoutTextCount"`
	// Sampled is true when the document was too long to check page by page.
	Sampled bool `json:"sampled,omitempty"`
}

// OCR says whether DevKit can turn a scanned page into text. It cannot: there
// is no OCR engine in this build, so the way to read a scan is to look at it.
type OCR struct {
	Available bool   `json:"available"`
	Note      string `json:"note"`
}

// Info describes a document.
type Info struct {
	Pages     int        `json:"pages"`
	Title     string     `json:"title,omitempty"`
	Author    string     `json:"author,omitempty"`
	Subject   string     `json:"subject,omitempty"`
	Keywords  string     `json:"keywords,omitempty"`
	Creator   string     `json:"creator,omitempty"`
	Producer  string     `json:"producer,omitempty"`
	Created   string     `json:"created,omitempty"`
	Modified  string     `json:"modified,omitempty"`
	PageSize  [2]float64 `json:"pageSizePoints"`
	TextLayer TextLayer  `json:"textLayer"`
	// Chrome is what was found printed around the document and left out of
	// text by default (a website's sidebar, a running header and footer).
	Chrome []string `json:"pageChrome,omitempty"`
	OCR    OCR      `json:"ocr"`
}

// OCRNote is the one sentence about scans, kept in one place so the tool, the
// prompt and the error agree.
const OCRNote = "DevKit has no OCR engine. A page with no text layer can only be read by looking at its image (viewPages)."

// Inspect reads a document's metadata and works out whether it has a text
// layer. It is what an attachment is checked with when it is added: an
// unreadable PDF fails here, with its reason.
func (e *Engine) Inspect(ctx context.Context, src Source) (Info, error) {
	var info Info
	err := e.open(ctx, src, func(p pdfium.Pdfium, doc references.FPDF_DOCUMENT, pages int) error {
		entry := e.docs.get(src.key(), pages)
		info.Pages = pages
		meta := func(tag string) string {
			r, err := p.FPDF_GetMetaText(&requests.FPDF_GetMetaText{Document: doc, Tag: tag})
			if err != nil {
				return ""
			}
			return strings.TrimSpace(r.Value)
		}
		info.Title, info.Author, info.Subject, info.Keywords = meta("Title"), meta("Author"), meta("Subject"), meta("Keywords")
		info.Creator, info.Producer = meta("Creator"), meta("Producer")
		info.Created, info.Modified = meta("CreationDate"), meta("ModDate")

		first, err := e.readPage(p, doc, entry, 0)
		if err != nil {
			return err
		}
		info.PageSize = [2]float64{first.w, first.h}

		indexes := sampleIndexes(pages, infoSamplePages)
		if pages <= infoFullScanPages {
			indexes = sampleIndexes(pages, pages)
		}
		var without []int
		for _, i := range indexes {
			if err := ctx.Err(); err != nil {
				return err
			}
			pi, err := e.readPage(p, doc, entry, i)
			if err != nil {
				return err
			}
			if !pi.hasText() {
				without = append(without, i+1)
			}
		}
		info.TextLayer = TextLayer{Count: len(without), Sampled: len(indexes) < pages}
		switch {
		case len(without) == 0:
			info.TextLayer.State = "full"
		case len(without) == len(indexes):
			info.TextLayer.State = "none"
		default:
			info.TextLayer.State = "partial"
		}
		if len(without) > 60 {
			without = without[:60]
		}
		info.TextLayer.PagesWithoutText = without

		if model, err := e.chromeFor(ctx, p, doc, entry); err == nil {
			info.Chrome = model.summary()
		}
		info.OCR = OCR{Available: false, Note: OCRNote}
		return nil
	})
	return info, err
}

// TextOptions select what ReadText returns.
type TextOptions struct {
	// IncludeChrome keeps the sidebar, header and footer a web page printed
	// around the document; by default they are left out and counted.
	IncludeChrome bool
	// Layout keeps the page's columns on a character grid instead of one cell
	// per tab-separated field — for tables.
	Layout bool
}

// PageText is one page's text.
type PageText struct {
	Page int    `json:"page"`
	Text string `json:"text"`
	// HasTextLayer is false for a scan or an image-only page: Text is empty and
	// the page can only be read by viewing it.
	HasTextLayer bool `json:"hasTextLayer"`
	// ChromeLines is how many lines were left out as page chrome, with the first
	// few quoted so the agent can tell whether the call was right.
	ChromeLines  int      `json:"chromeLinesOmitted,omitempty"`
	ChromeSample []string `json:"chromeSample,omitempty"`
}

// ReadText returns the text of the given pages (1-based, any order, at most
// MaxReadPages).
func (e *Engine) ReadText(ctx context.Context, src Source, pages []int, o TextOptions) ([]PageText, error) {
	if len(pages) == 0 {
		return nil, fmt.Errorf("%w: no pages named", ErrPageRange)
	}
	if len(pages) > MaxReadPages {
		return nil, fmt.Errorf("%w: %d pages in one call, limit %d", ErrPageRange, len(pages), MaxReadPages)
	}
	var out []PageText
	err := e.open(ctx, src, func(p pdfium.Pdfium, doc references.FPDF_DOCUMENT, count int) error {
		if err := checkPages(pages, count); err != nil {
			return err
		}
		entry := e.docs.get(src.key(), count)
		var model *chromeModel
		if !o.IncludeChrome {
			m, err := e.chromeFor(ctx, p, doc, entry)
			if err != nil {
				return err
			}
			model = m
		}
		for _, n := range pages {
			if err := ctx.Err(); err != nil {
				return err
			}
			pi, err := e.readPage(p, doc, entry, n-1)
			if err != nil {
				return err
			}
			out = append(out, pageText(n, pi, model, o))
		}
		return nil
	})
	return out, err
}

func pageText(n int, pi *pageInfo, model *chromeModel, o TextOptions) PageText {
	res := PageText{Page: n, HasTextLayer: pi.hasText()}
	if !res.HasTextLayer {
		return res
	}
	kept := pi.lines
	if model != nil {
		kept = kept[:0:0]
		for _, l := range pi.lines {
			if model.classify(l, pi.w, pi.h) != ZoneBody {
				res.ChromeLines++
				if len(res.ChromeSample) < 3 {
					res.ChromeSample = append(res.ChromeSample, clip(l.text(), 80))
				}
				continue
			}
			kept = append(kept, l)
		}
	}
	rows := buildRows(kept)
	if o.Layout {
		res.Text = layoutText(rows)
	} else {
		res.Text = flowText(rows)
	}
	return res
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// SearchOptions select where and how to search.
type SearchOptions struct {
	Query         string
	FromPage      int // 1-based; 0 = 1
	ToPage        int // 0 = last
	IncludeChrome bool
	MaxHits       int // default 15, at most 50
}

// SearchResult is the answer to a search, including what it could not cover.
type SearchResult struct {
	Hits []Hit `json:"hits"`
	// Searched is how many pages were looked at.
	Searched int `json:"pagesSearched"`
	// Complete is false when the time budget ran out; ResumeFrom is where to
	// continue. Silence about uncovered pages would read as "not found".
	Complete   bool `json:"complete"`
	ResumeFrom int  `json:"resumeFromPage,omitempty"`
	// Truncated is true when there were more hits than MaxHits.
	Truncated bool `json:"truncated,omitempty"`
	// PagesWithoutText are pages in range that cannot be searched: scans.
	PagesWithoutText []int `json:"pagesWithoutText,omitempty"`
}

// Search finds a phrase, or failing that its words, on each page.
func (e *Engine) Search(ctx context.Context, src Source, o SearchOptions) (SearchResult, error) {
	phrase, words := queryTerms(o.Query)
	if phrase == "" {
		return SearchResult{}, fmt.Errorf("%w: the query is empty", ErrPageRange)
	}
	maxHits := o.MaxHits
	if maxHits <= 0 {
		maxHits = 15
	}
	if maxHits > 50 {
		maxHits = 50
	}
	res := SearchResult{Hits: []Hit{}, Complete: true}
	err := e.open(ctx, src, func(p pdfium.Pdfium, doc references.FPDF_DOCUMENT, count int) error {
		from, to := o.FromPage, o.ToPage
		if from <= 0 {
			from = 1
		}
		if to <= 0 || to > count {
			to = count
		}
		if err := checkPages([]int{from, to}, count); err != nil {
			return err
		}
		if from > to {
			return fmt.Errorf("%w: from page %d is after page %d", ErrPageRange, from, to)
		}
		entry := e.docs.get(src.key(), count)
		var model *chromeModel
		if !o.IncludeChrome {
			m, err := e.chromeFor(ctx, p, doc, entry)
			if err != nil {
				return err
			}
			model = m
		}
		deadline := time.Now().Add(searchBudget)
		for n := from; n <= to; n++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			if time.Now().After(deadline) {
				res.Complete, res.ResumeFrom = false, n
				break
			}
			pi, err := e.readPage(p, doc, entry, n-1)
			if err != nil {
				return err
			}
			res.Searched++
			if !pi.hasText() {
				res.PagesWithoutText = append(res.PagesWithoutText, n)
				continue
			}
			text := pageText(n, pi, model, TextOptions{})
			for _, h := range searchPage(n, text.Text, phrase, words, 5, 110) {
				if len(res.Hits) >= maxHits*4 {
					res.Truncated = true
					break
				}
				res.Hits = append(res.Hits, h)
			}
		}
		return nil
	})
	if err != nil {
		return SearchResult{}, err
	}
	sortHits(res.Hits)
	if len(res.Hits) > maxHits {
		res.Hits, res.Truncated = res.Hits[:maxHits], true
	}
	return res, nil
}
