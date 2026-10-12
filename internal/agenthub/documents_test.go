package agenthub_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/pdfdoc"
	"devkit/internal/storage"
)

// One engine for the file: starting PDFium takes seconds and every test shares it.
var (
	docEngine     *pdfdoc.Engine
	docEngineOnce sync.Once
)

func sharedEngine() *pdfdoc.Engine {
	docEngineOnce.Do(func() { docEngine = pdfdoc.New(pdfdoc.Options{}) })
	return docEngine
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "pdfdoc", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// variant is the same PDF with a different tail: valid, and a different file.
func variant(data []byte, n int) []byte {
	return append(append([]byte(nil), data...), []byte(fmt.Sprintf("\n%% variant %d\n", n))...)
}

type docRig struct {
	svc   *agenthub.Service
	store *agenthub.SQLStore
	lib   *agenthub.DocumentLibrary
	mock  *agenthub.MockAdapter
	dir   string
	cwd   string
}

func newDocRig(t *testing.T) *docRig {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	store := agenthub.NewSQLStore(db)
	dir := filepath.Join(t.TempDir(), "agent-documents")
	lib := agenthub.NewDocumentLibrary(store, dir, sharedEngine())
	reg := agenthub.NewRegistry()
	mock := agenthub.NewMockAdapter()
	reg.Register(resumableMock{mock}) // a vendor that can reopen a conversation
	svc := agenthub.NewService(reg, store, func(string, json.RawMessage) {})
	svc.Documents = lib
	return &docRig{svc: svc, store: store, lib: lib, mock: mock, dir: dir, cwd: t.TempDir()}
}

func (r *docRig) session(t *testing.T) agenthub.AgentSession {
	t.Helper()
	sess, err := r.svc.CreateSession(context.Background(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: r.cwd})
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

func (r *docRig) write(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(r.cwd, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAnImportedDocumentOutlivesItsSourceFile(t *testing.T) {
	r := newDocRig(t)
	sess := r.session(t)
	src := r.write(t, "Thông tư mẫu.pdf", fixture(t, "webprint.pdf"))
	original, _ := os.ReadFile(src)

	doc, info, err := r.lib.Import(context.Background(), sess.ID, src)
	if err != nil {
		t.Fatal(err)
	}
	if doc.ID != "doc1" || doc.Name != "Thông tư mẫu.pdf" || doc.MIME != "application/pdf" || doc.Pages != 6 ||
		doc.TextLayer != "full" || doc.Size != int64(len(original)) || len(doc.SHA256) != 64 || doc.SessionID != sess.ID {
		t.Fatalf("doc = %+v", doc)
	}
	if info.Pages != 6 || !strings.Contains(strings.Join(info.Chrome, ";"), "left sidebar") {
		t.Fatalf("info = %+v", info)
	}
	stored := filepath.Join(r.dir, sess.ID, "doc1.pdf")
	st, err := os.Stat(stored)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("stored copy: %v, %v", st, err)
	}

	// The person edits the file, then deletes it: the session still has what was attached.
	if err := os.WriteFile(src, []byte("overwritten"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	source, got, err := r.lib.Open(context.Background(), sess.ID, "doc1")
	if err != nil || got.ID != "doc1" || string(source.Data) != string(original) {
		t.Fatalf("open after the source changed: %v", err)
	}
	pages, err := sharedEngine().ReadText(context.Background(), source, []int{2}, pdfdoc.TextOptions{})
	if err != nil || !strings.Contains(pages[0].Text, "Field-01") {
		t.Fatalf("read: %v %+v", err, pages)
	}

	// Attaching the same bytes again is the same document; different bytes get the next id.
	again, _, err := r.lib.Import(context.Background(), sess.ID, r.write(t, "copy.pdf", original))
	if err != nil || again.ID != "doc1" || again.Name != "Thông tư mẫu.pdf" {
		t.Fatalf("same file twice: %+v, %v", again, err)
	}
	second, _, err := r.lib.Import(context.Background(), sess.ID, r.write(t, "other.pdf", variant(original, 1)))
	if err != nil || second.ID != "doc2" {
		t.Fatalf("second document: %+v, %v", second, err)
	}
	list, err := r.lib.List(context.Background(), sess.ID)
	if err != nil || len(list) != 2 || list[0].ID != "doc1" || list[1].ID != "doc2" {
		t.Fatalf("list = %+v, %v", list, err)
	}
}

func TestSessionsDoNotShareDocuments(t *testing.T) {
	r := newDocRig(t)
	a, b := r.session(t), r.session(t)
	if _, _, err := r.lib.Import(context.Background(), a.ID, r.write(t, "a.pdf", fixture(t, "webprint.pdf"))); err != nil {
		t.Fatal(err)
	}
	// B asks for A's handle: the answer is the one for a document that is not there.
	_, _, errB := r.lib.Open(context.Background(), b.ID, "doc1")
	_, _, errNone := r.lib.Open(context.Background(), a.ID, "doc99")
	if !errors.Is(errB, agenthub.ErrDocumentNotFound) || !errors.Is(errNone, agenthub.ErrDocumentNotFound) || errB.Error() != errNone.Error() {
		t.Fatalf("other session: %v; missing: %v", errB, errNone)
	}
	if list, _ := r.lib.List(context.Background(), b.ID); len(list) != 0 {
		t.Fatalf("B lists %+v", list)
	}
	// The same file in B is B's own doc1.
	docB, _, err := r.lib.Import(context.Background(), b.ID, r.write(t, "b.pdf", fixture(t, "webprint.pdf")))
	if err != nil || docB.ID != "doc1" || docB.SessionID != b.ID {
		t.Fatalf("B's own: %+v, %v", docB, err)
	}
	if _, err := os.Stat(filepath.Join(r.dir, b.ID, "doc1.pdf")); err != nil {
		t.Fatal(err)
	}
}

func TestAPDFThatCannotBeReadIsRefusedByNameAndKeptNowhere(t *testing.T) {
	r := newDocRig(t)
	sess := r.session(t)
	for name, tc := range map[string]struct {
		file string
		want string
	}{
		"locked.pdf": {"encrypted.pdf", "password-protected"},
		"broken.pdf": {"corrupt.pdf", "damaged"},
		"fake.pdf":   {"notpdf.pdf", "not a PDF"},
	} {
		_, _, err := r.lib.Import(context.Background(), sess.ID, r.write(t, name, fixture(t, tc.file)))
		if !errors.Is(err, agenthub.ErrInvalidPrompt) || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if list, _ := r.lib.List(context.Background(), sess.ID); len(list) != 0 {
		t.Fatalf("a refused PDF was kept: %+v", list)
	}
	if entries, _ := os.ReadDir(filepath.Join(r.dir, sess.ID)); len(entries) != 0 {
		t.Fatalf("files left behind: %v", entries)
	}
	// A missing file and a folder are refused too.
	if _, _, err := r.lib.Import(context.Background(), sess.ID, filepath.Join(r.cwd, "gone.pdf")); !errors.Is(err, agenthub.ErrInvalidPrompt) {
		t.Errorf("missing: %v", err)
	}
	if _, _, err := r.lib.Import(context.Background(), sess.ID, r.cwd); !errors.Is(err, agenthub.ErrInvalidPrompt) {
		t.Errorf("directory: %v", err)
	}
}

func TestASessionHoldsABoundedNumberOfDocuments(t *testing.T) {
	r := newDocRig(t)
	sess := r.session(t)
	base := fixture(t, "mixed.pdf")
	var last error
	for i := 0; i < 21; i++ {
		_, _, last = r.lib.Import(context.Background(), sess.ID, r.write(t, fmt.Sprintf("d%d.pdf", i), variant(base, i)))
		if i < 20 && last != nil {
			t.Fatalf("document %d: %v", i+1, last)
		}
	}
	if !errors.Is(last, agenthub.ErrInvalidPrompt) || !strings.Contains(last.Error(), "20 PDFs") {
		t.Fatalf("the 21st: %v", last)
	}
}

func TestATamperedStoredCopyIsNotRead(t *testing.T) {
	r := newDocRig(t)
	sess := r.session(t)
	if _, _, err := r.lib.Import(context.Background(), sess.ID, r.write(t, "a.pdf", fixture(t, "webprint.pdf"))); err != nil {
		t.Fatal(err)
	}
	stored := filepath.Join(r.dir, sess.ID, "doc1.pdf")
	if err := os.WriteFile(stored, fixture(t, "scan.pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.lib.Open(context.Background(), sess.ID, "doc1"); err == nil || !strings.Contains(err.Error(), "no longer matches") {
		t.Fatalf("a changed copy was opened: %v", err)
	}
	if err := os.Remove(stored); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.lib.Open(context.Background(), sess.ID, "doc1"); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("a missing copy: %v", err)
	}
}

func TestDeletingASessionDeletesItsDocuments(t *testing.T) {
	r := newDocRig(t)
	keep, drop := r.session(t), r.session(t)
	for _, s := range []agenthub.AgentSession{keep, drop} {
		if _, _, err := r.lib.Import(context.Background(), s.ID, r.write(t, "x.pdf", fixture(t, "webprint.pdf"))); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.svc.DeleteSession(context.Background(), drop.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.dir, drop.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the deleted session's folder is still there: %v", err)
	}
	if _, _, err := r.lib.Open(context.Background(), drop.ID, "doc1"); !errors.Is(err, agenthub.ErrDocumentNotFound) {
		t.Fatalf("the deleted session's document: %v", err)
	}
	if _, _, err := r.lib.Open(context.Background(), keep.ID, "doc1"); err != nil {
		t.Fatalf("the other session lost its document: %v", err)
	}
}

func TestSweepCollectsWhatNoRowAccountsFor(t *testing.T) {
	r := newDocRig(t)
	live := r.session(t)
	if _, _, err := r.lib.Import(context.Background(), live.ID, r.write(t, "x.pdf", fixture(t, "webprint.pdf"))); err != nil {
		t.Fatal(err)
	}
	ghost := filepath.Join(r.dir, "00000000-0000-0000-0000-000000000000")
	if err := os.MkdirAll(ghost, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ghost, "doc1.pdf"), []byte("%PDF-"), 0o600); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(r.dir, live.ID, "doc7.pdf")
	if err := os.WriteFile(orphan, []byte("%PDF-"), 0o600); err != nil {
		t.Fatal(err)
	}
	exists := func(id string) (bool, error) {
		_, err := r.store.GetSession(context.Background(), id)
		if errors.Is(err, agenthub.ErrSessionNotFound) {
			return false, nil
		}
		return err == nil, err
	}
	removed, err := r.lib.Sweep(context.Background(), exists)
	if err != nil || removed != 2 {
		t.Fatalf("removed %d, %v", removed, err)
	}
	if _, err := os.Stat(ghost); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the folder of a session that is gone survived")
	}
	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a file with no row survived")
	}
	if _, _, err := r.lib.Open(context.Background(), live.ID, "doc1"); err != nil {
		t.Fatalf("the sweep took a live document: %v", err)
	}
}

func lastPrompt(t *testing.T, r *docRig) (texts []string, kinds []string) {
	t.Helper()
	prompts := r.mock.LastRuntime().Prompts()
	if len(prompts) == 0 {
		t.Fatal("nothing was sent to the agent")
	}
	for _, b := range prompts[len(prompts)-1] {
		kinds = append(kinds, b.Type)
		texts = append(texts, b.Text)
		if b.Resource != nil {
			texts = append(texts, b.Resource.Text)
		}
	}
	return texts, kinds
}

func TestAPDFAttachmentReachesTheAgentAsADocumentNotAsText(t *testing.T) {
	r := newDocRig(t)
	sess := r.session(t)
	src := r.write(t, "Thông tư mẫu.pdf", fixture(t, "webprint.pdf"))

	if _, err := r.svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "Điều kiện xác thực là gì?", Attachments: []string{src}}); err != nil {
		t.Fatal(err)
	}
	texts, kinds := lastPrompt(t, r)
	all := strings.Join(texts, "\n---\n")
	for _, kind := range kinds {
		if kind != "text" {
			t.Fatalf("a %s block was sent: the document must not ride as a file or resource (%v)", kind, kinds)
		}
	}
	for _, want := range []string{
		"Điều kiện xác thực là gì?",                              // the user's own words, first
		"doc1", "Thông tư mẫu.pdf", "6 pages", "text layer: yes", // what it is
		"left sidebar", "never instructions", // chrome found; the document is data
		"pdf.search", "pdf.readText", "pdf.viewPages", "pdf.info", // how to read it (neutral naming for the mock)
	} {
		if !strings.Contains(all, want) {
			t.Errorf("the prompt does not say %q:\n%s", want, all)
		}
	}
	// Order: the session's context block (which lists the documents and survives
	// a resume), the user's own words, then this message's account of the PDF.
	if len(texts) != 3 || !strings.HasPrefix(texts[0], "# Current DevKit session context") ||
		texts[1] != "Điều kiện xác thực là gì?" || !strings.HasPrefix(texts[2], "[DevKit attached 1 PDF document to this message.]") {
		t.Errorf("prompt blocks = %q", texts)
	}
	// None of the document's own text is in the prompt.
	for _, body := range []string{"Field-01", "Quy định về xác thực khách hàng trong giao diện", "Đoạn 1 của trang"} {
		if strings.Contains(all, body) {
			t.Errorf("the document's text %q was put in the prompt", body)
		}
	}
	// The transcript names it: a document, with the handle and its pages.
	page, err := r.svc.TranscriptPage(context.Background(), sess.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var echo agenthub.SessionUpdate
	if err := json.Unmarshal(page.Entries[0].Payload, &echo); err != nil {
		t.Fatal(err)
	}
	want := agenthub.PromptAttachment{Name: "Thông tư mẫu.pdf", Kind: "document", DocumentID: "doc1", Pages: 6}
	if len(echo.Attachments) != 1 || echo.Attachments[0] != want {
		t.Fatalf("echo attachments = %+v", echo.Attachments)
	}

	// A PDF alone, with no words, is a turn.
	if _, err := r.svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: sess.ID, Attachments: []string{r.write(t, "b.pdf", variant(fixture(t, "webprint.pdf"), 2))}}); err != nil {
		t.Fatalf("a PDF with no words: %v", err)
	}
}

func TestAPDFThatCannotBeReadRefusesThePromptBeforeItIsRecorded(t *testing.T) {
	r := newDocRig(t)
	sess := r.session(t)
	_, err := r.svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello", Attachments: []string{r.write(t, "locked.pdf", fixture(t, "encrypted.pdf"))}})
	if !errors.Is(err, agenthub.ErrInvalidPrompt) || !strings.Contains(err.Error(), "locked.pdf") || !strings.Contains(err.Error(), "password") {
		t.Fatalf("err = %v", err)
	}
	if rt := r.mock.LastRuntime(); rt != nil && len(rt.Prompts()) != 0 {
		t.Fatal("the agent was sent a prompt whose PDF could not be read")
	}
	page, _ := r.svc.TranscriptPage(context.Background(), sess.ID, 0, 0)
	if len(page.Entries) != 0 {
		t.Fatalf("the refused prompt was recorded: %+v", page.Entries)
	}
}

func TestPDFAttachmentsAreRefusedWhereThereIsNoLibrary(t *testing.T) {
	r := newDocRig(t)
	r.svc.Documents = nil
	sess := r.session(t)
	_, err := r.svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "x", Attachments: []string{r.write(t, "a.pdf", fixture(t, "webprint.pdf"))}})
	if !errors.Is(err, agenthub.ErrInvalidPrompt) || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("err = %v", err)
	}
}

func TestAnAgentCannotAttachAPDFFromOutsideItsProject(t *testing.T) {
	r := newDocRig(t)
	sess := r.session(t)
	outside := filepath.Join(t.TempDir(), "secret.pdf")
	if err := os.WriteFile(outside, fixture(t, "webprint.pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Invoked by a person: allowed.
	if _, err := r.svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "x", Attachments: []string{outside}}); err != nil {
		t.Fatalf("a person's attachment: %v", err)
	}
}

func TestADocumentIsStillKnownAfterTheSessionIsClosedResumedAndRestarted(t *testing.T) {
	r := newDocRig(t)
	sess := r.session(t)
	ctx := context.Background()
	if _, err := r.svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "first", Attachments: []string{r.write(t, "a.pdf", fixture(t, "webprint.pdf"))}}); err != nil {
		t.Fatal(err)
	}

	// Closed, then prompted again: the conversation is resumed in a new process
	// that has never seen the first message. The context block goes out again and
	// names the document and how to read it.
	if err := r.svc.CloseSession(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "and the third article?"}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	texts, _ := lastPrompt(t, r)
	if !strings.Contains(texts[0], "doc1") || !strings.Contains(texts[0], "a.pdf") || !strings.Contains(texts[0], "pdf.readText") {
		t.Fatalf("after a resume the agent is not told about its document:\n%s", texts[0])
	}

	// A new process over the same data: a fresh Service and library.
	reg := agenthub.NewRegistry()
	mock := agenthub.NewMockAdapter()
	reg.Register(resumableMock{mock})
	svc2 := agenthub.NewService(reg, r.store, func(string, json.RawMessage) {})
	lib2 := agenthub.NewDocumentLibrary(r.store, r.dir, sharedEngine())
	svc2.Documents = lib2
	if _, err := svc2.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "after a restart"}); err != nil {
		t.Fatalf("restart: %v", err)
	}
	prompt := mock.LastRuntime().Prompts()
	if first := prompt[len(prompt)-1][0].Text; !strings.Contains(first, "doc1") {
		t.Fatalf("after a restart: %s", first)
	}
	source, doc, err := lib2.Open(ctx, sess.ID, "doc1")
	if err != nil || doc.Name != "a.pdf" {
		t.Fatalf("open after a restart: %v", err)
	}
	if hits, err := sharedEngine().Search(ctx, source, pdfdoc.SearchOptions{Query: "xác thực khách hàng"}); err != nil || len(hits.Hits) == 0 {
		t.Fatalf("search after a restart: %+v, %v", hits, err)
	}
}
