package bridge

import (
	"errors"
	"testing"

	"devkit/internal/app"
	"devkit/internal/cloudconfig"
	"devkit/internal/syncclient"
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
	container.SyncBlocked = cloudconfig.ErrUnavailable
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
	container.Auth = &fakeAuthService{}
	container.SyncBlocked = cloudconfig.ErrAppBelowMinimum
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
