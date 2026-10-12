package agenthub

import (
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
	"unicode/utf8"

	"devkit/internal/pdfdoc"
)

// A PDF a person attaches is not turned into prompt text. A long document read
// into the prompt costs the whole of it on every turn whether or not the
// question needs it, and a scan has no text to turn into. So the prompt carries
// what the document is, and the agent reads what it needs through DevKit's PDF
// tools (internal/mcp/tools_pdf.go): search, text by page, the page as an image.
//
// For that the document has to outlive the file it came from, and the session
// that was given it has to be the only one that can read it. DocumentLibrary
// is that: a copy of the bytes in DevKit's data directory, named and described
// in a row of its own, kept as long as the session is.

var (
	// ErrDocumentNotFound: no such document in this session. The same answer for
	// a document that does not exist and one that belongs to another session —
	// a session must not be able to tell which.
	ErrDocumentNotFound = errors.New("agenthub.documentNotFound")
	// ErrDocumentsUnavailable: this build has no document library (server mode,
	// or a host that did not wire one).
	ErrDocumentsUnavailable = errors.New("agenthub.documentsUnavailable")
)

// Limits on what one session may hold. Each is a refusal with its reason, never
// a document quietly left out.
const (
	maxDocumentsPerSession = 20
	maxDocumentBytesTotal  = 500 << 20
	maxDocumentNameRunes   = 160
)

// Document is a PDF attached to a session.
type Document struct {
	// ID is the session's own handle for it ("doc1"): short enough for a model to
	// copy exactly, unique only within the session, never reused.
	ID        string    `json:"id"`
	SessionID string    `json:"sessionId"`
	Kind      string    `json:"kind"` // "pdf"
	Name      string    `json:"name"` // the file's own name, as attached
	MIME      string    `json:"mimeType"`
	Size      int64     `json:"size"`
	SHA256    string    `json:"sha256"`
	Pages     int       `json:"pages"`
	TextLayer string    `json:"textLayer"` // full, partial or none, as found when attached
	Chrome    string    `json:"chrome,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// DocumentStore is the persistence DocumentLibrary needs; SQLStore has it.
type DocumentStore interface {
	InsertDocument(ctx context.Context, d Document) error
	GetDocument(ctx context.Context, sessionID, id string) (Document, error)
	FindDocumentBySHA(ctx context.Context, sessionID, sha string) (Document, bool, error)
	ListDocuments(ctx context.Context, sessionID string) ([]Document, error)
	// DocumentUsage reports how many documents and bytes the session holds, and
	// the highest handle number ever given, so the next handle is never a reused one.
	DocumentUsage(ctx context.Context, sessionID string) (count int, bytes int64, ever int, err error)
	DocumentSessionIDs(ctx context.Context) ([]string, error)
}

// DocumentLibrary keeps attached PDFs.
type DocumentLibrary struct {
	store  DocumentStore
	dir    string
	engine *pdfdoc.Engine
	mu     sync.Mutex // serialises id allocation per library; imports are rare
}

// NewDocumentLibrary keeps documents under dir (created on demand, private to
// the user), reads them with engine, and records them in store.
func NewDocumentLibrary(store DocumentStore, dir string, engine *pdfdoc.Engine) *DocumentLibrary {
	return &DocumentLibrary{store: store, dir: dir, engine: engine}
}

// Engine is the reader the library's documents are opened with.
func (l *DocumentLibrary) Engine() *pdfdoc.Engine { return l.engine }

func (l *DocumentLibrary) file(sessionID, id string) string {
	return filepath.Join(l.dir, sessionID, id+".pdf")
}

// Import copies the PDF at path into the session's store, after checking that it
// can be read. The same file attached twice to a session is one document. A PDF
// that cannot be read — damaged, password-protected, too large — is refused
// with the reason, so the prompt that carried it is not sent as though it had
// been.
func (l *DocumentLibrary) Import(ctx context.Context, sessionID, path string) (Document, pdfdoc.Info, error) {
	name := displayName(filepath.Base(path))
	info, err := os.Stat(path)
	if err != nil {
		return Document{}, pdfdoc.Info{}, fmt.Errorf("%w: %s: %v", ErrInvalidPrompt, name, err)
	}
	if info.IsDir() {
		return Document{}, pdfdoc.Info{}, fmt.Errorf("%w: %s is a directory", ErrInvalidPrompt, name)
	}
	if info.Size() > pdfdoc.MaxFileBytes {
		return Document{}, pdfdoc.Info{}, fmt.Errorf("%w: %s is %d MiB, over the %d MiB limit for a PDF", ErrInvalidPrompt, name, info.Size()>>20, pdfdoc.MaxFileBytes>>20)
	}
	data, err := readAllLimited(path, pdfdoc.MaxFileBytes)
	if err != nil {
		return Document{}, pdfdoc.Info{}, fmt.Errorf("%w: %s: %v", ErrInvalidPrompt, name, err)
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])

	if existing, ok, err := l.store.FindDocumentBySHA(ctx, sessionID, hash); err != nil {
		return Document{}, pdfdoc.Info{}, err
	} else if ok {
		// Already held (same bytes): the same document, with the name it first
		// had. Its description is read again so the prompt can say it.
		pinfo, _ := l.engine.Inspect(ctx, pdfdoc.Source{Data: data, Key: hash})
		return existing, pinfo, nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	count, total, ever, err := l.store.DocumentUsage(ctx, sessionID)
	if err != nil {
		return Document{}, pdfdoc.Info{}, err
	}
	if count >= maxDocumentsPerSession {
		return Document{}, pdfdoc.Info{}, fmt.Errorf("%w: a session can hold %d PDFs and this one has %d; start another session for %s", ErrInvalidPrompt, maxDocumentsPerSession, count, name)
	}
	if total+int64(len(data)) > maxDocumentBytesTotal {
		return Document{}, pdfdoc.Info{}, fmt.Errorf("%w: attaching %s would take this session's PDFs past %d MiB", ErrInvalidPrompt, name, maxDocumentBytesTotal>>20)
	}

	pinfo, err := l.engine.Inspect(ctx, pdfdoc.Source{Data: data, Key: hash})
	if err != nil {
		return Document{}, pdfdoc.Info{}, fmt.Errorf("%w: %s: %s", ErrInvalidPrompt, name, documentProblem(err))
	}

	doc := Document{
		ID:        fmt.Sprintf("doc%d", ever+1),
		SessionID: sessionID,
		Kind:      "pdf",
		Name:      name,
		MIME:      "application/pdf",
		Size:      int64(len(data)),
		SHA256:    hash,
		Pages:     pinfo.Pages,
		TextLayer: pinfo.TextLayer.State,
		Chrome:    strings.Join(pinfo.Chrome, "; "),
		CreatedAt: time.Now().UTC(),
	}
	if err := writeFileAtomic(l.file(sessionID, doc.ID), data); err != nil {
		return Document{}, pdfdoc.Info{}, fmt.Errorf("could not keep a copy of %s: %w", name, err)
	}
	if err := l.store.InsertDocument(ctx, doc); err != nil {
		_ = os.Remove(l.file(sessionID, doc.ID))
		return Document{}, pdfdoc.Info{}, err
	}
	return doc, pinfo, nil
}

// Open returns the stored copy of a session's document, checked against the
// hash it was stored with. Another session's document is ErrDocumentNotFound.
func (l *DocumentLibrary) Open(ctx context.Context, sessionID, id string) (pdfdoc.Source, Document, error) {
	if l == nil {
		return pdfdoc.Source{}, Document{}, ErrDocumentsUnavailable
	}
	doc, err := l.store.GetDocument(ctx, sessionID, id)
	if err != nil {
		return pdfdoc.Source{}, Document{}, err
	}
	data, err := os.ReadFile(l.file(sessionID, id))
	if err != nil {
		return pdfdoc.Source{}, Document{}, fmt.Errorf("the stored copy of %s (%s) is missing: %w", doc.ID, doc.Name, err)
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != doc.SHA256 {
		return pdfdoc.Source{}, Document{}, fmt.Errorf("the stored copy of %s (%s) no longer matches what was attached", doc.ID, doc.Name)
	}
	return pdfdoc.Source{Data: data, Key: doc.SHA256}, doc, nil
}

// List is the session's documents, oldest first.
func (l *DocumentLibrary) List(ctx context.Context, sessionID string) ([]Document, error) {
	if l == nil {
		return nil, nil
	}
	return l.store.ListDocuments(ctx, sessionID)
}

// Remove deletes the stored copies of the given sessions. Called after their
// rows are gone; failing to remove a file is reported but never undoes the
// delete — Sweep collects what is left.
func (l *DocumentLibrary) Remove(sessionIDs []string) error {
	if l == nil {
		return nil
	}
	var first error
	for _, id := range sessionIDs {
		if id == "" || strings.ContainsAny(id, `/\`) || id == "." || id == ".." {
			continue
		}
		if err := os.RemoveAll(filepath.Join(l.dir, id)); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Sweep removes what no row accounts for: a session's folder whose session is
// gone, and a file whose row was never written (a crash between the two). Run
// at start-up.
func (l *DocumentLibrary) Sweep(ctx context.Context, sessionExists func(string) (bool, error)) (removed int, err error) {
	if l == nil {
		return 0, nil
	}
	entries, err := os.ReadDir(l.dir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sessionID := e.Name()
		ok, err := sessionExists(sessionID)
		if err != nil {
			return removed, err
		}
		if !ok {
			if os.RemoveAll(filepath.Join(l.dir, sessionID)) == nil {
				removed++
			}
			continue
		}
		docs, err := l.store.ListDocuments(ctx, sessionID)
		if err != nil {
			return removed, err
		}
		known := map[string]bool{}
		for _, d := range docs {
			known[d.ID+".pdf"] = true
		}
		files, _ := os.ReadDir(filepath.Join(l.dir, sessionID))
		for _, f := range files {
			if !known[f.Name()] && os.Remove(filepath.Join(l.dir, sessionID, f.Name())) == nil {
				removed++
			}
		}
	}
	return removed, nil
}

// documentProblem words an engine error for the person attaching the file.
func documentProblem(err error) string {
	switch {
	case errors.Is(err, pdfdoc.ErrPasswordRequired):
		return "it is password-protected, which DevKit cannot open. Save an unprotected copy and attach that"
	case errors.Is(err, pdfdoc.ErrCorrupt):
		return "the PDF is damaged and cannot be read"
	case errors.Is(err, pdfdoc.ErrNotPDF):
		return "it is not a PDF file"
	case errors.Is(err, pdfdoc.ErrTooManyPages):
		return strings.TrimPrefix(err.Error(), "pdfdoc: ")
	case errors.Is(err, pdfdoc.ErrTooLarge):
		return "it is too large"
	case errors.Is(err, pdfdoc.ErrUnavailable):
		return "DevKit's PDF reader could not start on this machine (" + err.Error() + ")"
	default:
		return err.Error()
	}
}

func displayName(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	if utf8.RuneCountInString(name) > maxDocumentNameRunes {
		name = string([]rune(name)[:maxDocumentNameRunes]) + "…"
	}
	return name
}

func readAllLimited(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("the file is larger than %d MiB", limit>>20)
	}
	return data, nil
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".import-*")
	if err != nil {
		return err
	}
	cleanup := func() { _ = tmp.Close(); _ = os.Remove(tmp.Name()) }
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
