package bridge

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"devkit/internal/app"
	"devkit/internal/pdfdoc"
)

// pdfWorkspace is codeWorkspace with a PDF engine and a document in the first folder.
func pdfWorkspace(t *testing.T) (*AppService, string, string) {
	t.Helper()
	svc, ws := codeWorkspace(t)
	engine := pdfdoc.New(pdfdoc.Options{CacheDir: t.TempDir()})
	t.Cleanup(engine.Close)
	svc.container.PDFView = app.NewPDFView(svc.container.WorkspaceCode, engine)
	data, err := os.ReadFile(filepath.Join("..", "pdfdoc", "testdata", "webprint.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	folder := ws.Services[0]
	if err := os.WriteFile(filepath.Join(folder.Path, "doc.pdf"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return svc, ws.Workspace.ID, folder.ID
}

func TestPdfViewerOpensDrawsAndSearchesAFolderPDF(t *testing.T) {
	svc, w, f := pdfWorkspace(t)
	ctx := context.Background()

	info, err := svc.WorkspacePdfInfo(ctx, w, f, "doc.pdf")
	if err != nil || info.Pages < 2 || len(info.PageSizes) != info.Pages || info.Revision == "" {
		t.Fatalf("info = %+v, %v", info, err)
	}
	page, err := svc.WorkspacePdfPage(ctx, w, f, "doc.pdf", info.Revision, 2, 96)
	if err != nil || page.Page != 2 || len(page.Data) == 0 || page.Width == 0 || page.MIME == "" {
		t.Fatalf("page = %+v, %v", page, err)
	}
	if _, err := svc.WorkspacePdfPage(ctx, w, f, "doc.pdf", info.Revision, info.Pages+1, 96); publicCode(err) != "validation_failed" {
		t.Fatalf("page past the end: %q (%v)", publicCode(err), err)
	}
	found, err := svc.WorkspacePdfSearch(ctx, w, f, "doc.pdf", info.Revision, "table")
	if err != nil || !found.Complete || found.Hits == nil {
		t.Fatalf("search = %+v, %v", found, err)
	}
}

func TestPdfViewerNoticesAFileReplacedAfterItWasOpened(t *testing.T) {
	svc, w, f := pdfWorkspace(t)
	ctx := context.Background()
	info, err := svc.WorkspacePdfInfo(ctx, w, f, "doc.pdf")
	if err != nil {
		t.Fatal(err)
	}
	scan, err := os.ReadFile(filepath.Join("..", "pdfdoc", "testdata", "scan.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	folder, _ := svc.container.WorkspaceCode.Folder(w, f)
	if err := os.WriteFile(filepath.Join(folder.Path, "doc.pdf"), scan, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WorkspacePdfPage(ctx, w, f, "doc.pdf", info.Revision, 1, 96); publicCode(err) != "pdf_changed" {
		t.Fatalf("code = %q (%v), want pdf_changed", publicCode(err), err)
	}
	// Opening again is the way forward, and it shows the new document.
	again, err := svc.WorkspacePdfInfo(ctx, w, f, "doc.pdf")
	if err != nil || again.Revision == info.Revision {
		t.Fatalf("reopen = %+v, %v", again, err)
	}
}

func TestPdfViewerRefusesWhatIsNotAPDFOrLeavesTheFolder(t *testing.T) {
	svc, w, f := pdfWorkspace(t)
	ctx := context.Background()
	folder, _ := svc.container.WorkspaceCode.Folder(w, f)
	if err := os.WriteFile(filepath.Join(folder.Path, "fake.pdf"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WorkspacePdfInfo(ctx, w, f, "fake.pdf"); publicCode(err) != "validation_failed" {
		t.Fatalf("a text file named .pdf: %q (%v)", publicCode(err), err)
	}
	if _, err := svc.WorkspacePdfInfo(ctx, w, f, "../doc.pdf"); publicCode(err) != "validation_failed" {
		t.Fatalf("outside the folder: %q (%v)", publicCode(err), err)
	}
	if _, err := svc.WorkspacePdfInfo(ctx, w, "ffffffffffffffffffffffff", "doc.pdf"); publicCode(err) != "folder_not_in_workspace" {
		t.Fatalf("unknown folder: %q (%v)", publicCode(err), err)
	}
	// With no engine wired the viewer says so.
	svc.container.PDFView = nil
	if _, err := svc.WorkspacePdfInfo(ctx, w, f, "doc.pdf"); publicCode(err) != "pdf.unavailable" {
		t.Fatalf("no engine: %q (%v)", publicCode(err), err)
	}
}
