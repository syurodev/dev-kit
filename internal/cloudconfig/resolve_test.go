package cloudconfig

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveStartupEnvWinsOverCache(t *testing.T) {
	t.Setenv("DEVKIT_OIDC_ISSUER", "https://env.example.test/realms/devkit")

	dir := t.TempDir()
	store := NewFileStore(dir)
	if err := store.Save(Snapshot{OIDCIssuer: "https://cache.example.test/realms/devkit"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"oidc_issuer":"https://remote.example.test/realms/devkit"}`))
	}))
	defer server.Close()

	result, err := ResolveStartup(context.Background(), StartupOptions{
		ConfigDir: dir,
		Client:    server.Client(),
		Timeout:   startupFetchTimeout,
	})
	if err != nil {
		t.Fatalf("ResolveStartup: %v", err)
	}
	if !result.FromEnv || !result.HasAuthConfig {
		t.Fatalf("result = %#v", result)
	}
	if result.AuthConfig.Issuer != "https://env.example.test/realms/devkit" {
		t.Fatalf("issuer = %q", result.AuthConfig.Issuer)
	}
}

func TestResolveStartupUsesCacheWhenFetchFails(t *testing.T) {
	t.Setenv("DEVKIT_OIDC_ISSUER", "")

	dir := t.TempDir()
	store := NewFileStore(dir)
	if err := store.Save(Snapshot{
		OIDCIssuer:    "https://cache.example.test/realms/devkit",
		MinAppVersion: "0.0.1",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	result, err := ResolveStartup(context.Background(), StartupOptions{
		ConfigDir: dir,
		Client:    &http.Client{Timeout: startupFetchTimeout},
		Timeout:   startupFetchTimeout,
	})
	if err != nil {
		t.Fatalf("ResolveStartup: %v", err)
	}
	if !result.HasAuthConfig || result.FromEnv || result.Unavailable {
		t.Fatalf("result = %#v", result)
	}
	if result.AuthConfig.Issuer != "https://cache.example.test/realms/devkit" {
		t.Fatalf("issuer = %q", result.AuthConfig.Issuer)
	}
}

func TestResolveStartupUnavailableWithoutCacheOrFetch(t *testing.T) {
	t.Setenv("DEVKIT_OIDC_ISSUER", "")

	result, err := ResolveStartup(context.Background(), StartupOptions{
		ConfigDir: t.TempDir(),
		Client:    &http.Client{Timeout: startupFetchTimeout},
		Timeout:   startupFetchTimeout,
	})
	if err != nil {
		t.Fatalf("ResolveStartup: %v", err)
	}
	if result.HasAuthConfig || result.SyncBlocked() == nil {
		t.Fatalf("result = %#v", result)
	}
	if result.SyncBlocked() != ErrUnavailable {
		t.Fatalf("blocked = %v", result.SyncBlocked())
	}
}

func TestResolveStartupBlocksBelowMinimum(t *testing.T) {
	t.Setenv("DEVKIT_OIDC_ISSUER", "https://env.example.test/realms/devkit")

	dir := t.TempDir()
	store := NewFileStore(dir)
	if err := store.Save(Snapshot{OIDCIssuer: "https://cache.example.test/realms/devkit", MinAppVersion: "9.9.9"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	result, err := ResolveStartup(context.Background(), StartupOptions{
		ConfigDir: dir,
		Client:    &http.Client{Timeout: startupFetchTimeout},
		Timeout:   startupFetchTimeout,
	})
	if err != nil {
		t.Fatalf("ResolveStartup: %v", err)
	}
	if result.Blocked != ErrAppBelowMinimum {
		t.Fatalf("blocked = %v", result.Blocked)
	}
}

func TestResolveStartupDetectsIssuerChange(t *testing.T) {
	t.Setenv("DEVKIT_OIDC_ISSUER", "")

	dir := t.TempDir()
	store := NewFileStore(dir)
	if err := store.Save(Snapshot{OIDCIssuer: "https://old.example.test/realms/devkit"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	result, err := ResolveStartup(context.Background(), StartupOptions{
		ConfigDir: dir,
		Fetch: func(context.Context, string, string, *http.Client) (Snapshot, error) {
			return Snapshot{OIDCIssuer: "https://new.example.test/realms/devkit"}, nil
		},
	})
	if err != nil {
		t.Fatalf("ResolveStartup: %v", err)
	}
	if !result.IssuerChanged {
		t.Fatal("expected issuer change")
	}
	if result.AuthConfig.Issuer != "https://new.example.test/realms/devkit" {
		t.Fatalf("issuer = %q", result.AuthConfig.Issuer)
	}
}

func TestResolveStartupRejectsInvalidEnv(t *testing.T) {
	t.Setenv("DEVKIT_OIDC_ISSUER", "http://auth.example.test/realms/devkit")

	_, err := ResolveStartup(context.Background(), StartupOptions{ConfigDir: t.TempDir()})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveStartupRejectsUnreadableCache(t *testing.T) {
	t.Setenv("DEVKIT_OIDC_ISSUER", "")

	dir := t.TempDir()
	path := filepath.Join(dir, "DevKit", "desktop-config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := ResolveStartup(context.Background(), StartupOptions{ConfigDir: dir})
	if err == nil {
		t.Fatal("expected error")
	}
}
