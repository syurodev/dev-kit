package bridge

import (
	"errors"
	"testing"

	"changeme/internal/app"
	"changeme/internal/apperrors"
	"changeme/internal/capabilities"
	"changeme/internal/database"
	"changeme/internal/permissions"
)

func TestVaultSearchReturnsCrossTypeHits(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)
	if _, err := service.SetupVault("master-password"); err != nil {
		t.Fatalf("SetupVault: %v", err)
	}

	if _, err := service.NotesCreate("staging runbook", "body"); err != nil {
		t.Fatalf("NotesCreate: %v", err)
	}
	if _, err := service.DBCreateProfile(database.CreateProfileInput{
		Name: "staging db",
		Host: "localhost",
		Port: 5432,
		Env:  "dev",
	}); err != nil {
		t.Fatalf("DBCreateProfile: %v", err)
	}

	results, err := service.VaultSearch("staging")
	if err != nil {
		t.Fatalf("VaultSearch: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("VaultSearch(staging) = %d results, want 2", len(results))
	}
	for _, r := range results {
		if r.ID == "" || r.Type == "" || r.Label == "" {
			t.Fatalf("result missing fields: %+v", r)
		}
	}
}

func TestVaultSearchDeniedWithoutGrant(t *testing.T) {
	container := app.NewContainer()
	container.Permissions = permissions.NewPolicy(nil) // grant nothing
	container.Capabilities = capabilities.NewGateway(container.Permissions, container.Audit)
	service := NewAppService(container)

	_, err := service.VaultSearch("anything")
	var publicErr *apperrors.AppError
	if !errors.As(err, &publicErr) {
		t.Fatalf("VaultSearch error = %T %v, want *apperrors.AppError", err, err)
	}
	if publicErr.Code != "permission_denied" {
		t.Fatalf("error code = %q, want permission_denied", publicErr.Code)
	}
}
