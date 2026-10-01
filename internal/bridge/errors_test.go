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
