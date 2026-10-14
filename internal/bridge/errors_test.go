package bridge

import (
	"devkit/internal/codegraph"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"

	"devkit/internal/app"
	"devkit/internal/apperrors"
	"devkit/internal/cloudconfig"
	"devkit/internal/notes"
	"devkit/internal/syncclient"
	"devkit/internal/tasks"
)

func TestPublicErrorMapsCloudConfigUnavailable(t *testing.T) {
	err := publicError("builtin.sync", cloudconfig.ErrUnavailable)
	if err == nil || err.Error() != "connection_failed: Cloud configuration is unavailable. Connect to the internet once or set DEVKIT_OIDC_ISSUER." {
		t.Fatalf("public error = %v", err)
	}
}

func TestPublicErrorMapsAppBelowMinimum(t *testing.T) {
	err := publicError("builtin.sync", cloudconfig.ErrAppBelowMinimum)
	if err == nil || err.Error() != "connection_failed: This DevKit version is too old for Sync. Please update." {
		t.Fatalf("public error = %v", err)
	}
}

func TestAuthLoginReturnsCloudConfigUnavailable(t *testing.T) {
	container := app.NewContainer()
	container.SetSyncBlockedErr(cloudconfig.ErrUnavailable)
	if err := app.RegisterBuiltins(container); err != nil {
		t.Fatalf("RegisterBuiltins: %v", err)
	}
	service := NewAppService(container)

	_, err := service.AuthLogin(t.Context())
	if err == nil || err.Error() != "connection_failed: Cloud configuration is unavailable. Connect to the internet once or set DEVKIT_OIDC_ISSUER." {
		t.Fatalf("AuthLogin error = %v", err)
	}
}

func TestSyncConnectReturnsAppBelowMinimum(t *testing.T) {
	container := app.NewContainer()
	container.Sync = &fakeSyncService{}
	container.SetAuth(&fakeAuthService{})
	container.SetSyncBlockedErr(cloudconfig.ErrAppBelowMinimum)
	if err := app.RegisterBuiltins(container); err != nil {
		t.Fatalf("RegisterBuiltins: %v", err)
	}
	service := NewAppService(container)

	err := service.SyncConnect(t.Context())
	if err == nil || err.Error() != "connection_failed: This DevKit version is too old for Sync. Please update." {
		t.Fatalf("SyncConnect error = %v", err)
	}
}

func TestSyncDependencyErrFallsBackToInvalidDependencies(t *testing.T) {
	service := NewAppService(app.NewContainer())
	if !errors.Is(service.syncDependencyErr(), syncclient.ErrInvalidDependencies) {
		t.Fatalf("syncDependencyErr = %v", service.syncDependencyErr())
	}
}

func TestPublicErrorMapsCodeGraphFileInputs(t *testing.T) {
	for _, cause := range []error{codegraph.ErrPathOutsideProject, os.ErrNotExist, codegraph.ErrIndexNotReady} {
		got := publicError("builtin.codegraph", fmt.Errorf("read: %w", cause))
		if !strings.HasPrefix(got.Error(), "validation_failed:") {
			t.Fatalf("%v: %v", cause, got)
		}
	}
}

// A missing file used to surface as one fixed sentence, so a failed index gave
// no hint whether the analyzer, its runtime or the project folder was gone.
func TestPublicErrorNamesTheMissingCodeGraphPath(t *testing.T) {
	launch := fmt.Errorf("codegraph: analyze %q: %w", "/work/app", &fs.PathError{Op: "fork/exec", Path: "/opt/jdk/bin/java", Err: fs.ErrNotExist})
	got := publicError("builtin.codegraph", launch)
	want := "validation_failed: CodeGraph file or directory not found: /opt/jdk/bin/java (fork/exec)."
	if got.Error() != want {
		t.Fatalf("error = %q, want %q", got.Error(), want)
	}

	bare := publicError("builtin.codegraph", fmt.Errorf("read: %w", os.ErrNotExist))
	if bare.Error() != "validation_failed: CodeGraph file or directory not found." {
		t.Fatalf("error without a path = %q", bare.Error())
	}
}

// An invalid folder color used to fall through to internal_error, which
// told an MCP agent DevKit had broken rather than that its input was wrong.
func TestPublicErrorMapsNotesColorToValidation(t *testing.T) {
	cause := fmt.Errorf("%w %q: use one of red, blue", notes.ErrInvalidColor, "#fff")
	err := publicError("builtin.notes", cause)
	if err == nil || err.Code != codeValidationFailed || !strings.Contains(err.Message, "#fff") {
		t.Fatalf("public error = %v, want validation_failed carrying the message", err)
	}
	if err := publicError("builtin.notes", notes.ErrTooManyTags); err == nil || err.Code != codeValidationFailed {
		t.Fatalf("too many tags = %v, want validation_failed", err)
	}
}

// A move that lost its race is something the board answers by showing where
// the task is now, so it has its own code rather than the unexpected-error
// fallback it used to fall through to.
func TestPublicErrorMapsTaskStatusChanged(t *testing.T) {
	err := publicError("builtin.tasks", fmt.Errorf("transition: %w", tasks.ErrStatusChanged))
	if err == nil || err.Code != codeTaskStatusChanged {
		t.Fatalf("public error = %v", err)
	}
	if got := publicError("builtin.tasks", tasks.ErrDependencyCycle); got == nil || got.Code != codeValidationFailed {
		t.Fatalf("dependency cycle = %v", got)
	}
	if got := publicError("builtin.tasks", tasks.ErrParentHasParent); got == nil || got.Code != codeValidationFailed {
		t.Fatalf("parent-has-parent = %v", got)
	}
}

// pdf.* and memory.* AppErrors are raised inside Caller.Call by tools whose
// messages are written for the reader. They used to be redacted to
// internal_error, hiding the one thing an agent or a result view can act on.
func TestPublicErrorKeepsPDFAndMemoryErrors(t *testing.T) {
	for _, tc := range []struct{ subject, code, message string }{
		{"builtin.agenthub", "pdf.notFound", "No such document."},
		{"builtin.agenthub", "pdf.sessionDenied", "From a terminal, pass sessionId: the Agent Hub session whose documents to read."},
		{"builtin.agenthub", "pdf.pageRange", "page out of range: page 9 of a 3-page document"},
		{"builtin.agenthub", "pdf.timeout", "Reading the document took too long; ask for fewer pages."},
		{"builtin.agenthub", "pdf.unreadable", "The PDF is damaged and cannot be read."},
		{"builtin.workspace-memory", "memory.notFound", "Workspace memory was not found."},
		{"builtin.workspace-memory", "memory.revisionConflict", "Workspace memory changed. Refetch the current entry or history before editing."},
		{"builtin.workspace-memory", "memory.workspaceDenied", "Select a workspace for this session; terminal callers must supply workspaceId."},
	} {
		got := publicError(tc.subject, apperrors.New(tc.code, tc.message, true))
		if got == nil || got.Code != tc.code || got.Message != tc.message || !got.Recoverable {
			t.Errorf("publicError(%s) = %+v, want the code and message kept", tc.code, got)
		}
	}
	// A wrapped AppError is found the same way.
	wrapped := fmt.Errorf("call: %w", apperrors.New("pdf.timeout", "Reading the document took too long; ask for fewer pages.", true))
	if got := publicError("builtin.agenthub", wrapped); got == nil || got.Code != "pdf.timeout" {
		t.Errorf("wrapped pdf.timeout = %+v", got)
	}
}

// pdf.failed and pdf.unavailable carry the engine's or store's own error text
// (file paths, wazero and driver detail), so they keep their code and lose
// that text.
func TestPublicErrorRedactsPDFDetail(t *testing.T) {
	for _, code := range []string{"pdf.failed", "pdf.unavailable"} {
		raw := "Could not read the document: open /home/u/.local/share/dev-kit/agent-documents/s1/doc1.pdf: permission denied"
		got := publicError("builtin.agenthub", apperrors.New(code, raw, true))
		if got == nil || got.Code != code {
			t.Fatalf("%s = %+v, want the code kept", code, got)
		}
		if strings.Contains(got.Message, "/home") || strings.Contains(got.Message, "permission denied") || got.Message == "" {
			t.Errorf("%s message = %q, want a generic one", code, got.Message)
		}
	}
}

func TestPublicErrorStillRedactsUnknownCodes(t *testing.T) {
	// The kept codes are a list, not a prefix: a pdf.* or memory.* code
	// nobody vetted may carry an underlying error's text in its message.
	for _, code := range []string{"pdfx.notFound", "memoryish", "storage.boom", "pdf.somethingNew", "memory.somethingNew", "pdf."} {
		got := publicError("builtin.agenthub", apperrors.New(code, "secret detail", true))
		if got == nil || got.Code != codeInternalError || strings.Contains(got.Message, "secret") {
			t.Errorf("%s = %+v, want internal_error", code, got)
		}
	}
}

// The engine's own text rides as an AppError's cause (internal/mcp
// pdfFailure): logged here, never published, whatever the code.
func TestPublicErrorNeverPublishesAPDFCause(t *testing.T) {
	engine := errors.New("wasm error: unreachable\nwasm stack trace:\n\tpdfium.wasm.FPDF_LoadMemDocument64(i32,i64,i32) i32")
	for _, tc := range []struct{ code, message string }{
		{"pdf.unreadable", "The PDF is damaged and cannot be read."},
		{"pdf.unavailable", "DevKit's PDF reader is not available."},
		{"pdf.failed", "Could not read the document."},
	} {
		got := publicError("builtin.agenthub", fmt.Errorf("call: %w", apperrors.Wrap(tc.code, tc.message, true, fmt.Errorf("pdfdoc: the PDF is damaged and cannot be read: %w", engine))))
		if got == nil || got.Code != tc.code || got.Message != tc.message {
			t.Errorf("%s = %+v, want %q", tc.code, got, tc.message)
			continue
		}
		if got.Unwrap() != nil || strings.Contains(got.Error(), "wasm") {
			t.Errorf("%s published its cause: %+v", tc.code, got)
		}
	}
}
