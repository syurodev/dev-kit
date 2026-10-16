package bridge

import (
	"context"
	"errors"

	"devkit/internal/app"
)

// PDFs of a workspace folder, for the Files viewer. Like the rest of
// workspace_code.go, every call names (workspaceId, folderId, relativePath) and
// Go resolves the folder; audit metadata names the path, never the query or the
// pages. A page comes back as a picture: nothing in a PDF reaches the webview
// as markup or script.

func (s *AppService) pdfView() (*app.PDFView, error) {
	if s.container.PDFView == nil {
		return nil, app.ErrPDFUnavailable
	}
	return s.container.PDFView, nil
}

// WorkspacePdfInfo opens a PDF of a folder: its page count and page sizes, and
// the revision later calls name.
func (s *AppService) WorkspacePdfInfo(ctx context.Context, workspaceID, folderID, relativePath string) (app.PDFInfo, error) {
	result, err := s.call(ctx, "builtin.codegraph", "codegraph:fs", "file:"+relativePath, map[string]any{
		"workspace_id":  recordID(workspaceID),
		"folder_id":     recordID(folderID),
		"action":        "pdf_info",
		"relative_path": relativePath,
	}, func(ctx context.Context) (any, error) {
		pdf, err := s.pdfView()
		if err != nil {
			return nil, err
		}
		return pdf.Info(ctx, workspaceID, folderID, relativePath)
	})
	if err != nil {
		return app.PDFInfo{}, err
	}
	return result.(app.PDFInfo), nil
}

// WorkspacePdfPage draws one page (1-based) of the revision WorkspacePdfInfo
// reported, at dpi (0 = default; bounded by the engine).
func (s *AppService) WorkspacePdfPage(ctx context.Context, workspaceID, folderID, relativePath, revision string, page, dpi int) (app.PDFPage, error) {
	result, err := s.call(ctx, "builtin.codegraph", "codegraph:fs", "file:"+relativePath, map[string]any{
		"workspace_id":  recordID(workspaceID),
		"folder_id":     recordID(folderID),
		"action":        "pdf_page",
		"relative_path": relativePath,
	}, func(ctx context.Context) (any, error) {
		pdf, err := s.pdfView()
		if err != nil {
			return nil, err
		}
		return pdf.Page(ctx, workspaceID, folderID, relativePath, revision, page, dpi)
	})
	if err != nil {
		return app.PDFPage{}, err
	}
	return result.(app.PDFPage), nil
}

// WorkspacePdfSearch finds a phrase in the text of a PDF's pages.
func (s *AppService) WorkspacePdfSearch(ctx context.Context, workspaceID, folderID, relativePath, revision, query string) (app.PDFSearch, error) {
	result, err := s.call(ctx, "builtin.codegraph", "codegraph:fs", "file:"+relativePath, map[string]any{
		"workspace_id":  recordID(workspaceID),
		"folder_id":     recordID(folderID),
		"action":        "pdf_search",
		"relative_path": relativePath,
	}, func(ctx context.Context) (any, error) {
		pdf, err := s.pdfView()
		if err != nil {
			return nil, err
		}
		return pdf.Search(ctx, workspaceID, folderID, relativePath, revision, query)
	})
	if err != nil {
		return app.PDFSearch{}, err
	}
	return result.(app.PDFSearch), nil
}

var errRevealUnavailable = errors.New("bridge.revealUnavailable")

// WorkspaceFileReveal shows a file of a folder in the file manager (Finder,
// File Explorer…). Desktop only: in server mode the bridge is unauthenticated
// loopback HTTP and the machine it would act on has no one looking at it.
func (s *AppService) WorkspaceFileReveal(workspaceID, folderID, relativePath string) error {
	_, err := s.call(context.Background(), "builtin.codegraph", "codegraph:fs", "file:"+relativePath, map[string]any{
		"workspace_id":  recordID(workspaceID),
		"folder_id":     recordID(folderID),
		"action":        "reveal_file",
		"relative_path": relativePath,
	}, func(context.Context) (any, error) {
		if s.inServerMode() {
			return nil, errRevealUnavailable
		}
		code, err := s.workspaceCode()
		if err != nil {
			return nil, err
		}
		return nil, code.RevealFile(workspaceID, folderID, relativePath)
	})
	return err
}
