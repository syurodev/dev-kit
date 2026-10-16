package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"devkit/internal/codegraph"
	"devkit/internal/pdfdoc"
)

// PDFView is what the Files viewer asks for to show a PDF that lives in a
// workspace folder: how many pages and how big, one page as an image, and a
// search. The frontend names the file as (workspace, folder, relative path) —
// Go resolves the folder, keeps the path inside it and reads the bytes, so the
// viewer never holds a path and never runs anything from the file: pages come
// back as pictures drawn by internal/pdfdoc (PDFium under wazero, no file
// system and no network).
//
// A viewer scrolls through hundreds of pages, one call each, so the bytes of a
// recently shown document are kept (a few documents, bounded in total) and
// checked against the file's size and mtime before every reuse.
type PDFView struct {
	code   *WorkspaceCode
	engine *pdfdoc.Engine

	mu      sync.Mutex
	entries map[string]*pdfViewEntry
	bytes   int64
	clock   func() time.Time
}

type pdfViewEntry struct {
	src      pdfdoc.Source
	sig      codegraph.FileSignature
	revision string
	used     time.Time
}

const (
	pdfViewMaxEntries = 4
	pdfViewMaxBytes   = 256 << 20
	// A page is drawn at the zoom the viewer shows it; these bound what a
	// caller can ask for (pdfdoc.MinDPI..MaxDPI is the engine's own range).
	pdfViewDefaultDPI = 110
)

// ErrPDFChanged: the file on disk is not the revision the viewer opened. The
// answer is to open it again, not to retry the call.
var ErrPDFChanged = errors.New("app: the PDF changed on disk")

// ErrPDFUnavailable: no PDF engine was wired (a container without one).
var ErrPDFUnavailable = errors.New("app: the PDF viewer is not available")

func NewPDFView(code *WorkspaceCode, engine *pdfdoc.Engine) *PDFView {
	return &PDFView{code: code, engine: engine, entries: map[string]*pdfViewEntry{}, clock: time.Now}
}

// PDFInfo is a document as the viewer lays it out.
type PDFInfo struct {
	// Revision is the content hash of the bytes shown; later calls name it so a
	// file replaced meanwhile is noticed instead of mixing two documents.
	Revision string `json:"revision"`
	Pages    int    `json:"pages"`
	// PageSizes is width and height in points (1/72 in) per page, rotation applied.
	PageSizes  [][2]float64 `json:"pageSizes"`
	Size       int64        `json:"size"`
	ModifiedAt int64        `json:"modifiedAt"`
}

// PDFPage is one drawn page.
type PDFPage struct {
	Page   int    `json:"page"`
	MIME   string `json:"mimeType"`
	Data   []byte `json:"data"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// PDFHit is one place a search found.
type PDFHit struct {
	Page    int    `json:"page"`
	Snippet string `json:"snippet"`
}

// PDFSearch is the answer to a search, including what it could not cover.
type PDFSearch struct {
	Hits             []PDFHit `json:"hits"`
	Truncated        bool     `json:"truncated"`
	Complete         bool     `json:"complete"`
	PagesWithoutText []int    `json:"pagesWithoutText"`
}

// Info opens a document and reports its pages. It always reads the file's
// current state: this is the call that starts (or restarts) a viewer.
func (v *PDFView) Info(ctx context.Context, workspaceID, folderID, rel string) (PDFInfo, error) {
	if v == nil || v.engine == nil {
		return PDFInfo{}, ErrPDFUnavailable
	}
	entry, err := v.load(workspaceID, folderID, rel, "")
	if err != nil {
		return PDFInfo{}, err
	}
	sizes, err := v.engine.PageSizes(ctx, entry.src)
	if err != nil {
		return PDFInfo{}, err
	}
	return PDFInfo{Revision: entry.revision, Pages: len(sizes), PageSizes: sizes, Size: entry.sig.Size, ModifiedAt: entry.sig.ModifiedAt}, nil
}

// Page draws one page (1-based) at dpi (0 = a default; the engine bounds it).
func (v *PDFView) Page(ctx context.Context, workspaceID, folderID, rel, revision string, page, dpi int) (PDFPage, error) {
	if v == nil || v.engine == nil {
		return PDFPage{}, ErrPDFUnavailable
	}
	entry, err := v.load(workspaceID, folderID, rel, revision)
	if err != nil {
		return PDFPage{}, err
	}
	if dpi == 0 {
		dpi = pdfViewDefaultDPI
	}
	images, err := v.engine.Render(ctx, entry.src, []int{page}, pdfdoc.RenderOptions{DPI: dpi, MaxEdge: pdfdoc.MaxMaxEdge})
	if err != nil {
		return PDFPage{}, err
	}
	img := images[0]
	return PDFPage{Page: img.Page, MIME: img.MIME, Data: img.Data, Width: img.Width, Height: img.Height}, nil
}

// Search finds a phrase (or, failing that, its words) on each page. Hits are
// pages with a snippet; the viewer jumps to the page and does not mark the text.
func (v *PDFView) Search(ctx context.Context, workspaceID, folderID, rel, revision, query string) (PDFSearch, error) {
	if v == nil || v.engine == nil {
		return PDFSearch{}, ErrPDFUnavailable
	}
	entry, err := v.load(workspaceID, folderID, rel, revision)
	if err != nil {
		return PDFSearch{}, err
	}
	res, err := v.engine.Search(ctx, entry.src, pdfdoc.SearchOptions{Query: query, MaxHits: 50})
	if err != nil {
		return PDFSearch{}, err
	}
	out := PDFSearch{Hits: make([]PDFHit, 0, len(res.Hits)), Truncated: res.Truncated, Complete: res.Complete, PagesWithoutText: res.PagesWithoutText}
	for _, h := range res.Hits {
		out.Hits = append(out.Hits, PDFHit{Page: h.Page, Snippet: h.Snippet})
	}
	if out.PagesWithoutText == nil {
		out.PagesWithoutText = []int{}
	}
	return out, nil
}

// load returns the document's bytes, from the cache when the file is as it was
// and from disk otherwise. A non-empty revision must match what is loaded.
func (v *PDFView) load(workspaceID, folderID, rel, revision string) (*pdfViewEntry, error) {
	folder, err := v.code.Folder(workspaceID, folderID)
	if err != nil {
		return nil, err
	}
	key := folder.Path + "\x00" + rel
	sig, err := codegraph.SignatureAt(folder.Path, rel)
	if err != nil {
		return nil, err
	}

	v.mu.Lock()
	if e := v.entries[key]; e != nil && e.sig == sig && (revision == "" || e.revision == revision) {
		e.used = v.clock()
		v.mu.Unlock()
		return e, nil
	}
	v.mu.Unlock()

	file, err := codegraph.ReadBinaryAt(folder.Path, rel, pdfdoc.MaxFileBytes)
	if errors.Is(err, codegraph.ErrFileTooLarge) {
		return nil, pdfdoc.ErrTooLarge
	}
	if err != nil {
		return nil, err
	}
	hash := pdfdoc.Hash(file.Data)
	// A viewer that names a revision wants that document, not whatever is there now.
	if revision != "" && hash != revision {
		return nil, ErrPDFChanged
	}
	entry := &pdfViewEntry{src: pdfdoc.Source{Data: file.Data, Key: hash}, sig: file.Signature, revision: hash, used: v.clock()}

	v.mu.Lock()
	defer v.mu.Unlock()
	if old := v.entries[key]; old != nil {
		v.bytes -= int64(len(old.src.Data))
	}
	v.entries[key] = entry
	v.bytes += int64(len(file.Data))
	v.evictLocked(key)
	return entry, nil
}

// evictLocked drops the least recently used documents until the cache is within
// its bounds, never the one just loaded.
func (v *PDFView) evictLocked(keep string) {
	for len(v.entries) > pdfViewMaxEntries || v.bytes > pdfViewMaxBytes {
		oldest, oldestKey := time.Time{}, ""
		for k, e := range v.entries {
			if k == keep {
				continue
			}
			if oldestKey == "" || e.used.Before(oldest) {
				oldest, oldestKey = e.used, k
			}
		}
		if oldestKey == "" {
			return
		}
		v.bytes -= int64(len(v.entries[oldestKey].src.Data))
		delete(v.entries, oldestKey)
	}
}
