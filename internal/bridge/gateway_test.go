package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"devkit/internal/app"
	"devkit/internal/apperrors"
	"devkit/internal/capabilities"
	"devkit/internal/environments"
	"devkit/internal/identity"
	"devkit/internal/permissions"
	sshservice "devkit/internal/ssh"
	"devkit/internal/vault"
	"devkit/internal/vaultitem"
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

// The window is told "Permission denied."; an agent is told what was missing,
// because it cannot otherwise tell a person's no from a grant that was never
// installed (every agent fetch and pull was refused for the second reason and
// reported as the first).
func TestPermissionDenialNamesTheMissingGrantOnlyToNonWindowCallers(t *testing.T) {
	container := app.NewContainer()
	container.Permissions = permissions.NewPolicy(nil)
	container.Capabilities = capabilities.NewGateway(container.Permissions, container.Audit)
	service := NewAppService(container)
	run := func(ctx context.Context) string {
		t.Helper()
		_, err := service.call(ctx, "builtin.git", "git:write", "repo:sync", nil, func(context.Context) (any, error) { return nil, nil })
		if err == nil {
			t.Fatal("call was allowed with no grants")
		}
		return err.Error()
	}

	if got := run(identity.WithCaller(context.Background(), identity.UI())); got != "permission_denied: Permission denied." {
		t.Fatalf("window caller got %q", got)
	}
	got := run(identity.WithCaller(context.Background(), identity.Agent("11111111-2222-3333-4444-555555555555")))
	if !strings.Contains(got, "permission_denied") || !strings.Contains(got, "git:write on repo:sync") {
		t.Fatalf("agent caller got %q, want the capability and scope named", got)
	}
}

func TestAppServiceCallSetsMetadata(t *testing.T) {
	// Default container already grants notes:read / note:list; audit records Call metadata.
	container := app.NewContainer()
	svc := NewAppService(container)

	_, err := NewCaller(svc).Call(context.Background(), "builtin.notes", "notes:read", "note:list", map[string]any{
		"origin": "mcp",
		"action": "list",
	}, func(context.Context) (any, error) {
		return []string{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	events := container.Audit.Events()
	if len(events) != 1 {
		t.Fatalf("len(audit events) = %d, want 1", len(events))
	}
	if events[0].Metadata["origin"] != "mcp" {
		t.Fatalf("origin = %v", events[0].Metadata["origin"])
	}
}

func TestPublicErrorRedacts(t *testing.T) {
	const internalCause = "postgres driver failed at postgres://admin:password@internal-db:5432/devkit"
	container := app.NewContainerWithStores(
		vault.NewMemoryKeyringStore(),
		failingNotesRepository{err: errors.New(internalCause)},
		environments.NewMemoryRepository(),
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

func TestPublicErrorPrioritizesWrappedVaultLock(t *testing.T) {
	err := publicError("builtin.database", apperrors.Wrap("database_failure", "unsafe storage detail", false, vault.ErrLocked))
	if err.Code != "vault_locked" {
		t.Fatalf("error code = %q, want vault_locked", err.Code)
	}
}

func TestPublicErrorDoesNotTrustWrappedVaultLockedCode(t *testing.T) {
	err := publicError("builtin.vault", apperrors.Wrap("vault_locked", "unsafe storage detail", true, errors.New("keyring I/O failure")))
	if err.Code != "internal_error" {
		t.Fatalf("error code = %q, want internal_error", err.Code)
	}
}

func TestPublicErrorPrioritizesWrappedCancellation(t *testing.T) {
	err := publicError("builtin.database", apperrors.Wrap("database_failure", "unsafe query detail", false, context.Canceled))
	if err.Code != "operation_cancelled" {
		t.Fatalf("error code = %q, want operation_cancelled", err.Code)
	}
}

func TestBridgeAuditUsesOnlyOpaqueIDs(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)
	const hostileProfileID = "postgres://admin:password@internal-db/devkit"
	const rawSQL = "SELECT password FROM credentials"
	_, _ = service.DBQuery(context.Background(), hostileProfileID, rawSQL, 0, "")

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

func TestSSHBridgeDoesNotAuditPassphrase(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)
	const passphrase = "one-time-passphrase-must-not-be-audited"

	_ = service.SSHTestConnection(context.Background(), "missing-profile", passphrase)

	if events := fmt.Sprint(container.Audit.Events()); strings.Contains(events, passphrase) {
		t.Fatalf("audit events leaked SSH passphrase: %s", events)
	}
}

func TestSSHForwardUsesDedicatedCapability(t *testing.T) {
	container := app.NewContainer()
	container.Permissions = permissions.NewPolicy([]permissions.Grant{
		{SubjectID: "builtin.ssh", Capability: "ssh:connect", Scope: "profile:*"},
	})
	container.Capabilities = capabilities.NewGateway(container.Permissions, container.Audit)
	service := NewAppService(container)

	_, err := service.SSHStartForward(context.Background(), "000000000000000000000000", sshservice.ForwardSpec{
		Direction:  sshservice.ForwardDirectionLocal,
		ListenHost: "127.0.0.1",
		TargetHost: "127.0.0.1",
		TargetPort: 5432,
	}, "one-time-passphrase")
	var publicErr *apperrors.AppError
	if !errors.As(err, &publicErr) {
		t.Fatalf("SSHStartForward error = %T %v, want *apperrors.AppError", err, err)
	}
	if publicErr.Code != codePermissionDenied {
		t.Fatalf("error code = %q, want permission_denied", publicErr.Code)
	}
}

func TestSSHForwardBridgeDoesNotAuditPassphrase(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)
	const passphrase = "forward-passphrase-must-not-be-audited"

	_, _ = service.SSHStartForward(context.Background(), "000000000000000000000000", sshservice.ForwardSpec{
		Direction:  sshservice.ForwardDirectionLocal,
		ListenHost: "0.0.0.0",
		TargetHost: "127.0.0.1",
		TargetPort: 5432,
	}, passphrase)

	if events := fmt.Sprint(container.Audit.Events()); strings.Contains(events, passphrase) {
		t.Fatalf("audit events leaked SSH forwarding passphrase: %s", events)
	}
}

type failingNotesRepository struct {
	err error
}

func (r failingNotesRepository) List() ([]vaultitem.Record, error) {
	return nil, r.err
}

func (r failingNotesRepository) ListByType(string) ([]vaultitem.Record, error) {
	return nil, r.err
}

func (r failingNotesRepository) ListByTypeAndMetaField(string, string, string) ([]vaultitem.Record, error) {
	return nil, r.err
}

func (r failingNotesRepository) Get(string) (vaultitem.Record, error) {
	return vaultitem.Record{}, r.err
}

func (r failingNotesRepository) Put(vaultitem.Record) error {
	return r.err
}

func (r failingNotesRepository) Delete(string) error {
	return r.err
}

var _ vaultitem.Repository = failingNotesRepository{}

func TestSSHWorkspaceMethodsRequireConnectionGrant(t *testing.T) {
	container := app.NewContainer()
	container.Permissions = permissions.NewPolicy(nil)
	container.Capabilities = capabilities.NewGateway(container.Permissions, container.Audit)
	service := NewAppService(container)
	const id = "000000000000000000000000"
	_, terminalErr := service.SSHStartTerminal(context.Background(), id, 24, 80)
	_, updateErr := service.SSHUpdateProfile(id, sshservice.UpdateProfileInput{Name: "edited", Host: "host", Port: 22, Username: "deploy", Env: "dev"})
	for _, err := range []error{terminalErr, updateErr} {
		var publicErr *apperrors.AppError
		if !errors.As(err, &publicErr) || publicErr.Code != codePermissionDenied {
			t.Fatalf("workspace call bypassed grant: %v", err)
		}
	}
}
