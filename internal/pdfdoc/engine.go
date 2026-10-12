// Package pdfdoc reads PDF documents for Agent Hub: metadata, text by page,
// search, and page images. It is a leaf — nothing in DevKit is imported — so the
// attachment store and the MCP tools can both stand on it.
//
// The engine is PDFium (the library Chrome renders PDFs with) compiled to
// WebAssembly and run under wazero: pure Go, no cgo, no installed Poppler or
// MuPDF, one binary on every platform DevKit ships for. It also runs sandboxed —
// the module is given no file system and no network, and gets the document as
// bytes — so a hostile PDF that finds a PDFium bug is still inside a box.
package pdfdoc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	pdfium "github.com/klippa-app/go-pdfium"
	pdfiumerrors "github.com/klippa-app/go-pdfium/errors"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
)

// What the engine refuses, by name — the caller words each for the person or
// the agent. Nothing here ever turns into an empty result: a document that
// cannot be read says so.
var (
	// ErrNotPDF: the bytes are not a PDF at all (no %PDF header).
	ErrNotPDF = errors.New("pdfdoc: not a PDF file")
	// ErrCorrupt: it starts like a PDF but cannot be parsed.
	ErrCorrupt = errors.New("pdfdoc: the PDF is damaged and cannot be read")
	// ErrPasswordRequired: it is encrypted and needs a password DevKit does not have.
	ErrPasswordRequired = errors.New("pdfdoc: the PDF is password-protected")
	// ErrTooLarge: over MaxFileBytes.
	ErrTooLarge = errors.New("pdfdoc: the PDF is too large")
	// ErrTooManyPages: over MaxPages.
	ErrTooManyPages = errors.New("pdfdoc: the PDF has too many pages")
	// ErrPageRange: a page number outside the document.
	ErrPageRange = errors.New("pdfdoc: page out of range")
	// ErrUnavailable: the PDF engine could not start on this machine.
	ErrUnavailable = errors.New("pdfdoc: the PDF engine is not available")
)

// Limits. Each is checked before work starts and reported when hit, never
// applied by quietly doing less.
const (
	MaxFileBytes = 100 << 20 // one document
	MaxPages     = 3000      // pages in one document
	// MaxReadPages is how many pages one text call may cover; MaxRenderPages how
	// many page images one call may return.
	MaxReadPages   = 25
	MaxRenderPages = 4
	// Search covers every page; this bounds how long one call may spend on it.
	searchBudget = 25 * time.Second
	// How many pages Info looks at for a text layer before it samples.
	infoFullScanPages = 400
	infoSamplePages   = 120
)

// Source is a document to read: its bytes, how to identify it for caching, and
// a password if one is known.
type Source struct {
	Data     []byte
	Key      string // identifies the content; Hash(Data) when empty
	Password string
}

// Hash is the content hash a document is known by.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s Source) key() string {
	if s.Key != "" {
		return s.Key
	}
	return Hash(s.Data)
}

// Options configure an Engine.
type Options struct {
	// CacheDir keeps the compiled form of the PDFium module between runs, so the
	// seconds spent compiling it happen once per install rather than per launch.
	// Empty compiles in memory each run.
	CacheDir string
	// Concurrency bounds simultaneous operations; each holds its own copy of the
	// module's memory. Default 2.
	Concurrency int
	// Discard silences PDFium's own diagnostics (default: they go nowhere).
	Stderr io.Writer
}

// Engine is a process-wide PDF reader. The zero value is not usable; use New.
type Engine struct {
	opts Options

	initOnce sync.Once
	pool     pdfium.Pool
	initErr  error
	cache    wazero.CompilationCache

	sem chan struct{}

	docs    *docCache
	renders *renderCache
}

// New creates an Engine. Nothing starts until the first use (or Warm).
func New(opts Options) *Engine {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 2
	}
	return &Engine{
		opts:    opts,
		sem:     make(chan struct{}, opts.Concurrency),
		docs:    newDocCache(6),
		renders: newRenderCache(48 << 20),
	}
}

// Warm starts the engine in the background so the first real call does not pay
// for it. Safe to call any number of times.
func (e *Engine) Warm() { go func() { _, _ = e.instance() }() }

// Close releases the engine. Calls after Close fail with ErrUnavailable.
func (e *Engine) Close() {
	e.initOnce.Do(func() { e.initErr = ErrUnavailable })
	if e.pool != nil {
		_ = e.pool.Close()
	}
	if e.cache != nil {
		_ = e.cache.Close(context.Background())
	}
}

func (e *Engine) start() {
	cfg := wazero.NewRuntimeConfig().WithCoreFeatures(api.CoreFeaturesV2 | experimental.CoreFeaturesExceptionHandling)
	if e.opts.CacheDir != "" {
		if err := os.MkdirAll(e.opts.CacheDir, 0o700); err == nil {
			if cache, err := wazero.NewCompilationCacheWithDir(filepath.Clean(e.opts.CacheDir)); err == nil {
				e.cache = cache
				cfg = cfg.WithCompilationCache(cache)
			}
		}
	}
	stderr := e.opts.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	pool, err := webassembly.Init(webassembly.Config{
		MinIdle:       1,
		MaxIdle:       e.opts.Concurrency,
		MaxTotal:      e.opts.Concurrency,
		RuntimeConfig: cfg,
		// No mounts: the default would give the module the whole host file system.
		FSConfig: wazero.NewFSConfig(),
		Stdout:   io.Discard,
		Stderr:   stderr,
	})
	if err != nil {
		e.initErr = fmt.Errorf("%w: %v", ErrUnavailable, err)
		return
	}
	e.pool = pool
}

func (e *Engine) instance() (pdfium.Pdfium, error) {
	e.initOnce.Do(e.start)
	if e.initErr != nil {
		return nil, e.initErr
	}
	inst, err := e.pool.GetInstance(60 * time.Second)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return inst, nil
}

func (e *Engine) acquire(ctx context.Context) (release func(), err error) {
	select {
	case e.sem <- struct{}{}:
		return func() { <-e.sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// open validates src and opens it, then calls fn with the document. The engine
// instance and the document are released afterwards, whatever fn does.
func (e *Engine) open(ctx context.Context, src Source, fn func(p pdfium.Pdfium, doc references.FPDF_DOCUMENT, pages int) error) error {
	if err := checkBytes(src.Data); err != nil {
		return err
	}
	release, err := e.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return err
	}
	inst, err := e.instance()
	if err != nil {
		return err
	}
	defer inst.Close()

	data := src.Data
	req := &requests.OpenDocument{File: &data}
	if src.Password != "" {
		pw := src.Password
		req.Password = &pw
	}
	doc, err := inst.OpenDocument(req)
	if err != nil {
		return openError(err)
	}
	defer inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})

	count, err := inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if count.PageCount < 1 {
		return fmt.Errorf("%w: it has no pages", ErrCorrupt)
	}
	if count.PageCount > MaxPages {
		return fmt.Errorf("%w (%d pages, limit %d)", ErrTooManyPages, count.PageCount, MaxPages)
	}
	return fn(inst, doc.Document, count.PageCount)
}

func checkBytes(data []byte) error {
	if int64(len(data)) > MaxFileBytes {
		return fmt.Errorf("%w (%d MiB, limit %d MiB)", ErrTooLarge, len(data)>>20, MaxFileBytes>>20)
	}
	head := data
	if len(head) > 1024 {
		head = head[:1024]
	}
	if !bytes.Contains(head, []byte("%PDF-")) {
		return ErrNotPDF
	}
	return nil
}

func openError(err error) error {
	switch {
	case errors.Is(err, pdfiumerrors.ErrPassword):
		return ErrPasswordRequired
	case errors.Is(err, pdfiumerrors.ErrSecurity):
		return fmt.Errorf("%w: it uses an encryption DevKit cannot open", ErrPasswordRequired)
	case errors.Is(err, pdfiumerrors.ErrFormat), errors.Is(err, pdfiumerrors.ErrFile):
		return ErrCorrupt
	default:
		if strings.Contains(strings.ToLower(err.Error()), "password") {
			return ErrPasswordRequired
		}
		return fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
}

// pageRef is how the engine names a page: by 0-based index in a document.
func pageRef(doc references.FPDF_DOCUMENT, index int) requests.Page {
	return requests.Page{ByIndex: &requests.PageByIndex{Document: doc, Index: index}}
}

// checkPages validates 1-based page numbers against the page count.
func checkPages(pages []int, count int) error {
	for _, p := range pages {
		if p < 1 || p > count {
			return fmt.Errorf("%w: page %d of a %d-page document", ErrPageRange, p, count)
		}
	}
	return nil
}
