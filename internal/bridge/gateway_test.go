package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"changeme/internal/app"
	"changeme/internal/apperrors"
	"changeme/internal/capabilities"
	"changeme/internal/permissions"
	"changeme/internal/services/database"
	"changeme/internal/services/notes"
	sshservice "changeme/internal/services/ssh"
	"changeme/internal/vault"
)

func TestNotesBridgeUsesGateway(t *testing.T) {
	container := app.NewContainer()
	container.Permissions = permissions.NewPolicy(nil)
	container.Capabilities = capabilities.NewGateway(container.Permissions, container.Audit)
	service := NewAppService(container)

	_, err := service.NotesList()
	var publicErr *apperrors.AppError
	if !errors.As(err, &publicErr) {
		t.Fatalf("NotesList error = %T %v, want *apperrors.AppError", err, err)
	}
	if publicErr.Code != "permission_denied" {
		t.Fatalf("error code = %q, want permission_denied", publicErr.Code)
	}
}

func TestPublicErrorRedacts(t *testing.T) {
	const internalCause = "postgres driver failed at postgres://admin:password@internal-db:5432/devkit"
	container := app.NewContainerWithStores(
		vault.NewMemoryKeyringStore(),
		failingNotesRepository{err: errors.New(internalCause)},
		database.NewMemoryRepository(),
		sshservice.NewMemoryRepository(),
	)
	service := NewAppService(container)

	_, err := service.NotesList()
	var publicErr *apperrors.AppError
	if !errors.As(err, &publicErr) {
		t.Fatalf("NotesList error = %T %v, want *apperrors.AppError", err, err)
	}
	if publicErr.Code != "internal_error" {
		t.Fatalf("error code = %q, want internal_error", publicErr.Code)
	}
	payload, marshalErr := json.Marshal(publicErr)
	if marshalErr != nil {
		t.Fatalf("json.Marshal: %v", marshalErr)
	}
	for _, public := range []string{publicErr.Error(), string(payload)} {
		if strings.Contains(public, internalCause) || strings.Contains(public, "postgres://") || strings.Contains(public, "driver") {
			t.Fatalf("public error leaked internal cause: %s", public)
		}
	}
}

func TestPublicErrorNormalizesUnsafeExistingAppError(t *testing.T) {
	const internalCause = "postgres driver failed at postgres://admin:password@internal-db:5432/devkit"
	err := publicError("builtin.database", apperrors.Wrap("database_failure", internalCause, true, errors.New(internalCause)))

	if err.Code != "internal_error" {
		t.Fatalf("error code = %q, want internal_error", err.Code)
	}
	payload, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("json.Marshal: %v", marshalErr)
	}
	for _, public := range []string{err.Error(), string(payload)} {
		if strings.Contains(public, internalCause) || strings.Contains(public, "postgres://") || strings.Contains(public, "driver") {
			t.Fatalf("public error leaked internal AppError: %s", public)
		}
	}
}

func TestPublicErrorMapsOnlyRecognizedVaultLock(t *testing.T) {
	if err := publicError("builtin.vault", errors.New("keyring I/O failure")); err.Code != "internal_error" {
		t.Fatalf("unknown vault error code = %q, want internal_error", err.Code)
	}
	if err := publicError("builtin.vault", vault.ErrLocked); err.Code != "vault_locked" {
		t.Fatalf("vault locked error code = %q, want vault_locked", err.Code)
	}
}

func TestBridgeAuditUsesOnlyOpaqueIDs(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)
	const hostileProfileID = "postgres://admin:password@internal-db/devkit"
	const rawSQL = "SELECT password FROM credentials"
	_, _ = service.DBQuery(context.Background(), hostileProfileID, rawSQL)

	const hostileCommandID = "rm -rf /private/data"
	_, _ = service.ExecuteCommand(context.Background(), ExecuteCommandRequest{
		ID: hostileCommandID,
		Payload: map[string]any{
			"input": "do-not-audit-this-secret",
		},
	})

	auditText := fmt.Sprint(container.Audit.Events())
	for _, forbidden := range []string{hostileProfileID, rawSQL, hostileCommandID, "do-not-audit-this-secret"} {
		if strings.Contains(auditText, forbidden) {
			t.Fatalf("audit events leaked raw input %q: %s", forbidden, auditText)
		}
	}
}

type failingNotesRepository struct {
	err error
}

func (r failingNotesRepository) List() ([]notes.Record, error) {
	return nil, r.err
}

func (r failingNotesRepository) Get(string) (notes.Record, error) {
	return notes.Record{}, r.err
}

func (r failingNotesRepository) Put(notes.Record) error {
	return r.err
}

func (r failingNotesRepository) Delete(string) error {
	return r.err
}

var _ notes.Repository = failingNotesRepository{}
